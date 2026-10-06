import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createHash } from "node:crypto";
import test from "node:test";
import { DatabaseSync } from "node:sqlite";
import { createObservabilityStore, MODEL_USD_PRICES_PER_1M } from "../lib/observability-store.mjs";
import { taskLogEntry, estimateCloudCost } from "../lib/task-event.mjs";

function temporaryStore(t) {
  const directory = mkdtempSync(join(tmpdir(), "nexus-task-columns-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const store = createObservabilityStore({
    databasePath: join(directory, "observability.sqlite"),
    jsonlPath: join(directory, "mcp-tasks.jsonl"),
  });
  return { directory, store };
}

function event(overrides = {}) {
  return {
    tool: "ollama_commit_msg",
    model: "qwen2.5-coder:1.5b",
    routing: "local",
    tokens_in: 100,
    tokens_out: 50,
    cloud_cost_equivalent: 0.00105,
    ms: 42,
    ok: true,
    ts: Date.parse("2026-10-05T12:00:00.000Z"),
    ...overrides,
  };
}

function readTask(t, directory) {
  const database = new DatabaseSync(join(directory, "observability.sqlite"));
  t.after(() => database.close());
  const rows = database.prepare("SELECT * FROM tasks").all().map((row) => Object.assign({}, row));
  assert.equal(rows.length, 1);
  return rows[0];
}

const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

test("recordMcpTask populates every previously-empty column (#112)", (t) => {
  const { directory, store } = temporaryStore(t);
  const inputHash = createHash("sha256").update("write a commit message", "utf8").digest("hex");
  const result = store.recordMcpTask(event({
    input_bytes: 21,
    output_bytes: 7,
    input_hash: inputHash,
    trace_id: "trace-abc123",
    span_id: "span-def456",
    idempotency_key: "req-1",
    quality_rating: 4,
  }));
  assert.equal(result.sqlite.ok, true);

  const row = readTask(t, directory);
  assert.equal(row.cost_usd, 0); // local ollama route: actual spend is $0
  assert.equal(row.input_bytes, 21);
  assert.equal(row.output_bytes, 7);
  assert.equal(row.input_hash, inputHash);
  assert.equal(row.task_type, "ollama_commit_msg");
  assert.equal(row.model_provider, "ollama");
  assert.equal(row.trace_id, "trace-abc123");
  assert.equal(row.span_id, "span-def456");
  assert.equal(row.idempotency_key, "req-1");
  assert.equal(row.quality_rating, 4);
});

test("trace_id/span_id default to fresh correlation values; other new columns null", (t) => {
  const { directory, store } = temporaryStore(t);
  const result = store.recordMcpTask(event());
  assert.equal(result.sqlite.ok, true);

  const row = readTask(t, directory);
  assert.match(row.trace_id, UUID_PATTERN);
  assert.equal(row.span_id, row.id);
  assert.equal(row.cost_usd, 0);
  assert.equal(row.input_bytes, null);
  assert.equal(row.output_bytes, null);
  assert.equal(row.input_hash, null);
  assert.equal(row.idempotency_key, null);
  assert.equal(row.quality_rating, null);
});

test("fast-path route also records zero actual cost with fast-path provider", (t) => {
  const { directory, store } = temporaryStore(t);
  store.recordMcpTask(event({ model: "fast-path", routing: "deterministic" }));
  const row = readTask(t, directory);
  assert.equal(row.model_provider, "fast-path");
  assert.equal(row.cost_usd, 0);
});

test("extended fields are validated like the rest of the contract", (t) => {
  const { store } = temporaryStore(t);
  assert.throws(() => store.recordMcpTask(event({ input_hash: "not-hex" })), /input_hash/);
  assert.throws(() => store.recordMcpTask(event({ input_hash: "ab".repeat(31) })), /input_hash/);
  assert.throws(() => store.recordMcpTask(event({ quality_rating: 0 })), /quality_rating/);
  assert.throws(() => store.recordMcpTask(event({ quality_rating: 6 })), /quality_rating/);
  assert.throws(() => store.recordMcpTask(event({ trace_id: "has spaces" })), /trace_id/);
  assert.throws(() => store.recordMcpTask(event({ input_bytes: -1 })), /input_bytes/);
  assert.throws(() => store.recordMcpTask(event({ prompt: "x" })), /unsupported field/);
});

test("MODEL_USD_PRICES_PER_1M documents zero local cost and is frozen", (t) => {
  assert.deepEqual(MODEL_USD_PRICES_PER_1M.ollama, { input: 0, output: 0 });
  assert.deepEqual(MODEL_USD_PRICES_PER_1M["fast-path"], { input: 0, output: 0 });
  assert.equal(Object.isFrozen(MODEL_USD_PRICES_PER_1M), true);
});

test("taskLogEntry measures bytes and hashes input without including content", (t) => {
  const entry = taskLogEntry({
    tool: "ollama_commit_msg",
    model: "qwen2.5-coder:1.5b",
    ms: 10,
    ok: true,
    prompt: "héllo wörld",
    response: "ok",
  });
  assert.equal(entry.input_bytes, Buffer.byteLength("héllo wörld", "utf8"));
  assert.equal(entry.output_bytes, 2);
  assert.equal(entry.input_hash, createHash("sha256").update("héllo wörld", "utf8").digest("hex"));
  assert.equal("prompt" in entry, false);
  assert.equal("response" in entry, false);
  // Existing fields still behave as before.
  assert.equal(entry.tokens_in, 3);
  assert.equal(entry.cloud_cost_equivalent, estimateCloudCost(3, 1));
});
