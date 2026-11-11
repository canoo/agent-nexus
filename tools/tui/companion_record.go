package main

// Native Go writer for Companion tool-activity spans, backing the
// `activity.record` control-protocol operation (protocol v1).
//
// The native host used to ingest through a vendored copy of the JS
// observability store because protocol v1 had no record operation. This
// writer mirrors tools/mcp/lib/observability-store.mjs
// (normalizeToolActivityEvent plus ObservabilityStore.recordToolActivity)
// field for field, so the host can send the same fixed envelope to
// `nexus control --json` and drop the vendored copy. No node runtime and no
// repository checkout are needed: the store path is the same
// ~/.config/nexus/logs/observability.sqlite the MCP server owns, and the
// companion migrations (004/005) are applied idempotently when absent.
//
// Consent is the authorization: collection must be enabled and the
// adapter/tool grant current, and the span must start on or after the
// resume/consent boundary. The envelope has no generic metadata field, so
// URLs, titles, prompts, responses and account identifiers cannot reach the
// store through this operation.

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var companionActivityTimestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,3})?Z$`)
var companionLegacyTimestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`)

var companionToolIDs = map[string]bool{
	"chatgpt": true, "claude": true, "gemini": true, "copilot": true, "perplexity": true,
}
var companionBrowserFamilies = map[string]bool{"chrome": true, "edge": true}
var companionPlatforms = map[string]bool{"linux": true, "macos": true}

// companionActivityEvent is the fixed ingestion envelope, byte-compatible
// with normalizeToolActivityEvent in tools/mcp/lib/observability-store.mjs.
// Optional fields are pointers so an explicit JSON null is distinguishable
// from an absent field.
type companionActivityEvent struct {
	ToolID               string  `json:"tool_id"`
	Surface              string  `json:"surface"`
	StartedAt            string  `json:"started_at"`
	EndedAt              string  `json:"ended_at"`
	Detector             string  `json:"detector"`
	Confidence           string  `json:"confidence"`
	BrowserFamily        *string `json:"browser_family,omitempty"`
	Platform             *string `json:"platform,omitempty"`
	SchemaVersion        int     `json:"schema_version"`
	ConsentPolicyVersion int     `json:"consent_policy_version"`
}

type normalizedCompanionActivityEvent struct {
	toolID               string
	surface              string
	startedAt            time.Time
	endedAt              time.Time
	startedAtISO         string
	endedAtISO           string
	detector             string
	confidence           string
	browserFamily        string
	platform             string
	schemaVersion        int
	consentPolicyVersion int
	adapterID            string
}

func companionActivityISO(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func parseCompanionActivityTimestamp(value string) (time.Time, error) {
	if !companionActivityTimestampPattern.MatchString(value) {
		return time.Time{}, errors.New("control_params_invalid")
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, errors.New("control_params_invalid")
	}
	return parsed.UTC(), nil
}

// validateCompanionActivityEvent mirrors normalizeToolActivityEvent: same
// allowlists, same surface/detector/browser cross-checks, same timestamp
// shape. The raw params are checked too, so an explicit JSON null for an
// optional field is rejected exactly as the JS normalizer rejects it.
func validateCompanionActivityEvent(event companionActivityEvent, raw json.RawMessage) (normalizedCompanionActivityEvent, error) {
	invalid := func() (normalizedCompanionActivityEvent, error) {
		return normalizedCompanionActivityEvent{}, errors.New("control_params_invalid")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return invalid()
	}
	for _, key := range []string{"browser_family", "platform"} {
		if value, ok := fields[key]; ok && string(bytes.TrimSpace(value)) == "null" {
			return invalid()
		}
	}
	if !companionToolIDs[event.ToolID] {
		return invalid()
	}
	if event.Surface != "browser" && event.Surface != "desktop" {
		return invalid()
	}
	if event.Detector != "selected-browser-tab" && event.Detector != "foreground-app" {
		return invalid()
	}
	if event.Confidence != "surface-active" {
		return invalid()
	}
	if event.SchemaVersion != 1 || event.ConsentPolicyVersion != 1 {
		return invalid()
	}
	normalized := normalizedCompanionActivityEvent{
		toolID:               event.ToolID,
		surface:              event.Surface,
		detector:             event.Detector,
		confidence:           event.Confidence,
		schemaVersion:        event.SchemaVersion,
		consentPolicyVersion: event.ConsentPolicyVersion,
	}
	if event.Platform != nil {
		if !companionPlatforms[*event.Platform] {
			return invalid()
		}
		normalized.platform = *event.Platform
	}
	switch event.Surface {
	case "browser":
		if event.Detector != "selected-browser-tab" {
			return invalid()
		}
		if event.BrowserFamily == nil || !companionBrowserFamilies[*event.BrowserFamily] {
			return invalid()
		}
		normalized.browserFamily = *event.BrowserFamily
		normalized.adapterID = "browser-" + *event.BrowserFamily
	case "desktop":
		if event.Detector != "foreground-app" {
			return invalid()
		}
		if event.BrowserFamily != nil {
			return invalid()
		}
		normalized.adapterID = "desktop-foreground-app"
	}
	startedAt, err := parseCompanionActivityTimestamp(event.StartedAt)
	if err != nil {
		return invalid()
	}
	endedAt, err := parseCompanionActivityTimestamp(event.EndedAt)
	if err != nil {
		return invalid()
	}
	if !endedAt.After(startedAt) {
		return invalid()
	}
	normalized.startedAt = startedAt
	normalized.endedAt = endedAt
	normalized.startedAtISO = companionActivityISO(startedAt)
	normalized.endedAtISO = companionActivityISO(endedAt)
	return normalized, nil
}

func newCompanionActivityID() (string, error) {
	var raw [16]byte
	if _, err := io.ReadFull(rand.Reader, raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}

// companionMigration004Statements is the DDL of
// tools/mcp/migrations/004_companion-activity.sql, split into single
// statements. It is idempotent (CREATE TABLE IF NOT EXISTS, INSERT OR
// IGNORE), and the matching schema_migrations row is recorded so the JS
// store never re-applies it.
var companionMigration004Statements = []string{
	`CREATE TABLE IF NOT EXISTS companion_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    collection_enabled INTEGER NOT NULL DEFAULT 0 CHECK (collection_enabled IN (0, 1)),
    raw_span_retention_days INTEGER NOT NULL DEFAULT 14 CHECK (raw_span_retention_days >= 0),
    daily_aggregate_retention_days INTEGER NOT NULL DEFAULT 90 CHECK (daily_aggregate_retention_days >= 0),
    updated_at TEXT NOT NULL
)`,
	`INSERT OR IGNORE INTO companion_settings (
    id, collection_enabled, raw_span_retention_days, daily_aggregate_retention_days, updated_at
) VALUES (1, 0, 14, 90, CURRENT_TIMESTAMP)`,
	`CREATE TABLE IF NOT EXISTS companion_tool_consents (
    adapter_id TEXT NOT NULL,
    tool_id TEXT NOT NULL,
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    consent_policy_version INTEGER NOT NULL CHECK (consent_policy_version > 0),
    updated_at TEXT NOT NULL,
    PRIMARY KEY (adapter_id, tool_id)
)`,
	`CREATE TABLE IF NOT EXISTS tool_activity (
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
)`,
	`CREATE INDEX IF NOT EXISTS idx_tool_activity_started_at ON tool_activity(started_at)`,
	`CREATE INDEX IF NOT EXISTS idx_tool_activity_tool_id ON tool_activity(tool_id)`,
}

const companionSchemaMigrationsTable = `CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
)`

func appliedCompanionMigration(ctx context.Context, db *sql.DB, version int) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&count)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return false, nil
		}
		return false, err
	}
	return count > 0, nil
}

func recordCompanionMigration(ctx context.Context, tx *sql.Tx, version int, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, version, companionActivityISO(now))
	return err
}

// ensureCompanionActivitySchema applies companion migrations 004 and 005
// the way the JS store's applyMigrations does, including the
// schema_migrations bookkeeping, so a later JS open skips them. Migration
// 005's one-off (add collection_started_at, pause enabled collection) runs
// only when the column is actually missing.
func ensureCompanionActivitySchema(ctx context.Context, db *sql.DB, now time.Time) error {
	storeErr := errors.New("companion_store_unavailable")
	if _, err := db.ExecContext(ctx, companionSchemaMigrationsTable); err != nil {
		return storeErr
	}
	applied4, err := appliedCompanionMigration(ctx, db, 4)
	if err != nil {
		return storeErr
	}
	if !applied4 {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return storeErr
		}
		defer tx.Rollback()
		for _, statement := range companionMigration004Statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return storeErr
			}
		}
		if err := recordCompanionMigration(ctx, tx, 4, now); err != nil {
			return storeErr
		}
		if err := tx.Commit(); err != nil {
			return storeErr
		}
	}
	applied5, err := appliedCompanionMigration(ctx, db, 5)
	if err != nil {
		return storeErr
	}
	if !applied5 {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return storeErr
		}
		defer tx.Rollback()
		rows, err := tx.QueryContext(ctx, `PRAGMA table_info(companion_settings)`)
		if err != nil {
			return storeErr
		}
		hasColumn := false
		for rows.Next() {
			var cid int
			var name, typ string
			var notnull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
				rows.Close()
				return storeErr
			}
			if name == "collection_started_at" {
				hasColumn = true
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return storeErr
		}
		rows.Close()
		if !hasColumn {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE companion_settings ADD COLUMN collection_started_at TEXT`); err != nil {
				return storeErr
			}
			if _, err := tx.ExecContext(ctx, `UPDATE companion_settings SET collection_enabled = 0, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE collection_enabled = 1`); err != nil {
				return storeErr
			}
		}
		if err := recordCompanionMigration(ctx, tx, 5, now); err != nil {
			return storeErr
		}
		if err := tx.Commit(); err != nil {
			return storeErr
		}
	}
	return nil
}

func companionRetentionDays(ctx context.Context, tx *sql.Tx) (int, error) {
	var days int
	err := tx.QueryRowContext(ctx, `SELECT raw_span_retention_days FROM companion_settings WHERE id = 1`).Scan(&days)
	if err != nil {
		return 0, errors.New("companion_store_unavailable")
	}
	if days < 0 || days > 365 {
		return 0, errors.New("companion_store_unavailable")
	}
	return days, nil
}

// pruneCompanionActivityRows deletes expired spans exactly like the JS
// store: a zero retention deletes everything, otherwise rows whose start
// is older than the cutoff and whose end is older than the cutoff.
func pruneCompanionActivityRows(ctx context.Context, tx *sql.Tx, retentionDays int, now time.Time) error {
	if retentionDays == 0 {
		_, err := tx.ExecContext(ctx, `DELETE FROM tool_activity`)
		return err
	}
	cutoff := now.Add(-time.Duration(retentionDays) * 24 * time.Hour)
	cutoffISO := companionActivityISO(cutoff)
	candidateBound := cutoffISO[:19] + "~"
	_, err := tx.ExecContext(ctx, `DELETE FROM tool_activity
      WHERE started_at < ? AND julianday(ended_at) < julianday(?)`, candidateBound, cutoffISO)
	return err
}

// pruneCompanionActivity runs the JS store's cleanup in its own transaction:
// it commits even when the span that follows is later rejected.
func pruneCompanionActivity(ctx context.Context, db *sql.DB, now time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("companion_store_unavailable")
	}
	defer tx.Rollback()
	days, err := companionRetentionDays(ctx, tx)
	if err != nil {
		return err
	}
	if err := pruneCompanionActivityRows(ctx, tx, days, now); err != nil {
		return errors.New("companion_store_unavailable")
	}
	if err := tx.Commit(); err != nil {
		return errors.New("companion_store_unavailable")
	}
	return nil
}

// companionBoundaryMillis parses a resume/consent boundary the way the JS
// store does: SQLite CURRENT_TIMESTAMP legacy format
// ("YYYY-MM-DD HH:MM:SS") or a UTC RFC3339 timestamp. A missing boundary
// fails closed.
func companionBoundaryMillis(value sql.NullString) (int64, error) {
	unavailable := errors.New("companion_activity_boundary_unavailable")
	if !value.Valid {
		return 0, unavailable
	}
	utc := value.String
	if companionLegacyTimestampPattern.MatchString(utc) {
		utc = utc[:10] + "T" + utc[11:] + "Z"
	}
	if !companionActivityTimestampPattern.MatchString(utc) {
		return 0, unavailable
	}
	parsed, err := time.Parse(time.RFC3339Nano, utc)
	if err != nil {
		return 0, unavailable
	}
	return parsed.UnixMilli(), nil
}

// insertCompanionActivity runs the consent-gated insert in a single
// immediate transaction, mirroring #writeToolActivity: collection must be
// enabled, the adapter/tool grant must be current, the span must start on
// or after the later of the resume and consent boundaries, must not end in
// the future, and must fall inside the retention window.
func insertCompanionActivity(ctx context.Context, db *sql.DB, event normalizedCompanionActivityEvent, now time.Time) (string, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", errors.New("companion_store_unavailable")
	}
	defer tx.Rollback()
	var collectionEnabled int
	var collectionStartedAt sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT collection_enabled, collection_started_at
          FROM companion_settings WHERE id = 1`).Scan(&collectionEnabled, &collectionStartedAt)
	if err != nil {
		return "", errors.New("companion_store_unavailable")
	}
	if collectionEnabled != 1 {
		return "", errors.New("companion_collection_disabled")
	}
	var consentUpdatedAt string
	err = tx.QueryRowContext(ctx, `SELECT updated_at FROM companion_tool_consents
          WHERE adapter_id = ? AND tool_id = ? AND enabled = 1
            AND consent_policy_version = ?`, event.adapterID, event.toolID, event.consentPolicyVersion).Scan(&consentUpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("companion_tool_consent_missing")
		}
		return "", errors.New("companion_store_unavailable")
	}
	resumeBoundary, err := companionBoundaryMillis(collectionStartedAt)
	if err != nil {
		return "", err
	}
	consentBoundary, err := companionBoundaryMillis(sql.NullString{String: consentUpdatedAt, Valid: true})
	if err != nil {
		return "", err
	}
	boundary := resumeBoundary
	if consentBoundary > boundary {
		boundary = consentBoundary
	}
	if event.startedAt.UnixMilli() < boundary {
		return "", errors.New("companion_activity_crosses_boundary")
	}
	nowMillis := now.UnixMilli()
	if event.endedAt.UnixMilli() > nowMillis {
		return "", errors.New("companion_activity_future")
	}
	retentionDays, err := companionRetentionDays(ctx, tx)
	if err != nil {
		return "", err
	}
	if retentionDays == 0 {
		return "", errors.New("companion_retention_disabled")
	}
	if event.endedAt.UnixMilli() < nowMillis-int64(retentionDays)*86400000 {
		return "", errors.New("companion_activity_expired")
	}
	id, err := newCompanionActivityID()
	if err != nil {
		return "", errors.New("companion_store_unavailable")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tool_activity (
          id, session_id, tool_id, surface, started_at, ended_at, detector,
          confidence, browser_family, platform, schema_version, consent_policy_version
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, nil, event.toolID, event.surface, event.startedAtISO, event.endedAtISO,
		event.detector, event.confidence,
		sql.NullString{String: event.browserFamily, Valid: event.browserFamily != ""},
		sql.NullString{String: event.platform, Valid: event.platform != ""},
		event.schemaVersion, event.consentPolicyVersion)
	if err != nil {
		return "", errors.New("companion_store_unavailable")
	}
	if err := tx.Commit(); err != nil {
		return "", errors.New("companion_store_unavailable")
	}
	return id, nil
}

func openCompanionRecordDB(path string) (*sql.DB, bool, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, false, errors.New("companion_store_unavailable")
	}
	created := false
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		created = true
		file.Close()
	} else if !os.IsExist(err) {
		return nil, false, errors.New("companion_store_unavailable")
	}
	// No foreign_keys pragma: the record op ensures only the companion
	// migrations (004/005), and tool_activity references sessions(id) from
	// migration 001. With FK enforcement on, SQLite rejects the insert when
	// the parent table does not exist yet; the insert always uses a NULL
	// session_id, and the JS store (which applies all migrations) enforces
	// the key on its own writes.
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate")
	if err != nil {
		return nil, false, errors.New("companion_store_unavailable")
	}
	db.SetMaxOpenConns(1)
	return db, created, nil
}

// recordCompanionActivity validates nothing: the caller must have run
// validateCompanionActivityEvent first. It ensures the companion schema,
// prunes expired spans, then performs the consent-gated insert. Like the
// JS store it creates a missing store file (0600, private logs dir) rather
// than failing a first write on a fresh machine.
func recordCompanionActivity(event normalizedCompanionActivityEvent) (string, error) {
	now := time.Now().UTC()
	path, err := observabilityDBPath()
	if err != nil {
		return "", errors.New("companion_store_unavailable")
	}
	db, _, err := openCompanionRecordDB(path)
	if err != nil {
		return "", err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return "", errors.New("companion_store_unavailable")
	}
	if err := ensureCompanionActivitySchema(ctx, db, now); err != nil {
		return "", err
	}
	if err := pruneCompanionActivity(ctx, db, now); err != nil {
		return "", err
	}
	return insertCompanionActivity(ctx, db, event, now)
}
