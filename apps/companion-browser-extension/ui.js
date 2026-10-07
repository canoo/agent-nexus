import { TOOL_DEFINITIONS, sanitizeConsents } from "./lib/policy.js";
import { changeToolConsent } from "./lib/consent.js";

const CONSENTS_KEY = "toolConsents";

async function readConsents() {
  const stored = await chrome.storage.local.get(CONSENTS_KEY);
  return sanitizeConsents(stored[CONSENTS_KEY]);
}

async function writeConsent(toolId, enabled) {
  const result = await changeToolConsent({
    toolId,
    enabled,
    rawConsents: await readConsents(),
    requestOrigins: (origins) => chrome.permissions.request({ origins }),
    removeOrigins: (origins) => chrome.permissions.remove({ origins }),
  });
  if (result.applied) await chrome.storage.local.set({ [CONSENTS_KEY]: result.consents });
  return result;
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
      const result = await writeConsent(tool.id, requestedEnabled).catch(() => ({
        applied: false,
        consents,
      }));
      checkbox.checked = result.consents[tool.id];
      if (!result.applied) {
        const status = document.createElement("p");
        status.className = "permission-status";
        status.setAttribute("role", "status");
        status.textContent = requestedEnabled
          ? `Could not enable ${tool.label}; browser permission was not granted.`
          : `Could not disable ${tool.label}; its browser permission is still active.`;
        row.append(status);
      }
      checkbox.disabled = false;
    });
    row.append(checkbox, text);
    container.append(row);
  }
}
