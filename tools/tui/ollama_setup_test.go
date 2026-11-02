package main

import (
	"strings"
	"testing"
)

func TestIsLocalOllamaURL(t *testing.T) {
	locals := []string{"", defaultOllamaURL, "http://127.0.0.1:11434", "http://[::1]:11434/"}
	for _, raw := range locals {
		if !isLocalOllamaURL(raw) {
			t.Fatalf("expected local: %q", raw)
		}
	}
	remotes := []string{"http://10.0.0.99:11434", "http://gpu.example:11434", "not a url"}
	for _, raw := range remotes {
		if isLocalOllamaURL(raw) {
			t.Fatalf("expected remote: %q", raw)
		}
	}
}

func TestEnsureOllamaReadyFailsWithoutBinaryOrInstaller(t *testing.T) {
	withIsolatedPath(t)
	err := ensureOllamaReady(defaultOllamaURL)
	if err == nil {
		t.Fatal("expected error when ollama and installers are absent from PATH")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ollama not installed") {
		t.Fatalf("missing not-installed prefix: %q", msg)
	}
	if !strings.Contains(msg, "https://ollama.com") {
		t.Fatalf("missing install URL: %q", msg)
	}
}

func TestEnsureOllamaReadyRemoteDoesNotInstall(t *testing.T) {
	withIsolatedPath(t)
	err := ensureOllamaReady("http://10.0.0.99:11434")
	if err == nil {
		t.Fatal("expected remote unreachable error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "remote Ollama") {
		t.Fatalf("expected remote guidance, got %q", msg)
	}
	if strings.Contains(msg, "ollama not installed") {
		t.Fatalf("must not attempt local install for remote URL: %q", msg)
	}
}

func TestOllamaReachableFalseForBadURL(t *testing.T) {
	if ollamaReachable("http://127.0.0.1:1") {
		t.Fatal("expected unreachable host to report false")
	}
}

func TestTruncateOutput(t *testing.T) {
	short := truncateOutput([]byte("ok"))
	if short != "ok" {
		t.Fatalf("short: %q", short)
	}
	long := strings.Repeat("a", 300)
	got := truncateOutput([]byte(long))
	if !strings.HasSuffix(got, "…") || !strings.HasPrefix(got, strings.Repeat("a", 240)) {
		t.Fatalf("long truncate: %q", got)
	}
}
