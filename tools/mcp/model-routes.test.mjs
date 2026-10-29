import { test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';
import { createModelRoutes, isValidModelName } from './model-routes.mjs';
import { loadSettings } from './settings.mjs';

const keys = [
  'NEXUS_SUPERVISOR_MODEL', 'NEXUS_LOGIC_MODEL', 'NEXUS_MODEL_COMMIT_MSG',
  'NEXUS_MODEL_BOILERPLATE', 'NEXUS_MODEL_TEST_SCAFFOLD',
  'NEXUS_MODEL_LINT_FIX', 'NEXUS_MODEL_LOGIC_REFACTOR',
];
function isolatedHome(t) {
  const home = realpathSync(mkdtempSync(join(tmpdir(), 'nexus-model-config-')));
  t.after(() => rmSync(home, { recursive: true, force: true }));
  return home;
}

test('model references accept bare, tagged, namespaced and registry forms with component limits', () => {
  for (const name of [
    'qwen2.5-coder:1.5b', 'llama3.2', '_local-model:q4_K_M',
    'team/model:latest', 'registry.example:5000/team/model:7b',
    'https://registry.example/team/model', 'http://localhost:5000/team/model:v1',
    'm'.repeat(80), `model:${'t'.repeat(80)}`,
    `${'n'.repeat(80)}/model`, `${'h'.repeat(350)}/team/model`,
  ]) assert.equal(isValidModelName(name), true, name);
});

test('invalid names reject empty parts, ambiguous separators and unsafe characters without trimming', () => {
  for (const value of [
    undefined, null, 12, {}, '', ' ', ' model', 'model ', 'model\n', 'model\r',
    'model\0', 'model\x1b[31m', 'model\u2028', 'model:tag:other', 'model:', ':tag',
    '/model', 'team/', 'registry//model', 'a/b/c/d', 'bad.namespace/model',
    '-model', 'model:-tag', 'model@sha256:abcdef', 'model?query', 'model#fragment',
    '$(echo private)', '../model', 'https://registry/model', 'ftp://registry/team/model',
    'm'.repeat(81), `model:${'t'.repeat(81)}`, `${'n'.repeat(81)}/model`,
    `${'h'.repeat(351)}/team/model`,
  ]) assert.equal(isValidModelName(value), false, String(value));
});

test('routing keeps defaults and per-task over band precedence', () => {
  assert.deepEqual(createModelRoutes(), {
    'commit-msg': 'qwen2.5-coder:1.5b', boilerplate: 'qwen2.5-coder:1.5b',
    'test-scaffold': 'qwen2.5-coder:1.5b', 'lint-fix': 'llama3.2:3b',
    'logic-refactor': 'llama3.2:3b',
  });
  assert.deepEqual(createModelRoutes({
    NEXUS_SUPERVISOR_MODEL: 'supervisor:v2', NEXUS_LOGIC_MODEL: 'logic:v3',
    NEXUS_MODEL_COMMIT_MSG: 'commit:v4', NEXUS_MODEL_LOGIC_REFACTOR: 'refactor:v5',
  }), {
    'commit-msg': 'commit:v4', boilerplate: 'supervisor:v2',
    'test-scaffold': 'supervisor:v2', 'lint-fix': 'logic:v3',
    'logic-refactor': 'refactor:v5',
  });
});

test('all seven explicit settings fail with a key-only diagnostic, even when shadowed', () => {
  for (const key of keys) {
    for (const value of ['', undefined, null, 'private content\ninvalid']) {
      assert.throws(() => createModelRoutes({ [key]: value }), {
        message: `${key} must be a non-empty Ollama model name (for example qwen2.5-coder:1.5b).`,
      });
    }
  }
  assert.throws(() => createModelRoutes({
    NEXUS_SUPERVISOR_MODEL: '', NEXUS_MODEL_COMMIT_MSG: 'valid',
    NEXUS_MODEL_BOILERPLATE: 'valid', NEXUS_MODEL_TEST_SCAFFOLD: 'valid',
  }), /NEXUS_SUPERVISOR_MODEL/);
});

test('literal settings preserve environment precedence and reject an explicit empty override', t => {
  const home = isolatedHome(t);
  writeFileSync(join(home, '.env'), 'NEXUS_SUPERVISOR_MODEL="file:v1"\nNEXUS_LOGIC_MODEL="logic:v2"\n');
  assert.equal(createModelRoutes(loadSettings({ NEXUS_REPO: home })).boilerplate, 'file:v1');
  assert.equal(createModelRoutes(loadSettings({
    NEXUS_REPO: home, NEXUS_SUPERVISOR_MODEL: 'env:v3',
  })).boilerplate, 'env:v3');
  assert.throws(() => createModelRoutes(loadSettings({
    NEXUS_REPO: home, NEXUS_SUPERVISOR_MODEL: '',
  })), /NEXUS_SUPERVISOR_MODEL/);
});

test('actual MCP startup rejects bad overrides before creating storage or writing protocol stdout', t => {
  const home = isolatedHome(t);
  const env = { ...process.env, HOME: home, NEXUS_REPO: home };
  for (const key of keys) delete env[key];
  const server = fileURLToPath(new URL('./server.mjs', import.meta.url));
  const check = (key, override = {}) => {
    const result = spawnSync(process.execPath, [server], {
      env: { ...env, ...override }, encoding: 'utf8', timeout: 5000,
    });
    assert.ifError(result.error);
    assert.equal(result.status, 1);
    assert.equal(result.stdout, '');
    assert.equal(result.stderr.trim(), `NEXUS model configuration: ${key} must be a non-empty Ollama model name (for example qwen2.5-coder:1.5b).`);
    assert.equal(existsSync(join(home, '.config')), false);
  };
  for (const key of keys) check(key, { [key]: '' });
  check('NEXUS_LOGIC_MODEL', { NEXUS_LOGIC_MODEL: 'private path\ninvalid' });
  writeFileSync(join(home, '.env'), 'NEXUS_MODEL_BOILERPLATE="$(echo private)"\n');
  check('NEXUS_MODEL_BOILERPLATE');
});

test('valid overrides still allow an actual MCP initialize handshake', t => {
  const home = isolatedHome(t);
  const env = { ...process.env, HOME: home, NEXUS_REPO: home };
  for (const key of keys) delete env[key];
  env.NEXUS_SUPERVISOR_MODEL = 'team/supervisor:v1';
  env.NEXUS_LOGIC_MODEL = 'registry.example:5000/team/logic:v2';
  const result = spawnSync(process.execPath, [fileURLToPath(new URL('./server.mjs', import.meta.url))], {
    env, encoding: 'utf8', timeout: 5000,
    input: JSON.stringify({
      jsonrpc: '2.0', id: 1, method: 'initialize',
      params: {
        protocolVersion: '2025-03-26', capabilities: {},
        clientInfo: { name: 'nexus-model-test', version: '1.0.0' },
      },
    }) + '\n',
  });
  assert.ifError(result.error);
  assert.equal(result.status, 0, result.stderr);
  const reply = JSON.parse(result.stdout.trim());
  assert.equal(reply.id, 1);
  assert.equal(reply.result.serverInfo.name, 'nexus-ollama');
  assert.equal(reply.error, undefined);
  assert.equal(existsSync(join(home, '.config/nexus/logs/observability.sqlite')), true);
});
