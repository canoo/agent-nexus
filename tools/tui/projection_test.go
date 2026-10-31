package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func putInstruction(t *testing.T, root, path, body string) {
	t.Helper()
	p := filepath.Join(root, path)
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(body), 0644); e != nil {
		t.Fatal(e)
	}
}
func getInstruction(t *testing.T, root, path string) string {
	t.Helper()
	b, e := os.ReadFile(filepath.Join(root, path))
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func TestProjectAdoptSync(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	putInstruction(t, root, "CLAUDE.md", "Use small functions.\n")
	putInstruction(t, root, ".gemini/settings.json", `{"secret":"not instructions"}`)
	for _, d := range []string{".kiro", ".cursor"} {
		if e := os.Mkdir(filepath.Join(root, d), 0755); e != nil {
			t.Fatal(e)
		}
	}
	var out bytes.Buffer
	if e := projectInstructions(root, "adopt", true, &out); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(root, ".nexus")); !os.IsNotExist(e) {
		t.Fatal("dry run wrote canonical state")
	}
	if !strings.Contains(out.String(), "would write AGENTS.md") {
		t.Fatal(out.String())
	}
	if e := projectInstructions(root, "adopt", false, &out); e != nil {
		t.Fatal(e)
	}
	canonical := getInstruction(t, root, instructionPath)
	if !strings.Contains(canonical, "Imported from CLAUDE.md") || strings.Contains(canonical, "secret") {
		t.Fatal(canonical)
	}
	original := getInstruction(t, root, "CLAUDE.md")
	if !strings.HasPrefix(original, "Use small functions.\n") {
		t.Fatal("original text lost")
	}
	var state projectionState
	if e := json.Unmarshal([]byte(getInstruction(t, root, projectionStatePath)), &state); e != nil {
		t.Fatal(e)
	}
	if len(state.Sources) != 1 || state.Sources[0].SHA256 == "" {
		t.Fatal(state)
	}
	if !strings.HasPrefix(getInstruction(t, root, ".kiro/steering/nexus-project.md"), "---\ninclusion: always\n---") {
		t.Fatal("missing Kiro metadata")
	}
	if !strings.HasPrefix(getInstruction(t, root, ".cursor/rules/nexus-project.mdc"), "---\nalwaysApply: true\n---") {
		t.Fatal("missing Cursor metadata")
	}
	before := getInstruction(t, root, projectionStatePath)
	if e := projectInstructions(root, "sync", false, &out); e != nil {
		t.Fatal(e)
	}
	if before != getInstruction(t, root, projectionStatePath) || original != getInstruction(t, root, "CLAUDE.md") {
		t.Fatal("sync not idempotent")
	}
	// AGENTS was created by adoption and has no outside text; it is not a new source.
	if e := projectInstructions(root, "adopt", false, &out); e != nil {
		t.Fatal(e)
	}
	putInstruction(t, root, instructionPath, canonical+"\nNew canonical policy.\n")
	if e := projectInstructions(root, "sync", false, &out); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(getInstruction(t, root, "GEMINI.md"), "New canonical policy.") {
		t.Fatal("canonical updates not projected")
	}
	if !strings.Contains(getInstruction(t, root, "CLAUDE.md"), "Use small functions.\n") {
		t.Fatal("original lost after update")
	}
}
func TestProjectionConflictsPreflight(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	putInstruction(t, root, "AGENTS.md", "User policy\n")
	var out bytes.Buffer
	if e := projectInstructions(root, "adopt", false, &out); e != nil {
		t.Fatal(e)
	}
	canonical := getInstruction(t, root, instructionPath)
	agents := getInstruction(t, root, "AGENTS.md")
	putInstruction(t, root, "AGENTS.md", strings.Replace(agents, "# Project instructions", "# Edited managed content", 1))
	putInstruction(t, root, instructionPath, canonical+"New policy\n")
	manifest := getInstruction(t, root, projectionStatePath)
	if e := projectInstructions(root, "sync", false, &out); e == nil || !strings.Contains(e.Error(), "edited") {
		t.Fatalf("expected ownership conflict: %v", e)
	}
	if manifest != getInstruction(t, root, projectionStatePath) {
		t.Fatal("conflict modified state")
	}
	putInstruction(t, root, "AGENTS.md", agents+"Changed outside instructions\n")
	if e := projectInstructions(root, "adopt", false, &out); e == nil || !strings.Contains(e.Error(), "source instructions changed") {
		t.Fatalf("expected source conflict: %v", e)
	}
	if canonical+"New policy\n" != getInstruction(t, root, instructionPath) {
		t.Fatal("adopt overwrote canonical edits")
	}
}
func TestProjectionRejectsSymlinksAndMalformedMarkers(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	outside := t.TempDir()
	putInstruction(t, outside, "policy.md", "Private policy")
	if e := os.Symlink(filepath.Join(outside, "policy.md"), filepath.Join(root, "AGENTS.md")); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e := projectInstructions(root, "adopt", false, &out); e == nil || !strings.Contains(e.Error(), "symlink") {
		t.Fatal(e)
	}
	if getInstruction(t, outside, "policy.md") != "Private policy" {
		t.Fatal("symlink modified")
	}
	if e := os.Remove(filepath.Join(root, "AGENTS.md")); e != nil {
		t.Fatal(e)
	}
	putInstruction(t, root, "AGENTS.md", projectionStart+"\nunclosed")
	if e := projectInstructions(root, "adopt", false, &out); e == nil {
		t.Fatal("accepted malformed block")
	}
}
func TestProjectionRejectsUnownedRuleAndJSONOnly(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	putInstruction(t, root, ".gemini/settings.json", `{"model":"x"}`)
	var out bytes.Buffer
	if e := projectInstructions(root, "adopt", false, &out); e == nil {
		t.Fatal("adopted JSON settings")
	}
	putInstruction(t, root, "AGENTS.md", "Project policy")
	putInstruction(t, root, ".cursor/rules/nexus-project.mdc", "---\nglobs: '*.go'\n---\nScoped policy")
	if e := projectInstructions(root, "adopt", false, &out); e == nil {
		t.Fatal("overwrote scoped user rule")
	}
	if _, e := os.Stat(filepath.Join(root, instructionPath)); !os.IsNotExist(e) {
		t.Fatal("preflight conflict wrote canonical")
	}
}
func TestProjectionCLIValidation(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var out bytes.Buffer
	if code := runProjectionCLI("sync", []string{"--dir", t.TempDir()}, &out, &out); code != 1 {
		t.Fatal(code)
	}
	if code := runProjectionCLI("adopt", []string{"extra"}, &out, &out); code != 2 {
		t.Fatal(code)
	}
}

func TestProjectionLateConflictMakesNoWrites(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	putInstruction(t, root, "AGENTS.md", "Original AGENTS\n")
	putInstruction(t, root, "CLAUDE.md", "Original Claude\n")
	var out bytes.Buffer
	if e := projectInstructions(root, "adopt", false, &out); e != nil {
		t.Fatal(e)
	}
	agents := getInstruction(t, root, "AGENTS.md")
	manifest := getInstruction(t, root, projectionStatePath)
	canonical := getInstruction(t, root, instructionPath) + "\nRevised policy\n"
	putInstruction(t, root, instructionPath, canonical)
	claude := strings.Replace(getInstruction(t, root, "CLAUDE.md"), "# Project instructions", "# Manually edited", 1)
	putInstruction(t, root, "CLAUDE.md", claude)
	if e := projectInstructions(root, "sync", false, &out); e == nil {
		t.Fatal("expected late target conflict")
	}
	if agents != getInstruction(t, root, "AGENTS.md") || manifest != getInstruction(t, root, projectionStatePath) || canonical != getInstruction(t, root, instructionPath) || claude != getInstruction(t, root, "CLAUDE.md") {
		t.Fatal("preflight conflict mutated files")
	}
}
func TestProjectionCanonicalDryRunThenIdempotent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	putInstruction(t, root, "AGENTS.md", "Original\n")
	var out bytes.Buffer
	if e := projectInstructions(root, "adopt", false, &out); e != nil {
		t.Fatal(e)
	}
	agents := getInstruction(t, root, "AGENTS.md")
	manifest := getInstruction(t, root, projectionStatePath)
	putInstruction(t, root, instructionPath, getInstruction(t, root, instructionPath)+"\nNew policy\n")
	if e := projectInstructions(root, "sync", true, &out); e != nil {
		t.Fatal(e)
	}
	if agents != getInstruction(t, root, "AGENTS.md") || manifest != getInstruction(t, root, projectionStatePath) {
		t.Fatal("dry run changed files")
	}
	if e := projectInstructions(root, "sync", false, &out); e != nil {
		t.Fatal(e)
	}
	agents = getInstruction(t, root, "AGENTS.md")
	manifest = getInstruction(t, root, projectionStatePath)
	out.Reset()
	if e := projectInstructions(root, "sync", false, &out); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out.String(), "wrote ") || agents != getInstruction(t, root, "AGENTS.md") || manifest != getInstruction(t, root, projectionStatePath) {
		t.Fatal("canonical update not idempotent")
	}
}

func TestProjectionRemovedOwnedTargetConflicts(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	putInstruction(t, root, "CLAUDE.md", "Project policy\n")
	var out bytes.Buffer
	if e := projectInstructions(root, "adopt", false, &out); e != nil {
		t.Fatal(e)
	}
	if e := os.Remove(filepath.Join(root, "CLAUDE.md")); e != nil {
		t.Fatal(e)
	}
	if e := projectInstructions(root, "sync", false, &out); e == nil || !strings.Contains(e.Error(), "removed") {
		t.Fatalf("expected removed target conflict: %v", e)
	}
	if _, e := os.Stat(filepath.Join(root, "CLAUDE.md")); !os.IsNotExist(e) {
		t.Fatal("silently recreated removed target")
	}
}

func TestProjectionArchivesAndValidation(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	putInstruction(t, root, "CLAUDE.md", "Original policy\n")
	putInstruction(t, root, ".cursor/rules/always.mdc", "---\nalwaysApply: true\n---\nAlways test changes.\n")
	putInstruction(t, root, ".cursor/rules/scoped.mdc", "---\nglobs: '*.go'\nalwaysApply: false\n---\nGo-only policy.\n")
	putInstruction(t, root, ".kiro/steering/project.md", "---\ninclusion: always\n---\nKiro project policy.\n")
	putInstruction(t, root, ".claude/settings.json", `{"env":{"TOKEN":"never-copy-this"}}`)
	var out bytes.Buffer
	if e := projectInstructions(root, "adopt", false, &out); e != nil {
		t.Fatal(e)
	}
	canonical := getInstruction(t, root, instructionPath)
	if !strings.Contains(canonical, "Always test changes.") || !strings.Contains(canonical, "Kiro project policy.") || strings.Contains(canonical, "Go-only policy") || strings.Contains(canonical, "never-copy-this") || strings.Contains(canonical, "alwaysApply:") {
		t.Fatal(canonical)
	}
	if getInstruction(t, root, ".nexus/.adopted/CLAUDE.md") != "Original policy\n" {
		t.Fatal("original not archived")
	}
	if getInstruction(t, root, ".nexus/.adopted/.cursor/rules/always.mdc") != "---\nalwaysApply: true\n---\nAlways test changes.\n" {
		t.Fatal("rule original not archived intact")
	}
	if _, e := os.Stat(filepath.Join(root, ".nexus/.adopted/.claude/settings.json")); !os.IsNotExist(e) {
		t.Fatal("settings archived")
	}
	report := getInstruction(t, root, "AGENTS.validation.json")
	if !strings.Contains(report, "scoped.mdc") || !strings.Contains(report, "settings.json") || strings.Contains(report, "never-copy-this") {
		t.Fatal(report)
	}
	if !strings.Contains(out.String(), "review .cursor/rules/scoped.mdc") {
		t.Fatal("missing visible unsupported warning")
	}
	if e := projectInstructions(root, "adopt", false, &out); e != nil {
		t.Fatal(e)
	}
}
func TestProjectionSnapshotRollback(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	putInstruction(t, root, "AGENTS.md", "Original\n")
	var out bytes.Buffer
	if e := projectInstructions(root, "adopt", false, &out); e != nil {
		t.Fatal(e)
	}
	originalProjected := getInstruction(t, root, "AGENTS.md")
	state := getInstruction(t, root, projectionStatePath)
	canonical := getInstruction(t, root, instructionPath) + "\nUpdated instructions\n"
	putInstruction(t, root, instructionPath, canonical)
	if e := projectInstructions(root, "sync", false, &out); e != nil {
		t.Fatal(e)
	}
	changed := getInstruction(t, root, "AGENTS.md")
	if originalProjected == changed {
		t.Fatal("test did not update projection")
	}
	if e := rollbackInstructions(root, true, &out); e != nil {
		t.Fatal(e)
	}
	if changed != getInstruction(t, root, "AGENTS.md") {
		t.Fatal("rollback dry run mutated file")
	}
	if e := rollbackInstructions(root, false, &out); e != nil {
		t.Fatal(e)
	}
	if originalProjected != getInstruction(t, root, "AGENTS.md") || state != getInstruction(t, root, projectionStatePath) {
		t.Fatal("rollback did not restore targets and ownership")
	}
	if canonical != getInstruction(t, root, instructionPath) {
		t.Fatal("rollback modified externally edited source")
	}
	if e := rollbackInstructions(root, false, &out); e == nil {
		t.Fatal("snapshot reused")
	}
	if e := projectInstructions(root, "sync", false, &out); e != nil {
		t.Fatal(e)
	}
	putInstruction(t, root, "AGENTS.md", getInstruction(t, root, "AGENTS.md")+"User edit\n")
	if e := rollbackInstructions(root, false, &out); e == nil || !strings.Contains(e.Error(), "local edits") {
		t.Fatalf("rollback should preserve local edit: %v", e)
	}
}
func TestProjectionInitialAdoptionRollback(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	putInstruction(t, root, "CLAUDE.md", "Original bytes\n")
	var out bytes.Buffer
	if e := projectInstructions(root, "adopt", false, &out); e != nil {
		t.Fatal(e)
	}
	if e := rollbackInstructions(root, false, &out); e != nil {
		t.Fatal(e)
	}
	if getInstruction(t, root, "CLAUDE.md") != "Original bytes\n" {
		t.Fatal("original not restored")
	}
	for _, path := range []string{instructionPath, projectionStatePath, "AGENTS.md", "AGENTS.validation.json"} {
		if _, e := os.Stat(filepath.Join(root, path)); !os.IsNotExist(e) {
			t.Fatalf("rollback failed to remove %s", path)
		}
	}
	if !strings.Contains(getInstruction(t, root, ".nexus/.gitignore"), ".snapshots/\n.adopted/") {
		t.Fatal("rollback exposed retained private backups to Git")
	}
	if getInstruction(t, root, ".nexus/.adopted/CLAUDE.md") != "Original bytes\n" {
		t.Fatal("rollback removed archival safety copy")
	}
}
func TestProjectionRollbackRejectsTraversal(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	putInstruction(t, root, latestInstructionSnapshot, `{"version":1,"files":[{"path":"../escape","expected_sha256":"x"}]}`)
	var out bytes.Buffer
	if e := rollbackInstructions(root, false, &out); e == nil || !strings.Contains(e.Error(), "invalid snapshot path") {
		t.Fatal(e)
	}
}

func TestProjectionPrivacyAndIgnorePreservation(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	putInstruction(t, root, "AGENTS.md", "Original policy\n")
	putInstruction(t, root, ".gitignore", "root-rule\n")
	putInstruction(t, root, ".nexus/.gitignore", "local-rule\n")
	var out bytes.Buffer
	if e := projectInstructions(root, "adopt", false, &out); e != nil {
		t.Fatal(e)
	}
	if getInstruction(t, root, ".gitignore") != "root-rule\n" {
		t.Fatal("root gitignore modified")
	}
	if getInstruction(t, root, ".nexus/.gitignore") != "local-rule\n.snapshots/\n.adopted/\n" {
		t.Fatal("local gitignore not preserved")
	}
	for _, path := range []string{".nexus/.adopted", ".nexus/.snapshots"} {
		info, e := os.Stat(filepath.Join(root, path))
		if e != nil {
			t.Fatal(e)
		}
		if info.Mode().Perm() != 0700 {
			t.Fatalf("private directory mode %s: %v", path, info.Mode())
		}
	}
	for _, path := range []string{".nexus/.adopted/AGENTS.md", latestInstructionSnapshot} {
		info, e := os.Stat(filepath.Join(root, path))
		if e != nil {
			t.Fatal(e)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("private file mode %s: %v", path, info.Mode())
		}
	}
	putInstruction(t, root, "AGENTS.validation.json", "User replacement report\n")
	if e := projectInstructions(root, "sync", false, &out); e == nil || !strings.Contains(e.Error(), "refusing overwrite") {
		t.Fatalf("unowned report not protected: %v", e)
	}
}
