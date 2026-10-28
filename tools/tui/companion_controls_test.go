package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCompanionControlsRejectInvalidBeforeHelper(t *testing.T) {
	for _, args := range [][]string{{"pause"}, {"resume", "--json", "extra"}, {"disable", "--json"}, {"consent", "grant", "--adapter", "browser-chrome", "--tool", "chatgpt", "--json"}, {"consent", "revoke", "--adapter", "browser-firefox", "--tool", "chatgpt", "--json"}} {
		var out bytes.Buffer
		if code := runCompanionCLI(args, "/missing", &out); code != 2 {
			t.Fatalf("%v: %d %s", args, code, out.String())
		}
	}
}
func TestCompanionControlsBinaryLifecycle(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node required")
	}
	cwd, _ := os.Getwd()
	root := filepath.Clean(filepath.Join(cwd, "../.."))
	home := t.TempDir()
	binary := filepath.Join(t.TempDir(), "nexus")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, output)
	}
	// Actual executable uses its pinned runtime even when node is absent from PATH.
	t.Setenv("HOME", home)
	payload := filepath.Join(t.TempDir(), "relocated host")
	stage := exec.Command(node, "--input-type=module", "-e", "import {stageNativeHostPackage} from './apps/companion-native-host/lib/package.mjs';stageNativeHostPackage({outputDir:process.argv[1],version:'0.3.0-dev.3'});", payload)
	stage.Dir = root
	if output, err := stage.CombinedOutput(); err != nil {
		t.Fatalf("stage: %v %s", err, output)
	}
	t.Setenv("NEXUS_REPO", payload)
	t.Setenv("NEXUS_COMPANION_NODE", node)
	init := exec.Command(node, "--input-type=module", "-e", "import {createObservabilityStore} from './tools/mcp/lib/observability-store.mjs';createObservabilityStore().migrate();")
	init.Dir = root
	if output, err := init.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, output)
	}
	t.Setenv("PATH", t.TempDir())
	run := func(want int, args ...string) companionControlsResponse {
		t.Helper()
		cmd := exec.Command(binary, append([]string{"companion"}, args...)...)
		var out, stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		err := cmd.Run()
		code := 0
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				code = e.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		if code != want || stderr.Len() != 0 {
			t.Fatalf("%v code=%d want=%d stdout=%s stderr=%s", args, code, want, out.String(), stderr.String())
		}
		var result companionControlsResponse
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	status := run(0, "status", "--json")
	if *status.CollectionEnabled || len(status.Consents) != 10 {
		t.Fatal(status)
	}
	if result := run(1, "resume", "--json"); result.Error != "companion_consent_required" {
		t.Fatal(result)
	}
	grant := []string{"consent", "grant", "--adapter", "browser-chrome", "--tool", "chatgpt", "--acknowledge-policy", "1", "--json"}
	status = run(0, grant...)
	if *status.CollectionEnabled || !*status.Consents[0].Enabled {
		t.Fatal(status)
	}
	if !*run(0, "resume", "--json").CollectionEnabled {
		t.Fatal("resume failed")
	}
	if *run(0, "pause", "--json").CollectionEnabled {
		t.Fatal("pause failed")
	}
	status = run(0, "consent", "revoke", "--adapter", "browser-chrome", "--tool", "chatgpt", "--json")
	if *status.Consents[0].Enabled {
		t.Fatal("revoke failed")
	}
	run(0, grant...)
	status = run(0, "disable", "--confirm", "--json")
	if *status.CollectionEnabled {
		t.Fatal("disable failed")
	}
	for _, c := range status.Consents {
		if *c.Enabled {
			t.Fatal("grant remained")
		}
	}
	// Existing data commands also use the pinned executable.
	cmd := exec.Command(binary, "companion", "data", "--json")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	t.Setenv("NEXUS_COMPANION_NODE", "/missing/runtime")
	run(1, "status", "--json")
}
func TestCompanionControlsRejectUnsafeHelperReplies(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tools", "mcp")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{
		`process.stdout.write(JSON.stringify({schemaVersion:1,ok:false,error:"private SQL/path"}));process.exitCode=1;`,
		`process.stdout.write(JSON.stringify({schemaVersion:1,ok:true,action:"status",collectionEnabled:false,policyVersion:1,retentionDays:14,storedSpans:0,consents:[],private:"path"}));`,
		`process.stdout.write("x".repeat(5000));`,
	} {
		if err := os.WriteFile(filepath.Join(dir, "companion-controls.mjs"), []byte(script), 0600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if code := runCompanionCLI([]string{"status", "--json"}, root, &out); code != 1 {
			t.Fatal(code)
		}
		if bytes.Contains(out.Bytes(), []byte("private")) || !bytes.Contains(out.Bytes(), []byte("companion_controls_unavailable")) {
			t.Fatal(out.String())
		}
	}
}

func TestCompanionControlsRejectForgedSuccessfulResponses(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tools", "mcp")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"valid", "missing-enabled", "duplicate-tool", "trailing-json", "no-resume-grant", "wrong-action", "disabled-resume"} {
		enabled := true
		count := int64(0)
		days := 14
		r := companionControlsResponse{SchemaVersion: 1, OK: &enabled, Action: "resume", CollectionEnabled: &enabled, PolicyVersion: 1, RetentionDays: &days, StoredSpans: &count}
		for _, adapter := range []string{"browser-chrome", "browser-edge"} {
			for _, tool := range []string{"chatgpt", "claude", "gemini", "copilot", "perplexity"} {
				yes := true
				r.Consents = append(r.Consents, companionConsent{AdapterID: adapter, ToolID: tool, Enabled: &yes})
			}
		}
		switch variant {
		case "missing-enabled":
			r.Consents[0].Enabled = nil
		case "duplicate-tool":
			r.Consents[1].ToolID = "chatgpt"
		case "no-resume-grant":
			for i := range r.Consents {
				no := false
				r.Consents[i].Enabled = &no
			}
		case "wrong-action":
			r.Action = "pause"
		case "disabled-resume":
			no := false
			r.CollectionEnabled = &no
		}
		payload, _ := json.Marshal(r)
		if variant == "trailing-json" {
			payload = append(payload, []byte(" {}")...)
		}
		quoted, _ := json.Marshal(string(payload))
		if err := os.WriteFile(filepath.Join(dir, "companion-controls.mjs"), []byte("process.stdout.write("+string(quoted)+");"), 0600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		code := runCompanionCLI([]string{"resume", "--json"}, root, &out)
		if variant == "valid" {
			if code != 0 {
				t.Fatalf("valid: %s", out.String())
			}
		} else if code != 1 || !bytes.Contains(out.Bytes(), []byte("companion_controls_unavailable")) {
			t.Fatalf("%s: %d %s", variant, code, out.String())
		}
	}
}
