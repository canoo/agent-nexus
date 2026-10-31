package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
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
			} else if strings.HasPrefix(args[i], "-") {
				return routeRequest{}, fmt.Errorf("unknown route option: %s", args[i])
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
		{"test-scaffold", "medium", []string{"test scaffold", "scaffold test"}},
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return routePromptContext(ctx, nexusDir, request, output)
}

func routePromptContext(ctx context.Context, nexusDir string, request routeRequest, output io.Writer) error {
	cfg, err := loadRouteConfig(nexusDir)
	if err != nil {
		return err
	}
	decision := cfg.classify(request.Prompt)
	if decision.Destination == "local" && routeCoolingDown(nexusDir) {
		decision.Reason = "local route cooling down after failure or timeout"
		if request.DryRun {
			return json.NewEncoder(output).Encode(decision)
		}
		if !request.AllowCloudFallback {
			return errors.New("local route cooling down; rerun with --allow-cloud-fallback to send this prompt to agy")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fmt.Fprintln(os.Stderr, "NEXUS route: failover=agy consent=true reason=cooldown")
		return runCloudRoute(ctx, request.Prompt)
	}
	if request.DryRun {
		return json.NewEncoder(output).Encode(decision)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if decision.Destination == "agy" {
		return runCloudRoute(ctx, request.Prompt)
	}
	var routeErr error
	for attempt := 0; attempt <= cfg.retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		started := time.Now()
		attemptCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
		routeErr = runLocalRoute(attemptCtx, nexusDir, decision.TaskType, request.Prompt)
		cancel()
		status := "success"
		if routeErr != nil {
			status = "failed"
		}
		fmt.Fprintf(os.Stderr, "NEXUS route: task=%s attempt=%d elapsed_ms=%d status=%s\n", decision.TaskType, attempt+1, time.Since(started).Milliseconds(), status)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if routeErr == nil {
			clearRouteFailure(nexusDir)
			return nil
		}
		if attempt < cfg.retries {
			timer := time.NewTimer(time.Duration(1<<attempt) * 100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	recordRouteFailure(nexusDir)
	if !request.AllowCloudFallback {
		return errors.New("local route unavailable; rerun with --allow-cloud-fallback to send this prompt to agy")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	fmt.Fprintln(os.Stderr, "NEXUS route: failover=agy consent=true")
	return runCloudRoute(ctx, request.Prompt)
}

func defaultLocalRoute(ctx context.Context, nexusDir, taskType, prompt string) error {
	if !localAIEnabled(nexusDir) {
		return errors.New("NEXUS_LOCAL_AI is disabled")
	}
	if err := routeHealthPreflight(ctx, nexusDir); err != nil {
		return err
	}
	contextFile, err := os.CreateTemp("", "nexus-route-*")
	if err != nil {
		return err
	}
	path := contextFile.Name()
	defer os.Remove(path)
	if err := contextFile.Chmod(0600); err != nil {
		contextFile.Close()
		return err
	}
	if _, err := contextFile.WriteString(prompt); err != nil {
		contextFile.Close()
		return err
	}
	if err := contextFile.Close(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "bash", filepath.Join(nexusDir, "tools", "automation", "ollama-delegate.sh"), taskType, path)
	var output controlBuffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, &output, &output
	configureRouteProcess(cmd)
	err = cmd.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil {
		_, err = os.Stdout.Write(output.Bytes())
	}
	return err
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

func defaultCloudRoute(ctx context.Context, prompt string) error {
	if _, err := exec.LookPath("agy"); err != nil {
		return errors.New("agy is not installed or not on PATH")
	}
	cmd := exec.CommandContext(ctx, "agy", "--prompt-interactive", prompt)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	configureRouteProcess(cmd)
	return cmd.Run()
}
