package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// nexusLogDir resolves the same directory the MCP observability store uses:
// $HOME/.config/nexus/logs (see tools/mcp/lib/observability-store.mjs
// DEFAULT_LOG_DIR). Both sides must agree on this path.
func nexusLogDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("cannot resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "nexus", "logs"), nil
}

func observabilityDBPath() (string, error) {
	dir, err := nexusLogDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "observability.sqlite"), nil
}

// openObservabilityDB opens the SQLite observability database for reading.
// SQLite is the only task-log source; an absent or unreadable database
// yields an empty log view, never a fallback. WAL mode lets this reader
// proceed while the MCP server holds write
// transactions; the mode change persists on the database file, so the server
// inherits it on its next open. busy_timeout turns a contested lock into a
// retry instead of an instant SQLITE_BUSY. MaxOpenConns(1) keeps pragma state
// predictable on the single connection.
func openObservabilityDB(dbPath string) (*sql.DB, error) {
	// Never create the database from the TUI: an absent file means the MCP
	// server has never run here, so the log view shows an empty state.
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("observability database not present: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	// Belt and suspenders: the DSN pragmas above should already have applied,
	// but an explicit pass makes the intent independent of DSN parsing.
	// Either may fail on a locked or foreign file; reads below still decide.
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	_, _ = db.Exec("PRAGMA busy_timeout=5000")
	return db, nil
}

// sqliteTaskRow holds one tasks row. Nullable columns use sql.Null* because
// legacy imports and older writers leave them NULL.
type sqliteTaskRow struct {
	tool                sql.NullString
	model               string
	routing             sql.NullString
	taskType            sql.NullString
	modelProvider       sql.NullString
	routeBand           sql.NullString
	tokensIn            sql.NullInt64
	tokensOut           sql.NullInt64
	latencyMs           sql.NullInt64
	ok                  int64
	errText             sql.NullString
	timestamp           string
	cloudCostEquivalent sql.NullFloat64
	costUSD             sql.NullFloat64
	inputBytes          sql.NullInt64
	outputBytes         sql.NullInt64
}

func (r sqliteTaskRow) toEntry() taskLogEntry {
	e := taskLogEntry{
		Tool:                r.tool.String,
		Model:               r.model,
		Routing:             r.routing.String,
		TaskType:            r.taskType.String,
		ModelProvider:       r.modelProvider.String,
		RouteBand:           r.routeBand.String,
		TokensIn:            int(r.tokensIn.Int64),
		TokensOut:           int(r.tokensOut.Int64),
		CloudCostEquivalent: r.cloudCostEquivalent.Float64,
		CostUSD:             r.costUSD.Float64,
		InputBytes:          r.inputBytes.Int64,
		OutputBytes:         r.outputBytes.Int64,
		Ms:                  int(r.latencyMs.Int64),
		Ok:                  r.ok != 0,
		Error:               r.errText.String,
	}
	if ts, err := time.Parse(time.RFC3339, r.timestamp); err == nil {
		e.Ts = ts.UnixMilli()
	}
	return e
}

// loadTaskLogSQLiteFrom reads the most recent tasks from SQLite. Any failure
// (absent/corrupt/unreadable database, missing table, locked longer than the
// busy timeout) returns an error; the caller shows an empty state instead.
func loadTaskLogSQLiteFrom(dbPath string) ([]taskLogEntry, error) {
	db, err := openObservabilityDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT tool, model, routing, task_type, model_provider, route_band,
		tokens_in, tokens_out, latency_ms, ok, error, timestamp,
		cloud_cost_equivalent, cost_usd, input_bytes, output_bytes
		FROM tasks ORDER BY timestamp DESC LIMIT 50`)
	if err != nil {
		return nil, fmt.Errorf("tasks query failed: %w", err)
	}
	defer rows.Close()

	var entries []taskLogEntry
	for rows.Next() {
		var r sqliteTaskRow
		if err := rows.Scan(
			&r.tool, &r.model, &r.routing, &r.taskType, &r.modelProvider, &r.routeBand,
			&r.tokensIn, &r.tokensOut, &r.latencyMs, &r.ok, &r.errText, &r.timestamp,
			&r.cloudCostEquivalent, &r.costUSD, &r.inputBytes, &r.outputBytes,
		); err != nil {
			return nil, fmt.Errorf("task row scan failed: %w", err)
		}
		entries = append(entries, r.toEntry())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("task row iteration failed: %w", err)
	}
	return entries, nil
}

// loadTaskLogEntries returns the most recent tasks from SQLite, the single
// task-log source. Any read failure is logged and yields an empty view:
// a corrupt or missing database must never panic the TUI.
func loadTaskLogEntries(dbPath string) []taskLogEntry {
	entries, err := loadTaskLogSQLiteFrom(dbPath)
	if err != nil {
		log.Printf("NEXUS task log: SQLite read failed (%v); showing empty task log", err)
		return nil
	}
	return entries
}
