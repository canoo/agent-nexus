import assert from "node:assert/strict";
import test from "node:test";
import { dispatchToNativeHost, sanitizeActiveSpans, transitionSelectedTab } from "../lib/activity.js";
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
