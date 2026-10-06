'use strict';
// mcp-conformance.test.mjs — conformance tests for the shared shell MCP
// writers (scripts/mcp-merge.js, scripts/mcp-remove.js). Every writer path
// must preserve unknown keys and per-entry extras, and fail closed on
// null/unparseable/non-object configs (the #114 guards).
//
// Run: node --test scripts/mcp-conformance.test.mjs

import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import child from 'node:child_process';
import { fileURLToPath } from 'node:url';

const SCRIPTS = path.dirname(fileURLToPath(import.meta.url));
const SERVER = '/x/nexus/tools/mcp/server.mjs';

function tmpFile(name, content) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'mcp-conf-'));
  const p = path.join(dir, name);
  if (content !== undefined) {
    fs.writeFileSync(p, content);
  }
  return p;
}

function merge(configPath, serverPath) {
  const r = child.spawnSync('node', [path.join(SCRIPTS, 'mcp-merge.js'), configPath, serverPath || SERVER], { encoding: 'utf8' });
  return { status: r.status, stdout: r.stdout.trim(), stderr: r.stderr.trim() };
}

function remove(configPath) {
  const r = child.spawnSync('node', [path.join(SCRIPTS, 'mcp-remove.js'), configPath], { encoding: 'utf8' });
  return { status: r.status, stdout: r.stdout.trim(), stderr: r.stderr.trim() };
}

function readJSON(p) {
  return JSON.parse(fs.readFileSync(p, 'utf8'));
}

test('merge: creates a fresh config when the file is missing', function () {
  const p = tmpFile('mcp.json');
  const r = merge(p);
  assert.equal(r.status, 0);
  assert.equal(r.stdout, 'added');
  const cfg = readJSON(p);
  assert.deepEqual(cfg.mcpServers['nexus-ollama'], { command: 'node', args: [SERVER] });
});

test('merge: preserves unknown top-level keys and per-entry extras', function () {
  const p = tmpFile('mcp.json', JSON.stringify({
    customKey: true,
    nested: { a: [1, 2] },
    mcpServers: {
      other: { command: 'python', args: ['serve.py'], env: { K: 'v' }, cwd: '/tmp', disabled: true },
    },
  }));
  const r = merge(p);
  assert.equal(r.status, 0);
  assert.equal(r.stdout, 'added');
  const cfg = readJSON(p);
  assert.equal(cfg.customKey, true);
  assert.deepEqual(cfg.nested, { a: [1, 2] });
  assert.deepEqual(cfg.mcpServers.other,
    { command: 'python', args: ['serve.py'], env: { K: 'v' }, cwd: '/tmp', disabled: true });
  assert.deepEqual(cfg.mcpServers['nexus-ollama'], { command: 'node', args: [SERVER] });
});

test('merge: updates a stale entry but keeps its extras', function () {
  const p = tmpFile('mcp.json', JSON.stringify({
    mcpServers: {
      'nexus-ollama': { command: 'node', args: ['/old/path.mjs'], env: { KEEP: '1' }, cwd: '/work' },
    },
  }));
  const r = merge(p);
  assert.equal(r.status, 0);
  assert.equal(r.stdout, 'updated');
  const entry = readJSON(p).mcpServers['nexus-ollama'];
  assert.deepEqual(entry, { command: 'node', args: [SERVER], env: { KEEP: '1' }, cwd: '/work' });
});

test('merge: is idempotent and does not rewrite an unchanged file', function () {
  const p = tmpFile('mcp.json');
  assert.equal(merge(p).stdout, 'added');
  const before = fs.statSync(p).mtimeMs;
  const r = merge(p);
  assert.equal(r.status, 0);
  assert.equal(r.stdout, 'unchanged');
  assert.equal(fs.statSync(p).mtimeMs, before);
});

test('merge: fail-closed on null / non-object / unparseable configs', function () {
  const cases = [
    ['null config', 'null'],
    ['top-level array', '[]'],
    ['top-level string', '"hello"'],
    ['mcpServers null', '{"mcpServers": null}'],
    ['mcpServers string', '{"mcpServers": "junk"}'],
    ['mcpServers array', '{"mcpServers": []}'],
    ['unparseable', '{"mcpServers": {broken'],
    ['existing entry not an object', '{"mcpServers": {"nexus-ollama": "junk"}}'],
  ];
  for (const pair of cases) {
    const name = pair[0];
    const content = pair[1];
    const p = tmpFile('mcp.json', content);
    const r = merge(p);
    assert.notEqual(r.status, 0, name + ': expected refusal');
    assert.equal(fs.readFileSync(p, 'utf8'), content, name + ': file must be untouched');
  }
});

test('remove: deletes only the nexus-ollama entry, preserving the rest', function () {
  const p = tmpFile('mcp.json', JSON.stringify({
    customKey: 'keep',
    mcpServers: {
      'nexus-ollama': { command: 'node', args: [SERVER], env: { K: 'v' } },
      other: { command: 'x', env: { A: 'b' } },
    },
  }));
  const r = remove(p);
  assert.equal(r.status, 0);
  assert.equal(r.stdout, 'removed');
  const cfg = readJSON(p);
  assert.equal(cfg.customKey, 'keep');
  assert.ok(!('nexus-ollama' in cfg.mcpServers));
  assert.deepEqual(cfg.mcpServers.other, { command: 'x', env: { A: 'b' } });
});

test('remove: deletes the file when nothing else remains', function () {
  const p = tmpFile('mcp.json', JSON.stringify({
    mcpServers: { 'nexus-ollama': { command: 'node', args: [SERVER] } },
  }));
  const r = remove(p);
  assert.equal(r.status, 0);
  assert.equal(r.stdout, 'removed-file');
  assert.ok(!fs.existsSync(p));
});

test('remove: keeps the file when other top-level keys exist', function () {
  const p = tmpFile('mcp.json', JSON.stringify({
    customKey: true,
    mcpServers: { 'nexus-ollama': { command: 'node', args: [SERVER] } },
  }));
  const r = remove(p);
  assert.equal(r.status, 0);
  assert.equal(r.stdout, 'removed');
  const cfg = readJSON(p);
  assert.equal(cfg.customKey, true);
  assert.deepEqual(cfg.mcpServers, {});
});

test('remove: no-op when the entry or file is absent', function () {
  const p1 = tmpFile('mcp.json', JSON.stringify({ mcpServers: { other: { command: 'x' } } }));
  const before = fs.readFileSync(p1, 'utf8');
  assert.equal(remove(p1).stdout, 'unchanged');
  assert.equal(fs.readFileSync(p1, 'utf8'), before);

  const p2 = tmpFile('missing.json');
  assert.equal(remove(p2).stdout, 'unchanged');
});

test('remove: fail-closed on null / non-object / unparseable configs', function () {
  const cases = [
    ['null config', 'null'],
    ['mcpServers null', '{"mcpServers": null}'],
    ['mcpServers string', '{"mcpServers": "junk", "other": 1}'],
    ['unparseable', '{"mcpServers": {broken'],
  ];
  for (const pair of cases) {
    const name = pair[0];
    const content = pair[1];
    const p = tmpFile('mcp.json', content);
    const r = remove(p);
    assert.notEqual(r.status, 0, name + ': expected refusal');
    assert.equal(fs.readFileSync(p, 'utf8'), content, name + ': file must be untouched');
  }
});
