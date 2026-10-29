package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// mergeMCPConfig merges the nexus-ollama server entry into raw MCP config
// JSON and returns the updated document. It implements the canonical MCP
// merge spec shared with scripts/mcp-merge.js (see the spec comment there):
//
//   - unknown top-level keys are preserved;
//   - other servers are untouched;
//   - when the nexus-ollama entry already exists, only the managed fields
//     (command, args) are updated; per-entry extras (env, cwd, ...) are kept;
//   - fail-closed: blank input yields a fresh config, but unparseable JSON,
//     a null or non-object top-level value, or a present-but-not-object
//     mcpServers value is an error and nothing is written;
//   - when nothing changes, the input is returned byte-identical so the
//     caller can skip the write.
//
// The #114 nil-map guards live here: a null document or a null mcpServers is
// refused, never coerced into an empty object (writing into the nil map
// produced by json.Unmarshal of `null` would panic).
func mergeMCPConfig(data []byte, serverPath string) ([]byte, error) {
	const serverName = "nexus-ollama"

	// Decode into raw maps so top-level keys and other servers' fields (env,
	// disabled, autoApprove, ...) survive the rewrite untouched.
	cfg := map[string]json.RawMessage{}
	servers := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("refusing to merge unparseable MCP config: %w", err)
		}
		if cfg == nil {
			return nil, errors.New("refusing to merge null MCP config")
		}
		if raw, ok := cfg["mcpServers"]; ok {
			if err := json.Unmarshal(raw, &servers); err != nil {
				return nil, fmt.Errorf("refusing to merge MCP config: mcpServers is not an object: %w", err)
			}
			if servers == nil {
				return nil, errors.New("refusing to merge MCP config: mcpServers is not an object")
			}
		}
	}

	docChanged := false
	if raw, exists := servers[serverName]; exists {
		entry := map[string]json.RawMessage{}
		if err := json.Unmarshal(raw, &entry); err != nil || entry == nil {
			return nil, fmt.Errorf("refusing to merge MCP config: %q entry is not an object", serverName)
		}
		// Managed fields are always canonical; per-entry extras survive.
		// Compared semantically (not by raw bytes): our own pretty-printer
		// expands arrays across lines, so byte comparison would never settle.
		var entryCommand string
		var entryArgs []string
		_ = json.Unmarshal(entry["command"], &entryCommand)
		_ = json.Unmarshal(entry["args"], &entryArgs)
		if entryCommand != "node" || len(entryArgs) != 1 || entryArgs[0] != serverPath {
			entry["command"], _ = json.Marshal("node")
			entry["args"], _ = json.Marshal([]string{serverPath})
			docChanged = true
		}
		merged, err := json.Marshal(entry)
		if err != nil {
			return nil, err
		}
		servers[serverName] = merged
	} else {
		entry, err := json.Marshal(struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		}{Command: "node", Args: []string{serverPath}})
		if err != nil {
			return nil, err
		}
		servers[serverName] = entry
		docChanged = true
	}

	if !docChanged {
		return data, nil // byte-identical: nothing to write
	}
	serversRaw, err := json.Marshal(servers)
	if err != nil {
		return nil, err
	}
	cfg["mcpServers"] = serversRaw
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func configureMCP(mcpFile, serverPath string) error {
	if err := os.MkdirAll(filepath.Dir(mcpFile), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(mcpFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	merged, err := mergeMCPConfig(data, serverPath)
	if err != nil {
		return fmt.Errorf("%s: %w", mcpFile, err)
	}
	if bytes.Equal(merged, data) {
		return nil // unchanged
	}
	return atomicWriteFile(mcpFile, merged, 0600)
}

// configureClaudeMCP registers nexus-ollama at Claude Code's user scope via its
// CLI. ~/.claude.json holds Claude's own state, so it is never edited directly.
// Returns skipped=true when the claude CLI is not installed.
func configureClaudeMCP(serverPath string) (skipped bool, err error) {
	if _, err := exec.LookPath("claude"); err != nil {
		return true, nil
	}
	if exec.Command("claude", "mcp", "get", "nexus-ollama").Run() == nil {
		return false, nil
	}
	if out, err := exec.Command("claude", "mcp", "add", "--scope", "user", "nexus-ollama", "--", "node", serverPath).CombinedOutput(); err != nil {
		return false, fmt.Errorf("claude mcp add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return false, nil
}

// installMCPDeps runs npm ci for the MCP server unless node_modules already
// matches the lockfile. node_modules is not committed, so server.mjs cannot
// load its SDK import without this.
func installMCPDeps(mcpDir string) (string, error) {
	lock, err := os.Stat(filepath.Join(mcpDir, "package-lock.json"))
	if err != nil {
		return "", err
	}
	if installed, err := os.Stat(filepath.Join(mcpDir, "node_modules", ".package-lock.json")); err == nil && !lock.ModTime().After(installed.ModTime()) {
		return "already installed", nil
	}
	if _, err := exec.LookPath("npm"); err != nil {
		return "skipped (npm not installed)", nil
	}
	cmd := exec.Command("npm", "ci", "--omit=dev", "--no-audit", "--no-fund", "--loglevel=error")
	cmd.Dir = mcpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("npm ci: %v: %s", err, truncateCol(strings.TrimSpace(string(out)), 200))
	}
	return "installed", nil
}
