package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// --- install step types ---

type stepStatus int

const (
	stepPending stepStatus = iota
	stepRunning
	stepDone
	stepSkipped
	stepFailed
)

type installStep struct {
	label  string
	status stepStatus
	detail string
}

type stepDoneMsg struct {
	idx    int
	ok     bool
	detail string
}

// --- install wizard ---

func buildInstallSteps() []installStep {
	return []installStep{
		{label: "Validate repo"},
		{label: "Symlink core files"},
		{label: "Symlink config directories"},
		{label: "Configure MCP server"},
		{label: "Check dependencies"},
		{label: "Pull Ollama models"},
	}
}

func runInstallStep(m model, idx int) tea.Cmd {
	return func() tea.Msg {
		switch idx {
		case 0:
			return installStepValidateRepo(idx, m)
		case 1:
			return installStepSymlinkCoreFiles(idx, m)
		case 2:
			return installStepSymlinkConfigDirs(idx, m)
		case 3:
			return installStepConfigureMCP(idx, m)
		case 4:
			return installStepCheckDependencies(idx, m)
		case 5:
			return installStepPullOllamaModels(idx, m)
		}
		return stepDoneMsg{idx: idx, ok: true}
	}
}

// isCriticalInstallStep reports whether a failed install step must stop the
// wizard. Steps 0-3 (repo validation, symlinks, MCP config) are load-bearing;
// later steps only degrade functionality.
func isCriticalInstallStep(idx int) bool {
	return idx <= 3
}

// The installStep* workers below were extracted verbatim from runInstallStep
// so each of the six install steps can be unit-tested in isolation. Behavior
// is unchanged; runInstallStep is now only a dispatcher.

// installStepValidateRepo checks the repo has the paths the installer links.
func installStepValidateRepo(idx int, m model) stepDoneMsg {
	nexus := m.nexusDir
	for _, req := range []string{"core/NEXUS.md", "core/CLAUDE.md", "personas", "tools"} {
		if _, err := os.Stat(filepath.Join(nexus, req)); err != nil {
			return stepDoneMsg{idx: idx, ok: false, detail: "missing " + req}
		}
	}
	return stepDoneMsg{idx: idx, ok: true, detail: nexus}
}

// installStepSymlinkCoreFiles links the three core prompt files into the
// vendor config locations.
func installStepSymlinkCoreFiles(idx int, m model) stepDoneMsg {
	nexus := m.nexusDir
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return stepDoneMsg{idx: idx, ok: false, detail: "cannot resolve home directory: /home/cano unset"}
	}
	links := []struct{ src, dst string }{
		{"core/NEXUS.md", filepath.Join(home, ".gemini", "GEMINI.md")},
		{"core/CLAUDE.md", filepath.Join(home, ".claude", "CLAUDE.md")},
		{"core/kiro-nexus-steering.md", filepath.Join(home, ".kiro", "steering", "nexus-orchestrator.md")},
	}
	for _, l := range links {
		if err := safeLink(filepath.Join(nexus, l.src), l.dst); err != nil {
			return stepDoneMsg{idx: idx, ok: false, detail: err.Error()}
		}
	}
	return stepDoneMsg{idx: idx, ok: true, detail: "3 core files linked"}
}

// installStepSymlinkConfigDirs links the nexus content dirs into
// ~/.config/nexus.
func installStepSymlinkConfigDirs(idx int, m model) stepDoneMsg {
	nexus := m.nexusDir
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return stepDoneMsg{idx: idx, ok: false, detail: "cannot resolve home directory: /home/cano unset"}
	}
	configDir := filepath.Join(home, ".config", "nexus")
	dirs := []string{"core", "personas", "tools", "prompts", "agent-memory"}
	for _, d := range dirs {
		if err := safeLink(filepath.Join(nexus, d), filepath.Join(configDir, d)); err != nil {
			return stepDoneMsg{idx: idx, ok: false, detail: err.Error()}
		}
	}
	return stepDoneMsg{idx: idx, ok: true, detail: fmt.Sprintf("%d directories linked", len(dirs))}
}

// installStepConfigureMCP installs the MCP server deps and registers
// nexus-ollama in the Kiro and Gemini MCP configs (plus Claude Code via its
// CLI when present).
func installStepConfigureMCP(idx int, m model) stepDoneMsg {
	nexus := m.nexusDir
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return stepDoneMsg{idx: idx, ok: false, detail: "cannot resolve home directory: /home/cano unset"}
	}
	configDir := filepath.Join(home, ".config", "nexus")
	serverPath := filepath.Join(configDir, "tools", "mcp", "server.mjs")

	deps, err := installMCPDeps(filepath.Join(nexus, "tools", "mcp"))
	if err != nil {
		return stepDoneMsg{idx: idx, ok: false, detail: err.Error()}
	}
	configured := []string{}
	for _, t := range []struct{ name, file string }{
		{"Kiro", filepath.Join(home, ".kiro", "settings", "mcp.json")},
		{"Gemini", filepath.Join(home, ".gemini", "config", "mcp_config.json")},
	} {
		if err := configureMCP(t.file, serverPath); err != nil {
			return stepDoneMsg{idx: idx, ok: false, detail: t.name + ": " + err.Error()}
		}
		configured = append(configured, t.name)
	}
	skipped, err := configureClaudeMCP(serverPath)
	if err != nil {
		return stepDoneMsg{idx: idx, ok: false, detail: "Claude: " + err.Error()}
	}
	if !skipped {
		configured = append(configured, "Claude")
	}
	return stepDoneMsg{idx: idx, ok: true, detail: fmt.Sprintf("nexus-ollama → %s | deps %s", strings.Join(configured, ", "), deps)}
}

// installStepCheckDependencies reports which external tools are present.
func installStepCheckDependencies(idx int, _ model) stepDoneMsg {
	var found, missing []string
	for _, dep := range []string{"node", "ollama", "git"} {
		if _, err := exec.LookPath(dep); err == nil {
			found = append(found, dep)
		} else {
			missing = append(missing, dep)
		}
	}
	detail := "found: " + strings.Join(found, ", ")
	if len(missing) > 0 {
		detail += " | missing: " + strings.Join(missing, ", ")
	}
	return stepDoneMsg{idx: idx, ok: true, detail: detail}
}

// installStepPullOllamaModels pulls the default local models when ollama is
// installed and reachable. Every outcome is ok=true: models are best-effort.
func installStepPullOllamaModels(idx int, m model) stepDoneMsg {
	if _, err := exec.LookPath("ollama"); err != nil {
		return stepDoneMsg{idx: idx, ok: true, detail: "skipped (ollama not installed)"}
	}
	// Check if ollama is reachable using the configured URL
	client := &http.Client{Timeout: 3 * time.Second}
	ollamaURL := m.configVals[1]
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}
	resp, err := client.Get(ollamaURL)
	if err != nil {
		return stepDoneMsg{idx: idx, ok: true, detail: "skipped (ollama not running)"}
	}
	resp.Body.Close()
	models := []string{"qwen2.5-coder:1.5b", "llama3.2:3b"}
	var pulled []string
	for _, name := range models {
		cmd := exec.Command("ollama", "pull", name)
		if err := cmd.Run(); err == nil {
			pulled = append(pulled, name)
		}
	}
	if len(pulled) == 0 {
		return stepDoneMsg{idx: idx, ok: true, detail: "no models pulled (check ollama)"}
	}
	return stepDoneMsg{idx: idx, ok: true, detail: strings.Join(pulled, ", ")}
}

func updateInstall(msg tea.Msg, m model) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case stepDoneMsg:
		if msg.ok {
			m.steps[msg.idx].status = stepDone
		} else {
			m.steps[msg.idx].status = stepFailed
		}
		m.steps[msg.idx].detail = msg.detail

		// If failed on a critical step, stop
		if !msg.ok && isCriticalInstallStep(msg.idx) {
			m.installDone = true
			return m, nil
		}

		// After symlink dirs (step 2), pause to ask about local AI
		if msg.idx == 2 && !m.localAIAsked {
			return m, nil // wait for y/n input
		}

		return m, advanceInstall(&m, msg.idx)

	case tea.KeyPressMsg:
		// Handle the local AI prompt
		if !m.localAIAsked && m.currentStep == 2 && m.steps[2].status == stepDone {
			switch msg.String() {
			case "y":
				m.localAI = true
				m.localAIAsked = true
				m.configVals[0] = "true"
				_ = saveEnv(m)
				return m, advanceInstall(&m, 2)
			case "n":
				m.localAI = false
				m.localAIAsked = true
				m.configVals[0] = "false"
				_ = saveEnv(m)
				// Skip MCP, deps, models
				for i := 3; i < len(m.steps); i++ {
					m.steps[i].status = stepSkipped
					m.steps[i].detail = "local AI disabled"
				}
				m.installDone = true
				return m, nil
			}
		}

	case spinner.TickMsg:
		if !m.installDone {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func advanceInstall(m *model, fromIdx int) tea.Cmd {
	next := fromIdx + 1
	if next < len(m.steps) {
		m.currentStep = next
		m.steps[next].status = stepRunning
		return runInstallStep(*m, next)
	}
	m.installDone = true
	return nil
}

func installView(m model) string {
	s := m.styles.title.Render("⚡ Install NEXUS") + "\n\n"

	for _, step := range m.steps {
		var icon string
		switch step.status {
		case stepPending:
			icon = m.styles.subtle.Render("○")
		case stepRunning:
			icon = m.spinner.View()
		case stepDone:
			icon = m.styles.success.Render("✓")
		case stepSkipped:
			icon = m.styles.warn.Render("–")
		case stepFailed:
			icon = m.styles.errStyle.Render("✗")
		}

		line := icon + " " + step.label
		if step.detail != "" && step.status != stepPending && step.status != stepRunning {
			line += m.styles.subtle.Render("  " + step.detail)
		}
		s += line + "\n"
	}

	if m.installDone {
		s += "\n"
		allOk := true
		for _, step := range m.steps {
			if step.status == stepFailed {
				allOk = false
				break
			}
		}
		if allOk {
			s += m.styles.success.Render("Setup complete!") + "\n"
		} else {
			s += m.styles.errStyle.Render("Setup failed — check errors above.") + "\n"
		}
	} else if !m.localAIAsked && m.currentStep == 2 && len(m.steps) > 2 && m.steps[2].status == stepDone {
		s += "\n" + m.styles.subtle.Render("────────────────────────────────") + "\n"
		s += m.styles.selected.Render("Enable local AI? (MCP server + Ollama models)") + "\n"
		s += m.styles.subtle.Render("y: yes • n: no (skip remaining steps)") + "\n"
	}

	s += "\n" + m.styles.subtle.Render("esc: back")
	return m.borderBox(s)
}

// --- filesystem helpers ---

func safeLink(source, target string) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	fi, err := os.Lstat(target)
	if err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			existing, _ := os.Readlink(target)
			if existing == source {
				return nil // already correct
			}
			os.Remove(target)
		} else {
			os.Rename(target, target+".bak")
		}
	}

	return os.Symlink(source, target)
}
