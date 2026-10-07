import { DatabaseSync } from 'node:sqlite';
import { existsSync, statSync } from 'node:fs';
import { resolve } from 'node:path';
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
    code === 'companion_retention_invalid' ||
    code === 'companion_clock_invalid' ||
    code === 'companion_data_unavailable'
  ) {
    return code;
  }
  return 'companion_data_unavailable';
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

  if (args.length === 1 && args[0] === 'status') {
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

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  process.exitCode = runCompanionDataCLI(process.argv.slice(2));
}
