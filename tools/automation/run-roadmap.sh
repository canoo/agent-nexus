#!/bin/bash
# run-roadmap.sh — Codelogiic autonomous phase runner
#
# Usage: run-roadmap.sh [--max-phases N] [--dry-run]
# Run from the project root after docs/dev-roadmap.md has been generated.
#
# Requires: claude CLI, git
# Permissions: uses --dangerously-skip-permissions for non-interactive automation.
#   Alternatively, configure allowed tools in .claude/settings.json instead.

set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────────
ROADMAP="docs/dev-roadmap.md"
STATUS_FILE=".phase-status"
LOG_FILE=".phase-log"
MAX_PHASES=30
DRY_RUN=false

# ── Arg parsing ───────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case $1 in
    --max-phases) MAX_PHASES="$2"; shift 2 ;;
    --dry-run)    DRY_RUN=true; shift ;;
    *) echo "Unknown arg: $1"; exit 1 ;;
  esac
done

# ── Helpers ───────────────────────────────────────────────────────────────────
log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" | tee -a "$LOG_FILE"; }

check_prereqs() {
  if ! command -v claude &>/dev/null; then
    log "ERROR: claude CLI not found"; exit 1
  fi
  if [ ! -f "$ROADMAP" ]; then
    log "ERROR: $ROADMAP not found — run engineering-dev-roadmap first"; exit 1
  fi
  if ! git rev-parse --git-dir &>/dev/null; then
    log "ERROR: not a git repo"; exit 1
  fi
}

check_dirty_tree() {
  if [ -n "$(git status --porcelain)" ]; then
    log "WARN: dirty working tree detected (partial phase or uncommitted files)"
    log "Stashing changes before retry..."
    git stash push -m "auto-stash: run-roadmap.sh phase retry $(date '+%Y%m%d-%H%M%S')"
    log "Stashed. Continuing."
  fi
}

read_status() {
  if [ -f "$STATUS_FILE" ]; then
    cat "$STATUS_FILE"
  else
    echo ""
  fi
}

# ── Phase prompt ──────────────────────────────────────────────────────────────
build_prompt() {
  cat <<'PROMPT'
You are the Codelogiic pipeline coordinator. Your job is coordination only — you do NOT write code, edit files, or make commits directly.

STEP 1 — Identify the next phase:
- Read docs/dev-roadmap.md
- Run `git log --oneline` to see completed commits
- Cross-reference against the Master Commit List to find the next incomplete phase

STEP 2 — Delegate to the Agents Orchestrator:
- Use the Agent tool with subagent_type="agents-orchestrator" to execute the phase
- Pass the phase name, phase number, and the exact list of commits expected from docs/dev-roadmap.md
- The orchestrator must delegate implementation to the appropriate specialist agents (frontend-agent, firebase-agent, etc.)
- All git commits must go through the git-workflow-master agent
- Do NOT implement anything yourself — if you find yourself writing code or editing files, stop and delegate instead

STEP 3 — Write phase status to the .phase-status file using bash:
- Run: echo "PHASE_COMPLETE: Phase N — [phase name]" > .phase-status
- Run: echo "ALL_COMPLETE" > .phase-status  (if this was the final phase)
- Run: echo "BLOCKED: [reason]" > .phase-status  (if phase could not complete cleanly)
You MUST write this file using a tool call — do not just print the status to output.

RULES:
- Execute exactly one phase per session
- No partial commits — if blocked, write BLOCKED status and stop
- You are a coordinator, not an implementer

PROMPT
}

# ── Main loop ─────────────────────────────────────────────────────────────────
main() {
  check_prereqs

  log "═══════════════════════════════════════════"
  log "Codelogiic Phase Runner started"
  log "Roadmap: $ROADMAP"
  log "Max phases: $MAX_PHASES"
  log "═══════════════════════════════════════════"

  rm -f "$STATUS_FILE"
  phase_count=0

  while true; do
    # Safety ceiling
    if [ "$phase_count" -ge "$MAX_PHASES" ]; then
      log "MAX_PHASES ($MAX_PHASES) reached — stopping."
      break
    fi

    # Guard: clean dirty tree before each session
    check_dirty_tree

    log "--- Starting session $((phase_count + 1)) ---"

    if [ "$DRY_RUN" = true ]; then
      log "[DRY RUN] Would invoke: claude -p \"$(build_prompt)\""
      break
    fi

    # Run claude session
    # Note: --dangerously-skip-permissions required for non-interactive automation.
    # Alternative: add tool permissions to .claude/settings.json in the project.
    claude -p "$(build_prompt)" \
      --dangerously-skip-permissions \
      2>&1 | tee -a "$LOG_FILE"

    EXIT_CODE=${PIPESTATUS[0]}

    if [ "$EXIT_CODE" -ne 0 ]; then
      log "Session exited with code $EXIT_CODE — stopping loop."
      log "Check $LOG_FILE for details. Re-run to retry from last committed phase."
      break
    fi

    STATUS="$(read_status)"

    if [ -z "$STATUS" ]; then
      log "WARN: .phase-status not written by session — stopping as precaution."
      break
    fi

    log "Status: $STATUS"

    if echo "$STATUS" | grep -q "^ALL_COMPLETE"; then
      log "═══════════════════════════════════════════"
      log "All phases complete. Roadmap executed successfully."
      log "═══════════════════════════════════════════"
      break
    fi

    if echo "$STATUS" | grep -q "^BLOCKED"; then
      log "Phase blocked: $STATUS"
      log "Resolve the issue manually, then re-run to continue."
      break
    fi

    if echo "$STATUS" | grep -q "^PHASE_COMPLETE"; then
      log "Phase complete — starting next session."
      rm -f "$STATUS_FILE"
      phase_count=$((phase_count + 1))
      sleep 3  # brief pause between sessions
      continue
    fi

    log "WARN: unrecognised status '$STATUS' — stopping."
    break
  done

  log "Runner finished. Phases executed: $phase_count"
}

main
