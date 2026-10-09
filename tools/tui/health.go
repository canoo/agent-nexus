package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

type healthMsg struct {
	ollamaUp                   bool
	links                      []string
	gpu                        gpuInfo
	tokscale                   tokscaleHealth
	claudeSessionRetentionDays int
}

// tokscaleHealthState is deliberately separate from the health of NEXUS.
// Tokscale is optional, so its absence can never make the NEXUS health check
// fail.
type tokscaleHealthState int

const (
	tokscaleHealthUnknown tokscaleHealthState = iota
	tokscaleHealthReady
	tokscaleHealthUnavailable
	tokscaleHealthDegraded
)

type tokscaleHealth struct {
	state   tokscaleHealthState
	version string
	clients []string
}

// --- health ---

func updateHealth(msg tea.Msg, m model) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case healthMsg:
		m.running = false
		m.health = msg
	case spinner.TickMsg:
		if m.running {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func healthView(m model) string {
	s := m.styles.title.Render("⚡ Health Check") + "\n\n"
	if m.running {
		s += m.spinner.View() + " Checking...\n"
	} else {
		if m.health.ollamaUp {
			s += m.styles.success.Render("✓ Ollama reachable") + "\n"
		} else {
			s += m.styles.errStyle.Render("✗ Ollama unreachable") + "\n"
		}

		s += "\nGPU:\n"
		if m.health.gpu.Platform == "unknown" {
			s += "  " + m.styles.warn.Render("⚠ No GPU detected") + "\n"
		} else {
			s += "  " + m.styles.success.Render("✓ "+m.health.gpu.String()) + "\n"
			sup, logic := m.health.gpu.RecommendedModels()
			s += m.styles.subtle.Render(fmt.Sprintf("    Recommended: supervisor=%s  logic=%s", sup, logic)) + "\n"
		}

		s += "\nTokscale (optional):\n"
		switch m.health.tokscale.state {
		case tokscaleHealthReady:
			s += "  " + m.styles.success.Render("✓ Tokscale "+m.health.tokscale.version+" installed") + "\n"
			s += m.styles.subtle.Render("    Model usage aggregates available; clients: "+strings.Join(m.health.tokscale.clients, ", ")) + "\n"
		case tokscaleHealthUnavailable:
			s += "  " + m.styles.subtle.Render("○ Not installed — optional; NEXUS health is unaffected") + "\n"
		case tokscaleHealthDegraded:
			label := "⚠ Tokscale"
			if m.health.tokscale.version != "" {
				label += " " + m.health.tokscale.version
			}
			s += "  " + m.styles.warn.Render(label+" installed, but local usage aggregates are unavailable") + "\n"
		default:
			s += "  " + m.styles.subtle.Render("○ Status not checked") + "\n"
		}

		s += "\nClaude session retention (NEXUS guidance):\n"
		s += m.styles.subtle.Render("  Guidance for usage-history completeness; it does not modify Claude's own retention.") + "\n"
		switch {
		case m.health.claudeSessionRetentionDays == 0:
			s += "  " + m.styles.subtle.Render("○ Unknown or disabled — set NEXUS_CLAUDE_SESSION_RETENTION_DAYS=30 for more complete usage history.") + "\n"
		case m.health.claudeSessionRetentionDays < defaultClaudeSessionRetention:
			s += "  " + m.styles.warn.Render(fmt.Sprintf("⚠ NEXUS guidance is %d days; 30+ days is recommended for more complete usage history.", m.health.claudeSessionRetentionDays)) + "\n"
		default:
			s += "  " + m.styles.success.Render(fmt.Sprintf("✓ NEXUS guidance is %d days", m.health.claudeSessionRetentionDays)) + "\n"
		}

		s += "\nSymlinks:\n"
		for _, l := range m.health.links {
			s += "  " + l + "\n"
		}
	}
	s += "\n" + m.styles.subtle.Render("esc: back")
	return m.borderBox(s)
}

func checkHealth(nexusDir, ollamaURL string, claudeSessionRetentionDays int, tokscale TokscaleAdapter) tea.Cmd {
	return func() tea.Msg {
		h := healthMsg{claudeSessionRetentionDays: claudeSessionRetentionDays}
		if ollamaURL == "" {
			ollamaURL = "http://localhost:11434"
		}
		client := &http.Client{Timeout: 3 * time.Second}
		if resp, err := client.Get(ollamaURL); err == nil {
			resp.Body.Close()
			h.ollamaUp = true
		}

		h.gpu = detectGPU()
		// The optional CLI has one bounded context for both fixed, non-interactive
		// commands. A timeout or a malformed response only degrades Tokscale's
		// own status; it must never fail the NEXUS health check.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		h.tokscale = detectTokscaleHealth(ctx, tokscale)
		cancel()

		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			h.links = append(h.links, "✗ home directory: $HOME unset")
			return h
		}
		links := []struct{ label, path string }{
			{"Gemini", filepath.Join(home, ".gemini", "GEMINI.md")},
			{"Claude", filepath.Join(home, ".claude", "CLAUDE.md")},
			{"Kiro", filepath.Join(home, ".kiro", "steering", "nexus-orchestrator.md")},
			{"Core", filepath.Join(home, ".config", "nexus", "core")},
			{"Personas", filepath.Join(home, ".config", "nexus", "personas")},
			{"Tools", filepath.Join(home, ".config", "nexus", "tools")},
			{"Prompts", filepath.Join(home, ".config", "nexus", "prompts")},
			{"Agent Memory", filepath.Join(home, ".config", "nexus", "agent-memory")},
		}
		for _, l := range links {
			fi, err := os.Lstat(l.path)
			if err != nil {
				h.links = append(h.links, "✗ "+l.label+": missing")
			} else if fi.Mode()&os.ModeSymlink != 0 {
				if _, err := os.Stat(l.path); err != nil {
					h.links = append(h.links, "✗ "+l.label+": broken symlink")
				} else {
					h.links = append(h.links, "✓ "+l.label+": linked")
				}
			} else {
				h.links = append(h.links, "⚠ "+l.label+": exists (not a symlink)")
			}
		}
		return h
	}
}

func detectTokscaleHealth(ctx context.Context, adapter TokscaleAdapter) tokscaleHealth {
	version, err := adapter.Version(ctx)
	if err != nil {
		if errors.Is(err, ErrTokscaleUnavailable) {
			return tokscaleHealth{state: tokscaleHealthUnavailable}
		}
		return tokscaleHealth{state: tokscaleHealthDegraded}
	}

	report, err := adapter.Load(ctx)
	if err != nil {
		// version is trusted only after strict semantic-version parsing. It is
		// safe to retain even when the optional aggregate command is unavailable.
		return tokscaleHealth{state: tokscaleHealthDegraded, version: version}
	}
	return tokscaleHealth{
		state:   tokscaleHealthReady,
		version: version,
		clients: recognizedTokscaleClients(report.Entries),
	}
}

// recognizedTokscaleClients maps only known adapter client identifiers to
// fixed product labels. A third-party CLI's arbitrary client field can never
// be rendered in the health screen.
func recognizedTokscaleClients(entries []TokscaleUsage) []string {
	labels := map[string]string{
		"amp":         "Amp",
		"claude":      "Claude Code",
		"claude-code": "Claude Code",
		"codex":       "Codex",
		"copilot":     "Copilot",
		"cursor":      "Cursor",
		"gemini":      "Gemini CLI",
		"gemini-cli":  "Gemini CLI",
		"antigravity": "Antigravity CLI",
		"openclaw":    "OpenClaw",
	}
	seen := make(map[string]bool)
	clients := make([]string, 0, len(entries))
	for _, entry := range entries {
		label, ok := labels[strings.ToLower(entry.Client)]
		if !ok || seen[label] {
			continue
		}
		seen[label] = true
		clients = append(clients, label)
	}
	if len(clients) == 0 {
		return []string{"none recorded"}
	}
	sort.Strings(clients)
	return clients
}
