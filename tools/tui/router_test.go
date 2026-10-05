package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassifyPrompt(t *testing.T) {
	cases := []struct{ prompt, destination, task string }{
		{"write a conventional commit message", "local", "commit-msg"},
		{"scaffold test cases", "local", "test-scaffold"},
		{"refactor this parser", "local", "logic-refactor"},
		{"design the authentication architecture", "agy", ""},
		{"help me improve this application", "agy", ""},
	}
	for _, tc := range cases {
		t.Run(tc.prompt, func(t *testing.T) {
			got := classifyPrompt(tc.prompt)
			if got.Destination != tc.destination || got.TaskType != tc.task {
				t.Fatalf("got %#v", got)
			}
		})
	}
}

func TestParseRouteRequest(t *testing.T) {
	got, err := parseRouteRequest([]string{"--dry-run", "--goal", "write", "a", "commit message"})
	if err != nil || !got.DryRun || got.Prompt != "write a commit message" {
		t.Fatalf("got %#v, %v", got, err)
	}
	if _, err := parseRouteRequest([]string{"--goal"}); err == nil {
		t.Fatal("accepted empty goal")
	}
	if _, err := parseRouteRequest([]string{"--tui", "hello"}); err == nil {
		t.Fatal("allowed tui with prompt")
	}
}

func TestRoutePromptDoesNotFallbackWithoutConsent(t *testing.T) {
	oldLocal, oldCloud := runLocalRoute, runCloudRoute
	t.Cleanup(func() { runLocalRoute, runCloudRoute = oldLocal, oldCloud })
	runLocalRoute = func(string, string, string) error { return errors.New("offline") }
	cloudCalled := false
	runCloudRoute = func(string) error { cloudCalled = true; return nil }
	var output bytes.Buffer
	err := routePrompt("/repo", routeRequest{Prompt: "write a conventional commit message"}, &output)
	if err == nil || cloudCalled || !strings.Contains(err.Error(), "--allow-cloud-fallback") {
		t.Fatalf("err=%v cloud=%t", err, cloudCalled)
	}
	err = routePrompt("/repo", routeRequest{Prompt: "write a conventional commit message", AllowCloudFallback: true}, &output)
	if err != nil || !cloudCalled {
		t.Fatalf("err=%v cloud=%t", err, cloudCalled)
	}
}

func TestRoutePromptDryRunNeverExecutes(t *testing.T) {
	oldLocal, oldCloud := runLocalRoute, runCloudRoute
	t.Cleanup(func() { runLocalRoute, runCloudRoute = oldLocal, oldCloud })
	runLocalRoute = func(string, string, string) error { t.Fatal("local executed"); return nil }
	runCloudRoute = func(string) error { t.Fatal("cloud executed"); return nil }
	var output bytes.Buffer
	if err := routePrompt("/repo", routeRequest{Prompt: "refactor this", DryRun: true}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"destination":"local"`) {
		t.Fatal(output.String())
	}
}

func TestLocalAIEnabledUsesFileThenEnvironment(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("NEXUS_LOCAL_AI=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if localAIEnabled(dir) {
		t.Fatal("file setting ignored")
	}
	t.Setenv("NEXUS_LOCAL_AI", "true")
	if !localAIEnabled(dir) {
		t.Fatal("environment override ignored")
	}
}
