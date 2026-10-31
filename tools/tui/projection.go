package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const instructionPath = ".nexus/source.md"
const projectionStatePath = ".nexus/projection.json"
const projectionStart = "<!-- nexus:instructions:start -->"
const projectionEnd = "<!-- nexus:instructions:end -->"
const projectionHeader = "<!-- Managed by NEXUS — edit .nexus/source.md, not this file -->"

// Drivers deliberately handle plain, project-wide instruction text only.
// Settings JSON, scoped rules and executable configuration are not instructions.
type instructionDriver struct {
	Name, Path string
	Detect     []string
	Prefix     string
}

var instructionDrivers = []instructionDriver{
	{"agents", "AGENTS.md", nil, ""},
	{"claude", "CLAUDE.md", []string{"CLAUDE.md", ".claude"}, ""},
	{"gemini", "GEMINI.md", []string{"GEMINI.md", ".gemini"}, ""},
	{"copilot", ".github/copilot-instructions.md", []string{".github/copilot-instructions.md"}, ""},
	{"windsurf", ".windsurfrules", []string{".windsurfrules", ".windsurf"}, ""},
	{"cline", ".clinerules", []string{".clinerules"}, ""},
	{"kiro", ".kiro/steering/nexus-project.md", []string{".kiro"}, "---\ninclusion: always\n---\n\n"},
	{"cursor", ".cursor/rules/nexus-project.mdc", []string{".cursor"}, "---\nalwaysApply: true\n---\n\n"},
}

type instructionSource struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type projectionState struct {
	Version          int                 `json:"version"`
	ValidationSHA256 string              `json:"validation_sha256,omitempty"`
	Sources          []instructionSource `json:"sources"`
	Blocks           map[string]string   `json:"blocks"`
}
type projectionWrite struct {
	Path    string
	Content []byte
}

func instructionHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Resolve the selected project root once, then refuse links within it so
// instruction files cannot redirect writes into a global instruction store.
func instructionRead(root, rel string) ([]byte, error) {
	path := filepath.Join(root, filepath.FromSlash(rel))
	for p := path; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("refusing symlink: %s", p)
		}
		if p == root || filepath.Dir(p) == p {
			break
		}
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return b, err
}
func splitInstructionBlock(b []byte) (string, string, string, error) {
	s := string(b)
	n, m := strings.Count(s, projectionStart), strings.Count(s, projectionEnd)
	if n == 0 && m == 0 {
		return s, "", "", nil
	}
	if n != 1 || m != 1 {
		return "", "", "", fmt.Errorf("malformed NEXUS instruction markers")
	}
	a, z := strings.Index(s, projectionStart), strings.Index(s, projectionEnd)
	if z < a {
		return "", "", "", fmt.Errorf("reversed NEXUS instruction markers")
	}
	z += len(projectionEnd)
	return s[:a], s[a:z], s[z:], nil
}
func runProjectionCLI(command string, args []string, out, errs io.Writer) int {
	flags := flag.NewFlagSet("nexus "+command, flag.ContinueOnError)
	flags.SetOutput(errs)
	dir := flags.String("dir", ".", "project directory")
	dry := flags.Bool("dry-run", false, "preview without writing")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(errs, "unexpected positional arguments")
		return 2
	}
	root, err := filepath.Abs(*dir)
	if err == nil {
		if command == "rollback" {
			err = rollbackInstructions(root, *dry, out)
		} else {
			err = projectInstructions(root, command, *dry, out)
		}
	}
	if err != nil {
		fmt.Fprintln(errs, "NEXUS:", err)
		return 1
	}
	return 0
}
func projectInstructions(root, command string, dry bool, out io.Writer) error {
	var resolveErr error
	root, resolveErr = filepath.EvalSymlinks(root)
	if resolveErr != nil {
		return resolveErr
	}
	root, resolveErr = filepath.Abs(root)
	if resolveErr != nil {
		return resolveErr
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("project root must be a directory")
	}
	canonical, err := instructionRead(root, instructionPath)
	if err != nil {
		return err
	}
	stateBytes, err := instructionRead(root, projectionStatePath)
	if err != nil {
		return err
	}
	state := projectionState{Version: 1, Blocks: map[string]string{}}
	if stateBytes != nil {
		if err = json.Unmarshal(stateBytes, &state); err != nil {
			return fmt.Errorf("invalid projection manifest: %w", err)
		}
		if state.Version != 1 || state.Blocks == nil {
			return fmt.Errorf("unsupported projection manifest")
		}
	}
	var writes []projectionWrite
	inventory, err := inspectInstructionInventory(root, state)
	if err != nil {
		return err
	}
	reportBytes, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return err
	}
	reportBytes = append(reportBytes, '\n')
	for _, finding := range inventory.Findings {
		if finding.Status == "unsupported" {
			fmt.Fprintf(out, "review %s: %s\n", finding.Path, finding.Reason)
		}
	}
	if command == "adopt" {
		var body strings.Builder
		body.WriteString("# Project instructions\n")
		var sources []instructionSource
		for _, source := range inventory.imports {
			text := source.Text
			sources = append(sources, instructionSource{source.Path, instructionHash([]byte(text))})
			fmt.Fprintf(&body, "\n## Imported from %s\n\n%s\n", source.Path, text)
			if canonical == nil {
				archivePath := ".nexus/.adopted/" + source.Path
				old, e := instructionRead(root, archivePath)
				if e != nil {
					return e
				}
				if old != nil && string(old) != string(source.Original) {
					return fmt.Errorf("archive conflict: %s", archivePath)
				}
				if old == nil {
					writes = append(writes, projectionWrite{archivePath, source.Original})
				}
			}
		}

		imported := []byte(body.String())
		if canonical != nil {
			// Adoption never overwrites a user's canonical edits or silently absorbs
			// changed sources. Once adopted, canonical is the source of truth.
			if stateBytes == nil {
				return fmt.Errorf("%s exists without provenance; refusing to overwrite", instructionPath)
			}
			if !sameInstructionSources(sources, state.Sources) {
				return fmt.Errorf("source instructions changed since adoption; reconcile them manually in %s", instructionPath)
			}
			fmt.Fprintln(out, "unchanged "+instructionPath)
			return nil
		}
		if stateBytes != nil {
			return fmt.Errorf("manifest exists but canonical instructions are missing")
		}
		if len(sources) == 0 {
			return fmt.Errorf("no supported project instruction files found; create AGENTS.md first")
		}
		canonical = imported
		state.Sources = sources
		writes = append(writes, projectionWrite{instructionPath, canonical})
	} else if canonical == nil {
		return fmt.Errorf("%s missing; run nexus adopt first", instructionPath)
	}
	if strings.Contains(string(canonical), projectionStart) || strings.Contains(string(canonical), projectionEnd) {
		return fmt.Errorf("canonical instructions contain reserved projection markers")
	}
	block := projectionStart + "\n" + projectionHeader + "\n" + strings.TrimRight(string(canonical), "\n") + "\n" + projectionEnd
	for _, driver := range instructionDrivers {
		_, previouslyProjected := state.Blocks[driver.Path]
		detected := driver.Name == "agents" || previouslyProjected
		for _, marker := range driver.Detect {
			if _, e := os.Lstat(filepath.Join(root, marker)); e == nil {
				detected = true
			} else if !os.IsNotExist(e) {
				return e
			}
		}
		if !detected {
			continue
		}
		existing, e := instructionRead(root, driver.Path)
		if e != nil {
			return e
		}
		if driver.Prefix != "" && existing != nil && state.Blocks[driver.Path] == "" {
			return fmt.Errorf("%s already exists and is not owned by NEXUS", driver.Path)
		}
		before, old, after, e := splitInstructionBlock(existing)
		if e != nil {
			return fmt.Errorf("%s: %w", driver.Path, e)
		}
		if driver.Prefix != "" {
			if existing == nil {
				before = driver.Prefix
			} else if !strings.HasPrefix(before, driver.Prefix) {
				return fmt.Errorf("%s: projection frontmatter changed", driver.Path)
			}
		}
		expected, owned := state.Blocks[driver.Path]
		if (old != "" && (!owned || instructionHash([]byte(old)) != expected)) || (old == "" && owned) {
			return fmt.Errorf("%s: managed block edited or removed; reconcile manually", driver.Path)
		}
		if old == "" && len(before) > 0 && !strings.HasSuffix(before, "\n") {
			before += "\n"
		}
		if old == "" && len(before) > 0 {
			before += "\n"
		}
		next := []byte(before + block + after)
		if old == "" {
			next = append(next, '\n')
		}
		if string(next) != string(existing) {
			writes = append(writes, projectionWrite{driver.Path, next})
		} else {
			fmt.Fprintln(out, "unchanged "+driver.Path)
		}
		state.Blocks[driver.Path] = instructionHash([]byte(block))
	}
	ignorePath := ".nexus/.gitignore"
	existingIgnore, ignoreErr := instructionRead(root, ignorePath)
	if ignoreErr != nil {
		return ignoreErr
	}
	ignoreText := string(existingIgnore)
	for _, rule := range []string{".snapshots/", ".adopted/"} {
		found := false
		for _, line := range strings.Split(ignoreText, "\n") {
			if strings.TrimSpace(line) == rule {
				found = true
			}
		}
		if !found {
			if ignoreText != "" && !strings.HasSuffix(ignoreText, "\n") {
				ignoreText += "\n"
			}
			ignoreText += rule + "\n"
		}
	}
	if ignoreText != string(existingIgnore) {
		writes = append(writes, projectionWrite{ignorePath, []byte(ignoreText)})
	}
	oldReport, e := instructionRead(root, "AGENTS.validation.json")
	if e != nil {
		return e
	}
	if (oldReport != nil && (state.ValidationSHA256 == "" || instructionHash(oldReport) != state.ValidationSHA256)) || (oldReport == nil && state.ValidationSHA256 != "") {
		return fmt.Errorf("AGENTS.validation.json edited, removed, or unowned; refusing overwrite")
	}
	state.ValidationSHA256 = instructionHash(reportBytes)
	if string(oldReport) != string(reportBytes) {
		writes = append(writes, projectionWrite{"AGENTS.validation.json", reportBytes})
	}
	encoded, e := json.MarshalIndent(state, "", "  ")
	if e != nil {
		return e
	}
	encoded = append(encoded, '\n')
	if string(encoded) != string(stateBytes) {
		writes = append(writes, projectionWrite{projectionStatePath, encoded})
	}
	// Preflight all paths before any mutation; each file is replaced atomically.
	for _, w := range writes {
		if _, e := instructionRead(root, w.Path); e != nil {
			return e
		}
	}
	if len(writes) > 0 {
		if dry {
			fmt.Fprintln(out, "would snapshot changed files in .nexus/.snapshots/")
		} else if e := snapshotInstructions(root, writes); e != nil {
			return e
		}
	}
	for _, w := range writes {
		if dry {
			fmt.Fprintln(out, "would write "+w.Path)
			continue
		}
		path := filepath.Join(root, w.Path)
		dirMode := os.FileMode(0755)
		if strings.HasPrefix(w.Path, ".nexus/.adopted/") {
			dirMode = 0700
		}
		if e := os.MkdirAll(filepath.Dir(path), dirMode); e != nil {
			return e
		}
		f, e := os.CreateTemp(filepath.Dir(path), ".nexus-*")
		if e != nil {
			return e
		}
		tmp := f.Name()
		mode := os.FileMode(0644)
		if strings.HasPrefix(w.Path, ".nexus/.adopted/") {
			mode = 0600
		}
		if info, e := os.Stat(path); e == nil {
			mode = info.Mode().Perm()
		}
		e = f.Chmod(mode)
		if e == nil {
			_, e = f.Write(w.Content)
		}
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
		if e == nil {
			e = os.Rename(tmp, path)
		}
		os.Remove(tmp)
		if e != nil {
			return fmt.Errorf("write %s: %w (earlier files may have been written)", w.Path, e)
		}
		fmt.Fprintln(out, "wrote "+w.Path)
	}
	return nil
}
func sameInstructionSources(a, b []instructionSource) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
