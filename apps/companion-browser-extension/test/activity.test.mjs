import assert from "node:assert/strict";
import test from "node:test";
import {
  discardUnconsentedSpans,
  dispatchToNativeHost,
  isFocusedSelectedTab,
  sanitizeActiveSpans,
  transitionAllSpansInactive,
  transitionSelectedTab,
} from "../lib/activity.js";
import { changeToolConsent, reconcileConsentsAfterPermissionRemoval } from "../lib/consent.js";
import { originForUrl, selectedToolId, toolIdForOrigin } from "../lib/policy.js";

const now = "2026-10-02T18:00:00Z";
const later = "2026-10-02T18:04:12Z";
const enabledChatGpt = { chatgpt: true };

test("only documented origins map to fixed tool IDs", () => {
  assert.equal(toolIdForOrigin("https://chatgpt.com"), "chatgpt");
  assert.equal(toolIdForOrigin("https://evil.example"), null);
  assert.equal(originForUrl("https://chatgpt.com/c/private?account=hidden"), "https://chatgpt.com");
  assert.equal(originForUrl("file:///private/prompt.txt"), null);
});

test("disabled and unmatched selected surfaces produce no activity event", () => {
  const disabled = transitionSelectedTab({
    activeSpans: {}, windowId: 1, origin: "https://chatgpt.com", consents: {}, now,
    browserFamily: "chrome", platform: "linux",
  });
  assert.deepEqual(disabled.activeSpans, {});
  assert.equal(disabled.event, null);

  const unmatched = transitionSelectedTab({
    activeSpans: {}, windowId: 1, origin: "https://not-an-ai-tool.example", consents: enabledChatGpt, now,
    browserFamily: "chrome", platform: "linux",
  });
  assert.deepEqual(unmatched.activeSpans, {});
  assert.equal(unmatched.event, null);
  assert.equal(selectedToolId("https://chatgpt.com", {}), null);
});

test("an activation cannot begin activity until its browser window is focused", () => {
  assert.equal(isFocusedSelectedTab({ tabActive: true, windowFocused: true }), true);
  assert.equal(isFocusedSelectedTab({ tabActive: true, windowFocused: false }), false);
  assert.equal(isFocusedSelectedTab({ tabActive: false, windowFocused: true }), false);
});

test("a selected enabled tool emits only the fixed envelope when it becomes inactive", () => {
  const selected = transitionSelectedTab({
    activeSpans: {}, windowId: 1, origin: "https://chatgpt.com", consents: enabledChatGpt, now,
    browserFamily: "chrome", platform: "linux",
  });
  assert.equal(selected.event, null);
  const closed = transitionSelectedTab({
    activeSpans: selected.activeSpans, windowId: 1, origin: "https://unrelated.example", consents: enabledChatGpt, now: later,
    browserFamily: "chrome", platform: "linux",
  });
  assert.deepEqual(closed.event, {
    tool_id: "chatgpt", surface: "browser", started_at: "2026-10-02T18:00:00.000Z", ended_at: "2026-10-02T18:04:12.000Z",
    detector: "selected-browser-tab", confidence: "surface-active", browser_family: "chrome", platform: "linux",
    schema_version: 1, consent_policy_version: 1,
  });
  assert.deepEqual(Object.keys(closed.event).sort(), [
    "browser_family", "confidence", "consent_policy_version", "detector", "ended_at", "platform",
    "schema_version", "started_at", "surface", "tool_id",
  ]);
});

test("raw URLs and unknown storage data cannot enter an emitted envelope", () => {
  const rawUrl = "https://chatgpt.com/c/private?prompt=do-not-store&account=secret";
  const contaminatedState = {
    "1": { tool_id: "chatgpt", started_at: now, url: rawUrl, title: "private", prompt: "secret" },
    "2": { tool_id: "unknown-tool", started_at: now, response: "secret" },
  };
  const cleaned = sanitizeActiveSpans(contaminatedState);
  assert.deepEqual(cleaned, { "1": { tool_id: "chatgpt", started_at: "2026-10-02T18:00:00.000Z" } });
  const result = transitionSelectedTab({
    activeSpans: cleaned, windowId: 1, origin: originForUrl(rawUrl), consents: {}, now: later,
    browserFamily: "chrome", platform: "linux", arbitrary_metadata: rawUrl,
  });
  const serialized = JSON.stringify(result.event);
  for (const forbidden of ["url", "title", "prompt", "response", "account", "metadata", rawUrl]) {
    assert.equal(serialized.includes(forbidden), false, `${forbidden} must not be emitted`);
  }
});

test("revoking consent discards an in-progress span", () => {
  const selected = transitionSelectedTab({
    activeSpans: {}, windowId: 1, origin: "https://chatgpt.com", consents: enabledChatGpt, now,
    browserFamily: "chrome", platform: "linux",
  });
  const revoked = transitionSelectedTab({
    activeSpans: selected.activeSpans, windowId: 1, origin: "https://unrelated.example", consents: {}, now: later,
    browserFamily: "chrome", platform: "linux",
  });
  assert.equal(revoked.event, null);
  assert.deepEqual(revoked.activeSpans, {});
});

test("browser app focus loss ends every active span with fixed envelopes", () => {
  const activeSpans = {
    "1": { tool_id: "chatgpt", started_at: now },
    "3": { tool_id: "claude", started_at: now },
  };
  const ended = transitionAllSpansInactive({
    activeSpans,
    consents: { chatgpt: true, claude: true },
    now: later,
    browserFamily: "edge",
    platform: "macos",
  });
  assert.deepEqual(ended.activeSpans, {});
  assert.equal(ended.events.length, 2);
  assert.deepEqual(ended.events.map((event) => event.tool_id).sort(), ["chatgpt", "claude"]);
  assert.ok(ended.events.every((event) => event.browser_family === "edge" && event.platform === "macos"));
});

test("permission removal clears stale consent and discards only its active span", () => {
  const consents = { chatgpt: true, claude: true };
  const next = reconcileConsentsAfterPermissionRemoval(consents, ["https://chatgpt.com/*"]);
  assert.equal(next.chatgpt, false);
  assert.equal(next.claude, true);
  assert.deepEqual(discardUnconsentedSpans({
    "1": { tool_id: "chatgpt", started_at: now },
    "2": { tool_id: "claude", started_at: now },
  }, next), {
    "2": { tool_id: "claude", started_at: "2026-10-02T18:00:00.000Z" },
  });
});

test("consent changes only persist after the browser allows the matching permission change", async () => {
  let requestedOrigins;
  const allowed = await changeToolConsent({
    toolId: "chatgpt",
    enabled: true,
    rawConsents: {},
    requestOrigins: async (origins) => { requestedOrigins = origins; return true; },
    removeOrigins: async () => { throw new Error("must not remove while enabling"); },
  });
  assert.equal(allowed.applied, true);
  assert.equal(allowed.consents.chatgpt, true);
  assert.deepEqual(requestedOrigins, ["https://chatgpt.com/*", "https://chat.openai.com/*"]);

  const denied = await changeToolConsent({
    toolId: "claude",
    enabled: true,
    rawConsents: {},
    requestOrigins: async () => false,
    removeOrigins: async () => { throw new Error("must not remove while enabling"); },
  });
  assert.equal(denied.applied, false);
  assert.equal(denied.consents.claude, false);

  const removalFailed = await changeToolConsent({
    toolId: "chatgpt",
    enabled: false,
    rawConsents: { chatgpt: true },
    requestOrigins: async () => { throw new Error("must not request while disabling"); },
    removeOrigins: async () => false,
  });
  assert.equal(removalFailed.applied, false);
  assert.equal(removalFailed.consents.chatgpt, true);
});

test("native dispatch fails closed when unavailable and never uses a fallback", async () => {
  let calls = 0;
  const fixedEvent = {
    tool_id: "chatgpt", surface: "browser", started_at: now, ended_at: later,
    detector: "selected-browser-tab", confidence: "surface-active", browser_family: "chrome",
    platform: "linux", schema_version: 1, consent_policy_version: 1,
  };
  const sent = await dispatchToNativeHost(fixedEvent, async () => { calls += 1; throw new Error("host unavailable"); });
  assert.equal(sent, false);
  assert.equal(calls, 1);
  assert.equal(await dispatchToNativeHost({ tool_id: "chatgpt", prompt: "must not leave" }, async () => {
    throw new Error("must not be called");
  }), false);
  assert.equal(await dispatchToNativeHost(null, undefined), false);
});

test("native dispatch accepts only the fixed positive acknowledgement", async () => {
  const event = {
    tool_id: "chatgpt", surface: "browser", started_at: now, ended_at: later,
    detector: "selected-browser-tab", confidence: "surface-active", browser_family: "chrome",
    platform: "linux", schema_version: 1, consent_policy_version: 1,
  };
  assert.equal(await dispatchToNativeHost(event, async () => ({schema_version: 1, ok: true})), true);
  for (const reply of [undefined, null, [], "private reply", {schema_version: 1, ok: false}, {schema_version: 2, ok: true}, {schema_version: 1, ok: "true"}, {schema_version: 1, ok: true, url: "https://private.example"}]) {
    assert.equal(await dispatchToNativeHost(event, async () => reply), false);
  }
});
