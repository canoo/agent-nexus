package main

import (
	"bytes"
	"net/http"
	"os"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type compatLoadedMsg struct {
	report compatibilityReport
	err    error
}
type compatSyncedMsg struct {
	output string
	err    error
}

func loadCompat(nexusDir, project string, refresh bool) tea.Cmd {
	return func() tea.Msg {
		if refresh {
			if err := refreshCompat(http.DefaultClient); err != nil {
				return compatLoadedMsg{err: err}
			}
		}
		report, err := inspectCompatibility(nexusDir, project)
		return compatLoadedMsg{report, err}
	}
}
func updateCompat(msg tea.Msg, m model) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		refreshCompatViewport(&m)
		return m, nil
	case compatLoadedMsg:
		m.running = false
		m.err = msg.err
		if msg.err == nil {
			m.compatReport = msg.report
		}
		refreshCompatViewport(&m)
		return m, nil
	case compatSyncedMsg:
		m.running = false
		m.output = msg.output
		m.err = msg.err
		refreshCompatViewport(&m)
		return m, nil
	case tea.KeyPressMsg:
		if m.running {
			return m, nil
		}
		switch msg.String() {
		case "r", "u":
			m.running = true
			m.err = nil
			return m, loadCompat(m.nexusDir, m.compatProject, msg.String() == "u")
		case "s":
			m.running = true
			m.err = nil
			project := m.compatProject
			return m, func() tea.Msg {
				var out bytes.Buffer
				err := projectInstructions(project, "sync", false, &out)
				return compatSyncedMsg{out.String(), err}
			}
		}
	}
	var cmd tea.Cmd
	m.compatViewport, cmd = m.compatViewport.Update(msg)
	return m, cmd
}
func compatView(m model) string {
	s := m.compatViewport.View()
	if m.running {
		s += "\nWorking…"
	}
	s += "\nj/k: scroll • r: inspect • u: refresh registry\ns: sync this project • esc: back"
	return m.borderBox(s)
}
func refreshCompatViewport(m *model) {
	width := m.width - 10
	if m.width == 0 {
		width = 88
	}
	width = max(20, min(88, width))
	height := m.height - 10
	if m.height == 0 {
		height = 18
	}
	height = max(3, height)
	m.compatViewport.SetWidth(width)
	m.compatViewport.SetHeight(height)
	s := formatCompatibility(m.compatReport)
	if m.err != nil {
		s += "\nError: " + m.err.Error()
	}
	s += "\n" + m.output
	m.compatViewport.SetContent(lipgloss.NewStyle().Width(width).Render(s))
}
func startCompat(m model) (tea.Model, tea.Cmd) {
	m.screen = screenCompat
	m.compatProject, m.err = os.Getwd()
	m.output = ""
	m.compatReport = compatibilityReport{Project: m.compatProject, Source: "loading local registry"}
	m.compatViewport = viewport.New()
	refreshCompatViewport(&m)
	if m.err != nil {
		return m, nil
	}
	m.running = true
	return m, loadCompat(m.nexusDir, m.compatProject, false)
}
