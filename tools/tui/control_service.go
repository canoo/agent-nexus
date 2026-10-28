package main

// The desktop transport calls these same workers used by the TUI. No HTTP
// listener or shell/eval endpoint is introduced: one bounded request per process.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const controlProtocolVersion = 1
const controlMaxRequest = 65536
const controlMaxOutput = 4 * 1024 * 1024

type controlRequest struct {
	SchemaVersion int             `json:"schemaVersion"`
	Operation     string          `json:"operation"`
	Confirm       bool            `json:"confirm,omitempty"`
	Params        json.RawMessage `json:"params,omitempty"`
}
type controlCapability struct {
	ID           string `json:"id"`
	Feature      string `json:"feature"`
	Mutates      bool   `json:"mutates"`
	Confirmation bool   `json:"confirmation"`
}

var controlCapabilities = []controlCapability{
	{"capabilities", "system", false, false}, {"status.get", "system", false, false},
	{"install.run", "Install NEXUS", true, true},
	{"config.get", "Configure", false, false}, {"config.set", "Configure", true, true},
	{"health.check", "Health Check", false, false},
	{"tasks.list", "Task Log", false, false}, {"usage.get", "Usage & Cost Dashboard", false, false},
	{"activity.list", "Companion Tool Activity", false, false}, {"companion.command", "Companion Tool Activity", true, false},
	{"update.check", "Update NEXUS", false, false}, {"update.apply", "Update NEXUS", true, true},
	{"uninstall.run", "Uninstall NEXUS", true, true},
	{"memory.command", "Project Memory", true, false}, {"memory.delete", "Project Memory", true, true},
	{"route.preview", "Routing", false, false}, {"route.run", "Routing", true, true},
}

func strictControlParams(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("invalid params")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

type controlEmpty struct{}

func controlFailure(code string) error { return errors.New(code) }
func executeControl(req controlRequest, nexusDir string) (any, error) {
	if req.SchemaVersion != controlProtocolVersion {
		return nil, controlFailure("control_protocol_unsupported")
	}
	var capability *controlCapability
	for i := range controlCapabilities {
		if controlCapabilities[i].ID == req.Operation {
			capability = &controlCapabilities[i]
			break
		}
	}
	if capability == nil {
		return nil, controlFailure("control_operation_unknown")
	}
	if capability.Confirmation && !req.Confirm {
		return nil, controlFailure("control_confirmation_required")
	}
	noParams := func() error { return strictControlParams(req.Params, &controlEmpty{}) }
	switch req.Operation {
	case "capabilities":
		if err := noParams(); err != nil {
			return nil, controlFailure("control_params_invalid")
		}
		return controlCapabilities, nil
	case "status.get":
		if err := noParams(); err != nil {
			return nil, controlFailure("control_params_invalid")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		return localStatus(home), nil
	case "config.get", "config.set":
		var p struct {
			Values map[string]string `json:"values,omitempty"`
		}
		if err := strictControlParams(req.Params, &p); err != nil {
			return nil, controlFailure("control_params_invalid")
		}
		m := initialModel()
		m.nexusDir = nexusDir
		loadEnv(&m)
		if req.Operation == "config.get" && len(p.Values) > 0 {
			return nil, controlFailure("control_params_invalid")
		}
		if req.Operation == "config.set" {
			if len(p.Values) == 0 {
				return nil, controlFailure("control_params_invalid")
			}
			for key, value := range p.Values {
				found := false
				for i, k := range m.configKeys {
					if k == key {
						found = true
						m.configVals[i] = value
					}
				}
				if !found || len(value) > 2048 || strings.ContainsAny(value, "\x00\r\n") {
					return nil, controlFailure("control_params_invalid")
				}
				if key == "NEXUS_LOCAL_AI" && value != "true" && value != "false" {
					return nil, controlFailure("control_params_invalid")
				}
			}
			if err := saveEnv(m); err != nil {
				return nil, err
			}
			loadEnv(&m)
		}
		values := map[string]string{}
		for i, key := range m.configKeys {
			values[key] = m.configVals[i]
		}
		return values, nil
	case "health.check":
		if err := noParams(); err != nil {
			return nil, controlFailure("control_params_invalid")
		}
		m := initialModel()
		m.nexusDir = nexusDir
		loadEnv(&m)
		h := checkHealth(nexusDir, m.configVals[1], configuredClaudeSessionRetentionDays(m), m.tokscaleAdapter)().(healthMsg)
		return map[string]any{"ollamaUp": h.ollamaUp, "links": h.links, "gpu": h.gpu, "tokscale": map[string]any{"state": h.tokscale.state, "version": h.tokscale.version, "clients": h.tokscale.clients}, "claudeSessionRetentionDays": h.claudeSessionRetentionDays}, nil
	case "tasks.list", "usage.get":
		if err := noParams(); err != nil {
			return nil, controlFailure("control_params_invalid")
		}
		path, err := observabilityDBPath()
		if err != nil {
			return nil, err
		}
		entries, _ := loadTaskLogSQLiteReadOnly(path)
		if req.Operation == "tasks.list" {
			return entries, nil
		}
		s := summarizeTaskLog(entries)
		report := loadTokscaleUsage(TokscaleAdapter{})().(usageDashboardMsg)
		aggregates := []map[string]any{}
		for _, e := range report.report.Entries {
			aggregates = append(aggregates, map[string]any{"client": e.Client, "provider": e.Provider, "model": e.Model, "inputTokens": e.InputTokens, "outputTokens": e.OutputTokens, "cacheReadTokens": e.CacheReadTokens, "cacheWriteTokens": e.CacheWriteTokens, "reasoningTokens": e.ReasoningTokens, "messageCount": e.MessageCount, "costUSD": e.CostUSD})
		}
		return map[string]any{"native": map[string]any{"tasks": s.total, "successes": s.successes, "failures": s.failures, "averageMs": s.avgMs, "p95Ms": s.p95Ms, "costUSD": s.costUSD, "cloudEquivalentUSD": s.cloudUSD, "savingsUSD": s.savingsUSD, "models": s.modelTasks, "routes": s.routes, "routeBands": s.routeBands}, "tokscale": map[string]any{"state": report.state, "groupBy": report.report.GroupBy, "entries": aggregates}}, nil
	case "activity.list":
		if err := noParams(); err != nil {
			return nil, controlFailure("control_params_invalid")
		}
		entries, state := readCompanionActivity(defaultObservabilityDatabasePath())
		rows := []map[string]any{}
		for _, e := range entries {
			rows = append(rows, map[string]any{"toolId": e.toolID, "surface": e.surface, "startedAt": e.startedAt, "endedAt": e.endedAt})
		}
		return map[string]any{"state": state, "entries": rows}, nil
	case "companion.command":
		var p struct {
			Args []string `json:"args"`
		}
		if err := strictControlParams(req.Params, &p); err != nil {
			return nil, controlFailure("control_params_invalid")
		}
		var out bytes.Buffer
		code := runCompanionCLI(p.Args, nexusDir, &out)
		var result any
		if json.Unmarshal(out.Bytes(), &result) != nil {
			return nil, controlFailure("control_operation_failed")
		}
		return map[string]any{"exitCode": code, "result": result}, nil
	case "memory.command":
		var p struct {
			Args  []string `json:"args"`
			Input string   `json:"input,omitempty"`
		}
		if err := strictControlParams(req.Params, &p); err != nil || len(p.Args) == 0 {
			return nil, controlFailure("control_params_invalid")
		}
		// Existing parser owns component validation, command allowlists and atomic writes.
		root, err := memoryRoot()
		if err != nil {
			return nil, err
		}
		var out controlBuffer
		var stderr controlBuffer
		code := runMemoryCLI(p.Args, root, strings.NewReader(p.Input), &out, &stderr)
		if code != 0 {
			return nil, controlFailure("control_operation_failed")
		}
		return map[string]any{"output": out.String()}, nil
	case "memory.delete":
		var p struct {
			Project string `json:"project"`
			Name    string `json:"name"`
		}
		if err := strictControlParams(req.Params, &p); err != nil {
			return nil, controlFailure("control_params_invalid")
		}
		root, err := memoryRoot()
		if err != nil {
			return nil, err
		}
		if err := deleteMemory(root, p.Project, p.Name); err != nil {
			return nil, err
		}
		return map[string]bool{"deleted": true}, nil
	case "install.run":
		if err := noParams(); err != nil {
			return nil, controlFailure("control_params_invalid")
		}
		m := initialModel()
		m.nexusDir = nexusDir
		loadEnv(&m)
		steps := buildInstallSteps()
		results := []map[string]any{}
		for i, step := range steps {
			result := runInstallStep(m, i)().(stepDoneMsg)
			results = append(results, map[string]any{"index": i, "label": step.label, "ok": result.ok})
			if !result.ok && isCriticalInstallStep(i) {
				return map[string]any{"ok": false, "steps": results}, nil
			}
		}
		return map[string]any{"ok": true, "steps": results}, nil
	case "update.check":
		if err := noParams(); err != nil {
			return nil, controlFailure("control_params_invalid")
		}
		result := checkLatestVersion()().(versionCheckMsg)
		if result.err != nil {
			return nil, result.err
		}
		return map[string]string{"current": version, "latest": result.latest, "url": result.updateURL}, nil
	case "update.apply":
		var p struct {
			Version string `json:"version"`
		}
		if err := strictControlParams(req.Params, &p); err != nil || !validTag.MatchString(p.Version) {
			return nil, controlFailure("control_params_invalid")
		}
		result := runSelfUpdate(p.Version)().(updateDoneMsg)
		if result.err != nil {
			return nil, result.err
		}
		return map[string]bool{"updated": true}, nil
	case "uninstall.run":
		if err := noParams(); err != nil {
			return nil, controlFailure("control_params_invalid")
		}
		result := runScript(nexusDir, "teardown-nexus.sh")().(cmdDoneMsg)
		if result.err != nil {
			return nil, result.err
		}
		return map[string]bool{"uninstalled": true}, nil
	case "route.preview", "route.run":
		var p struct {
			Prompt     string `json:"prompt"`
			AllowCloud bool   `json:"allowCloud,omitempty"`
		}
		if err := strictControlParams(req.Params, &p); err != nil || strings.TrimSpace(p.Prompt) == "" {
			return nil, controlFailure("control_params_invalid")
		}
		decision := classifyPrompt(p.Prompt)
		if req.Operation == "route.preview" {
			return decision, nil
		}
		if decision.Destination == "agy" && !p.AllowCloud {
			return nil, controlFailure("control_cloud_acknowledgement_required")
		}
		binary, err := os.Executable()
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		args := []string{"route", "--goal", p.Prompt}
		if p.AllowCloud {
			args = append(args, "--allow-cloud-fallback")
		}
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Env = append(os.Environ(), "NEXUS_REPO="+nexusDir)
		var out controlBuffer
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			return nil, err
		}
		return map[string]string{"output": out.String()}, nil
	}
	return nil, controlFailure("control_operation_unknown")
}

type controlBuffer struct{ bytes.Buffer }

func (b *controlBuffer) Write(p []byte) (int, error) {
	if len(p) > controlMaxOutput-b.Len() {
		return 0, errors.New("output limit")
	}
	return b.Buffer.Write(p)
}
func runControlCLI(args []string, nexusDir string, in io.Reader, out io.Writer) int {
	respond := func(response any, code int) int {
		if json.NewEncoder(out).Encode(response) != nil {
			return 1
		}
		return code
	}
	failure := func(code string, status int) int {
		return respond(map[string]any{"schemaVersion": controlProtocolVersion, "ok": false, "error": code}, status)
	}
	if len(args) != 1 || args[0] != "--json" {
		return failure("control_command_invalid", 2)
	}
	raw, err := io.ReadAll(io.LimitReader(in, controlMaxRequest+1))
	if err != nil || len(raw) > controlMaxRequest {
		return failure("control_request_invalid", 2)
	}
	var request controlRequest
	if err := strictControlParams(raw, &request); err != nil {
		return failure("control_request_invalid", 2)
	}
	result, err := executeControl(request, nexusDir)
	if err != nil {
		code := err.Error()
		switch code {
		case "control_protocol_unsupported", "control_operation_unknown", "control_confirmation_required", "control_params_invalid", "control_cloud_acknowledgement_required":
		default:
			code = "control_operation_failed"
		}
		return failure(code, 1)
	}
	// Buffer serialization before emitting so oversized results never become partial JSON.
	payload, err := json.Marshal(map[string]any{"schemaVersion": controlProtocolVersion, "ok": true, "operation": request.Operation, "result": result})
	if err != nil || len(payload)+1 > controlMaxOutput {
		return failure("control_output_unavailable", 1)
	}
	if _, err := out.Write(append(payload, '\n')); err != nil {
		return 1
	}
	return 0
}
