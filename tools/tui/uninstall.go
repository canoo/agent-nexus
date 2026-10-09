package main

import (
	"os/exec"
	"path/filepath"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

type cmdDoneMsg struct {
	output string
	err    error
}

// --- uninstall ---

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

func runScript(nexusDir, script string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("bash", filepath.Join(nexusDir, script))
		out, err := cmd.CombinedOutput()
		return cmdDoneMsg{output: string(out), err: err}
	}
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max] + "\n... (truncated)"
	}
	return s
}
