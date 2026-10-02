import { isToolId, selectedToolId } from "./policy.js";

const RFC3339_UTC = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,3})?Z$/;

function normalizedTimestamp(value) {
  if (typeof value !== "string" || !RFC3339_UTC.test(value)) return null;
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? null : parsed.toISOString();
}

function normalizedWindowId(value) {
  return Number.isSafeInteger(value) && value >= 0 ? String(value) : null;
}

function normalizedActiveSpan(value) {
  if (!value || typeof value !== "object" || !isToolId(value.tool_id)) return null;
  const startedAt = normalizedTimestamp(value.started_at);
  return startedAt ? Object.freeze({ tool_id: value.tool_id, started_at: startedAt }) : null;
}

/** Only fixed fields may survive a service-worker restart in storage.session. */
export function sanitizeActiveSpans(rawSpans) {
  const spans = {};
  if (!rawSpans || typeof rawSpans !== "object" || Array.isArray(rawSpans)) return spans;
  for (const [windowId, value] of Object.entries(rawSpans)) {
    if (!/^\d+$/.test(windowId)) continue;
    const span = normalizedActiveSpan(value);
    if (span) spans[windowId] = span;
  }
  return spans;
}

function envelopeFor(span, endedAt, browserFamily, platform) {
  const startedAt = normalizedTimestamp(span.started_at);
  const normalizedEndedAt = normalizedTimestamp(endedAt);
  if (!startedAt || !normalizedEndedAt || Date.parse(normalizedEndedAt) <= Date.parse(startedAt)) return null;
  if (browserFamily !== "chrome" && browserFamily !== "edge") return null;
  if (platform !== undefined && platform !== "linux" && platform !== "macos") return null;
  return Object.freeze({
    tool_id: span.tool_id,
    surface: "browser",
    started_at: startedAt,
    ended_at: normalizedEndedAt,
    detector: "selected-browser-tab",
    confidence: "surface-active",
    browser_family: browserFamily,
    ...(platform === undefined ? {} : { platform }),
    schema_version: 1,
    consent_policy_version: 1,
  });
}

function isFixedEnvelope(event) {
  if (!event || typeof event !== "object" || Array.isArray(event)) return false;
  const allowedKeys = new Set([
    "tool_id", "surface", "started_at", "ended_at", "detector", "confidence",
    "browser_family", "platform", "schema_version", "consent_policy_version",
  ]);
  if (Object.keys(event).some((key) => !allowedKeys.has(key))) return false;
  return isToolId(event.tool_id)
    && event.surface === "browser"
    && normalizedTimestamp(event.started_at) !== null
    && normalizedTimestamp(event.ended_at) !== null
    && Date.parse(event.ended_at) > Date.parse(event.started_at)
    && event.detector === "selected-browser-tab"
    && event.confidence === "surface-active"
    && (event.browser_family === "chrome" || event.browser_family === "edge")
    && (event.platform === undefined || event.platform === "linux" || event.platform === "macos")
    && event.schema_version === 1
    && event.consent_policy_version === 1;
}

/**
 * Maps only a local origin to an allowlisted tool and returns either a fixed
 * envelope or no event. `origin` is deliberately not copied into state or an
 * envelope, so raw URLs can never cross this module's boundary.
 */
export function transitionSelectedTab({ activeSpans, windowId, origin, consents, now, browserFamily, platform }) {
  const key = normalizedWindowId(windowId);
  const endedAt = normalizedTimestamp(now);
  const next = sanitizeActiveSpans(activeSpans);
  if (!key || !endedAt) return Object.freeze({ activeSpans: next, event: null });

  const previous = next[key] ?? null;
  const selectedTool = selectedToolId(origin, consents);
  if (previous?.tool_id === selectedTool) return Object.freeze({ activeSpans: next, event: null });

  // A consent change applies immediately, including to an in-progress span.
  const event = previous && consents?.[previous.tool_id] === true
    ? envelopeFor(previous, endedAt, browserFamily, platform)
    : null;
  if (selectedTool) next[key] = Object.freeze({ tool_id: selectedTool, started_at: endedAt });
  else delete next[key];
  return Object.freeze({ activeSpans: next, event });
}

/**
 * Native messaging is the only dispatch route. A missing or failed host
 * produces no fallback, retry queue, console output, or payload log.
 */
export async function dispatchToNativeHost(event, sendNativeMessage) {
  if (!isFixedEnvelope(event) || typeof sendNativeMessage !== "function") return false;
  try {
    await sendNativeMessage(event);
    return true;
  } catch {
    return false;
  }
}
