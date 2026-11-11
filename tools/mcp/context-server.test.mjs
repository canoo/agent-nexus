import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, readdirSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StdioClientTransport } from '@modelcontextprotocol/sdk/client/stdio.js';

// End-to-end: spawn the real context-server.mjs over stdio and speak MCP to
// it, with temp memory/persona dirs so no real user data is touched.
async function connect(t, { memory, personas, project }) {
  const client = new Client({ name: 'nexus-context-test', version: '1.0.0' }, { capabilities: {} });
  const transport = new StdioClientTransport({
    command: process.execPath,
    args: [new URL('./context-server.mjs', import.meta.url).pathname],
    env: {
      ...process.env,
      NEXUS_MEMORY_ROOT: memory,
      NEXUS_PERSONAS_DIR: personas,
      NEXUS_PROJECT: project,
    },
    stderr: 'ignore',
  });
  t.after(() => client.close().catch(() => {}));
  await client.connect(transport);
  return client;
}

function callText(client, name, args) {
  return client.callTool({ name, arguments: args }).then((r) => ({
    isError: r.isError === true,
    text: r.content.map((c) => c.text).join('\n'),
  }));
}

test('server lists 5 tools and round-trips a decision through memory', async (t) => {
  const base = mkdtempSync(join(tmpdir(), 'nexus-context-server-'));
  t.after(() => rmSync(base, { recursive: true, force: true }));
  const memory = join(base, 'agent-memory');
  const personas = join(base, 'personas');
  mkdirSync(personas, { recursive: true });
  writeFileSync(join(personas, 'code-reviewer.md'),
    '---\nname: Code Reviewer\ndescription: Reviews code like a mentor.\ncolor: purple\nemoji: 👁️\n---\n\n# Code Reviewer Agent\n\nYou are Code Reviewer.\n');
  writeFileSync(join(personas, 'onboarding-guide.md'),
    '---\nname: Onboarding Guide\ndescription: Walks new users through setup.\n---\n\n# Onboarding Guide Agent\n\nYou are Onboarding Guide.\n');

  const client = await connect(t, { memory, personas, project: 'testproj' });

  const tools = await client.listTools();
  assert.deepEqual(
    tools.tools.map((tool) => tool.name).sort(),
    ['nexus_get_context', 'nexus_get_persona', 'nexus_get_session_summary', 'nexus_list_personas', 'nexus_log_decision'],
  );

  const list = await callText(client, 'nexus_list_personas', {});
  assert.equal(list.isError, false);
  assert.match(list.text, /code-reviewer — Code Reviewer/);
  assert.match(list.text, /onboarding-guide — Onboarding Guide/);

  const persona = await callText(client, 'nexus_get_persona', { name: 'Code Reviewer' });
  assert.equal(persona.isError, false);
  assert.match(persona.text, /# Code Reviewer Agent/);

  const before = await callText(client, 'nexus_get_context', {});
  assert.equal(before.isError, false);
  assert.match(before.text, /No agent-memory recorded yet for project "testproj"/);

  const logged = await callText(client, 'nexus_log_decision', {
    what: 'Use SQLite for task log',
    why: 'Single writer, no lock contention',
  });
  assert.equal(logged.isError, false);
  assert.match(logged.text, /Decision logged for project "testproj"/);
  const files = readdirSync(join(memory, 'testproj'));
  assert.equal(files.length, 1);
  assert.match(files[0], /^\d{8}-\d{6}-use-sqlite-for-task-log\.md$/);
  assert.match(readFileSync(join(memory, 'testproj', files[0]), 'utf8'), /tags:\n    - decision/);

  const after = await callText(client, 'nexus_get_context', {});
  assert.equal(after.isError, false);
  assert.match(after.text, /Use SQLite for task log/);
  assert.match(after.text, /Single writer, no lock contention/);

  const summary = await callText(client, 'nexus_get_session_summary', {});
  assert.equal(summary.isError, false);
  assert.match(summary.text, /Session summary — testproj/);
  assert.match(summary.text, /Use SQLite for task log/);
  assert.match(summary.text, /Personas available: 2/);

  const unknown = await callText(client, 'nexus_get_persona', { name: 'no-such-persona' });
  assert.equal(unknown.isError, true);
  assert.match(unknown.text, /unknown persona/);
  assert.match(unknown.text, /code-reviewer/);
});

test('server rejects empty decision fields via schema validation', async (t) => {
  const base = mkdtempSync(join(tmpdir(), 'nexus-context-server-'));
  t.after(() => rmSync(base, { recursive: true, force: true }));
  const client = await connect(t, {
    memory: join(base, 'agent-memory'),
    personas: join(base, 'personas'),
    project: 'testproj',
  });
  // The SDK surfaces input-validation failures as an isError tool result,
  // not a rejected call.
  const result = await callText(client, 'nexus_log_decision', { what: '', why: 'x' });
  assert.equal(result.isError, true);
  assert.match(result.text, /Input validation error/);
  assert.match(result.text, /what/);
});
