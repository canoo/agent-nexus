import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, existsSync, readdirSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  defaultProject,
  formatRfc3339,
  formatStamp,
  getContext,
  getPersona,
  getSessionSummary,
  listMemories,
  listPersonas,
  listProjects,
  memorySlug,
  parseMemoryFile,
  resolveMemoryRoot,
  resolvePersonasDir,
  saveDecision,
  validMemoryComponent,
} from './context-store.mjs';

const REPO_PERSONAS = new URL('../../personas', import.meta.url).pathname;

function tmpRoot(t) {
  const dir = mkdtempSync(join(tmpdir(), 'nexus-context-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  return dir;
}

test('parseMemoryFile mirrors Go parseMemory: frontmatter, preview, # title fallback', () => {
  const entry = parseMemoryFile('20261006-200818-project-memory-system-launch.md',
    '---\ntitle: Project memory system launch\ndate: "2026-10-06T20:08:18-06:00"\n---\n\nProject memory now ships.\n');
  assert.equal(entry.title, 'Project memory system launch');
  assert.equal(entry.date, '2026-10-06T20:08:18-06:00');
  assert.equal(entry.preview, 'Project memory now ships.');
  assert.equal(entry.body, '\nProject memory now ships.\n');

  const bare = parseMemoryFile('notes.md', '# Heading title\n\nBody text.\n');
  assert.equal(bare.title, 'Heading title');
  assert.equal(bare.preview, '# Heading title');
});

test('saveDecision writes Go-compatible files readable by listMemories', (t) => {
  const root = tmpRoot(t);
  const now = new Date('2026-11-10T19:00:00');
  const path = saveDecision(root, 'agent-nexus', 'Use SQLite for task log', 'Single writer, no lock contention', now);
  assert.match(path, /20261110-190000-use-sqlite-for-task-log\.md$/);
  const raw = readFileSync(path, 'utf8');
  assert.match(raw, /^---\ntitle: Use SQLite for task log\ndate: "2026-11-10T19:00:00[+-]\d\d:\d\d"\ntags:\n    - decision\n---\n\n/);
  assert.match(raw, /## Why\n\nSingle writer, no lock contention/);
  const entries = listMemories(root, 'agent-nexus');
  assert.equal(entries.length, 1);
  assert.equal(entries[0].title, 'Use SQLite for task log');
  assert.deepEqual(entries[0].tags, ['decision']);

  // Same-second second save must not overwrite: collision loop steps a second.
  const path2 = saveDecision(root, 'agent-nexus', 'Use SQLite for task log', 'Second reason', now);
  assert.notEqual(path2, path);
  assert.match(path2, /20261110-190001-use-sqlite-for-task-log\.md$/);
  assert.equal(listMemories(root, 'agent-nexus').length, 2);
});

test('saveDecision rejects empty fields and traversal project names', (t) => {
  const root = tmpRoot(t);
  assert.throws(() => saveDecision(root, 'agent-nexus', '', 'why'), /title \(what\) is required/);
  assert.throws(() => saveDecision(root, 'agent-nexus', 'what', ''), /reason \(why\) is required/);
  assert.throws(() => saveDecision(root, '../escape', 'what', 'why'), /invalid project name/);
  assert.throws(() => saveDecision(root, 'a/b', 'what', 'why'), /invalid project name/);
  assert.equal(validMemoryComponent('..'), false);
  assert.equal(validMemoryComponent('ok-name_1'), true);
  assert.equal(existsSync(join(root, 'escape')), false);
});

test('getContext: missing project is empty, not an error; newest first; caps apply', (t) => {
  const root = tmpRoot(t);
  const empty = getContext(root, 'nope');
  assert.equal(empty.exists, false);
  assert.deepEqual(empty.entries, []);

  const dir = join(root, 'proj');
  mkdirSync(dir, { recursive: true });
  for (let i = 0; i < 12; i++) {
    writeFileSync(join(dir, `2026110${i % 9}-12000${i % 10}-entry-${i}.md`),
      `---\ntitle: Entry ${i}\ndate: "2026-11-0${i % 9}T12:00:00Z"\n---\n\nBody ${i}\n`);
  }
  const ctx = getContext(root, 'proj');
  assert.equal(ctx.exists, true);
  assert.equal(ctx.entries.length, 10); // MAX_CONTEXT_ENTRIES
  assert.deepEqual(ctx.projects, ['proj']);
});

test('listPersonas/getPersona read the real repo personas (no fixture personas)', () => {
  const personas = listPersonas(REPO_PERSONAS);
  assert.ok(personas.length >= 10, `expected >=10 real personas, got ${personas.length}`);
  const ids = personas.map((p) => p.id);
  assert.ok(ids.includes('engineering-code-reviewer'));
  const byStem = getPersona(REPO_PERSONAS, 'engineering-code-reviewer');
  assert.equal(byStem.name, 'Code Reviewer');
  assert.match(byStem.description, /code review/i);
  assert.match(byStem.content, /# Code Reviewer Agent/);
  const byName = getPersona(REPO_PERSONAS, 'Code Reviewer');
  assert.equal(byName.id, 'engineering-code-reviewer');
  const bySlug = getPersona(REPO_PERSONAS, 'code-reviewer');
  assert.equal(bySlug.id, 'engineering-code-reviewer');
  assert.throws(() => getPersona(REPO_PERSONAS, 'no-such-persona'), /unknown persona.*available:/);
});

test('getSessionSummary rolls up stored memory and never invents entries', (t) => {
  const root = tmpRoot(t);
  const personasDir = join(tmpRoot(t), 'personas');
  mkdirSync(personasDir, { recursive: true });
  writeFileSync(join(personasDir, 'a.md'), '---\nname: A\ndescription: First.\n---\n\n# A\n');
  saveDecision(root, 'proj', 'Ship it', 'Deadline', new Date('2026-11-10T10:00:00'));
  writeFileSync(join(root, 'proj', '20261110-090000-note.md'),
    '---\ntitle: A note\ndate: "2026-11-10T09:00:00Z"\n---\n\nJust a note.\n');
  const summary = getSessionSummary(root, personasDir, 'proj');
  assert.equal(summary.project, 'proj');
  assert.equal(summary.totalEntries, 2);
  assert.deepEqual(summary.projects, [{ name: 'proj', count: 2 }]);
  assert.equal(summary.recentDecisions.length, 1);
  assert.equal(summary.recentDecisions[0].title, 'Ship it');
  assert.equal(summary.recentEntries.length, 2);
  assert.equal(summary.personaCount, 1);
});

test('stamp and RFC3339 formats match Go layouts', () => {
  const d = new Date('2026-11-10T19:33:05');
  assert.match(formatStamp(d), /^\d{8}-\d{6}$/);
  assert.match(formatRfc3339(d), /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[+-]\d{2}:\d{2}$/);
  assert.equal(memorySlug('Use SQLite for task log'), 'use-sqlite-for-task-log');
  assert.equal(memorySlug('!!!'), 'memory');
});

test('resolvers honour env overrides and default project falls back to cwd', () => {
  assert.equal(resolveMemoryRoot({ NEXUS_MEMORY_ROOT: '/tmp/x' }), '/tmp/x');
  assert.equal(resolvePersonasDir({ NEXUS_PERSONAS_DIR: '/tmp/p' }), '/tmp/p');
  assert.equal(defaultProject({ NEXUS_PROJECT: 'myproj' }), 'myproj');
  assert.equal(typeof defaultProject({}), 'string');
});
