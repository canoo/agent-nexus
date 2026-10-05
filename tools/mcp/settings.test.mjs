import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { execFileSync } from 'node:child_process';
import { parseSettings, loadSettings, ollamaFetch } from './settings.mjs';

test('shell and MCP agree on literal values, duplicates and process overrides', () => {
 const dir = mkdtempSync(join(tmpdir(), 'nexus-settings-'));
 try {
  const text = "# comment\n NEXUS_SUPERVISOR_MODEL = 'first'\r\nNEXUS_SUPERVISOR_MODEL=last\nNEXUS_LOGIC_MODEL='$(echo never-executed)'\nOLLAMA_HOST_URL=http://file:11434\nPATH=/invalid\n";
  writeFileSync(join(dir,'.env'), text);
  assert.equal(parseSettings(text).NEXUS_SUPERVISOR_MODEL, 'last');
  const environment = { PATH: process.env.PATH, NEXUS_REPO: dir, OLLAMA_HOST_URL:'http://override:11434' };
  const settings = loadSettings(environment);
  const output = execFileSync('bash', ['-c', 'source "$1"; _nexus_load_settings "$2"; printf "%s\\n" "$NEXUS_SUPERVISOR_MODEL" "$NEXUS_LOGIC_MODEL" "$OLLAMA_HOST_URL" "$PATH"', 'test', new URL('../automation/settings.sh', import.meta.url).pathname, join(dir,'.env')], {env:environment,encoding:'utf8'}).trimEnd().split('\n');
  assert.deepEqual(output,[settings.NEXUS_SUPERVISOR_MODEL,settings.NEXUS_LOGIC_MODEL,settings.OLLAMA_HOST_URL,process.env.PATH]);
  assert.equal(settings.NEXUS_LOGIC_MODEL,'$(echo never-executed)');
  assert.equal(loadSettings({...environment,NEXUS_SUPERVISOR_MODEL:''}).NEXUS_SUPERVISOR_MODEL,'');
 } finally {rmSync(dir,{recursive:true,force:true});}
});

test('missing settings use environment; unreadable data fails instead of silently ignoring', () => {
 const dir = mkdtempSync(join(tmpdir(),'nexus-settings-'));
 try {
  assert.equal(loadSettings({NEXUS_REPO:dir,NEXUS_LOGIC_MODEL:'env'}).NEXUS_LOGIC_MODEL,'env');
  assert.throws(() => loadSettings({NEXUS_REPO:'/dev/null'}));
 } finally {rmSync(dir,{recursive:true,force:true});}
});


test('disabled local AI never invokes the HTTP adapter and shell refuses before curl', () => {
 const original = globalThis.fetch;
 let calls = 0;
 globalThis.fetch = () => { calls++; throw new Error('unexpected network'); };
 try {
  assert.throws(() => ollamaFetch({NEXUS_LOCAL_AI:'false'}, 'http://invalid'), /LOCAL_AI_DISABLED/);
  assert.equal(calls, 0);
 } finally { globalThis.fetch = original; }
 try {
  execFileSync('bash', [new URL('../automation/ollama-delegate.sh', import.meta.url).pathname, 'commit-msg', '/missing-fixture'], {env:{PATH:process.env.PATH,NEXUS_REPO:'/missing-fixture',NEXUS_LOCAL_AI:'false'},stdio:'pipe'});
  assert.fail('delegation unexpectedly succeeded');
 } catch (error) {
  assert.equal(error.status,3);
  assert.match(error.stderr.toString(), /LOCAL_AI_DISABLED/);
 }
});
