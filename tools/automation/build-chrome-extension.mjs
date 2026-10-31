#!/usr/bin/env node
// Build only the Chrome extension. No desktop build, registration or publishing.
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { isAbsolute, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
import { stageBrowserExtension } from './lib/companion-extension-package.mjs';

function crc32(bytes) {
  let crc = 0xffffffff;
  for (const byte of bytes) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ ((crc & 1) ? 0xedb88320 : 0);
  }
  return (crc ^ 0xffffffff) >>> 0;
}

// Deterministic ZIP using stored entries: no shell, archive dependency or host paths.
function zipEntries(entries) {
  const local = [], central = [];
  let offset = 0, centralSize = 0;
  for (const { name, bytes } of entries) {
    const filename = Buffer.from(name);
    const checksum = crc32(bytes);
    const header = Buffer.alloc(30);
    header.writeUInt32LE(0x04034b50, 0);
    header.writeUInt16LE(20, 4);
    header.writeUInt16LE(0x800, 6); // UTF-8
    header.writeUInt16LE(33, 12); // 1980-01-01
    header.writeUInt32LE(checksum, 14);
    header.writeUInt32LE(bytes.length, 18);
    header.writeUInt32LE(bytes.length, 22);
    header.writeUInt16LE(filename.length, 26);
    local.push(header, filename, bytes);
    const record = Buffer.alloc(46);
    record.writeUInt32LE(0x02014b50, 0);
    record.writeUInt16LE(20, 4);
    record.writeUInt16LE(20, 6);
    record.writeUInt16LE(0x800, 8);
    record.writeUInt16LE(33, 14);
    record.writeUInt32LE(checksum, 16);
    record.writeUInt32LE(bytes.length, 20);
    record.writeUInt32LE(bytes.length, 24);
    record.writeUInt16LE(filename.length, 28);
    record.writeUInt32LE(offset, 42);
    central.push(record, filename);
    offset += header.length + filename.length + bytes.length;
    centralSize += record.length + filename.length;
  }
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(entries.length, 8);
  end.writeUInt16LE(entries.length, 10);
  end.writeUInt32LE(centralSize, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...local, ...central, end]);
}

export function buildChromeExtension({ outputDir, version }) {
  if (typeof outputDir !== 'string' || !isAbsolute(outputDir)
      || typeof version !== 'string' || !/^0\.3\.0-[a-zA-Z-][a-zA-Z0-9-]*(?:\.(?:0|[1-9]\d*))*$/.test(version)) {
    throw new Error('extension_input_invalid');
  }
  // Exclusive creation preserves all existing artifacts.
  mkdirSync(outputDir, { mode: 0o755 });
  const unpacked = join(outputDir, 'extension');
  const { files } = stageBrowserExtension({ outputDir: unpacked, version });
  const bytes = zipEntries(files.map(name => ({ name, bytes: readFileSync(join(unpacked, name)) })));
  const archive = `nexus-chrome-${version}.zip`;
  writeFileSync(join(outputDir, archive), bytes, { flag: 'wx', mode: 0o644 });
  const result = { version, archive, sha256: createHash('sha256').update(bytes).digest('hex'), files: files.length, published: false, liveGuiVerified: false };
  writeFileSync(join(outputDir, 'build.json'), JSON.stringify(result, null, 2) + '\n', { flag: 'wx' });
  return result;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const args = process.argv.slice(2);
    if (args.length !== 4 || args[0] !== '--version' || args[2] !== '--output') throw new Error('extension_input_invalid');
    console.log(JSON.stringify(buildChromeExtension({ version: args[1], outputDir: args[3] })));
  } catch {
    console.error('Chrome extension build failed. Use a valid prerelease version and a new absolute output directory outside the checkout.');
    process.exitCode = 1;
  }
}
