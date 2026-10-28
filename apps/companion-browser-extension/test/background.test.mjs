import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { writeConsent } from "../ui.js";
import { TOOL_DEFINITIONS, sanitizeConsents } from "../lib/policy.js";

const descriptors = Object.fromEntries(["chrome", "navigator", "Date", "setTimeout", "clearTimeout"].map((key) => [key, Object.getOwnPropertyDescriptor(globalThis, key)]));
afterEach(() => {
  for (const [key, descriptor] of Object.entries(descriptors)) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor);
    else delete globalThis[key];
  }
});
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
function event() {
  const listeners = [];
  return { addListener(listener) { listeners.push(listener); }, emit(...args) { return listeners.map((listener) => listener(...args)); } };
}
let instance = 0;
async function fixture({ spans = {}, send = async () => ({ schema_version: 1, ok: true }) } = {}) {
  const RealDate = descriptors.Date.value;
  let clock = RealDate.parse("2026-10-07T12:00:00Z");
  class FixtureDate extends RealDate {
    static now() { return clock; }
    constructor(...args) { super(...(args.length ? args : [clock += 1000])); }
  }
  Object.defineProperty(globalThis, "Date", { configurable: true, writable: true, value: FixtureDate });
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: { userAgent: "Chrome Linux" } });
  const onChanged = event();
  const session = { activeSpans: structuredClone(spans) };
  const local = { toolConsents: sanitizeConsents(Object.fromEntries(TOOL_DEFINITIONS.map(({ id }) => [id, true]))) };
  const deliveries = [];
  const allowed = new Set(TOOL_DEFINITIONS.flatMap(({ origins }) => origins));
  let nextRead, readCount = 0;
  function storageArea(state, areaName) {
    return {
      async get(key) {
        const snapshot = structuredClone({ [key]: state[key] });
        if (areaName === "session") {
          readCount += 1;
          const held = nextRead;
          nextRead = undefined;
          if (held) await held.promise;
        }
        return snapshot;
      },
      async set(values) {
        const changes = {};
        for (const [key, value] of Object.entries(values)) {
          const oldValue = state[key];
          state[key] = structuredClone(value);
          changes[key] = { oldValue, newValue: structuredClone(value) };
        }
        onChanged.emit(changes, areaName);
      },
    };
  }
  const tabs = new Map([
    [1, { active: true, windowId: 1, url: "https://chatgpt.com/c/private-chat?secret=x" }],
    [2, { active: true, windowId: 1, url: "https://claude.ai/chat/private-chat" }],
  ]);
  const chrome = {
    storage: { local: storageArea(local, "local"), session: storageArea(session, "session"), onChanged },
    tabs: {
      onActivated: event(), onUpdated: event(),
      async get(id) { if (!tabs.has(id)) throw new Error("private-tab-error"); return tabs.get(id); },
      async query() { return [{ id: 1 }]; },
    },
    windows: { onFocusChanged: event(), onRemoved: event(), WINDOW_ID_NONE: -1, async get() { return { focused: true }; } },
    permissions: { onRemoved: event(), onAdded: event(), async request() { return true; }, async contains({ origins }) { return origins.every((origin) => allowed.has(origin)); } },
    runtime: {
      id: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", onMessage: event(),
      getURL(path) { return `chrome-extension://${this.id}/${path}`; },
      sendNativeMessage(host, payload) { deliveries.push(structuredClone(payload)); return send(host, payload); },
    },
  };
  Object.defineProperty(globalThis, "chrome", { configurable: true, value: chrome });
  const worker = await import(`../background.js?fixture=${++instance}`);
  return {
    chrome, session, local, deliveries, tabs, allowed,
    advance(ms) { clock += ms; },
    message(request, sender = { id: chrome.runtime.id, url: chrome.runtime.getURL("popup.html") }) {
      const reply = deferred();
      const responses = chrome.runtime.onMessage.emit(request, sender, reply.resolve);
      if (!responses.includes(true)) reply.resolve(undefined);
      return reply.promise;
    },
    holdRead() { const held = deferred(); nextRead = held; return held; },
    get readCount() { return readCount; },
    activate(tabId) { chrome.tabs.onActivated.emit({ tabId, windowId: 1 }); },
    async flush() { for (let i = 0; i < 4; i += 1) await worker.whenIdle(); },
    consent(id, enabled) {
      const oldValue = local.toolConsents;
      local.toolConsents = { ...oldValue, [id]: enabled };
      onChanged.emit({ toolConsents: { oldValue, newValue: structuredClone(local.toolConsents) } }, "local");
    },
  };
}
const oldSpan = { tool_id: "chatgpt", started_at: "2026-10-07T11:59:00.000Z" };

test("overlapping activation and focus loss emit each completed span once", async () => {
  const f = await fixture();
  const held = f.holdRead();
  f.activate(1);
  await new Promise((resolve) => setImmediate(resolve));
  f.activate(2);
  f.chrome.windows.onFocusChanged.emit(-1);
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(f.readCount, 1, "later reads must wait for the first transition");
  held.resolve();
  await f.flush();
  assert.deepEqual(f.session.activeSpans, {});
  assert.deepEqual(f.deliveries.map((payload) => payload.tool_id), ["chatgpt", "claude"]);
  assert.equal(f.deliveries[0].ended_at, f.deliveries[1].started_at);
  for (const payload of f.deliveries) {
    assert.deepEqual(Object.keys(payload).sort(), ["tool_id", "surface", "started_at", "ended_at", "detector", "confidence", "browser_family", "platform", "schema_version", "consent_policy_version"].sort());
    assert.doesNotMatch(JSON.stringify(payload), /private-chat|secret|https:/);
  }
});
test("false consent snapshot discards old spans after immediate regrant", async () => {
  const f = await fixture({ spans: { 1: oldSpan } });
  const held = f.holdRead();
  f.consent("chatgpt", false);
  await new Promise((resolve) => setImmediate(resolve));
  f.consent("chatgpt", true);
  held.resolve();
  await f.flush();
  assert.deepEqual(f.session.activeSpans, {});
  assert.deepEqual(f.deliveries, []);
  f.activate(1);
  await f.flush();
  assert.equal(f.session.activeSpans[1].tool_id, "chatgpt");
  assert.notEqual(f.session.activeSpans[1].started_at, oldSpan.started_at);
});
test("permission revocation during pending transition suppresses outgoing activity", async () => {
  const f = await fixture({ spans: { 1: oldSpan } });
  const held = f.holdRead();
  f.activate(2);
  await new Promise((resolve) => setImmediate(resolve));
  f.allowed.delete("https://chatgpt.com/*");
  f.chrome.permissions.onRemoved.emit({ origins: ["https://chatgpt.com/*", "https://unrelated.example/*"] });
  held.resolve();
  await f.flush();
  assert.equal(f.local.toolConsents.chatgpt, false);
  assert.deepEqual(f.deliveries, []);
  assert.equal(f.session.activeSpans[1].tool_id, "claude");
});
test("later revocation defeats a late earlier permission check", async () => {
  const f = await fixture({ spans: { 1: oldSpan } });
  f.consent("chatgpt", false);
  await f.flush();
  const held = deferred(), checking = deferred();
  f.chrome.permissions.contains = async () => { checking.resolve(); return held.promise; };
  f.consent("chatgpt", true);
  await checking.promise;
  f.consent("chatgpt", false);
  held.resolve(true);
  await f.flush();
  f.local.toolConsents.chatgpt = true; // stale concurrent write, without a grant event
  f.activate(1);
  await f.flush();
  assert.deepEqual(f.session.activeSpans, {});
  assert.deepEqual(f.deliveries, []);
});
test("grant without required browser permission remains blocked", async () => {
  const f = await fixture();
  f.consent("chatgpt", false);
  await f.flush();
  f.allowed.delete("https://chat.openai.com/*");
  f.consent("chatgpt", true);
  await f.flush();
  f.activate(1);
  await f.flush();
  assert.deepEqual(f.session.activeSpans, {});
});
test("stalled native replies allow cleanup and deliveries remain bounded", async () => {
  const f = await fixture({ send: () => new Promise(() => {}) });
  for (let i = 0; i < 10; i += 1) f.activate(i % 2 ? 2 : 1);
  await f.flush();
  assert.equal(f.deliveries.length, 4);
  f.consent("claude", false);
  await f.flush();
  assert.deepEqual(f.session.activeSpans, {});
});
test("storage failure and vanished tab do not poison later transitions", async () => {
  const f = await fixture();
  const held = f.holdRead();
  f.activate(1);
  await new Promise((resolve) => setImmediate(resolve));
  f.activate(99);
  f.activate(2);
  held.reject(new Error("private-storage-error"));
  await f.flush();
  assert.equal(f.session.activeSpans[1].tool_id, "claude");
  assert.deepEqual(f.deliveries, []);
});
test("updated tabs, window focus and removal use the serialized lifecycle", async () => {
  const f = await fixture();
  f.chrome.tabs.onUpdated.emit(1, { status: "loading" }, { active: true, windowId: 1 });
  await f.flush();
  assert.deepEqual(f.session.activeSpans, {});
  f.chrome.tabs.onUpdated.emit(1, { status: "complete" }, { active: true, windowId: 1 });
  f.chrome.windows.onRemoved.emit(1);
  f.chrome.windows.onFocusChanged.emit(1);
  await f.flush();
  assert.equal(f.deliveries.length, 1);
  assert.equal(f.session.activeSpans[1].tool_id, "chatgpt");
});


test("simultaneous one-tool grants preserve each other's settings", async () => {
  const f = await fixture();
  f.consent("chatgpt", false);
  f.consent("claude", false);
  await f.flush();
  const checking = deferred(), held = deferred();
  const contains = f.chrome.permissions.contains;
  let first = true;
  f.chrome.permissions.contains = async (request) => {
    if (first) { first = false; checking.resolve(); await held.promise; }
    return contains(request);
  };
  const chatgpt = f.message({ kind: "set-tool-consent", toolId: "chatgpt", enabled: true });
  await checking.promise;
  const claude = f.message({ kind: "set-tool-consent", toolId: "claude", enabled: true }, {
    id: f.chrome.runtime.id, url: f.chrome.runtime.getURL("options.html"),
  });
  held.resolve();
  assert.equal((await chatgpt).applied, true);
  assert.equal((await claude).applied, true);
  await f.flush();
  assert.equal(f.local.toolConsents.chatgpt, true);
  assert.equal(f.local.toolConsents.claude, true);
});

test("disable arriving during a grant check wins and discards old activity", async () => {
  const f = await fixture({ spans: { 1: oldSpan } });
  const checking = deferred(), held = deferred();
  f.chrome.permissions.contains = async () => { checking.resolve(); return held.promise; };
  const grant = f.message({ kind: "set-tool-consent", toolId: "chatgpt", enabled: true });
  await checking.promise;
  const disable = f.message({ kind: "set-tool-consent", toolId: "chatgpt", enabled: false });
  held.resolve(true);
  assert.equal((await grant).applied, false);
  assert.equal((await disable).applied, true);
  await f.flush();
  assert.equal(f.local.toolConsents.chatgpt, false);
  assert.deepEqual(f.session.activeSpans, {});
  assert.deepEqual(f.deliveries, []);
});

test("worker rejects malformed requests and senders outside its own UI pages", async () => {
  const f = await fixture();
  const valid = { kind: "set-tool-consent", toolId: "chatgpt", enabled: false };
  for (const request of [null, {}, [], { ...valid, toolId: "unknown" }, { ...valid, enabled: 1 }, { ...valid, url: "private" }]) {
    assert.equal(await f.message(request), undefined);
  }
  for (const sender of [null, { id: "other", url: f.chrome.runtime.getURL("popup.html") },
    { id: f.chrome.runtime.id, url: "https://chatgpt.com/" },
    { id: f.chrome.runtime.id, url: f.chrome.runtime.getURL("popup.html?extra=true") }]) {
    assert.equal(await f.message(valid, sender), undefined);
  }
  assert.equal(f.local.toolConsents.chatgpt, true);
  assert.equal(f.readCount, 0);
});

test("worker refuses grants without permissions and sanitizes storage errors", async () => {
  const f = await fixture();
  f.allowed.clear();
  const denied = await f.message({ kind: "set-tool-consent", toolId: "chatgpt", enabled: true });
  assert.equal(denied.applied, false);
  f.chrome.storage.local.set = async () => { throw new Error("private-storage-path"); };
  const failure = await f.message({ kind: "set-tool-consent", toolId: "chatgpt", enabled: false });
  assert.equal(failure.applied, false);
  assert.deepEqual(Object.keys(failure).sort(), ["applied", "consents"]);
  assert.doesNotMatch(JSON.stringify(failure), /private-storage-path/);
});


test("popup destruction after worker-owned permission request does not lose explicit consent", async () => {
  const f = await fixture();
  f.consent("chatgpt", false);
  await f.flush();
  f.allowed.delete("https://chatgpt.com/*");
  f.allowed.delete("https://chat.openai.com/*");
  const permission = deferred();
  let requested = false;
  f.chrome.permissions.request = ({ origins }) => {
    requested = true;
    assert.deepEqual(origins, ["https://chatgpt.com/*", "https://chat.openai.com/*"]);
    return permission.promise;
  };
  // The worker gets the real UI message, but the popup's response channel is
  // destroyed before the browser prompt completes. Any follow-up UI mutation
  // would fail; successful persistence must belong to the worker itself.
  let popupAlive = true;
  f.chrome.runtime.sendMessage = (message) => {
    assert.equal(popupAlive, true);
    void f.message(message);
    return new Promise(() => {});
  };
  void writeConsent("chatgpt", true);
  assert.equal(requested, true, "request must occur synchronously, before a storage await");
  popupAlive = false;
  f.allowed.add("https://chatgpt.com/*");
  f.allowed.add("https://chat.openai.com/*");
  permission.resolve(true);
  await f.flush();
  assert.equal(f.local.toolConsents.chatgpt, true);
});

test("denied or failed permission requests cannot create consent, nor can unrelated grants", async () => {
  const f = await fixture();
  f.consent("chatgpt", false);
  await f.flush();
  for (const request of [async () => false, async () => { throw new Error("private prompt failure"); }, () => { throw new Error("gesture unavailable"); }]) {
    f.chrome.permissions.request = request;
    const response = await f.message({ kind: "request-tool-grant", toolId: "chatgpt" });
    assert.equal(response.applied, false);
    assert.equal(response.consents.chatgpt, false);
  }
  f.chrome.permissions.onAdded.emit({ origins: ["https://chatgpt.com/*", "https://chat.openai.com/*"] });
  await f.flush();
  assert.equal(f.local.toolConsents.chatgpt, false);
});

test("revocation and explicit disable cancel permission requests before late grant completion", async () => {
  for (const revoke of [false, true]) {
    const f = await fixture({ spans: { 1: oldSpan } });
    const permission = deferred();
    f.chrome.permissions.request = () => permission.promise;
    const grant = f.message({ kind: "request-tool-grant", toolId: "chatgpt" });
    const disabled = revoke ? undefined : f.message({ kind: "set-tool-consent", toolId: "chatgpt", enabled: false });
    if (revoke) f.chrome.permissions.onRemoved.emit({ origins: ["https://chatgpt.com/*"] });
    permission.resolve(true);
    assert.equal((await grant).applied, false);
    if (disabled) assert.equal((await disabled).applied, true);
    await f.flush();
    assert.equal(f.local.toolConsents.chatgpt, false);
    assert.deepEqual(f.session.activeSpans, {});
  }
});

test("permission result expires at 60 seconds and a late result cannot revive it", async () => {
  const f = await fixture();
  f.consent("chatgpt", false);
  await f.flush();
  const permission = deferred();
  f.chrome.permissions.request = () => permission.promise;
  const grant = f.message({ kind: "request-tool-grant", toolId: "chatgpt" });
  f.advance(60_000);
  permission.resolve(true);
  assert.equal((await grant).applied, false);
  assert.equal(f.local.toolConsents.chatgpt, false);
});

test("expiry timer cancels a stalled prompt and subsequent external permissions grant stays disabled", async () => {
  const f = await fixture();
  f.consent("chatgpt", false);
  await f.flush();
  const permission = deferred();
  f.chrome.permissions.request = () => permission.promise;
  let expire;
  Object.defineProperty(globalThis, "setTimeout", { configurable: true, writable: true, value: (fn, ms) => { assert.equal(ms, 60_000); expire = fn; return 1; } });
  Object.defineProperty(globalThis, "clearTimeout", { configurable: true, writable: true, value: () => {} });
  const grant = f.message({ kind: "request-tool-grant", toolId: "chatgpt" });
  expire();
  assert.equal((await grant).applied, false);
  permission.resolve(true);
  f.chrome.permissions.onAdded.emit({ origins: [...f.allowed] });
  await f.flush();
  assert.equal(f.local.toolConsents.chatgpt, false);
});

test("later explicit request supersedes old result and permission recheck still fails closed", async () => {
  const f = await fixture();
  f.consent("chatgpt", false);
  await f.flush();
  const first = deferred(), second = deferred();
  let calls = 0;
  f.chrome.permissions.request = () => (++calls === 1 ? first.promise : second.promise);
  const oldGrant = f.message({ kind: "request-tool-grant", toolId: "chatgpt" });
  const newGrant = f.message({ kind: "request-tool-grant", toolId: "chatgpt" });
  assert.equal((await oldGrant).applied, false);
  first.resolve(true);
  f.allowed.delete("https://chat.openai.com/*");
  second.resolve(true);
  assert.equal((await newGrant).applied, false);
  assert.equal(f.local.toolConsents.chatgpt, false);
});

test("worker-owned grants reject malformed messages, unknown tools and untrusted pages before requesting", async () => {
  const f = await fixture();
  f.chrome.permissions.request = () => assert.fail("untrusted request");
  for (const message of [{kind:"request-tool-grant",toolId:"unknown"}, {kind:"request-tool-grant",toolId:"chatgpt",enabled:true}]) assert.equal(await f.message(message), undefined);
  assert.equal(await f.message({kind:"request-tool-grant",toolId:"chatgpt"}, {id:f.chrome.runtime.id,url:"https://chatgpt.com/"}), undefined);
});


test("expiry during an in-flight storage grant is repaired before later observations", async () => {
  const f = await fixture();
  f.consent("chatgpt", false);
  await f.flush();
  const writing = deferred(), held = deferred();
  const set = f.chrome.storage.local.set;
  let first = true;
  f.chrome.storage.local.set = async (values) => {
    if (first && values.toolConsents?.chatgpt) { first = false; writing.resolve(); await held.promise; }
    return set(values);
  };
  const grant = f.message({ kind: "request-tool-grant", toolId: "chatgpt" });
  await writing.promise;
  f.advance(60_000);
  held.resolve();
  assert.equal((await grant).applied, false);
  await f.flush();
  assert.equal(f.local.toolConsents.chatgpt, false);
  assert.equal(f.local.toolConsents.claude, true);
  f.activate(1);
  await f.flush();
  assert.deepEqual(f.session.activeSpans, {});
});


test("independent explicit permission prompts preserve each other's pending consent", async () => {
  const f = await fixture();
  f.consent("chatgpt", false);
  f.consent("claude", false);
  await f.flush();
  const first = deferred(), second = deferred();
  let requests = 0;
  f.chrome.permissions.request = () => (++requests === 1 ? first.promise : second.promise);
  const chatgpt = f.message({ kind: "request-tool-grant", toolId: "chatgpt" });
  const claude = f.message({ kind: "request-tool-grant", toolId: "claude" });
  first.resolve(true);
  assert.equal((await chatgpt).applied, true);
  second.resolve(true);
  assert.equal((await claude).applied, true);
  assert.equal(f.local.toolConsents.chatgpt, true);
  assert.equal(f.local.toolConsents.claude, true);
});
