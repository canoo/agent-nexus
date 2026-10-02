-- Receipts identify stable rows in an append-only source. snapshot_hash is
-- retained as audit metadata; it is deliberately not part of the row identity
-- so importing an appended log only writes its new tail.
-- The importer writes a receipt in the same transaction as its task row, so a
-- restart cannot leave a task without its idempotency marker (or vice versa).
CREATE TABLE IF NOT EXISTS legacy_import_receipts (
    source_path TEXT NOT NULL,
    snapshot_hash TEXT NOT NULL,
    raw_line_hash TEXT NOT NULL,
    occurrence INTEGER NOT NULL CHECK (occurrence > 0),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    imported_at TEXT NOT NULL,
    PRIMARY KEY (source_path, raw_line_hash, occurrence),
    UNIQUE (task_id)
);

CREATE INDEX IF NOT EXISTS idx_legacy_import_receipts_snapshot
    ON legacy_import_receipts(source_path, snapshot_hash);
