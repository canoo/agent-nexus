import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

export const EXTENSION_PAYLOAD_FILES = Object.freeze([
  'manifest.json',
  'background.js',
  'popup.html',
  'popup.js',
  'options.html',
  'options.js',
  'ui.js',
  'ui.css',
  'lib/activity.js',
  'lib/policy.js',
  'lib/consent.js',
  'lib/event-queue.js',
  'README.md'
]);

const EXTENSION_REL_BASE = 'apps/companion-browser-extension';
const VERSION_REGEX = /^0\.3\.0-[a-zA-Z-][a-zA-Z0-9-]*(?:\.(?:0|[1-9]\d*))*$/;

function validateComponentsNoSymlink(basePath, relativePath) {
  const parts = relativePath.split(/[/\\]+/).filter(Boolean);
  let current = basePath;

  const rootStat = fs.lstatSync(current, { throwIfNoEntry: false });
  if (!rootStat || !rootStat.isDirectory() || rootStat.isSymbolicLink()) {
    throw new Error('extension_source_invalid');
  }

  for (let i = 0; i < parts.length; i++) {
    current = path.join(current, parts[i]);
    const st = fs.lstatSync(current, { throwIfNoEntry: false });
    if (!st || st.isSymbolicLink()) {
      throw new Error('extension_source_invalid');
    }
    const isLast = i === parts.length - 1;
    if (isLast) {
      if (!st.isFile()) {
        throw new Error('extension_source_invalid');
      }
    } else {
      if (!st.isDirectory()) {
        throw new Error('extension_source_invalid');
      }
    }
  }
}

export function stageBrowserExtension(options = {}) {
  if (!options || typeof options !== 'object' || Array.isArray(options)) {
    throw new Error('extension_input_invalid');
  }
  const allowedKeys = new Set(['outputDir', 'version', 'sourceRoot']);
  const optionKeys = Object.keys(options);
  for (let i = 0; i < optionKeys.length; i++) {
    if (!allowedKeys.has(optionKeys[i])) {
      throw new Error('extension_input_invalid');
    }
  }

  const {
    outputDir,
    version,
    sourceRoot = fileURLToPath(new URL('../../../', import.meta.url))
  } = options;

  if (
    typeof outputDir !== 'string' ||
    !path.isAbsolute(outputDir) ||
    typeof sourceRoot !== 'string' ||
    !path.isAbsolute(sourceRoot) ||
    typeof version !== 'string' ||
    VERSION_REGEX.exec(version)?.[0] !== version
  ) {
    throw new Error('extension_input_invalid');
  }

  let normalizedOutput = path.resolve(outputDir);
  const normalizedSourceRoot = path.resolve(sourceRoot);

  try {
    const outStat = fs.lstatSync(normalizedOutput, { throwIfNoEntry: false });
    if (outStat !== undefined) {
      throw new Error('extension_output_exists');
    }
  } catch (err) {
    if (err.message === 'extension_output_exists') {
      throw err;
    }
    throw new Error('extension_write_failed');
  }

  let canonicalRoot;
  try { canonicalRoot = fs.realpathSync(normalizedSourceRoot); }
  catch { throw new Error('extension_source_invalid'); }
  try {
    normalizedOutput = path.join(fs.realpathSync(path.dirname(normalizedOutput)), path.basename(normalizedOutput));
  } catch { throw new Error('extension_input_invalid'); }
  const relFromRoot = path.relative(canonicalRoot, normalizedOutput);
  if (!relFromRoot || (!relFromRoot.startsWith('../') && !path.isAbsolute(relFromRoot))) {
    throw new Error('extension_input_invalid');
  }

  const fileBuffers = new Map();

  try {
    const extBaseStat = fs.lstatSync(path.join(normalizedSourceRoot, EXTENSION_REL_BASE), { throwIfNoEntry: false });
    if (!extBaseStat || !extBaseStat.isDirectory() || extBaseStat.isSymbolicLink()) {
      throw new Error('extension_source_invalid');
    }

    for (const relFile of EXTENSION_PAYLOAD_FILES) {
      const fullRelPath = path.join(EXTENSION_REL_BASE, relFile);
      validateComponentsNoSymlink(normalizedSourceRoot, fullRelPath);

      const filePath = path.join(normalizedSourceRoot, fullRelPath);
      let content = fs.readFileSync(filePath);

      if (relFile === 'manifest.json') {
        let parsed;
        try {
          parsed = JSON.parse(content.toString('utf8'));
        } catch {
          throw new Error('extension_source_invalid');
        }
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
          throw new Error('extension_source_invalid');
        }
        parsed.version = '0.3.0';
        parsed.version_name = version;
        content = Buffer.from(JSON.stringify(parsed, null, 2) + '\n', 'utf8');
      }

      fileBuffers.set(relFile, content);
    }
  } catch (err) {
    if (err.message === 'extension_source_invalid') {
      throw err;
    }
    throw new Error('extension_source_invalid');
  }

  try {
    fs.mkdirSync(normalizedOutput, { mode: 0o755 });
    fs.chmodSync(normalizedOutput, 0o755);
  } catch (err) {
    if (err.code === 'EEXIST') {
      throw new Error('extension_output_exists');
    }
    throw new Error('extension_write_failed');
  }

  try {
    for (const relFile of EXTENSION_PAYLOAD_FILES) {
      const targetFilePath = path.join(normalizedOutput, relFile);
      const targetDirPath = path.dirname(targetFilePath);

      if (targetDirPath !== normalizedOutput && !fs.existsSync(targetDirPath)) {
        fs.mkdirSync(targetDirPath, { recursive: true, mode: 0o755 });
        fs.chmodSync(targetDirPath, 0o755);
      }

      const content = fileBuffers.get(relFile);
      fs.writeFileSync(targetFilePath, content, { mode: 0o644, flag: 'wx' });
      fs.chmodSync(targetFilePath, 0o644);
    }
  } catch {
    throw new Error('extension_write_failed');
  }

  const sortedFiles = [...EXTENSION_PAYLOAD_FILES].sort();

  return {
    version,
    files: sortedFiles
  };
}
