-- Small key/value store for store-level bookkeeping (one-shot migration
-- markers, schema-owner notes). Kept separate from schema_migrations because
-- these are runtime facts, not schema versions.
CREATE TABLE IF NOT EXISTS store_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
