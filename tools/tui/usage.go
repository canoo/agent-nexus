package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

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
		if stats.cloudUSD > 0 || stats.costUSD > 0 {
			s += fmt.Sprintf("  Est. local savings: $%.4f\n", stats.savingsUSD)
		}
		s += fmt.Sprintf("  Routes: %s\n", summarizeIntCounts(stats.routes, 3))
		s += fmt.Sprintf("  Route bands: %s\n", summarizeIntCounts(stats.routeBands, 3))
		if stats.cloudUSD > 0 || stats.costUSD > 0 {
			s += fmt.Sprintf("  Cloud equivalent: $%.4f  Local cost: $%.4f\n", stats.cloudUSD, stats.costUSD)
		}
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
