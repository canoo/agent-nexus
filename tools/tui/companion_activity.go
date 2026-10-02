package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

const companionActivityDisplayLimit = 50

var companionToolLabels = map[string]string{
	"chatgpt":    "ChatGPT",
	"claude":     "Claude",
	"gemini":     "Gemini",
	"copilot":    "GitHub Copilot",
	"perplexity": "Perplexity",
}

var companionSurfaceLabels = map[string]string{
	"browser": "Browser",
	"desktop": "Desktop",
}

// loadCompanionActivity is a read-only view over the migration-owned SQLite
// store. Companion activity intentionally has no JSONL compatibility reader:
// mcp-tasks.jsonl is task history, not activity history.
func loadCompanionActivity(databasePath string) tea.Cmd {
	return func() tea.Msg {
		entries, state := readCompanionActivity(databasePath)
		return companionActivityMsg{entries: entries, state: state}
	}
}

func readCompanionActivity(databasePath string) ([]companionActivityEntry, companionActivityLoadState) {
	if databasePath == "" {
		return nil, companionActivityUnavailable
	}
	if _, err := os.Stat(databasePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, companionActivityUnavailable
		}
		return nil, companionActivityDegraded
	}

	// mode=ro prevents the dashboard from creating, migrating, or changing the
	// activity database. The native host/shared store remains its sole writer.
	databaseURL := (&url.URL{Scheme: "file", Path: databasePath, RawQuery: "mode=ro"}).String()
	database, err := sql.Open("sqlite", databaseURL)
	if err != nil {
		return nil, companionActivityDegraded
	}
	defer database.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		return nil, companionReadErrorState(err)
	}

	var collectionEnabled int
	err = database.QueryRowContext(ctx, `SELECT collection_enabled
        FROM companion_settings WHERE id = 1`).Scan(&collectionEnabled)
	if err != nil {
		return nil, companionReadErrorState(err)
	}
	if collectionEnabled != 0 && collectionEnabled != 1 {
		return nil, companionActivityDegraded
	}

	var enabledConsents int
	err = database.QueryRowContext(ctx, `SELECT COUNT(*)
        FROM companion_tool_consents
        WHERE enabled = 1 AND consent_policy_version = 1`).Scan(&enabledConsents)
	if err != nil {
		return nil, companionReadErrorState(err)
	}

	entries, err := queryCompanionActivity(ctx, database)
	if err != nil {
		return nil, companionReadErrorState(err)
	}

	if collectionEnabled == 0 {
		return entries, companionActivityDisabled
	}
	if enabledConsents == 0 {
		return entries, companionActivityNoConsent
	}
	return entries, companionActivityReady
}

func companionReadErrorState(err error) companionActivityLoadState {
	if errors.Is(err, sql.ErrNoRows) {
		return companionActivityUnavailable
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "no such table") ||
		strings.Contains(message, "unable to open database") ||
		strings.Contains(message, "readonly database") {
		return companionActivityUnavailable
	}
	return companionActivityDegraded
}

// queryCompanionActivity selects only the six fixed schema fields required to
// produce the activity view. The static allowlists prevent arbitrary database
// values, including metadata a user may have inserted manually, from entering
// the rendering model.
func queryCompanionActivity(ctx context.Context, database *sql.DB) ([]companionActivityEntry, error) {
	rows, err := database.QueryContext(ctx, `SELECT tool_id, surface, started_at, ended_at, detector, confidence
        FROM tool_activity
        WHERE tool_id IN ('chatgpt', 'claude', 'gemini', 'copilot', 'perplexity')
          AND surface IN ('browser', 'desktop')
          AND confidence = 'surface-active'
          AND ((surface = 'browser' AND detector = 'selected-browser-tab')
            OR (surface = 'desktop' AND detector = 'foreground-app'))
          AND length(started_at) <= 40
          AND length(ended_at) <= 40
        ORDER BY started_at DESC
        LIMIT ?`, companionActivityDisplayLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]companionActivityEntry, 0)
	for rows.Next() {
		var toolID, surface, startedAt, endedAt, detector, confidence string
		if err := rows.Scan(&toolID, &surface, &startedAt, &endedAt, &detector, &confidence); err != nil {
			return nil, err
		}
		entry, ok := mapCompanionActivityEntry(toolID, surface, startedAt, endedAt, detector, confidence)
		if ok {
			entries = append(entries, entry)
		}
	}
	return entries, rows.Err()
}

func mapCompanionActivityEntry(toolID, surface, startedAt, endedAt, detector, confidence string) (companionActivityEntry, bool) {
	if _, ok := companionToolLabels[toolID]; !ok {
		return companionActivityEntry{}, false
	}
	if _, ok := companionSurfaceLabels[surface]; !ok || confidence != "surface-active" {
		return companionActivityEntry{}, false
	}
	if (surface == "browser" && detector != "selected-browser-tab") ||
		(surface == "desktop" && detector != "foreground-app") {
		return companionActivityEntry{}, false
	}
	start, err := time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return companionActivityEntry{}, false
	}
	end, err := time.Parse(time.RFC3339Nano, endedAt)
	if err != nil || !end.After(start) {
		return companionActivityEntry{}, false
	}
	return companionActivityEntry{toolID: toolID, surface: surface, startedAt: start.UTC(), endedAt: end.UTC()}, true
}

func startCompanionActivityLoad(m model) (tea.Model, tea.Cmd) {
	m.companionActivityLoading = true
	m.companionActivityState = companionActivityLoading
	return m, tea.Batch(m.spinner.Tick, loadCompanionActivity(m.companionDatabasePath))
}

func updateCompanionActivity(msg tea.Msg, m model) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case companionActivityMsg:
		m.companionActivityLoading = false
		m.companionActivityState = msg.state
		m.companionActivity = msg.entries
	case spinner.TickMsg:
		if m.companionActivityLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case tea.KeyPressMsg:
		if msg.String() == "r" {
			return startCompanionActivityLoad(m)
		}
	}
	return m, nil
}

func companionActivityView(m model) string {
	s := m.styles.title.Render("◌ Companion Tool Activity") + "\n"
	s += m.styles.subtle.Render("Companion surface activity is separate from NEXUS task routing and Tokscale token/cost data; timestamps never create a task relationship.") + "\n\n"

	if m.companionActivityLoading {
		s += "  " + m.spinner.View() + " Loading local Companion activity...\n"
		s += "\n" + m.styles.subtle.Render("r: refresh • esc: back")
		return m.borderBox(s)
	}

	switch m.companionActivityState {
	case companionActivityDisabled:
		s += m.styles.warn.Render("  Collection is disabled. No new Companion activity is recorded.") + "\n"
	case companionActivityNoConsent:
		s += m.styles.warn.Render("  Collection has no current tool consent. No new Companion activity is recorded.") + "\n"
	case companionActivityUnavailable:
		s += m.styles.subtle.Render("  Companion storage is not available yet. Enable Companion and grant a tool consent before activity can appear here.") + "\n"
		s += "\n" + m.styles.subtle.Render("r: refresh • esc: back")
		return m.borderBox(s)
	case companionActivityDegraded:
		s += m.styles.warn.Render("  Companion activity is unavailable. Check local NEXUS storage, then press r.") + "\n"
		s += "\n" + m.styles.subtle.Render("r: refresh • esc: back")
		return m.borderBox(s)
	}

	if len(m.companionActivity) == 0 {
		s += m.styles.subtle.Render("  No Companion activity spans are stored yet.") + "\n"
		s += m.styles.subtle.Render("  Activity means an enabled supported surface was active; it does not mean a prompt was sent or a response was received.") + "\n"
	} else {
		totalDuration := time.Duration(0)
		for _, entry := range m.companionActivity {
			totalDuration += entry.endedAt.Sub(entry.startedAt)
		}
		s += fmt.Sprintf("  Stored spans: %d  Active time: %s\n\n", len(m.companionActivity), formatCompanionDuration(totalDuration))
		s += fmt.Sprintf("  %-18s %-10s %-12s %s\n", "Tool", "Surface", "Duration", "Ended (UTC)")
		s += m.styles.subtle.Render("  "+strings.Repeat("─", 66)) + "\n"
		for _, entry := range m.companionActivity {
			tool, toolOK := companionToolLabels[entry.toolID]
			surface, surfaceOK := companionSurfaceLabels[entry.surface]
			if !toolOK || !surfaceOK || !entry.endedAt.After(entry.startedAt) {
				continue
			}
			s += fmt.Sprintf("  %-18s %-10s %-12s %s\n", tool, surface,
				formatCompanionDuration(entry.endedAt.Sub(entry.startedAt)), entry.endedAt.Format("2006-01-02 15:04"))
		}
	}

	s += "\n" + m.styles.subtle.Render("Read-only local SQLite view • r: refresh • esc: back")
	return m.borderBox(s)
}

func formatCompanionDuration(duration time.Duration) string {
	if duration < time.Second {
		return "<1s"
	}
	return duration.Round(time.Second).String()
}
