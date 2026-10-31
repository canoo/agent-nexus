package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const compatURL = "https://raw.githubusercontent.com/canoo/agent-nexus/main/tools/compat/tools.json"
const compatLimit = 64 * 1024

type compatTool struct {
	ID              string `json:"id"`
	InstructionFile string `json:"instruction_file"`
	FileSupport     string `json:"file_support"`
	MinimumVersion  string `json:"minimum_version"`
	TestedRange     string `json:"tested_range"`
}
type compatCatalog struct {
	SchemaVersion int          `json:"schema_version"`
	UpdatedAt     string       `json:"updated_at"`
	Tools         []compatTool `json:"tools"`
}
type compatEntry struct {
	compatTool
	ProjectMarker bool   `json:"project_marker"`
	Executable    string `json:"executable,omitempty"`
	Version       string `json:"detected_version"`
	Status        string `json:"status"`
}
type compatibilityReport struct {
	SchemaVersion int           `json:"schema_version"`
	UpdatedAt     string        `json:"updated_at"`
	Source        string        `json:"source"`
	Project       string        `json:"project"`
	Tools         []compatEntry `json:"tools"`
}

// The registry describes files, never commands. Only these known executables
// may be discovered and invoked, always with the literal argv --version.
func driverBinaries(id string) []string {
	switch id {
	case "claude":
		return []string{"claude"}
	case "gemini":
		return []string{"gemini"}
	case "copilot":
		return []string{"copilot"}
	case "windsurf":
		return []string{"windsurf"}
	case "cline":
		return []string{"cline"}
	case "kiro":
		return []string{"kiro-cli", "kiro"}
	case "cursor":
		return []string{"cursor"}
	}
	return nil
}
func driverExecutable(id string) string {
	for _, name := range driverBinaries(id) {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

var compatVersionPattern = regexp.MustCompile(`(?:^|[^A-Za-z0-9])v?([0-9]{1,5}\.[0-9]{1,5}(?:\.[0-9]{1,5})?(?:[-+][A-Za-z0-9.]{1,40})?)\b`)

type boundedVersionOutput struct{ buffer bytes.Buffer }

func (b *boundedVersionOutput) Write(p []byte) (int, error) {
	n := len(p)
	if b.buffer.Len()+n > 4096 {
		return 0, errors.New("version output too large")
	}
	return b.buffer.Write(p)
}
func executableVersion(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	// Bound both pipe lifetime (including descendants) and captured output.
	configureRouteProcess(cmd)
	cmd.WaitDelay = 100 * time.Millisecond
	var out boundedVersionOutput
	cmd.Stdout = &out
	cmd.Stderr = &out
	if cmd.Run() != nil {
		return "unknown"
	}
	if v := compatVersionPattern.FindStringSubmatch(out.buffer.String()); len(v) > 1 {
		return v[1]
	}
	return "unknown"
}
func decodeCompat(data []byte) (compatCatalog, error) {
	var c compatCatalog
	if len(data) > compatLimit {
		return c, errors.New("compatibility registry too large")
	}
	if err := strictControlParams(data, &c); err != nil {
		return c, err
	}
	if c.SchemaVersion != 1 {
		return c, errors.New("unsupported compatibility schema")
	}
	if _, err := time.Parse("2006-01-02", c.UpdatedAt); err != nil {
		return c, errors.New("invalid registry date")
	}
	if len(c.Tools) != len(instructionDrivers) {
		return c, errors.New("registry must cover every instruction driver")
	}
	seen := map[string]bool{}
	for _, t := range c.Tools {
		valid := false
		for _, d := range instructionDrivers {
			if t.ID == d.Name && t.InstructionFile == d.Path {
				valid = true
			}
		}
		// Version evidence is optional and restricted to stable numeric triples.
		if !valid || seen[t.ID] || t.FileSupport != "project-wide text" || !validCompatEvidence(t.MinimumVersion, t.TestedRange) {
			return c, errors.New("invalid or unsupported registry tool/evidence")
		}
		seen[t.ID] = true
	}
	return c, nil
}
func builtInCompat() compatCatalog {
	c := compatCatalog{SchemaVersion: 1, UpdatedAt: "2026-10-30"}
	for _, d := range instructionDrivers {
		c.Tools = append(c.Tools, compatTool{ID: d.Name, InstructionFile: d.Path, FileSupport: "project-wide text"})
	}
	return c
}

type compatCache struct {
	SHA256  string          `json:"sha256"`
	Catalog json.RawMessage `json:"catalog"`
}

func compatCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "nexus", "compat-registry.json"), nil
}
func readCompatCache(path string) (compatCatalog, error) {
	b, err := readCompatFile(path, compatLimit*2)
	if err != nil {
		return compatCatalog{}, err
	}
	if len(b) > compatLimit*2 {
		return compatCatalog{}, errors.New("compatibility cache too large")
	}
	var cache compatCache
	if err = strictControlParams(b, &cache); err != nil {
		return compatCatalog{}, err
	}
	if instructionHash(cache.Catalog) != cache.SHA256 {
		return compatCatalog{}, errors.New("compatibility cache locally modified; preserve or remove it before refresh")
	}
	return decodeCompat(cache.Catalog)
}
func loadCompatCatalog(nexusDir string) (compatCatalog, string, error) {
	path, err := compatCachePath()
	if err == nil {
		c, e := readCompatCache(path)
		if e == nil {
			return c, "official registry cache", nil
		}
		if !os.IsNotExist(e) {
			return c, "", e
		}
	}
	path = filepath.Join(nexusDir, "tools", "compat", "tools.json")
	data, err := readCompatFile(path, compatLimit)
	if err == nil {
		c, e := decodeCompat(data)
		return c, "repository registry", e
	}
	if !os.IsNotExist(err) {
		return compatCatalog{}, "", err
	}
	return builtInCompat(), "bundled file contracts", nil
}
func refreshCompat(client *http.Client) error {
	path, err := compatCachePath()
	if err != nil {
		return err
	}
	if err = validateCompatCachePath(path); err != nil {
		return err
	}
	// Cache separately from the checkout, and refuse to replace local edits.
	if _, err = readCompatCache(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, compatURL, nil)
	if err != nil {
		return err
	}
	// Do not follow a redirect to an unreviewed endpoint.
	safeClient := *client
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := safeClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("official registry refresh: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, compatLimit+1))
	if err != nil {
		return err
	}
	if _, err = decodeCompat(data); err != nil {
		return err
	}
	// Compact raw JSON so its hash survives envelope encoding.
	var compact bytes.Buffer
	if err = json.Compact(&compact, data); err != nil {
		return err
	}
	data = compact.Bytes()
	encoded, err := json.Marshal(compatCache{instructionHash(data), data})
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".compat-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(encoded); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Recheck after download before replacement.
	if err = validateCompatCachePath(path); err != nil {
		return err
	}
	if _, err = readCompatCache(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(f.Name(), path)
}
func inspectCompatibility(nexusDir, project string) (compatibilityReport, error) {
	c, source, err := loadCompatCatalog(nexusDir)
	if err != nil {
		return compatibilityReport{}, err
	}
	project, err = filepath.Abs(project)
	if err != nil {
		return compatibilityReport{}, err
	}
	if info, e := os.Stat(project); e != nil {
		return compatibilityReport{}, e
	} else if !info.IsDir() {
		return compatibilityReport{}, errors.New("project must be a directory")
	}
	r := compatibilityReport{SchemaVersion: 1, UpdatedAt: c.UpdatedAt, Source: source, Project: project}
	for _, t := range c.Tools {
		e := compatEntry{compatTool: t, Version: "unknown", Status: "missing"}
		for _, d := range instructionDrivers {
			if d.Name == t.ID {
				markers := append([]string{d.Path}, d.Detect...)
				for _, marker := range markers {
					if _, err := os.Lstat(filepath.Join(project, marker)); err == nil {
						e.ProjectMarker = true
					}
				}
			}
		}
		e.Executable = driverExecutable(t.ID)
		if e.Executable != "" {
			e.Version = executableVersion(e.Executable)
			e.Status = compatVersionStatus(t, e.Version)
		} else if e.ProjectMarker {
			e.Status = "partial (project file only; executable absent)"
		}
		if t.ID == "agents" {
			e.Status = "file contract (vendor version unverified)"
		}
		r.Tools = append(r.Tools, e)
	}
	return r, nil
}
func formatCompatibility(r compatibilityReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Tool Compatibility — %s\nRegistry: %s (%s)\n\n", r.Project, r.Source, r.UpdatedAt)
	for _, t := range r.Tools {
		installed := "absent"
		if t.Executable != "" {
			installed = t.Executable
		}
		fmt.Fprintf(&b, "%s: %s\n  file: %s • project marker: %t\n  CLI: %s • version: %s\n  minimum: %s • tested: %s\n", t.ID, t.Status, t.InstructionFile, t.ProjectMarker, installed, t.Version, compatEvidenceLabel(t.MinimumVersion), compatEvidenceLabel(t.TestedRange))
	}
	b.WriteString("\nFile support: project-wide instruction text only. Unknown evidence is unverified.\n")
	return b.String()
}
func runCompatCLI(args []string, nexusDir string, out, errs io.Writer) int {
	update := len(args) > 0 && args[0] == "update"
	if update {
		args = args[1:]
	}
	flags := flag.NewFlagSet("nexus compat", flag.ContinueOnError)
	flags.SetOutput(errs)
	dir := flags.String("dir", ".", "project directory")
	asJSON := flags.Bool("json", false, "JSON output")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(errs, "unexpected arguments")
		return 2
	}
	if update {
		if err := refreshCompat(http.DefaultClient); err != nil {
			fmt.Fprintln(errs, err)
			return 1
		}
	}
	r, err := inspectCompatibility(nexusDir, *dir)
	if err != nil {
		fmt.Fprintln(errs, err)
		return 1
	}
	if *asJSON {
		err = json.NewEncoder(out).Encode(r)
	} else {
		_, err = fmt.Fprint(out, formatCompatibility(r))
	}
	if err != nil {
		fmt.Fprintln(errs, err)
		return 1
	}
	return 0
}

// Deliberately small evidence grammar: stable x.y.z or inclusive x.y.z..x.y.z.
// A detected prerelease/build suffix cannot establish stable-version support.
var compatStablePattern = regexp.MustCompile(`^[0-9]{1,5}\.[0-9]{1,5}\.[0-9]{1,5}$`)

func compatTriple(raw string) ([3]int, bool) {
	var v [3]int
	if !compatStablePattern.MatchString(raw) {
		return v, false
	}
	for i, p := range strings.Split(raw, ".") {
		v[i], _ = strconv.Atoi(p)
	}
	return v, true
}
func compareCompat(a, b [3]int) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}
func compatRange(raw string) ([3]int, [3]int, bool) {
	var zero [3]int
	p := strings.Split(raw, "..")
	if len(p) != 2 {
		return zero, zero, false
	}
	lo, lok := compatTriple(p[0])
	hi, hok := compatTriple(p[1])
	return lo, hi, lok && hok && compareCompat(lo, hi) <= 0
}
func validCompatEvidence(minimum, tested string) bool {
	if minimum != "" {
		if _, ok := compatTriple(minimum); !ok {
			return false
		}
	}
	if tested != "" {
		lo, _, ok := compatRange(tested)
		if !ok {
			return false
		}
		if minimum != "" {
			min, _ := compatTriple(minimum)
			if compareCompat(lo, min) < 0 {
				return false
			}
		}
	}
	return true
}
func compatVersionStatus(tool compatTool, detected string) string {
	v, ok := compatTriple(detected)
	if !ok {
		return "unverified"
	}
	if tool.MinimumVersion != "" {
		min, _ := compatTriple(tool.MinimumVersion)
		if compareCompat(v, min) < 0 {
			return "unsupported (below minimum version)"
		}
	}
	if tool.TestedRange == "" || tool.MinimumVersion == "" {
		return "unverified"
	}
	lo, hi, ok := compatRange(tool.TestedRange)
	if !ok {
		return "unverified"
	}
	if compareCompat(v, lo) < 0 || compareCompat(v, hi) > 0 {
		return "warning (outside tested range)"
	}
	return "within tested range (file contract only)"
}
func compatEvidenceLabel(value string) string {
	if value == "" {
		return "unverified"
	}
	return value
}
func readCompatFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("compatibility catalog must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("compatibility catalog too large")
	}
	return data, nil
}
func validateCompatCachePath(path string) error {
	// Walk existing ancestors as well as the destination: a symlink in the
	// cache hierarchy must not redirect registry replacement into another tree.
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink compatibility cache: %s", current)
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return nil
}
