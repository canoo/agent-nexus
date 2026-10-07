package main

import (
	"charm.land/lipgloss/v2"
)

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
