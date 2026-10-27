package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

const (
	defaultOllamaURL     = "http://localhost:11434"
	ollamaReadyTimeout   = 20 * time.Second
	ollamaReadyPollEvery = 500 * time.Millisecond
	ollamaHTTPTimeout    = 3 * time.Second
)

// defaultOllamaModels are pulled during install when local AI is enabled.
var defaultOllamaModels = []string{"qwen2.5-coder:1.5b", "llama3.2:3b"}

// ensureOllamaReady installs the ollama binary if missing, starts the daemon
// when the host is unreachable, and waits until the HTTP API responds.
func ensureOllamaReady(ollamaURL string) error {
	if ollamaURL == "" {
		ollamaURL = defaultOllamaURL
	}
	if _, err := exec.LookPath("ollama"); err != nil {
		if err := installOllamaBinary(); err != nil {
			return fmt.Errorf("ollama not installed: %w — install from https://ollama.com", err)
		}
		if _, err := exec.LookPath("ollama"); err != nil {
			return errors.New("ollama installed but not on PATH — open a new shell or install from https://ollama.com")
		}
	}
	if ollamaReachable(ollamaURL) {
		return nil
	}
	if err := startOllamaDaemon(); err != nil {
		return fmt.Errorf("ollama not running: %w — start with: ollama serve", err)
	}
	deadline := time.Now().Add(ollamaReadyTimeout)
	for time.Now().Before(deadline) {
		if ollamaReachable(ollamaURL) {
			return nil
		}
		time.Sleep(ollamaReadyPollEvery)
	}
	return fmt.Errorf("ollama started but not reachable at %s within %s", ollamaURL, ollamaReadyTimeout)
}

func ollamaReachable(ollamaURL string) bool {
	client := &http.Client{Timeout: ollamaHTTPTimeout}
	resp, err := client.Get(ollamaURL)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

func installOllamaBinary() error {
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("brew"); err != nil {
			return errors.New("Homebrew not found; install from https://brew.sh or download Ollama from https://ollama.com")
		}
		cmd := exec.Command("brew", "install", "ollama")
		cmd.Env = append(os.Environ(), "HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ENV_HINTS=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("brew install ollama failed: %w (%s)", err, truncateOutput(out))
		}
		return nil
	case "linux":
		// Official installer; user opted into local AI in the TUI wizard.
		cmd := exec.Command("sh", "-c", "curl -fsSL https://ollama.com/install.sh | sh")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("ollama install script failed: %w (%s)", err, truncateOutput(out))
		}
		return nil
	default:
		return fmt.Errorf("unsupported OS %q; install from https://ollama.com", runtime.GOOS)
	}
}

func startOllamaDaemon() error {
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("brew"); err == nil {
			if err := exec.Command("brew", "services", "start", "ollama").Run(); err == nil {
				return nil
			}
		}
		return startOllamaServeBackground()
	case "linux":
		if err := exec.Command("systemctl", "is-active", "--quiet", "ollama").Run(); err == nil {
			return nil
		}
		if err := exec.Command("systemctl", "start", "ollama").Run(); err == nil {
			return nil
		}
		if err := exec.Command("systemctl", "--user", "start", "ollama").Run(); err == nil {
			return nil
		}
		return startOllamaServeBackground()
	default:
		return startOllamaServeBackground()
	}
}

func startOllamaServeBackground() error {
	cmd := exec.Command("ollama", "serve")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return err
	}
	// Detach from the installer process; the daemon keeps running.
	_ = cmd.Process.Release()
	return nil
}

func pullDefaultOllamaModels() []string {
	var pulled []string
	for _, name := range defaultOllamaModels {
		cmd := exec.Command("ollama", "pull", name)
		if err := cmd.Run(); err == nil {
			pulled = append(pulled, name)
		}
	}
	return pulled
}

func truncateOutput(out []byte) string {
	const max = 240
	s := string(out)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
