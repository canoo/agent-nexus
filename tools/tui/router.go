package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type routeDecision struct {
	Complexity  string `json:"complexity"`
	Destination string `json:"destination"`
	TaskType    string `json:"taskType,omitempty"`
	Reason      string `json:"reason"`
}

type routeRequest struct {
	Prompt             string
	DryRun             bool
	AllowCloudFallback bool
}

func parseRouteRequest(args []string) (routeRequest, error) {
	request := routeRequest{}
	var prompt []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			request.DryRun = true
		case "--allow-cloud-fallback":
			request.AllowCloudFallback = true
		case "--goal":
			if i+1 == len(args) {
				return routeRequest{}, errors.New("--goal requires a prompt")
			}
			i++
			prompt = append(prompt, args[i])
		case "--tui":
			return routeRequest{}, errors.New("--tui cannot be combined with a routed prompt")
		default:
			if strings.HasPrefix(args[i], "--goal=") {
				value := strings.TrimPrefix(args[i], "--goal=")
				if value == "" {
					return routeRequest{}, errors.New("--goal requires a prompt")
				}
				prompt = append(prompt, value)
			} else {
				prompt = append(prompt, args[i])
			}
		}
	}
	request.Prompt = strings.TrimSpace(strings.Join(prompt, " "))
	if request.Prompt == "" {
		return routeRequest{}, errors.New("provide a prompt or use --tui")
	}
	return request, nil
}

func classifyPrompt(prompt string) routeDecision {
	text := strings.ToLower(prompt)
	for _, keyword := range []string{"security", "authenticate", "authentication", "authorization", "password", "credential", "architecture", "database migration", "production incident", "deploy"} {
		if strings.Contains(text, keyword) {
			return routeDecision{Complexity: "complex", Destination: "agy", Reason: "sensitive or architecture keyword: " + keyword}
		}
	}
	for _, rule := range []struct {
		task, complexity string
		keywords         []string
	}{
		{"commit-msg", "simple", []string{"commit message", "commit msg", "conventional commit"}},
		{"boilerplate", "simple", []string{"boilerplate", "generate a component", "generate component"}},
		{"test-scaffold", "simple", []string{"test scaffold", "scaffold test"}},
		{"lint-fix", "medium", []string{"lint error", "lint fix", "fix lint"}},
		{"logic-refactor", "medium", []string{"refactor", "extract function"}},
	} {
		for _, keyword := range rule.keywords {
			if strings.Contains(text, keyword) {
				return routeDecision{Complexity: rule.complexity, Destination: "local", TaskType: rule.task, Reason: "recognized " + rule.task + " request"}
			}
		}
	}
	return routeDecision{Complexity: "complex", Destination: "agy", Reason: "ambiguous requests use the interactive frontier agent"}
}

var runLocalRoute = defaultLocalRoute
var runCloudRoute = defaultCloudRoute

func routePrompt(nexusDir string, request routeRequest, output io.Writer) error {
	decision := classifyPrompt(request.Prompt)
	if request.DryRun {
		return json.NewEncoder(output).Encode(decision)
	}
	if decision.Destination == "agy" {
		return runCloudRoute(request.Prompt)
	}
	if err := runLocalRoute(nexusDir, decision.TaskType, request.Prompt); err != nil {
		if !request.AllowCloudFallback {
			return fmt.Errorf("local %s route failed: %w; rerun with --allow-cloud-fallback to send this prompt to agy", decision.TaskType, err)
		}
		fmt.Fprintln(os.Stderr, "NEXUS: local route failed; using explicitly approved cloud fallback.")
		return runCloudRoute(request.Prompt)
	}
	return nil
}

func defaultLocalRoute(nexusDir, taskType, prompt string) error {
	if !localAIEnabled(nexusDir) {
		return errors.New("NEXUS_LOCAL_AI is disabled")
	}
	context, err := os.CreateTemp("", "nexus-route-*")
	if err != nil {
		return err
	}
	path := context.Name()
	defer os.Remove(path)
	if err := context.Chmod(0600); err != nil {
		context.Close()
		return err
	}
	if _, err := context.WriteString(prompt); err != nil {
		context.Close()
		return err
	}
	if err := context.Close(); err != nil {
		return err
	}
	cmd := exec.Command("bash", filepath.Join(nexusDir, "tools", "automation", "ollama-delegate.sh"), taskType, path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func localAIEnabled(nexusDir string) bool {
	values := parseSettings(readFileOrEmpty(filepath.Join(nexusDir, ".env")))
	value := values["NEXUS_LOCAL_AI"]
	if override, ok := os.LookupEnv("NEXUS_LOCAL_AI"); ok {
		value = override
	}
	return value != "false"
}

func readFileOrEmpty(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func defaultCloudRoute(prompt string) error {
	if _, err := exec.LookPath("agy"); err != nil {
		return errors.New("agy is not installed or not on PATH")
	}
	cmd := exec.Command("agy", "--prompt-interactive", prompt)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
