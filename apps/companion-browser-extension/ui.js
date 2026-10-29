import { TOOL_DEFINITIONS, definitionForTool, sanitizeConsents } from "./lib/policy.js";
import { changeToolConsent } from "./lib/consent.js";

const CONSENTS_KEY = "toolConsents";

async function readConsents() {
  const stored = await chrome.storage.local.get(CONSENTS_KEY);
  return sanitizeConsents(stored[CONSENTS_KEY]);
}

function isValidWorkerResponse(response) {
  if (!response || typeof response !== "object" || Array.isArray(response)) {
    return false;
  }
  const keys = Object.keys(response);
  if (keys.length !== 2 || !keys.includes("applied") || !keys.includes("consents")) {
    return false;
  }
  if (typeof response.applied !== "boolean") {
    return false;
  }
  const consents = response.consents;
  if (!consents || typeof consents !== "object" || Array.isArray(consents)) {
    return false;
  }
  const consentKeys = Object.keys(consents);
  if (consentKeys.length !== TOOL_DEFINITIONS.length) {
    return false;
  }
  for (const tool of TOOL_DEFINITIONS) {
    if (!Object.prototype.hasOwnProperty.call(consents, tool.id) || typeof consents[tool.id] !== "boolean") {
      return false;
    }
  }
  return true;
}

export async function writeConsent(toolId, enabled) {
  if (typeof enabled !== "boolean" || !definitionForTool(toolId)) return { applied: false, consents: await readConsents() };
  try {
    // Send directly from the checkbox gesture. The worker starts and owns the
    // permission request before any await, so popup destruction cannot lose
    // the grant-result continuation.
    if (enabled) {
      const response = await chrome.runtime.sendMessage({ kind: "request-tool-grant", toolId });
      if (isValidWorkerResponse(response)) return response;
    } else {
      const result = await changeToolConsent({
        toolId, enabled, rawConsents: {},
        requestOrigins: (origins) => chrome.permissions.request({ origins }),
        removeOrigins: (origins) => chrome.permissions.remove({ origins }),
      });
      if (result.applied) {
        const response = await chrome.runtime.sendMessage({ kind: "set-tool-consent", toolId, enabled: false });
        if (isValidWorkerResponse(response)) return response;
      }
    }
  } catch {
    // Errors and closed message channels return only a fresh safe snapshot.
  }
  return { applied: false, consents: await readConsents() };
}

export async function renderToolToggles(container) {
  const consents = await readConsents();
  for (const tool of TOOL_DEFINITIONS) {
    const row = document.createElement("label");
    row.className = "tool-row";
    const checkbox = document.createElement("input");
    checkbox.type = "checkbox";
    checkbox.checked = consents[tool.id];
    checkbox.setAttribute("aria-label", `Enable ${tool.label} activity signals`);
    const text = document.createElement("span");
    text.textContent = tool.label;
    checkbox.addEventListener("change", async () => {
      checkbox.disabled = true;
      const requestedEnabled = checkbox.checked;
      const result = await writeConsent(tool.id, requestedEnabled).catch(async () => ({
        applied: false,
        consents: await readConsents().catch(() => sanitizeConsents({})),
      }));
      checkbox.checked = result.consents[tool.id];
      if (!result.applied) {
        const status = document.createElement("p");
        status.className = "permission-status";
        status.setAttribute("role", "status");
        status.textContent = requestedEnabled
          ? `Could not enable ${tool.label}; browser permission was not granted or saving failed.`
          : `Could not disable ${tool.label}; browser permission could not be removed or saving failed.`;
        row.append(status);
      }
      checkbox.disabled = false;
    });
    row.append(checkbox, text);
    container.append(row);
  }
}
