import assert from "node:assert/strict";
import { copyFileSync, mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { DatabaseSync } from "node:sqlite";
import { createObservabilityStore } from "../lib/observability-store.mjs";

test("Companion upgrades a v0.2.2 database without losing tasks or import markers", (t) => {
  const directory = mkdtempSync(join(tmpdir(), "nexus-companion-upgrade-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const migrationsDir = join(directory, "shipped-migrations");
  mkdirSync(migrationsDir);
  for (const name of ["001_observability.sql", "002_legacy-import-receipts.sql", "003_store_meta.sql"]) {
    copyFileSync(join(import.meta.dirname, "..", "migrations", name), join(migrationsDir, name));
  }
  const databasePath = join(directory, "observability.sqlite");
  const previousStore = createObservabilityStore({ databasePath, migrationsDir });
  const recorded = previousStore.recordMcpTask({
    tool: "ollama_commit_msg", model: "qwen2.5-coder:1.5b", routing: "local",
    tokens_in: 12, tokens_out: 7, cloud_cost_equivalent: 0.000141,
    ms: 42, ok: true, ts: Date.parse("2026-10-02T17:23:45Z"),
  });
  assert.equal(recorded.sqlite.ok, true);
  const before = new DatabaseSync(databasePath);
  let task;
  try {
    task = { ...before.prepare("SELECT * FROM tasks WHERE id = ?").get(recorded.id) };
    before.prepare("INSERT INTO store_meta (key, value) VALUES (?, ?)").run("legacy_jsonl_import_done", "1");
    assert.deepEqual(before.prepare("SELECT version FROM schema_migrations ORDER BY version").all().map(row => row.version), [1, 2, 3]);
  } finally { before.close(); }

  const upgradedStore = createObservabilityStore({ databasePath });
  upgradedStore.migrate();
  upgradedStore.migrate();
  const after = new DatabaseSync(databasePath);
  try {
    assert.deepEqual(after.prepare("SELECT version FROM schema_migrations ORDER BY version").all().map(row => row.version), [1, 2, 3, 4]);
    assert.deepEqual({ ...after.prepare("SELECT * FROM tasks WHERE id = ?").get(recorded.id) }, task);
    assert.equal(after.prepare("SELECT value FROM store_meta WHERE key = ?").get("legacy_jsonl_import_done").value, "1");
    assert.equal(after.prepare("SELECT collection_enabled FROM companion_settings WHERE id = 1").get().collection_enabled, 0);
    assert.equal(after.prepare("SELECT COUNT(*) AS count FROM companion_tool_consents").get().count, 0);
    assert.equal(after.prepare("SELECT COUNT(*) AS count FROM tool_activity").get().count, 0);
  } finally { after.close(); }
});
