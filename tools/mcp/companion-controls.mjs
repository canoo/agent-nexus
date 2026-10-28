import { DatabaseSync } from 'node:sqlite';
import { lstatSync, realpathSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { DEFAULT_DATABASE_PATH } from './lib/observability-store.mjs';

export const COMPANION_ADAPTERS = Object.freeze(['browser-chrome', 'browser-edge']);
export const COMPANION_TOOLS = Object.freeze(['chatgpt', 'claude', 'gemini', 'copilot', 'perplexity']);
const POLICY = 1;
const CODES = new Set(['companion_command_invalid', 'companion_store_unavailable', 'companion_controls_unavailable', 'companion_consent_required']);
const fail = code => { throw new Error(code); };
function parse(args) {
  if (!Array.isArray(args) || !args.every(x => typeof x === 'string')) fail('companion_command_invalid');
  if (args.length === 1 && ['status', 'pause', 'resume'].includes(args[0])) return {action: args[0]};
  if (args.length === 2 && args[0] === 'disable' && args[1] === '--confirm') return {action: 'disable'};
  const [action, operation, adapterFlag, adapter, toolFlag, tool, ...ack] = args;
  if (action !== 'consent' || !['grant', 'revoke'].includes(operation) || adapterFlag !== '--adapter' || toolFlag !== '--tool'
      || !COMPANION_ADAPTERS.includes(adapter) || !COMPANION_TOOLS.includes(tool)
      || (operation === 'grant' ? ack.length !== 2 || ack[0] !== '--acknowledge-policy' || ack[1] !== String(POLICY) : ack.length !== 0)) fail('companion_command_invalid');
  return {action: 'consent', operation, adapter, tool};
}
function snapshot(db, allowInvalidBoundary = false) {
  const row = db.prepare('SELECT collection_enabled, collection_started_at, raw_span_retention_days FROM companion_settings WHERE id=1').get();
  if (!row || ![0,1].includes(row.collection_enabled) || !Number.isSafeInteger(row.raw_span_retention_days) || row.raw_span_retention_days < 0 || row.raw_span_retention_days > 365) fail('companion_controls_unavailable');
  if (row.collection_enabled === 1 && !allowInvalidBoundary) {
    const boundary = row.collection_started_at;
    const millis = typeof boundary === 'string' ? Date.parse(boundary) : NaN;
    if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,3})?Z$/.test(boundary)
        || !Number.isFinite(millis) || new Date(millis).toISOString().slice(0,19) !== boundary.slice(0,19)) fail('companion_controls_unavailable');
  }
  const consents = [];
  const query = db.prepare('SELECT enabled,consent_policy_version FROM companion_tool_consents WHERE adapter_id=? AND tool_id=?');
  for (const adapterId of COMPANION_ADAPTERS) for (const toolId of COMPANION_TOOLS) {
    const grant = query.get(adapterId,toolId);
    if (grant && (![0,1].includes(grant.enabled) || !Number.isSafeInteger(grant.consent_policy_version))) fail('companion_controls_unavailable');
    consents.push({adapterId,toolId,enabled:grant?.enabled === 1 && grant?.consent_policy_version === POLICY});
  }
  const storedSpans = db.prepare('SELECT COUNT(*) AS count FROM tool_activity').get().count;
  if (!Number.isSafeInteger(storedSpans) || storedSpans < 0) fail('companion_controls_unavailable');
  return {collectionEnabled: row.collection_enabled === 1, policyVersion: POLICY, retentionDays: row.raw_span_retention_days, storedSpans, consents};
}

// Controls require an already owned, current store. Reads never initialize,
// migrate, prune or modify it; mutations use one immediate transaction.
export function runCompanionControlsCLI(args, {databasePath = DEFAULT_DATABASE_PATH, out = process.stdout} = {}) {
  let db;
  let transaction = false;
  let command;
  let response;
  let code = 0;
  try {
    command = parse(args);
    let stat;
    try { stat = lstatSync(databasePath); } catch { fail('companion_store_unavailable'); }
    if (!stat.isFile() || stat.isSymbolicLink()) fail('companion_store_unavailable');
    db = new DatabaseSync(databasePath, {readOnly: command.action === 'status'});
    db.exec('PRAGMA busy_timeout = 5000');
    // Schema 005 owns the collection boundary. Refuse old stores without upgrading.
    const migrations = db.prepare('SELECT version FROM schema_migrations ORDER BY version').all().map(r => r.version);
    if (migrations.length !== 5 || migrations.some((v,i) => v !== i+1)) fail('companion_controls_unavailable');
    db.exec(command.action === 'status' ? 'BEGIN' : 'BEGIN IMMEDIATE');
    transaction = true;
    const before = snapshot(db, ['pause','disable','resume'].includes(command.action));
    if (command.action === 'resume' && !before.consents.some(c => c.enabled)) fail('companion_consent_required');
    if (command.action === 'consent') {
      db.prepare(`INSERT INTO companion_tool_consents(adapter_id,tool_id,enabled,consent_policy_version,updated_at)
        VALUES(?,?,?,?,strftime('%Y-%m-%dT%H:%M:%fZ','now')) ON CONFLICT(adapter_id,tool_id) DO UPDATE SET
        enabled=excluded.enabled,consent_policy_version=excluded.consent_policy_version,updated_at=excluded.updated_at`)
        .run(command.adapter,command.tool,command.operation === 'grant' ? 1 : 0,POLICY);
    } else if (['pause','resume','disable'].includes(command.action)) {
      const enabled = command.action === 'resume' ? 1 : 0;
      const result = db.prepare(`UPDATE companion_settings SET collection_enabled=?,
        collection_started_at=CASE WHEN ?=1 THEN strftime('%Y-%m-%dT%H:%M:%fZ','now') ELSE NULL END,
        updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=1`).run(enabled,enabled);
      if (result.changes !== 1) fail('companion_controls_unavailable');
      if (command.action === 'disable') db.exec(`UPDATE companion_tool_consents SET enabled=0,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')`);
    }
    response = {schemaVersion:1,ok:true,action:command.action,...snapshot(db)};
    db.exec('COMMIT');
    transaction = false;
  } catch (error) {
    if (transaction) { try { db.exec('ROLLBACK'); } catch {} }
    const safe = CODES.has(error?.message) ? error.message : 'companion_controls_unavailable';
    response = {schemaVersion:1,ok:false,error:safe};
    code = safe === 'companion_command_invalid' ? 2 : 1;
  } finally { try { db?.close(); } catch {} }
  try { out.write(JSON.stringify(response)+'\n'); } catch { return 1; }
  return code;
}

let entry = false;
try { entry = Boolean(process.argv[1]) && realpathSync(process.argv[1]) === realpathSync(fileURLToPath(import.meta.url)); } catch {}
if (entry) process.exitCode = runCompanionControlsCLI(process.argv.slice(2));
