-- Track the latest explicit resume separately from retention/settings updates.
ALTER TABLE companion_settings ADD COLUMN collection_started_at TEXT;

-- Preview stores with an enabled flag have no trustworthy resume boundary.
-- Pause them once on upgrade; preserve history and per-tool grants. Resuming
-- after upgrade establishes the first boundary using the desktop controls.
UPDATE companion_settings
SET collection_enabled = 0, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE collection_enabled = 1;
