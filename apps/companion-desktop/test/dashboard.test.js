import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const dashboardPath = path.resolve(__dirname, "../ui/dashboard.js");
const dashboardCode = fs.readFileSync(dashboardPath, "utf-8");

function createMockElement(id = "", tagName = "div") {
  const listeners = {};
  const children = [];
  return {
    id,
    tagName,
    textContent: "",
    className: "",
    disabled: false,
    checked: false,
    value: "",
    children,
    addEventListener(event, handler) {
      if (!listeners[event]) listeners[event] = [];
      listeners[event].push(handler);
    },
    async trigger(event, eventObj = {}) {
      if (listeners[event]) {
        for (const handler of listeners[event]) {
          await handler(eventObj);
        }
      }
    },
    replaceChildren(...newChildren) {
      children.length = 0;
      children.push(...newChildren);
    },
    append(...newChildren) {
      children.push(...newChildren);
    },
    querySelector(selector) {
      for (const child of children) {
        if (
          selector.startsWith(".") &&
          child.className &&
          child.className.split(" ").includes(selector.slice(1))
        ) {
          return child;
        }
        if (selector.startsWith("#") && child.id === selector.slice(1)) {
          return child;
        }
        if (child.querySelector) {
          const found = child.querySelector(selector);
          if (found) return found;
        }
      }
      return null;
    },
  };
}

function setupEnvironment({ onInvoke }) {
  const elements = {
    "#collection-heading": createMockElement("collection-heading", "h1"),
    "#collection-detail": createMockElement("collection-detail", "p"),
    "#consents": createMockElement("consents", "div"),
    "#host-state": createMockElement("host-state", "p"),
    "#resume-collection": createMockElement("resume-collection", "button"),
    "#pause-collection": createMockElement("pause-collection", "button"),
    "#disable-collection": createMockElement("disable-collection", "button"),
    "#control-feedback": createMockElement("control-feedback", "p"),
    "#consent-acknowledgement": createMockElement("consent-acknowledgement", "input"),
    "#consent-feedback": createMockElement("consent-feedback", "p"),
    "#native-host-form": createMockElement("native-host-form", "form"),
    "#register-host": createMockElement("register-host", "button"),
    "#host-feedback": createMockElement("host-feedback", "p"),
    "#browser": createMockElement("browser", "select"),
    "#extension-id": createMockElement("extension-id", "input"),
    "#host-path": createMockElement("host-path", "input"),
    "#stored-spans": createMockElement("stored-spans"),
    "#retention-days": createMockElement("retention-days", "input"),
    "#save-retention": createMockElement("save-retention", "button"),
    "#prune-history": createMockElement("prune-history", "button"),
    "#clear-acknowledgement": createMockElement("clear-acknowledgement", "input"),
    "#clear-history": createMockElement("clear-history", "button"),
    "#data-feedback": createMockElement("data-feedback"),
  };

  const documentMock = {
    querySelector(selector) {
      return elements[selector] || null;
    },
    createElement(tagName) {
      return createMockElement("", tagName);
    },
  };

  const windowMock = {
    __TAURI__: {
      core: {
        invoke: (...args) => onInvoke(...args),
      },
    },
  };

  const context = vm.createContext({
    window: windowMock,
    document: documentMock,
    Set,
    Object,
    Array,
    String,
    Boolean,
    console,
  });

  return { elements, context };
}

test("no initial mutations: initial render calls only get_companion_dashboard", async () => {
  const invokeCalls = [];
  const dashboardState = {
    collection: "disabled",
    store: "ready",
    consents: [
      { adapterId: "browser-chrome", adapter: "Chrome browser", toolId: "chatgpt", tool: "ChatGPT", state: "disabled" },
    ],
    nativeHost: { chrome: "unregistered", edge: "unregistered", registrationControl: "available" },
  };

  const onInvoke = async (command, payload) => {
    invokeCalls.push({ command, payload });
    if (command === "get_companion_dashboard") return dashboardState;
    throw new Error(`Unexpected command: ${command}`);
  };

  const { elements, context } = setupEnvironment({ onInvoke });
  const script = new vm.Script(dashboardCode, { filename: "dashboard.js" });
  script.runInContext(context);
  await new Promise(setImmediate);

  assert.equal(invokeCalls.length, 1);
  assert.equal(invokeCalls[0].command, "get_companion_dashboard");
  assert.equal(elements["#collection-heading"].textContent, "Collection is disabled");
  assert.equal(elements["#consent-acknowledgement"].checked, false);
});

test("acknowledgement gating: grant buttons are disabled until checkbox is checked", async () => {
  const dashboardState = {
    collection: "disabled",
    store: "ready",
    consents: [
      { adapterId: "browser-chrome", adapter: "Chrome browser", toolId: "chatgpt", tool: "ChatGPT", state: "disabled" },
    ],
    nativeHost: { chrome: "unregistered", edge: "unregistered", registrationControl: "available" },
  };

  const { elements, context } = setupEnvironment({
    onInvoke: async (cmd) => {
      if (cmd === "get_companion_dashboard") return dashboardState;
      throw new Error(`Unexpected: ${cmd}`);
    },
  });

  const script = new vm.Script(dashboardCode, { filename: "dashboard.js" });
  script.runInContext(context);
  await new Promise(setImmediate);

  const row = elements["#consents"].children[0];
  const grantBtn = row.querySelector(".grant-button");
  assert.ok(grantBtn, "grant button should exist");
  assert.equal(grantBtn.disabled, true, "grant button must be initially disabled when unacknowledged");

  // Check acknowledgement
  elements["#consent-acknowledgement"].checked = true;
  await elements["#consent-acknowledgement"].trigger("change");

  assert.equal(grantBtn.disabled, false, "grant button must be enabled when acknowledged");

  // Uncheck acknowledgement
  elements["#consent-acknowledgement"].checked = false;
  await elements["#consent-acknowledgement"].trigger("change");

  assert.equal(grantBtn.disabled, true, "grant button must be disabled again when unchecked");
});

test("fixed grant payload: clicking grant dispatches set_companion_consent with exact parameters", async () => {
  const invokeCalls = [];
  let dashboardState = {
    collection: "disabled",
    store: "ready",
    consents: [
      { adapterId: "browser-chrome", adapter: "Chrome browser", toolId: "chatgpt", tool: "ChatGPT", state: "disabled" },
    ],
    nativeHost: { chrome: "unregistered", edge: "unregistered", registrationControl: "available" },
  };

  const onInvoke = async (command, payload) => {
    invokeCalls.push({ command, payload });
    if (command === "get_companion_dashboard") return dashboardState;
    if (command === "set_companion_consent") {
      dashboardState = {
        collection: "disabled",
        store: "ready",
        consents: [
          { adapterId: "browser-chrome", adapter: "Chrome browser", toolId: "chatgpt", tool: "ChatGPT", state: "enabled" },
        ],
        nativeHost: { chrome: "unregistered", edge: "unregistered", registrationControl: "available" },
      };
      return dashboardState;
    }
    throw new Error(`Unexpected command: ${command}`);
  };

  const { elements, context } = setupEnvironment({ onInvoke });
  const script = new vm.Script(dashboardCode, { filename: "dashboard.js" });
  script.runInContext(context);
  await new Promise(setImmediate);

  elements["#consent-acknowledgement"].checked = true;
  await elements["#consent-acknowledgement"].trigger("change");

  const grantBtn = elements["#consents"].children[0].querySelector(".grant-button");
  await grantBtn.trigger("click");

  assert.equal(invokeCalls.length, 2);
  assert.equal(invokeCalls[1].command, "set_companion_consent");
  assert.deepEqual(JSON.parse(JSON.stringify(invokeCalls[1].payload)), {
    request: {
      adapterId: "browser-chrome",
      toolId: "chatgpt",
      enabled: true,
      policyVersion: 1,
    },
  });

  // Verify UI updated to reflect enabled consent row with revoke button
  const updatedRow = elements["#consents"].children[0];
  const revokeBtn = updatedRow.querySelector(".revoke-button");
  assert.ok(revokeBtn, "revoke button should now exist in place of grant button");
  assert.equal(revokeBtn.disabled, false);
});

test("pause/resume dispatch: resume only enabled with ready+current grant, pause only enabled when collection enabled", async () => {
  const invokeCalls = [];
  let dashboardState = {
    collection: "disabled",
    store: "ready",
    consents: [
      { adapterId: "browser-chrome", adapter: "Chrome browser", toolId: "chatgpt", tool: "ChatGPT", state: "enabled" },
    ],
    nativeHost: { chrome: "unregistered", edge: "unregistered", registrationControl: "available" },
  };

  const onInvoke = async (command, payload) => {
    invokeCalls.push({ command, payload });
    if (command === "get_companion_dashboard") return dashboardState;
    if (command === "resume_companion_collection") {
      dashboardState = {
        ...dashboardState,
        collection: "enabled",
      };
      return dashboardState;
    }
    if (command === "pause_companion_collection") {
      dashboardState = {
        ...dashboardState,
        collection: "disabled",
      };
      return dashboardState;
    }
    throw new Error(`Unexpected command: ${command}`);
  };

  const { elements, context } = setupEnvironment({ onInvoke });
  const script = new vm.Script(dashboardCode, { filename: "dashboard.js" });
  script.runInContext(context);
  await new Promise(setImmediate);

  // Ready + disabled collection + current browser grant: resume is enabled, pause is disabled
  assert.equal(elements["#resume-collection"].disabled, false);
  assert.equal(elements["#pause-collection"].disabled, true);

  // Click resume
  await elements["#resume-collection"].trigger("click");
  assert.equal(invokeCalls[1].command, "resume_companion_collection");
  assert.equal(elements["#collection-heading"].textContent, "Collection is enabled");
  assert.equal(elements["#resume-collection"].disabled, true);
  assert.equal(elements["#pause-collection"].disabled, false);

  // Click pause
  await elements["#pause-collection"].trigger("click");
  assert.equal(invokeCalls[2].command, "pause_companion_collection");
  assert.equal(elements["#collection-heading"].textContent, "Collection is disabled");
  assert.equal(elements["#resume-collection"].disabled, false);
  assert.equal(elements["#pause-collection"].disabled, true);
});

test("revoke-all: disable collection triggers disable_companion_collection", async () => {
  const invokeCalls = [];
  let dashboardState = {
    collection: "enabled",
    store: "ready",
    consents: [
      { adapterId: "browser-chrome", adapter: "Chrome browser", toolId: "chatgpt", tool: "ChatGPT", state: "enabled" },
    ],
    nativeHost: { chrome: "unregistered", edge: "unregistered", registrationControl: "available" },
  };

  const onInvoke = async (command, payload) => {
    invokeCalls.push({ command, payload });
    if (command === "get_companion_dashboard") return dashboardState;
    if (command === "disable_companion_collection") {
      dashboardState = {
        collection: "disabled",
        store: "ready",
        consents: [
          { adapterId: "browser-chrome", adapter: "Chrome browser", toolId: "chatgpt", tool: "ChatGPT", state: "disabled" },
        ],
        nativeHost: { chrome: "unregistered", edge: "unregistered", registrationControl: "available" },
      };
      return dashboardState;
    }
    throw new Error(`Unexpected command: ${command}`);
  };

  const { elements, context } = setupEnvironment({ onInvoke });
  const script = new vm.Script(dashboardCode, { filename: "dashboard.js" });
  script.runInContext(context);
  await new Promise(setImmediate);

  await elements["#disable-collection"].trigger("click");
  assert.equal(invokeCalls[1].command, "disable_companion_collection");
  assert.equal(elements["#collection-heading"].textContent, "Collection is disabled");
  assert.equal(elements["#consents"].children[0].querySelector(".grant-button").disabled, true);
});

test("desktop foreground adapter is read-only without grant/revoke buttons", async () => {
  const dashboardState = {
    collection: "disabled",
    store: "ready",
    consents: [
      {
        adapterId: "desktop-foreground-app",
        adapter: "Desktop foreground adapter",
        toolId: "chatgpt",
        tool: "ChatGPT",
        state: "disabled",
      },
    ],
    nativeHost: { chrome: "unregistered", edge: "unregistered", registrationControl: "available" },
  };

  const { elements, context } = setupEnvironment({
    onInvoke: async () => dashboardState,
  });

  const script = new vm.Script(dashboardCode, { filename: "dashboard.js" });
  script.runInContext(context);
  await new Promise(setImmediate);

  const row = elements["#consents"].children[0];
  assert.equal(row.querySelector(".grant-button"), null);
  assert.equal(row.querySelector(".revoke-button"), null);
  const badge = row.querySelector(".read-only-badge");
  assert.ok(badge);
  assert.equal(badge.textContent, "read-only");
});

test("failed load fail-closed controls: when get_companion_dashboard fails, all controls disable", async () => {
  const { elements, context } = setupEnvironment({
    onInvoke: async () => {
      throw new Error("Store connection error");
    },
  });

  const script = new vm.Script(dashboardCode, { filename: "dashboard.js" });
  script.runInContext(context);
  await new Promise(setImmediate);

  assert.equal(elements["#collection-heading"].textContent, "Collection status unavailable");
  assert.equal(
    elements["#collection-detail"].textContent,
    "The local observability status is unavailable. Nothing is being enabled."
  );
  assert.equal(elements["#resume-collection"].disabled, true);
  assert.equal(elements["#pause-collection"].disabled, true);
  assert.equal(elements["#disable-collection"].disabled, true);
  assert.equal(elements["#consent-acknowledgement"].disabled, true);
});

test("safe errors: unknown errors generic, allowlisted errors displayed verbatim", async () => {
  let failWith = "Unknown sqlite internal crash details";
  const { elements, context } = setupEnvironment({
    onInvoke: async (cmd) => {
      if (cmd === "get_companion_dashboard") {
        return {
          collection: "disabled",
          store: "ready",
          consents: [
            { adapterId: "browser-chrome", adapter: "Chrome browser", toolId: "chatgpt", tool: "ChatGPT", state: "enabled" },
          ],
          nativeHost: { chrome: "unregistered", edge: "unregistered", registrationControl: "available" },
        };
      }
      if (cmd === "resume_companion_collection") {
        throw failWith;
      }
      throw new Error("unexpected");
    },
  });

  const script = new vm.Script(dashboardCode, { filename: "dashboard.js" });
  script.runInContext(context);
  await new Promise(setImmediate);

  // Trigger error with unlisted message
  await elements["#resume-collection"].trigger("click");
  assert.equal(
    elements["#control-feedback"].textContent,
    "The requested local action could not be completed."
  );

  // Trigger error with allowlisted message
  failWith = "The local NEXUS observability store could not be updated; no setting was changed.";
  await elements["#resume-collection"].trigger("click");
  assert.equal(
    elements["#control-feedback"].textContent,
    "The local NEXUS observability store could not be updated; no setting was changed."
  );
});


test("unacknowledged click and unknown consent IDs cannot enable collection", async () => {
  const calls = [];
  const status = { collection: "disabled", store: "ready", consents: [
    { adapterId: "browser-chrome", adapter: "Chrome browser", toolId: "chatgpt", tool: "ChatGPT", state: "disabled" },
    { adapterId: "browser-edge", adapter: "Edge browser", toolId: "unknown", tool: "Claude", state: "enabled" },
  ], nativeHost: { chrome: "unregistered", edge: "unregistered", registrationControl: "unavailable" } };
  const { elements, context } = setupEnvironment({onInvoke: async command => { calls.push(command); return status; }});
  new vm.Script(dashboardCode).runInContext(context);
  await new Promise(setImmediate);
  assert.equal(elements["#consents"].children.length, 1);
  assert.equal(elements["#resume-collection"].disabled, true);
  await elements["#consents"].children[0].querySelector(".grant-button").trigger("click");
  assert.deepEqual(calls, ["get_companion_dashboard"]);
});

test("failed mutation followed by unavailable status clears stale resume readiness", async () => {
  let reads = 0;
  const status = { collection: "enabled", store: "ready", consents: [
    { adapterId: "browser-chrome", adapter: "Chrome browser", toolId: "chatgpt", tool: "ChatGPT", state: "enabled" },
  ], nativeHost: { chrome: "unregistered", edge: "unregistered", registrationControl: "unavailable" } };
  const {elements, context} = setupEnvironment({onInvoke: async command => {
    if (command === "get_companion_dashboard" && reads++ === 0) return status;
    throw new Error("sensitive arbitrary provider payload");
  }});
  new vm.Script(dashboardCode).runInContext(context);
  await new Promise(setImmediate);
  await elements["#pause-collection"].trigger("click");
  assert.equal(elements["#resume-collection"].disabled, true);
  assert.equal(elements["#pause-collection"].disabled, true);
  assert.equal(elements["#consent-acknowledgement"].disabled, true);
  assert.equal(elements["#control-feedback"].textContent, "The requested local action could not be completed.");
});

function dataDashboard(dataControls = {state: "ready", retentionDays: 14, storedSpans: 2}) {
  return {collection: "disabled", store: "ready", consents: [], nativeHost: {chrome: "unregistered", edge: "unregistered", registrationControl: "unavailable"}, dataControls};
}
async function dataEnvironment(handler) {
  const env = setupEnvironment({onInvoke: handler});
  new vm.Script(dashboardCode).runInContext(env.context);
  await new Promise(setImmediate);
  return env;
}
test("data controls validate retention and require a fresh clear acknowledgement", async () => {
  const calls = [];
  const {elements: e} = await dataEnvironment(async (command, payload) => {
    calls.push({command, payload}); return dataDashboard();
  });
  assert.equal(e["#stored-spans"].textContent, "2");
  assert.equal(e["#clear-history"].disabled, true);
  await e["#clear-history"].trigger("click");
  for (const value of ["", "-1", "366", "1.5", "1e2", "NaN"]) {
    e["#retention-days"].value = value;
    await e["#save-retention"].trigger("click");
  }
  assert.equal(calls.length, 1);
  e["#retention-days"].value = "0";
  await e["#save-retention"].trigger("click");
  assert.equal(JSON.stringify(calls[1]), JSON.stringify({command: "set_companion_retention", payload: {request: {days: 0}}}));
  e["#clear-acknowledgement"].checked = true;
  await e["#clear-acknowledgement"].trigger("change");
  await e["#clear-history"].trigger("click");
  assert.equal(JSON.stringify(calls[2]), JSON.stringify({command: "clear_companion_history", payload: {request: {confirmed: true}}}));
  assert.equal(e["#clear-acknowledgement"].checked, false);
  assert.equal(e["#clear-history"].disabled, true);
  await e["#prune-history"].trigger("click");
  assert.equal(calls[3].command, "prune_companion_history");
});
test("missing helper and malformed data status fail closed", async () => {
  for (const controls of [null, {state: "unavailable"}, {state: "ready", retentionDays: 366, storedSpans: 0}, {state: "ready", retentionDays: 14, storedSpans: -1}, {state: "unknown", retentionDays: 14, storedSpans: 0}, {state: "ready", retentionDays: 14, storedSpans: 0, private: "path"}]) {
    let calls = 0;
    const {elements: e} = await dataEnvironment(async () => {calls++;return dataDashboard(controls);});
    assert.equal(e["#stored-spans"].textContent, "Unavailable");
    for (const id of ["#save-retention", "#prune-history", "#clear-history"]) {
      assert.equal(e[id].disabled, true); await e[id].trigger("click");
    }
    assert.equal(calls, 1);
  }
});
test("data mutations serialize and failed actions sanitize and refresh readiness", async () => {
  let resolveAction, reads = 0;
  const calls = [];
  const {elements: e} = await dataEnvironment(async (command) => {
    calls.push(command);
    if (command === "get_companion_dashboard") {reads++;return dataDashboard(reads === 1 ? {state: "ready", retentionDays: 14, storedSpans: 1} : {state: "unavailable"});}
    return new Promise((resolve, reject) => {resolveAction = () => reject(new Error("private SQL/path"));});
  });
  const pending = e["#prune-history"].trigger("click");
  assert.equal(e["#save-retention"].disabled, true);
  await e["#save-retention"].trigger("click");
  resolveAction(); await pending;
  assert.deepEqual(calls, ["get_companion_dashboard", "prune_companion_history", "get_companion_dashboard"]);
  assert.equal(e["#prune-history"].disabled, true);
  assert.ok(!e["#data-feedback"].textContent.includes("private"));
});
