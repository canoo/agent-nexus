package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestInitialModel(t *testing.T) {
	m := initialModel()
	if m.screen != screenMenu {
		t.Errorf("expected screenMenu, got %d", m.screen)
	}
	if len(m.configKeys) != 4 {
		t.Errorf("expected 4 config keys, got %d", len(m.configKeys))
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
	_ = configureMCP(mcpFile, "/path/b") // should not overwrite

	data, _ := os.ReadFile(mcpFile)
	var cfg struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	_ = json.Unmarshal(data, &cfg)

	if len(cfg.MCPServers) != 1 {
		t.Errorf("expected 1 server entry, got %d", len(cfg.MCPServers))
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

func TestSaveAndLoadEnv(t *testing.T) {
	dir := t.TempDir()
	m := initialModel()
	m.nexusDir = dir
	m.configVals = []string{"false", "http://gpu:11434", "qwen2.5-coder:7b", "llama3.2:3b"}

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

func TestViewsDoNotPanic(t *testing.T) {
	m := initialModel()

	screens := []screen{screenMenu, screenInstall, screenConfigure, screenHealth, screenUninstall, screenUpdate, screenTaskLog}
	for _, s := range screens {
		m.screen = s
		m.steps = buildInstallSteps()
		// Should not panic
		_ = m.View()
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