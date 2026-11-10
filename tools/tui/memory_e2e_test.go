package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Exercise main's real dispatch and home resolution with an isolated home.
func TestMemoryBinaryEndToEnd(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "nexus")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	home := t.TempDir()
	environment := []string{}
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "HOME=") {
			environment = append(environment, value)
		}
	}
	environment = append(environment, "HOME="+home)
	run := func(args ...string) string {
		t.Helper()
		command := exec.Command(binary, args...)
		command.Env = environment
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, output)
		}
		quoted := make([]string, len(args))
		for i, arg := range args {
			if strings.ContainsAny(arg, " \t\n\"") {
				quoted[i] = strconv.Quote(arg)
			} else {
				quoted[i] = arg
			}
		}
		t.Logf("$ nexus %s\n%s", strings.Join(quoted, " "), output)
		return string(output)
	}
	run("memory", "init", "agent-nexus")
	first := strings.TrimSpace(run("memory", "save", "agent-nexus", "--title", "v0.3.0 TUI refactor notes", "--body", "The TUI screen-file refactor moves configure, hardware, health, and install into dedicated Go files. The root model keeps shared state and delegates each screen to its update and view functions. Project Memory follows the same pattern in memory.go."))
	run("memory", "save", "agent-nexus", "--title", "Project memory system launch", "--body", "Project memory now ships with list, show, save, search, and init CLI commands backed by Markdown files. The TUI adds project switching, previews, full-file reading, inline creation, filtering, and confirmed deletion. Agents can keep reading the existing memory directory as a fallback.")
	if output := run("memory", "list"); !strings.Contains(output, "agent-nexus\t3 files") {
		t.Fatal(output)
	}
	if output := run("memory", "show", "agent-nexus"); !strings.Contains(output, "Project memory system launch") {
		t.Fatal(output)
	}
	if output := run("memory", "search", "refactor"); !strings.Contains(output, "screen-file refactor") {
		t.Fatal(output)
	}
	if output := run("memory", "show", "agent-nexus", strings.TrimSuffix(filepath.Base(first), ".md")); !strings.Contains(output, "title:") || !strings.Contains(output, "dedicated Go files") {
		t.Fatal(output)
	}
	invalid := exec.Command(binary, "memory", "save", "agent-nexus")
	invalid.Env = environment
	output, err := invalid.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 2 || !strings.Contains(string(output), "Usage:") {
		t.Fatalf("usage: %v %s", err, output)
	}
}
