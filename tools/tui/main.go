package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

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
	screenUpdate
	screenTaskLog
	screenUsageDashboard
	screenCompanionActivity
	screenMemory
)

// --- model ---

type model struct {
	screen   screen
	cursor   int
	styles   styles
	nexusDir string
	spinner  spinner.Model
	width    int
	height   int
	memory   memoryState

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

	// Companion activity is an independent privacy-preserving source. It is
	// never joined with task routing or Tokscale usage, even when timestamps
	// overlap.
	companionDatabasePath    string
	companionActivityLoading bool
	companionActivityState   companionActivityLoadState
	companionActivity        []companionActivityEntry

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
		styles:                newStyles(),
		nexusDir:              findNexusDir(),
		spinner:               s,
		localAI:               true, // default on
		companionDatabasePath: defaultObservabilityDatabasePath(),
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
		m.height = msg.Height
		if m.screen == screenMemory {
			return updateMemory(msg, m)
		}
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
		if msg.String() == "esc" && m.screen != screenMenu && m.screen != screenMemory {
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
	case screenCompanionActivity:
		return updateCompanionActivity(msg, m)
	case screenMemory:
		return updateMemory(msg, m)
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
	case screenCompanionActivity:
		s = companionActivityView(m)
	case screenMemory:
		s = memoryView(m)
	}
	v := tea.NewView(s)
	v.AltScreen = true
	return v
}

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--tui" {
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "Usage: nexus --tui")
			os.Exit(2)
		}
		args = nil
	}
	if len(args) > 0 && args[0] == "companion" {
		os.Exit(runCompanionCLI(args[1:], findNexusDir(), os.Stdout))
	}
	if len(args) > 0 && args[0] == "memory" {
		root, err := memoryRoot()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Cannot locate user home directory:", err)
			os.Exit(1)
		}
		os.Exit(runMemoryCLI(args[1:], root, os.Stdin, os.Stdout, os.Stderr))
	}
	if len(args) > 0 && args[0] == "status" {
		if len(args) != 2 || args[1] != "--json" {
			fmt.Fprintln(os.Stderr, "Usage: nexus status --json")
			os.Exit(2)
		}
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Cannot locate user home directory")
			os.Exit(1)
		}
		if err := json.NewEncoder(os.Stdout).Encode(localStatus(home)); err != nil {
			fmt.Fprintln(os.Stderr, "Cannot write status")
			os.Exit(1)
		}
		return
	}
	if len(args) > 0 && args[0] == "--version" {
		fmt.Println("nexus " + version)
		return
	}
	if len(args) > 0 && args[0] == "configure" {
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "Usage: nexus configure")
			os.Exit(2)
		}
		m := initialModel()
		m.screen = screenConfigure
		p := tea.NewProgram(m)
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(args) > 0 && args[0] == "route" {
		args = args[1:]
	}
	if len(args) > 0 {
		request, err := parseRouteRequest(args)
		if err != nil {
			fmt.Fprintln(os.Stderr, "NEXUS:", err)
			os.Exit(2)
		}
		if err := routePrompt(findNexusDir(), request, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "NEXUS:", err)
			os.Exit(1)
		}
		return
	}
	m := initialModel()
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
