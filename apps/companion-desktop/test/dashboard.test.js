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
  };
}

function setupEnvironment({ onInvoke }) {
  const elements = {
    "#collection-heading": createMockElement("collection-heading", "h1"),
    "#collection-detail": createMockElement("collection-detail", "p"),
    "#consents": createMockElement("consents", "div"),
    "#host-state": createMockElement("host-state", "p"),
    "#disable-collection": createMockElement("disable-collection", "button"),
    "#disable-feedback": createMockElement("disable-feedback", "p"),
    "#native-host-form": createMockElement("native-host-form", "form"),
    "#register-host": createMockElement("register-host", "button"),
    "#host-feedback": createMockElement("host-feedback", "p"),
    "#browser": createMockElement("browser", "select"),
    "#extension-id": createMockElement("extension-id", "input"),
    "#host-path": createMockElement("host-path", "input"),
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

test("dashboard executes in VM: verifies initial status render and disable click updates status", async () => {
  const invokeCalls = [];
  let dashboardState = {
    collection: "enabled",
    store: "ready",
    consents: [
      { adapter: "Chrome browser", tool: "ChatGPT", state: "enabled" },
    ],
    nativeHost: {
      chrome: "registered",
      edge: "unregistered",
      registrationControl: "available",
    },
  };

  const onInvoke = async (command, payload) => {
    invokeCalls.push({ command, payload });
    if (command === "get_companion_dashboard") {
      return dashboardState;
    }
    if (command === "disable_companion_collection") {
      dashboardState = {
        collection: "disabled",
        store: "ready",
        consents: [
          { adapter: "Chrome browser", tool: "ChatGPT", state: "disabled" },
        ],
        nativeHost: {
          chrome: "registered",
          edge: "unregistered",
          registrationControl: "available",
        },
      };
      return dashboardState;
    }
    throw new Error(`Unexpected command: ${command}`);
  };

  const { elements, context } = setupEnvironment({ onInvoke });

  // Execute the actual dashboard.js script within Node's VM context
  const script = new vm.Script(dashboardCode, { filename: "dashboard.js" });
  script.runInContext(context);

  // Wait for initial refreshDashboard microtasks
  await new Promise(setImmediate);

  // Verify initial status render
  assert.equal(invokeCalls.length, 1);
  assert.equal(invokeCalls[0].command, "get_companion_dashboard");
  assert.equal(elements["#collection-heading"].textContent, "Collection is enabled");
  assert.equal(
    elements["#collection-detail"].textContent,
    "Only fixed, consented activity envelopes can be accepted by the shared local store."
  );
  assert.equal(elements["#consents"].children.length, 1);
  assert.equal(
    elements["#host-state"].textContent,
    "Chrome: registered. Edge: unregistered."
  );

  // Trigger disable click
  const disableButton = elements["#disable-collection"];
  await disableButton.trigger("click");

  // Verify disable_companion_collection was invoked
  assert.equal(invokeCalls.length, 2);
  assert.equal(invokeCalls[1].command, "disable_companion_collection");

  // Verify UI updated to reflect disabled collection status
  assert.equal(elements["#collection-heading"].textContent, "Collection is disabled");
  assert.equal(
    elements["#collection-detail"].textContent,
    "No new Companion activity can be recorded."
  );
  assert.equal(
    elements["#disable-feedback"].textContent,
    "Collection is disabled and all fixed consents are revoked."
  );
  assert.equal(elements["#consents"].children[0].children[1].textContent, "disabled");
});
