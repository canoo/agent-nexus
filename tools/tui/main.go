package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var version = "dev"

const (
	claudeSessionRetentionKey     = "NEXUS_CLAUDE_SESSION_RETENTION_DAYS"
	defaultClaudeSessionRetention = 30
)

// screens
type screen int

const (
	screenMenu screen = iota
	screenInstall
	screenConfigure
	screenHealth
	screenUninstall
	screenUpdate
	screenTaskLog
	screenUsageDashboard
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

type saveFeedbackMsg struct{}

type versionCheckMsg struct {
	latest    string
	updateURL string
	err       error
}

type updateDoneMsg struct {
	err error
}

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

// --- task log ---

type taskLogEntry struct {
	Tool                string  `json:"tool"`
	Model               string  `json:"model"`
	Routing             string  `json:"routing,omitempty"`
	TokensIn            int     `json:"tokens_in,omitempty"`
	TokensOut           int     `json:"tokens_out,omitempty"`
	CloudCostEquivalent float64 `json:"cloud_cost_equivalent,omitempty"`
	Ms                  int     `json:"ms"`
	Ok                  bool    `json:"ok"`
	Error               string  `json:"error,omitempty"`
	Ts                  int64   `json:"ts"`
}

type taskLogMsg struct {
	entries []taskLogEntry
}

type taskLogStats struct {
	total      int
	successes  int
	failures   int
	avgMs      int
	p95Ms      int
	modelTasks map[string]int
	routes     map[string]int
	savingsUSD float64
}

// usageDashboardMsg contains a privacy-safe classification of a Tokscale load.
// The adapter error itself is deliberately not retained: third-party CLI errors
// can contain data that does not belong in the TUI.
type usageDashboardMsg struct {
	report TokscaleReport
	state  tokscaleLoadState
}

type tokscaleLoadState int

const (
	tokscaleLoading tokscaleLoadState = iota
	tokscaleReady
	tokscaleUnavailable
	tokscaleDegraded
)

// tokscaleUsageStats is intentionally separate from taskLogStats. NEXUS task
// routing data and Tokscale's provider usage aggregates can overlap, so they
// must never be summed or used to infer each other.
type tokscaleUsageStats struct {
	aggregates       int
	inputTokens      int64
	outputTokens     int64
	cacheReadTokens  int64
	cacheWriteTokens int64
	reasoningTokens  int64
	messages         int64
	costUSD          float64
}

// --- GPU detection ---

type gpuInfo struct {
	Name     string
	MemoryMB int
	Platform string // "nvidia", "amd", "apple", "unknown"
}

func detectGPU() gpuInfo {
	// Linux: NVIDIA via nvidia-smi
	if out, err := exec.Command("nvidia-smi", "--query-gpu=name,memory.total",
		"--format=csv,noheader,nounits").Output(); err == nil {
		// Multi-GPU: take the first line only
		line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
		// Split by last ", " to handle GPU names containing commas
		if idx := strings.LastIndex(line, ", "); idx > 0 {
			mem := 0
			fmt.Sscanf(strings.TrimSpace(line[idx+2:]), "%d", &mem)
			return gpuInfo{Name: strings.TrimSpace(line[:idx]), MemoryMB: mem, Platform: "nvidia"}
		}
	}

	// macOS: Apple Silicon via system_profiler
	if out, err := exec.Command("system_profiler", "SPHardwareDataType").Output(); err == nil {
		text := string(out)
		// Intel Macs have "Processor Name:" instead of "Chip:" — skip them
		if strings.Contains(text, "Processor Name:") {
			return gpuInfo{Platform: "unknown"}
		}
		info := gpuInfo{Platform: "apple"}
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Chip:") {
				info.Name = strings.TrimSpace(strings.TrimPrefix(line, "Chip:"))
			}
			if strings.HasPrefix(line, "Memory:") {
				val := strings.TrimSpace(strings.TrimPrefix(line, "Memory:"))
				mem := 0
				if strings.Contains(val, "GB") {
					fmt.Sscanf(val, "%d", &mem)
					mem *= 1024
				}
				info.MemoryMB = mem
			}
		}
		if info.Name != "" {
			return info
		}
	}

	// Linux: AMD via sysfs
	matches, _ := filepath.Glob("/sys/class/drm/card*/device/mem_info_vram_total")
	if len(matches) > 0 {
		if data, err := os.ReadFile(matches[0]); err == nil {
			bytes := 0
			fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &bytes)
			return gpuInfo{Name: "AMD GPU", MemoryMB: bytes / 1024 / 1024, Platform: "amd"}
		}
	}

	return gpuInfo{Platform: "unknown"}
}

func (g gpuInfo) String() string {
	if g.Platform == "unknown" {
		return "No GPU detected"
	}
	mem := g.MemoryMB / 1024
	unit := "GB"
	if g.Platform == "apple" {
		return fmt.Sprintf("%s — %d %s unified memory", g.Name, mem, unit)
	}
	return fmt.Sprintf("%s — %d %s VRAM", g.Name, mem, unit)
}

func (g gpuInfo) RecommendedModels() (supervisor, logic string) {
	mem := g.MemoryMB
	switch {
	case g.Platform == "apple" && mem <= 8*1024:
		return "qwen2.5-coder:3b", "llama3.2:3b"
	case mem <= 4*1024:
		return "qwen2.5-coder:1.5b", "llama3.2:3b"
	case mem <= 8*1024:
		return "qwen2.5-coder:7b", "llama3.1:8b"
	case mem <= 16*1024:
		return "qwen2.5-coder:7b", "qwen2.5:14b"
	case mem <= 18*1024:
		return "qwen2.5-coder:7b", "llama3.1:8b"
	case mem <= 24*1024:
		return "qwen2.5-coder:14b", "qwen2.5:32b"
	default:
		return "qwen2.5-coder:14b", "qwen2.5:32b"
	}
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
	teaGreen := lipgloss.Color("#c7efcf")
	gold := lipgloss.Color("#f7b538")
	ochre := lipgloss.Color("#db7c26")

	return styles{
		title:    lipgloss.NewStyle().Bold(true).Foreground(gold).MarginBottom(1),
		menu:     lipgloss.NewStyle().PaddingLeft(2),
		selected: lipgloss.NewStyle().Foreground(gold).Bold(true),
		subtle:   lipgloss.NewStyle().Foreground(teaGreen),
		success:  lipgloss.NewStyle().Foreground(teaGreen),
		warn:     lipgloss.NewStyle().Foreground(ochre),
		errStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
		border:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2).BorderForeground(ochre),
	}
}

// borderBox returns the border style capped to the terminal width (min 40, max 100).
func (m model) borderBox(content string) string {
	w := m.width - 4 // account for border + padding
	if w < 40 {
		w = 40
	}
	if w > 100 || m.width == 0 {
		w = 100
	}
	return m.styles.border.MaxWidth(w).Render(content)
}

// --- model ---

type model struct {
	screen   screen
	cursor   int
	styles   styles
	nexusDir string
	spinner  spinner.Model
	width    int

	// install wizard
	steps        []installStep
	currentStep  int
	installDone  bool
	localAI      bool
	localAIAsked bool // true after user answered the prompt during install

	// uninstall / generic operation
	running            bool
	output             string
	err                error
	uninstallConfirmed bool

	// health
	health          healthMsg
	tokscaleAdapter TokscaleAdapter

	// task log
	taskLog []taskLogEntry

	// usage dashboard: each source loads independently so a missing or slow
	// optional provider never blocks NEXUS-native task data.
	usageTaskLog         []taskLogEntry
	usageNativeLoading   bool
	usageTokscaleLoading bool
	usageTokscaleState   tokscaleLoadState
	usageTokscaleReport  TokscaleReport

	// configure
	configCursor  int
	configEditing bool
	configKeys    []string
	configLabels  []string
	configVals    []string
	editBuf       string

	// update
	latestVersion string
	updateURL     string
	updateNotice  string
}

var menuItems = []string{
	"Install NEXUS",
	"Configure",
	"Health Check",
	"Task Log",
	"Usage & Cost Dashboard",
	"Update NEXUS",
	"Uninstall NEXUS",
}

func findNexusDir() string {
	if env := os.Getenv("NEXUS_REPO"); env != "" {
		return env
	}
	// Walk up from the executable looking for the repo root (dev/source build).
	// Stops after 5 levels to avoid traversing the whole filesystem.
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for i := 0; i < 5; i++ {
			if _, err := os.Stat(filepath.Join(dir, "core", "NEXUS.md")); err == nil {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	// Default: curl installer clones here. If $HOME is unset (CI container,
	// restricted service account) fall back to a CWD-relative path so we
	// never resolve to a root-filesystem location like "/.config/nexus/repo".
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".config", "nexus", "repo")
	}
	return filepath.Join(home, ".config", "nexus", "repo")
}

func initialModel() model {
	s := spinner.New(spinner.WithSpinner(spinner.Dot))
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#f7b538"))

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
			claudeSessionRetentionKey,
		},
		configLabels: []string{
			"Local AI",
			"Ollama Host URL",
			"Supervisor Model",
			"Logic Model",
			"Claude History Guidance (days)",
		},
		configVals: []string{
			"true",
			"http://localhost:11434",
			"qwen2.5-coder:1.5b",
			"llama3.2:3b",
			strconv.Itoa(defaultClaudeSessionRetention),
		},
	}
	loadEnv(&m)
	m.localAI = m.configVals[0] != "false"
	return m
}

func (m model) Init() tea.Cmd { return checkLatestVersion() }

// --- root update / view ---

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = msg.Width
		return m, nil
	}
	if msg, ok := msg.(versionCheckMsg); ok {
		if msg.err == nil && msg.latest != "" && msg.latest != version && version != "dev" {
			m.latestVersion = msg.latest
			m.updateURL = msg.updateURL
			m.updateNotice = "Update available: v" + msg.latest
		}
		return m, nil
	}
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
	case screenUpdate:
		return updateUpdateScreen(msg, m)
	case screenTaskLog:
		return updateTaskLog(msg, m)
	case screenUsageDashboard:
		return updateUsageDashboard(msg, m)
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
	case screenUpdate:
		s = updateScreenView(m)
	case screenTaskLog:
		s = taskLogView(m)
	case screenUsageDashboard:
		s = usageDashboardView(m)
	}
	v := tea.NewView(s)
	v.AltScreen = true
	return v
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
				return m, tea.Batch(m.spinner.Tick, checkHealth(m.nexusDir, m.configVals[1], configuredClaudeSessionRetentionDays(m), m.tokscaleAdapter))
			case 3:
				m.screen = screenTaskLog
				m.running = true
				return m, tea.Batch(m.spinner.Tick, loadTaskLog())
			case 4:
				m.screen = screenUsageDashboard
				return startUsageDashboardLoad(m)
			case 5:
				m.screen = screenUpdate
				m.running = false
				m.output = ""
				m.err = nil
				if m.latestVersion == "" {
					m.running = true
					return m, tea.Batch(m.spinner.Tick, checkLatestVersion())
				}
				return m, nil
			case 6:
				m.screen = screenUninstall
				m.running = false
				m.output = ""
				m.err = nil
				m.uninstallConfirmed = false
				return m, nil
			}
		}
	}
	return m, nil
}

func menuView(m model) string {
	logo := " _____  _____  __  __  __ __  _____\n" +
		"/  _  \\/   __\\/  \\/  \\/  |  \\/  ___>\n" +
		"|  |  ||   __|>-    -<|  |  ||___  |\n" +
		"\\__|__/\\_____/\\__/\\__/\\_____/<_____/"
	s := m.styles.title.Render(logo) + "\n"
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
	if m.updateNotice != "" {
		s += "\n" + m.styles.warn.Render("⬆ "+m.updateNotice)
	}
	return m.borderBox(s)
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
			home, err := os.UserHomeDir()
			if err != nil || home == "" {
				return stepDoneMsg{idx: idx, ok: false, detail: "cannot resolve home directory: $HOME unset"}
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

		case 2: // symlink config dirs
			home, err := os.UserHomeDir()
			if err != nil || home == "" {
				return stepDoneMsg{idx: idx, ok: false, detail: "cannot resolve home directory: $HOME unset"}
			}
			configDir := filepath.Join(home, ".config", "nexus")
			dirs := []string{"core", "personas", "tools", "prompts", "mcp-configs", "agent-memory"}
			for _, d := range dirs {
				if err := safeLink(filepath.Join(nexus, d), filepath.Join(configDir, d)); err != nil {
					return stepDoneMsg{idx: idx, ok: false, detail: err.Error()}
				}
			}
			return stepDoneMsg{idx: idx, ok: true, detail: fmt.Sprintf("%d directories linked", len(dirs))}

		case 3: // configure MCP
			home, err := os.UserHomeDir()
			if err != nil || home == "" {
				return stepDoneMsg{idx: idx, ok: false, detail: "cannot resolve home directory: $HOME unset"}
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
			// Check if ollama is reachable using the configured URL
			client := &http.Client{Timeout: 3 * time.Second}
			ollamaURL := m.configVals[1]
			if ollamaURL == "" {
				ollamaURL = "http://localhost:11434"
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

// --- uninstall ---

func loadTaskLog() tea.Cmd {
	return func() tea.Msg {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			// No $HOME means no log file to load. Return empty rather
			// than reading from "/.config/nexus/logs/mcp-tasks.jsonl".
			return taskLogMsg{}
		}
		logFile := filepath.Join(home, ".config", "nexus", "logs", "mcp-tasks.jsonl")
		data, err := os.ReadFile(logFile)
		if err != nil {
			return taskLogMsg{}
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")

		// Log rotation: cap file at 5000 lines
		const maxLines = 5000
		if len(lines) > maxLines {
			lines = lines[len(lines)-maxLines:]
			tmp := logFile + ".tmp"
			if os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0644) == nil {
				os.Rename(tmp, logFile)
			}
		}

		var entries []taskLogEntry
		for _, line := range lines {
			if line == "" {
				continue
			}
			var e taskLogEntry
			if json.Unmarshal([]byte(line), &e) == nil {
				entries = append(entries, e)
			}
		}
		// Show most recent first, cap display at 50
		for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
			entries[i], entries[j] = entries[j], entries[i]
		}
		if len(entries) > 50 {
			entries = entries[:50]
		}
		return taskLogMsg{entries: entries}
	}
}

func updateTaskLog(msg tea.Msg, m model) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case taskLogMsg:
		m.running = false
		m.taskLog = msg.entries
	case spinner.TickMsg:
		if m.running {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case tea.KeyPressMsg:
		if msg.String() == "r" {
			m.running = true
			return m, tea.Batch(m.spinner.Tick, loadTaskLog())
		}
	}
	return m, nil
}

func taskLogView(m model) string {
	s := m.styles.title.Render("⚡ Task Log") + "\n\n"
	if m.running {
		s += m.spinner.View() + " Loading...\n"
	} else if len(m.taskLog) == 0 {
		s += m.styles.subtle.Render("No MCP tasks recorded yet.") + "\n"
		s += m.styles.subtle.Render("Tasks appear here when AI tools use the nexus-ollama MCP server.") + "\n"
	} else {
		stats := summarizeTaskLog(m.taskLog)
		successRate := 0
		if stats.total > 0 {
			successRate = stats.successes * 100 / stats.total
		}
		s += fmt.Sprintf("  Tasks: %d  Success: %d%%  Failures: %d\n",
			stats.total, successRate, stats.failures)
		s += fmt.Sprintf("  Latency: avg %dms  p95 %dms\n", stats.avgMs, stats.p95Ms)
		if stats.savingsUSD > 0 {
			s += fmt.Sprintf("  Est. local savings: $%.4f\n", stats.savingsUSD)
		}
		s += fmt.Sprintf("  Routes: %s\n", summarizeIntCounts(stats.routes, 3))
		s += fmt.Sprintf("  Models: %s\n\n", summarizeModelUsage(stats.modelTasks, 3))

		// Header
		s += fmt.Sprintf("  %-24s %-22s %8s  %s\n",
			"Tool", "Model", "Time", "Status")
		s += m.styles.subtle.Render("  "+strings.Repeat("─", 66)) + "\n"
		for _, e := range m.taskLog {
			status := m.styles.success.Render("✓")
			if !e.Ok {
				status = m.styles.errStyle.Render("✗")
			}
			dur := fmt.Sprintf("%dms", e.Ms)
			if e.Ms == 0 && e.Ok {
				dur = "<1ms"
			}
			ts := time.UnixMilli(e.Ts).Format("15:04:05")
			s += fmt.Sprintf("  %-24s %-22s %8s  %s  %s\n",
				truncateCol(e.Tool, 24), truncateCol(e.Model, 22), dur, status, m.styles.subtle.Render(ts))
		}
	}
	s += "\n" + m.styles.subtle.Render("r: refresh • esc: back")
	return m.borderBox(s)
}

// --- usage and cost dashboard ---

// startUsageDashboardLoad loads the two independent sources concurrently. The
// NEXUS task log remains usable even if the optional Tokscale command is absent
// or fails.
func startUsageDashboardLoad(m model) (tea.Model, tea.Cmd) {
	m.usageNativeLoading = true
	m.usageTokscaleLoading = true
	m.usageTokscaleState = tokscaleLoading
	return m, tea.Batch(m.spinner.Tick, loadTaskLog(), loadTokscaleUsage(m.tokscaleAdapter))
}

func loadTokscaleUsage(adapter TokscaleAdapter) tea.Cmd {
	return func() tea.Msg {
		// A local report should be fast. The timeout prevents an optional CLI
		// dependency from leaving the dashboard in a loading state indefinitely.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		report, err := adapter.Load(ctx)
		switch {
		case err == nil:
			return usageDashboardMsg{report: report, state: tokscaleReady}
		case errors.Is(err, ErrTokscaleUnavailable):
			return usageDashboardMsg{state: tokscaleUnavailable}
		default:
			// Do not attach err. Tokscale output or diagnostics must not enter
			// the model, View, or any later TUI diagnostics.
			return usageDashboardMsg{state: tokscaleDegraded}
		}
	}
}

func updateUsageDashboard(msg tea.Msg, m model) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case taskLogMsg:
		m.usageNativeLoading = false
		m.usageTaskLog = msg.entries
	case usageDashboardMsg:
		m.usageTokscaleLoading = false
		m.usageTokscaleState = msg.state
		m.usageTokscaleReport = msg.report
	case spinner.TickMsg:
		if m.usageNativeLoading || m.usageTokscaleLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case tea.KeyPressMsg:
		if msg.String() == "r" {
			return startUsageDashboardLoad(m)
		}
	}
	return m, nil
}

func usageDashboardView(m model) string {
	s := m.styles.title.Render("⚡ Usage & Cost Dashboard") + "\n"
	s += m.styles.subtle.Render("NEXUS task routing and Tokscale CLI usage are separate; they are never combined.") + "\n\n"

	s += m.styles.selected.Render("NEXUS-native task routing") + "\n"
	if m.usageNativeLoading {
		s += "  " + m.spinner.View() + " Loading local task history...\n"
	} else if len(m.usageTaskLog) == 0 {
		s += m.styles.subtle.Render("  No MCP tasks recorded yet.") + "\n"
	} else {
		stats := summarizeTaskLog(m.usageTaskLog)
		successRate := 0
		if stats.total > 0 {
			successRate = stats.successes * 100 / stats.total
		}
		s += fmt.Sprintf("  Tasks: %d  Success: %d%%  Failures: %d\n", stats.total, successRate, stats.failures)
		s += fmt.Sprintf("  Latency: avg %dms  p95 %dms\n", stats.avgMs, stats.p95Ms)
		if stats.savingsUSD > 0 {
			s += fmt.Sprintf("  Est. local savings: $%.4f\n", stats.savingsUSD)
		}
		s += fmt.Sprintf("  Routes: %s\n", summarizeIntCounts(stats.routes, 3))
	}

	s += "\n" + m.styles.selected.Render("Tokscale CLI provider aggregates") + "\n"
	switch {
	case m.usageTokscaleLoading:
		s += "  " + m.spinner.View() + " Loading local Tokscale aggregates...\n"
	case m.usageTokscaleState == tokscaleUnavailable:
		s += m.styles.subtle.Render("  Tokscale is not installed. Run `bunx tokscale@latest` (or add it to PATH), then press r.") + "\n"
	case m.usageTokscaleState == tokscaleDegraded:
		s += m.styles.warn.Render("  Tokscale data is unavailable. Check its local configuration, then press r.") + "\n"
	default:
		stats := summarizeTokscaleUsage(m.usageTokscaleReport.Entries)
		s += fmt.Sprintf("  Aggregates: %d  Messages: %d  Cost: $%.4f\n", stats.aggregates, stats.messages, stats.costUSD)
		s += fmt.Sprintf("  Tokens: input %d  output %d  cache read %d  cache write %d  reasoning %d\n",
			stats.inputTokens, stats.outputTokens, stats.cacheReadTokens, stats.cacheWriteTokens, stats.reasoningTokens)
		if m.usageTokscaleReport.GroupBy != "" {
			s += m.styles.subtle.Render("  Report grouping: "+m.usageTokscaleReport.GroupBy) + "\n"
		}
	}

	s += "\n" + m.styles.subtle.Render("r: refresh both sources • esc: back")
	return m.borderBox(s)
}

func summarizeTokscaleUsage(entries []TokscaleUsage) tokscaleUsageStats {
	stats := tokscaleUsageStats{aggregates: len(entries)}
	for _, entry := range entries {
		stats.inputTokens = saturatingAddInt64(stats.inputTokens, entry.InputTokens)
		stats.outputTokens = saturatingAddInt64(stats.outputTokens, entry.OutputTokens)
		stats.cacheReadTokens = saturatingAddInt64(stats.cacheReadTokens, entry.CacheReadTokens)
		stats.cacheWriteTokens = saturatingAddInt64(stats.cacheWriteTokens, entry.CacheWriteTokens)
		stats.reasoningTokens = saturatingAddInt64(stats.reasoningTokens, entry.ReasoningTokens)
		stats.messages = saturatingAddInt64(stats.messages, entry.MessageCount)
		stats.costUSD = saturatingAddFloat64(stats.costUSD, entry.CostUSD)
	}
	return stats
}

func saturatingAddInt64(current, value int64) int64 {
	if value > math.MaxInt64-current {
		return math.MaxInt64
	}
	return current + value
}

func saturatingAddFloat64(current, value float64) float64 {
	if value > math.MaxFloat64-current {
		return math.MaxFloat64
	}
	return current + value
}

func summarizeTaskLog(entries []taskLogEntry) taskLogStats {
	stats := taskLogStats{
		modelTasks: map[string]int{},
		routes:     map[string]int{},
	}
	if len(entries) == 0 {
		return stats
	}

	totalMs := 0
	latencies := make([]int, 0, len(entries))
	for _, e := range entries {
		stats.total++
		totalMs += e.Ms
		latencies = append(latencies, e.Ms)
		if e.Ok {
			stats.successes++
		} else {
			stats.failures++
		}
		if e.Routing == "local" || e.Routing == "deterministic" {
			stats.savingsUSD += e.CloudCostEquivalent
		}
		model := strings.TrimSpace(e.Model)
		if model == "" {
			model = "unknown"
		}
		stats.modelTasks[model]++
		route := strings.TrimSpace(e.Routing)
		if route == "" {
			route = "unknown"
		}
		stats.routes[route]++
	}
	stats.avgMs = totalMs / stats.total
	stats.p95Ms = percentile95(latencies)
	return stats
}

func summarizeModelUsage(modelTasks map[string]int, maxModels int) string {
	return summarizeIntCounts(modelTasks, maxModels)
}

func summarizeIntCounts(countsByName map[string]int, maxItems int) string {
	if len(countsByName) == 0 {
		return "none"
	}

	type namedCount struct {
		name  string
		count int
	}
	counts := make([]namedCount, 0, len(countsByName))
	for name, count := range countsByName {
		counts = append(counts, namedCount{name: name, count: count})
	}
	for i := 0; i < len(counts); i++ {
		for j := i + 1; j < len(counts); j++ {
			if counts[j].count > counts[i].count ||
				(counts[j].count == counts[i].count && counts[j].name < counts[i].name) {
				counts[i], counts[j] = counts[j], counts[i]
			}
		}
	}

	if maxItems <= 0 || maxItems > len(counts) {
		maxItems = len(counts)
	}
	parts := make([]string, 0, maxItems+1)
	for i := 0; i < maxItems; i++ {
		parts = append(parts, fmt.Sprintf("%s (%d)", counts[i].name, counts[i].count))
	}
	if len(counts) > maxItems {
		parts = append(parts, fmt.Sprintf("+%d more", len(counts)-maxItems))
	}
	return strings.Join(parts, ", ")
}

func percentile95(values []int) int {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int(nil), values...)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	idx := (95*len(sorted) + 99) / 100
	if idx < 1 {
		idx = 1
	}
	return sorted[idx-1]
}

// --- uninstall (original) ---

func updateUninstall(msg tea.Msg, m model) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if !m.uninstallConfirmed && !m.running {
			switch msg.String() {
			case "y":
				m.uninstallConfirmed = true
				m.running = true
				return m, tea.Batch(m.spinner.Tick, runScript(m.nexusDir, "teardown-nexus.sh"))
			case "n":
				m.screen = screenMenu
				return m, nil
			}
		}
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
	if !m.uninstallConfirmed && !m.running {
		s += m.styles.warn.Render("This will remove all NEXUS symlinks and the nexus binary.") + "\n\n"
		s += m.styles.selected.Render("Are you sure? (y/n)") + "\n"
	} else if m.running {
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
	return m.borderBox(s)
}

// --- configure ---

func updateConfigure(msg tea.Msg, m model) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(saveFeedbackMsg); ok {
		_ = msg
		m.output = ""
		m.err = nil
		return m, nil
	}
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		if m.configEditing {
			switch msg.String() {
			case "enter":
				if m.configKeys[m.configCursor] == claudeSessionRetentionKey {
					normalized, valid := normalizeClaudeSessionRetention(m.editBuf)
					m.configVals[m.configCursor] = normalized
					if !valid {
						m.output = fmt.Sprintf("Invalid %s; using default %d days", claudeSessionRetentionKey, defaultClaudeSessionRetention)
						m.err = nil
					}
				} else {
					m.configVals[m.configCursor] = m.editBuf
				}
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
			if err := saveEnv(m); err != nil {
				m.output = "Error saving .env: " + err.Error()
				m.err = err
			} else {
				m.output = "Saved .env"
				m.err = nil
			}
			return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return saveFeedbackMsg{} })
		}
	}
	return m, nil
}

func configureView(m model) string {
	s := m.styles.title.Render("⚡ Configure") + "\n\n"
	for i := range m.configKeys {
		cursor := "  "
		if m.configCursor == i {
			cursor = "▸ "
		}
		label := m.configLabels[i]
		var line string
		if i == 0 {
			toggle := "OFF"
			if m.configVals[0] == "true" {
				toggle = "ON"
			}
			line = fmt.Sprintf("%s%-24s [%s]", cursor, label, toggle)
		} else {
			val := m.configVals[i]
			if m.configEditing && m.configCursor == i {
				val = m.editBuf + "▏"
			}
			line = fmt.Sprintf("%s%-24s %s", cursor, label, val)
		}
		if m.configCursor == i {
			s += m.styles.selected.Render(line) + "\n"
		} else {
			s += m.styles.menu.Render(line) + "\n"
		}
	}
	if m.output != "" {
		if m.err != nil {
			s += "\n" + m.styles.errStyle.Render("✗ "+m.output) + "\n"
		} else {
			s += "\n" + m.styles.success.Render("✓ "+m.output) + "\n"
		}
	}
	hint := "j/k: navigate • enter: edit • s: save .env • esc: back"
	if m.configEditing {
		hint = "type value • enter: confirm • esc: cancel"
	}
	s += "\n" + m.styles.subtle.Render(hint)
	return m.borderBox(s)
}

// --- update ---

const repoAPI = "https://api.github.com/repos/canoo/agent-nexus/releases/latest"

func checkLatestVersion() tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(repoAPI)
		if err != nil {
			return versionCheckMsg{err: err}
		}
		defer resp.Body.Close()
		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			return versionCheckMsg{err: err}
		}
		latest := strings.TrimPrefix(release.TagName, "v")
		return versionCheckMsg{
			latest:    latest,
			updateURL: "https://github.com/canoo/agent-nexus/releases/tag/" + release.TagName,
		}
	}
}

var validTag = regexp.MustCompile(`^[0-9A-Za-z._-]+$`)

func runSelfUpdate(tag string) tea.Cmd {
	return func() tea.Msg {
		if !validTag.MatchString(tag) {
			return updateDoneMsg{err: fmt.Errorf("invalid version tag: %q", tag)}
		}
		// tag is passed as $1 so it is never interpolated into the script text.
		cmd := exec.Command("bash", "-c", selfUpdateScript(), "bash", tag)
		out, err := cmd.CombinedOutput()
		if err != nil && len(out) > 0 {
			err = fmt.Errorf("%w: %s", err, string(out))
		}
		return updateDoneMsg{err: err}
	}
}

// selfUpdateScript downloads the release's install.sh and checksums.txt,
// verifies install.sh against its checksums.txt entry (added via
// checksum.extra_files in .goreleaser.yml), then executes it.
func selfUpdateScript() string {
	return `
set -e
TAG="$1"
BASE="https://github.com/canoo/agent-nexus/releases/download/v${TAG}"
SCRIPT=$(mktemp)
SUMS=$(mktemp)
trap 'rm -f "$SCRIPT" "$SUMS"' EXIT

curl -sSL "${BASE}/install.sh"      -o "$SCRIPT"
curl -sSL "${BASE}/checksums.txt"   -o "$SUMS"

EXPECTED=$(awk '$2 == "install.sh" {print $1}' "$SUMS")
if [ -z "$EXPECTED" ]; then
  echo "checksum entry for install.sh not found" >&2; exit 1
fi

if command -v shasum >/dev/null 2>&1; then
  echo "$EXPECTED  $SCRIPT" | shasum -a 256 --check --status
elif command -v sha256sum >/dev/null 2>&1; then
  echo "$EXPECTED  $SCRIPT" | sha256sum --check --status
else
  echo "no sha256 tool available" >&2; exit 1
fi

bash "$SCRIPT"
`
}

func updateUpdateScreen(msg tea.Msg, m model) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case versionCheckMsg:
		m.running = false
		if msg.err != nil {
			m.err = msg.err
		} else if msg.latest != "" && version != "dev" {
			m.latestVersion = msg.latest
			m.updateURL = msg.updateURL
			if msg.latest == version {
				m.updateNotice = ""
			} else {
				m.updateNotice = "Update available: v" + msg.latest
			}
		}
		return m, nil
	case updateDoneMsg:
		m.running = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.output = "Updated successfully! Restart nexus to use the new version."
			m.latestVersion = ""
			m.updateNotice = ""
		}
		return m, nil
	case spinner.TickMsg:
		if m.running {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.screen = screenMenu
			return m, nil
		case "u":
			if m.latestVersion != "" && m.latestVersion != version && !m.running {
				m.running = true
				m.output = ""
				m.err = nil
				return m, tea.Batch(m.spinner.Tick, runSelfUpdate(m.latestVersion))
			}
		}
	}
	return m, nil
}

func updateScreenView(m model) string {
	s := m.styles.title.Render("⬆ Update NEXUS") + "\n\n"
	s += m.styles.subtle.Render("Current version: v"+version) + "\n"

	if m.running {
		s += m.spinner.View() + " Checking...\n"
	} else if m.err != nil {
		s += m.styles.errStyle.Render("✗ "+m.err.Error()) + "\n"
	} else if m.output != "" {
		s += m.styles.success.Render("✓ "+m.output) + "\n"
	} else if m.latestVersion == "" || m.latestVersion == version {
		s += m.styles.success.Render("✓ You're on the latest version") + "\n"
	} else {
		s += m.styles.warn.Render("⬆ New version available: v"+m.latestVersion) + "\n"
		s += "\n" + m.styles.subtle.Render("Press u to update • esc: back")
		return m.borderBox(s)
	}

	s += "\n" + m.styles.subtle.Render("esc: back")
	return m.borderBox(s)
}

// --- commands ---

func runScript(nexusDir, script string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("bash", filepath.Join(nexusDir, script))
		out, err := cmd.CombinedOutput()
		return cmdDoneMsg{output: string(out), err: err}
	}
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
			{"MCP Configs", filepath.Join(home, ".config", "nexus", "mcp-configs")},
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
	if err := os.MkdirAll(filepath.Dir(mcpFile), 0755); err != nil {
		return err
	}

	// Decode into raw maps so top-level keys and other servers' fields (env,
	// disabled, autoApprove, ...) survive the rewrite untouched.
	cfg := map[string]json.RawMessage{}
	servers := map[string]json.RawMessage{}
	data, err := os.ReadFile(mcpFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("refusing to overwrite unparseable %s: %w", mcpFile, err)
		}
		if cfg == nil {
			// json.Unmarshal of `null` leaves the map nil; writing into it panics.
			return fmt.Errorf("refusing to overwrite unparseable %s: null configuration", mcpFile)
		}
		if raw, ok := cfg["mcpServers"]; ok {
			if err := json.Unmarshal(raw, &servers); err != nil {
				return fmt.Errorf("%s: mcpServers is not an object: %w", mcpFile, err)
			}
			if servers == nil {
				return fmt.Errorf("%s: mcpServers is not an object", mcpFile)
			}
		}
	}

	if _, exists := servers["nexus-ollama"]; exists {
		return nil
	}

	entry, err := json.Marshal(struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}{Command: "node", Args: []string{serverPath}})
	if err != nil {
		return err
	}
	servers["nexus-ollama"] = entry
	if cfg["mcpServers"], err = json.Marshal(servers); err != nil {
		return err
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(mcpFile, append(out, '\n'), 0644)
}

// configureClaudeMCP registers nexus-ollama at Claude Code's user scope via its
// CLI. ~/.claude.json holds Claude's own state, so it is never edited directly.
// Returns skipped=true when the claude CLI is not installed.
func configureClaudeMCP(serverPath string) (skipped bool, err error) {
	if _, err := exec.LookPath("claude"); err != nil {
		return true, nil
	}
	if exec.Command("claude", "mcp", "get", "nexus-ollama").Run() == nil {
		return false, nil
	}
	if out, err := exec.Command("claude", "mcp", "add", "--scope", "user", "nexus-ollama", "--", "node", serverPath).CombinedOutput(); err != nil {
		return false, fmt.Errorf("claude mcp add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return false, nil
}

// installMCPDeps runs npm ci for the MCP server unless node_modules already
// matches the lockfile. node_modules is not committed, so server.mjs cannot
// load its SDK import without this.
func installMCPDeps(mcpDir string) (string, error) {
	lock, err := os.Stat(filepath.Join(mcpDir, "package-lock.json"))
	if err != nil {
		return "", err
	}
	if installed, err := os.Stat(filepath.Join(mcpDir, "node_modules", ".package-lock.json")); err == nil && !lock.ModTime().After(installed.ModTime()) {
		return "already installed", nil
	}
	if _, err := exec.LookPath("npm"); err != nil {
		return "skipped (npm not installed)", nil
	}
	cmd := exec.Command("npm", "ci", "--omit=dev", "--no-audit", "--no-fund", "--loglevel=error")
	cmd.Dir = mcpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("npm ci: %v: %s", err, truncateCol(strings.TrimSpace(string(out)), 200))
	}
	return "installed", nil
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
				if k == claudeSessionRetentionKey {
					m.configVals[i], _ = normalizeClaudeSessionRetention(val)
				} else {
					m.configVals[i] = val
				}
			}
		}
	}
	m.localAI = m.configVals[0] != "false"
}

func saveEnv(m model) error {
	var lines []string
	for i, key := range m.configKeys {
		value := m.configVals[i]
		if key == claudeSessionRetentionKey {
			value, _ = normalizeClaudeSessionRetention(value)
		}
		lines = append(lines, fmt.Sprintf("%s=%q", key, value))
	}
	return os.WriteFile(filepath.Join(m.nexusDir, ".env"), []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

// parseClaudeSessionRetentionDays reads a NEXUS-owned setting only. It does
// not inspect or change any Claude configuration. Zero is intentionally valid
// and represents unknown or disabled guidance; malformed and negative values
// fall back to the conservative default.
func parseClaudeSessionRetentionDays(raw string) (days int, valid bool) {
	days, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || days < 0 {
		return defaultClaudeSessionRetention, false
	}
	return days, true
}

// normalizeClaudeSessionRetention is the single normalization point for the
// NEXUS_CLAUDE_SESSION_RETENTION_DAYS setting. It parses raw input with
// parseClaudeSessionRetentionDays and returns the canonical stored form: the
// valid day count as a string, or the default when the input is malformed or
// negative. All four touch points (loadEnv, saveEnv, the updateConfigure
// commit branch, and configuredClaudeSessionRetentionDays) route through here
// so the fallback rules cannot silently drift apart again.
func normalizeClaudeSessionRetention(raw string) (normalized string, valid bool) {
	days, valid := parseClaudeSessionRetentionDays(raw)
	return strconv.Itoa(days), valid
}

func configuredClaudeSessionRetentionDays(m model) int {
	for i, key := range m.configKeys {
		if key == claudeSessionRetentionKey && i < len(m.configVals) {
			normalized, _ := normalizeClaudeSessionRetention(m.configVals[i])
			days, _ := strconv.Atoi(normalized) // cannot fail: normalize emits Itoa output
			return days
		}
	}
	return defaultClaudeSessionRetention
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max] + "\n... (truncated)"
	}
	return s
}

func truncateCol(s string, max int) string {
	if len(s) > max {
		return s[:max-1] + "…"
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
