package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type diagnosticCheck struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Action string `json:"action,omitempty"`
}
type diagnosticReport struct {
	SchemaVersion int  `json:"schemaVersion"`
	Healthy       bool `json:"healthy"`
	Ollama        struct {
		Enabled            bool   `json:"enabled"`
		Reachable          bool   `json:"reachable"`
		URL                string `json:"url,omitempty"`
		ModelsReady        bool   `json:"modelsReady"`
		RequiredModelCount int    `json:"requiredModelCount"`
	} `json:"ollama"`
	Symlinks []diagnosticCheck `json:"symlinks"`
	MCP      struct {
		Running   *bool  `json:"running"`
		Startable bool   `json:"startable"`
		State     string `json:"state"`
	} `json:"mcp"`
	Tokscale struct {
		Installed bool `json:"installed"`
		Optional  bool `json:"optional"`
	} `json:"tokscale"`
	Checks []diagnosticCheck `json:"checks"`
}

var diagnosticHTTPClient = &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
var diagnosticModel = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:/-]{0,349}$`)

func collectDiagnostics(dir, home string) diagnosticReport {
	r := diagnosticReport{SchemaVersion: 1, Healthy: true, Symlinks: []diagnosticCheck{}, Checks: []diagnosticCheck{}}
	add := func(name string, ok bool, action string) {
		state := "ok"
		if !ok {
			state = "failed"
			r.Healthy = false
		}
		if ok {
			action = ""
		}
		r.Checks = append(r.Checks, diagnosticCheck{name, state, action})
	}
	settings := parseSettings(readFileOrEmpty(filepath.Join(dir, ".env")))
	get := func(key, fallback string) string {
		if v, ok := os.LookupEnv(key); ok {
			return v
		}
		if v, ok := settings[key]; ok {
			return v
		}
		return fallback
	}
	valid := true
	if _, err := os.ReadFile(filepath.Join(dir, ".env")); err != nil && !os.IsNotExist(err) {
		valid = false
	}
	local := get("NEXUS_LOCAL_AI", "true")
	if local != "true" && local != "false" {
		valid = false
	}
	r.Ollama.Enabled = local != "false"
	models := map[string]bool{}
	for _, band := range []string{"NEXUS_SUPERVISOR_MODEL", "NEXUS_LOGIC_MODEL"} {
		if _, present := settings[band]; present || os.Getenv(band) != "" {
			if !diagnosticModel.MatchString(get(band, "")) {
				valid = false
			}
		}
		if v, present := os.LookupEnv(band); present && !diagnosticModel.MatchString(v) {
			valid = false
		}
	}
	for _, task := range []string{"COMMIT_MSG", "BOILERPLATE", "TEST_SCAFFOLD", "LINT_FIX", "LOGIC_REFACTOR"} {
		band, def := "NEXUS_SUPERVISOR_MODEL", "qwen2.5-coder:1.5b"
		if task == "LINT_FIX" || task == "LOGIC_REFACTOR" || task == "TEST_SCAFFOLD" {
			band, def = "NEXUS_LOGIC_MODEL", "llama3.2:3b"
		}
		v := get("NEXUS_MODEL_"+task, get(band, def))
		if !diagnosticModel.MatchString(v) {
			valid = false
		}
		models[v] = true
	}
	endpoint := get("OLLAMA_HOST_URL", "http://localhost:11434")
	u, err := url.Parse(endpoint)
	urlOK := err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/")
	if !urlOK {
		valid = false
	} else {
		r.Ollama.URL = u.Scheme + "://" + u.Host
	}
	if _, err := loadRouteConfig(dir); err != nil {
		valid = false
	}
	add("environment", valid, "Review .env: use valid model names, booleans, routing settings and a credential-free HTTP(S) Ollama origin")
	r.Ollama.RequiredModelCount = len(models)
	if r.Ollama.Enabled && urlOK && valid {
		resp, err := diagnosticHTTPClient.Get(strings.TrimRight(endpoint, "/") + "/api/tags")
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				var data struct {
					Models []struct {
						Name string `json:"name"`
					} `json:"models"`
				}
				if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&data) == nil {
					r.Ollama.Reachable = true
					available := map[string]bool{}
					for _, m := range data.Models {
						available[m.Name] = true
					}
					r.Ollama.ModelsReady = true
					for m := range models {
						if !available[m] && !available[m+":latest"] {
							r.Ollama.ModelsReady = false
						}
					}
				}
			}
		}
	}
	if r.Ollama.Enabled {
		add("ollama", r.Ollama.Reachable, "Start Ollama and check OLLAMA_HOST_URL")
		add("models", r.Ollama.ModelsReady, "Pull each configured task model with ollama pull; review band and per-task overrides")
	} else {
		r.Checks = append(r.Checks, diagnosticCheck{"ollama", "disabled", ""})
	}
	nodeOK := false
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	out, err := exec.CommandContext(ctx, "node", "--version").Output()
	cancel()
	if err == nil {
		parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(string(out)), "v"), ".")
		if len(parts) == 3 {
			major, e1 := strconv.Atoi(parts[0])
			minor, e2 := strconv.Atoi(parts[1])
			patch, e3 := strconv.Atoi(parts[2])
			nodeOK = e1 == nil && e2 == nil && e3 == nil && patch >= 0 && (major > 22 || major == 22 && minor >= 13)
		}
	}
	add("node", nodeOK, "Install Node.js 22.13 or newer (node:sqlite required)")
	r.MCP.State = "unavailable"
	if nodeOK {
		r.MCP.Startable = probeDiagnosticMCP(dir)
		if r.MCP.Startable {
			r.MCP.State = "initialize_probe_passed"
		}
	}
	add("mcp_startup", r.MCP.Startable, "Run npm ci in tools/mcp and review MCP startup; probe never establishes a persistent running server")
	for _, name := range []string{"Gemini", "Claude", "Core", "Personas", "Tools", "Prompts", "Agent Memory", "Kiro"} {
		source, target := "", ""
		switch name {
		case "Gemini":
			source = filepath.Join(dir, "core", "NEXUS.md")
			target = filepath.Join(home, ".gemini", "GEMINI.md")
		case "Claude":
			source = filepath.Join(dir, "core", "CLAUDE.md")
			target = filepath.Join(home, ".claude", "CLAUDE.md")
		case "Kiro":
			source = filepath.Join(dir, "core", "kiro-nexus-steering.md")
			target = filepath.Join(home, ".kiro", "steering", "nexus-orchestrator.md")
		default:
			leaf := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
			source = filepath.Join(dir, leaf)
			target = filepath.Join(home, ".config", "nexus", leaf)
		}
		ok := false
		if name == "Kiro" {
			a, e1 := kiroSteeringCopy(source)
			b, e2 := os.ReadFile(target)
			fi, e3 := os.Lstat(target)
			ok = e1 == nil && e2 == nil && e3 == nil && fi.Mode().IsRegular() && bytes.Equal(a, b)
		} else {
			a, e1 := filepath.EvalSymlinks(source)
			b, e2 := filepath.EvalSymlinks(target)
			fi, e3 := os.Lstat(target)
			ok = e1 == nil && e2 == nil && e3 == nil && fi.Mode()&os.ModeSymlink != 0 && a == b
		}
		state := "ok"
		action := ""
		if !ok {
			state = "failed"
			action = "Review setup-nexus.sh installation target; preserve local files before repairing"
			r.Healthy = false
		}
		r.Symlinks = append(r.Symlinks, diagnosticCheck{name, state, action})
	}
	_, err = exec.LookPath("tokscale")
	r.Tokscale.Installed = err == nil
	r.Tokscale.Optional = true
	diskPath := get("OLLAMA_MODELS", filepath.Join(home, ".ollama", "models"))
	for {
		if _, err := os.Stat(diskPath); err == nil {
			break
		}
		parent := filepath.Dir(diskPath)
		if parent == diskPath {
			break
		}
		diskPath = parent
	}
	free, err := diagnosticDiskAvailable(diskPath)
	if r.Ollama.Enabled {
		add("disk_space", err == nil && free >= 5<<30, "Keep at least 5 GiB free for model downloads; actual requirements depend on configured models")
	}
	return r
}

// Probe only initialize, never tools/call. Isolate eager observability writes.
func probeDiagnosticMCP(dir string) bool {
	temp, err := os.MkdirTemp("", "nexus-doctor-")
	if err != nil {
		return false
	}
	defer os.RemoveAll(temp)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(dir, "tools", "mcp", "server.mjs"))
	cmd.Dir = dir
	for _, v := range os.Environ() {
		key := strings.SplitN(v, "=", 2)[0]
		if key != "HOME" && key != "USERPROFILE" && key != "XDG_CONFIG_HOME" {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Env = append(cmd.Env, "HOME="+temp, "USERPROFILE="+temp, "XDG_CONFIG_HOME="+temp)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return false
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false
	}
	if cmd.Start() != nil {
		return false
	}
	defer func() { stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	_, err = io.WriteString(stdin, "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2024-11-05\",\"capabilities\":{},\"clientInfo\":{\"name\":\"nexus-doctor\",\"version\":\"1\"}}}\n")
	if err != nil {
		return false
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 65536)
	for scanner.Scan() {
		var response struct {
			JSONRPC string `json:"jsonrpc"`
			ID      int    `json:"id"`
			Result  struct {
				ProtocolVersion string `json:"protocolVersion"`
				ServerInfo      struct {
					Name string `json:"name"`
				} `json:"serverInfo"`
			} `json:"result"`
		}
		if json.Unmarshal(scanner.Bytes(), &response) == nil && response.JSONRPC == "2.0" && response.ID == 1 && response.Result.ProtocolVersion != "" && response.Result.ServerInfo.Name != "" {
			return true
		}
	}
	return false
}
func runDiagnosticCLI(args []string, dir, home string, out, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "doctor" && args[0] != "health") || len(args) > 2 || len(args) == 2 && args[1] != "--json" {
		fmt.Fprintln(stderr, "Usage: nexus doctor [--json] | nexus health [--json]")
		return 2
	}
	r := collectDiagnostics(dir, home)
	if len(args) == 2 {
		if json.NewEncoder(out).Encode(r) != nil {
			return 1
		}
	} else {
		fmt.Fprintln(out, "NEXUS diagnostics (read only; MCP running state unknown)")
		checks := append(append([]diagnosticCheck{}, r.Checks...), r.Symlinks...)
		sort.SliceStable(checks, func(i, j int) bool { return checks[i].Name < checks[j].Name })
		for _, c := range checks {
			fmt.Fprintf(out, "%s: %s", c.Name, c.State)
			if c.Action != "" {
				fmt.Fprintf(out, " — %s", c.Action)
			}
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "Tokscale installed: %t (optional)\n", r.Tokscale.Installed)
	}
	if !r.Healthy {
		return 1
	}
	return 0
}
