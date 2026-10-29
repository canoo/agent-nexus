package main

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// --- task log ---

type taskLogEntry struct {
	Tool                string  `json:"tool"`
	Model               string  `json:"model"`
	Routing             string  `json:"routing,omitempty"`
	TaskType            string  `json:"task_type,omitempty"`
	ModelProvider       string  `json:"model_provider,omitempty"`
	RouteBand           string  `json:"route_band,omitempty"`
	TokensIn            int     `json:"tokens_in,omitempty"`
	TokensOut           int     `json:"tokens_out,omitempty"`
	CloudCostEquivalent float64 `json:"cloud_cost_equivalent,omitempty"`
	CostUSD             float64 `json:"cost_usd,omitempty"`
	InputBytes          int64   `json:"input_bytes,omitempty"`
	OutputBytes         int64   `json:"output_bytes,omitempty"`
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
	routeBands map[string]int
	cloudUSD   float64
	costUSD    float64
	savingsUSD float64
}

func loadTaskLog() tea.Cmd {
	return func() tea.Msg {
		// No $HOME means neither log location can be resolved.
		if _, err := nexusLogDir(); err != nil {
			return taskLogMsg{}
		}
		dbPath, _ := observabilityDBPath()
		return taskLogMsg{entries: loadTaskLogEntries(dbPath)}
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
		if stats.cloudUSD > 0 || stats.costUSD > 0 {
			s += fmt.Sprintf("  Est. local savings: $%.4f\n", stats.savingsUSD)
		}
		s += fmt.Sprintf("  Routes: %s\n", summarizeIntCounts(stats.routes, 3))
		s += fmt.Sprintf("  Route bands: %s\n", summarizeIntCounts(stats.routeBands, 3))
		if stats.cloudUSD > 0 || stats.costUSD > 0 {
			s += fmt.Sprintf("  Cloud equivalent: $%.4f  Local cost: $%.4f\n", stats.cloudUSD, stats.costUSD)
		}
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

func summarizeTaskLog(entries []taskLogEntry) taskLogStats {
	stats := taskLogStats{
		modelTasks: map[string]int{},
		routes:     map[string]int{},
		routeBands: map[string]int{},
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
			stats.savingsUSD += e.CloudCostEquivalent - e.CostUSD
			stats.cloudUSD += e.CloudCostEquivalent
			stats.costUSD += e.CostUSD
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
		routeBand := strings.TrimSpace(e.RouteBand)
		if routeBand == "" {
			routeBand = "unknown"
		}
		stats.routeBands[routeBand]++
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

func truncateCol(s string, max int) string {
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}
