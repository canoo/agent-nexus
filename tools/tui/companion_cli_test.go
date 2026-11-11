package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCompanionCLIRejectsBeforeHelper(t *testing.T) {
	for _, args := range [][]string{{}, {"initialize", "--json"}, {"initialize", "--confirm"}, {"clear", "--json"}, {"clear", "--confirm"}, {"retention", "--days", "-1", "--json"}, {"retention", "--days", "366", "--json"}, {"retention", "--days", "1.5", "--json"}, {"data", "--json", "extra"}} {
		var out bytes.Buffer
		if code := runCompanionCLI(args, "/no/helper", &out); code != 2 {
			t.Fatalf("%v code %d", args, code)
		}
		if !bytes.Contains(out.Bytes(), []byte(`"error":"companion_command_invalid"`)) {
			t.Fatal(out.String())
		}
	}
}
func TestCompanionCLIRealBinary(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node required")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(cwd, "../.."))
	home := t.TempDir()
	bin := filepath.Join(t.TempDir(), "nexus")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	t.Setenv("HOME", home)
	t.Setenv("NEXUS_REPO", root)
	run := func(args []string, wantCode int) map[string]any {
		t.Helper()
		cmd := exec.Command(bin, append([]string{"companion"}, args...)...)
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
		if code != wantCode {
			t.Fatalf("%v: code%d expected%d stdout%s stderr%s", args, code, wantCode, out.String(), stderr.String())
		}
		var reply map[string]any
		if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
			t.Fatal(err, out.String())
		}
		return reply
	}
	if run([]string{"data", "--json"}, 1)["error"] != "companion_store_unavailable" {
		t.Fatal("missing store must fail")
	}
	if _, err := os.Stat(filepath.Join(home, ".config")); !os.IsNotExist(err) {
		t.Fatal("status created data")
	}
	if run([]string{"initialize", "--json"}, 2)["error"] != "companion_command_invalid" {
		t.Fatal("unconfirmed setup accepted")
	}
	if _, err := os.Stat(filepath.Join(home, ".config")); !os.IsNotExist(err) {
		t.Fatal("unconfirmed setup created data")
	}
	if run([]string{"initialize", "--confirm", "--json"}, 0)["storedSpans"] != float64(0) {
		t.Fatal("fresh setup failed")
	}
	if run([]string{"initialize", "--confirm", "--json"}, 1)["error"] != "companion_store_exists" {
		t.Fatal("existing store must never be replaced")
	}
	setup := exec.Command("node", "--input-type=module", "-e", `import {createObservabilityStore} from './tools/mcp/lib/observability-store.mjs'; import {DatabaseSync} from 'node:sqlite'; const s=createObservabilityStore();s.migrate();const db=new DatabaseSync(s.databasePath);db.exec("INSERT INTO tool_activity (id,tool_id,surface,started_at,ended_at,detector,confidence,browser_family,platform,schema_version,consent_policy_version) VALUES ('old','chatgpt','browser','2000-01-01T00:00:00Z','2000-01-01T00:00:01Z','selected-browser-tab','surface-active','chrome','linux',1,1)");db.close();`)
	setup.Dir = root
	if out, err := setup.CombinedOutput(); err != nil {
		t.Fatalf("setup: %v %s", err, out)
	}
	if run([]string{"data", "--json"}, 0)["storedSpans"] != float64(1) {
		t.Fatal("read-only status unexpectedly pruned")
	}
	if run([]string{"clear", "--json"}, 2)["error"] != "companion_command_invalid" {
		t.Fatal("unconfirmed deletion")
	}
	if run([]string{"data", "--json"}, 0)["storedSpans"] != float64(1) {
		t.Fatal("unconfirmed clear changed history")
	}
	if run([]string{"prune", "--json"}, 0)["deleted"] != float64(1) {
		t.Fatal("prune failed")
	}
	if run([]string{"retention", "--days", "0", "--json"}, 0)["retentionDays"] != float64(0) {
		t.Fatal("policy mismatch")
	}
	if run([]string{"clear", "--confirm", "--json"}, 0)["deleted"] != float64(0) {
		t.Fatal("clear failed")
	}
}
func TestCompanionCLIRejectsPrivateHelperOutput(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node required")
	}
	root := t.TempDir()
	path := filepath.Join(root, "tools", "mcp")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{`{"schemaVersion":1,"ok":false,"error":"private SQL/path"}`, `{"schemaVersion":1,"ok":true,"action":"status","retentionDays":14,"storedSpans":-1}`, `{"schemaVersion":1,"ok":true,"action":"status","retentionDays":14,"storedSpans":0,"private":"path"}`, `{"schemaVersion":1,"ok":true,"action":"clear","retentionDays":14,"deleted":0}`} {
		encoded, _ := json.Marshal(payload)
		exitCode := "0"
		var probe map[string]any
		if err := json.Unmarshal([]byte(payload), &probe); err != nil {
			t.Fatal(err)
		}
		if probe["ok"] != true {
			exitCode = "1"
		}
		script := "process.stdout.write(" + string(encoded) + ");process.exitCode=" + exitCode + ";"
		if err := os.WriteFile(filepath.Join(path, "companion-data.mjs"), []byte(script), 0600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if code := runCompanionCLI([]string{"data", "--json"}, root, &out); code != 1 {
			t.Fatal(code)
		}
		if bytes.Contains(out.Bytes(), []byte("private")) || !bytes.Contains(out.Bytes(), []byte("companion_data_unavailable")) {
			t.Fatal(out.String())
		}
	}
}
