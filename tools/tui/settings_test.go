package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsPreserveLegacyAndRejectInjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	original := "# private legacy settings\nOTHER='leave me alone'\nNEXUS_LOCAL_AI=false\nNEXUS_LOCAL_AI=true\n"
	os.WriteFile(path, []byte(original), 0600)
	if err := writeSettings(path, []string{"NEXUS_LOCAL_AI"}, []string{"false"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# private legacy settings\nOTHER='leave me alone'\n") || strings.Count(string(data), "NEXUS_LOCAL_AI=\"false\"") != 2 {
		t.Fatal(string(data))
	}
	if err := writeSettings(path, []string{"NEXUS_LOCAL_AI"}, []string{"true\nINJECT=yes"}); err == nil {
		t.Fatal("accepted newline")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(data) {
		t.Fatal("failed save changed file")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("settings not private")
	}
}
func TestSettingsPrecedenceAndLiteralParsing(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte(" NEXUS_LOCAL_AI = 'false'\r\nNEXUS_SUPERVISOR_MODEL=first\nNEXUS_SUPERVISOR_MODEL=last\n"), 0600)
	m := model{nexusDir: dir, configKeys: []string{"NEXUS_LOCAL_AI", "NEXUS_SUPERVISOR_MODEL"}, configVals: []string{"true", "default"}}
	t.Setenv("NEXUS_SUPERVISOR_MODEL", "override")
	loadEnv(&m)
	if m.localAI || m.configVals[1] != "override" {
		t.Fatal(m.configVals)
	}
	t.Setenv("NEXUS_SUPERVISOR_MODEL", "")
	loadEnv(&m)
	if m.configVals[1] != settingDefaults["NEXUS_SUPERVISOR_MODEL"] {
		t.Fatal(m.configVals)
	}
	if parseSettings("KEY='$(touch /invalid)'\n")["KEY"] != "$(touch /invalid)" {
		t.Fatal("literal changed")
	}
}

func TestSettingsRejectSymlinkWithoutChangingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	path := filepath.Join(dir, ".env")
	os.WriteFile(target, []byte("original"), 0600)
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := writeSettings(path, []string{"NEXUS_LOCAL_AI"}, []string{"false"}); err == nil {
		t.Fatal("accepted symlink")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "original" {
		t.Fatal("target changed")
	}
}
