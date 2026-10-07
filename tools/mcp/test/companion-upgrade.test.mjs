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
    before.prepare("INSERT INTO store_meta (key, value) VALUES (?, ?)").run("legacy_jsonl_imported", "1");
    assert.deepEqual(before.prepare("SELECT version FROM schema_migrations ORDER BY version").all().map(row => row.version), [1, 2, 3]);
  } finally { before.close(); }

  const upgradedStore = createObservabilityStore({ databasePath });
  upgradedStore.migrate();
  upgradedStore.migrate();
  const after = new DatabaseSync(databasePath);
  try {
    assert.deepEqual(after.prepare("SELECT version FROM schema_migrations ORDER BY version").all().map(row => row.version), [1, 2, 3, 4, 5]);
    assert.deepEqual({ ...after.prepare("SELECT * FROM tasks WHERE id = ?").get(recorded.id) }, task);
    assert.equal(after.prepare("SELECT value FROM store_meta WHERE key = ?").get("legacy_jsonl_imported").value, "1");
    assert.equal(after.prepare("SELECT collection_enabled FROM companion_settings WHERE id = 1").get().collection_enabled, 0);
    assert.equal(after.prepare("SELECT COUNT(*) AS count FROM companion_tool_consents").get().count, 0);
    assert.equal(after.prepare("SELECT COUNT(*) AS count FROM tool_activity").get().count, 0);
  } finally { after.close(); }
});

test("migration 005 pauses boundary-less preview stores once and preserves grants/history", t => {
  const directory = mkdtempSync(join(tmpdir(), "nexus-boundary-upgrade-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const migrationsDir = join(directory, "preview-migrations");
  mkdirSync(migrationsDir);
  for (const name of ["001_observability.sql", "002_legacy-import-receipts.sql", "003_store_meta.sql", "004_companion-activity.sql"]) {
    copyFileSync(join(import.meta.dirname, "..", "migrations", name), join(migrationsDir, name));
  }
  const databasePath = join(directory, "preview.sqlite");
  const preview = createObservabilityStore({databasePath, migrationsDir});
  assert.equal(preview.databasePath, databasePath);
  preview.migrate();
  const database = new DatabaseSync(databasePath);
  try {
    database.exec(`UPDATE companion_settings SET collection_enabled=1;
      INSERT INTO companion_tool_consents VALUES ('browser-chrome','chatgpt',1,1,'2026-10-07 11:30:00');
      INSERT INTO tool_activity (id,tool_id,surface,started_at,ended_at,detector,confidence,browser_family,platform,schema_version,consent_policy_version)
      VALUES ('preserved','chatgpt','browser','2026-10-07T11:30:00Z','2026-10-07T11:31:00Z','selected-browser-tab','surface-active','chrome','linux',1,1);`);
    const before = database.prepare('SELECT * FROM tool_activity').all();
    const grants = database.prepare('SELECT * FROM companion_tool_consents').all();
    const upgraded = createObservabilityStore({databasePath});
    upgraded.migrate();
    assert.deepEqual({...database.prepare('SELECT collection_enabled,collection_started_at FROM companion_settings').get()}, {collection_enabled:0,collection_started_at:null});
    assert.deepEqual(database.prepare('SELECT * FROM tool_activity').all(), before);
    assert.deepEqual(database.prepare('SELECT * FROM companion_tool_consents').all(), grants);
    database.exec("UPDATE companion_settings SET collection_enabled=1,collection_started_at='2026-10-07T12:00:00Z'");
    upgraded.migrate();
    assert.equal(database.prepare('SELECT collection_enabled FROM companion_settings').get().collection_enabled,1);
    assert.equal(database.prepare('SELECT collection_started_at FROM companion_settings').get().collection_started_at,'2026-10-07T12:00:00Z');
  } finally { database.close(); }
});
