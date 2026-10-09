/**
 * This registry is intentionally the only browser-origin knowledge in the
 * extension.  It maps an origin to a fixed NEXUS identifier and never returns
 * a URL, title, provider label, or arbitrary page metadata.
 */
export const TOOL_DEFINITIONS = Object.freeze([
  Object.freeze({
    id: "chatgpt",
    label: "ChatGPT",
    origins: Object.freeze(["https://chatgpt.com/*", "https://chat.openai.com/*"]),
  }),
  Object.freeze({ id: "claude", label: "Claude", origins: Object.freeze(["https://claude.ai/*"]) }),
  Object.freeze({ id: "gemini", label: "Gemini", origins: Object.freeze(["https://gemini.google.com/*"]) }),
  Object.freeze({ id: "copilot", label: "Microsoft Copilot", origins: Object.freeze(["https://copilot.microsoft.com/*"]) }),
  Object.freeze({ id: "perplexity", label: "Perplexity", origins: Object.freeze(["https://www.perplexity.ai/*"]) }),
]);

export const TOOL_IDS = Object.freeze(TOOL_DEFINITIONS.map(({ id }) => id));

const TOOL_IDS_SET = new Set(TOOL_IDS);
const ORIGIN_TO_TOOL_ID = new Map(
  TOOL_DEFINITIONS.flatMap(({ id, origins }) => origins.map((pattern) => [new URL(pattern).origin, id])),
);

export function originForUrl(url) {
  if (typeof url !== "string") return null;
  try {
    const parsed = new URL(url);
    return parsed.protocol === "https:" ? parsed.origin : null;
  } catch {
    return null;
  }
}

export function toolIdForOrigin(origin) {
  return typeof origin === "string" ? ORIGIN_TO_TOOL_ID.get(origin) ?? null : null;
}

export function sanitizeConsents(rawConsents) {
  const consents = {};
  for (const toolId of TOOL_IDS) consents[toolId] = rawConsents?.[toolId] === true;
  return Object.freeze(consents);
}

export function selectedToolId(origin, rawConsents) {
  const toolId = toolIdForOrigin(origin);
  return toolId && sanitizeConsents(rawConsents)[toolId] ? toolId : null;
}

export function definitionForTool(toolId) {
  return TOOL_DEFINITIONS.find((tool) => tool.id === toolId) ?? null;
}

export function isToolId(toolId) {
  return TOOL_IDS_SET.has(toolId);
}
