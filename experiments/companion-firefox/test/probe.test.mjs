import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import vm from "node:vm";
import { stageFirefoxProbe } from "../stage.mjs";
import { encodeNativeMessage } from "../../../apps/companion-native-host/lib/native-messaging.mjs";
import { validateHostActivityEnvelope } from "../../../apps/companion-native-host/lib/host.mjs";
import { normalizeToolActivityEvent } from "../../../tools/mcp/lib/observability-store.mjs";

const ADDON_ID = "nexus-firefox-probe@codelogiic.local";
const PROBE = { kind: "transport-probe", browser_family: "firefox", schema_version: 1 };
const ACK = { schema_version: 1, fixture_only: true, browser_family: "firefox", ok: true };
function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), "nexus-firefox-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const output = join(root, "probe ' $dollar `backtick` $(ignored)");
  return { root, output, ...stageFirefoxProbe(output) };
}
function invoke(f, input = encodeNativeMessage(PROBE), addonId = ADDON_ID) {
  return spawnSync(f.launcher, [f.manifestPath, addonId], {
    input, env: { HOME: f.isolatedHome, PATH: "/nonexistent-path", NODE_NO_WARNINGS: "1" },
    timeout: 3000, maxBuffer: 4096,
  });
}
function decoded(result) {
  assert.equal(result.error, undefined);
  assert.equal(result.status, 0);
  assert.equal(result.stderr.toString(), "");
  assert.equal(result.stdout.readUInt32LE(0), result.stdout.length - 4);
  return JSON.parse(result.stdout.subarray(4));
}
function loadWorker(f, sendNativeMessage, setBadgeText) {
  let listener;
  const code = readFileSync(join(f.extension, "background.js"), "utf8");
  vm.runInNewContext(code, { browser: {
    runtime: { sendNativeMessage },
    action: { onClicked: { addListener(value) { listener = value; } }, setBadgeText },
  } });
  assert.equal(typeof listener, "function", "event listener must register synchronously");
  return listener;
}
const tick = () => new Promise((resolve) => setImmediate(resolve));

test("staged Firefox manifest uses event page, stable Gecko ID and a strict permission subset", (t) => {
  const f = fixture(t);
  const manifest = JSON.parse(readFileSync(join(f.extension, "manifest.json")));
  const production = JSON.parse(readFileSync(new URL("../../../apps/companion-browser-extension/manifest.json", import.meta.url)));
  assert.deepEqual(manifest.permissions, ["nativeMessaging"]);
  assert.ok(manifest.permissions.every((permission) => production.permissions.includes(permission)));
  assert.deepEqual(manifest.background, { scripts: ["background.js"] });
  assert.equal(manifest.browser_specific_settings.gecko.id, ADDON_ID);
  assert.deepEqual(manifest.browser_specific_settings.gecko.data_collection_permissions, { required: ["none"] });
  for (const key of ["content_scripts", "host_permissions", "optional_host_permissions", "optional_permissions"]) assert.equal(manifest[key], undefined);
  assert.equal(manifest.action.default_popup, undefined);
});

test("staging writes only isolated Linux registration with allowed_extensions and pinned executable", (t) => {
  const f = fixture(t);
  const manifest = JSON.parse(readFileSync(f.manifestPath));
  assert.equal(f.manifestPath, join(f.isolatedHome, ".mozilla/native-messaging-hosts/com.codelogiic.nexus.firefox_probe.json"));
  assert.deepEqual(manifest.allowed_extensions, [ADDON_ID]);
  assert.equal(manifest.allowed_origins, undefined);
  assert.equal(manifest.path, f.launcher);
  assert.equal(statSync(f.launcher).mode & 0o777, 0o700);
  assert.equal(statSync(f.manifestPath).mode & 0o777, 0o600);
  assert.deepEqual(decoded(invoke(f)), ACK);
  assert.equal(existsSync(join(f.isolatedHome, ".config")), false);
  assert.equal(existsSync(join(f.root, "ignored")), false);
});

test("fixture rejects activity, unknown/private fields, Chrome alias and wrong add-on ID without echo", (t) => {
  const f = fixture(t);
  for (const value of [{ ...PROBE, url: "https://private.invalid/secret" }, { ...PROBE, browser_family: "chrome" }, { browser_family: "firefox", tool_id: "chatgpt", started_at: "private" }, null]) {
    const result = invoke(f, encodeNativeMessage(value));
    assert.deepEqual(decoded(result), { ...ACK, ok: false });
    assert.equal(result.stdout.includes("private"), false);
  }
  assert.deepEqual(decoded(invoke(f, encodeNativeMessage(PROBE), "other@invalid")), { ...ACK, ok: false });
});

test("fixture bounds malformed, oversized and partial native frames", (t) => {
  const f = fixture(t);
  const oversized = Buffer.alloc(4);
  oversized.writeUInt32LE(1025);
  const malformed = Buffer.from([1, 0, 0, 0, 123]);
  for (const input of [oversized, malformed, Buffer.from([8, 0, 0, 0, 123])]) assert.deepEqual(decoded(invoke(f, input)), { ...ACK, ok: false });
});

test("Firefox action event reaches relocated fixture transport with no tab data or database", async (t) => {
  const f = fixture(t);
  const badges = [];
  const sends = [];
  const click = loadWorker(f, async (name, value) => {
    sends.push([name, JSON.parse(JSON.stringify(value))]);
    return decoded(invoke(f, encodeNativeMessage(value)));
  }, async (value) => { badges.push(value.text); });
  click({ url: "https://private.invalid", title: "private title" });
  await tick();
  assert.deepEqual(sends, [["com.codelogiic.nexus.firefox_probe", PROBE]]);
  assert.deepEqual(badges, ["OK"]);
  assert.equal(existsSync(join(f.isolatedHome, ".config/nexus")), false);
});

test("event page fails closed for missing host and mismatched responses", async (t) => {
  const f = fixture(t);
  for (const send of [async () => { throw new Error("private failure"); }, async () => ({ ...ACK, browser_family: "chrome" }), async () => ({ ...ACK, private: "secret" })]) {
    const badges = [];
    const click = loadWorker(f, send, async (value) => badges.push(value.text));
    click();
    await tick();
    assert.deepEqual(badges, ["ERR"]);
  }
});

test("event page bounds concurrent probes and can handle a fresh event after restart", async (t) => {
  const f = fixture(t);
  let release;
  let count = 0;
  const send = () => { count += 1; return new Promise((resolve) => { release = resolve; }); };
  const click = loadWorker(f, send, async () => undefined);
  click();
  click();
  assert.equal(count, 1);
  release(ACK);
  await tick();
  const afterRestart = loadWorker(f, async () => { count += 1; return ACK; }, async () => undefined);
  afterRestart();
  await tick();
  assert.equal(count, 2);
});

test("production ingestion still rejects Firefox instead of mapping it to Chrome", () => {
  const event = { tool_id: "chatgpt", surface: "browser", started_at: "2026-10-28T12:00:00Z", ended_at: "2026-10-28T12:00:01Z", detector: "selected-browser-tab", confidence: "surface-active", browser_family: "firefox", platform: "linux", schema_version: 1, consent_policy_version: 1 };
  assert.throws(() => normalizeToolActivityEvent(event), /browser_family must be chrome or edge/);
  assert.throws(() => validateHostActivityEnvelope(event, { browserFamily: "firefox" }), /browserFamily must be chrome or edge/);
  assert.equal(validateHostActivityEnvelope(event, { browserFamily: "chrome" }).ok, false);
});

test("staging refuses source tree and existing outputs without replacing files", (t) => {
  const f = fixture(t);
  writeFileSync(join(f.output, "keep"), "preserved");
  assert.throws(() => stageFirefoxProbe(f.output));
  assert.equal(readFileSync(join(f.output, "keep"), "utf8"), "preserved");
  assert.throws(() => stageFirefoxProbe("relative"), /stage_input_invalid/);
  assert.throws(() => stageFirefoxProbe(new URL("../should-not-exist", import.meta.url).pathname), /stage_input_invalid/);
});
