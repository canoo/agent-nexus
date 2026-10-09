package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryCLI(t *testing.T) {
	root := t.TempDir()
	var out, stderr bytes.Buffer
	run := func(args []string, input string) string {
		t.Helper()
		out.Reset()
		stderr.Reset()
		if code := runMemoryCLI(args, root, strings.NewReader(input), &out, &stderr); code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, stderr.String())
		}
		return out.String()
	}
	run([]string{"init", "demo"}, "")
	path := strings.TrimSpace(run([]string{"save", "demo", "--title", "Stdin", "--tags", "go, tui"}, "Refactor from stdin."))
	if !strings.Contains(run([]string{"show", "demo", filepath.Base(path)}, ""), "Refactor from stdin.") {
		t.Fatal("stdin missing")
	}
	file := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(file, []byte("Imported file"), 0600); err != nil {
		t.Fatal(err)
	}
	run([]string{"save", "demo", "--title", "Imported", "--file", file}, "")
	run([]string{"save", "demo", "--title", "Inline", "--body", "Inline body"}, "")
	if got := run([]string{"list"}, ""); !strings.Contains(got, "demo\t4 files") {
		t.Fatal(got)
	}
	if got := run([]string{"show", "demo"}, ""); !strings.Contains(got, "Imported file") || !strings.Contains(got, "Inline body") {
		t.Fatal(got)
	}
	if got := run([]string{"search", "REFACTOR"}, ""); !strings.Contains(got, "Refactor from stdin.") {
		t.Fatal(got)
	}
	for _, args := range [][]string{{}, {"unknown"}, {"list", "extra"}, {"show"}, {"show", "../x"}, {"save", "demo"}, {"save", "demo", "--title", "X", "--body", "", "--file", file}, {"search", ""}, {"init", ".."}} {
		stderr.Reset()
		if code := runMemoryCLI(args, root, strings.NewReader(""), &out, &stderr); code != 2 || !strings.Contains(stderr.String(), "Usage:") {
			t.Fatalf("%v: %d %s", args, code, stderr.String())
		}
	}
	if code := runMemoryCLI([]string{"show", "missing"}, root, strings.NewReader(""), &out, &stderr); code != 1 {
		t.Fatalf("missing: %d", code)
	}
}
