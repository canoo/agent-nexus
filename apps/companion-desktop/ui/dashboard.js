const { invoke } = window.__TAURI__.core;

const collectionHeading = document.querySelector("#collection-heading");
const collectionDetail = document.querySelector("#collection-detail");
const consents = document.querySelector("#consents");
const hostState = document.querySelector("#host-state");
const disableButton = document.querySelector("#disable-collection");
const disableFeedback = document.querySelector("#disable-feedback");
const nativeHostForm = document.querySelector("#native-host-form");
const registerHostButton = document.querySelector("#register-host");
const hostFeedback = document.querySelector("#host-feedback");

const safeCollectionCopy = Object.freeze({
  enabled: ["Collection is enabled", "Only fixed, consented activity envelopes can be accepted by the shared local store."],
  disabled: ["Collection is disabled", "No new Companion activity can be recorded."],
});
const safeStoreCopy = Object.freeze({
  unavailable: "The shared local observability store is not available yet. Nothing is being collected.",
  error: "The local observability status is unavailable. Nothing is being enabled.",
});
const safeHostCopy = Object.freeze({
  registered: "registered", unregistered: "unregistered", unavailable: "unavailable", error: "needs attention",
});
const allowedConsentLabels = Object.freeze({
  "Chrome browser:ChatGPT": "Chrome browser: ChatGPT",
  "Chrome browser:Claude": "Chrome browser: Claude",
  "Chrome browser:Gemini": "Chrome browser: Gemini",
  "Chrome browser:GitHub Copilot": "Chrome browser: GitHub Copilot",
  "Chrome browser:Perplexity": "Chrome browser: Perplexity",
  "Edge browser:ChatGPT": "Edge browser: ChatGPT",
  "Edge browser:Claude": "Edge browser: Claude",
  "Edge browser:Gemini": "Edge browser: Gemini",
  "Edge browser:GitHub Copilot": "Edge browser: GitHub Copilot",
  "Edge browser:Perplexity": "Edge browser: Perplexity",
  "Desktop foreground adapter:ChatGPT": "Desktop foreground adapter: ChatGPT",
  "Desktop foreground adapter:Claude": "Desktop foreground adapter: Claude",
  "Desktop foreground adapter:Gemini": "Desktop foreground adapter: Gemini",
  "Desktop foreground adapter:GitHub Copilot": "Desktop foreground adapter: GitHub Copilot",
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
  return safeActionErrors.has(error) ? error : "The requested local action could not be completed.";
}

function renderConsents(items) {
  consents.replaceChildren();
  for (const item of items) {
    const labelText = allowedConsentLabels[`${item.adapter}:${item.tool}`];
    if (!labelText || !allowedConsentStates.has(item.state)) continue;
    const row = document.createElement("p");
    row.className = "consent-row";
    const label = document.createElement("span");
    label.textContent = labelText;
    const state = document.createElement("span");
    state.className = `state state-${item.state}`;
    state.textContent = item.state;
    row.append(label, state);
    consents.append(row);
  }
}

function renderDashboard(status) {
  const [heading, detail] = safeCollectionCopy[status.collection] ?? safeCollectionCopy.disabled;
  collectionHeading.textContent = heading;
  collectionDetail.textContent = status.store === "ready" ? detail : safeStoreCopy[status.store];
  renderConsents(status.consents);
  const chrome = safeHostCopy[status.nativeHost.chrome] ?? safeHostCopy.error;
  const edge = safeHostCopy[status.nativeHost.edge] ?? safeHostCopy.error;
  hostState.textContent = `Chrome: ${chrome}. Edge: ${edge}.`;
  registerHostButton.disabled = status.nativeHost.registrationControl !== "available";
  if (registerHostButton.disabled) hostFeedback.textContent = "Native-host registration is unavailable in this installation.";
}

async function refreshDashboard() {
  try { renderDashboard(await invoke("get_companion_dashboard")); }
  catch { collectionHeading.textContent = "Collection is disabled"; collectionDetail.textContent = safeStoreCopy.error; }
}

disableButton.addEventListener("click", async () => {
  disableButton.disabled = true;
  disableFeedback.textContent = "Disabling local collection…";
  try {
    renderDashboard(await invoke("disable_companion_collection"));
    disableFeedback.textContent = "Collection is disabled and all fixed consents are revoked.";
  } catch (error) { disableFeedback.textContent = safeError(error); }
  finally { disableButton.disabled = false; }
});

nativeHostForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  registerHostButton.disabled = true;
  hostFeedback.textContent = "Registering this browser’s local native host…";
  try {
    const status = await invoke("register_native_host", { request: {
      browser: document.querySelector("#browser").value,
      extensionId: document.querySelector("#extension-id").value,
      hostPath: document.querySelector("#host-path").value,
    }});
    document.querySelector("#extension-id").value = "";
    document.querySelector("#host-path").value = "";
    renderDashboard(status);
    hostFeedback.textContent = "The browser-specific native-host manifest is registered. Collection remains unchanged.";
  } catch (error) { hostFeedback.textContent = safeError(error); }
  finally { if (hostFeedback.textContent !== "Native-host registration is unavailable in this installation.") registerHostButton.disabled = false; }
});

refreshDashboard();
