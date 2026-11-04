# NEXUS core 0.3.1 — routing, tiered execution, and TUI routing panel (draft)

Status: draft proposal for 0.3.1, on branch feat/nexus-core-0.3.1.
Implemented in this branch: confidence + tier on the existing
deterministic classifier, display-only. NOT implemented: advisor-gated
execution, parallel cheap fan-out, or a TUI panel. Those are proposed
below and need Emi's go-ahead before any execution change.

## What exists on main today (audited Nov 4)

- `tools/tui/router.go`: keyword classifier. Simple recognised tasks
  (commit message, boilerplate, test scaffold) go local; medium tasks
  (lint fix, refactor) go local; sensitive/architecture keywords and
  anything ambiguous go to agy. Dry-run prints the decision as JSON.
- `tools/tui/tokscale.go` + usage dashboard in `main.go`: usage
  aggregates come from an already-installed Tokscale CLI; absence of
  Tokscale never breaks NEXUS health. There is no standalone
  token/usage CLI check on main.
- `tools/mcp/server.mjs`: an MCP server with observability/task
  storage, settings, and tests. The context-projection MCP and a
  community MCP registry are still listed as TBD in ROADMAP.md.
- Memory: there is NO memory CLI on main (`agent-memory/.keep`
  only). The memory CLI, store, and TUI browser live on
  feat/nexus-core-pivot (`tools/tui/memory*.go`). A memory-vault
  view for 0.3.1 therefore cannot be built on main alone — see
  the blocker in the morning report.

## Proposed tiered session, provider-agnostic

Translate the tiers to roles, not vendor model names:

1. Cheap tier — small local model. Fans out in parallel for
   search, call-site lookup, test runs, and diff audits. A diff
   audit runs isolated in a throwaway worktree, never in the
   working tree.
2. Main tier — the main model. Leads the session, writes code,
   and fixes code. This is the session the user talks to.
3. Advisor tier — a top model, on call only. It never writes
   code itself. It advises and reviews.

## Advisor gates: exactly three moments

The advisor is silent on routine turns. It is consulted only:

1. Before a plan locks.
2. When the same error appears twice.
3. Before anything is called done / signed off, where it only
   approves or rejects.

Any other use is out of scope for this design.

## Deterministic fork layer

One layer below the advisor, fast micro-decisions — which file,
which tool, retry or stop — are resolved in code with a confidence
score. Only low-confidence splits escalate to a model. In this
branch that is implemented as: recognised simple tasks score
0.95, recognised medium tasks 0.85, sensitive/architecture
tasks 0.90, and ambiguous tasks 0.50, with a low-confidence
threshold of 0.70. The score is shown in dry-run JSON; execution
still follows the old destination, and no automatic escalation
happens yet.

## TUI routing panel to build (proposal, not code yet)

Layout, top to bottom, with a side panel:

- Top: lead session (main tier) — current goal, current step.
- Middle: deterministic fork layer — the decision just made,
  its confidence score, and whether it was resolved in code or
  escalated.
- Below that: parallel cheap subagent progress boxes — one box
  per cheap task (search, lookup, tests, diff audit), each
  showing percent, done, or running.
- Back to the lead: review / verify step, where the main tier
  checks the cheap tier's results before anything is accepted.
- Side panel: advisor on call — silent on routine turns, showing
  calls made, tokens read, and its last advice, only when a gate
  above was hit.

This panel reuses the existing router decision and the existing
Tokscale usage aggregates for the tokens-read figure; it does
not invent a new usage backend.

## What is NOT in this draft

- No change to route execution, no automatic advisor calls,
  no parallel fan-out, no new MCP tools, no memory backend.
- No merge to main. This branch is a draft PR only.
- No vendor model names are used as NEXUS tiers.
