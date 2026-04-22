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
	"charm.land/lipgloss/v2"
)

var version = "dev"

// screens
type screen int

const (
	screenMenu screen = iota
	screenInstall
	screenConfigure
	screenHealth
	screenUninstall
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

// --- other messages ---

type cmdDoneMsg struct {
	output string
	err    error
}

type healthMsg struct {
	ollamaUp bool
	links    []string
}

// --- styles ---

type styles struct {
	title    lipgloss.Style
	menu     lipgloss.Style
	selected lipgloss.Style
	subtle   lipgloss.Style
	success  lipgloss.Style
	warn     lipgloss.Style
	errStyle lipgloss.Style
	border   lipgloss.Style
}

func newStyles() styles {
	return styles{
		title:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).MarginBottom(1),
		menu:     lipgloss.NewStyle().PaddingLeft(2),
		selected: lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true),
		subtle:   lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		success:  lipgloss.NewStyle().Foreground(lipgloss.Color("82")),
		warn:     lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		errStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
		border:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2).BorderForeground(lipgloss.Color("63")),
	}
}

// --- model ---

type model struct {
	screen   screen
	cursor   int
	styles   styles
	nexusDir string
	spinner  spinner.Model

	// install wizard
	steps        []installStep
	currentStep  int
	installDone  bool
	localAI      bool
	localAIAsked bool // true after user answered the prompt during install

	// uninstall / generic operation
	running bool
	output  string
	err     error

	// health
	health healthMsg

	// configure
	configCursor  int
	configEditing bool
	configKeys    []string
	configVals    []string
	editBuf       string
}

var menuItems = []string{
	"Install NEXUS",
	"Configure",
	"Health Check",
	"Uninstall NEXUS",
}

func findNexusDir() string {
	if env := os.Getenv("NEXUS_REPO"); env != "" {
		return env
	}
	// Check if we're running from inside the repo (dev mode)
	exe, _ := os.Executable()
	candidate := filepath.Dir(filepath.Dir(filepath.Dir(exe))) // tools/tui/nexus -> repo root
	if _, err := os.Stat(filepath.Join(candidate, "core", "NEXUS.md")); err == nil {
		return candidate
	}
	// Default: curl installer clones here
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "nexus", "repo")
}

func initialModel() model {
	s := spinner.New(spinner.WithSpinner(spinner.Dot))
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	m := model{
		styles:   newStyles(),
		nexusDir: findNexusDir(),
		spinner:  s,
		localAI:  true, // default on
		configKeys: []string{
			"NEXUS_LOCAL_AI",
			"OLLAMA_HOST_URL",
			"NEXUS_SUPERVISOR_MODEL",
			"NEXUS_LOGIC_MODEL",
		},
		configVals: []string{
			"true",
			"http://localhost:11434",
			"qwen2.5-coder:1.5b",
			"llama3.2:3b",
		},
	}
	loadEnv(&m)
	m.localAI = m.configVals[0] != "false"
	return m
}

func (m model) Init() tea.Cmd { return nil }

// --- root update / view ---

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if msg.String() == "esc" && m.screen != screenMenu {
			m.screen = screenMenu
			m.running = false
			m.output = ""
			m.err = nil
			m.installDone = false
			return m, nil
		}
	}

	switch m.screen {
	case screenMenu:
		return updateMenu(msg, m)
	case screenInstall:
		return updateInstall(msg, m)
	case screenConfigure:
		return updateConfigure(msg, m)
	case screenHealth:
		return updateHealth(msg, m)
	case screenUninstall:
		return updateUninstall(msg, m)
	}
	return m, nil
}

func (m model) View() tea.View {
	var s string
	switch m.screen {
	case screenMenu:
		s = menuView(m)
	case screenInstall:
		s = installView(m)
	case screenConfigure:
		s = configureView(m)
	case screenHealth:
		s = healthView(m)
	case screenUninstall:
		s = uninstallView(m)
	}
	return tea.NewView(s)
}

// --- menu ---

func updateMenu(msg tea.Msg, m model) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(menuItems)-1 {
				m.cursor++
			}
		case "enter":
			switch m.cursor {
			case 0:
				m.screen = screenInstall
				m.installDone = false
				m.currentStep = 0
				m.steps = buildInstallSteps()
				m.steps[0].status = stepRunning
				return m, tea.Batch(m.spinner.Tick, runInstallStep(m, 0))
			case 1:
				m.screen = screenConfigure
				loadEnv(&m)
			case 2:
				m.screen = screenHealth
				m.running = true
				return m, tea.Batch(m.spinner.Tick, checkHealth(m.nexusDir))
			case 3:
				m.screen = screenUninstall
				m.running = true
				m.output = ""
				m.err = nil
				return m, tea.Batch(m.spinner.Tick, runScript(m.nexusDir, "teardown-nexus.sh"))
			}
		}
	}
	return m, nil
}

func menuView(m model) string {
	s := m.styles.title.Render("⚡ NEXUS Framework Manager") + "\n"
	s += m.styles.subtle.Render("   v"+version) + "\n\n"
	for i, item := range menuItems {
		cursor := "  "
		style := m.styles.menu
		if m.cursor == i {
			cursor = "▸ "
			style = m.styles.selected
		}
		s += style.Render(cursor+item) + "\n"
	}
	s += "\n" + m.styles.subtle.Render("j/k: navigate • enter: select • q: quit")
	return m.styles.border.Render(s)
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
	nexus := m.nexusDir
	return func() tea.Msg {
		switch idx {
		case 0: // validate repo
			for _, req := range []string{"core/NEXUS.md", "core/CLAUDE.md", "personas", "tools"} {
				if _, err := os.Stat(filepath.Join(nexus, req)); err != nil {
					return stepDoneMsg{idx: idx, ok: false, detail: "missing " + req}
				}
			}
			return stepDoneMsg{idx: idx, ok: true, detail: nexus}

		case 1: // symlink core files
			home, _ := os.UserHomeDir()
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

		case 2: // symlink config dirs
			home, _ := os.UserHomeDir()
			configDir := filepath.Join(home, ".config", "nexus")
			dirs := []string{"personas", "tools", "prompts", "mcp-configs", "agent-memory"}
			for _, d := range dirs {
				if err := safeLink(filepath.Join(nexus, d), filepath.Join(configDir, d)); err != nil {
					return stepDoneMsg{idx: idx, ok: false, detail: err.Error()}
				}
			}
			return stepDoneMsg{idx: idx, ok: true, detail: fmt.Sprintf("%d directories linked", len(dirs))}

		case 3: // configure MCP
			home, _ := os.UserHomeDir()
			mcpFile := filepath.Join(home, ".kiro", "settings", "mcp.json")
			configDir := filepath.Join(home, ".config", "nexus")
			serverPath := filepath.Join(configDir, "tools", "mcp", "server.mjs")
			if err := configureMCP(mcpFile, serverPath); err != nil {
				return stepDoneMsg{idx: idx, ok: false, detail: err.Error()}
			}
			return stepDoneMsg{idx: idx, ok: true, detail: "nexus-ollama configured"}

		case 4: // check dependencies
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

		case 5: // pull ollama models
			if _, err := exec.LookPath("ollama"); err != nil {
				return stepDoneMsg{idx: idx, ok: true, detail: "skipped (ollama not installed)"}
			}
			// Check if ollama is reachable
			client := &http.Client{Timeout: 3 * time.Second}
			ollamaURL := "http://localhost:11434"
			if env := os.Getenv("OLLAMA_HOST_URL"); env != "" {
				ollamaURL = env
			}
			if _, err := client.Get(ollamaURL); err != nil {
				return stepDoneMsg{idx: idx, ok: true, detail: "skipped (ollama not running)"}
			}
			models := []string{"qwen2.5-coder:1.5b", "llama3.2:3b"}
			var pulled []string
			for _, m := range models {
				cmd := exec.Command("ollama", "pull", m)
				if err := cmd.Run(); err == nil {
					pulled = append(pulled, m)
				}
			}
			if len(pulled) == 0 {
				return stepDoneMsg{idx: idx, ok: true, detail: "no models pulled (check ollama)"}
			}
			return stepDoneMsg{idx: idx, ok: true, detail: strings.Join(pulled, ", ")}
		}
		return stepDoneMsg{idx: idx, ok: true}
	}
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

		// If failed on a critical step (0-3), stop
		if !msg.ok && msg.idx <= 3 {
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
				saveEnv(m)
				return m, advanceInstall(&m, 2)
			case "n":
				m.localAI = false
				m.localAIAsked = true
				m.configVals[0] = "false"
				saveEnv(m)
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
		s += "\n" + m.styles.selected.Render("Enable local AI? (MCP server, Ollama models)") + "\n"
		s += m.styles.subtle.Render("y: yes • n: no") + "\n"
	}

	s += "\n" + m.styles.subtle.Render("esc: back")
	return m.styles.border.Render(s)
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
		s += "\nSymlinks:\n"
		for _, l := range m.health.links {
			s += "  " + l + "\n"
		}
	}
	s += "\n" + m.styles.subtle.Render("esc: back")
	return m.styles.border.Render(s)
}

// --- uninstall ---

func updateUninstall(msg tea.Msg, m model) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case cmdDoneMsg:
		m.running = false
		m.output = msg.output
		m.err = msg.err
	case spinner.TickMsg:
		if m.running {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func uninstallView(m model) string {
	s := m.styles.title.Render("⚡ Uninstall") + "\n\n"
	if m.running {
		s += m.spinner.View() + " Running teardown...\n"
	} else if m.err != nil {
		s += m.styles.errStyle.Render("✗ Error: "+m.err.Error()) + "\n\n"
		if m.output != "" {
			s += m.styles.subtle.Render(truncate(m.output, 800)) + "\n"
		}
	} else {
		s += m.styles.success.Render("✓ Uninstall complete") + "\n\n"
		if m.output != "" {
			s += m.styles.subtle.Render(truncate(m.output, 800)) + "\n"
		}
	}
	s += "\n" + m.styles.subtle.Render("esc: back")
	return m.styles.border.Render(s)
}

// --- configure ---

func updateConfigure(msg tea.Msg, m model) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		if m.configEditing {
			switch msg.String() {
			case "enter":
				m.configVals[m.configCursor] = m.editBuf
				m.configEditing = false
			case "backspace":
				if len(m.editBuf) > 0 {
					m.editBuf = m.editBuf[:len(m.editBuf)-1]
				}
			case "esc":
				m.configEditing = false
			default:
				if len(msg.String()) == 1 {
					m.editBuf += msg.String()
				}
			}
			return m, nil
		}

		switch msg.String() {
		case "up", "k":
			if m.configCursor > 0 {
				m.configCursor--
			}
		case "down", "j":
			if m.configCursor < len(m.configKeys)-1 {
				m.configCursor++
			}
		case "enter":
			if m.configCursor == 0 {
				// Toggle local AI
				if m.configVals[0] == "true" {
					m.configVals[0] = "false"
					m.localAI = false
				} else {
					m.configVals[0] = "true"
					m.localAI = true
				}
			} else {
				m.configEditing = true
				m.editBuf = m.configVals[m.configCursor]
			}
		case "s":
			saveEnv(m)
			m.output = "Saved .env"
		}
	}
	return m, nil
}

func configureView(m model) string {
	s := m.styles.title.Render("⚡ Configure") + "\n\n"
	for i, key := range m.configKeys {
		cursor := "  "
		if m.configCursor == i {
			cursor = "▸ "
		}
		var line string
		if i == 0 {
			// Toggle display for NEXUS_LOCAL_AI
			toggle := "OFF"
			if m.configVals[0] == "true" {
				toggle = "ON"
			}
			line = fmt.Sprintf("%s%-28s [%s]", cursor, "Local AI", toggle)
		} else {
			val := m.configVals[i]
			if m.configEditing && m.configCursor == i {
				val = m.editBuf + "▏"
			}
			line = fmt.Sprintf("%s%-28s %s", cursor, key, val)
		}
		if m.configCursor == i {
			s += m.styles.selected.Render(line) + "\n"
		} else {
			s += m.styles.menu.Render(line) + "\n"
		}
	}
	if m.output != "" {
		s += "\n" + m.styles.success.Render("✓ "+m.output) + "\n"
	}
	hint := "j/k: navigate • enter: edit • s: save .env • esc: back"
	if m.configEditing {
		hint = "type value • enter: confirm • esc: cancel"
	}
	s += "\n" + m.styles.subtle.Render(hint)
	return m.styles.border.Render(s)
}

// --- commands ---

func runScript(nexusDir, script string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("bash", filepath.Join(nexusDir, script))
		out, err := cmd.CombinedOutput()
		return cmdDoneMsg{output: string(out), err: err}
	}
}

func checkHealth(nexusDir string) tea.Cmd {
	return func() tea.Msg {
		h := healthMsg{}
		ollamaURL := "http://localhost:11434"
		if env := os.Getenv("OLLAMA_HOST_URL"); env != "" {
			ollamaURL = env
		}
		client := &http.Client{Timeout: 3 * time.Second}
		if _, err := client.Get(ollamaURL); err == nil {
			h.ollamaUp = true
		}

		home, _ := os.UserHomeDir()
		links := []struct{ label, path string }{
			{"Gemini", filepath.Join(home, ".gemini", "GEMINI.md")},
			{"Claude", filepath.Join(home, ".claude", "CLAUDE.md")},
			{"Kiro", filepath.Join(home, ".kiro", "steering", "nexus-orchestrator.md")},
			{"Personas", filepath.Join(home, ".config", "nexus", "personas")},
			{"Tools", filepath.Join(home, ".config", "nexus", "tools")},
			{"Prompts", filepath.Join(home, ".config", "nexus", "prompts")},
			{"MCP Configs", filepath.Join(home, ".config", "nexus", "mcp-configs")},
			{"Agent Memory", filepath.Join(home, ".config", "nexus", "agent-memory")},
		}
		for _, l := range links {
			fi, err := os.Lstat(l.path)
			if err != nil {
				h.links = append(h.links, "✗ "+l.label+": missing")
			} else if fi.Mode()&os.ModeSymlink != 0 {
				h.links = append(h.links, "✓ "+l.label+": linked")
			} else {
				h.links = append(h.links, "⚠ "+l.label+": exists (not a symlink)")
			}
		}
		return h
	}
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

func configureMCP(mcpFile, serverPath string) error {
	dir := filepath.Dir(mcpFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Read existing or start fresh
	content, err := os.ReadFile(mcpFile)
	if err != nil {
		content = []byte(`{"mcpServers":{}}`)
	}

	// Check if already configured
	if strings.Contains(string(content), "nexus-ollama") {
		return nil
	}

	// Simple JSON injection — find the mcpServers object and add our entry
	entry := fmt.Sprintf(`"nexus-ollama":{"command":"node","args":[%q]}`, serverPath)
	s := string(content)
	if idx := strings.Index(s, `"mcpServers"`); idx >= 0 {
		// Find the opening brace after "mcpServers"
		braceIdx := strings.Index(s[idx:], "{")
		if braceIdx >= 0 {
			insertAt := idx + braceIdx + 1
			if strings.TrimSpace(s[insertAt:insertAt+1]) == "}" {
				// Empty object
				s = s[:insertAt] + entry + s[insertAt:]
			} else {
				// Has existing entries
				s = s[:insertAt] + entry + "," + s[insertAt:]
			}
		}
	}

	return os.WriteFile(mcpFile, []byte(s), 0644)
}

// --- .env helpers ---

func loadEnv(m *model) {
	data, err := os.ReadFile(filepath.Join(m.nexusDir, ".env"))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), "\"")
		for i, k := range m.configKeys {
			if k == key {
				m.configVals[i] = val
			}
		}
	}
	m.localAI = m.configVals[0] != "false"
}

func saveEnv(m model) {
	var lines []string
	for i, key := range m.configKeys {
		lines = append(lines, fmt.Sprintf("%s=%q", key, m.configVals[i]))
	}
	os.WriteFile(filepath.Join(m.nexusDir, ".env"), []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max] + "\n... (truncated)"
	}
	return s
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("nexus " + version)
		return
	}
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
