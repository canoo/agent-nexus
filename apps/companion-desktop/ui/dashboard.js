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
  "Use a supported browser, a published Chrome-format extension ID, and an existing absolute host path.",
]);

function safeError(error) {
  const message = typeof error === "string" ? error : (error && error.message) ? error.message : "";
  return safeActionErrors.has(message) ? message : "The requested local action could not be completed.";
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
  const isReady = currentStatus && currentStatus.store === "ready" && Object.hasOwn(safeCollectionCopy, currentStatus.collection);
  const isEnabled = currentStatus && currentStatus.collection === "enabled";
  const hasGrant = hasActiveBrowserGrant(currentStatus);
  const acknowledged = Boolean(consentAcknowledgement && consentAcknowledgement.checked);

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
      consents: [],
      nativeHost: { chrome: "error", edge: "error", registrationControl: "unavailable" },
    };
    if (collectionHeading) collectionHeading.textContent = "Collection status unavailable";
    if (collectionDetail) collectionDetail.textContent = safeStoreCopy.error;
    renderConsents([]);
    updateControlStates();
  }
}

if (consentAcknowledgement) {
  consentAcknowledgement.addEventListener("change", () => {
    updateControlStates();
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
