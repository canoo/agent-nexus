import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { mkdtempSync, realpathSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { DatabaseSync } from 'node:sqlite';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StdioClientTransport } from '@modelcontextprotocol/sdk/client/stdio.js';
import { ensureModelAvailable } from './model-readiness.mjs';

const runFile = promisify(execFile);
const privateText = 'PRIVATE_PROVIDER_RESPONSE';
function homeFor(t) {
  const home = realpathSync(mkdtempSync(join(tmpdir(), 'nexus-readiness-')));
  t.after(() => rmSync(home, { recursive: true, force: true }));
  return home;
}
async function endpoint(t) {
  const fixture = { status: 404, requests: [], stall: false };
  const server = createServer(async (request, response) => {
    let body = '';
    for await (const chunk of request) body += chunk;
    fixture.requests.push({ path: request.url, method: request.method, body: JSON.parse(body || '{}') });
    if (fixture.stall) return;
    if (request.url === '/api/show') {
      response.writeHead(fixture.status, { 'Content-Type': 'application/json',
        ...(fixture.status === 302 ? { Location: '/api/generate' } : {}),
      });
      response.end(JSON.stringify({ modelfile: privateText, error: privateText }));
    } else if (request.url === '/api/generate') {
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ response: 'fixture output' }));
    } else {
      response.writeHead(500);
      response.end(privateText);
    }
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  fixture.url = `http://127.0.0.1:${server.address().port}`;
  t.after(async () => {
    server.closeAllConnections();
    await new Promise(resolve => server.close(resolve));
  });
  return fixture;
}
async function mcpClient(t, home, url, extra = {}) {
  const transport = new StdioClientTransport({
    command: process.execPath,
    args: [fileURLToPath(new URL('./server.mjs', import.meta.url))],
    env: { HOME: home, NEXUS_REPO: home, OLLAMA_HOST_URL: url, ...extra },
    stderr: 'pipe',
  });
  let stderr = '';
  transport.stderr.on('data', chunk => { stderr += chunk; });
  const client = new Client({ name: 'nexus-readiness-test', version: '1.0.0' });
  t.after(() => client.close());
  await client.connect(transport);
  return { client, stderr: () => stderr };
}

test('disabled local AI and invalid model names cannot call the readiness transport', async () => {
  const original = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = async () => { calls++; throw new Error(privateText); };
  try {
    await assert.rejects(ensureModelAvailable({ NEXUS_LOCAL_AI: 'false' }, 'valid'), /LOCAL_AI_DISABLED/);
    await assert.rejects(ensureModelAvailable({}, 'private invalid'), /invalid model name/);
    assert.equal(calls, 0);
  } finally { globalThis.fetch = original; }
});

test('readiness cancels response bodies without reading provider data and sanitizes fetch failures', async () => {
  const original = globalThis.fetch;
  let cancelled = 0;
  globalThis.fetch = async (_url, options) => {
    assert.equal(options.redirect, 'error');
    assert.equal(options.method, 'POST');
    assert.deepEqual(JSON.parse(options.body), { model: 'team/model:v1' });
    return {
      status: 200, body: { async cancel() { cancelled++; } },
      async json() { assert.fail('readiness must not read provider data'); },
      async text() { assert.fail('readiness must not read provider data'); },
    };
  };
  try {
    await ensureModelAvailable({}, 'team/model:v1');
    assert.equal(cancelled, 1);
    globalThis.fetch = async () => { throw new Error(privateText); };
    await assert.rejects(ensureModelAvailable({}, 'valid'), {
      message: 'Ollama model availability check failed or timed out; verify the configured Ollama instance.',
    });
  } finally { globalThis.fetch = original; }
});

test('real HTTP readiness handles missing models, provider failures, redirects and deadlines', async t => {
  const fixture = await endpoint(t);
  const settings = { OLLAMA_HOST_URL: fixture.url };
  await assert.rejects(ensureModelAvailable(settings, 'missing'), /not installed.*ollama pull/s);
  fixture.status = 503;
  await assert.rejects(ensureModelAvailable(settings, 'valid'), /returned HTTP 503/);
  fixture.status = 302;
  await assert.rejects(ensureModelAvailable(settings, 'valid'), /failed or timed out/);
  fixture.status = 200;
  await ensureModelAvailable(settings, 'model-with-default-tag');
  assert.deepEqual(fixture.requests.map(r => r.path), Array(4).fill('/api/show'));
  assert.equal(fixture.requests.every(r => Object.keys(r.body).join(',') === 'model'), true);
  fixture.stall = true;
  await assert.rejects(ensureModelAvailable(settings, 'valid', 50), /failed or timed out/);
});

test('actual MCP tools preflight every route and recheck after a model is removed', async t => {
  const home = homeFor(t);
  const fixture = await endpoint(t);
  const { client, stderr } = await mcpClient(t, home, fixture.url, { NEXUS_MODEL_BOILERPLATE: 'custom:v2' });
  assert.equal(fixture.requests.length, 0, 'startup must not contact Ollama');
  for (const [name, args, expected] of [
    ['ollama_commit_msg', { diff: 'nontrivial input' }, 'qwen2.5-coder:1.5b'],
    ['ollama_boilerplate', { specification: 'fixture specification' }, 'custom:v2'],
    ['ollama_test_scaffold', { source_code: 'fixture source' }, 'qwen2.5-coder:1.5b'],
    ['ollama_lint_fix', { file_and_errors: 'fixture lint' }, 'llama3.2:3b'],
    ['ollama_logic_refactor', { code: 'fixture code' }, 'llama3.2:3b'],
  ]) {
    const reply = await client.callTool({ name, arguments: args });
    assert.equal(reply.isError, true, JSON.stringify(reply));
    assert.match(reply.content[0].text, /not installed.*ollama pull/s);
    assert.equal(reply.content[0].text.includes(privateText), false);
    assert.equal(fixture.requests.at(-1).body.model, expected);
  }
  assert.equal(fixture.requests.length, 5);
  assert.equal(fixture.requests.every(r => r.path === '/api/show'), true);
  fixture.status = 200;
  const success = await client.callTool({ name: 'ollama_boilerplate', arguments: { specification: 'fixture specification' } });
  assert.equal(success.isError, undefined);
  assert.equal(success.content[0].text, 'fixture output');
  assert.deepEqual(fixture.requests.slice(-2).map(r => r.path), ['/api/show', '/api/generate']);
  assert.equal(fixture.requests.at(-1).body.model, 'custom:v2');
  fixture.status = 404;
  const removed = await client.callTool({ name: 'ollama_boilerplate', arguments: { specification: 'fixture specification' } });
  assert.equal(removed.isError, true);
  assert.equal(fixture.requests.filter(r => r.path === '/api/generate').length, 1);
  assert.equal(stderr().includes(privateText), false);
  await client.close();
  const database = new DatabaseSync(join(home, '.config/nexus/logs/observability.sqlite'), { readOnly: true });
  try {
    const rows = database.prepare('SELECT ok, error FROM tasks ORDER BY rowid').all();
    assert.equal(rows.length, 7);
    assert.equal(rows.filter(r => r.ok === 0).every(r => r.error === 'ollama_request_failed'), true);
  } finally { database.close(); }
});

test('MCP deterministic fast paths and disabled local AI avoid all readiness requests', async t => {
  const home = homeFor(t);
  const fixture = await endpoint(t);
  const { client } = await mcpClient(t, home, fixture.url, { NEXUS_LOCAL_AI: 'false' });
  const fast = await client.callTool({ name: 'ollama_commit_msg', arguments: {
    diff: 'diff --git a/go.sum b/go.sum\n+fixture',
  } });
  assert.equal(fast.content[0].text, 'chore: update lock file');
  const disabled = await client.callTool({ name: 'ollama_boilerplate', arguments: { specification: 'fixture' } });
  assert.equal(disabled.isError, true);
  assert.match(disabled.content[0].text, /LOCAL_AI_DISABLED/);
  assert.equal(fixture.requests.length, 0);
});

test('actual shell delegate checks its selected model before generation and preserves exit codes', async t => {
  const home = homeFor(t);
  const fixture = await endpoint(t);
  const context = join(home, 'context.txt');
  writeFileSync(context, 'fixture context');
  const script = fileURLToPath(new URL('../automation/ollama-delegate.sh', import.meta.url));
  const env = {
    PATH: process.env.PATH, HOME: home, NEXUS_REPO: home,
    OLLAMA_HOST_URL: fixture.url, NEXUS_MODEL_BOILERPLATE: 'shell-custom:v2',
  };
  const invoke = extra => runFile('bash', [script, 'boilerplate', context], {
    env: { ...env, ...extra }, timeout: 7000,
  });
  await assert.rejects(invoke(), error => {
    assert.equal(error.code, 3);
    assert.equal(error.stdout, '');
    assert.match(error.stderr, /shell-custom:v2.*not installed.*ollama pull/s);
    assert.equal(error.stderr.includes(privateText), false);
    return true;
  });
  assert.deepEqual(fixture.requests.map(r => r.path), ['/api/show']);
  assert.deepEqual(fixture.requests[0].body, { model: 'shell-custom:v2' });
  fixture.status = 200;
  const success = await invoke();
  assert.equal(success.stdout, 'fixture output\n');
  assert.deepEqual(fixture.requests.slice(-2).map(r => r.path), ['/api/show', '/api/generate']);
  fixture.status = 503;
  await assert.rejects(invoke(), error => error.code === 3 && /HTTP 503/.test(error.stderr));
  const count = fixture.requests.length;
  await assert.rejects(invoke({ NEXUS_LOCAL_AI: 'false' }), error => error.code === 3 && /LOCAL_AI_DISABLED/.test(error.stderr));
  assert.equal(fixture.requests.length, count);
  fixture.stall = true;
  await assert.rejects(invoke(), error => error.code === 3 && /failed or timed out/.test(error.stderr));
});
