import { definitionForTool, sanitizeConsents } from "./policy.js";

/**
 * Applies one user-requested consent change without permitting a browser
 * permission result and local consent state to drift apart.
 */
export async function changeToolConsent({ toolId, enabled, rawConsents, requestOrigins, removeOrigins }) {
  const current = sanitizeConsents(rawConsents);
  const tool = definitionForTool(toolId);
  if (!tool || typeof requestOrigins !== "function" || typeof removeOrigins !== "function") {
    return Object.freeze({ applied: false, consents: current });
  }

  const permissionChanged = enabled
    ? await requestOrigins([...tool.origins])
    : await removeOrigins([...tool.origins]);
  if (permissionChanged !== true) return Object.freeze({ applied: false, consents: current });

  return Object.freeze({
    applied: true,
    consents: Object.freeze({ ...current, [toolId]: enabled }),
  });
}

/**
 * Browser permission changes can occur outside NEXUS's toggle UI. Removing
 * any origin required by an enabled tool invalidates that tool's local consent.
 */
export function reconcileConsentsAfterPermissionRemoval(rawConsents, removedOrigins) {
  const current = sanitizeConsents(rawConsents);
  const removed = new Set(Array.isArray(removedOrigins) ? removedOrigins : []);
  const next = { ...current };

  for (const toolId of Object.keys(current)) {
    const tool = definitionForTool(toolId);
    if (current[toolId] && tool?.origins.some((origin) => removed.has(origin))) next[toolId] = false;
  }
  return Object.freeze(next);
}

export function consentsEqual(left, right) {
  const normalizedLeft = sanitizeConsents(left);
  const normalizedRight = sanitizeConsents(right);
  return Object.keys(normalizedLeft).every((toolId) => normalizedLeft[toolId] === normalizedRight[toolId]);
}
