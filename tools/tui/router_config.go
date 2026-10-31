package main

import (
	"context"
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

// A short circuit prefers a ready frontier route over repeatedly waiting for an
// unhealthy local node. The file contains only a timestamp, never prompt data.
func routeStatePath(dir string) string { return filepath.Join(dir, ".nexus", "route-failure") }
func routeCoolingDown(dir string) bool {
	data, err := os.ReadFile(routeStatePath(dir))
	if err != nil {
		return false
	}
	stamp, err := strconv.ParseInt(string(data), 10, 64)
	age := time.Since(time.Unix(0, stamp))
	return err == nil && age >= 0 && age < 30*time.Second
}
func recordRouteFailure(dir string) {
	path := routeStatePath(dir)
	if os.MkdirAll(filepath.Dir(path), 0700) == nil {
		_ = os.WriteFile(path, []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0600)
	}
}
func clearRouteFailure(dir string) { _ = os.Remove(routeStatePath(dir)) }
