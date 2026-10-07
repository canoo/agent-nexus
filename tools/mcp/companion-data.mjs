import { DatabaseSync } from 'node:sqlite';
import { closeSync, existsSync, lstatSync, mkdirSync, openSync, realpathSync, statSync } from 'node:fs';
import { dirname, isAbsolute, parse, relative } from 'node:path';
import { homedir } from 'node:os';
import { fileURLToPath } from 'node:url';
import { createObservabilityStore, DEFAULT_DATABASE_PATH } from './lib/observability-store.mjs';

function emitJson(out, payload) {
  try {
    out.write(JSON.stringify(payload) + '\n');
    return true;
  } catch {
    return false;
  }
}

function normalizeError(err) {
  const code = err && typeof err === 'object' ? (err.code ?? err.message) : '';
  if (
    code === 'companion_store_unavailable' ||
    code === 'companion_setup_unavailable' ||
    code === 'companion_store_exists' ||
    code === 'companion_retention_invalid' ||
    code === 'companion_clock_invalid' ||
    code === 'companion_data_unavailable'
  ) {
    return code;
  }
  return 'companion_data_unavailable';
}


function setupFailure(code = 'companion_setup_unavailable') {
  const error = new Error(code);
  error.code = code;
  return error;
}

// Create missing directories only. Never follow a redirected store directory.
function setupDirectory(path, anchor) {
  if (path !== anchor) setupDirectory(dirname(path), anchor);
  try {
    const stat = lstatSync(path);
    if (!stat.isDirectory() || stat.isSymbolicLink()) throw setupFailure();
  } catch (error) {
    if (error.code !== 'ENOENT') throw setupFailure();
    try { mkdirSync(path, { mode: 0o700 }); }
    catch (error) {
      if (error.code !== 'EEXIST') throw setupFailure();
      const stat = lstatSync(path);
      if (!stat.isDirectory() || stat.isSymbolicLink()) throw setupFailure();
    }
  }
}

function initializeStore(databasePath) {
  if (typeof databasePath !== 'string' || !isAbsolute(databasePath)
      || !['linux', 'darwin'].includes(process.platform)
      || process.env.FLATPAK_ID || existsSync('/.flatpak-info')) throw setupFailure();
  for (const path of [databasePath, ...['-wal', '-shm', '-journal'].map((suffix) => databasePath + suffix)]) {
    try {
      lstatSync(path);
      throw setupFailure('companion_store_exists');
    } catch (error) {
      if (error.code !== 'ENOENT') throw error.code === 'companion_store_exists' ? error : setupFailure();
    }
  }
  const homeRelative = relative(homedir(), databasePath);
  const anchor = homeRelative && !homeRelative.startsWith('..') && !isAbsolute(homeRelative) ? homedir() : parse(databasePath).root;
  setupDirectory(dirname(databasePath), anchor);
  try { closeSync(openSync(databasePath, 'wx', 0o600)); }
  catch (error) { throw setupFailure(error.code === 'EEXIST' ? 'companion_store_exists' : undefined); }
  // This exclusively reserved file is the only file we migrate. Never remove it
  // on failure: another process may have opened it after reservation.
  const store = createObservabilityStore({ databasePath });
  if (store.databasePath !== databasePath) throw setupFailure();
  store.migrate();
  const database = new DatabaseSync(databasePath, { readOnly: true });
  try {
    const settings = database.prepare('SELECT collection_enabled, collection_started_at, raw_span_retention_days FROM companion_settings WHERE id=1').get();
    const consent = database.prepare('SELECT COUNT(*) AS n FROM companion_tool_consents WHERE enabled=1').get();
    const activity = database.prepare('SELECT COUNT(*) AS n FROM tool_activity').get();
    if (!settings || settings.collection_enabled !== 0 || settings.collection_started_at !== null
        || settings.raw_span_retention_days !== 14 || consent.n !== 0 || activity.n !== 0) throw setupFailure();
    return { retentionDays: 14, storedSpans: 0 };
  } finally { database.close(); }
}

export function runCompanionDataCLI(
  args,
  {
    databasePath = DEFAULT_DATABASE_PATH,
    out = process.stdout,
    store,
  } = {}
) {
  if (!Array.isArray(args) || !args.every((arg) => typeof arg === 'string')) {
    emitJson(out, { schemaVersion: 1, ok: false, error: 'companion_command_invalid' });
    return 2;
  }

  let action = null;
  let retentionArg = null;

  if (args.length === 2 && args[0] === 'initialize' && args[1] === '--confirm') {
    action = 'initialize';
  } else if (args.length === 1 && args[0] === 'status') {
    action = 'status';
  } else if (args.length === 1 && args[0] === 'prune') {
    action = 'prune';
  } else if (args.length === 3 && args[0] === 'retention' && args[1] === '--days') {
    if (/^\d+$/.test(args[2])) {
      const parsedDays = Number(args[2]);
      if (Number.isSafeInteger(parsedDays) && parsedDays >= 0 && parsedDays <= 365) {
        action = 'retention';
        retentionArg = parsedDays;
      }
    }
  } else if (args.length === 2 && args[0] === 'clear' && args[1] === '--confirm') {
    action = 'clear';
  }

  if (!action) {
    emitJson(out, { schemaVersion: 1, ok: false, error: 'companion_command_invalid' });
    return 2;
  }

  try {
    if (action === 'initialize') {
      const result = initializeStore(databasePath);
      return emitJson(out, { schemaVersion: 1, ok: true, action, ...result }) ? 0 : 1;
    }
    if (action === 'status') {
      if (!existsSync(databasePath)) {
        const err = new Error('Database file does not exist');
        err.code = 'companion_store_unavailable';
        throw err;
      }
      const stat = statSync(databasePath);
      if (!stat.isFile()) {
        const err = new Error('Database path is not a regular file');
        err.code = 'companion_store_unavailable';
        throw err;
      }

      let db;
      try {
        db = new DatabaseSync(databasePath, { readOnly: true });
      } catch (e) {
        const err = new Error('Failed to open database');
        err.code = 'companion_store_unavailable';
        throw err;
      }

      let retentionDays;
      let storedSpans;

      try {
        const settingsStmt = db.prepare(
          'SELECT raw_span_retention_days FROM companion_settings WHERE id = 1'
        );
        const settingsRow = settingsStmt.get();
        if (!settingsRow || typeof settingsRow.raw_span_retention_days !== 'number') {
          const err = new Error('Invalid retention settings');
          err.code = 'companion_data_unavailable';
          throw err;
        }
        retentionDays = settingsRow.raw_span_retention_days;
        if (!Number.isSafeInteger(retentionDays) || retentionDays < 0 || retentionDays > 365) {
          const err = new Error('Retention days out of bounds');
          err.code = 'companion_retention_invalid';
          throw err;
        }

        const countStmt = db.prepare('SELECT COUNT(*) AS count FROM tool_activity');
        const countRow = countStmt.get();
        if (!countRow || typeof countRow.count !== 'number') {
          const err = new Error('Invalid stored spans count');
          err.code = 'companion_data_unavailable';
          throw err;
        }
        storedSpans = countRow.count;
        if (!Number.isSafeInteger(storedSpans) || storedSpans < 0) {
          const err = new Error('Stored spans count invalid');
          err.code = 'companion_data_unavailable';
          throw err;
        }
      } catch (err) {
        if (
          err &&
          typeof err === 'object' &&
          (err.code === 'companion_retention_invalid' ||
            err.code === 'companion_data_unavailable' ||
            err.code === 'companion_store_unavailable' ||
            err.code === 'companion_clock_invalid')
        ) {
          throw err;
        }
        const wrapped = new Error('Query error');
        wrapped.code = 'companion_data_unavailable';
        throw wrapped;
      } finally {
        try {
          db.close();
        } catch {
          // ignore close errors
        }
      }

      const written = emitJson(out, {
        schemaVersion: 1,
        ok: true,
        action: 'status',
        retentionDays,
        storedSpans,
      });
      return written ? 0 : 1;
    }

    const activeStore = store !== undefined ? store : createObservabilityStore({ databasePath });

    let result;
    if (action === 'prune') {
      result = activeStore.pruneToolActivity();
    } else if (action === 'retention') {
      result = activeStore.setCompanionRetentionDays(retentionArg);
    } else if (action === 'clear') {
      result = activeStore.clearToolActivity();
    }

    if (
      !result ||
      typeof result !== 'object' ||
      !Number.isSafeInteger(result.deleted) ||
      result.deleted < 0 ||
      !Number.isSafeInteger(result.retentionDays) ||
      result.retentionDays < 0 ||
      result.retentionDays > 365
    ) {
      const err = new Error('Invalid store result summary');
      err.code = 'companion_data_unavailable';
      throw err;
    }

    const written = emitJson(out, {
      schemaVersion: 1,
      ok: true,
      action,
      deleted: result.deleted,
      retentionDays: result.retentionDays,
    });
    return written ? 0 : 1;
  } catch (err) {
    const errorCode = normalizeError(err);
    emitJson(out, {schemaVersion: 1, ok: false, error: errorCode});
    return 1;
  }
}

function isEntryPoint() {
  if (!process.argv[1]) return false;
  try {
    return realpathSync(process.argv[1]) === realpathSync(fileURLToPath(import.meta.url));
  } catch {
    return false;
  }
}

if (isEntryPoint()) {
  process.exitCode = runCompanionDataCLI(process.argv.slice(2));
}
