import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import test from "node:test";
import { createObservabilityStore } from "../../../tools/mcp/lib/observability-store.mjs";
import { transitionSelectedTab } from "../../companion-browser-extension/lib/activity.js";
import { encodeNativeMessage, MAX_NATIVE_MESSAGE_BYTES } from "../lib/native-messaging.mjs";

const BIN_CHROME = join(import.meta.dirname, "../bin/nexus-companion-native-host-chrome.mjs");
const BIN_EDGE = join(import.meta.dirname, "../bin/nexus-companion-native-host-edge.mjs");

function createIsolatedFixture(t) {
  const home = mkdtempSync(join(tmpdir(), "nexus-native-exec-"));
  t.after(() => rmSync(home, { recursive: true, force: true }));

  const databasePath = join(home, ".config", "nexus", "logs", "observability.sqlite");
  const jsonlPath = join(dirname(databasePath), "mcp-tasks.jsonl");

  const store = createObservabilityStore({ databasePath, jsonlPath });
  assert.equal(store.databasePath, databasePath);

  store.migrate();

  const env = {
    ...process.env,
    HOME: home,
  };

  return { home, databasePath, jsonlPath, env };
}

function enableSettingsAndConsent(databasePath, {
  adapterId = "browser-chrome",
  toolId = "chatgpt",
  collectionStartedAt = new Date(Date.now() - 10000).toISOString(),
  consentUpdatedAt = new Date(Date.now() - 10000).toISOString(),
  collectionEnabled = 1,
  consentEnabled = 1,
} = {}) {
  const db = new DatabaseSync(databasePath);
  try {
    db.prepare(`
      UPDATE companion_settings
      SET collection_enabled = ?, collection_started_at = ?
      WHERE id = 1
    `).run(collectionEnabled, collectionStartedAt);

    db.prepare(`
      INSERT INTO companion_tool_consents (
        adapter_id, tool_id, enabled, consent_policy_version, updated_at
      ) VALUES (?, ?, ?, 1, ?)
      ON CONFLICT(adapter_id, tool_id) DO UPDATE SET
        enabled = excluded.enabled,
        consent_policy_version = excluded.consent_policy_version,
        updated_at = excluded.updated_at
    `).run(adapterId, toolId, consentEnabled, consentUpdatedAt);
  } finally {
    db.close();
  }
}

function buildRealExtensionEnvelope({
  origin = "https://chatgpt.com",
  browserFamily = "chrome",
  platform = "linux",
  consents = { chatgpt: true },
  windowId = 1,
  startOffsetMs = 3000,
  endOffsetMs = 2000,
} = {}) {
  const activeSpans = {};
  const t1 = transitionSelectedTab({
    activeSpans,
    windowId,
    origin,
    consents,
    now: new Date(Date.now() - startOffsetMs).toISOString(),
    browserFamily,
    platform,
  });

  const t2 = transitionSelectedTab({
    activeSpans: t1.activeSpans,
    windowId,
    origin: null,
    consents,
    now: new Date(Date.now() - endOffsetMs).toISOString(),
    browserFamily,
    platform,
  });

  assert.ok(t2.event, "Expected a non-null fixed envelope event");
  return t2.event;
}

function queryDb(databasePath, sql, ...params) {
  const db = new DatabaseSync(databasePath);
  try {
    return db.prepare(sql).all(...params);
  } finally {
    db.close();
  }
}

function countRows(databasePath, table) {
  const db = new DatabaseSync(databasePath);
  try {
    return db.prepare(`SELECT COUNT(*) AS count FROM ${table}`).get().count;
  } finally {
    db.close();
  }
}

function assertCleanStderr(stderr) {
  const errStr = stderr.toString("utf8");
  for (const line of errStr.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    assert.match(
      trimmed,
      /^\(node:\d+\) ExperimentalWarning: SQLite is an experimental feature and might change at any time$|^\(Use `node --trace-warnings \.\.\.` to show where the warning was created\)$/,
      `Unexpected stderr output: ${trimmed}`
    );
  }
}

test("successful delivery for chrome executable writes activity row and no task/jsonl", (t) => {
  const { home, databasePath, jsonlPath, env } = createIsolatedFixture(t);
  enableSettingsAndConsent(databasePath, { adapterId: "browser-chrome", toolId: "chatgpt" });

  const event = buildRealExtensionEnvelope({ browserFamily: "chrome", platform: "linux" });
  const input = encodeNativeMessage(event);

  const res = spawnSync(process.execPath, [BIN_CHROME], { env, input, timeout: 5000 });
  assert.equal(res.status, 0);
  assert.equal(res.stdout.length, 0);
  assertCleanStderr(res.stderr);
  assert.doesNotMatch(res.stderr.toString("utf8"), /chatgpt/);

  const rows = queryDb(databasePath, "SELECT * FROM tool_activity");
  assert.equal(rows.length, 1);
  const row = rows[0];
  assert.equal(row.tool_id, event.tool_id);
  assert.equal(row.surface, event.surface);
  assert.equal(row.started_at, event.started_at);
  assert.equal(row.ended_at, event.ended_at);
  assert.equal(row.detector, event.detector);
  assert.equal(row.confidence, event.confidence);
  assert.equal(row.browser_family, event.browser_family);
  assert.equal(row.platform, event.platform);
  assert.equal(row.schema_version, event.schema_version);
  assert.equal(row.consent_policy_version, event.consent_policy_version);
  assert.equal(row.session_id, null);

  assert.equal(countRows(databasePath, "tasks"), 0);
  assert.equal(existsSync(jsonlPath), false);
});

test("successful delivery for edge executable writes activity row and no task/jsonl", (t) => {
  const { home, databasePath, jsonlPath, env } = createIsolatedFixture(t);
  enableSettingsAndConsent(databasePath, { adapterId: "browser-edge", toolId: "chatgpt" });

  const event = buildRealExtensionEnvelope({ browserFamily: "edge", platform: "linux" });
  const input = encodeNativeMessage(event);

  const res = spawnSync(process.execPath, [BIN_EDGE], { env, input, timeout: 5000 });
  assert.equal(res.status, 0);
  assert.equal(res.stdout.length, 0);
  assertCleanStderr(res.stderr);
  assert.doesNotMatch(res.stderr.toString("utf8"), /chatgpt/);

  const rows = queryDb(databasePath, "SELECT * FROM tool_activity");
  assert.equal(rows.length, 1);
  const row = rows[0];
  assert.equal(row.tool_id, event.tool_id);
  assert.equal(row.surface, event.surface);
  assert.equal(row.started_at, event.started_at);
  assert.equal(row.ended_at, event.ended_at);
  assert.equal(row.detector, event.detector);
  assert.equal(row.confidence, event.confidence);
  assert.equal(row.browser_family, event.browser_family);
  assert.equal(row.platform, event.platform);
  assert.equal(row.schema_version, event.schema_version);
  assert.equal(row.consent_policy_version, event.consent_policy_version);
  assert.equal(row.session_id, null);

  assert.equal(countRows(databasePath, "tasks"), 0);
  assert.equal(existsSync(jsonlPath), false);
});

test("disabled collection rejects ingestion and leaves tables empty", (t) => {
  const { home, databasePath, jsonlPath, env } = createIsolatedFixture(t);
  enableSettingsAndConsent(databasePath, {
    adapterId: "browser-chrome",
    toolId: "chatgpt",
    collectionEnabled: 0,
  });

  const event = buildRealExtensionEnvelope({ browserFamily: "chrome", platform: "linux" });
  const input = encodeNativeMessage(event);

  const res = spawnSync(process.execPath, [BIN_CHROME], { env, input, timeout: 5000 });
  assert.equal(res.status, 0);
  assert.equal(res.stdout.length, 0);
  assertCleanStderr(res.stderr);
  assert.doesNotMatch(res.stderr.toString("utf8"), /chatgpt/);

  assert.equal(countRows(databasePath, "tool_activity"), 0);
  assert.equal(countRows(databasePath, "tasks"), 0);
  assert.equal(existsSync(jsonlPath), false);
});

test("consent revoked rejects ingestion and leaves tables empty", (t) => {
  const { home, databasePath, jsonlPath, env } = createIsolatedFixture(t);
  enableSettingsAndConsent(databasePath, {
    adapterId: "browser-chrome",
    toolId: "chatgpt",
    consentEnabled: 0,
  });

  const event = buildRealExtensionEnvelope({ browserFamily: "chrome", platform: "linux" });
  const input = encodeNativeMessage(event);

  const res = spawnSync(process.execPath, [BIN_CHROME], { env, input, timeout: 5000 });
  assert.equal(res.status, 0);
  assert.equal(res.stdout.length, 0);
  assertCleanStderr(res.stderr);
  assert.doesNotMatch(res.stderr.toString("utf8"), /chatgpt/);

  assert.equal(countRows(databasePath, "tool_activity"), 0);
  assert.equal(countRows(databasePath, "tasks"), 0);
  assert.equal(existsSync(jsonlPath), false);
});

test("moved collection_started_at=Date.now()-1000 rejects older span", (t) => {
  const { home, databasePath, jsonlPath, env } = createIsolatedFixture(t);
  enableSettingsAndConsent(databasePath, {
    adapterId: "browser-chrome",
    toolId: "chatgpt",
    collectionStartedAt: new Date(Date.now() - 1000).toISOString(),
    consentUpdatedAt: new Date(Date.now() - 10000).toISOString(),
  });

  const event = buildRealExtensionEnvelope({
    browserFamily: "chrome",
    platform: "linux",
    startOffsetMs: 3000,
    endOffsetMs: 2000,
  });
  const input = encodeNativeMessage(event);

  const res = spawnSync(process.execPath, [BIN_CHROME], { env, input, timeout: 5000 });
  assert.equal(res.status, 0);
  assert.equal(res.stdout.length, 0);
  assertCleanStderr(res.stderr);
  assert.doesNotMatch(res.stderr.toString("utf8"), /chatgpt/);

  assert.equal(countRows(databasePath, "tool_activity"), 0);
  assert.equal(countRows(databasePath, "tasks"), 0);
  assert.equal(existsSync(jsonlPath), false);
});

test("crossed consent regrant similarly rejects older span", (t) => {
  const { home, databasePath, jsonlPath, env } = createIsolatedFixture(t);
  enableSettingsAndConsent(databasePath, {
    adapterId: "browser-chrome",
    toolId: "chatgpt",
    collectionStartedAt: new Date(Date.now() - 10000).toISOString(),
    consentUpdatedAt: new Date(Date.now() - 1000).toISOString(),
  });

  const event = buildRealExtensionEnvelope({
    browserFamily: "chrome",
    platform: "linux",
    startOffsetMs: 3000,
    endOffsetMs: 2000,
  });
  const input = encodeNativeMessage(event);

  const res = spawnSync(process.execPath, [BIN_CHROME], { env, input, timeout: 5000 });
  assert.equal(res.status, 0);
  assert.equal(res.stdout.length, 0);
  assertCleanStderr(res.stderr);
  assert.doesNotMatch(res.stderr.toString("utf8"), /chatgpt/);

  assert.equal(countRows(databasePath, "tool_activity"), 0);
  assert.equal(countRows(databasePath, "tasks"), 0);
  assert.equal(existsSync(jsonlPath), false);
});

test("envelope injected private url field rejects, never echoes to stdout/stderr, and writes no row", (t) => {
  const { home, databasePath, jsonlPath, env } = createIsolatedFixture(t);
  enableSettingsAndConsent(databasePath, { adapterId: "browser-chrome", toolId: "chatgpt" });

  const event = {
    ...buildRealExtensionEnvelope({ browserFamily: "chrome", platform: "linux" }),
    url: "https://secret-internal.corp/private-page-12345",
  };
  const input = encodeNativeMessage(event);

  const res = spawnSync(process.execPath, [BIN_CHROME], { env, input, timeout: 5000 });
  assert.equal(res.status, 0);
  assert.equal(res.stdout.length, 0);
  assertCleanStderr(res.stderr);
  assert.doesNotMatch(res.stdout.toString("utf8"), /secret-internal/);
  assert.doesNotMatch(res.stderr.toString("utf8"), /secret-internal/);
  assert.doesNotMatch(res.stdout.toString("utf8"), /private-page-12345/);
  assert.doesNotMatch(res.stderr.toString("utf8"), /private-page-12345/);

  assert.equal(countRows(databasePath, "tool_activity"), 0);
  assert.equal(countRows(databasePath, "tasks"), 0);
  assert.equal(existsSync(jsonlPath), false);
});

test("browser-family mismatch rejects ingestion and writes no row", (t) => {
  const { home, databasePath, jsonlPath, env } = createIsolatedFixture(t);
  enableSettingsAndConsent(databasePath, { adapterId: "browser-chrome", toolId: "chatgpt" });

  const event = buildRealExtensionEnvelope({ browserFamily: "edge", platform: "linux" });
  const input = encodeNativeMessage(event);

  const res = spawnSync(process.execPath, [BIN_CHROME], { env, input, timeout: 5000 });
  assert.equal(res.status, 0);
  assert.equal(res.stdout.length, 0);
  assertCleanStderr(res.stderr);
  assert.doesNotMatch(res.stderr.toString("utf8"), /chatgpt/);

  assert.equal(countRows(databasePath, "tool_activity"), 0);
  assert.equal(countRows(databasePath, "tasks"), 0);
  assert.equal(existsSync(jsonlPath), false);
});

test("malformed or oversize frame fails closed", (t) => {
  const { home, databasePath, jsonlPath, env } = createIsolatedFixture(t);
  enableSettingsAndConsent(databasePath, { adapterId: "browser-chrome", toolId: "chatgpt" });

  // Malformed JSON frame
  const malformedInput = Buffer.concat([Buffer.from([1, 0, 0, 0]), Buffer.from("{")]);
  const resMalformed = spawnSync(process.execPath, [BIN_CHROME], { env, input: malformedInput, timeout: 5000 });
  assert.equal(resMalformed.status, 0);
  assert.equal(resMalformed.stdout.length, 0);
  assertCleanStderr(resMalformed.stderr);

  // Oversize frame: header exceeds the fixed 4 KiB activity limit by one byte.
  const oversizeHeader = Buffer.alloc(4);
  oversizeHeader.writeUInt32LE(MAX_NATIVE_MESSAGE_BYTES + 1, 0);
  const resOversize = spawnSync(process.execPath, [BIN_CHROME], { env, input: oversizeHeader, timeout: 5000 });
  assert.equal(resOversize.status, 0);
  assert.equal(resOversize.stdout.length, 0);
  assertCleanStderr(resOversize.stderr);

  assert.equal(countRows(databasePath, "tool_activity"), 0);
  assert.equal(countRows(databasePath, "tasks"), 0);
  assert.equal(existsSync(jsonlPath), false);
});
