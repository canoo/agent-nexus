package main

import (
	"bytes"
	tea "charm.land/bubbletea/v2"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolatedCompat(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
}
func fakeCompatBinary(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
}
func TestCompatCatalogAndDetection(t *testing.T) {
	isolatedCompat(t)
	data, err := os.ReadFile("../compat/tools.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeCompat(data); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	fakeCompatBinary(t, bin, "claude", "[ \"$1\" = --version ] || exit 1\nprintf '\\033[31mClaude 2.4.6\\033[0m\\n'")
	fakeCompatBinary(t, bin, "gemini", "exit 1")
	putInstruction(t, dir, ".cursor/marker", "present")
	r, err := inspectCompatibility(t.TempDir(), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range r.Tools {
		switch tool.ID {
		case "claude":
			if tool.ProjectMarker || tool.Version != "2.4.6" || tool.Status != "unverified" {
				t.Fatalf("%+v", tool)
			}
		case "gemini":
			if tool.Version != "unknown" {
				t.Fatalf("%+v", tool)
			}
		case "cursor":
			if !tool.ProjectMarker || tool.Executable != "" || !strings.HasPrefix(tool.Status, "partial") {
				t.Fatalf("%+v", tool)
			}
		case "cline":
			if tool.Status != "missing" {
				t.Fatalf("%+v", tool)
			}
		}
	}
	var out bytes.Buffer
	if code := runCompatCLI([]string{"--dir", dir, "--json"}, t.TempDir(), &out, &out); code != 0 {
		t.Fatal(out.String())
	}
	var decoded compatibilityReport
	if err = json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Tools) != len(instructionDrivers) {
		t.Fatal("driver coverage")
	}
}

type compatTransport func(*http.Request) (*http.Response, error)

func (f compatTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func registryClient(body string, status int) *http.Client {
	return &http.Client{Transport: compatTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != compatURL || r.Method != "GET" {
			panic("unexpected endpoint")
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
}
func TestCompatRefreshPreservesCatalog(t *testing.T) {
	isolatedCompat(t)
	data, _ := json.Marshal(builtInCompat())
	if err := refreshCompat(registryClient(string(data), 200)); err != nil {
		t.Fatal(err)
	}
	path, _ := compatCachePath()
	before, _ := os.ReadFile(path)
	if _, err := readCompatCache(path); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body   string
		status int
	}{{"missing", 404}, {strings.Repeat("x", compatLimit+1), 200}, {`{"schema_version":99}`, 200}, {`{"schema_version":1,"commands":["sh"]}`, 200}} {
		if refreshCompat(registryClient(tc.body, tc.status)) == nil {
			t.Fatal("accepted invalid response")
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatal("failed refresh changed cache")
		}
	}
	var cache compatCache
	json.Unmarshal(before, &cache)
	cache.Catalog = []byte(strings.Replace(string(cache.Catalog), "2026-10-30", "2026-10-29", 1))
	edited, _ := json.Marshal(cache)
	os.WriteFile(path, edited, 0600)
	if refreshCompat(registryClient(string(data), 200)) == nil {
		t.Fatal("overwrote user edit")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(edited, after) {
		t.Fatal("user edit lost")
	}
}
func TestCompatRejectsUnsupportedEvidence(t *testing.T) {
	c := builtInCompat()
	c.Tools[0].TestedRange = "all versions"
	b, _ := json.Marshal(c)
	if _, err := decodeCompat(b); err == nil {
		t.Fatal("accepted unsupported version evidence")
	}
	c = builtInCompat()
	c.Tools[0].InstructionFile = "../../escape"
	b, _ = json.Marshal(c)
	if _, err := decodeCompat(b); err == nil {
		t.Fatal("accepted unknown driver path")
	}
}
func TestProjectionDetectsInstalledToolsWithoutExecuting(t *testing.T) {
	isolatedCompat(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	dir := t.TempDir()
	fakeCompatBinary(t, bin, "claude", "printf executed > '"+filepath.Join(bin, "was-executed")+"'; exit 99")
	putInstruction(t, dir, instructionPath, "project instructions")
	if err := projectInstructions(dir, "sync", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(bin, "was-executed")); !os.IsNotExist(err) {
		t.Fatal("sync executed vendor CLI")
	}
	if !strings.Contains(getInstruction(t, dir, "CLAUDE.md"), "project instructions") {
		t.Fatal("installed tool was not projected")
	}
}
func TestCompatScreenSyncUsesProject(t *testing.T) {
	isolatedCompat(t)
	project := t.TempDir()
	source := t.TempDir()
	putInstruction(t, project, instructionPath, "project instructions")
	putInstruction(t, source, instructionPath, "SOURCE MUST NOT CHANGE")
	m := initialModel()
	m.screen = screenCompat
	m.nexusDir = source
	m.compatProject = project
	next, cmd := updateCompat(tea.KeyPressMsg{Code: 's'}, m)
	if cmd == nil {
		t.Fatal("missing async command")
	}
	msg := cmd()
	synced := msg.(compatSyncedMsg)
	if synced.err != nil {
		t.Fatal(synced.err)
	}
	if !strings.Contains(getInstruction(t, project, "AGENTS.md"), "project instructions") {
		t.Fatal("wrong project")
	}
	if _, err := os.Stat(filepath.Join(source, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("source repository changed")
	}
	next, cmd = updateCompat(msg, next.(model))
	if next.(model).running || cmd != nil {
		t.Fatal("worker did not complete")
	}
	putInstruction(t, project, "AGENTS.md", "user edit")
	_, cmd = updateCompat(tea.KeyPressMsg{Code: 's'}, next.(model))
	if cmd().(compatSyncedMsg).err == nil {
		t.Fatal("conflict not reported")
	}
}

func TestCompatVersionBoundsAndSanitizes(t *testing.T) {
	isolatedCompat(t)
	bin := t.TempDir()
	fakeCompatBinary(t, bin, "noisy", "i=0; while [ \"$i\" -lt 5000 ]; do printf x; i=$((i+1)); done; printf ' 9.8.7'")
	if got := executableVersion(filepath.Join(bin, "noisy")); got != "unknown" {
		t.Fatalf("unbounded output accepted: %q", got)
	}
	fakeCompatBinary(t, bin, "control", "printf '\\033[31mTool 2.3.4-beta.1\\033[0m\\n'; printf 'malicious control text'")
	if got := executableVersion(filepath.Join(bin, "control")); got != "2.3.4-beta.1" {
		t.Fatalf("unsanitized version: %q", got)
	}
	fakeCompatBinary(t, bin, "stalled", "while :; do :; done")
	if got := executableVersion(filepath.Join(bin, "stalled")); got != "unknown" {
		t.Fatal(got)
	}
}

func TestCompatControlRead(t *testing.T) {
	isolatedCompat(t)
	project := t.TempDir()
	params, _ := json.Marshal(map[string]string{"dir": project})
	result, err := executeControl(controlRequest{SchemaVersion: 1, Operation: "compat.get", Params: params}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if result.(compatibilityReport).Project != project {
		t.Fatal("wrong project")
	}
}

func TestCompatVersionEvidence(t *testing.T) {
	tool := compatTool{MinimumVersion: "1.0.0", TestedRange: "1.2.0..2.0.0"}
	for _, tc := range []struct{ version, status string }{
		{"0.9.0", "unsupported (below minimum version)"},
		{"1.0.0", "warning (outside tested range)"},
		{"1.2.0", "within tested range (file contract only)"},
		{"1.9.9", "within tested range (file contract only)"},
		{"2.0.0", "within tested range (file contract only)"},
		{"2.0.1", "warning (outside tested range)"},
		{"1.2.0-beta.1", "unverified"}, {"1.2.0+build", "unverified"}, {"unknown", "unverified"}, {"1.2", "unverified"},
	} {
		if got := compatVersionStatus(tool, tc.version); got != tc.status {
			t.Fatalf("%s: %s", tc.version, got)
		}
	}
	for _, tc := range []compatTool{{}, {MinimumVersion: "1.0.0"}, {TestedRange: "1.0.0..2.0.0"}} {
		if got := compatVersionStatus(tc, "1.2.0"); got != "unverified" {
			t.Fatal(got)
		}
	}
	c := builtInCompat()
	c.Tools[1].MinimumVersion = tool.MinimumVersion
	c.Tools[1].TestedRange = tool.TestedRange
	b, _ := json.Marshal(c)
	if _, err := decodeCompat(b); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"all", "1.0.0..0.1.0", "1.0.0-beta..2.0.0", "0.0.0..2.0.0"} {
		c.Tools[1].TestedRange = bad
		b, _ = json.Marshal(c)
		if _, err := decodeCompat(b); err == nil {
			t.Fatal("accepted", bad)
		}
	}
}
func TestCompatRefreshRejectsSymlinkAndOversizeCache(t *testing.T) {
	isolatedCompat(t)
	path, _ := compatCachePath()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Dir(path)); err != nil {
		t.Skip(err)
	}
	data, _ := json.Marshal(builtInCompat())
	if refreshCompat(registryClient(string(data), 200)) == nil {
		t.Fatal("accepted symlink cache directory")
	}
	if entries, _ := os.ReadDir(target); len(entries) != 0 {
		t.Fatal("wrote through symlink")
	}
	os.Remove(filepath.Dir(path))
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte(strings.Repeat("x", compatLimit*2+1)), 0600)
	if _, err := readCompatCache(path); err == nil {
		t.Fatal("accepted oversize local file")
	}
}

func TestCompatScreenSmallTerminalScrollAndResize(t *testing.T) {
	isolatedCompat(t)
	m := initialModel()
	m.width = 60
	m.height = 20
	m.nexusDir = t.TempDir()
	next, cmd := startCompat(m)
	if cmd == nil {
		t.Fatal("missing local worker")
	}
	next, _ = updateCompat(cmd(), next.(model))
	m = next.(model)
	initial := compatView(m)
	for i := 0; i < 100; i++ {
		next, _ = updateCompat(tea.KeyPressMsg{Code: 'j'}, m)
		m = next.(model)
	}
	if got := compatView(m); got == initial || !strings.Contains(got, "Unknown evidence") {
		t.Fatal("final catalog details inaccessible by scrolling")
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m = next.(model)
	if m.compatViewport.Width() != 70 || m.compatViewport.Height() != 20 {
		t.Fatal("resize not delegated")
	}
}
