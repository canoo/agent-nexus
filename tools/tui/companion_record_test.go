package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func companionRecordHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func companionRecordDBPath(home string) string {
	return filepath.Join(home, ".config", "nexus", "logs", "observability.sqlite")
}

// seedCompanionStore creates the companion schema through the same ensure
// path the record op uses, then enables collection with one adapter/tool
// grant. Boundaries are set boundaryAge in the past so spans ending now pass.
func seedCompanionStore(t *testing.T, home string, enabled bool, retentionDays int, adapter, tool string, boundaryAge time.Duration) *sql.DB {
	t.Helper()
	path := companionRecordDBPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	db, _, err := openCompanionRecordDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	now := time.Now().UTC()
	if err := ensureCompanionActivitySchema(ctx, db, now); err != nil {
		t.Fatal(err)
	}
	boundary := companionActivityISO(now.Add(-boundaryAge))
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	if _, err := db.ExecContext(ctx, `UPDATE companion_settings SET collection_enabled = ?, collection_started_at = ?, raw_span_retention_days = ? WHERE id = 1`, enabledInt, boundary, retentionDays); err != nil {
		t.Fatal(err)
	}
	if adapter != "" {
		if _, err := db.ExecContext(ctx, `INSERT INTO companion_tool_consents (adapter_id, tool_id, enabled, consent_policy_version, updated_at) VALUES (?, ?, 1, 1, ?)`, adapter, tool, boundary); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func companionRecordParams(toolID, surface, detector, browserFamily, platform string, started, ended time.Time) map[string]any {
	params := map[string]any{
		"tool_id":                toolID,
		"surface":                surface,
		"started_at":             companionActivityISO(started),
		"ended_at":               companionActivityISO(ended),
		"detector":               detector,
		"confidence":             "surface-active",
		"schema_version":         1,
		"consent_policy_version": 1,
	}
	if browserFamily != "" {
		params["browser_family"] = browserFamily
	}
	if platform != "" {
		params["platform"] = platform
	}
	return params
}

func companionRecordSpan(t *testing.T, home string, params map[string]any) (map[string]any, int) {
	t.Helper()
	root := t.TempDir()
	return controlCall(t, root, "activity.record", false, params)
}

func countCompanionSpans(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM tool_activity`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func readCompanionSpan(t *testing.T, db *sql.DB, id string) map[string]any {
	t.Helper()
	var toolID, surface, startedAt, endedAt, detector, confidence string
	var browserFamily, platform sql.NullString
	var schemaVersion, consentPolicyVersion int
	err := db.QueryRowContext(context.Background(), `SELECT tool_id, surface, started_at, ended_at, detector, confidence, browser_family, platform, schema_version, consent_policy_version FROM tool_activity WHERE id = ?`,
		id).Scan(&toolID, &surface, &startedAt, &endedAt, &detector, &confidence, &browserFamily, &platform, &schemaVersion, &consentPolicyVersion)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"tool_id": toolID, "surface": surface, "started_at": startedAt, "ended_at": endedAt,
		"detector": detector, "confidence": confidence, "browser_family": browserFamily.String,
		"platform": platform.String, "schema_version": schemaVersion,
		"consent_policy_version": consentPolicyVersion,
	}
}

func TestActivityRecordStoresBrowserSpan(t *testing.T) {
	home := companionRecordHome(t)
	db := seedCompanionStore(t, home, true, 14, "browser-chrome", "chatgpt", time.Hour)
	now := time.Now().UTC()
	started := now.Add(-60 * time.Second)
	ended := now.Add(-30 * time.Second)
	result, code := companionRecordSpan(t, home, companionRecordParams("chatgpt", "browser", "selected-browser-tab", "chrome", "linux", started, ended))
	if code != 0 {
		t.Fatal(result)
	}
	got := result["result"].(map[string]any)
	if got["recorded"] != true {
		t.Fatal(got)
	}
	id, _ := got["id"].(string)
	if id == "" {
		t.Fatal(got)
	}
	if countCompanionSpans(t, db) != 1 {
		t.Fatal("expected one stored span")
	}
	row := readCompanionSpan(t, db, id)
	if row["tool_id"] != "chatgpt" || row["surface"] != "browser" || row["browser_family"] != "chrome" || row["platform"] != "linux" {
		t.Fatal(row)
	}
	if row["started_at"] != companionActivityISO(started) || row["ended_at"] != companionActivityISO(ended) {
		t.Fatal(row)
	}
	if row["detector"] != "selected-browser-tab" || row["confidence"] != "surface-active" {
		t.Fatal(row)
	}
}

func TestActivityRecordStoresDesktopSpan(t *testing.T) {
	home := companionRecordHome(t)
	db := seedCompanionStore(t, home, true, 14, "desktop-foreground-app", "claude", time.Hour)
	now := time.Now().UTC()
	result, code := companionRecordSpan(t, home, companionRecordParams("claude", "desktop", "foreground-app", "", "macos", now.Add(-60*time.Second), now.Add(-30*time.Second)))
	if code != 0 {
		t.Fatal(result)
	}
	id := result["result"].(map[string]any)["id"].(string)
	if countCompanionSpans(t, db) != 1 {
		t.Fatal("expected one stored span")
	}
	row := readCompanionSpan(t, db, id)
	if row["browser_family"] != "" || row["platform"] != "macos" {
		t.Fatal(row)
	}
}

func TestActivityRecordRejectsInvalidEnvelopes(t *testing.T) {
	home := companionRecordHome(t)
	db := seedCompanionStore(t, home, true, 14, "browser-chrome", "chatgpt", time.Hour)
	now := time.Now().UTC()
	started := now.Add(-60 * time.Second)
	ended := now.Add(-30 * time.Second)
	cases := map[string]map[string]any{
		"unknown tool":             companionRecordParams("notion", "browser", "selected-browser-tab", "chrome", "linux", started, ended),
		"unknown surface":          companionRecordParams("chatgpt", "phone", "selected-browser-tab", "chrome", "linux", started, ended),
		"wrong detector":           companionRecordParams("chatgpt", "browser", "foreground-app", "chrome", "linux", started, ended),
		"wrong confidence":         companionRecordParams("chatgpt", "browser", "selected-browser-tab", "chrome", "linux", started, ended),
		"browser without family":   companionRecordParams("chatgpt", "browser", "selected-browser-tab", "", "linux", started, ended),
		"desktop with family":      companionRecordParams("chatgpt", "desktop", "foreground-app", "chrome", "linux", started, ended),
		"unknown platform":         companionRecordParams("chatgpt", "browser", "selected-browser-tab", "chrome", "windows", started, ended),
		"schema version":           companionRecordParams("chatgpt", "browser", "selected-browser-tab", "chrome", "linux", started, ended),
		"ended before started":     companionRecordParams("chatgpt", "browser", "selected-browser-tab", "chrome", "linux", ended, started),
		"sensitive extra field":    companionRecordParams("chatgpt", "browser", "selected-browser-tab", "chrome", "linux", started, ended),
		"unknown extra field":      companionRecordParams("chatgpt", "browser", "selected-browser-tab", "chrome", "linux", started, ended),
		"null browser family":      companionRecordParams("chatgpt", "browser", "selected-browser-tab", "chrome", "linux", started, ended),
		"non-UTC timestamp":        companionRecordParams("chatgpt", "browser", "selected-browser-tab", "chrome", "linux", started, ended),
		"fractional seconds > 3dp": companionRecordParams("chatgpt", "browser", "selected-browser-tab", "chrome", "linux", started, ended),
	}
	cases["wrong confidence"]["confidence"] = "likely-active"
	cases["schema version"]["schema_version"] = 2
	cases["sensitive extra field"]["url"] = "https://example.invalid/private"
	cases["unknown extra field"]["session_id"] = "spoofed"
	cases["null browser family"]["browser_family"] = nil
	cases["non-UTC timestamp"]["started_at"] = started.Format("2006-01-02T15:04:05.000-07:00")
	cases["fractional seconds > 3dp"]["started_at"] = started.Format("2006-01-02T15:04:05.1234Z")
	for name, params := range cases {
		result, code := companionRecordSpan(t, home, params)
		if code != 1 || result["error"] != "control_params_invalid" {
			t.Fatalf("%s: got code=%d result=%v", name, code, result)
		}
	}
	if countCompanionSpans(t, db) != 0 {
		t.Fatal("invalid envelopes stored spans")
	}
}

func TestActivityRecordRequiresCollectionAndConsent(t *testing.T) {
	now := time.Now().UTC()
	started := now.Add(-60 * time.Second)
	ended := now.Add(-30 * time.Second)
	params := companionRecordParams("chatgpt", "browser", "selected-browser-tab", "chrome", "linux", started, ended)

	home := companionRecordHome(t)
	seedCompanionStore(t, home, false, 14, "browser-chrome", "chatgpt", time.Hour)
	result, code := companionRecordSpan(t, home, params)
	if code != 1 || result["error"] != "companion_collection_disabled" {
		t.Fatalf("disabled collection: code=%d result=%v", code, result)
	}

	home = companionRecordHome(t)
	seedCompanionStore(t, home, true, 14, "", "", time.Hour)
	result, code = companionRecordSpan(t, home, params)
	if code != 1 || result["error"] != "companion_tool_consent_missing" {
		t.Fatalf("no consent: code=%d result=%v", code, result)
	}

	home = companionRecordHome(t)
	seedCompanionStore(t, home, true, 14, "browser-edge", "chatgpt", time.Hour)
	result, code = companionRecordSpan(t, home, params)
	if code != 1 || result["error"] != "companion_tool_consent_missing" {
		t.Fatalf("wrong adapter consent: code=%d result=%v", code, result)
	}
}

func TestActivityRecordEnforcesBoundaryRetentionAndFuture(t *testing.T) {
	now := time.Now().UTC()
	home := companionRecordHome(t)
	db := seedCompanionStore(t, home, true, 14, "browser-chrome", "chatgpt", 30*24*time.Hour)

	// Span starting before the resume/consent boundary (30 days ago).
	result, code := companionRecordSpan(t, home, companionRecordParams("chatgpt", "browser", "selected-browser-tab", "chrome", "linux", now.Add(-40*24*time.Hour), now.Add(-40*24*time.Hour+time.Minute)))
	if code != 1 || result["error"] != "companion_activity_crosses_boundary" {
		t.Fatalf("crosses boundary: code=%d result=%v", code, result)
	}

	// Span ending in the future.
	result, code = companionRecordSpan(t, home, companionRecordParams("chatgpt", "browser", "selected-browser-tab", "chrome", "linux", now.Add(-time.Minute), now.Add(time.Minute)))
	if code != 1 || result["error"] != "companion_activity_future" {
		t.Fatalf("future span: code=%d result=%v", code, result)
	}

	// Span older than the 14-day retention window.
	oldEnd := now.Add(-15 * 24 * time.Hour)
	result, code = companionRecordSpan(t, home, companionRecordParams("chatgpt", "browser", "selected-browser-tab", "chrome", "linux", oldEnd.Add(-time.Minute), oldEnd))
	if code != 1 || result["error"] != "companion_activity_expired" {
		t.Fatalf("expired span: code=%d result=%v", code, result)
	}

	// Zero retention refuses every span.
	if _, err := db.ExecContext(context.Background(), `UPDATE companion_settings SET raw_span_retention_days = 0 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	result, code = companionRecordSpan(t, home, companionRecordParams("chatgpt", "browser", "selected-browser-tab", "chrome", "linux", now.Add(-60*time.Second), now.Add(-30*time.Second)))
	if code != 1 || result["error"] != "companion_retention_disabled" {
		t.Fatalf("zero retention: code=%d result=%v", code, result)
	}
	if countCompanionSpans(t, db) != 0 {
		t.Fatal("rejected spans stored rows")
	}
}

func TestActivityRecordPrunesExpiredSpans(t *testing.T) {
	home := companionRecordHome(t)
	db := seedCompanionStore(t, home, true, 14, "browser-chrome", "chatgpt", time.Hour)
	now := time.Now().UTC()
	oldStart := companionActivityISO(now.Add(-20 * 24 * time.Hour))
	oldEnd := companionActivityISO(now.Add(-20*24*time.Hour + time.Minute))
	if _, err := db.ExecContext(context.Background(), `INSERT INTO tool_activity (id, session_id, tool_id, surface, started_at, ended_at, detector, confidence, browser_family, platform, schema_version, consent_policy_version) VALUES ('old-span', NULL, 'chatgpt', 'browser', ?, ?, 'selected-browser-tab', 'surface-active', 'chrome', 'linux', 1, 1)`, oldStart, oldEnd); err != nil {
		t.Fatal(err)
	}
	result, code := companionRecordSpan(t, home, companionRecordParams("chatgpt", "browser", "selected-browser-tab", "chrome", "linux", now.Add(-60*time.Second), now.Add(-30*time.Second)))
	if code != 0 {
		t.Fatal(result)
	}
	if countCompanionSpans(t, db) != 1 {
		t.Fatal("expired span was not pruned, or fresh span missing")
	}
	var remaining string
	if err := db.QueryRowContext(context.Background(), `SELECT id FROM tool_activity`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining == "old-span" {
		t.Fatal("old span survived the prune")
	}
}

func TestActivityRecordCreatesMissingStore(t *testing.T) {
	home := companionRecordHome(t)
	now := time.Now().UTC()
	result, code := companionRecordSpan(t, home, companionRecordParams("chatgpt", "browser", "selected-browser-tab", "chrome", "linux", now.Add(-60*time.Second), now.Add(-30*time.Second)))
	// No store existed, so the op creates the schema; collection is off by
	// default, which is the first gate that rejects the span.
	if code != 1 || result["error"] != "companion_collection_disabled" {
		t.Fatalf("missing store: code=%d result=%v", code, result)
	}
	path := companionRecordDBPath(home)
	if _, err := os.Stat(path); err != nil {
		t.Fatal("record op did not create the store file")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("store file permissions = %o, want 0600", info.Mode().Perm())
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tables string
	err = db.QueryRowContext(context.Background(), `SELECT group_concat(name) FROM sqlite_master WHERE type = 'table' AND name IN ('companion_settings', 'companion_tool_consents', 'tool_activity', 'schema_migrations')`).Scan(&tables)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"companion_settings", "companion_tool_consents", "tool_activity", "schema_migrations"} {
		if !strings.Contains(tables, want) {
			t.Fatalf("missing table %s in %s", want, tables)
		}
	}
	var versions string
	err = db.QueryRowContext(context.Background(), `SELECT group_concat(version) FROM schema_migrations`).Scan(&versions)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(versions, "4") || !strings.Contains(versions, "5") {
		t.Fatalf("schema_migrations missing 4/5: %s", versions)
	}
}

func TestActivityRecordAppearsInCapabilities(t *testing.T) {
	result, code := controlCall(t, t.TempDir(), "capabilities", false, map[string]any{})
	if code != 0 {
		t.Fatal(result)
	}
	found := false
	for _, item := range result["result"].([]any) {
		entry := item.(map[string]any)
		if entry["id"] == "activity.record" {
			found = true
			if entry["mutates"] != true || entry["confirmation"] != false {
				t.Fatalf("activity.record capability flags wrong: %v", entry)
			}
		}
	}
	if !found {
		t.Fatal("activity.record missing from capabilities")
	}
}
