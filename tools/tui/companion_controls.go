package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type companionConsent struct {
	AdapterID string `json:"adapterId"`
	ToolID    string `json:"toolId"`
	Enabled   *bool  `json:"enabled"`
}
type companionControlsResponse struct {
	SchemaVersion     int                `json:"schemaVersion"`
	OK                *bool              `json:"ok"`
	Action            string             `json:"action,omitempty"`
	CollectionEnabled *bool              `json:"collectionEnabled,omitempty"`
	PolicyVersion     int                `json:"policyVersion,omitempty"`
	RetentionDays     *int               `json:"retentionDays,omitempty"`
	StoredSpans       *int64             `json:"storedSpans,omitempty"`
	Consents          []companionConsent `json:"consents,omitempty"`
	Error             string             `json:"error,omitempty"`
}

func companionNodeExecutable() (string, error) {
	if node, configured := os.LookupEnv("NEXUS_COMPANION_NODE"); configured {
		if !filepath.IsAbs(node) {
			return "", os.ErrInvalid
		}
		stat, err := os.Stat(node)
		if err != nil {
			return "", err
		}
		if !stat.Mode().IsRegular() || stat.Mode().Perm()&0111 == 0 {
			return "", os.ErrPermission
		}
		return node, nil
	}
	return exec.LookPath("node")
}
func companionControlArgs(args []string) ([]string, bool) {
	if len(args) < 2 || args[len(args)-1] != "--json" {
		return nil, false
	}
	core := args[:len(args)-1]
	if len(core) == 1 && (core[0] == "status" || core[0] == "pause" || core[0] == "resume") {
		return core, true
	}
	if len(core) == 2 && core[0] == "disable" && core[1] == "--confirm" {
		return core, true
	}
	if len(core) != 6 && len(core) != 8 {
		return nil, false
	}
	if core[0] != "consent" || core[2] != "--adapter" || core[4] != "--tool" {
		return nil, false
	}
	if core[3] != "browser-chrome" && core[3] != "browser-edge" {
		return nil, false
	}
	switch core[5] {
	case "chatgpt", "claude", "gemini", "copilot", "perplexity":
	default:
		return nil, false
	}
	if core[1] == "revoke" && len(core) == 6 {
		return core, true
	}
	if core[1] == "grant" && len(core) == 8 && core[6] == "--acknowledge-policy" && core[7] == "1" {
		return core, true
	}
	return nil, false
}
func runCompanionControlsCLI(args []string, nexusDir string, out io.Writer) int {
	helperArgs, valid := companionControlArgs(args)
	if !valid {
		emitError(out, "companion_command_invalid")
		return 2
	}
	unavailable := func() int { emitError(out, "companion_controls_unavailable"); return 1 }
	node, err := companionNodeExecutable()
	if err != nil {
		return unavailable()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, append([]string{filepath.Join(nexusDir, "tools", "mcp", "companion-controls.mjs")}, helperArgs...)...)
	var buf boundedCompanionOutput
	cmd.Stdout = &buf
	runErr := cmd.Run()
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	dec.DisallowUnknownFields()
	var r companionControlsResponse
	if dec.Decode(&r) != nil || r.OK == nil || r.SchemaVersion != 1 {
		return unavailable()
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return unavailable()
	}
	if !*r.OK {
		if runErr == nil || r.Action != "" || r.CollectionEnabled != nil || r.PolicyVersion != 0 || r.RetentionDays != nil || r.StoredSpans != nil || r.Consents != nil {
			return unavailable()
		}
		switch r.Error {
		case "companion_store_unavailable", "companion_controls_unavailable", "companion_consent_required":
			emitError(out, r.Error)
			return 1
		default:
			return unavailable()
		}
	}
	if runErr != nil || r.Error != "" || r.Action != helperArgs[0] || r.CollectionEnabled == nil || r.PolicyVersion != 1 || r.RetentionDays == nil || *r.RetentionDays < 0 || *r.RetentionDays > 365 || r.StoredSpans == nil || *r.StoredSpans < 0 || *r.StoredSpans > 9007199254740991 || len(r.Consents) != 10 {
		return unavailable()
	}
	tools := []string{"chatgpt", "claude", "gemini", "copilot", "perplexity"}
	for i, c := range r.Consents {
		adapter := "browser-chrome"
		if i >= 5 {
			adapter = "browser-edge"
		}
		if c.AdapterID != adapter || c.ToolID != tools[i%5] || c.Enabled == nil {
			return unavailable()
		}
	}
	// Validate mutation postconditions, in addition to the fixed response schema.
	if (r.Action == "pause" || r.Action == "disable") && *r.CollectionEnabled {
		return unavailable()
	}
	anyEnabled := false
	for _, consent := range r.Consents {
		anyEnabled = anyEnabled || *consent.Enabled
	}
	if r.Action == "resume" && (!*r.CollectionEnabled || !anyEnabled) {
		return unavailable()
	}
	if r.Action == "disable" {
		for _, c := range r.Consents {
			if *c.Enabled {
				return unavailable()
			}
		}
	}
	if r.Action == "consent" {
		for _, c := range r.Consents {
			if c.AdapterID == helperArgs[3] && c.ToolID == helperArgs[5] && *c.Enabled != (helperArgs[1] == "grant") {
				return unavailable()
			}
		}
	}
	returnValue := 0
	if json.NewEncoder(out).Encode(r) != nil {
		returnValue = 1
	}
	return returnValue
}
