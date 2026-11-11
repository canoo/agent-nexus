package main

import (
	tea "charm.land/bubbletea/v2"
)

var menuItems = []string{
	"Install NEXUS",
	"Configure",
	"Health Check",
	"Task Log",
	"Usage & Cost Dashboard",
	"Companion Tool Activity",
	"Update NEXUS",
	"Uninstall NEXUS",
	"Project Memory",
}

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
				m.screen = screenCompanionActivity
				return startCompanionActivityLoad(m)
			case 6:
				m.screen = screenUpdate
				m.running = false
				m.output = ""
				m.err = nil
				if m.latestVersion == "" {
					m.running = true
					return m, tea.Batch(m.spinner.Tick, checkLatestVersion())
				}
				return m, nil
			case 7:
				m.screen = screenUninstall
				m.running = false
				m.output = ""
				m.err = nil
				m.uninstallConfirmed = false
				return m, nil
			case 8:
				return startMemory(m)
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
