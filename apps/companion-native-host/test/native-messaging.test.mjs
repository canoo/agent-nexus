import assert from "node:assert/strict";
import { mkdtempSync, existsSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";
import { DatabaseSync } from "node:sqlite";
import { createObservabilityStore } from "../../../tools/mcp/lib/observability-store.mjs";
import { CompanionNativeMessagingHost, validateHostActivityEnvelope } from "../lib/host.mjs";
import { encodeNativeMessage, NativeMessageDecoder } from "../lib/native-messaging.mjs";
import { createNativeHostManifest, manifestPath, nativeMessagingDirectory } from "../lib/registration.mjs";

function activity(overrides = {}) {
  return {
    tool_id: "chatgpt",
    surface: "browser",
    started_at: "2026-10-02T18:00:00Z",
    ended_at: "2026-10-02T18:04:12Z",
    detector: "selected-browser-tab",
    confidence: "surface-active",
    browser_family: "chrome",
    platform: "linux",
    schema_version: 1,
    consent_policy_version: 1,
    ...overrides,
  };
}

function temporaryStore(t) {
  const directory = mkdtempSync(join(tmpdir(), "nexus-native-host-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const databasePath = join(directory, "logs", "observability.sqlite");
  return { databasePath, store: createObservabilityStore({ databasePath, jsonlPath: join(directory, "logs", "mcp.jsonl") }) };
}

function enableChromeConsent(databasePath) {
  const database = new DatabaseSync(databasePath);
  try {
    database.prepare("UPDATE companion_settings SET collection_enabled = 1 WHERE id = 1").run();
    database.prepare(`INSERT INTO companion_tool_consents (
      adapter_id, tool_id, enabled, consent_policy_version, updated_at
    ) VALUES (?, ?, ?, ?, ?)`)
      .run("browser-chrome", "chatgpt", 1, 1, "2026-10-02T18:00:00.000Z");
  } finally {
    database.close();
  }
}

function countRows(databasePath, table) {
  const database = new DatabaseSync(databasePath);
  try { return database.prepare(`SELECT COUNT(*) AS count FROM ${table}`).get().count; } finally { database.close(); }
}

test("native decoder supports fragmented and batched little-endian frames", () => {
  const decoder = new NativeMessageDecoder();
  const first = encodeNativeMessage({ one: true });
  const second = encodeNativeMessage({ two: true });
  assert.deepEqual(decoder.push(first.subarray(0, 3)), []);
  assert.deepEqual(decoder.push(Buffer.concat([first.subarray(3), second])), [
    { ok: true, message: { one: true } },
    { ok: true, message: { two: true } },
  ]);
});

test("native decoder closes on a declared or buffered oversize frame without retaining input", () => {
  const decoder = new NativeMessageDecoder({ maxMessageBytes: 32 });
  const header = Buffer.alloc(4);
  header.writeUInt32LE(33, 0);
  assert.deepEqual(decoder.push(header), [{ ok: false, code: "native_message_too_large" }]);
  assert.equal(decoder.buffer.length, 0);
  assert.equal(decoder.closed, true);
  assert.deepEqual(decoder.push(Buffer.from("ignored")), [{ ok: false, code: "native_input_closed" }]);
});

test("host rejects malformed frames, unknown fields, and sensitive content without storage or echo", (t) => {
  const { store, databasePath } = temporaryStore(t);
  const host = new CompanionNativeMessagingHost({ browserFamily: "chrome", store });
  const malformed = Buffer.concat([Buffer.from([1, 0, 0, 0]), Buffer.from("{")]);
  assert.deepEqual(host.ingest(malformed), [{ ok: false, code: "malformed_native_json" }]);
  for (const field of ["prompt", "response", "url", "title", "metadata", "adapter_id"]) {
    const result = host.ingest(encodeNativeMessage(activity({ [field]: `do not persist ${field}` })));
    assert.deepEqual(result, [{ ok: false, code: "invalid_activity_envelope" }]);
    assert.doesNotMatch(JSON.stringify(result), /do not persist/);
  }
  assert.equal(existsSync(databasePath), false);
});

test("host rejects an envelope whose browser family does not match its fixed adapter", (t) => {
  const { store, databasePath } = temporaryStore(t);
  const host = new CompanionNativeMessagingHost({ browserFamily: "chrome", store });
  assert.deepEqual(host.ingest(encodeNativeMessage(activity({ browser_family: "edge" }))), [
    { ok: false, code: "browser_family_mismatch" },
  ]);
  assert.equal(validateHostActivityEnvelope(activity({ browser_family: "edge" }), { browserFamily: "chrome" }).ok, false);
  assert.equal(existsSync(databasePath), false);
});

test("host refuses valid activity while collection is disabled", (t) => {
  const { store, databasePath } = temporaryStore(t);
  const host = new CompanionNativeMessagingHost({ browserFamily: "chrome", store });
  assert.deepEqual(host.ingest(encodeNativeMessage(activity())), [
    { ok: false, code: "companion_collection_disabled" },
  ]);
  assert.equal(countRows(databasePath, "tool_activity"), 0);
});

test("host persists a valid consented activity only in the SQLite activity table", (t) => {
  const { store, databasePath } = temporaryStore(t);
  store.migrate();
  enableChromeConsent(databasePath);
  const host = new CompanionNativeMessagingHost({ browserFamily: "chrome", store });
  assert.deepEqual(host.ingest(encodeNativeMessage(activity())), [{ ok: true }]);
  assert.equal(countRows(databasePath, "tool_activity"), 1);
  assert.equal(countRows(databasePath, "tasks"), 0);
  assert.equal(existsSync(join(dirname(databasePath), "mcp.jsonl")), false);
});

test("registration manifests require supplied valid extension IDs and stay browser-specific", () => {
  const extensionId = "a".repeat(32);
  const chrome = createNativeHostManifest({
    browser: "chrome", extensionIds: [extensionId], hostPath: "/opt/nexus/chrome-host",
  });
  assert.deepEqual(chrome.allowed_origins, [`chrome-extension://${extensionId}/`]);
  assert.throws(() => createNativeHostManifest({
    browser: "chrome", extensionIds: ["not-an-extension"], hostPath: "/opt/nexus/chrome-host",
  }), /published IDs/);
  assert.notEqual(
    nativeMessagingDirectory({ browser: "chrome", platform: "linux", home: "/tmp/nexus-home" }),
    nativeMessagingDirectory({ browser: "edge", platform: "linux", home: "/tmp/nexus-home" }),
  );
  assert.match(manifestPath({ browser: "edge", platform: "darwin", home: "/tmp/nexus-home" }), /Microsoft Edge/);
});
