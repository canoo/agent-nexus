import assert from "node:assert/strict";
import { copyFileSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { DatabaseSync } from "node:sqlite";
import { createObservabilityStore } from "../lib/observability-store.mjs";

const FIXTURE = join(import.meta.dirname, "fixtures", "mcp-tasks.jsonl");

function temporaryStore(t, { withFixture = false } = {}) {
  const directory = mkdtempSync(join(tmpdir(), "nexus-legacy-import-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const databasePath = join(directory, "observability.sqlite");
  const jsonlPath = join(directory, "mcp-tasks.jsonl");
  if (withFixture) copyFileSync(FIXTURE, jsonlPath);
  return {
    directory,
    databasePath,
    jsonlPath,
    store: createObservabilityStore({ databasePath, jsonlPath }),
  };
}

function readDatabase(path, callback) {
  const database = new DatabaseSync(path);
  try { return callback(database); } finally { database.close(); }
}

function plain(row) {
  return Object.assign({}, row);
}

function storeMetaMarker(databasePath) {
  return readDatabase(databasePath, (database) =>
    database.prepare("SELECT value FROM store_meta WHERE key = 'legacy_jsonl_imported'").get(),
  );
}

test("ensureLegacyJsonlImported runs once: imports rows, marks, second call is a no-op", (t) => {
  const { store, databasePath } = temporaryStore(t, { withFixture: true });

  const first = store.ensureLegacyJsonlImported();
  assert.equal(first.ran, true);
  assert.equal(first.result.imported, 3);
  assert.equal(first.result.alreadyImported, 0);

  const marker = storeMetaMarker(databasePath);
  assert.ok(marker && typeof marker.value === "string" && marker.value.length > 0);

  const second = store.ensureLegacyJsonlImported();
  assert.deepEqual({ ran: second.ran, reason: second.reason }, { ran: false, reason: "already-imported" });

  readDatabase(databasePath, (database) => {
    assert.equal(database.prepare("SELECT COUNT(*) AS count FROM tasks").get().count, 3);
    assert.equal(database.prepare("SELECT COUNT(*) AS count FROM legacy_import_receipts").get().count, 3);
  });
});

test("ensureLegacyJsonlImported treats a missing legacy log as nothing to do", (t) => {
  const { store, databasePath } = temporaryStore(t, { withFixture: false });

  const result = store.ensureLegacyJsonlImported();
  assert.deepEqual({ ran: result.ran, reason: result.reason }, { ran: false, reason: "no-legacy-jsonl" });

  // Marker is set so startup never retries against a file that does not exist.
  assert.ok(storeMetaMarker(databasePath));

  readDatabase(databasePath, (database) => {
    assert.equal(database.prepare("SELECT COUNT(*) AS count FROM tasks").get().count, 0);
  });
});

test("legacy rows land with computed cost_usd and NULL extended metadata", (t) => {
  const { store, databasePath } = temporaryStore(t, { withFixture: true });
  store.ensureLegacyJsonlImported();

  readDatabase(databasePath, (database) => {
    const rows = database.prepare(`SELECT cost_usd, input_bytes, output_bytes, input_hash,
      trace_id, span_id, idempotency_key, quality_rating FROM tasks`).all().map(plain);
    assert.equal(rows.length, 3);
    for (const row of rows) {
      assert.deepEqual(row, {
        cost_usd: 0,
        input_bytes: null,
        output_bytes: null,
        input_hash: null,
        trace_id: null,
        span_id: null,
        idempotency_key: null,
        quality_rating: null,
      });
    }
  });
});
