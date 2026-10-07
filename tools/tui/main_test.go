package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

type tokscaleProbeResult struct {
	output []byte
	err    error
}

type sequenceTokscaleRunner struct {
	results []tokscaleProbeResult
	calls   []string
}

func (r *sequenceTokscaleRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	if len(r.results) == 0 {
		return nil, errors.New("unexpected Tokscale command")
	}
	result := r.results[0]
	r.results = r.results[1:]
	return result.output, result.err
}

func TestInitialModel(t *testing.T) {
	m := initialModel()
	if m.screen != screenMenu {
		t.Errorf("expected screenMenu, got %d", m.screen)
	}
	if len(m.configKeys) != 5 {
		t.Errorf("expected 5 config keys, got %d", len(m.configKeys))
	}
	if len(m.configLabels) != len(m.configKeys) {
		t.Errorf("configLabels length %d != configKeys length %d", len(m.configLabels), len(m.configKeys))
	}
	if m.localAI != true {
		t.Error("expected localAI default true")
	}
}

func TestBuildInstallSteps(t *testing.T) {
	steps := buildInstallSteps()
	if len(steps) != 6 {
		t.Errorf("expected 6 install steps, got %d", len(steps))
	}
	for i, s := range steps {
		if s.label == "" {
			t.Errorf("step %d has empty label", i)
		}
		if s.status != stepPending {
			t.Errorf("step %d should be pending, got %d", i, s.status)
		}
	}
}

func TestTruncate(t *testing.T) {
	short := "hello"
	if truncate(short, 10) != short {
		t.Error("short string should not be truncated")
	}
	long := "abcdefghij"
	result := truncate(long, 5)
	if result != "abcde\n... (truncated)" {
		t.Errorf("unexpected truncation: %q", result)
	}
}

func TestConfigureMCP_NewFile(t *testing.T) {
	dir := t.TempDir()
	mcpFile := filepath.Join(dir, "settings", "mcp.json")
	serverPath := "/path/to/server.mjs"

	if err := configureMCP(mcpFile, serverPath); err != nil {
		t.Fatalf("configureMCP failed: %v", err)
	}

	data, err := os.ReadFile(mcpFile)
	if err != nil {
		t.Fatalf("failed to read mcp file: %v", err)
	}

	var cfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	entry, ok := cfg.MCPServers["nexus-ollama"]
	if !ok {
		t.Fatal("nexus-ollama entry missing")
	}
	if entry.Command != "node" {
		t.Errorf("expected command 'node', got %q", entry.Command)
	}
	if len(entry.Args) != 1 || entry.Args[0] != serverPath {
		t.Errorf("unexpected args: %v", entry.Args)
	}
}

func TestConfigureMCP_Idempotent(t *testing.T) {
	dir := t.TempDir()
	mcpFile := filepath.Join(dir, "mcp.json")

	_ = configureMCP(mcpFile, "/path/a")

	// Unchanged input is not rewritten.
	data, _ := os.ReadFile(mcpFile)
	before := string(data)
	_ = configureMCP(mcpFile, "/path/a")
	data, _ = os.ReadFile(mcpFile)
	if string(data) != before {
		t.Error("unchanged config was rewritten")
	}

	// Managed fields refresh on a stale entry, but the entry is not duplicated.
	_ = configureMCP(mcpFile, "/path/b")
	data, _ = os.ReadFile(mcpFile)
	var cfg struct {
		MCPServers map[string]struct {
			Args []string `json:"args"`
		} `json:"mcpServers"`
	}
	_ = json.Unmarshal(data, &cfg)
	if len(cfg.MCPServers) != 1 {
		t.Fatalf("expected 1 server entry, got %d", len(cfg.MCPServers))
	}
	if args := cfg.MCPServers["nexus-ollama"].Args; len(args) != 1 || args[0] != "/path/b" {
		t.Errorf("stale args not refreshed: %v", args)
	}
}

func TestConfigureMCP_PreservesExisting(t *testing.T) {
	dir := t.TempDir()
	mcpFile := filepath.Join(dir, "mcp.json")

	existing := `{"mcpServers":{"other-server":{"command":"python","args":["serve.py"]}}}`
	os.WriteFile(mcpFile, []byte(existing), 0644)

	if err := configureMCP(mcpFile, "/path/to/server.mjs"); err != nil {
		t.Fatalf("configureMCP failed: %v", err)
	}

	data, _ := os.ReadFile(mcpFile)
	var cfg struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	_ = json.Unmarshal(data, &cfg)

	if _, ok := cfg.MCPServers["other-server"]; !ok {
		t.Error("existing server entry was lost")
	}
	if _, ok := cfg.MCPServers["nexus-ollama"]; !ok {
		t.Error("nexus-ollama entry not added")
	}
}

func TestConfigureMCP_PreservesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	mcpFile := filepath.Join(dir, "mcp.json")
	existing := `{"customKey":true,"mcpServers":{"other":{"command":"x","env":{"K":"v"},"disabled":true}}}`
	os.WriteFile(mcpFile, []byte(existing), 0644)

	if err := configureMCP(mcpFile, "/s.mjs"); err != nil {
		t.Fatalf("configureMCP failed: %v", err)
	}

	data, _ := os.ReadFile(mcpFile)
	var cfg struct {
		CustomKey  bool `json:"customKey"`
		MCPServers map[string]struct {
			Env      map[string]string `json:"env"`
			Disabled bool              `json:"disabled"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !cfg.CustomKey {
		t.Error("top-level customKey was dropped")
	}
	other := cfg.MCPServers["other"]
	if other.Env["K"] != "v" || !other.Disabled {
		t.Errorf("other server fields were dropped: %+v", other)
	}
}

func TestConfigureMCP_RefusesMalformedFile(t *testing.T) {
	dir := t.TempDir()
	mcpFile := filepath.Join(dir, "mcp.json")
	broken := `{"mcpServers": {broken`
	os.WriteFile(mcpFile, []byte(broken), 0644)

	if err := configureMCP(mcpFile, "/s.mjs"); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
	if data, _ := os.ReadFile(mcpFile); string(data) != broken {
		t.Error("malformed file was overwritten")
	}
}

func TestInstallMCPDepsSkipsWhenInSync(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("{}"), 0644)
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0755)
	os.WriteFile(filepath.Join(dir, "node_modules", ".package-lock.json"), []byte("{}"), 0644)

	got, err := installMCPDeps(dir)
	if err != nil || got != "already installed" {
		t.Fatalf("installMCPDeps = %q, %v; want already installed", got, err)
	}
}

func TestSaveAndLoadEnv(t *testing.T) {
	dir := t.TempDir()
	m := initialModel()
	m.nexusDir = dir
	m.configVals = []string{"false", "http://gpu:11434", "qwen2.5-coder:7b", "llama3.2:3b", "30"}

	if err := saveEnv(m); err != nil {
		t.Fatalf("saveEnv failed: %v", err)
	}

	m2 := initialModel()
	m2.nexusDir = dir
	loadEnv(&m2)

	for i, key := range m.configKeys {
		if m2.configVals[i] != m.configVals[i] {
			t.Errorf("%s: expected %q, got %q", key, m.configVals[i], m2.configVals[i])
		}
	}
	if m2.localAI != false {
		t.Error("expected localAI=false after loading env with NEXUS_LOCAL_AI=false")
	}
}

func TestParseClaudeSessionRetentionDays(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		want  int
		valid bool
	}{
		{name: "default recommendation", raw: "30", want: 30, valid: true},
		{name: "unknown or disabled", raw: "0", want: 0, valid: true},
		{name: "trimmed integer", raw: " 45 ", want: 45, valid: true},
		{name: "negative falls back", raw: "-1", want: defaultClaudeSessionRetention, valid: false},
		{name: "text falls back", raw: "thirty", want: defaultClaudeSessionRetention, valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, valid := parseClaudeSessionRetentionDays(tt.raw)
			if got != tt.want || valid != tt.valid {
				t.Fatalf("parseClaudeSessionRetentionDays(%q) = (%d, %t), want (%d, %t)", tt.raw, got, valid, tt.want, tt.valid)
			}
		})
	}
}

func TestClaudeSessionRetentionConfigFallsBackSafely(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(claudeSessionRetentionKey+"=\"not-a-number\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m := initialModel()
	m.nexusDir = dir
	loadEnv(&m)
	if got := configuredClaudeSessionRetentionDays(m); got != defaultClaudeSessionRetention {
		t.Fatalf("loaded retention = %d, want safe default %d", got, defaultClaudeSessionRetention)
	}

	for i, key := range m.configKeys {
		if key == claudeSessionRetentionKey {
			m.configVals[i] = "-2"
		}
	}
	if err := saveEnv(m); err != nil {
		t.Fatalf("saveEnv failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), claudeSessionRetentionKey+"=\"30\"") {
		t.Fatalf("saved invalid retention without safe fallback: %q", data)
	}
}

func TestConfigureClaudeSessionRetentionValidatesOnEdit(t *testing.T) {
	m := initialModel()
	m.screen = screenConfigure
	for i, key := range m.configKeys {
		if key == claudeSessionRetentionKey {
			m.configCursor = i
		}
	}

	m.configEditing = true
	m.editBuf = "29"
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
	got := updated.(model)
	if got.configEditing || configuredClaudeSessionRetentionDays(got) != 29 {
		t.Fatalf("valid edit retained unexpected state: editing=%t retention=%d", got.configEditing, configuredClaudeSessionRetentionDays(got))
	}

	got.configEditing = true
	got.editBuf = "invalid"
	updated, _ = got.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
	got = updated.(model)
	if got.configEditing || configuredClaudeSessionRetentionDays(got) != defaultClaudeSessionRetention {
		t.Fatalf("invalid edit did not use safe default: editing=%t retention=%d", got.configEditing, configuredClaudeSessionRetentionDays(got))
	}
	if !strings.Contains(got.output, "using default 30 days") {
		t.Fatalf("invalid edit did not explain fallback: %q", got.output)
	}
}

func TestHealthViewClaudeSessionRetentionGuidance(t *testing.T) {
	tests := []struct {
		name     string
		days     int
		want     string
		unwanted string
	}{
		{
			name:     "29 days warns",
			days:     29,
			want:     "NEXUS guidance is 29 days; 30+ days is recommended",
			unwanted: "✓ NEXUS guidance is 29 days",
		},
		{
			name:     "30 days meets guidance",
			days:     30,
			want:     "✓ NEXUS guidance is 30 days",
			unwanted: "30+ days is recommended",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := initialModel()
			m.screen = screenHealth
			m.health.claudeSessionRetentionDays = tt.days
			view := m.View().Content
			if !strings.Contains(view, tt.want) || strings.Contains(view, tt.unwanted) {
				t.Fatalf("health view for %d days = %q", tt.days, view)
			}
			if !strings.Contains(view, "it does not modify Claude's own retention") {
				t.Fatalf("health view did not explain NEXUS-only guidance: %q", view)
			}
		})
	}
}

func TestSafeLink(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.txt")
	target := filepath.Join(dir, "sub", "link.txt")

	os.WriteFile(source, []byte("hello"), 0644)

	if err := safeLink(source, target); err != nil {
		t.Fatalf("safeLink failed: %v", err)
	}

	resolved, err := os.Readlink(target)
	if err != nil {
		t.Fatalf("target is not a symlink: %v", err)
	}
	if resolved != source {
		t.Errorf("symlink points to %q, expected %q", resolved, source)
	}

	// Idempotent — calling again should not error
	if err := safeLink(source, target); err != nil {
		t.Fatalf("safeLink idempotent call failed: %v", err)
	}
}

func TestSafeLink_BacksUpExistingFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.txt")
	target := filepath.Join(dir, "existing.txt")

	os.WriteFile(source, []byte("new"), 0644)
	os.WriteFile(target, []byte("old"), 0644)

	if err := safeLink(source, target); err != nil {
		t.Fatalf("safeLink failed: %v", err)
	}

	// Original should be backed up
	bak, err := os.ReadFile(target + ".bak")
	if err != nil {
		t.Fatal("backup file not created")
	}
	if string(bak) != "old" {
		t.Errorf("backup content: %q, expected 'old'", string(bak))
	}
}

func TestFindNexusDir_EnvOverride(t *testing.T) {
	t.Setenv("NEXUS_REPO", "/custom/path")
	if got := findNexusDir(); got != "/custom/path" {
		t.Errorf("expected /custom/path, got %q", got)
	}
}

func TestMenuNavigation(t *testing.T) {
	m := initialModel()

	// Navigate down
	m2, _ := m.Update(tea.KeyPressMsg{Code: -1, Text: "j"})
	if m2.(model).cursor != 1 {
		t.Errorf("expected cursor 1 after j, got %d", m2.(model).cursor)
	}

	// Navigate up
	m3, _ := m2.Update(tea.KeyPressMsg{Code: -1, Text: "k"})
	if m3.(model).cursor != 0 {
		t.Errorf("expected cursor 0 after k, got %d", m3.(model).cursor)
	}

	// Don't go below 0
	m4, _ := m3.Update(tea.KeyPressMsg{Code: -1, Text: "k"})
	if m4.(model).cursor != 0 {
		t.Errorf("cursor should not go below 0, got %d", m4.(model).cursor)
	}
}

func TestMenuSelectConfigure(t *testing.T) {
	m := initialModel()

	// Move to Configure (index 1)
	m2, _ := m.Update(tea.KeyPressMsg{Code: -1, Text: "j"})
	m3, _ := m2.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})

	if m3.(model).screen != screenConfigure {
		t.Errorf("expected screenConfigure, got %d", m3.(model).screen)
	}
}

func TestMenuSelectUsageDashboardStartsIndependentLoads(t *testing.T) {
	m := initialModel()
	m.cursor = 4

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
	got := updated.(model)
	if got.screen != screenUsageDashboard {
		t.Fatalf("screen = %d, want usage dashboard", got.screen)
	}
	if !got.usageNativeLoading || !got.usageTokscaleLoading || got.usageTokscaleState != tokscaleLoading {
		t.Fatalf("dashboard loading state = native:%t tokscale:%t state:%d", got.usageNativeLoading, got.usageTokscaleLoading, got.usageTokscaleState)
	}
	if cmd == nil {
		t.Fatal("opening dashboard should start non-blocking load commands")
	}
}

func TestDetectTokscaleHealthUsesFixedCommandsAndSafeClientLabels(t *testing.T) {
	runner := &sequenceTokscaleRunner{results: []tokscaleProbeResult{
		{output: []byte("tokscale 1.2.3\n")},
		{output: []byte(`{"groupBy":"model","entries":[
			{"client":"codex","provider":"openai","model":"gpt-5","input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"messageCount":0,"cost":0},
			{"client":"third-party-private-client","provider":"unknown","model":"unknown","input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"messageCount":0,"cost":0},
			{"client":"claude-code","provider":"anthropic","model":"claude","input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"messageCount":0,"cost":0}
		]}`)},
	}}

	got := detectTokscaleHealth(context.Background(), TokscaleAdapter{Runner: runner})
	if got.state != tokscaleHealthReady || got.version != "v1.2.3" {
		t.Fatalf("health = %#v, want ready v1.2.3", got)
	}
	if strings.Join(got.clients, ", ") != "Claude Code, Codex" {
		t.Fatalf("clients = %q", got.clients)
	}
	if strings.Join(runner.calls, " | ") != "tokscale --version | tokscale models --json" {
		t.Fatalf("commands = %q", runner.calls)
	}

	m := initialModel()
	m.screen = screenHealth
	m.health.tokscale = got
	view := m.View().Content
	if strings.Contains(view, "third-party-private-client") {
		t.Fatalf("health view leaked a CLI client identifier: %q", view)
	}
}

func TestDetectTokscaleHealthMakesAbsenceOptionalAndFailureSafe(t *testing.T) {
	missing := detectTokscaleHealth(context.Background(), TokscaleAdapter{Runner: &sequenceTokscaleRunner{results: []tokscaleProbeResult{{err: exec.ErrNotFound}}}})
	if missing.state != tokscaleHealthUnavailable || missing.version != "" || len(missing.clients) != 0 {
		t.Fatalf("missing health = %#v", missing)
	}

	privateMarker := "private Tokscale output"
	broken := detectTokscaleHealth(context.Background(), TokscaleAdapter{Runner: &sequenceTokscaleRunner{results: []tokscaleProbeResult{
		{output: []byte("tokscale 1.2.3")},
		{err: errors.New(privateMarker)},
	}}})
	if broken.state != tokscaleHealthDegraded || broken.version != "v1.2.3" {
		t.Fatalf("broken health = %#v", broken)
	}
	m := initialModel()
	m.screen = screenHealth
	m.health.tokscale = broken
	view := m.View().Content
	if !strings.Contains(view, "Tokscale v1.2.3 installed") || !strings.Contains(view, "local usage aggregates are unavailable") || strings.Contains(view, privateMarker) {
		t.Fatalf("degraded health view is unsafe: %q", view)
	}
}

func TestEscReturnsToMenu(t *testing.T) {
	m := initialModel()
	m.screen = screenConfigure

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
	if m2.(model).screen != screenMenu {
		t.Errorf("expected screenMenu after esc, got %d", m2.(model).screen)
	}
}

func TestUninstallConfirmation(t *testing.T) {
	m := initialModel()
	m.screen = screenUninstall
	m.uninstallConfirmed = false

	// Press 'n' to cancel
	m2, _ := m.Update(tea.KeyPressMsg{Code: -1, Text: "n"})
	if m2.(model).screen != screenMenu {
		t.Error("expected return to menu after 'n' on uninstall confirm")
	}
}

func TestConfigureToggleLocalAI(t *testing.T) {
	m := initialModel()
	m.screen = screenConfigure
	m.configCursor = 0

	// Toggle off
	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
	if m2.(model).configVals[0] != "false" {
		t.Error("expected NEXUS_LOCAL_AI toggled to false")
	}

	// Toggle back on
	m3, _ := m2.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
	if m3.(model).configVals[0] != "true" {
		t.Error("expected NEXUS_LOCAL_AI toggled back to true")
	}
}

func TestUpdateConfigureBackspace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "ASCII",
			input:    "abc",
			expected: "ab",
		},
		{
			name:     "accented",
			input:    "café",
			expected: "caf",
		},
		{
			name:     "CJK",
			input:    "你好世界",
			expected: "你好世",
		},
		{
			name:     "emoji",
			input:    "hello👋",
			expected: "hello",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := initialModel()
			m.screen = screenConfigure
			m.configEditing = true
			m.editBuf = tc.input

			updated, _ := updateConfigure(tea.KeyPressMsg{Code: tea.KeyBackspace, Text: "backspace"}, m)
			got := updated.(model).editBuf
			if got != tc.expected {
				t.Errorf("editBuf after backspace = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestViewsDoNotPanic(t *testing.T) {
	m := initialModel()

	screens := []screen{screenMenu, screenInstall, screenConfigure, screenHealth, screenUninstall, screenUpdate, screenTaskLog, screenUsageDashboard}
	for _, s := range screens {
		m.screen = s
		m.steps = buildInstallSteps()
		// Should not panic
		_ = m.View()
	}
}

func TestUsageDashboardUpdatesSourcesIndependently(t *testing.T) {
	m := initialModel()
	m.screen = screenUsageDashboard
	m.usageNativeLoading = true
	m.usageTokscaleLoading = true
	m.usageTokscaleState = tokscaleLoading

	updated, _ := m.Update(taskLogMsg{entries: []taskLogEntry{{Tool: "ollama_commit_msg", Ok: true, Ms: 12}}})
	got := updated.(model)
	if got.usageNativeLoading || !got.usageTokscaleLoading {
		t.Fatalf("native and Tokscale loading must complete independently: native=%t tokscale=%t", got.usageNativeLoading, got.usageTokscaleLoading)
	}
	if len(got.usageTaskLog) != 1 {
		t.Fatalf("native task entries = %d, want 1", len(got.usageTaskLog))
	}

	updated, _ = got.Update(usageDashboardMsg{
		state:  tokscaleReady,
		report: TokscaleReport{GroupBy: "model", Entries: []TokscaleUsage{{InputTokens: 4, OutputTokens: 5, MessageCount: 1, CostUSD: 0.25}}},
	})
	got = updated.(model)
	if got.usageTokscaleLoading || got.usageTokscaleState != tokscaleReady {
		t.Fatalf("Tokscale state = loading:%t state:%d", got.usageTokscaleLoading, got.usageTokscaleState)
	}
	if got.usageTaskLog[0].Tool != "ollama_commit_msg" || got.usageTokscaleReport.Entries[0].InputTokens != 4 {
		t.Fatal("dashboard did not retain the sources separately")
	}
}

func TestUsageDashboardUnavailableAndDegradedViewsAreSafe(t *testing.T) {
	m := initialModel()
	m.screen = screenUsageDashboard
	m.usageTokscaleState = tokscaleUnavailable
	unavailable := m.View().Content
	if !strings.Contains(unavailable, "Tokscale is not installed") || !strings.Contains(unavailable, "bunx tokscale@latest") {
		t.Fatalf("unavailable view should provide install hint: %q", unavailable)
	}

	msg := loadTokscaleUsage(TokscaleAdapter{Runner: &fakeTokscaleRunner{err: errors.New("private tokscale output")}})()
	updated, _ := m.Update(msg)
	got := updated.(model)
	if got.usageTokscaleState != tokscaleDegraded {
		t.Fatalf("Tokscale failure state = %d, want degraded", got.usageTokscaleState)
	}
	degraded := got.View().Content
	if !strings.Contains(degraded, "Tokscale data is unavailable") || strings.Contains(degraded, "private tokscale output") {
		t.Fatalf("degraded view must be safe: %q", degraded)
	}
}

func TestSummarizeTokscaleUsage(t *testing.T) {
	stats := summarizeTokscaleUsage([]TokscaleUsage{
		{InputTokens: 10, OutputTokens: 20, CacheReadTokens: 30, CacheWriteTokens: 40, ReasoningTokens: 50, MessageCount: 2, CostUSD: 0.25},
		{InputTokens: 1, OutputTokens: 2, CacheReadTokens: 3, CacheWriteTokens: 4, ReasoningTokens: 5, MessageCount: 1, CostUSD: 0.75},
	})
	if stats.aggregates != 2 || stats.inputTokens != 11 || stats.outputTokens != 22 ||
		stats.cacheReadTokens != 33 || stats.cacheWriteTokens != 44 || stats.reasoningTokens != 55 ||
		stats.messages != 3 || math.Abs(stats.costUSD-1) > 0.000001 {
		t.Fatalf("unexpected Tokscale stats: %#v", stats)
	}

	if got := saturatingAddInt64(math.MaxInt64-1, 2); got != math.MaxInt64 {
		t.Errorf("int64 saturation = %d, want %d", got, int64(math.MaxInt64))
	}
}

func TestSummarizeTaskLog(t *testing.T) {
	entries := []taskLogEntry{
		{Tool: "ollama_commit_msg", Model: "qwen2.5-coder:1.5b", Routing: "local", RouteBand: "supervisor", CloudCostEquivalent: 0.001, CostUSD: 0.0002, Ms: 10, Ok: true},
		{Tool: "ollama_boilerplate", Model: "qwen2.5-coder:1.5b", Routing: "local", RouteBand: "supervisor", CloudCostEquivalent: 0.002, Ms: 20, Ok: true},
		{Tool: "ollama_commit_msg", Model: "fast-path", Routing: "deterministic", RouteBand: "fast-path", CloudCostEquivalent: 0.001, Ms: 0, Ok: true},
		{Tool: "ollama_lint_fix", Model: "llama3.2:3b", Routing: "local", RouteBand: "logic", CloudCostEquivalent: 0.003, Ms: 30, Ok: false},
		{Tool: "cloud_delegate", Model: "cloud-model", Routing: "cloud", RouteBand: "cloud", CloudCostEquivalent: 0.005, CostUSD: 0.004, Ms: 40, Ok: true},
	}

	stats := summarizeTaskLog(entries)
	if stats.total != 5 {
		t.Errorf("total: got %d, want 5", stats.total)
	}
	if stats.successes != 4 {
		t.Errorf("successes: got %d, want 4", stats.successes)
	}
	if stats.failures != 1 {
		t.Errorf("failures: got %d, want 1", stats.failures)
	}
	if stats.avgMs != 20 {
		t.Errorf("avgMs: got %d, want 20", stats.avgMs)
	}
	if stats.p95Ms != 40 {
		t.Errorf("p95Ms: got %d, want 40", stats.p95Ms)
	}
	if math.Abs(stats.savingsUSD-0.0068) > 0.000001 {
		t.Errorf("savingsUSD: got %f, want 0.0068", stats.savingsUSD)
	}
	if math.Abs(stats.cloudUSD-0.007) > 0.000001 || math.Abs(stats.costUSD-0.0002) > 0.000001 {
		t.Errorf("cost totals: cloud=%f local=%f", stats.cloudUSD, stats.costUSD)
	}
	if stats.routeBands["supervisor"] != 2 || stats.routeBands["fast-path"] != 1 || stats.routeBands["logic"] != 1 || stats.routeBands["cloud"] != 1 {
		t.Errorf("route bands: %#v", stats.routeBands)
	}
	if stats.modelTasks["qwen2.5-coder:1.5b"] != 2 {
		t.Errorf("qwen count: got %d, want 2", stats.modelTasks["qwen2.5-coder:1.5b"])
	}
	if stats.modelTasks["llama3.2:3b"] != 1 {
		t.Errorf("llama count: got %d, want 1", stats.modelTasks["llama3.2:3b"])
	}
	if stats.routes["local"] != 3 {
		t.Errorf("local route count: got %d, want 3", stats.routes["local"])
	}
	if stats.routes["deterministic"] != 1 {
		t.Errorf("deterministic route count: got %d, want 1", stats.routes["deterministic"])
	}
}

func TestSummarizeIntCounts(t *testing.T) {
	got := summarizeIntCounts(map[string]int{
		"llama3.2:3b":        1,
		"qwen2.5-coder:1.5b": 3,
		"fast-path":          2,
		"qwen2.5:14b":        1,
	}, 2)

	want := "qwen2.5-coder:1.5b (3), fast-path (2), +2 more"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPercentile95(t *testing.T) {
	tests := []struct {
		name   string
		values []int
		want   int
	}{
		{"empty", nil, 0},
		{"single", []int{42}, 42},
		{"unsorted", []int{30, 0, 20, 10}, 30},
		{"hundred", []int{1, 50, 95, 100}, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := percentile95(tt.values); got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBorderBoxClampsWidth(t *testing.T) {
	m := initialModel()

	// Zero width defaults to max 100
	m.width = 0
	out := m.borderBox("test")
	if out == "" {
		t.Error("borderBox returned empty string")
	}

	// Narrow terminal
	m.width = 30
	out = m.borderBox("test")
	if out == "" {
		t.Error("borderBox returned empty string for narrow terminal")
	}
}

func TestGpuInfoString(t *testing.T) {
	tests := []struct {
		name string
		gpu  gpuInfo
		want string
	}{
		{"unknown", gpuInfo{Platform: "unknown"}, "No GPU detected"},
		{"nvidia", gpuInfo{Name: "RTX 3060", MemoryMB: 12288, Platform: "nvidia"}, "RTX 3060 — 12 GB VRAM"},
		{"apple", gpuInfo{Name: "Apple M3 Pro", MemoryMB: 18432, Platform: "apple"}, "Apple M3 Pro — 18 GB unified memory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.gpu.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRecommendedModels(t *testing.T) {
	tests := []struct {
		name    string
		gpu     gpuInfo
		wantSup string
		wantLog string
	}{
		{"4gb", gpuInfo{MemoryMB: 4096, Platform: "nvidia"}, "qwen2.5-coder:1.5b", "llama3.2:3b"},
		{"8gb nvidia", gpuInfo{MemoryMB: 8192, Platform: "nvidia"}, "qwen2.5-coder:7b", "llama3.1:8b"},
		{"8gb apple", gpuInfo{MemoryMB: 8192, Platform: "apple"}, "qwen2.5-coder:3b", "llama3.2:3b"},
		{"16gb", gpuInfo{MemoryMB: 16384, Platform: "nvidia"}, "qwen2.5-coder:7b", "qwen2.5:14b"},
		{"24gb", gpuInfo{MemoryMB: 24576, Platform: "nvidia"}, "qwen2.5-coder:14b", "qwen2.5:32b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sup, logic := tt.gpu.RecommendedModels()
			if sup != tt.wantSup {
				t.Errorf("supervisor: got %q, want %q", sup, tt.wantSup)
			}
			if logic != tt.wantLog {
				t.Errorf("logic: got %q, want %q", logic, tt.wantLog)
			}
		})
	}
}

func TestDetectGPU_ReturnsValidStruct(t *testing.T) {
	// Just verify it doesn't panic and returns a valid platform
	gpu := detectGPU()
	validPlatforms := map[string]bool{"nvidia": true, "amd": true, "apple": true, "unknown": true}
	if !validPlatforms[gpu.Platform] {
		t.Errorf("unexpected platform: %q", gpu.Platform)
	}
}

func TestConfigureMCP_RejectsNullWithoutPanic(t *testing.T) {
	for _, content := range []string{`{`, `null`, `[]`, `{"mcpServers":[]}`, `{"mcpServers":null}`} {
		t.Run(content, func(t *testing.T) {
			dir := t.TempDir()
			mcpFile := filepath.Join(dir, "mcp.json")
			if err := os.WriteFile(mcpFile, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			if err := configureMCP(mcpFile, "/s.mjs"); err == nil {
				t.Fatal("expected an error for null/malformed configuration")
			}
			if data, _ := os.ReadFile(mcpFile); string(data) != content {
				t.Error("configuration was overwritten")
			}
		})
	}
}

func TestSelfUpdateScriptVerifiesInstallSh(t *testing.T) {
	script := selfUpdateScript()
	for _, want := range []string{
		"checksums.txt",
		`$2 == "install.sh"`,
		"install.sh",
		`"${BASE}/checksums.txt"`,
		"shasum -a 256 --check",
		"sha256sum --check",
		// The tag must reach the script as $1, never interpolated into the text.
		`TAG="$1"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("self-update script missing %q", want)
		}
	}
	// v${TAG} is fine: TAG is a shell variable assigned from $1, never a
	// hardcoded version interpolated into the script text.
	for _, bad := range []string{"0.2.1", "latest", `"v"` + "0.2"} {
		if strings.Contains(script, bad) {
			t.Errorf("self-update script interpolates a version: contains %q", bad)
		}
	}
	if !strings.Contains(script, "exit 1") {
		t.Error("self-update script should fail closed when the checksum entry is missing")
	}
}

func TestNormalizeClaudeSessionRetention(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		want  string
		valid bool
	}{
		{name: "valid days", raw: "7", want: "7", valid: true},
		{name: "zero is valid", raw: "0", want: "0", valid: true},
		{name: "trims whitespace", raw: "  45 ", want: "45", valid: true},
		{name: "strips leading zeros", raw: "007", want: "7", valid: true},
		{name: "empty falls back to default", raw: "", want: "30", valid: false},
		{name: "text falls back to default", raw: "thirty", want: "30", valid: false},
		{name: "negative falls back to default", raw: "-2", want: "30", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, valid := normalizeClaudeSessionRetention(tt.raw)
			if got != tt.want || valid != tt.valid {
				t.Fatalf("normalizeClaudeSessionRetention(%q) = (%q, %t), want (%q, %t)",
					tt.raw, got, valid, tt.want, tt.valid)
			}
		})
	}
}

// retentionKeyIndex returns the configVals index of the retention key.
func retentionKeyIndex(m model) int {
	for i, key := range m.configKeys {
		if key == claudeSessionRetentionKey {
			return i
		}
	}
	return -1
}

// TestRetentionNormalizationIsConsistentAcrossTouchPoints guards the exact
// failure mode of the #102 silent merge: four copies of the same fallback
// logic drifting apart. Every touch point must produce the identical
// canonical string for the same raw input.
func TestRetentionNormalizationIsConsistentAcrossTouchPoints(t *testing.T) {
	for _, raw := range []string{"29", "0", "007", " 14 ", "-5", "junk", ""} {
		want, _ := normalizeClaudeSessionRetention(raw)

		// Touch point 1: loadEnv.
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".env"),
			[]byte(claudeSessionRetentionKey+"=\""+raw+"\"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		m := initialModel()
		m.nexusDir = dir
		loadEnv(&m)
		if got := m.configVals[retentionKeyIndex(m)]; got != want {
			t.Errorf("raw %q: loadEnv = %q, want %q", raw, got, want)
		}

		// Touch point 2: saveEnv.
		m2 := initialModel()
		m2.nexusDir = dir
		m2.configVals[retentionKeyIndex(m2)] = raw
		if err := saveEnv(m2); err != nil {
			t.Fatalf("raw %q: saveEnv failed: %v", raw, err)
		}
		data, err := os.ReadFile(filepath.Join(dir, ".env"))
		if err != nil {
			t.Fatal(err)
		}
		if wantLine := claudeSessionRetentionKey + "=\"" + want + "\""; !strings.Contains(string(data), wantLine) {
			t.Errorf("raw %q: saveEnv wrote %q, want line %q", raw, data, wantLine)
		}

		// Touch point 3: updateConfigure commit branch.
		m3 := initialModel()
		m3.screen = screenConfigure
		m3.configCursor = retentionKeyIndex(m3)
		m3.configEditing = true
		m3.editBuf = raw
		updated, _ := m3.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
		got3 := updated.(model)
		if got3.configVals[retentionKeyIndex(got3)] != want {
			t.Errorf("raw %q: updateConfigure commit = %q, want %q",
				raw, got3.configVals[retentionKeyIndex(got3)], want)
		}

		// Touch point 4: configuredClaudeSessionRetentionDays.
		m4 := initialModel()
		m4.configVals[retentionKeyIndex(m4)] = raw
		wantDays, _ := strconv.Atoi(want)
		if got := configuredClaudeSessionRetentionDays(m4); got != wantDays {
			t.Errorf("raw %q: configuredClaudeSessionRetentionDays = %d, want %d", raw, got, wantDays)
		}
	}
}

func TestIsCriticalInstallStep(t *testing.T) {
	for idx := 0; idx <= 5; idx++ {
		if got := isCriticalInstallStep(idx); got != (idx <= 3) {
			t.Errorf("isCriticalInstallStep(%d) = %t, want %t", idx, got, idx <= 3)
		}
	}
}

// withTempHome points /home/cano at a fresh temp dir for the duration of the test.
func withTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

// withIsolatedPath empties PATH so exec.LookPath cannot find real tools
// (ollama, claude, npm); steps must degrade gracefully, never shell out.
func withIsolatedPath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// makeRepoLayout creates the repo paths the installer validates/links.
func makeRepoLayout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, req := range []string{"core/NEXUS.md", "core/CLAUDE.md", "core/kiro-nexus-steering.md", "personas", "tools"} {
		p := filepath.Join(dir, req)
		if strings.HasSuffix(req, ".md") {
			if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
				t.Fatal(err)
			}
		} else if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// makeMCPDepsReady marks the MCP server deps as already installed so step 3
// never invokes npm.
func makeMCPDepsReady(t *testing.T, nexusDir string) {
	t.Helper()
	mcpDir := filepath.Join(nexusDir, "tools", "mcp")
	nmDir := filepath.Join(mcpDir, "node_modules")
	if err := os.MkdirAll(nmDir, 0755); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(mcpDir, "package-lock.json")
	installed := filepath.Join(nmDir, ".package-lock.json")
	if err := os.WriteFile(lock, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	// installed marker must be newer than the lockfile.
	past := time.Now().Add(-time.Hour)
	recent := time.Now()
	if err := os.Chtimes(lock, past, past); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(installed, recent, recent); err != nil {
		t.Fatal(err)
	}
}

func TestInstallStepValidateRepo(t *testing.T) {
	dir := makeRepoLayout(t)
	m := initialModel()
	m.nexusDir = dir

	msg := installStepValidateRepo(0, m)
	if !msg.ok || msg.idx != 0 || msg.detail != dir {
		t.Fatalf("valid repo: got %+v", msg)
	}

	if err := os.Remove(filepath.Join(dir, "core", "NEXUS.md")); err != nil {
		t.Fatal(err)
	}
	msg = installStepValidateRepo(0, m)
	if msg.ok || msg.detail != "missing core/NEXUS.md" {
		t.Fatalf("missing file: got %+v", msg)
	}
}

func TestInstallStepSymlinkCoreFiles(t *testing.T) {
	home := withTempHome(t)
	dir := makeRepoLayout(t)
	m := initialModel()
	m.nexusDir = dir

	msg := installStepSymlinkCoreFiles(1, m)
	if !msg.ok || msg.idx != 1 || msg.detail != "3 core files linked" {
		t.Fatalf("got %+v", msg)
	}
	for _, link := range []struct{ dst, src string }{
		{filepath.Join(home, ".gemini", "GEMINI.md"), filepath.Join(dir, "core/NEXUS.md")},
		{filepath.Join(home, ".claude", "CLAUDE.md"), filepath.Join(dir, "core/CLAUDE.md")},
		{filepath.Join(home, ".kiro", "steering", "nexus-orchestrator.md"), filepath.Join(dir, "core/kiro-nexus-steering.md")},
	} {
		target, err := os.Readlink(link.dst)
		if err != nil {
			t.Fatalf("readlink %s: %v", link.dst, err)
		}
		if target != link.src {
			t.Errorf("%s -> %s, want %s", link.dst, target, link.src)
		}
	}
}

func TestInstallStepSymlinkConfigDirs(t *testing.T) {
	home := withTempHome(t)
	dir := makeRepoLayout(t)
	m := initialModel()
	m.nexusDir = dir

	msg := installStepSymlinkConfigDirs(2, m)
	if !msg.ok || msg.idx != 2 || msg.detail != "5 directories linked" {
		t.Fatalf("got %+v", msg)
	}
	for _, d := range []string{"core", "personas", "tools", "prompts", "agent-memory"} {
		dst := filepath.Join(home, ".config", "nexus", d)
		target, err := os.Readlink(dst)
		if err != nil {
			t.Fatalf("readlink %s: %v", dst, err)
		}
		if want := filepath.Join(dir, d); target != want {
			t.Errorf("%s -> %s, want %s", dst, target, want)
		}
	}
}

func TestInstallStepConfigureMCP(t *testing.T) {
	home := withTempHome(t)
	withIsolatedPath(t) // no claude CLI: must report skipped, never shell out
	dir := makeRepoLayout(t)
	makeMCPDepsReady(t, dir)
	m := initialModel()
	m.nexusDir = dir

	msg := installStepConfigureMCP(3, m)
	if !msg.ok || msg.idx != 3 {
		t.Fatalf("got %+v", msg)
	}
	if !strings.Contains(msg.detail, "nexus-ollama → Kiro, Gemini") {
		t.Fatalf("detail missing Kiro+Gemini coverage: %q", msg.detail)
	}
	if !strings.Contains(msg.detail, "deps already installed") {
		t.Fatalf("detail missing deps status: %q", msg.detail)
	}
	for _, f := range []string{
		filepath.Join(home, ".kiro", "settings", "mcp.json"),
		filepath.Join(home, ".gemini", "config", "mcp_config.json"),
	} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if !strings.Contains(string(data), "nexus-ollama") {
			t.Errorf("%s missing nexus-ollama entry: %s", f, data)
		}
	}
}

func TestInstallStepCheckDependencies(t *testing.T) {
	m := initialModel()
	msg := installStepCheckDependencies(4, m)
	if !msg.ok || msg.idx != 4 {
		t.Fatalf("got %+v", msg)
	}
	if !strings.HasPrefix(msg.detail, "found: ") {
		t.Fatalf("detail has unexpected shape: %q", msg.detail)
	}
}

func TestInstallStepPullOllamaModelsSkipsWithoutOllama(t *testing.T) {
	withIsolatedPath(t)
	m := initialModel()
	msg := installStepPullOllamaModels(5, m)
	if !msg.ok || msg.idx != 5 || msg.detail != "skipped (ollama not installed)" {
		t.Fatalf("got %+v", msg)
	}
}

// TestRunInstallStepDispatchesAllSteps proves the dispatcher still reaches
// every extracted worker and preserves the legacy out-of-range fallback.
func TestRunInstallStepDispatchesAllSteps(t *testing.T) {
	withTempHome(t)
	withIsolatedPath(t)
	dir := makeRepoLayout(t)
	makeMCPDepsReady(t, dir)
	m := initialModel()
	m.nexusDir = dir

	for idx := 0; idx <= 5; idx++ {
		raw := runInstallStep(m, idx)()
		msg, ok := raw.(stepDoneMsg)
		if !ok {
			t.Fatalf("step %d: message is %T, want stepDoneMsg", idx, raw)
		}
		if msg.idx != idx {
			t.Errorf("step %d: message idx = %d", idx, msg.idx)
		}
		if !msg.ok {
			t.Errorf("step %d: not ok: %+v", idx, msg)
		}
	}

	raw := runInstallStep(m, 99)()
	msg, ok := raw.(stepDoneMsg)
	if !ok || !msg.ok || msg.idx != 99 {
		t.Fatalf("out-of-range idx: got %+v (ok=%t)", raw, ok)
	}
}

func TestMergeMCPConfigPreservesUnknownKeysAndExtras(t *testing.T) {
	in := []byte(`{"customKey":true,"nested":{"a":[1,2]},"mcpServers":{"other":{"command":"python","args":["serve.py"],"env":{"K":"v"},"cwd":"/tmp","disabled":true}}}`)
	out, err := mergeMCPConfig(in, "/s.mjs")
	if err != nil {
		t.Fatalf("mergeMCPConfig failed: %v", err)
	}
	var cfg struct {
		CustomKey  bool                       `json:"customKey"`
		Nested     map[string][]int           `json:"nested"`
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !cfg.CustomKey {
		t.Error("top-level customKey was dropped")
	}
	if len(cfg.Nested["a"]) != 2 {
		t.Errorf("top-level nested key was dropped: %v", cfg.Nested)
	}
	var other map[string]json.RawMessage
	if err := json.Unmarshal(cfg.MCPServers["other"], &other); err != nil {
		t.Fatalf("other server unparseable: %v", err)
	}
	for _, key := range []string{"command", "args", "env", "cwd", "disabled"} {
		if _, ok := other[key]; !ok {
			t.Errorf("other server lost key %q", key)
		}
	}
	var entry struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal(cfg.MCPServers["nexus-ollama"], &entry); err != nil {
		t.Fatalf("nexus-ollama entry unparseable: %v", err)
	}
	if entry.Command != "node" || len(entry.Args) != 1 || entry.Args[0] != "/s.mjs" {
		t.Errorf("unexpected nexus-ollama entry: %+v", entry)
	}
}

func TestMergeMCPConfigUpdatesStaleEntryPreservingExtras(t *testing.T) {
	in := []byte(`{"mcpServers":{"nexus-ollama":{"command":"node","args":["/old.mjs"],"env":{"KEEP":"1"},"cwd":"/work"}}}`)
	out, err := mergeMCPConfig(in, "/new.mjs")
	if err != nil {
		t.Fatalf("mergeMCPConfig failed: %v", err)
	}
	var cfg struct {
		MCPServers map[string]map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	entry := cfg.MCPServers["nexus-ollama"]
	var args []string
	if err := json.Unmarshal(entry["args"], &args); err != nil || len(args) != 1 || args[0] != "/new.mjs" {
		t.Errorf("managed args not refreshed: %s", entry["args"])
	}
	var cmd string
	if err := json.Unmarshal(entry["command"], &cmd); err != nil || cmd != "node" {
		t.Errorf("managed command not canonical: %s", entry["command"])
	}
	var env map[string]string
	if err := json.Unmarshal(entry["env"], &env); err != nil || env["KEEP"] != "1" {
		t.Errorf("per-entry env extra was dropped: %s", entry["env"])
	}
	var cwd string
	if err := json.Unmarshal(entry["cwd"], &cwd); err != nil || cwd != "/work" {
		t.Errorf("per-entry cwd extra was dropped: %s", entry["cwd"])
	}
}

func TestMergeMCPConfigIdempotentByteIdentical(t *testing.T) {
	first, err := mergeMCPConfig(nil, "/s.mjs")
	if err != nil {
		t.Fatalf("mergeMCPConfig failed: %v", err)
	}
	second, err := mergeMCPConfig(first, "/s.mjs")
	if err != nil {
		t.Fatalf("second mergeMCPConfig failed: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Error("idempotent merge rewrote byte-identical input")
	}
}

// TestMergeMCPConfigRefusesBadInput re-runs the #114 fail-closed guards
// against the refactored mergeMCPConfig: null/non-object documents and
// mcpServers values must be refused, never coerced.
func TestMergeMCPConfigRefusesBadInput(t *testing.T) {
	cases := []struct{ name, in string }{
		{"null document", `null`},
		{"top-level array", `[]`},
		{"top-level string", `"hello"`},
		{"top-level number", `42`},
		{"mcpServers null", `{"mcpServers": null}`},
		{"mcpServers string", `{"mcpServers": "junk"}`},
		{"mcpServers array", `{"mcpServers": []}`},
		{"unparseable", `{"mcpServers": {broken`},
		{"entry not an object", `{"mcpServers": {"nexus-ollama": "junk"}}`},
		{"entry null", `{"mcpServers": {"nexus-ollama": null}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if out, err := mergeMCPConfig([]byte(tc.in), "/s.mjs"); err == nil {
				t.Errorf("expected refusal, got output: %s", out)
			}
		})
	}
}

func TestMergeMCPConfigBlankInputYieldsFreshConfig(t *testing.T) {
	for _, in := range []string{"", "   \n  "} {
		out, err := mergeMCPConfig([]byte(in), "/s.mjs")
		if err != nil {
			t.Fatalf("input %q: %v", in, err)
		}
		var cfg struct {
			MCPServers map[string]struct {
				Command string `json:"command"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal(out, &cfg); err != nil {
			t.Fatalf("input %q: invalid JSON: %v", in, err)
		}
		if cfg.MCPServers["nexus-ollama"].Command != "node" {
			t.Errorf("input %q: missing nexus-ollama entry", in)
		}
	}
}

func TestConfigureMCPWritesOnlyOnChange(t *testing.T) {
	dir := t.TempDir()
	mcpFile := filepath.Join(dir, "mcp.json")
	if err := configureMCP(mcpFile, "/s.mjs"); err != nil {
		t.Fatalf("configureMCP failed: %v", err)
	}
	before, err := os.ReadFile(mcpFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := configureMCP(mcpFile, "/s.mjs"); err != nil {
		t.Fatalf("second configureMCP failed: %v", err)
	}
	after, err := os.ReadFile(mcpFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("unchanged config was rewritten")
	}
	// A stale server path is refreshed (managed fields), extras preserved.
	stale := []byte(`{"mcpServers":{"nexus-ollama":{"command":"node","args":["/old.mjs"],"env":{"K":"v"}}}}`)
	if err := os.WriteFile(mcpFile, stale, 0644); err != nil {
		t.Fatal(err)
	}
	if err := configureMCP(mcpFile, "/new.mjs"); err != nil {
		t.Fatalf("configureMCP failed: %v", err)
	}
	data, _ := os.ReadFile(mcpFile)
	if !strings.Contains(string(data), `"/new.mjs"`) {
		t.Errorf("stale args not refreshed: %s", data)
	}
	if !strings.Contains(string(data), `"K":"v"`) && !strings.Contains(string(data), `"K": "v"`) {
		t.Errorf("per-entry extras dropped: %s", data)
	}
}

// sharedScriptPath resolves a repo-root scripts/ file from the tools/tui
// test working directory.
func sharedScriptPath(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "..", "scripts", name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("shared script %s not present: %v", name, err)
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not installed")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// TestSharedMCPMergeScriptConformance proves the exact scripts/setup-nexus.sh
// calls conform to the canonical merge spec.
func TestSharedMCPMergeScriptConformance(t *testing.T) {
	script := sharedScriptPath(t, "mcp-merge.js")
	serverPath := "/x/server.mjs"

	run := func(configPath string) (string, int, string) {
		cmd := exec.Command("node", script, configPath, serverPath)
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				t.Fatalf("node failed: %v", err)
			}
		}
		return strings.TrimSpace(string(out)), code, configPath
	}

	t.Run("round trip preserves unknown keys and extras", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "mcp.json")
		in := `{"customKey":true,"mcpServers":{"other":{"command":"x","env":{"A":"b"},"cwd":"/w"},"nexus-ollama":{"command":"node","args":["/old.mjs"],"env":{"KEEP":"1"}}}}`
		if err := os.WriteFile(f, []byte(in), 0644); err != nil {
			t.Fatal(err)
		}
		if out, code, _ := run(f); code != 0 || out != "updated" {
			t.Fatalf("merge = %q, code %d", out, code)
		}
		data, _ := os.ReadFile(f)
		var cfg struct {
			CustomKey  bool                       `json:"customKey"`
			MCPServers map[string]json.RawMessage `json:"mcpServers"`
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Fatal(err)
		}
		if !cfg.CustomKey {
			t.Error("customKey dropped")
		}
		if !strings.Contains(string(cfg.MCPServers["other"]), `"A":"b"`) &&
			!strings.Contains(string(cfg.MCPServers["other"]), `"A": "b"`) {
			t.Error("other server extras dropped")
		}
		var entry map[string]json.RawMessage
		_ = json.Unmarshal(cfg.MCPServers["nexus-ollama"], &entry)
		var args []string
		_ = json.Unmarshal(entry["args"], &args)
		if len(args) != 1 || args[0] != serverPath {
			t.Errorf("args not refreshed: %v", args)
		}
		var env map[string]string
		_ = json.Unmarshal(entry["env"], &env)
		if env["KEEP"] != "1" {
			t.Error("entry extras dropped on update")
		}
	})

	t.Run("fail closed on null config", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "mcp.json")
		if err := os.WriteFile(f, []byte("null"), 0644); err != nil {
			t.Fatal(err)
		}
		_, code, _ := run(f)
		if code == 0 {
			t.Fatal("expected non-zero exit for null config")
		}
		if data, _ := os.ReadFile(f); string(data) != "null" {
			t.Errorf("null config was modified: %q", data)
		}
	})
}

// TestSharedMCPRemoveScriptConformance proves the exact
// scripts/teardown-nexus.sh calls conform to the canonical remove spec.
func TestSharedMCPRemoveScriptConformance(t *testing.T) {
	script := sharedScriptPath(t, "mcp-remove.js")

	run := func(configPath string) (string, int) {
		cmd := exec.Command("node", script, configPath)
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				t.Fatalf("node failed: %v", err)
			}
		}
		return strings.TrimSpace(string(out)), code
	}

	t.Run("removes only our entry", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "mcp.json")
		in := `{"custom":1,"mcpServers":{"nexus-ollama":{"command":"node","args":["/s.mjs"]},"other":{"command":"x"}}}`
		if err := os.WriteFile(f, []byte(in), 0644); err != nil {
			t.Fatal(err)
		}
		if out, code := run(f); code != 0 || out != "removed" {
			t.Fatalf("remove = %q, code %d", out, code)
		}
		data, _ := os.ReadFile(f)
		if strings.Contains(string(data), "nexus-ollama") {
			t.Errorf("entry not removed: %s", data)
		}
		if !strings.Contains(string(data), `"custom"`) {
			t.Errorf("unknown top-level key dropped: %s", data)
		}
		if !strings.Contains(string(data), `"other"`) {
			t.Errorf("other server dropped: %s", data)
		}
	})

	t.Run("fail closed on malformed mcpServers", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "mcp.json")
		in := `{"mcpServers": "junk"}`
		if err := os.WriteFile(f, []byte(in), 0644); err != nil {
			t.Fatal(err)
		}
		_, code := run(f)
		if code == 0 {
			t.Fatal("expected non-zero exit for non-object mcpServers")
		}
		if data, _ := os.ReadFile(f); string(data) != in {
			t.Errorf("file was modified or deleted: %q", data)
		}
		if _, err := os.Stat(f); err != nil {
			t.Errorf("file was deleted on refusal: %v", err)
		}
	})
}
