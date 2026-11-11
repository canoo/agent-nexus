package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMemoryDecisionRoundtrip(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 10, 19, 0, 0, 0, time.UTC)
	path, err := saveDecision(root, "demo", "JWT over sessions", "Chose JWT for auth because the API needs to be stateless for horizontal scaling.", []string{"cookie sessions (stateful)", "OAuth (too heavy for internal API)"}, now)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "title: JWT over sessions") || !strings.Contains(content, "tags:\n    - decision") {
		t.Fatalf("decision frontmatter: %s", content)
	}
	if !strings.Contains(content, "Ruled out: cookie sessions (stateful); OAuth (too heavy for internal API).") {
		t.Fatalf("ruled out missing: %s", content)
	}
	if got := filepath.Base(path); !strings.HasPrefix(got, "20261110-190000-jwt-over-sessions.md") {
		t.Fatalf("filename: %s", got)
	}
	// A plain note must not show up in the decision list.
	if _, err := saveMemory(root, "demo", "Unrelated note", "Just a note.", nil, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	decisions, err := listDecisions(root, "demo", 0)
	if err != nil || len(decisions) != 1 {
		t.Fatalf("listDecisions: %v %v", decisions, err)
	}
	if decisions[0].Title != "JWT over sessions" || decisions[0].Date != now.Format(time.RFC3339) {
		t.Fatalf("decision entry: %+v", decisions[0])
	}
	rendered := formatDecisions(decisions)
	want := "## 2026-11-10 — JWT over sessions\nChose JWT for auth because the API needs to be stateless for horizontal scaling.\n\nRuled out: cookie sessions (stateful); OAuth (too heavy for internal API).\n"
	if rendered != want {
		t.Fatalf("format:\n%s\nwant:\n%s", rendered, want)
	}
	// Newest first, limit applies.
	if _, err := saveDecision(root, "demo", "Postgres over SQLite", "SQLite insufficient for concurrent writes.", nil, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	decisions, err = listDecisions(root, "demo", 1)
	if err != nil || len(decisions) != 1 || decisions[0].Title != "Postgres over SQLite" {
		t.Fatalf("limit: %v %v", decisions, err)
	}
	if _, err := saveDecision(root, "demo", "", "why", nil, now); err == nil {
		t.Fatal("empty what accepted")
	}
	if _, err := saveDecision(root, "demo", "what", "", nil, now); err == nil {
		t.Fatal("empty why accepted")
	}
}

func TestMemoryDecisionCLI(t *testing.T) {
	root := t.TempDir()
	var out, stderr bytes.Buffer
	run := func(args []string) (string, int) {
		t.Helper()
		out.Reset()
		stderr.Reset()
		code := runMemoryCLI(args, root, strings.NewReader(""), &out, &stderr)
		return out.String(), code
	}
	if got, code := run([]string{"decisions", "demo"}); code != 1 {
		t.Fatalf("decisions on missing project: %d %s", code, got)
	}
	path, code := run([]string{"decision", "demo", "--what", "JWT over sessions", "--why", "Stateless scaling.", "--ruled-out", "cookie sessions, OAuth"})
	if code != 0 {
		t.Fatalf("decision: %d %s", code, stderr.String())
	}
	path = strings.TrimSpace(path)
	if !strings.HasSuffix(path, "-jwt-over-sessions.md") {
		t.Fatalf("decision path: %s", path)
	}
	// The decision is a regular memory too: visible via show/search.
	if got, _ := run([]string{"show", "demo"}); !strings.Contains(got, "JWT over sessions") {
		t.Fatalf("show: %s", got)
	}
	got, code := run([]string{"decisions", "demo"})
	if code != 0 {
		t.Fatalf("decisions: %d %s", code, stderr.String())
	}
	today := time.Now().Format("2006-01-02")
	if !strings.Contains(got, "## "+today+" — JWT over sessions") || !strings.Contains(got, "Ruled out: cookie sessions; OAuth.") {
		t.Fatalf("decisions output: %s", got)
	}
	if _, code := run([]string{"decision", "demo", "--what", "x", "--ruled-out", "y"}); code != 2 {
		t.Fatalf("missing --why: %d", code)
	}
	if _, code := run([]string{"decision", "demo", "--why", "y"}); code != 2 {
		t.Fatalf("missing --what: %d", code)
	}
	if _, code := run([]string{"decision", "../x", "--what", "x", "--why", "y"}); code != 2 {
		t.Fatalf("bad project: %d", code)
	}
	if _, code := run([]string{"decisions", "demo", "--limit", "-1"}); code != 2 {
		t.Fatalf("negative limit: %d", code)
	}
	// Empty project reports no decisions instead of erroring.
	if _, err := initProject(root, "empty"); err != nil {
		t.Fatal(err)
	}
	if got, code := run([]string{"decisions", "empty"}); code != 0 || !strings.Contains(got, "No decisions logged yet") {
		t.Fatalf("empty decisions: %d %s", code, got)
	}
}
