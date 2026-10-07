import { createObservabilityStore, normalizeToolActivityEvent } from "../../../tools/mcp/lib/observability-store.mjs";
import { NativeMessageDecoder } from "./native-messaging.mjs";

const BROWSER_FAMILIES = new Set(["chrome", "edge"]);
const SAFE_STORE_CODES = new Map([
  ["companion_collection_disabled", "companion_collection_disabled"],
  ["companion_tool_consent_missing", "companion_tool_consent_missing"],
  ["companion_retention_disabled", "companion_retention_disabled"],
  ["companion_activity_expired", "companion_activity_expired"],
]);

function safeFailure(code) {
  return Object.freeze({ ok: false, code });
}

function normalizedBrowserFamily(browserFamily) {
  if (!BROWSER_FAMILIES.has(browserFamily)) {
    throw new TypeError("browserFamily must be chrome or edge");
  }
  return browserFamily;
}

/**
 * The browser registration selects the executable wrapper, which fixes this
 * browser family before stdin is read. The frame cannot choose another adapter
 * or override the family selected by that wrapper.
 */
export function validateHostActivityEnvelope(rawMessage, { browserFamily }) {
  const fixedBrowserFamily = normalizedBrowserFamily(browserFamily);
  if (!rawMessage || typeof rawMessage !== "object" || Array.isArray(rawMessage)) {
    return safeFailure("invalid_activity_envelope");
  }
  if (rawMessage.browser_family !== fixedBrowserFamily) {
    return safeFailure("browser_family_mismatch");
  }
  try {
    normalizeToolActivityEvent(rawMessage);
  } catch {
    // normalizeToolActivityEvent rejects unknown/sensitive fields before any
    // store call. Never place the rejected message or its error text in output.
    return safeFailure("invalid_activity_envelope");
  }
  return Object.freeze({ ok: true });
}

/**
 * One browser-specific host process. It emits no protocol response and writes
 * no diagnostics: extension events are one-way and rejected content is never
 * reflected back to a browser, terminal, or log.
 */
export class CompanionNativeMessagingHost {
  constructor({ browserFamily, store = createObservabilityStore(), decoder } = {}) {
    this.browserFamily = normalizedBrowserFamily(browserFamily);
    if (!store || typeof store.recordToolActivity !== "function") {
      throw new TypeError("store must expose recordToolActivity");
    }
    this.store = store;
    this.decoder = decoder ?? new NativeMessageDecoder();
  }

  handleMessage(message) {
    const validation = validateHostActivityEnvelope(message, { browserFamily: this.browserFamily });
    if (!validation.ok) return validation;

    const result = this.store.recordToolActivity(message);
    if (result?.sqlite?.ok === true) return Object.freeze({ ok: true });
    return safeFailure(SAFE_STORE_CODES.get(result?.sqlite?.error) ?? "activity_store_unavailable");
  }

  ingest(chunk) {
    const frames = this.decoder.push(chunk);
    return Object.freeze(frames.map((frame) => (
      frame.ok ? this.handleMessage(frame.message) : safeFailure(frame.code)
    )));
  }
}

/** Cleanup is silent and independent of collection/consent state. */
export function startCompanionRetentionMaintenance(store, {
  schedule = setInterval, cancel = clearInterval,
} = {}) {
  if (typeof store?.pruneToolActivity !== "function") return () => {};
  const prune = () => { try { store.pruneToolActivity(); } catch {} };
  prune();
  const timer = schedule(prune, 30 * 60 * 1000);
  timer.unref?.();
  return () => cancel(timer);
}

/** Starts a silent stdio native-messaging process for one registered browser. */
export function runNativeMessagingHost({ browserFamily, store } = {}) {
  const host = new CompanionNativeMessagingHost({ browserFamily, store });
  const stopMaintenance = startCompanionRetentionMaintenance(host.store);
  process.stdin.once("end", stopMaintenance);
  process.stdin.once("close", stopMaintenance);
  process.stdin.on("data", (chunk) => {
    host.ingest(chunk);
    if (host.decoder.closed) process.stdin.pause();
  });
  return host;
}
