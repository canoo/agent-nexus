import assert from "node:assert/strict";
import test from "node:test";
import { DatabaseSync } from "node:sqlite";

test("the supported Node runtime provides node:sqlite", () => {
  assert.equal(typeof DatabaseSync, "function");

  const database = new DatabaseSync(":memory:");
  try {
    database.exec("CREATE TABLE runtime_check (value TEXT)");
    database.prepare("INSERT INTO runtime_check VALUES (?)").run("available");

    assert.equal(
      database.prepare("SELECT value FROM runtime_check").get().value,
      "available",
    );
  } finally {
    database.close();
  }
});
