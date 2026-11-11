package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderRoutePanelDisplayOnly(t *testing.T) {
	d := classifyPrompt("write a conventional commit message")
	got := renderRoutePanel(d, t.TempDir())
	for _, want := range []string{"Lead session", "Fork layer", "confidence 0.95", "Cheap swarm", "planned, not run", "Advisor side panel", "silent on routine turns", "Memory vault: 0 projects, 0 entries"} {
		if !strings.Contains(got, want) {
			t.Fatalf("panel missing %q:\n%s", want, got)
		}
	}
	root := t.TempDir()
	now := time.Date(2026, 10, 6, 12, 30, 0, 0, time.UTC)
	if _, err := saveMemory(root, "demo", "Panel note", "Body", nil, now); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(root, "demo"), 0700)
	got = renderRoutePanel(d, root)
	if !strings.Contains(got, "Memory vault: 1 projects, 1 entries") {
		t.Fatalf("vault count wrong:\n%s", got)
	}
	low := classifyPrompt("help me improve this application")
	if !strings.Contains(renderRoutePanel(low, t.TempDir()), "would escalate to a model (not automatic yet)") {
		t.Fatal("low-confidence panel should flag escalation as not automatic")
	}
}
