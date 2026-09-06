package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Status is a local inventory, not a provider health or authentication check.
// Keep this explicit allowlist: never serialize config maps or credential files.
type statusReport struct {
	SchemaVersion int              `json:"schemaVersion"`
	Version       string           `json:"version"`
	Personas      personaInventory `json:"personas"`
	Executables   map[string]bool  `json:"executables"`
}

type personaInventory struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

func localStatus(home string) statusReport {
	report := statusReport{
		SchemaVersion: 1,
		Version:       version,
		Personas:      personaInventory{State: "available"},
		Executables:   make(map[string]bool),
	}
	// Match the current installer's layout until the XDG migration is implemented.
	entries, err := os.ReadDir(filepath.Join(home, ".config", "nexus", "personas"))
	if os.IsNotExist(err) {
		report.Personas.State = "missing"
	} else if err != nil {
		report.Personas.State = "unreadable"
	} else {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
				report.Personas.Count++
			}
		}
	}
	for _, name := range []string{"codex", "ollama", "node", "omarchy"} {
		_, err := exec.LookPath(name)
		report.Executables[name] = err == nil
	}
	return report
}
