import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import {
  saveDecision,
  listDecisions,
  formatDecisions,
  parseMemory,
  slugify,
  validMemoryComponent,
} from "../lib/agent-memory.mjs";

function tmpRoot(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "nexus-decisions-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return dir;
}

const WHEN = new Date("2026-11-10T19:00:00.000Z");

test("saveDecision writes the Go store format and round-trips", (t) => {
  const root = tmpRoot(t);
  const file = saveDecision({
    root,
    project: "demo",
    what: "JWT over sessions",
    why: "Chose JWT for auth because the API needs to be stateless for horizontal scaling.",
    ruledOut: ["cookie sessions (stateful)", "OAuth (too heavy for internal API)"],
    now: WHEN,
  });
  // Filename: local-time stamp + slug, same shape as the Go store.
  assert.match(path.basename(file), /^\d{8}-\d{6}-jwt-over-sessions\.md$/);
  const raw = fs.readFileSync(file, "utf8");
  assert.ok(raw.startsWith('---\ntitle: "JWT over sessions"\n'));
  assert.ok(raw.includes("tags:\n    - decision\n"));
  assert.ok(raw.includes("Ruled out: cookie sessions (stateful); OAuth (too heavy for internal API).\n"));
  const mode = fs.statSync(file).mode & 0o777;
  assert.equal(mode, 0o600);

  const decisions = listDecisions({ root, project: "demo" });
  assert.equal(decisions.length, 1);
  assert.equal(decisions[0].title, "JWT over sessions");
  assert.equal(decisions[0].date, WHEN.toISOString());
  assert.deepEqual(decisions[0].tags, ["decision"]);

  const rendered = formatDecisions(decisions);
  const date = WHEN.toISOString().slice(0, 10);
  assert.equal(
    rendered,
    `## ${date} — JWT over sessions\n` +
      "Chose JWT for auth because the API needs to be stateless for horizontal scaling.\n" +
      "\nRuled out: cookie sessions (stateful); OAuth (too heavy for internal API).\n",
  );
});

test("a file written in the Go store format is readable (cross-language fixture)", (t) => {
  const root = tmpRoot(t);
  const dir = path.join(root, "demo");
  fs.mkdirSync(dir, { recursive: true });
  // Bytes exactly as Go yaml.Marshal + saveMemory produce them.
  fs.writeFileSync(
    path.join(dir, "20261110-190000-postgres-over-sqlite.md"),
    "---\ntitle: Postgres over SQLite\ndate: 2026-11-10T19:00:00Z\ntags:\n    - decision\n---\n\nSQLite insufficient for concurrent writes.\n",
  );
  fs.writeFileSync(
    path.join(dir, "20261110-190100-just-a-note.md"),
    "---\ntitle: Just a note\ndate: 2026-11-10T19:01:00Z\n---\n\nNot a decision.\n",
  );
  const decisions = listDecisions({ root, project: "demo" });
  assert.equal(decisions.length, 1);
  assert.equal(decisions[0].title, "Postgres over SQLite");
  assert.equal(decisions[0].body.trim(), "SQLite insufficient for concurrent writes.");
});

test("listDecisions is newest first and honors limit", (t) => {
  const root = tmpRoot(t);
  saveDecision({ root, project: "demo", what: "First", why: "Older.", now: new Date("2026-11-10T19:00:00Z") });
  saveDecision({ root, project: "demo", what: "Second", why: "Newer.", now: new Date("2026-11-10T19:02:00Z") });
  const all = listDecisions({ root, project: "demo" });
  assert.deepEqual(all.map((d) => d.title), ["Second", "First"]);
  assert.deepEqual(
    listDecisions({ root, project: "demo", limit: 1 }).map((d) => d.title),
    ["Second"],
  );
});

test("same-second saves do not overwrite each other", (t) => {
  const root = tmpRoot(t);
  const a = saveDecision({ root, project: "demo", what: "Same", why: "One.", now: WHEN });
  const b = saveDecision({ root, project: "demo", what: "Same", why: "Two.", now: WHEN });
  assert.notEqual(a, b);
  assert.equal(listDecisions({ root, project: "demo" }).length, 2);
});

test("validation rejects empty fields and escaping project names", (t) => {
  const root = tmpRoot(t);
  assert.throws(() => saveDecision({ root, project: "demo", what: " ", why: "y" }), /what/);
  assert.throws(() => saveDecision({ root, project: "demo", what: "x", why: "" }), /why/);
  assert.throws(() => saveDecision({ root, project: "../x", what: "x", why: "y" }), /invalid project/);
  assert.throws(() => listDecisions({ root, project: ".." }), /invalid project/);
  assert.equal(validMemoryComponent("ok-name_1"), true);
  assert.equal(validMemoryComponent("a/b"), false);
});

test("slugify mirrors the Go store", () => {
  assert.equal(slugify("JWT over sessions"), "jwt-over-sessions");
  assert.equal(slugify("  spaces & CAPS!! "), "spaces-caps");
  assert.equal(slugify("!!!"), "memory");
  assert.equal(slugify("a".repeat(100)), "a".repeat(80));
});

test("parseMemory falls back to heading and filename titles", () => {
  const headed = parseMemory("note.md", "# My heading\n\nBody text.\n");
  assert.equal(headed.title, "My heading");
  assert.equal(headed.preview, "# My heading");
  const bare = parseMemory("bare.md", "Just body.\n");
  assert.equal(bare.title, "bare.md");
});
