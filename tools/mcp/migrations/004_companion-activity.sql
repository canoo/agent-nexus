-- NEXUS Companion is disabled unless a future local UI/native host records
-- explicit consent. These tables stay in the existing local observability
-- database: no sync, network, or second activity store is introduced here.
CREATE TABLE IF NOT EXISTS companion_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    collection_enabled INTEGER NOT NULL DEFAULT 0 CHECK (collection_enabled IN (0, 1)),
    raw_span_retention_days INTEGER NOT NULL DEFAULT 14 CHECK (raw_span_retention_days >= 0),
    daily_aggregate_retention_days INTEGER NOT NULL DEFAULT 90 CHECK (daily_aggregate_retention_days >= 0),
    updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO companion_settings (
    id, collection_enabled, raw_span_retention_days, daily_aggregate_retention_days, updated_at
) VALUES (1, 0, 14, 90, CURRENT_TIMESTAMP);

-- Consent must be explicit per local adapter and fixed tool identifier. The
-- first Companion UI/native host will be the only writer for this table.
CREATE TABLE IF NOT EXISTS companion_tool_consents (
    adapter_id TEXT NOT NULL,
    tool_id TEXT NOT NULL,
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    consent_policy_version INTEGER NOT NULL CHECK (consent_policy_version > 0),
    updated_at TEXT NOT NULL,
    PRIMARY KEY (adapter_id, tool_id)
);

CREATE TABLE IF NOT EXISTS tool_activity (
    id TEXT PRIMARY KEY,
    session_id TEXT REFERENCES sessions(id),
    tool_id TEXT NOT NULL,
    surface TEXT NOT NULL,
    started_at TEXT NOT NULL,
    ended_at TEXT NOT NULL,
    detector TEXT NOT NULL,
    confidence TEXT NOT NULL,
    browser_family TEXT,
    platform TEXT,
    schema_version INTEGER NOT NULL,
    consent_policy_version INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tool_activity_started_at
    ON tool_activity(started_at);

CREATE INDEX IF NOT EXISTS idx_tool_activity_tool_id
    ON tool_activity(tool_id);
