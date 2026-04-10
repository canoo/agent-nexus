#!/bin/bash
# run-pipeline.sh — Codelogiic multi-model phase runner v2
#
# Usage:
#   run-pipeline.sh               Run full pipeline from docs/dev-roadmap.md
#   run-pipeline.sh --resume      Skip phases whose Tasks are already completed
#   run-pipeline.sh --audit-only  Run Gemini audit gate only, no implementation
#   run-pipeline.sh --skip-audit  Skip the Gemini audit gate (faster iteration)
#   run-pipeline.sh --max-phases N  Safety ceiling (default: 30)
#
# Requires: claude CLI, git, gh, jq, python3
# Env vars (optional):
#   GEMINI_API_KEY    — enables pre-implementation audit gate
#   OLLAMA_HOST_URL   — enables L1 local model delegation (default: http://localhost:11434)
#   GITHUB_USERNAME   — your GitHub handle for issue approval pings
#
# Model: Claude Opus 4.6 for orchestration; each phase delegates to Sonnet/Haiku
# State: Managed via Claude Code's native Task system (not .phase-status)

set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────────
ROADMAP="docs/dev-roadmap.md"
LOG_FILE=".pipeline-log"
MAX_PHASES=30

# ── Flags ─────────────────────────────────────────────────────────────────────
AUDIT_ONLY=false
SKIP_AUDIT=false
RESUME=false
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

while [[ $# -gt 0 ]]; do
  case $1 in
    --audit-only) AUDIT_ONLY=true;  shift ;;
    --skip-audit) SKIP_AUDIT=true;  shift ;;
    --resume)     RESUME=true;      shift ;;
    --max-phases) MAX_PHASES="$2";  shift 2 ;;
    *) echo "Unknown arg: $1"; exit 1 ;;
  esac
done

# ── Helpers ───────────────────────────────────────────────────────────────────
log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" | tee -a "$LOG_FILE"; }

check_prereqs() {
  command -v claude  &>/dev/null || { log "ERROR: claude CLI not found"; exit 1; }
  command -v git     &>/dev/null || { log "ERROR: git not found"; exit 1; }
  command -v gh      &>/dev/null || { log "ERROR: gh CLI not found (needed for PRs)"; exit 1; }
  command -v jq      &>/dev/null || { log "ERROR: jq not found (brew install jq)"; exit 1; }
  command -v python3 &>/dev/null || { log "ERROR: python3 not found"; exit 1; }
  [ -f "$ROADMAP" ] || {
    log "ERROR: $ROADMAP not found — run engineering-dev-roadmap agent first"
    exit 1
  }
  git rev-parse --git-dir &>/dev/null || { log "ERROR: not a git repo"; exit 1; }
  if [ -n "$(git status --porcelain)" ]; then
    log "WARN: dirty working tree — stashing before pipeline run"
    git stash push -m "auto-stash: run-pipeline.sh $(date '+%Y%m%d-%H%M%S')"
    log "Stashed. Continuing."
  fi
}

# ── Gemini audit gate ─────────────────────────────────────────────────────────
run_audit_gate() {
  if [[ "$SKIP_AUDIT" == true ]]; then
    log "Skipping Gemini audit gate (--skip-audit)"
    return 0
  fi

  if [ -z "${GEMINI_API_KEY:-}" ]; then
    log "WARN: GEMINI_API_KEY not set — skipping Gemini audit gate"
    log "      Set GEMINI_API_KEY to enable pre-implementation review."
    return 0
  fi

  local audit_script="${SCRIPT_DIR}/gemini-audit.sh"
  if [ ! -x "$audit_script" ]; then
    log "WARN: gemini-audit.sh not found or not executable at $audit_script — skipping"
    return 0
  fi

  log "Running Gemini audit gate..."
  set +e
  "$audit_script" "$ROADMAP" "$(pwd)" 2>&1 | tee -a "$LOG_FILE"
  local AUDIT_EXIT=${PIPESTATUS[0]}
  set -e

  case "$AUDIT_EXIT" in
    0)
      log "Gemini audit: PASS or WARN — proceeding to implementation."
      ;;
    1)
      log "════════════════════════════════════════════════════════"
      log "GEMINI AUDIT FAILED — implementation is BLOCKED."
      log "Review .gemini-audit-report.json and revise docs/dev-roadmap.md."
      log "Re-run after revision, or use --skip-audit to bypass (not recommended)."
      log "════════════════════════════════════════════════════════"
      exit 1
      ;;
    2)
      log "WARN: Gemini API error (exit 2) — could not complete audit."
      log "      Check your GEMINI_API_KEY and network. Proceeding without audit."
      ;;
    *)
      log "WARN: Unexpected audit exit code $AUDIT_EXIT — proceeding without audit."
      ;;
  esac
}

# ── Orchestrator prompt ───────────────────────────────────────────────────────
build_prompt() {
  local mode_flags=""
  [[ "$AUDIT_ONLY" == true ]] && mode_flags="**MODE: AUDIT ONLY** — After Step 1 (Task graph), stop. Do not execute any phases. The audit gate ran as a shell pre-check before this session started."
  [[ "$RESUME"     == true ]] && mode_flags="${mode_flags} **MODE: RESUME** — In Step 3, call TaskList first. Skip any tasks already showing status=completed."

  # Ollama availability note for the orchestrator
  local ollama_note=""
  if [ -n "${OLLAMA_HOST_URL:-}" ]; then
    ollama_note="**L1 LOCAL MODELS AVAILABLE** — scripts/ollama-delegate.sh is configured at ${OLLAMA_HOST_URL}.
Use it (via Bash tool) for: commit-msg, boilerplate, test-scaffold, lint-fix, logic-refactor tasks.
Circuit breaker rules — when ollama-delegate.sh exits with code 3:
  1. Immediately echo to console: 'WARN: Local Ollama unreachable. Tripping circuit breaker.'
  2. Call TaskUpdate on the affected task, status=blocked, reason='ollama-circuit-breaker'
  3. Allow all currently-running parallel Agent calls to finish normally
  4. Do NOT spawn any new phases after the wave completes
  5. Print final summary of completed/blocked tasks and exit
When ollama-delegate.sh exits with code 4 (BAD_OUTPUT): retry once, then fallback to handling the task yourself.
"
  else
    ollama_note="**L1 LOCAL MODELS NOT CONFIGURED** — OLLAMA_HOST_URL is not set. Handle all L1 tasks (commit messages, boilerplate) yourself."
  fi

  cat <<PROMPT
You are the Codelogiic pipeline orchestrator running with Claude Opus 4.6. Execute the development roadmap at ${ROADMAP} using the multi-model pipeline.

${mode_flags}

${ollama_note}

---

## STEP 1 — Parse the roadmap into a dependency graph

Read ${ROADMAP}.

For each Phase N, extract:
- phase_id (integer)
- phase_name (string)
- commits: ordered list of { order, type, message, files_affected, goal }
- phase_type: classify as one of: baseline | cleanup | infrastructure | architecture | feature | polish | docs
- effort: infer from commit count → 1-2=S, 3-5=M, 6-10=L, 11+=XL

If the roadmap includes a "## Phase Dependencies" section, use it directly.
Otherwise infer dependencies by this rule:
- Phase 0 (Baseline) always blocks all others
- If Phase B lists files in "Files Affected" that overlap with Phase A's outputs, B depends_on A
- If no file overlap exists, phases are independent and can run in parallel
- Phase 6 (Docs/Release) always depends on all implementation phases
- Prefer parallelism: only add a dependency when you see a concrete file conflict

Assign model tier per phase:
- model_tier="haiku"  if phase_type is "baseline" or "docs"
- model_tier="sonnet" for all other phase types

---

## STEP 2 — Build the Task graph

Call TaskCreate for each phase with:
- subject:     "Phase {N} — {phase_name}"
- description: full commit list as a markdown table, plus effort={S|M|L|XL}, phase_type={type}, model_tier={haiku|sonnet}

After all tasks are created, log the dependency graph so it is visible in the session output.

---

## STEP 3 — Execute phases in parallel waves

### Finding the first wave
Call TaskList to get all tasks with status=pending that have no blocking dependencies.
Spawn ALL of them simultaneously in a single message using the Agent tool (multiple calls in one response).

### Per-phase Agent call spec
For each unblocked phase, spawn:
  subagent_type: "agents-orchestrator"
  isolation:     "worktree"
  model:         {model_tier from task — "sonnet" or "haiku"}
  prompt:        (use the Phase Execution Prompt template below, filled in for this phase)

### After each wave completes
When an Agent returns:
1. Call TaskUpdate to mark the task completed (or blocked with reason if it returned BLOCKED)
2. Call TaskList to identify newly unblocked tasks
3. Spawn the next wave
4. Repeat until all tasks are completed or blocked

### Blocked phase handling
- If a phase returns BLOCKED: mark the task blocked, log the reason, continue with other independent phases
- Retry a blocked phase up to 2 times (re-spawn with the BLOCKED reason included in the prompt)
- After 2 failed retries: leave the task blocked, note it in the final summary, do not halt the pipeline

---

## STEP 4 — Final integration check

When all tasks are either completed or blocked (no more pending tasks):
Spawn: subagent_type="testing-reality-checker"
  prompt: "Run final integration check. Verify the build passes, tests pass if they exist, and there are no obvious regressions introduced across the completed phases."

---

## STEP 5 — Merge completed worktrees

For each completed phase (not blocked):
- If effort=S: auto-merge its worktree branch to main in dependency order using git-workflow-master
- If effort=M, L, or XL: create a PR:
    gh pr create --title "Phase {N} — {phase_name}" --body "{commit list from task description}"

Always merge/PR in dependency order (Phase 0 first, then independent phases alphabetically, then dependent phases).

---

## PHASE EXECUTION PROMPT TEMPLATE

When spawning an agents-orchestrator sub-agent for a phase, use exactly this prompt structure:

---
You are executing Phase {N} — {phase_name} of the Codelogiic development pipeline.

Phase type: {phase_type}
Effort: {effort}
You are operating in an isolated git worktree. Do not touch main branch.

Commits to execute in order:
{full commit table from task description}

Commit-type & Tag → specialist routing:
- FIRST, check for the [L1] tag in the commit message or goal. 
  → If [L1] is present: You MUST execute this task locally using the Bash tool to call \`scripts/ollama-delegate.sh "\$OLLAMA_HOST_URL" "<task_description>"\`. Do NOT spawn an Anthropic agent for [L1] tasks.
- If NO [L1] tag, route by commit type:
  - feat:/fix: on frontend/UI files  → frontend-developer
  - feat:/fix: on backend/API files  → engineering-backend-architect
  - feat:/fix: on mobile/Capacitor   → engineering-mobile-app-builder
  - chore: deps/config/CI            → engineering-devops-automator
  - refactor:                        → engineering-code-reviewer
  - test:                            → delegate to engineering-code-reviewer or appropriate test agent
  - style:/perf:                     → frontend-developer
  - docs:                            → handle directly with Write/Edit tools

Git rules (non-negotiable):
- Every commit goes through git-workflow-master agent — no direct git commits
- One commit per task item — do not batch multiple commit messages
- If a commit fails: report BLOCKED, do not attempt to fix by squashing

When all commits are executed:
- Run: git log --oneline to verify commit count matches the plan
- Output exactly: PHASE_COMPLETE: Phase {N} — {phase_name}

If blocked at any point:
- Output exactly: BLOCKED: Phase {N} — {reason}
- Do not continue past a blocking error
---

---

## ORCHESTRATOR RULES

- You are the coordinator. You do NOT write code, edit source files, or make commits.
- State lives entirely in the Task system (TaskCreate/TaskList/TaskUpdate).
- Do NOT write .phase-status — that file is deprecated and must not be created.
- Log every decision as text output — this is the observability layer. Be verbose about what you're doing and why.
- Max phases safety ceiling: ${MAX_PHASES} total Agent spawns across all waves. Stop and report if exceeded.
- The pipeline never requires all phases to succeed — it completes when no more phases can run.

PROMPT
}

# ── Main ──────────────────────────────────────────────────────────────────────
check_prereqs

log "═══════════════════════════════════════════════════════"
log "Codelogiic Pipeline v2"
log "Roadmap:    $ROADMAP"
log "Audit only: $AUDIT_ONLY"
log "Skip audit: $SKIP_AUDIT"
log "Resume:     $RESUME"
log "Max phases: $MAX_PHASES"
[ -n "${OLLAMA_HOST_URL:-}" ] && log "Ollama:     $OLLAMA_HOST_URL" || log "Ollama:     not configured"
log "═══════════════════════════════════════════════════════"

# Run Gemini audit gate as a shell-level pre-check (before the Opus session starts)
run_audit_gate

# If audit-only, stop here — no Opus session needed
if [[ "$AUDIT_ONLY" == true ]]; then
  log "Audit-only mode: done. Review .gemini-audit-report.json for the full report."
  exit 0
fi

claude -p "$(build_prompt)" \
  --dangerously-skip-permissions \
  --model claude-opus-4-6 \
  2>&1 | tee -a "$LOG_FILE"

EXIT_CODE=${PIPESTATUS[0]}

if [ "$EXIT_CODE" -ne 0 ]; then
  log "Pipeline exited with code $EXIT_CODE — check $LOG_FILE for details."
  exit "$EXIT_CODE"
fi

log "Pipeline session complete."