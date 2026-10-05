package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMCPRetainsUnownedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	before := `{"theme":"dark","mcpServers":{"remote":{"url":"https://example.invalid","headers":{"Authorization":"fixture-token"},"disabled":true},"local":{"command":"node","env":{"CUSTOM":"value"}}}}`
	if err := os.WriteFile(path, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	if err := configureMCP(path, "/test/server.mjs"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var want, got map[string]any
	if err := json.Unmarshal([]byte(before), &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after, &got); err != nil {
		t.Fatal(err)
	}
	delete(got["mcpServers"].(map[string]any), "nexus-ollama")
	if !reflect.DeepEqual(want, got) {
		t.Fatal("unowned configuration changed")
	}
}

func TestMCPRejectsMalformedWithoutWriting(t *testing.T) {
	for _, content := range []string{`{`, `null`, `[]`, `{"mcpServers":[]}`, `{"mcpServers":null}`} {
		t.Run(content, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "mcp.json")
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if err := configureMCP(path, "/test/server.mjs"); err == nil {
				t.Fatal("expected error")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != content {
				t.Fatal("malformed configuration overwritten")
			}
		})
	}
}

func TestLocalStatusOmitsPrivateContent(t *testing.T) {
	home := t.TempDir()
	if got := localStatus(home).Personas.State; got != "missing" {
		t.Fatalf("state = %s", got)
	}
	dir := filepath.Join(home, ".config", "nexus", "personas")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "private-persona.md"), []byte("private-content"), 0600); err != nil {
		t.Fatal(err)
	}
	status := localStatus(home)
	if status.Personas.Count != 1 || status.Personas.State != "available" {
		t.Fatal(status.Personas)
	}
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{home, "private-persona", "private-content"} {
		if strings.Contains(string(data), private) {
			t.Fatal("status disclosed private data")
		}
	}
}
