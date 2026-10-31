package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type diagnosticTransport func(*http.Request) (*http.Response, error)

func (f diagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func diagnosticFixture(t *testing.T) (string, string) {
	t.Helper()
	dir, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NEXUS_LOCAL_AI", "true")
	t.Setenv("OLLAMA_HOST_URL", "http://localhost:11434")
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	if err := os.WriteFile(filepath.Join(bin, "node"), []byte("#!/bin/sh\nif [ \"$1\" = --version ]; then echo v22.13.0; else read line; echo '{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2024-11-05\",\"serverInfo\":{\"name\":\"fixture\"}}}'; fi\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, leaf := range []string{"core", "personas", "tools", "prompts", "agent-memory"} {
		os.MkdirAll(filepath.Join(dir, leaf), 0700)
		os.MkdirAll(filepath.Join(home, ".config", "nexus"), 0700)
		os.Symlink(filepath.Join(dir, leaf), filepath.Join(home, ".config", "nexus", leaf))
	}
	for _, item := range []struct{ src, dst string }{{"NEXUS.md", ".gemini/GEMINI.md"}, {"CLAUDE.md", ".claude/CLAUDE.md"}, {"kiro-nexus-steering.md", ".kiro/steering/nexus-orchestrator.md"}} {
		src := filepath.Join(dir, "core", item.src)
		dst := filepath.Join(home, item.dst)
		os.WriteFile(src, []byte("fixture"), 0600)
		os.MkdirAll(filepath.Dir(dst), 0700)
		if item.src == "kiro-nexus-steering.md" {
			copy, err := kiroSteeringCopy(src)
			if err != nil {
				t.Fatal(err)
			}
			os.WriteFile(dst, copy, 0600)
		} else {
			os.Symlink(src, dst)
		}
	}
	return dir, home
}
func TestDiagnosticsHealthyAndReadOnly(t *testing.T) {
	dir, home := diagnosticFixture(t)
	t.Setenv("NEXUS_MODEL_COMMIT_MSG", "custom:latest")
	t.Setenv("NEXUS_SUPERVISOR_MODEL", "band:latest")
	old := diagnosticHTTPClient
	t.Cleanup(func() { diagnosticHTTPClient = old })
	diagnosticHTTPClient = &http.Client{Transport: diagnosticTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/tags" {
			t.Errorf("path %s", req.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"models":[{"name":"custom:latest"},{"name":"band:latest"},{"name":"llama3.2:3b"}]}`))}, nil
	})}
	r := collectDiagnostics(dir, home)
	if !r.Healthy || !r.MCP.Startable || r.MCP.Running != nil || !r.Ollama.ModelsReady {
		t.Fatalf("report %+v", r)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "nexus", "logs")); !os.IsNotExist(err) {
		t.Fatal("created observability state")
	}
	if r.Tokscale.Installed || !r.Tokscale.Optional {
		t.Fatal("optional executable")
	}
	os.Remove(filepath.Join(home, ".gemini", "GEMINI.md"))
	os.Symlink(filepath.Join(dir, "core", "CLAUDE.md"), filepath.Join(home, ".gemini", "GEMINI.md"))
	if collectDiagnostics(dir, home).Healthy {
		t.Fatal("wrong target accepted")
	}
}
func TestDiagnosticsSecretsAndDisabled(t *testing.T) {
	dir, home := diagnosticFixture(t)
	t.Setenv("NEXUS_LOCAL_AI", "false")
	t.Setenv("OLLAMA_HOST_URL", "https://user:SUPERSECRET@example.com/path?token=SUPERSECRET")
	t.Setenv("NEXUS_MODEL_COMMIT_MSG", "SUPERSECRET?invalid")
	old := diagnosticHTTPClient
	t.Cleanup(func() { diagnosticHTTPClient = old })
	diagnosticHTTPClient = &http.Client{Transport: diagnosticTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("disabled network request")
		return nil, context.Canceled
	})}
	var out, stderr bytes.Buffer
	if code := runDiagnosticCLI([]string{"health", "--json"}, dir, home, &out, &stderr); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(out.String(), "SUPERSECRET") {
		t.Fatal("secret leaked")
	}
	var r diagnosticReport
	if json.Unmarshal(out.Bytes(), &r) != nil || r.SchemaVersion != 1 || r.Ollama.Enabled || r.MCP.Running != nil {
		t.Fatal(out.String())
	}
	if runDiagnosticCLI([]string{"doctor", "--bad"}, dir, home, &out, &stderr) != 2 {
		t.Fatal("usage exit")
	}
}
func TestDiagnosticsMCPRejectsMissingHandshake(t *testing.T) {
	dir, _ := diagnosticFixture(t)
	bin := os.Getenv("PATH")
	os.WriteFile(filepath.Join(bin, "node"), []byte("#!/bin/sh\necho '{}'\n"), 0700)
	if probeDiagnosticMCP(dir) {
		t.Fatal("non MCP response accepted")
	}
}

func TestDiagnosticsRejectsMalformedNodeVersion(t *testing.T) {
	dir, home := diagnosticFixture(t)
	t.Setenv("NEXUS_LOCAL_AI", "false")
	os.WriteFile(filepath.Join(os.Getenv("PATH"), "node"), []byte("#!/bin/sh\necho v22.13.junk\n"), 0700)
	r := collectDiagnostics(dir, home)
	for _, check := range r.Checks {
		if check.Name == "node" {
			if check.State != "failed" {
				t.Fatal("accepted malformed node version")
			}
			return
		}
	}
	t.Fatal("node check absent")
}
