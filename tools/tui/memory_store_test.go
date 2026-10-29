package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMemoryRoundtrip(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 6, 12, 30, 0, 0, time.UTC)
	path, err := saveMemory(root, "demo", "Quoted: \"title\"\nsecond line", "\nReal refactor notes.\n", []string{"go", "tui"}, now)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := listMemories(root, "demo")
	if err != nil || len(entries) != 1 {
		t.Fatalf("list: %v %v", entries, err)
	}
	e, err := readMemory(root, "demo", strings.TrimSuffix(filepath.Base(path), ".md"))
	if err != nil {
		t.Fatal(err)
	}
	if e.Title != "Quoted: \"title\"\nsecond line" || e.Date != now.Format(time.RFC3339) || e.Preview != "Real refactor notes." || !strings.Contains(e.Content, "tags:") {
		t.Fatalf("roundtrip: %+v", e)
	}
	projects, err := listProjects(root)
	if err != nil || len(projects) != 1 || projects[0].Count != 1 || !projects[0].Modified.Equal(e.Modified) {
		t.Fatalf("projects: %v %v", projects, err)
	}
	matches, err := searchMemories(root, "REFACTOR")
	if err != nil || len(matches) != 1 || matches[0].Project != "demo" || matches[0].Line != "Real refactor notes." {
		t.Fatalf("search: %v %v", matches, err)
	}
	second, err := saveMemory(root, "demo", e.Title, "Another body", nil, now)
	if err != nil || second == path {
		t.Fatalf("collision: %s %v", second, err)
	}
	original, _ := readMemory(root, "demo", filepath.Base(path))
	if original.Content != e.Content {
		t.Fatal("overwrote memory")
	}
}
func TestMemoryInitIdempotent(t *testing.T) {
	root := t.TempDir()
	path, err := initProject(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("user edits"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = initProject(root, "demo"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "user edits" {
		t.Fatal("init replaced existing README")
	}
	if err = os.Mkdir(filepath.Join(root, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	projects, err := listProjects(root)
	if err != nil || len(projects) != 2 || projects[1].Count != 0 || !projects[1].Modified.IsZero() {
		t.Fatalf("empty project: %v %v", projects, err)
	}
}
func TestMemoryParsing(t *testing.T) {
	cases := []struct{ content, title, date, preview string }{
		{"---\ntitle: 'Decision: one'\ndate: 2026-10-06\ntags: [go, tui]\n---\n\nBody", "Decision: one", "2026-10-06", "Body"},
		{"---\r\ntitle: Test\r\n---\r\n\r\nBody", "Test", "", "Body"},
		{"\n# Heading\n\nBody", "Heading", "", "# Heading"},
		{"\nPlain body", "note.md", "", "Plain body"},
		{"---\ntitle: [broken\n---\nBody", "note.md", "", "---"},
		{"---\ntitle: unterminated\nBody", "note.md", "", "---"},
	}
	for _, c := range cases {
		e := parseMemory("note.md", c.content)
		if e.Title != c.title || e.Date != c.date || e.Preview != c.preview {
			t.Errorf("%q: %+v", c.content, e)
		}
	}
}
func TestMemorySlug(t *testing.T) {
	for title, want := range map[string]string{"v0.3.0 TUI refactor notes": "v0-3-0-tui-refactor-notes", "  Hello / WORLD!  ": "hello-world", "???": "memory", "Déjà vu": "déjà-vu"} {
		if got := memorySlug(title); got != want {
			t.Errorf("%q: %q != %q", title, got, want)
		}
	}
	if len([]rune(memorySlug(strings.Repeat("é", 100)))) != 80 {
		t.Fatal("slug length")
	}
}
func TestMemoryPathValidation(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../escape", "/absolute", "..", "", "a\\b"} {
		if _, err := saveMemory(root, name, "title", "body", nil, time.Now()); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	if _, err := initProject(root, "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := readMemory(root, "demo", "../escape"); err == nil {
		t.Fatal("read traversal")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := saveMemory(root, "linked", "title", "body", nil, time.Now()); err == nil {
		t.Fatal("followed project symlink")
	}
	if err := os.Symlink(filepath.Join(outside, "missing.md"), filepath.Join(root, "demo", "link.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := readMemory(root, "demo", "link"); err == nil {
		t.Fatal("followed file symlink")
	}
}
