import { TOOL_DEFINITIONS, definitionForTool, sanitizeConsents } from "./lib/policy.js";

const CONSENTS_KEY = "toolConsents";

async function readConsents() {
  const stored = await chrome.storage.local.get(CONSENTS_KEY);
  return sanitizeConsents(stored[CONSENTS_KEY]);
}

async function writeConsent(toolId, enabled) {
  const tool = definitionForTool(toolId);
  if (!tool) return false;
  const permitted = enabled
    ? await chrome.permissions.request({ origins: [...tool.origins] })
    : await chrome.permissions.remove({ origins: [...tool.origins] });
  if (enabled && !permitted) return false;

  const next = { ...(await readConsents()), [toolId]: enabled };
  await chrome.storage.local.set({ [CONSENTS_KEY]: next });
  return true;
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
      const applied = await writeConsent(tool.id, checkbox.checked).catch(() => false);
      if (!applied) checkbox.checked = false;
      checkbox.disabled = false;
    });
    row.append(checkbox, text);
    container.append(row);
  }
}
