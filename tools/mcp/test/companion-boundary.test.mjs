import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { DatabaseSync } from 'node:sqlite';
import { createObservabilityStore } from '../lib/observability-store.mjs';

const FIXED_NOW = Date.parse('2026-10-07T12:00:00.000Z');

function fixture(t) {
  const dir = mkdtempSync(join(tmpdir(), 'nexus-boundary-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const databasePath = join(dir, 'logs', 'observability.sqlite');
  const store = createObservabilityStore({
    databasePath,
    jsonlPath: join(dir, 'legacy.jsonl'),
    now: () => FIXED_NOW
  });
  assert.equal(store.databasePath, databasePath);
  store.migrate();
  return { store, databasePath };
}

function db(path, run) {
  const conn = new DatabaseSync(path);
  try {
    return run(conn);
  } finally {
    conn.close();
  }
}

function initSettingsAndConsent(path, {
  collectionEnabled = 1,
  collectionStartedAt = '2026-10-07T11:30:00.000Z',
  consentEnabled = 1,
  consentPolicyVersion = 1,
  consentUpdatedAt = '2026-10-07T11:30:00.000Z'
} = {}) {
  db(path, (conn) => {
    conn.prepare(`
      UPDATE companion_settings
      SET collection_enabled = ?, collection_started_at = ?
      WHERE id = 1
    `).run(collectionEnabled, collectionStartedAt);

    if (consentEnabled !== null) {
      conn.prepare(`
        INSERT INTO companion_tool_consents (adapter_id, tool_id, enabled, consent_policy_version, updated_at)
        VALUES ('browser-chrome', 'chatgpt', ?, ?, ?)
        ON CONFLICT(adapter_id, tool_id) DO UPDATE SET
          enabled = excluded.enabled,
          consent_policy_version = excluded.consent_policy_version,
          updated_at = excluded.updated_at
      `).run(consentEnabled, consentPolicyVersion, consentUpdatedAt);
    }
  });
}

function activity(overrides = {}) {
  return {
    tool_id: 'chatgpt',
    surface: 'browser',
    started_at: '2026-10-07T11:30:00.000Z',
    ended_at: '2026-10-07T11:45:00.000Z',
    detector: 'selected-browser-tab',
    confidence: 'surface-active',
    browser_family: 'chrome',
    platform: 'linux',
    schema_version: 1,
    consent_policy_version: 1,
    ...overrides
  };
}

function countRows(path) {
  return db(path, (conn) => Number(conn.prepare('SELECT COUNT(*) AS count FROM tool_activity').get().count));
}

test('crossing latest resume returns companion_activity_crosses_boundary noinsert', (t) => {
  const { store, databasePath } = fixture(t);
  initSettingsAndConsent(databasePath, {
    collectionEnabled: 1,
    collectionStartedAt: '2026-10-07T11:30:00.000Z'
  });

  const span = activity({
    started_at: '2026-10-07T11:00:00.000Z',
    ended_at: '2026-10-07T11:59:00.000Z'
  });

  const res = store.recordToolActivity(span);
  assert.equal(res.sqlite.ok, false);
  assert.equal(res.sqlite.error, 'companion_activity_crosses_boundary');
  assert.equal(countRows(databasePath), 0);
});

test('start EXACT 11:30 succeeds', (t) => {
  const { store, databasePath } = fixture(t);
  initSettingsAndConsent(databasePath, {
    collectionEnabled: 1,
    collectionStartedAt: '2026-10-07T11:30:00.000Z'
  });

  const span = activity({
    started_at: '2026-10-07T11:30:00.000Z',
    ended_at: '2026-10-07T11:45:00.000Z'
  });

  const res = store.recordToolActivity(span);
  assert.equal(res.sqlite.ok, true);
  assert.equal(typeof res.id, 'string');
  assert.equal(countRows(databasePath), 1);
});

test('initial collection grant gate start before collection boundary rejected', (t) => {
  const { store, databasePath } = fixture(t);
  initSettingsAndConsent(databasePath, {
    collectionEnabled: 1,
    collectionStartedAt: '2026-10-07T11:30:00.000Z'
  });

  const span = activity({
    started_at: '2026-10-07T11:29:59.999Z',
    ended_at: '2026-10-07T11:40:00.000Z'
  });

  const res = store.recordToolActivity(span);
  assert.equal(res.sqlite.ok, false);
  assert.equal(res.sqlite.error, 'companion_activity_crosses_boundary');
  assert.equal(countRows(databasePath), 0);
});

test('repeated pause/resume simulated collection_enabled=0 rejects while paused then resume 11:45 rejects queued old span', (t) => {
  const { store, databasePath } = fixture(t);
  initSettingsAndConsent(databasePath, {
    collectionEnabled: 0,
    collectionStartedAt: null
  });

  const span = activity({
    started_at: '2026-10-07T11:35:00.000Z',
    ended_at: '2026-10-07T11:40:00.000Z'
  });

  const pausedRes = store.recordToolActivity(span);
  assert.equal(pausedRes.sqlite.ok, false);

  // Resume at 11:45
  db(databasePath, (conn) => {
    conn.prepare(`
      UPDATE companion_settings
      SET collection_enabled = 1, collection_started_at = '2026-10-07T11:45:00.000Z'
      WHERE id = 1
    `).run();
  });

  const resumedRes = store.recordToolActivity(span);
  assert.equal(resumedRes.sqlite.ok, false);
  assert.equal(resumedRes.sqlite.error, 'companion_activity_crosses_boundary');
  assert.equal(countRows(databasePath), 0);
});

test('revoke -> regrant updated_at 11:50 rejects 11:46 start, exact 11:50 succeeds', (t) => {
  const { store, databasePath } = fixture(t);
  initSettingsAndConsent(databasePath, {
    collectionEnabled: 1,
    collectionStartedAt: '2026-10-07T11:30:00.000Z',
    consentEnabled: 1,
    consentUpdatedAt: '2026-10-07T11:50:00.000Z'
  });

  const rejectedSpan = activity({
    started_at: '2026-10-07T11:46:00.000Z',
    ended_at: '2026-10-07T11:55:00.000Z'
  });
  const res1 = store.recordToolActivity(rejectedSpan);
  assert.equal(res1.sqlite.ok, false);
  assert.equal(res1.sqlite.error, 'companion_activity_crosses_boundary');

  const exactSpan = activity({
    started_at: '2026-10-07T11:50:00.000Z',
    ended_at: '2026-10-07T11:55:00.000Z'
  });
  const res2 = store.recordToolActivity(exactSpan);
  assert.equal(res2.sqlite.ok, true);
  assert.equal(countRows(databasePath), 1);
});

test('invalid or missing boundaries fail closed without storing a span', (t) => {
  const { store, databasePath } = fixture(t);
  for (const options of [
    {collectionStartedAt: null},
    {collectionStartedAt: 'not-a-valid-timestamp'},
    {collectionStartedAt: '2026-02-30T11:30:00Z'},
    {consentUpdatedAt: 'private SQL/path'},
    {consentUpdatedAt: '2026-02-30 11:30:00'},
  ]) {
    initSettingsAndConsent(databasePath, options);
    const result = store.recordToolActivity(activity());
    assert.equal(result.sqlite.error, 'companion_activity_boundary_unavailable');
    assert.equal(countRows(databasePath), 0);
  }
});

test('valid legacy SQLite timestamp 2026-10-07 11:30:00 interpreted UTC and exact start accepted', (t) => {
  const { store, databasePath } = fixture(t);
  initSettingsAndConsent(databasePath, {
    collectionEnabled: 1,
    collectionStartedAt: '2026-10-07 11:30:00',
    consentUpdatedAt: '2026-10-07 11:30:00'
  });

  const span = activity({
    started_at: '2026-10-07T11:30:00.000Z',
    ended_at: '2026-10-07T11:45:00.000Z'
  });

  const res = store.recordToolActivity(span);
  assert.equal(res.sqlite.ok, true);
  assert.equal(typeof res.id, 'string');
  assert.equal(countRows(databasePath), 1);
});

test('future ended_at beyond clock rejects companion_activity_future', (t) => {
  const { store, databasePath } = fixture(t);
  initSettingsAndConsent(databasePath, {
    collectionEnabled: 1,
    collectionStartedAt: '2026-10-07T11:30:00.000Z'
  });

  const futureSpan = activity({
    started_at: '2026-10-07T11:55:00.000Z',
    ended_at: '2026-10-07T12:00:00.001Z'
  });

  const res = store.recordToolActivity(futureSpan);
  assert.equal(res.sqlite.ok, false);
  assert.equal(res.sqlite.error, 'companion_activity_future');
  assert.equal(countRows(databasePath), 0);
});

test('retention-policy update does not change collection_started_at, and previous span remains accepted if new policy retains it', (t) => {
  const { store, databasePath } = fixture(t);
  initSettingsAndConsent(databasePath, {
    collectionEnabled: 1,
    collectionStartedAt: '2026-10-07T11:30:00.000Z'
  });

  const span = activity({
    started_at: '2026-10-07T11:30:00.000Z',
    ended_at: '2026-10-07T11:35:00.000Z'
  });
  const res1 = store.recordToolActivity(span);
  assert.equal(res1.sqlite.ok, true);

  store.setCompanionRetentionDays(30);

  const startedAt = db(databasePath, (conn) => {
    return conn.prepare('SELECT collection_started_at FROM companion_settings WHERE id = 1').get().collection_started_at;
  });
  assert.equal(startedAt, '2026-10-07T11:30:00.000Z');
  assert.equal(store.recordToolActivity(span).sqlite.ok, true);
  assert.equal(countRows(databasePath), 2);
});
