package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func controlCall(t *testing.T, root, operation string, confirm bool, params any) (map[string]any, int) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"schemaVersion": 1, "operation": operation, "confirm": confirm, "params": params})
	var out bytes.Buffer
	code := runControlCLI([]string{"--json"}, root, bytes.NewReader(raw), &out)
	var reply map[string]any
	if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
		t.Fatalf("invalid JSON: %s", out.String())
	}
	return reply, code
}
func TestControlCatalogCoversEveryTUIMenuFeature(t *testing.T) {
	seen := map[string]bool{}
	ids := map[string]bool{}
	for _, capability := range controlCapabilities {
		if ids[capability.ID] {
			t.Fatalf("duplicate %s", capability.ID)
		}
		ids[capability.ID] = true
		seen[capability.Feature] = true
		if capability.Confirmation && !capability.Mutates {
			t.Fatal(capability)
		}
	}
	for _, feature := range menuItems {
		if !seen[feature] {
			t.Fatalf("TUI feature %q has no shared operation", feature)
		}
	}
}
func TestControlProtocolRejectsUnboundedUnknownAndUnconfirmedRequests(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	for _, raw := range []string{"null", "{}", `{"schemaVersion":1,"operation":"status.get","private":"secret"}`, `{"schemaVersion":1,"operation":"status.get"} {}`, strings.Repeat("x", controlMaxRequest+1)} {
		var out bytes.Buffer
		if code := runControlCLI([]string{"--json"}, root, strings.NewReader(raw), &out); code == 0 {
			t.Fatal(raw)
		}
		if bytes.Contains(out.Bytes(), []byte("secret")) {
			t.Fatal(out.String())
		}
	}
	for _, op := range []string{"install.run", "config.set", "update.apply", "uninstall.run", "memory.delete", "route.run"} {
		result, code := controlCall(t, root, op, false, map[string]any{})
		if code != 1 || result["error"] != "control_confirmation_required" {
			t.Fatal(op, result)
		}
	}
	for _, op := range []string{"status.get", "capabilities", "activity.list", "tasks.list", "usage.get", "health.check", "install.run", "uninstall.run", "update.check"} {
		result, code := controlCall(t, root, op, true, map[string]any{"unexpected": "private"})
		if code != 1 || result["error"] != "control_params_invalid" {
			t.Fatal(op, result)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".env")); !os.IsNotExist(err) {
		t.Fatal("read/invalid requests wrote settings")
	}
}
func TestControlConfigUsesExistingTUISettingsWriter(t *testing.T) {
	root := t.TempDir()
	t.Setenv("NEXUS_REPO", root)
	path := filepath.Join(root, ".env")
	os.WriteFile(path, []byte("UNOWNED=preserved\nNEXUS_LOCAL_AI=true\n"), 0600)
	result, code := controlCall(t, root, "config.set", true, map[string]any{"values": map[string]string{"NEXUS_LOCAL_AI": "false", "NEXUS_CLAUDE_SESSION_RETENTION_DAYS": "14"}})
	if code != 0 {
		t.Fatal(result)
	}
	got := result["result"].(map[string]any)
	if got["NEXUS_LOCAL_AI"] != "false" || got["NEXUS_CLAUDE_SESSION_RETENTION_DAYS"] != "14" {
		t.Fatal(got)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Contains(data, []byte("UNOWNED=preserved")) {
		t.Fatal(string(data))
	}
	for key, value := range map[string]string{"SECRET": "do-not-write", "NEXUS_LOCAL_AI": "invalid", "OLLAMA_HOST_URL": "value\nBAD=1"} {
		result, code := controlCall(t, root, "config.set", true, map[string]any{"values": map[string]string{key: value}})
		if code != 1 || result["error"] != "control_params_invalid" {
			t.Fatal(result)
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(data, after) {
		t.Fatal("failed write changed settings")
	}
}
func TestControlMemoryUsesSameStoreAndConfirmsDeletion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	call := func(args ...string) map[string]any {
		t.Helper()
		result, code := controlCall(t, root, "memory.command", false, map[string]any{"args": args})
		if code != 0 {
			t.Fatal(result)
		}
		return result["result"].(map[string]any)
	}
	call("init", "shared")
	saved := call("save", "shared", "--title", "Shared engine", "--body", "Both clients read this")
	filename := filepath.Base(strings.TrimSpace(saved["output"].(string)))
	if !strings.Contains(call("show", "shared", filename)["output"].(string), "Both clients read this") {
		t.Fatal("missing content")
	}
	if !strings.Contains(call("search", "Both clients")["output"].(string), filename) {
		t.Fatal("search failed")
	}
	result, code := controlCall(t, root, "memory.delete", true, map[string]any{"project": "shared", "name": filename})
	if code != 0 {
		t.Fatal(result)
	}
	if _, err := os.Stat(filepath.Join(home, ".config/nexus/agent-memory/shared", filename)); !os.IsNotExist(err) {
		t.Fatal("not deleted")
	}
	result, code = controlCall(t, root, "memory.delete", true, map[string]any{"project": "..", "name": "secret"})
	if code != 1 || result["error"] != "control_operation_failed" {
		t.Fatal(result)
	}
}
func TestControlReadOperationsAndHealthDoNotInitializeStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	t.Setenv("OLLAMA_HOST_URL", server.URL)
	root := t.TempDir()
	for _, op := range []string{"status.get", "config.get", "tasks.list", "usage.get", "activity.list", "health.check"} {
		result, code := controlCall(t, root, op, false, map[string]any{})
		if code != 0 {
			t.Fatal(op, result)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".config/nexus/logs/observability.sqlite")); !os.IsNotExist(err) {
		t.Fatal("reader created DB")
	}
}
func TestControlRoutingPreviewAndCloudAcknowledgement(t *testing.T) {
	result, code := controlCall(t, t.TempDir(), "route.preview", false, map[string]any{"prompt": "production incident"})
	if code != 0 || result["result"].(map[string]any)["destination"] != "agy" {
		t.Fatal(result)
	}
	result, code = controlCall(t, t.TempDir(), "route.run", true, map[string]any{"prompt": "production incident"})
	if code != 1 || result["error"] != "control_cloud_acknowledgement_required" {
		t.Fatal(result)
	}
}
func TestControlActualBinarySpeaksJSONWithoutTerminal(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "nexus")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	cmd := exec.Command(binary, "control", "--json")
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
	cmd.Stdin = strings.NewReader(`{"schemaVersion":1,"operation":"capabilities"}`)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil || stderr.Len() > 0 {
		t.Fatalf("%v %s", err, stderr.String())
	}
	var reply map[string]any
	if json.Unmarshal(stdout.Bytes(), &reply) != nil || reply["ok"] != true {
		t.Fatal(stdout.String())
	}
}

func TestControlInstallAndUninstallReuseWorkersInIsolatedHome(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NEXUS_REPO", root)
	bin := t.TempDir()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash required")
	}
	if err := os.Symlink(bash, filepath.Join(bin, "bash")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	for _, dir := range []string{"core", "personas", "tools/mcp/node_modules", "prompts", "agent-memory"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"core/NEXUS.md", "core/CLAUDE.md", "core/kiro-nexus-steering.md", "tools/mcp/package-lock.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	result, code := controlCall(t, root, "install.run", true, map[string]any{})
	if code != 0 || result["result"].(map[string]any)["ok"] != true {
		t.Fatal(result)
	}
	link := filepath.Join(home, ".gemini/GEMINI.md")
	if _, err := os.Readlink(link); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".kiro/settings/mcp.json")); err != nil {
		t.Fatal(err)
	}
	// Controlled fixture exercises only the existing fixed teardown script path.
	if err := os.WriteFile(filepath.Join(root, "teardown-nexus.sh"), []byte("printf done > \"$HOME/teardown-ran\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	result, code = controlCall(t, root, "uninstall.run", true, map[string]any{})
	if code != 0 {
		t.Fatal(result)
	}
	if data, err := os.ReadFile(filepath.Join(home, "teardown-ran")); err != nil || string(data) != "done" {
		t.Fatal("fixed teardown worker not invoked", err)
	}
}
