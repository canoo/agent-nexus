import test from 'node:test';
import assert from 'node:assert/strict';
import { copyFileSync, existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, realpathSync, rmSync, symlinkSync, unlinkSync, writeFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { tmpdir } from 'node:os';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { EXTENSION_PAYLOAD_FILES, stageBrowserExtension } from '../../../tools/automation/lib/companion-extension-package.mjs';

const sourceRoot = fileURLToPath(new URL('../../../', import.meta.url));
const version = '0.3.0-dev.4';
function temporary(t) {
  const root = realpathSync(mkdtempSync(join(tmpdir(), 'nexus-extension-package-')));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  return root;
}
function filesAt(root, prefix = '') {
  return readdirSync(join(root, prefix)).flatMap(name => {
    const path = prefix ? `${prefix}/${name}` : name;
    return lstatSync(join(root, path)).isDirectory() ? filesAt(root, path) : [path];
  }).sort();
}
function fixtureSource(root) {
  const source = join(root, 'source');
  for (const path of EXTENSION_PAYLOAD_FILES) {
    const target = join(source, 'apps/companion-browser-extension', path);
    mkdirSync(dirname(target), { recursive: true });
    copyFileSync(join(sourceRoot, 'apps/companion-browser-extension', path), target);
  }
  return source;
}

test('extension staging includes only fixed runtime files and preserves permission/source policy', t => {
  const root = temporary(t);
  const source = fixtureSource(root);
  writeFileSync(join(source, 'apps/companion-browser-extension/private-fixture.txt'), 'excluded fixture');
  const outputDir = join(root, 'unpacked with spaces');
  const result = stageBrowserExtension({ outputDir, version, sourceRoot: source });
  assert.deepEqual(result, { version, files: [...EXTENSION_PAYLOAD_FILES].sort() });
  assert.deepEqual(filesAt(outputDir), [...EXTENSION_PAYLOAD_FILES].sort());
  const original = JSON.parse(readFileSync(join(source, 'apps/companion-browser-extension/manifest.json')));
  const packaged = JSON.parse(readFileSync(join(outputDir, 'manifest.json')));
  assert.deepEqual(packaged, { ...original, version: '0.3.0', version_name: version });
  assert.equal(original.version_name, undefined);
  for (const path of EXTENSION_PAYLOAD_FILES) {
    assert.equal(lstatSync(join(outputDir, path)).mode & 0o777, 0o644);
    if (path !== 'manifest.json') assert.deepEqual(readFileSync(join(outputDir, path)), readFileSync(join(source, 'apps/companion-browser-extension', path)));
  }
  const again = join(root, 'again');
  stageBrowserExtension({ outputDir: again, version, sourceRoot: source });
  for (const path of EXTENSION_PAYLOAD_FILES) assert.deepEqual(readFileSync(join(outputDir, path)), readFileSync(join(again, path)));
});

test('staged extension module imports work after relocation without a source checkout', async t => {
  const root = temporary(t);
  const outputDir = join(root, 'extension');
  stageBrowserExtension({ outputDir, version, sourceRoot });
  const relocated = join(root, 'relocated');
  mkdirSync(relocated);
  for (const path of EXTENSION_PAYLOAD_FILES) {
    mkdirSync(dirname(join(relocated, path)), { recursive: true });
    copyFileSync(join(outputDir, path), join(relocated, path));
  }
  rmSync(outputDir, { recursive: true });
  const { toolIdForOrigin, originForUrl } = await import(pathToFileURL(join(relocated, 'lib/policy.js')));
  const { transitionSelectedTab } = await import(pathToFileURL(join(relocated, 'lib/activity.js')));
  assert.equal(toolIdForOrigin('https://claude.ai'), 'claude');
  assert.equal(originForUrl('https://chatgpt.com/c/private?account=hidden'), 'https://chatgpt.com');
  assert.equal(transitionSelectedTab({ activeSpans: {}, windowId: 1, origin: 'https://chatgpt.com', consents: {}, now: '2026-10-08T12:00:00Z', browserFamily: 'chrome', platform: 'linux' }).event, null);
});

test('invalid inputs, existing outputs and redirected output parents never overwrite files', t => {
  const root = temporary(t);
  const source = fixtureSource(root);
  const outputDir = join(root, 'output');
  for (const options of [null, [], {}, { outputDir, version: '0.3.0', sourceRoot: source }, { outputDir, version: version + '\n', sourceRoot: source }, { outputDir: 'relative', version, sourceRoot: source }, { outputDir, version, sourceRoot: source, unexpected: true }]) {
    assert.throws(() => stageBrowserExtension(options), /extension_input_invalid/);
    assert.equal(existsSync(outputDir), false);
  }
  mkdirSync(outputDir);
  writeFileSync(join(outputDir, 'preserve.txt'), 'unchanged');
  assert.throws(() => stageBrowserExtension({ outputDir, version, sourceRoot: source }), /extension_output_exists/);
  assert.equal(readFileSync(join(outputDir, 'preserve.txt'), 'utf8'), 'unchanged');
  const link = join(root, 'redirect');
  symlinkSync(source, link, 'dir');
  assert.throws(() => stageBrowserExtension({ outputDir: join(link, 'new'), version, sourceRoot: source }), /extension_input_invalid/);
  assert.equal(existsSync(join(source, 'new')), false);
  assert.throws(() => stageBrowserExtension({ outputDir: join(source, '..internal'), version, sourceRoot: source }), /extension_input_invalid/);
  const dangling = join(root, 'dangling');
  symlinkSync(join(root, 'missing'), dangling);
  assert.throws(() => stageBrowserExtension({ outputDir: dangling, version, sourceRoot: source }), /extension_output_exists/);
});

test('missing, malformed and redirected source files fail before output creation', t => {
  const root = temporary(t);
  const source = fixtureSource(root);
  const file = join(source, 'apps/companion-browser-extension/manifest.json');
  const original = readFileSync(file);
  const outputDir = join(root, 'output');
  writeFileSync(file, '{broken');
  assert.throws(() => stageBrowserExtension({ outputDir, version, sourceRoot: source }), /extension_source_invalid/);
  assert.equal(existsSync(outputDir), false);
  unlinkSync(file);
  symlinkSync(join(sourceRoot, 'apps/companion-browser-extension/manifest.json'), file);
  assert.throws(() => stageBrowserExtension({ outputDir, version, sourceRoot: source }), /extension_source_invalid/);
  assert.equal(existsSync(outputDir), false);
  unlinkSync(file);
  writeFileSync(file, original);
  rmSync(join(source, 'apps/companion-browser-extension/lib'), { recursive: true });
  symlinkSync(join(sourceRoot, 'apps/companion-browser-extension/lib'), join(source, 'apps/companion-browser-extension/lib'), 'dir');
  assert.throws(() => stageBrowserExtension({ outputDir, version, sourceRoot: source }), /extension_source_invalid/);
  assert.equal(existsSync(outputDir), false);
});
