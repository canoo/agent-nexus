package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type routeConfig struct {
	timeout  time.Duration
	retries  int
	keywords map[string][]string
}

func routeSetting(dir, key, fallback string) string {
	value := parseSettings(readFileOrEmpty(filepath.Join(dir, ".env")))[key]
	if override, ok := os.LookupEnv(key); ok {
		value = override
	}
	if value == "" {
		return fallback
	}
	return value
}
func loadRouteConfig(dir string) (routeConfig, error) {
	cfg := routeConfig{keywords: map[string][]string{}}
	timeout, err := time.ParseDuration(routeSetting(dir, "NEXUS_ROUTE_TIMEOUT", "120s"))
	if err != nil || timeout < time.Millisecond || timeout > 10*time.Minute {
		return cfg, fmt.Errorf("NEXUS_ROUTE_TIMEOUT must be between 1ms and 10m")
	}
	retries, err := strconv.Atoi(routeSetting(dir, "NEXUS_ROUTE_RETRIES", "0"))
	if err != nil || retries < 0 || retries > 3 {
		return cfg, fmt.Errorf("NEXUS_ROUTE_RETRIES must be between 0 and 3")
	}
	cfg.timeout, cfg.retries = timeout, retries
	for _, task := range []string{"commit-msg", "boilerplate", "test-scaffold", "lint-fix", "logic-refactor"} {
		key := "NEXUS_ROUTE_" + strings.ToUpper(strings.ReplaceAll(task, "-", "_")) + "_KEYWORDS"
		value := routeSetting(dir, key, "")
		if value != "" {
			for _, word := range strings.Split(value, ",") {
				if strings.TrimSpace(word) == "" {
					return cfg, fmt.Errorf("%s contains an empty keyword", key)
				}
				cfg.keywords[task] = append(cfg.keywords[task], strings.ToLower(strings.TrimSpace(word)))
			}
		}
	}
	return cfg, nil
}
func (cfg routeConfig) classify(prompt string) routeDecision {
	base := classifyPrompt(prompt)
	// Sensitive classification always wins over configurable rules.
	text := strings.ToLower(prompt)
	for _, keyword := range []string{"security", "auth", "password", "credential", "architecture", "database migration", "production incident", "deploy"} {
		if strings.Contains(text, keyword) {
			return routeDecision{Complexity: "complex", Destination: "agy", Reason: "security-sensitive request"}
		}
	}
	for _, task := range []string{"commit-msg", "boilerplate", "test-scaffold", "lint-fix", "logic-refactor"} {
		for _, keyword := range cfg.keywords[task] {
			if strings.Contains(text, keyword) {
				complexity := "simple"
				if task == "lint-fix" || task == "logic-refactor" || task == "test-scaffold" {
					complexity = "medium"
				}
				return routeDecision{Complexity: complexity, Destination: "local", TaskType: task, Reason: "configured task keyword"}
			}
		}
	}
	return base
}

var routeHTTPClient = &http.Client{Timeout: 5 * time.Second}

func routeHealthPreflight(ctx context.Context, dir string) error {
	endpoint := routeSetting(dir, "OLLAMA_HOST_URL", "http://localhost:11434")
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("invalid OLLAMA_HOST_URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("invalid Ollama health endpoint")
	}
	resp, err := routeHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("local health preflight failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("local health preflight failed")
	}
	return nil
}

// A short circuit stores only an owned marker and timestamp, never prompts.
func routeStatePath(dir string) string { return filepath.Join(dir, ".nexus", "route-failure") }

type routeFailureState struct {
	Owner     string `json:"owner"`
	Timestamp int64  `json:"timestamp"`
}

const routeStateOwner = "nexus-route-failure-v1"

func routeStateRoot(dir string, create bool) (*os.Root, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if create {
		if err := root.Mkdir(".nexus", 0700); err != nil && !os.IsExist(err) {
			return nil, err
		}
	}
	info, err := root.Lstat(".nexus")
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("unsafe route state directory")
	}
	return root.OpenRoot(".nexus")
}
func readRouteState(root *os.Root) (routeFailureState, error) {
	var state routeFailureState
	info, err := root.Lstat("route-failure")
	if err != nil {
		return state, err
	}
	if !info.Mode().IsRegular() || info.Size() > 256 {
		return state, fmt.Errorf("foreign route state")
	}
	data, err := root.ReadFile("route-failure")
	if err != nil {
		return state, err
	}
	if json.Unmarshal(data, &state) != nil || state.Owner != routeStateOwner || state.Timestamp <= 0 {
		return state, fmt.Errorf("foreign route state")
	}
	return state, nil
}
func routeCoolingDown(dir string) bool {
	root, err := routeStateRoot(dir, false)
	if err != nil {
		return false
	}
	defer root.Close()
	state, err := readRouteState(root)
	if err != nil {
		return false
	}
	age := time.Since(time.Unix(0, state.Timestamp))
	return age >= 0 && age < 30*time.Second
}
func recordRouteFailure(dir string) {
	root, err := routeStateRoot(dir, true)
	if err != nil {
		return
	}
	defer root.Close()
	if _, err := readRouteState(root); err != nil && !os.IsNotExist(err) {
		return
	}
	name := ".route-failure-" + rand.Text()
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return
	}
	defer root.Remove(name)
	err = json.NewEncoder(file).Encode(routeFailureState{routeStateOwner, time.Now().UnixNano()})
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return
	}
	// Recheck ownership before replacing the directory entry atomically.
	if _, err := readRouteState(root); err != nil && !os.IsNotExist(err) {
		return
	}
	_ = root.Rename(name, "route-failure")
}
func clearRouteFailure(dir string) {
	root, err := routeStateRoot(dir, false)
	if err != nil {
		return
	}
	defer root.Close()
	if _, err := readRouteState(root); err == nil {
		_ = root.Remove("route-failure")
	}
}
func (cfg routeConfig) decision(dir, prompt string) routeDecision {
	decision := cfg.classify(prompt)
	if decision.Destination == "local" && routeCoolingDown(dir) {
		decision.Reason = "local route cooling down after failure or timeout"
	}
	return decision
}
