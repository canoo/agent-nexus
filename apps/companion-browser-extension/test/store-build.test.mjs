import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { buildChromeExtension } from '../../../tools/automation/build-chrome-extension.mjs';

test('Chrome store archive is deterministic, has a root manifest, and preserves existing outputs', t => {
  const root = mkdtempSync(join(tmpdir(), 'nexus-store-build-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const version = '0.3.0-extension.1';
  const first = buildChromeExtension({ outputDir: join(root, 'first'), version });
  const second = buildChromeExtension({ outputDir: join(root, 'second'), version });
  assert.deepEqual(first, second);
  const archive = readFileSync(join(root, 'first', first.archive));
  assert.deepEqual(archive, readFileSync(join(root, 'second', second.archive)));
  let offset = 0;
  const names = [];
  while (archive.readUInt32LE(offset) === 0x04034b50) {
    const size = archive.readUInt32LE(offset + 18);
    const nameLength = archive.readUInt16LE(offset + 26);
    const name = archive.subarray(offset + 30, offset + 30 + nameLength).toString();
    const bytes = archive.subarray(offset + 30 + nameLength, offset + 30 + nameLength + size);
    assert.deepEqual(bytes, readFileSync(join(root, 'first', 'extension', name)));
    names.push(name);
    offset += 30 + nameLength + size;
  }
  assert.equal(names.length, first.files);
  assert.ok(names.includes('manifest.json'));
  assert.ok(names.includes('lib/control.js'));
  assert.equal(archive.readUInt32LE(offset), 0x02014b50);
  assert.equal(archive.readUInt32LE(archive.length - 22), 0x06054b50);
  assert.throws(() => buildChromeExtension({ outputDir: join(root, 'first'), version }));
  assert.deepEqual(archive, readFileSync(join(root, 'first', first.archive)));
});
