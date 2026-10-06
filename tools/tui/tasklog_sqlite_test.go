package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// fixtureDB creates a SQLite database using the real MCP migration, so the
// reader is always tested against the true schema.
func fixtureDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "observability.sqlite")
	migration, err := os.ReadFile("../mcp/migrations/001_observability.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(string(migration)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	// The MCP server sets WAL on every open (observability-store.mjs
	// #openDatabase and migration 001), so the fixture mirrors production:
	// the database file is already in WAL mode before any reader arrives.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		t.Fatalf("enable WAL: %v", err)
	}
	return dbPath
}

func insertTask(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`INSERT INTO sessions (id, start_time, status, cli_tool, nexus_mode)
		VALUES ('mcp-2026-10-05', '2026-10-05T00:00:00.000Z', 'active', 'mcp', 'hybrid')`)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}
	_, err = db.Exec(`INSERT INTO tasks (
		id, session_id, timestamp, source, tool, task_type, model, model_provider,
		route_band, routing, routing_reason, tokens_in, tokens_out, total_tokens,
		latency_ms, cloud_cost_equivalent, ok, error,
		cost_usd, input_bytes, output_bytes, input_hash, trace_id, span_id,
		idempotency_key, quality_rating
	) VALUES (
		'task-1', 'mcp-2026-10-05', '2026-10-05T12:00:00.000Z', 'mcp-tool',
		'ollama_commit_msg', 'ollama_commit_msg', 'qwen2.5-coder:1.5b', 'ollama',
		'supervisor', 'local', 'mcp:ollama_commit_msg', 100, 50, 150,
		42, 0.00105, 1, NULL,
		0.0, 21, 7, 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		'trace-abc', 'span-def', 'req-1', 4
	)`)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
}

func TestLoadTaskLogSQLite_ReadsPopulatedColumns(t *testing.T) {
	dbPath := fixtureDB(t)
	insertTask(t, dbPath)

	entries, err := loadTaskLogSQLiteFrom(dbPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Tool != "ollama_commit_msg" || e.Model != "qwen2.5-coder:1.5b" {
		t.Errorf("identity mismatch: %+v", e)
	}
	if e.Routing != "local" || e.TaskType != "ollama_commit_msg" || e.ModelProvider != "ollama" {
		t.Errorf("routing/type/provider mismatch: %+v", e)
	}
	if e.TokensIn != 100 || e.TokensOut != 50 || e.Ms != 42 {
		t.Errorf("numeric mismatch: %+v", e)
	}
	if !e.Ok || e.Error != "" {
		t.Errorf("status mismatch: %+v", e)
	}
	if e.CloudCostEquivalent != 0.00105 || e.CostUSD != 0.0 {
		t.Errorf("cost mismatch: %+v", e)
	}
	if e.InputBytes != 21 || e.OutputBytes != 7 {
		t.Errorf("byte counts mismatch: %+v", e)
	}
	wantTs := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC).UnixMilli()
	if e.Ts != wantTs {
		t.Errorf("timestamp: got %d want %d", e.Ts, wantTs)
	}
}

func TestLoadTaskLogSQLite_NullableColumnsDefaultCleanly(t *testing.T) {
	dbPath := fixtureDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	// Minimal row: only NOT NULL columns plus the reader's essentials.
	_, err = db.Exec(`INSERT INTO sessions (id, start_time) VALUES ('s1', '2026-10-05T00:00:00.000Z')`)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}
	_, err = db.Exec(`INSERT INTO tasks (id, session_id, timestamp, source, model, routing, ok)
		VALUES ('t-min', 's1', '2026-10-05T13:00:00.000Z', 'mcp-tool', 'fast-path', 'deterministic', 0)`)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
	entries, err := loadTaskLogSQLiteFrom(dbPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Ok || e.Tool != "" || e.TaskType != "" || e.ModelProvider != "" {
		t.Errorf("nullable columns should zero-value: %+v", e)
	}
	if e.TokensIn != 0 || e.CostUSD != 0 || e.InputBytes != 0 {
		t.Errorf("nullable numerics should zero-value: %+v", e)
	}
}

func TestLoadTaskLogEntries_MissingDBFallsBackToJSONL(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "mcp-tasks.jsonl")
	line := `{"tool":"ollama_commit_msg","model":"qwen2.5-coder:1.5b","routing":"local",` +
		`"tokens_in":12,"tokens_out":7,"cloud_cost_equivalent":0.000141,` +
		`"ms":42,"ok":true,"ts":1759423425678}` + "\n"
	if err := os.WriteFile(jsonlPath, []byte(line), 0644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
	entries := loadTaskLogEntries(filepath.Join(dir, "observability.sqlite"), jsonlPath)
	if len(entries) != 1 {
		t.Fatalf("expected JSONL fallback with 1 entry, got %d", len(entries))
	}
	if entries[0].Tool != "ollama_commit_msg" || entries[0].TokensIn != 12 {
		t.Errorf("fallback entry mismatch: %+v", entries[0])
	}
}

func TestLoadTaskLogEntries_CorruptDBFallsBackWithoutCrashing(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "observability.sqlite")
	if err := os.WriteFile(dbPath, []byte("this is not a sqlite database at all"), 0644); err != nil {
		t.Fatalf("write corrupt db: %v", err)
	}
	jsonlPath := filepath.Join(dir, "mcp-tasks.jsonl")
	line := `{"tool":"ollama_boilerplate","model":"qwen2.5-coder:1.5b","ms":5,"ok":false,` +
		`"error":"ollama_unreachable","ts":1759423425678}` + "\n"
	if err := os.WriteFile(jsonlPath, []byte(line), 0644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
	entries := loadTaskLogEntries(dbPath, jsonlPath)
	if len(entries) != 1 || entries[0].Tool != "ollama_boilerplate" || entries[0].Ok {
		t.Fatalf("expected corrupt-DB fallback to JSONL entry, got %+v", entries)
	}
}

func TestLoadTaskLogEntries_EmptyDBShowsEmptyNotJSONL(t *testing.T) {
	// SQLite is the source of truth: an existing-but-empty database means an
	// empty log, not a fallback to the compatibility file.
	dbPath := fixtureDB(t)
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "mcp-tasks.jsonl")
	if err := os.WriteFile(jsonlPath, []byte("{}\n"), 0644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
	if entries := loadTaskLogEntries(dbPath, jsonlPath); len(entries) != 0 {
		t.Fatalf("expected empty (SQLite authoritative), got %d entries", len(entries))
	}
}

func TestOpenObservabilityDB_EnablesWAL(t *testing.T) {
	dbPath := fixtureDB(t)
	db, err := openObservabilityDB(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("expected WAL journal mode, got %q", mode)
	}
}

func TestLoadTaskLogSQLite_ConcurrentReadDuringWrite(t *testing.T) {
	dbPath := fixtureDB(t)
	insertTask(t, dbPath)

	// Hold an open write transaction while the reader runs: with WAL this
	// must not fail or stall.
	writer, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	defer writer.Close()
	tx, err := writer.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO sessions (id, start_time) VALUES ('s-held', '2026-10-05T00:00:00.000Z')`); err != nil {
		t.Fatalf("write in tx: %v", err)
	}

	start := time.Now()
	entries, err := loadTaskLogSQLiteFrom(dbPath)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("concurrent read failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 committed entry, got %d", len(entries))
	}
	// busy_timeout is 5s: a blocked read would take ~5s. WAL reads sail through.
	if elapsed > 4*time.Second {
		t.Fatalf("read stalled %v; WAL concurrent access not working", elapsed)
	}
}

func TestNexusLogDirMatchesMCPServer(t *testing.T) {
	dir, err := nexusLogDir()
	if err != nil {
		t.Fatalf("nexusLogDir: %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(dir), ".config/nexus/logs") {
		t.Fatalf("unexpected log dir %q; must match tools/mcp DEFAULT_LOG_DIR", dir)
	}
	dbPath, err := observabilityDBPath()
	if err != nil {
		t.Fatalf("observabilityDBPath: %v", err)
	}
	if filepath.Base(dbPath) != "observability.sqlite" {
		t.Fatalf("unexpected db name %q; must match DEFAULT_DATABASE_PATH", dbPath)
	}
}
