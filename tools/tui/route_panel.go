package main

import (
	"fmt"
	"strings"
)

// renderRoutePanel is display-only: it renders the tiered routing panel
// proposed in docs/core-0.3.1-routing.md for one dry-run decision. It
// never runs a cheap subagent, never calls an advisor model, and never
// changes execution. Cheap swarm boxes are labelled "planned, not run"
// until fan-out is separately approved and built. The advisor side
// panel is silent on routine turns: 0 calls, no last advice, until a
// gate is hit. Memory vault counts come from listProjects on the real
// vault root; a missing root reports 0, it is never invented.
func renderRoutePanel(d routeDecision, root string) string {
	projects, err := listProjects(root)
	projCount, entryCount := 0, 0
	if err == nil {
		projCount = len(projects)
		for _, p := range projects {
			entryCount += p.Count
		}
	}
	resolved := "resolved in code"
	if isLowConfidence(d) {
		resolved = "would escalate to a model (not automatic yet)"
	}
	var b strings.Builder
	b.WriteString("NEXUS routing panel (display-only)\n")
	fmt.Fprintf(&b, "Lead session [%s tier]: %s\n", d.Tier, d.Reason)
	fmt.Fprintf(&b, "Fork layer: confidence %.2f, threshold %.2f, %s\n", d.Confidence, lowConfidenceThreshold, resolved)
	b.WriteString("Cheap swarm (planned, not run):\n")
	for _, task := range []string{"repo search", "call-site lookup", "test runs", "diff audit (throwaway worktree)"} {
		fmt.Fprintf(&b, "  [ ] %s — planned, not run\n", task)
	}
	b.WriteString("Back to lead: review / verify before anything is accepted.\n")
	fmt.Fprintf(&b, "Advisor side panel: silent on routine turns, 0 calls, last advice: none. Gates: before a plan locks, on a repeated error, before sign-off.\n")
	fmt.Fprintf(&b, "Memory vault: %d projects, %d entries\n", projCount, entryCount)
	return b.String()
}
