package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type instructionFinding struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}
type importedInstruction struct {
	Path, Text string
	Original   []byte
}
type instructionValidation struct {
	Version        int                  `json:"version"`
	Primary        string               `json:"primary_target"`
	Canonical      string               `json:"canonical_source"`
	ReviewRequired bool                 `json:"manual_review_required"`
	Guarantees     []string             `json:"validation_contract"`
	Findings       []instructionFinding `json:"findings"`
	imports        []importedInstruction
}

func inspectInstructionInventory(root string, state projectionState) (instructionValidation, error) {
	report := instructionValidation{Version: 1, Primary: "AGENTS.md", Canonical: instructionPath, ReviewRequired: true,
		Guarantees: []string{"Deterministic instruction concatenation only; review conflicting policies manually before use.", "No model is called and no source content is transmitted.", "Settings, hooks, MCP environments/secrets, permissions, and session state are not equivalent to instruction Markdown and are not projected.", "File-level format validation only; live vendor loading and semantic policy equivalence are unverified."}, Findings: []instructionFinding{}}
	seen := map[string]bool{}
	// Registry order is stable and therefore so is provenance and output.
	var paths []string
	for _, d := range instructionDrivers {
		if d.Prefix == "" {
			paths = append(paths, d.Path)
		}
	}
	paths = append(paths, ".cursorrules", ".qrules")
	for _, pattern := range []string{".cursor/rules/*.mdc", ".kiro/steering/*.md", ".github/instructions/*.md"} {
		matches, e := filepath.Glob(filepath.Join(root, pattern))
		if e != nil {
			return report, e
		}
		sort.Strings(matches)
		for _, match := range matches {
			rel, e := filepath.Rel(root, match)
			if e != nil {
				return report, e
			}
			paths = append(paths, filepath.ToSlash(rel))
		}
	}
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		// Our projection-only files are outputs, never adoption inputs.
		ownedOutput := false
		for _, d := range instructionDrivers {
			if path == d.Path && d.Prefix != "" && state.Blocks[path] != "" {
				ownedOutput = true
			}
		}
		if ownedOutput {
			continue
		}
		b, e := instructionRead(root, path)
		if e != nil {
			return report, e
		}
		if b == nil {
			continue
		}
		before, block, after, e := splitInstructionBlock(b)
		if e != nil {
			return report, fmt.Errorf("%s: %w", path, e)
		}
		if block != "" && instructionHash([]byte(block)) != state.Blocks[path] {
			return report, fmt.Errorf("%s: managed block edited or unowned", path)
		}
		raw := before + after
		text, reason := normalizeInstructionText(path, raw)
		if reason != "" {
			report.Findings = append(report.Findings, instructionFinding{path, "unsupported", reason})
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		report.Findings = append(report.Findings, instructionFinding{path, "importable", "Plain project-wide instructions; semantic conflicts require manual review."})
		report.imports = append(report.imports, importedInstruction{path, text, []byte(raw)})
	}
	// Inventory paths only: never read, archive, or publish these settings files.
	for _, pattern := range []string{".claude/settings.json", ".claude/settings.local.json", ".gemini/settings.json", ".cursor/mcp.json", ".kiro/settings/*.json", ".continuerc.json", ".continue/config.*", "config.yaml", ".aider.conf.yml"} {
		matches, e := filepath.Glob(filepath.Join(root, pattern))
		if e != nil {
			return report, e
		}
		for _, match := range matches {
			rel, e := filepath.Rel(root, match)
			if e != nil {
				return report, e
			}
			report.Findings = append(report.Findings, instructionFinding{filepath.ToSlash(rel), "unsupported", "Non-equivalent settings: left in place, never imported or archived; validate hooks/MCP secrets/permissions/session state with the originating tool."})
		}
	}
	return report, nil
}
func normalizeInstructionText(path, text string) (string, string) {
	if strings.HasPrefix(strings.TrimSpace(text), "{") && json.Valid([]byte(text)) {
		return "", "JSON configuration is not instruction Markdown."
	}
	scoped := strings.HasPrefix(path, ".cursor/rules/") || strings.HasPrefix(path, ".kiro/steering/") || strings.HasPrefix(path, ".github/instructions/")
	if scoped && strings.HasPrefix(strings.TrimSpace(text), "---") {
		lines := strings.Split(strings.TrimSpace(text), "\n")
		end := -1
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				end = i
				break
			}
		}
		if end < 0 {
			return "", "Malformed rule frontmatter; resolve with the originating tool."
		}
		var fields map[string]interface{}
		if e := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &fields); e != nil {
			return "", "Invalid rule frontmatter; resolve with the originating tool."
		}
		for key, value := range fields {
			allowed := (strings.HasPrefix(path, ".cursor/") && key == "alwaysApply" && value == true) || (strings.HasPrefix(path, ".kiro/") && key == "inclusion" && value == "always")
			if !allowed {
				return "", "Scoped/conditional or unknown rule metadata cannot be represented faithfully in AGENTS.md."
			}
		}
		if strings.HasPrefix(path, ".cursor/") && fields["alwaysApply"] != true {
			return "", "Cursor rule does not declare alwaysApply: true; scope cannot be flattened."
		}
		text = strings.Join(lines[end+1:], "\n")
	} else if strings.HasPrefix(path, ".cursor/rules/") {
		return "", "Cursor rule lacks explicit alwaysApply: true; scope cannot be flattened."
	}
	if strings.Contains(text, "@file") || strings.Contains(text, "#[[") {
		return "", "Tool-specific file references require manual resolution; they are not flattened."
	}
	return text, ""
}
