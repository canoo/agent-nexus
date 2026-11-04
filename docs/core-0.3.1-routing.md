# NEXUS core 0.3.1 — routing, tiered execution, and TUI routing panel

Status: draft PR #124 only, not merged to main.

Implemented in this branch, in order:
1. Confidence + tier on the existing deterministic classifier, display-only
   (simple 0.95 cheap, medium 0.85 main, sensitive/architecture 0.90
   advisor, ambiguous 0.50 advisor, low-confidence threshold 0.70).
   No execution change, no automatic advisor escalation.
2. Memory vault, stage 1 — minimal port from feat/v0.3.0-integration
   (identical files on feat/nexus-core-pivot, verified by diff):
   `tools/tui/memory_store.go` (287 lines), `tools/tui/memory_cli.go`
   (159 lines), plus `memory_cli_test.go`, `memory_store_test.go`,
   and `memory_e2e_test.go`. New dependency: `gopkg.in/yaml.v3 v3.0.1`
   only. New CLI: `nexus memory list|show|save|search|init`, wired in
   `main.go` before the route fallback. Verified on Saturn with
   go1.27.3 (mise shim): `go vet ./...` clean, `go test ./...` ok.
   NOT ported yet: the TUI browser `memory.go` (361 lines) plus
   `memory_tui_test.go` (150 lines) — that needs screen/model wiring
   in main.go (screenMemory, memoryState, menu entry) and is stage 2,
   not silently included here.
3. Routing panel, display-only: `tools/tui/route_panel.go` renders
   lead session / fork layer / cheap swarm boxes / back-to-lead
   review / advisor side panel / real memory-vault count. Swarm
   boxes are labelled "planned, not run" and the advisor panel
   reports 0 calls with no last advice on routine turns. No fan-out,
   no advisor call, no execution change.

Proposed, NOT implemented (needs Emi approval separately):
- Advisor-gated execution at exactly three gates: before a plan
  locks, when the same error appears twice, before sign-off.
- Parallel cheap fan-out, with diff audit isolated in a throwaway
  worktree.
- TUI memory browser (stage 2 above).
- MCP additions and a standalone token/usage CLI check (audited
  only so far; Tokscale aggregation already exists, no new backend).

Beta split (proposal, not applied):
- #115 stays on feat/v0.3.0-integration. Keep it synced by merging
  main INTO the beta branch, never beta into main, until a clean
  live GUI test passes and Emi approves a merge.
- Tauri build stays on the beta branch: build it from that branch
  with its own workflow/artifacts, do not commit built binaries
  to main. Core pieces in #115 that are candidates to port to
  0.3.1 (not auto-merged): the memory files above, and the shared
  control service (`tools/tui/control_service.go`) — the latter
  is companion-coupled and needs its own review before any port.

Milestone proposal (not applied):
- Keep #8 "v0.3.1 — Universal Sync Layer" (sync/adopt scope,
  15 open issues) as-is. Create a NEW milestone for core 0.3.1,
  proposed title "v0.3.1 — Core: Routing, MCPs, Token and Memory".
  Renaming #8 would mislabel its 15 sync issues; keeping it is
  the cleaner trade-off, for Emi to decide in one line.
