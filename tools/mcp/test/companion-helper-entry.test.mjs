import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdtempSync, rmSync, symlinkSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import test from 'node:test';

function fixture(t) {
  const home = mkdtempSync(join(tmpdir(), 'companion-helper-entry-'));
  t.after(() => rmSync(home, { recursive: true, force: true }));
  const alias = join(home, 'aliased mcp');
  symlinkSync(resolve(import.meta.dirname, '..'), alias, 'dir');
  return { home, script: join(alias, 'companion-data.mjs'), env: { ...process.env, HOME: home, NODE_NO_WARNINGS: '1' } };
}

test('helper runs as a CLI through a symlinked parent directory', (t) => {
  const f = fixture(t);
  const result = spawnSync(process.execPath, [f.script, 'status'], { env: f.env, encoding: 'utf8', timeout: 10000 });
  assert.equal(result.error, undefined);
  assert.equal(result.status, 1);
  assert.equal(result.stderr, '');
  assert.deepEqual(JSON.parse(result.stdout), { schemaVersion: 1, ok: false, error: 'companion_store_unavailable' });
  assert.equal(existsSync(join(f.home, '.config')), false);
});

test('importing helper through a symlink does not run the CLI', (t) => {
  const f = fixture(t);
  const result = spawnSync(process.execPath, ['--input-type=module', '-e', `
    import { runCompanionDataCLI } from ${JSON.stringify(pathToFileURL(f.script).href)};
    if (typeof runCompanionDataCLI !== 'function') throw new Error('missing API');
    process.stdout.write('imported');
  `], { env: f.env, encoding: 'utf8', timeout: 10000 });
  assert.equal(result.error, undefined);
  assert.equal(result.status, 0);
  assert.equal(result.stderr, '');
  assert.equal(result.stdout, 'imported');
  assert.equal(existsSync(join(f.home, '.config')), false);
});
