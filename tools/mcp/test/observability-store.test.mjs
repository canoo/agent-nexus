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

function readDatabase(path, callback) {
  const database = new DatabaseSync(path);
  try { return callback(database); } finally { database.close(); }
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
    assert.deepEqual(database.prepare("SELECT version FROM schema_migrations").all().map(plain), [{ version: 1 }, { version: 2 }, { version: 3 }]);
    const tables = database.prepare("SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name").all()
      .map(({ name }) => name);
    assert.deepEqual(tables, ["legacy_import_receipts", "routing_decisions", "schema_migrations", "sessions", "store_meta", "tasks"]);
  });
});

test("one safe event creates compatibility JSONL plus its SQLite task, session, and routing decision", (t) => {
  const { store, databasePath, jsonlPath } = temporaryStore(t);
  const result = store.recordMcpTask(event());
  assert.equal(result.sqlite.ok, true);
  assert.equal(result.jsonl.ok, true);

  const lines = readFileSync(jsonlPath, "utf8").trim().split("\n");
  assert.equal(lines.length, 1);
  const compatibility = JSON.parse(lines[0]);
  assert.equal(compatibility.id, result.id);
  assert.deepEqual(Object.keys(compatibility), [
    "tool", "model", "routing", "tokens_in", "tokens_out", "cloud_cost_equivalent", "ms", "ok", "ts", "id",
  ]);

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

test("unknown and privacy-sensitive event fields are rejected before either persistence target", (t) => {
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

test("a SQLite failure still attempts compatibility JSONL without throwing", (t) => {
  const { store, jsonlPath } = temporaryStore(t, {
    databaseFactory: () => { throw new Error("simulated sqlite outage"); },
  });
  const result = store.recordMcpTask(event());
  assert.equal(result.sqlite.ok, false);
  assert.match(result.sqlite.error, /simulated sqlite outage/);
  assert.equal(result.jsonl.ok, true);
  assert.equal(JSON.parse(readFileSync(jsonlPath, "utf8")).id, result.id);
});

test("a JSONL failure leaves the committed SQLite task intact", (t) => {
  const { store, databasePath } = temporaryStore(t, {
    appendJsonl: () => { throw new Error("simulated JSONL outage"); },
  });
  const result = store.recordMcpTask(event());
  assert.equal(result.sqlite.ok, true);
  assert.equal(result.jsonl.ok, false);
  assert.match(result.jsonl.error, /simulated JSONL outage/);
  readDatabase(databasePath, (database) => {
    assert.equal(database.prepare("SELECT COUNT(*) AS count FROM tasks").get().count, 1);
  });
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
