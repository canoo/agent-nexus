const { invoke } = window.__TAURI__.core;

const collectionHeading = document.querySelector("#collection-heading");
const collectionDetail = document.querySelector("#collection-detail");
const consents = document.querySelector("#consents");
const hostState = document.querySelector("#host-state");
const resumeButton = document.querySelector("#resume-collection");
const pauseButton = document.querySelector("#pause-collection");
const disableButton = document.querySelector("#disable-collection");
const controlFeedback = document.querySelector("#control-feedback");
const consentAcknowledgement = document.querySelector("#consent-acknowledgement");
const consentFeedback = document.querySelector("#consent-feedback");
const nativeHostForm = document.querySelector("#native-host-form");
const registerHostButton = document.querySelector("#register-host");
const hostFeedback = document.querySelector("#host-feedback");

const storedSpansElement = document.querySelector("#stored-spans");
const retentionDaysInput = document.querySelector("#retention-days");
const saveRetentionButton = document.querySelector("#save-retention");
const pruneHistoryButton = document.querySelector("#prune-history");
const clearAcknowledgement = document.querySelector("#clear-acknowledgement");
const clearHistoryButton = document.querySelector("#clear-history");
const dataFeedback = document.querySelector("#data-feedback");

const unregisterHostButton = document.querySelector("#unregister-host");

function updateRemovalState() {
  const browserSelect = document.querySelector("#browser");
  const selectedBrowser = browserSelect ? browserSelect.value : null;
  const validBrowser = selectedBrowser === "chrome" || selectedBrowser === "edge";
  const regControlAvailable = currentStatus?.nativeHost?.registrationControl === "available";
  const browserState = validBrowser && currentStatus?.nativeHost ? currentStatus.nativeHost[selectedBrowser] : null;
  const eligibleState = browserState === "registered" || browserState === "error";

  const canUnregister = !isBusy && regControlAvailable && validBrowser && eligibleState;
  if (unregisterHostButton) {
    unregisterHostButton.disabled = !canUnregister;
  }
}

document.querySelector("#browser")?.addEventListener("change", () => {
  updateControlStates();
});

unregisterHostButton?.addEventListener("click", async () => {
  const selectedBrowser = document.querySelector("#browser")?.value;
  const validBrowser = selectedBrowser === "chrome" || selectedBrowser === "edge";
  const regControlAvailable = currentStatus?.nativeHost?.registrationControl === "available";
  const browserState = validBrowser && currentStatus?.nativeHost ? currentStatus.nativeHost[selectedBrowser] : null;
  const eligibleState = browserState === "registered" || browserState === "error";

  if (isBusy || !regControlAvailable || !validBrowser || !eligibleState) {
    return;
  }

  await runAction(async () => {
    if (hostFeedback) hostFeedback.textContent = "Removing native host registration...";
    const reply = await invoke("unregister_native_host", { request: { browser: selectedBrowser } });
    renderDashboard(reply);
    if (hostFeedback) hostFeedback.textContent = "This browser’s native host is removed. Collection and local history are unchanged.";
  }, hostFeedback);
});

const setupSection = document.querySelector("#setup-section");
const setupAcknowledgement = document.querySelector("#setup-acknowledgement");
const initializeStoreButton = document.querySelector("#initialize-store");
const setupFeedback = document.querySelector("#setup-feedback");

let currentStatus = null;
let isBusy = false;

const safeCollectionCopy = Object.freeze({
  enabled: ["Collection is enabled", "Only fixed, consented activity envelopes can be accepted by the shared local store."],
  disabled: ["Collection is disabled", "No new Companion activity can be recorded."],
});
const safeStoreCopy = Object.freeze({
  unavailable: "The shared local observability store is unavailable. Collection status cannot be confirmed.",
  error: "The local observability status is unavailable. Nothing is being enabled.",
});
const safeHostCopy = Object.freeze({
  registered: "registered",
  unregistered: "unregistered",
  unavailable: "unavailable",
  error: "needs attention",
});

const allowedConsentLabels = Object.freeze({
  "Chrome browser:ChatGPT": "Chrome browser: ChatGPT",
  "Chrome browser:Claude": "Chrome browser: Claude",
  "Chrome browser:Gemini": "Chrome browser: Gemini",
  "Chrome browser:Microsoft Copilot": "Chrome browser: Microsoft Copilot",
  "Chrome browser:Perplexity": "Chrome browser: Perplexity",
  "Edge browser:ChatGPT": "Edge browser: ChatGPT",
  "Edge browser:Claude": "Edge browser: Claude",
  "Edge browser:Gemini": "Edge browser: Gemini",
  "Edge browser:Microsoft Copilot": "Edge browser: Microsoft Copilot",
  "Edge browser:Perplexity": "Edge browser: Perplexity",
  "Desktop foreground adapter:ChatGPT": "Desktop foreground adapter: ChatGPT",
  "Desktop foreground adapter:Claude": "Desktop foreground adapter: Claude",
  "Desktop foreground adapter:Gemini": "Desktop foreground adapter: Gemini",
  "Desktop foreground adapter:Microsoft Copilot": "Desktop foreground adapter: Microsoft Copilot",
  "Desktop foreground adapter:Perplexity": "Desktop foreground adapter: Perplexity",
});
const allowedConsentStates = new Set(["enabled", "disabled", "unavailable"]);

const safeActionErrors = new Set([
  "The local NEXUS observability store is unavailable; no setting was changed.",
  "The local NEXUS observability store could not be updated; no setting was changed.",
  "Native-host registration is unavailable in this installation.",
  "Native-host registration could not be started.",
  "Native-host registration was not completed.",
  "Native-host registration could not be updated.",
  "Use a supported browser, a published Chrome-format extension ID, and an existing absolute host path.",
  "Local Companion data could not be updated; refresh status before trying again.",
  "Local Companion setup could not be completed; existing data was not replaced.",
]);

function safeError(error) {
  const message = typeof error === "string" ? error : (error && error.message) ? error.message : "";
  return safeActionErrors.has(message) ? message : "The requested local action could not be completed.";
}

function validDataControls(controls) {
  return (
    controls &&
    controls.state === "ready" &&
    Object.keys(controls).length === 3 &&
    typeof controls.retentionDays === "number" &&
    Number.isInteger(controls.retentionDays) &&
    controls.retentionDays >= 0 &&
    controls.retentionDays <= 365 &&
    typeof controls.storedSpans === "number" &&
    Number.isSafeInteger(controls.storedSpans) &&
    controls.storedSpans >= 0
  );
}

function isDataControlsReady(status) {
  return Boolean(
    status &&
    status.store === "ready" &&
    validDataControls(status.dataControls)
  );
}

const toolLabels = Object.freeze({ chatgpt: "ChatGPT", claude: "Claude", gemini: "Gemini", copilot: "Microsoft Copilot", perplexity: "Perplexity" });
const adapterLabels = Object.freeze({ "browser-chrome": "Chrome browser", "browser-edge": "Edge browser", "desktop-foreground-app": "Desktop foreground adapter" });
function validConsent(item) {
  return item && Object.hasOwn(toolLabels, item.toolId) && Object.hasOwn(adapterLabels, item.adapterId)
    && toolLabels[item.toolId] === item.tool && adapterLabels[item.adapterId] === item.adapter
    && allowedConsentStates.has(item.state);
}

function hasActiveBrowserGrant(status) {
  if (!status || !Array.isArray(status.consents)) return false;
  return status.consents.some(
    (c) =>
      validConsent(c) && (c.adapterId === "browser-chrome" || c.adapterId === "browser-edge") &&
      c.state === "enabled"
  );
}

function updateControlStates() {
  updateRemovalState();
  const isReady = currentStatus && currentStatus.store === "ready" && Object.hasOwn(safeCollectionCopy, currentStatus.collection);
  const isEnabled = currentStatus && currentStatus.collection === "enabled";
  const hasGrant = hasActiveBrowserGrant(currentStatus);
  const acknowledged = Boolean(consentAcknowledgement && consentAcknowledgement.checked);
  const dataReady = isDataControlsReady(currentStatus);
  const clearAcknowledged = Boolean(clearAcknowledgement && clearAcknowledgement.checked);

  if (resumeButton) {
    resumeButton.disabled = isBusy || !isReady || isEnabled || !hasGrant;
  }
  if (pauseButton) {
    pauseButton.disabled = isBusy || !isReady || !isEnabled;
  }
  if (disableButton) {
    disableButton.disabled = isBusy || !isReady;
  }
  if (registerHostButton) {
    const isHostAvailable =
      currentStatus &&
      currentStatus.nativeHost &&
      currentStatus.nativeHost.registrationControl === "available";
    registerHostButton.disabled = isBusy || !isHostAvailable;
  }
  if (consentAcknowledgement) {
    consentAcknowledgement.disabled = isBusy || !isReady;
  }

  if (retentionDaysInput) {
    retentionDaysInput.disabled = isBusy || !dataReady;
  }
  if (saveRetentionButton) {
    saveRetentionButton.disabled = isBusy || !dataReady;
  }
  if (pruneHistoryButton) {
    pruneHistoryButton.disabled = isBusy || !dataReady;
  }
  if (clearAcknowledgement) {
    clearAcknowledgement.disabled = isBusy || !dataReady;
  }
  if (clearHistoryButton) {
    clearHistoryButton.disabled = isBusy || !dataReady || !clearAcknowledged;
  }

  const isSetupAvailable = Boolean(
    currentStatus &&
    currentStatus.store === "unavailable" &&
    currentStatus.setupControl === "available"
  );
  const setupAcknowledged = Boolean(setupAcknowledgement && setupAcknowledgement.checked);
  if (setupAcknowledgement) {
    setupAcknowledgement.disabled = isBusy || !isSetupAvailable;
  }
  if (initializeStoreButton) {
    initializeStoreButton.disabled = isBusy || !isSetupAvailable || !setupAcknowledged;
  }

  if (consents && consents.children) {
    for (const row of consents.children) {
      const grantBtn = row.querySelector ? row.querySelector(".grant-button") : null;
      if (grantBtn) {
        grantBtn.disabled = isBusy || !isReady || !acknowledged;
      }
      const revokeBtn = row.querySelector ? row.querySelector(".revoke-button") : null;
      if (revokeBtn) {
        revokeBtn.disabled = isBusy || !isReady;
      }
    }
  }
}

function renderConsents(items) {
  if (!consents) return;
  consents.replaceChildren();
  if (!Array.isArray(items)) return;

  const isReady = currentStatus && currentStatus.store === "ready" && Object.hasOwn(safeCollectionCopy, currentStatus.collection);
  const acknowledged = Boolean(consentAcknowledgement && consentAcknowledgement.checked);

  for (const item of items) {
    const labelText = allowedConsentLabels[`${item.adapter}:${item.tool}`];
    if (!labelText || !validConsent(item)) continue;

    const row = document.createElement("div");
    row.className = "consent-row";

    const label = document.createElement("span");
    label.className = "consent-label";
    label.textContent = labelText;

    const state = document.createElement("span");
    state.className = `state state-${item.state}`;
    state.textContent = item.state;

    const actions = document.createElement("div");
    actions.className = "consent-actions";

    if (item.adapterId === "desktop-foreground-app") {
      const readOnlyNote = document.createElement("span");
      readOnlyNote.className = "read-only-badge";
      readOnlyNote.textContent = "read-only";
      actions.append(readOnlyNote);
    } else if (item.adapterId === "browser-chrome" || item.adapterId === "browser-edge") {
      if (item.state === "disabled") {
        const grantBtn = document.createElement("button");
        grantBtn.type = "button";
        grantBtn.className = "grant-button";
        grantBtn.textContent = "Grant";
        grantBtn.disabled = isBusy || !isReady || !acknowledged;
        grantBtn.addEventListener("click", async () => {
          if (grantBtn.disabled || !consentAcknowledgement.checked) return;
          await runAction(async () => {
            if (consentFeedback) consentFeedback.textContent = `Granting consent for ${labelText}…`;
            const updated = await invoke("set_companion_consent", {
              request: {
                adapterId: item.adapterId,
                toolId: item.toolId,
                enabled: true,
                policyVersion: 1,
              },
            });
            renderDashboard(updated);
            if (consentFeedback) consentFeedback.textContent = `Consent granted for ${labelText}.`;
          }, consentFeedback);
        });
        actions.append(grantBtn);
      } else if (item.state === "enabled") {
        const revokeBtn = document.createElement("button");
        revokeBtn.type = "button";
        revokeBtn.className = "revoke-button";
        revokeBtn.textContent = "Revoke";
        revokeBtn.disabled = isBusy || !isReady;
        revokeBtn.addEventListener("click", async () => {
          if (revokeBtn.disabled) return;
          await runAction(async () => {
            if (consentFeedback) consentFeedback.textContent = `Revoking consent for ${labelText}…`;
            const updated = await invoke("set_companion_consent", {
              request: {
                adapterId: item.adapterId,
                toolId: item.toolId,
                enabled: false,
                policyVersion: 1,
              },
            });
            renderDashboard(updated);
            if (consentFeedback) consentFeedback.textContent = `Consent revoked for ${labelText}.`;
          }, consentFeedback);
        });
        actions.append(revokeBtn);
      }
    }

    row.append(label, state, actions);
    consents.append(row);
  }
}

function renderDashboard(status) {
  currentStatus = status;
  const [heading, detail] = safeCollectionCopy[status.collection] ?? safeCollectionCopy.disabled;
  if (collectionHeading) collectionHeading.textContent = status.store === "ready" && Object.hasOwn(safeCollectionCopy, status.collection) ? heading : "Collection status unavailable";
  if (collectionDetail) {
    collectionDetail.textContent =
      status.store === "ready" ? detail : (safeStoreCopy[status.store] ?? safeStoreCopy.error);
  }
  renderConsents(status.consents);

  if (hostState && status.nativeHost) {
    const chrome = safeHostCopy[status.nativeHost.chrome] ?? safeHostCopy.error;
    const edge = safeHostCopy[status.nativeHost.edge] ?? safeHostCopy.error;
    hostState.textContent = `Chrome: ${chrome}. Edge: ${edge}.`;
  }
  if (registerHostButton && status.nativeHost && status.nativeHost.registrationControl !== "available") {
    if (hostFeedback) hostFeedback.textContent = "Native-host registration is unavailable in this installation.";
  }

  if (isDataControlsReady(status)) {
    if (storedSpansElement) {
      storedSpansElement.textContent = String(status.dataControls.storedSpans);
    }
    if (retentionDaysInput) {
      retentionDaysInput.value = String(status.dataControls.retentionDays);
    }
  } else {
    if (storedSpansElement) storedSpansElement.textContent = "Unavailable";
    if (dataFeedback) dataFeedback.textContent = "Local data controls require the installed NEXUS helper, Node.js, and an available store.";
  }

  if (setupSection) {
    if (status && status.store === "ready") {
      setupSection.hidden = true;
      if (setupFeedback) setupFeedback.textContent = "";
    } else {
      setupSection.hidden = false;
      if (!status || status.store !== "unavailable" || status.setupControl !== "available") {
        if (setupFeedback) setupFeedback.textContent = "Local Companion setup is unavailable.";
      }
    }
  }

  updateControlStates();
}

async function runAction(actionFn, feedbackElement) {
  if (isBusy) return;
  isBusy = true;
  updateControlStates();
  try {
    await actionFn();
  } catch (error) {
    if (feedbackElement) feedbackElement.textContent = safeError(error);
    await refreshDashboard();
  } finally {
    isBusy = false;
    updateControlStates();
  }
}

async function refreshDashboard() {
  try {
    const status = await invoke("get_companion_dashboard");
    renderDashboard(status);
  } catch {
    currentStatus = {
      collection: "disabled",
      store: "error",
      setupControl: "unavailable",
      consents: [],
      nativeHost: { chrome: "error", edge: "error", registrationControl: "unavailable" },
    };
    if (collectionHeading) collectionHeading.textContent = "Collection status unavailable";
    if (collectionDetail) collectionDetail.textContent = safeStoreCopy.error;
    renderConsents([]);
    if (storedSpansElement) storedSpansElement.textContent = "Unavailable";
    if (dataFeedback) dataFeedback.textContent = "Local data status is unavailable.";
    if (setupSection) {
      setupSection.hidden = false;
      if (setupFeedback) setupFeedback.textContent = "Local Companion setup is unavailable.";
    }
    updateControlStates();
  }
}

if (setupAcknowledgement) {
  setupAcknowledgement.addEventListener("change", () => {
    updateControlStates();
  });
}

if (initializeStoreButton) {
  initializeStoreButton.addEventListener("click", async () => {
    const isSetupAvailable = Boolean(
      currentStatus &&
      currentStatus.store === "unavailable" &&
      currentStatus.setupControl === "available"
    );
    if (initializeStoreButton.disabled || !isSetupAvailable || !setupAcknowledgement || !setupAcknowledgement.checked) {
      return;
    }
    await runAction(async () => {
      if (setupAcknowledgement) {
        setupAcknowledgement.checked = false;
      }
      if (setupFeedback) setupFeedback.textContent = "Initializing local SQLite store…";
      const status = await invoke("initialize_companion_store", {
        request: { confirmed: true },
      });
      renderDashboard(status);
      if (setupFeedback) {
        setupFeedback.textContent = "Local store initialized. Collection remains off.";
      }
    }, setupFeedback);
  });
}

if (consentAcknowledgement) {
  consentAcknowledgement.addEventListener("change", () => {
    updateControlStates();
  });
}

if (clearAcknowledgement) {
  clearAcknowledgement.addEventListener("change", () => {
    updateControlStates();
  });
}

if (saveRetentionButton) {
  saveRetentionButton.addEventListener("click", async () => {
    if (saveRetentionButton.disabled || !isDataControlsReady(currentStatus)) return;
    const rawVal = retentionDaysInput ? retentionDaysInput.value.trim() : "";
    if (!/^\d+$/.test(rawVal)) {
      if (dataFeedback) dataFeedback.textContent = "Retention days must be an integer between 0 and 365.";
      return;
    }
    const days = Number(rawVal);
    if (!Number.isInteger(days) || days < 0 || days > 365) {
      if (dataFeedback) dataFeedback.textContent = "Retention days must be an integer between 0 and 365.";
      return;
    }
    await runAction(async () => {
      if (dataFeedback) dataFeedback.textContent = "Updating retention window…";
      const status = await invoke("set_companion_retention", {
        request: { days },
      });
      renderDashboard(status);
      if (dataFeedback) dataFeedback.textContent = `Retention updated to ${days} days.`;
    }, dataFeedback);
  });
}

if (pruneHistoryButton) {
  pruneHistoryButton.addEventListener("click", async () => {
    if (pruneHistoryButton.disabled || !isDataControlsReady(currentStatus)) return;
    await runAction(async () => {
      if (dataFeedback) dataFeedback.textContent = "Pruning expired local history…";
      const status = await invoke("prune_companion_history");
      renderDashboard(status);
      if (dataFeedback) dataFeedback.textContent = "Expired local history pruned.";
    }, dataFeedback);
  });
}

if (clearHistoryButton) {
  clearHistoryButton.addEventListener("click", async () => {
    if (clearHistoryButton.disabled || !isDataControlsReady(currentStatus) || !clearAcknowledgement || !clearAcknowledgement.checked) return;
    await runAction(async () => {
      if (dataFeedback) dataFeedback.textContent = "Clearing recorded local history…";
      const status = await invoke("clear_companion_history", {
        request: { confirmed: true },
      });
      if (clearAcknowledgement) {
        clearAcknowledgement.checked = false;
      }
      renderDashboard(status);
      if (dataFeedback) dataFeedback.textContent = "Recorded local history cleared.";
    }, dataFeedback);
  });
}

if (resumeButton) {
  resumeButton.addEventListener("click", async () => {
    if (resumeButton.disabled) return;
    await runAction(async () => {
      if (controlFeedback) controlFeedback.textContent = "Resuming local collection…";
      const status = await invoke("resume_companion_collection");
      renderDashboard(status);
      if (controlFeedback) controlFeedback.textContent = "Collection is resumed.";
    }, controlFeedback);
  });
}

if (pauseButton) {
  pauseButton.addEventListener("click", async () => {
    if (pauseButton.disabled) return;
    await runAction(async () => {
      if (controlFeedback) controlFeedback.textContent = "Pausing local collection…";
      const status = await invoke("pause_companion_collection");
      renderDashboard(status);
      if (controlFeedback) controlFeedback.textContent = "Collection is paused. Fixed consents remain granted.";
    }, controlFeedback);
  });
}

if (disableButton) {
  disableButton.addEventListener("click", async () => {
    if (disableButton.disabled) return;
    await runAction(async () => {
      if (controlFeedback) controlFeedback.textContent = "Disabling local collection…";
      const status = await invoke("disable_companion_collection");
      renderDashboard(status);
      if (controlFeedback) controlFeedback.textContent = "Collection is disabled and all fixed consents are revoked.";
    }, controlFeedback);
  });
}

if (nativeHostForm) {
  nativeHostForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    if (registerHostButton.disabled) return;
    await runAction(async () => {
      if (hostFeedback) hostFeedback.textContent = "Registering this browser’s local native host…";
      const status = await invoke("register_native_host", {
        request: {
          browser: document.querySelector("#browser").value,
          extensionId: document.querySelector("#extension-id").value,
          hostPath: document.querySelector("#host-path").value,
        },
      });
      const extInput = document.querySelector("#extension-id");
      const hostInput = document.querySelector("#host-path");
      if (extInput) extInput.value = "";
      if (hostInput) hostInput.value = "";
      renderDashboard(status);
      if (hostFeedback) {
        hostFeedback.textContent =
          "The browser-specific native-host manifest is registered. Collection remains unchanged.";
      }
    }, hostFeedback);
  });
}

refreshDashboard();
