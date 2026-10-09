import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { writeConsent } from "../ui.js";
import { sanitizeConsents } from "../lib/policy.js";

const chromeDescriptor = Object.getOwnPropertyDescriptor(globalThis, "chrome");
afterEach(() => {
  if (chromeDescriptor) Object.defineProperty(globalThis, "chrome", chromeDescriptor);
  else delete globalThis.chrome;
});
function fixture() {
  const state = { consents: sanitizeConsents({ chatgpt: false, claude: true }) };
  const calls = [];
  const chrome = {
    storage: { local: {
      async get(key) { calls.push("read"); return { [key]: state.consents }; },
      async set() { assert.fail("UI must not write shared consent storage"); },
    } },
    permissions: {
      async request(request) { calls.push({ permission: "request", ...request }); return true; },
      async remove(request) { calls.push({ permission: "remove", ...request }); return true; },
    },
    runtime: { async sendMessage(message) {
      calls.push(message);
      state.consents = sanitizeConsents({ ...state.consents, [message.toolId]: message.enabled });
      return { applied: true, consents: state.consents };
    } },
  };
  Object.defineProperty(globalThis, "chrome", { configurable: true, value: chrome });
  return { chrome, state, calls };
}

test("UI requests permission immediately and submits only the one-tool mutation", async () => {
  const f = fixture();
  const pending = writeConsent("chatgpt", true);
  assert.deepEqual(f.calls, [{ permission: "request", origins: ["https://chatgpt.com/*", "https://chat.openai.com/*"] }]);
  const result = await pending;
  assert.equal(result.applied, true);
  assert.equal(result.consents.chatgpt, true);
  assert.equal(result.consents.claude, true);
  assert.deepEqual(f.calls[1], { kind: "set-tool-consent", toolId: "chatgpt", enabled: true });
  assert.equal(f.calls.length, 2);
  await writeConsent("claude", false);
  assert.deepEqual(f.calls[2], { permission: "remove", origins: ["https://claude.ai/*"] });
});

test("permission refusal reads fresh state and never sends a worker mutation", async () => {
  const f = fixture();
  f.chrome.permissions.request = async () => {
    f.state.consents = sanitizeConsents({ gemini: true });
    return false;
  };
  f.chrome.runtime.sendMessage = async () => assert.fail("no mutation after denied permission");
  assert.deepEqual(await writeConsent("chatgpt", true), { applied: false, consents: f.state.consents });
  assert.deepEqual(f.calls, ["read"]);
});

test("malformed worker responses and errors return fresh state without fallback writes", async () => {
  const f = fixture();
  const valid = { applied: true, consents: sanitizeConsents({ chatgpt: true }) };
  const bad = [null, [], {}, { ...valid, applied: 1 }, { ...valid, error: "private" },
    { ...valid, consents: [] }, { ...valid, consents: { chatgpt: true } },
    { ...valid, consents: { ...valid.consents, unknown: false } },
    { ...valid, consents: { ...valid.consents, claude: 1 } }];
  for (const response of bad) {
    f.chrome.runtime.sendMessage = async () => {
      f.state.consents = sanitizeConsents({ perplexity: true });
      return response;
    };
    assert.deepEqual(await writeConsent("chatgpt", true), { applied: false, consents: f.state.consents });
  }
  f.chrome.runtime.sendMessage = async () => { throw new Error("private-runtime-error"); };
  const failure = await writeConsent("chatgpt", false);
  assert.deepEqual(failure, { applied: false, consents: f.state.consents });
  assert.doesNotMatch(JSON.stringify(failure), /private-runtime-error/);
});

test("unknown tools and non-boolean consent values never request permission", async () => {
  const f = fixture();
  assert.equal((await writeConsent("unknown", true)).applied, false);
  assert.equal((await writeConsent("chatgpt", 1)).applied, false);
  assert.deepEqual(f.calls, ["read", "read"]);
});
