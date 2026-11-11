package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

const (
	claudeSessionRetentionKey     = "NEXUS_CLAUDE_SESSION_RETENTION_DAYS"
	defaultClaudeSessionRetention = 30
)

type saveFeedbackMsg struct{}

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
					_, size := utf8.DecodeLastRuneInString(m.editBuf)
					m.editBuf = m.editBuf[:len(m.editBuf)-size]
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

// --- .env helpers ---

func loadEnv(m *model) {
	data, _ := os.ReadFile(filepath.Join(m.nexusDir, ".env"))
	values := parseSettings(string(data))
	for i, key := range m.configKeys {
		if value, ok := settingDefaults[key]; ok {
			m.configVals[i] = value
		}
		if value, ok := values[key]; ok && value != "" {
			m.configVals[i] = value
		}
		if value, ok := os.LookupEnv(key); ok {
			if value != "" {
				m.configVals[i] = value
			} else {
				if fallback, ok := settingDefaults[key]; ok {
					m.configVals[i] = fallback
				}
			}
		}
		if key == claudeSessionRetentionKey {
			m.configVals[i], _ = normalizeClaudeSessionRetention(m.configVals[i])
		}
	}
	m.localAI = m.configVals[0] != "false"
}

func saveEnv(m model) error {
	values := append([]string(nil), m.configVals...)
	for i, key := range m.configKeys {
		if key == claudeSessionRetentionKey {
			values[i], _ = normalizeClaudeSessionRetention(values[i])
		}
	}
	return writeSettings(filepath.Join(m.nexusDir, ".env"), m.configKeys, values)
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
