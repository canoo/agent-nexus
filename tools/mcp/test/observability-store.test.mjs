import assert from "node:assert/strict";
import { appendFileSync, copyFileSync, existsSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { DatabaseSync } from "node:sqlite";
import { createObservabilityStore } from "../lib/observability-store.mjs";

function temporaryStore(t, options = {}) {
  const directory = mkdtempSync(join(tmpdir(), "nexus-observability-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const databasePath = join(directory, "logs", "observability.sqlite");
  const jsonlPath = join(directory, "logs", "mcp-tasks.jsonl");
  return {
    directory,
    databasePath,
    jsonlPath,
    store: createObservabilityStore({ databasePath, jsonlPath, ...options }),
  };
}

function event(overrides = {}) {
  return {
    tool: "ollama_commit_msg",
    model: "qwen2.5-coder:1.5b",
    routing: "local",
    tokens_in: 12,
    tokens_out: 7,
    cloud_cost_equivalent: 0.000141,
    ms: 42,
    ok: true,
    ts: Date.parse("2026-10-02T17:23:45.678Z"),
    ...overrides,
  };
}

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

function readDatabase(path, callback) {
  const database = new DatabaseSync(path);
  try { return callback(database); } finally { database.close(); }
}

function setCompanionCollection(databasePath, { enabled, adapterId, toolId, consentEnabled, policyVersion = 1 }) {
  readDatabase(databasePath, (database) => {
    database.prepare(`UPDATE companion_settings
      SET collection_enabled = ?, updated_at = ? WHERE id = 1`)
      .run(enabled ? 1 : 0, "2026-10-02T18:00:00.000Z");
    if (adapterId && toolId && consentEnabled !== undefined) {
      database.prepare(`INSERT INTO companion_tool_consents (
        adapter_id, tool_id, enabled, consent_policy_version, updated_at
      ) VALUES (?, ?, ?, ?, ?)
      ON CONFLICT(adapter_id, tool_id) DO UPDATE SET
        enabled = excluded.enabled,
        consent_policy_version = excluded.consent_policy_version,
        updated_at = excluded.updated_at`)
        .run(adapterId, toolId, consentEnabled ? 1 : 0, policyVersion, "2026-10-02T18:00:00.000Z");
    }
  });
}

function plain(row) {
  return Object.assign({}, row);
}

test("migrations are owned, transactional, and idempotent", (t) => {
  const { store, databasePath, jsonlPath } = temporaryStore(t);
  store.migrate();
  store.migrate();

  assert.equal(existsSync(databasePath), true);
  assert.equal(existsSync(jsonlPath), false);
  readDatabase(databasePath, (database) => {
    assert.deepEqual(database.prepare("SELECT version FROM schema_migrations").all().map(plain), [{ version: 1 }, { version: 2 }, { version: 3 }, { version: 4 }]);
    const tables = database.prepare("SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name").all()
      .map(({ name }) => name);
    assert.deepEqual(tables, [
      "companion_settings", "companion_tool_consents", "legacy_import_receipts",
      "routing_decisions", "schema_migrations", "sessions", "store_meta", "tasks", "tool_activity",
    ]);
    assert.deepEqual(plain(database.prepare(`SELECT collection_enabled, raw_span_retention_days,
      daily_aggregate_retention_days FROM companion_settings WHERE id = 1`).get()), {
      collection_enabled: 0,
      raw_span_retention_days: 14,
      daily_aggregate_retention_days: 90,
    });
  });
});

test("Companion collection is disabled by default and cannot persist a valid activity envelope", (t) => {
  const { store, databasePath, jsonlPath } = temporaryStore(t);
  const result = store.recordToolActivity(activity());
  assert.deepEqual(result.sqlite, { ok: false, error: "companion_collection_disabled" });
  assert.equal(existsSync(jsonlPath), false);
  readDatabase(databasePath, (database) => {
    assert.equal(database.prepare("SELECT COUNT(*) AS count FROM tool_activity").get().count, 0);
  });
});

test("Companion requires a current matching adapter/tool consent before persisting activity", (t) => {
  const { store, databasePath, jsonlPath } = temporaryStore(t);
  store.migrate();
  setCompanionCollection(databasePath, { enabled: true });

  const missing = store.recordToolActivity(activity());
  assert.deepEqual(missing.sqlite, { ok: false, error: "companion_tool_consent_missing" });

  setCompanionCollection(databasePath, {
    enabled: true, adapterId: "browser-chrome", toolId: "chatgpt", consentEnabled: false,
  });
  const revoked = store.recordToolActivity(activity());
  assert.deepEqual(revoked.sqlite, { ok: false, error: "companion_tool_consent_missing" });

  setCompanionCollection(databasePath, {
    enabled: true, adapterId: "browser-edge", toolId: "chatgpt", consentEnabled: true,
  });
  const wrongAdapter = store.recordToolActivity(activity());
  assert.deepEqual(wrongAdapter.sqlite, { ok: false, error: "companion_tool_consent_missing" });

  readDatabase(databasePath, (database) => {
    assert.equal(database.prepare("SELECT COUNT(*) AS count FROM tool_activity").get().count, 0);
  });
  assert.equal(existsSync(jsonlPath), false);
});

test("a valid Companion activity span with enabled matching consent is persisted only in shared SQLite", (t) => {
  const { store, databasePath, jsonlPath } = temporaryStore(t);
  store.migrate();
  setCompanionCollection(databasePath, {
    enabled: true, adapterId: "browser-chrome", toolId: "chatgpt", consentEnabled: true,
  });
  const result = store.recordToolActivity(activity());
  assert.equal(result.sqlite.ok, true);
  assert.equal(existsSync(jsonlPath), false);

  readDatabase(databasePath, (database) => {
    assert.deepEqual(plain(database.prepare(`SELECT id, session_id, tool_id, surface, started_at,
      ended_at, detector, confidence, browser_family, platform, schema_version,
      consent_policy_version FROM tool_activity`).get()), {
      id: result.id,
      session_id: null,
      tool_id: "chatgpt",
      surface: "browser",
      started_at: "2026-10-02T18:00:00.000Z",
      ended_at: "2026-10-02T18:04:12.000Z",
      detector: "selected-browser-tab",
      confidence: "surface-active",
      browser_family: "chrome",
      platform: "linux",
      schema_version: 1,
      consent_policy_version: 1,
    });
  });
});

test("Companion rejects unknown and sensitive activity data before storage", (t) => {
  const { store, databasePath, jsonlPath } = temporaryStore(t);
  for (const field of [
    "prompt", "response", "url", "title", "page_title", "source_code",
    "account_id", "project_path", "metadata", "extension_payload",
  ]) {
    assert.throws(
      () => store.recordToolActivity(activity({ [field]: `private ${field} content` })),
      /unsupported field/,
    );
  }
  assert.equal(existsSync(databasePath), false);
  assert.equal(existsSync(jsonlPath), false);
});

test("Companion rejects arbitrary identifiers and invalid activity states before storage", (t) => {
  const { store, databasePath } = temporaryStore(t);
  assert.throws(() => store.recordToolActivity(activity({ tool_id: "https://chat.example/private" })), /allowlisted/);
  assert.throws(() => store.recordToolActivity(activity({ confidence: "request-sent" })), /surface-active/);
  assert.throws(() => store.recordToolActivity(activity({ ended_at: "2026-10-02T18:00:00Z" })), /after started_at/);
  assert.equal(existsSync(databasePath), false);
});

test("one safe event creates only its SQLite task, session, and routing decision", (t) => {
  const { store, databasePath, jsonlPath } = temporaryStore(t);
  const result = store.recordMcpTask(event());
  assert.equal(result.sqlite.ok, true);

  // The compatibility JSONL log is frozen: recording must not create or
  // append to it.
  assert.equal(existsSync(jsonlPath), false);

  readDatabase(databasePath, (database) => {
    assert.deepEqual(plain(database.prepare(`SELECT id, session_id, timestamp, source, tool, model,
      model_provider, route_band, routing, routing_reason, tokens_in, tokens_out,
      total_tokens, latency_ms, cloud_cost_equivalent, ok, error FROM tasks`).get()), {
      id: result.id,
      session_id: "mcp-2026-10-02",
      timestamp: "2026-10-02T17:23:45.678Z",
      source: "mcp-tool",
      tool: "ollama_commit_msg",
      model: "qwen2.5-coder:1.5b",
      model_provider: "ollama",
      route_band: "supervisor",
      routing: "local",
      routing_reason: "mcp:ollama_commit_msg",
      tokens_in: 12,
      tokens_out: 7,
      total_tokens: 19,
      latency_ms: 42,
      cloud_cost_equivalent: 0.000141,
      ok: 1,
      error: null,
    });
    assert.deepEqual(plain(database.prepare("SELECT id, start_time, cli_tool, nexus_mode FROM sessions").get()), {
      id: "mcp-2026-10-02",
      start_time: "2026-10-02T00:00:00.000Z",
      cli_tool: "mcp",
      nexus_mode: "hybrid",
    });
    assert.deepEqual(plain(database.prepare("SELECT task_id, decided_at, reason, classifier_version FROM routing_decisions").get()), {
      task_id: result.id,
      decided_at: "2026-10-02T17:23:45.678Z",
      reason: "mcp:ollama_commit_msg",
      classifier_version: "rules-v1",
    });
  });
});

test("fast-path uses deterministic routing and fast-path provider", (t) => {
  const { store, databasePath } = temporaryStore(t);
  const result = store.recordMcpTask(event({ model: "fast-path", routing: "deterministic" }));
  assert.equal(result.sqlite.ok, true);
  readDatabase(databasePath, (database) => {
    assert.deepEqual(plain(database.prepare("SELECT model_provider, route_band, routing FROM tasks").get()), {
      model_provider: "fast-path",
      route_band: "fast-path",
      routing: "deterministic",
    });
  });
});

test("unknown and privacy-sensitive event fields are rejected before persistence", (t) => {
  const { store, databasePath, jsonlPath } = temporaryStore(t);
  for (const field of ["prompt", "response", "source", "url", "title", "api_key", "source_diff"]) {
    assert.throws(() => store.recordMcpTask(event({ [field]: `private ${field} content` })), /unsupported field/);
  }
  assert.equal(existsSync(databasePath), false);
  assert.equal(existsSync(jsonlPath), false);
});

test("provider error text resembling prompt, URL, and source content is rejected before persistence", (t) => {
  const { store, databasePath, jsonlPath } = temporaryStore(t);
  const providerError = "prompt: secret request; https://provider.example/api; source: const secret = 'x'";
  assert.throws(
    () => store.recordMcpTask(event({ ok: false, error: providerError })),
    /allowlisted safe error code/,
  );
  assert.equal(existsSync(databasePath), false);
  assert.equal(existsSync(jsonlPath), false);
});

test("a SQLite failure is reported in the result without throwing", (t) => {
  const { store } = temporaryStore(t, {
    databaseFactory: () => { throw new Error("simulated sqlite outage"); },
  });
  const result = store.recordMcpTask(event());
  assert.equal(result.sqlite.ok, false);
  assert.match(result.sqlite.error, /simulated sqlite outage/);
});


test("legacy JSONL import is idempotent, keeps its source unchanged, and preserves repeated rows", (t) => {
  const { store, databasePath, directory } = temporaryStore(t);
  const inputPath = join(import.meta.dirname, "fixtures", "mcp-tasks.jsonl");
  const before = readFileSync(inputPath);

  const first = store.importLegacyMcpJsonl({ inputPath });
  assert.equal(first.sourcePath, inputPath);
  assert.equal(first.imported, 3);
  assert.equal(first.alreadyImported, 0);
  assert.equal(first.skippedBlank, 0);
  assert.equal(first.skippedMalformed, 0);
  assert.equal(first.skippedUnsafe, 0);
  assert.equal(readFileSync(inputPath).equals(before), true);

  const second = createObservabilityStore({
    databasePath,
    jsonlPath: join(directory, "logs", "compatibility-must-not-change.jsonl"),
  }).importLegacyMcpJsonl({ inputPath });
  assert.equal(second.imported, 0);
  assert.equal(second.alreadyImported, 3);
  assert.equal(second.skippedBlank, 0);
  assert.equal(second.skippedMalformed, 0);
  assert.equal(second.skippedUnsafe, 0);
  assert.equal(readFileSync(inputPath).equals(before), true);

  readDatabase(databasePath, (database) => {
    assert.equal(database.prepare("SELECT COUNT(*) AS count FROM tasks").get().count, 3);
    assert.equal(database.prepare("SELECT COUNT(*) AS count FROM routing_decisions").get().count, 3);
    assert.equal(database.prepare("SELECT COUNT(*) AS count FROM legacy_import_receipts").get().count, 3);
    const rows = database.prepare(`SELECT id, session_id, timestamp, model_provider, route_band,
      routing, tokens_in, tokens_out, total_tokens, cloud_cost_equivalent FROM tasks ORDER BY timestamp, id`).all().map(plain);
    assert.equal(new Set(rows.map(({ id }) => id)).size, 3);
    assert.deepEqual(rows.map(({ session_id, timestamp }) => ({ session_id, timestamp })), [
      { session_id: "mcp-2026-10-02", timestamp: "2026-10-02T17:23:45.678Z" },
      { session_id: "mcp-2026-10-02", timestamp: "2026-10-02T17:23:45.678Z" },
      { session_id: "mcp-2026-10-03", timestamp: "2026-10-03T00:00:00.000Z" },
    ]);
    assert.deepEqual(rows.filter(({ model_provider }) => model_provider === "fast-path").map(({ route_band, routing }) => ({ route_band, routing })), [
      { route_band: "fast-path", routing: "deterministic" },
    ]);
    const localWithoutEstimates = rows.find(({ model_provider, tokens_in }) => model_provider === "ollama" && tokens_in === null);
    assert.deepEqual(
      { tokens_in: localWithoutEstimates.tokens_in, tokens_out: localWithoutEstimates.tokens_out,
        total_tokens: localWithoutEstimates.total_tokens, cloud_cost_equivalent: localWithoutEstimates.cloud_cost_equivalent },
      { tokens_in: null, tokens_out: null, total_tokens: null, cloud_cost_equivalent: null },
    );
  });
});

test("legacy JSONL import handles append-only source growth without duplicate tasks", (t) => {
  const { store, databasePath, directory } = temporaryStore(t);
  const fixturePath = join(import.meta.dirname, "fixtures", "mcp-tasks.jsonl");
  const inputPath = join(directory, "legacy-append.jsonl");
  copyFileSync(fixturePath, inputPath);
  assert.equal(store.importLegacyMcpJsonl({ inputPath }).imported, 3);

  appendFileSync(inputPath, "{\"tool\":\"ollama_logic_refactor\",\"model\":\"llama3.2:3b\",\"ms\":8,\"ok\":true,\"ts\":1791072000000}\n");
  const appendedSource = readFileSync(inputPath);
  const rerun = store.importLegacyMcpJsonl({ inputPath });
  assert.deepEqual({ imported: rerun.imported, alreadyImported: rerun.alreadyImported }, {
    imported: 1,
    alreadyImported: 3,
  });
  assert.equal(readFileSync(inputPath).equals(appendedSource), true);
  readDatabase(databasePath, (database) => {
    assert.equal(database.prepare("SELECT COUNT(*) AS count FROM tasks").get().count, 4);
    assert.equal(database.prepare("SELECT COUNT(*) AS count FROM legacy_import_receipts").get().count, 4);
    assert.equal(database.prepare("SELECT COUNT(DISTINCT id) AS count FROM tasks").get().count, 4);
  });
});

test("legacy JSONL import reports blank, malformed, and unsafe rows without changing its source", (t) => {
  const { store } = temporaryStore(t);
  const inputPath = join(import.meta.dirname, "fixtures", "mcp-tasks-malformed.jsonl");
  const before = readFileSync(inputPath);
  const result = store.importLegacyMcpJsonl({ inputPath });
  assert.deepEqual(
    { imported: result.imported, skippedBlank: result.skippedBlank,
      skippedMalformed: result.skippedMalformed, skippedUnsafe: result.skippedUnsafe },
    { imported: 0, skippedBlank: 1, skippedMalformed: 1, skippedUnsafe: 1 },
  );
  assert.equal(readFileSync(inputPath).equals(before), true);
});
