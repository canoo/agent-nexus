#!/bin/bash
# issue-manager.sh — Format UI notes into developer tasks, append to roadmap, create GitHub issues
#
# Usage:
#   issue-manager.sh --model haiku <notes-file-or-quoted-string>
#   issue-manager.sh --model local <notes-file-or-quoted-string>
#
# Models:
#   haiku   — Uses claude CLI (claude-haiku-4-5-20251001) with File System + GitHub MCP tools.
#             Haiku autonomously appends the phase to docs/dev-roadmap.md and creates issues.
#   local   — Uses Ollama (qwen3:8b). The script parses the model output, appends markdown
#             to the roadmap via cat, and loops gh issue create for each generated issue.
#             Does NOT ask the local model to use MCP tools.
#
# Env vars (optional):
#   OLLAMA_HOST_URL   — Ollama base URL (default: http://192.168.1.101:11434)
#   GITHUB_REPO       — Target repo for gh issues, e.g. owner/repo (default: current repo)
#
# Exit codes:
#   0  — Success
#   1  — Usage error or missing prerequisite
#   3  — CIRCUIT_BREAKER: Ollama unreachable or timed out (local mode only)
#   4  — BAD_OUTPUT: Model returned unparseable output after retry (local mode only)

set -euo pipefail

# ── Config ─────────────────────────────────────────────────────────────────────
ROADMAP="docs/dev-roadmap.md"
OLLAMA_HOST_URL="${OLLAMA_HOST_URL:-http://192.168.1.101:11434}"
OLLAMA_MODEL="qwen3:8b"
HAIKU_MODEL="claude-haiku-4-5-20251001"
CONNECT_TIMEOUT=5
REQUEST_TIMEOUT=120
# Unambiguous delimiter between the markdown section and JSON section in local mode
JSON_DELIMITER="===JSON_START==="

# ── Usage ──────────────────────────────────────────────────────────────────────
usage() {
  echo "Usage: issue-manager.sh --model <haiku|local> <notes-file-or-string>" >&2
  echo "" >&2
  echo "  --model haiku   Claude Haiku via CLI with MCP tools (autonomous)" >&2
  echo "  --model local   Ollama qwen3:8b — script handles roadmap + issue creation" >&2
  echo "" >&2
  echo "  Input: path to a .txt file, or a quoted string of bullet-point notes" >&2
  exit 1
}

# ── Arg parsing ────────────────────────────────────────────────────────────────
MODEL=""
INPUT=""

[[ $# -lt 3 ]] && usage

while [[ $# -gt 0 ]]; do
  case "$1" in
    --model)
      [[ -z "${2:-}" ]] && { echo "ERROR: --model requires a value" >&2; usage; }
      MODEL="$2"
      shift 2
      ;;
    -*)
      echo "ERROR: Unknown flag: $1" >&2
      usage
      ;;
    *)
      INPUT="$1"
      shift
      ;;
  esac
done

[[ -z "$MODEL" ]] && { echo "ERROR: --model is required" >&2; usage; }
[[ -z "$INPUT" ]] && { echo "ERROR: Input (notes file or string) is required" >&2; usage; }

if [[ "$MODEL" != "haiku" ]] && [[ "$MODEL" != "local" ]]; then
  echo "ERROR: --model must be 'haiku' or 'local', got: '${MODEL}'" >&2
  usage
fi

# ── Prereqs ────────────────────────────────────────────────────────────────────
check_prereqs() {
  command -v jq   &>/dev/null || { echo "ERROR: jq not found (brew install jq)" >&2; exit 1; }
  command -v gh   &>/dev/null || { echo "ERROR: gh CLI not found" >&2; exit 1; }
  command -v curl &>/dev/null || { echo "ERROR: curl not found" >&2; exit 1; }
  if [[ "$MODEL" == "haiku" ]]; then
    command -v claude &>/dev/null || { echo "ERROR: claude CLI not found" >&2; exit 1; }
  fi
}

# ── Resolve input (file or raw string) ────────────────────────────────────────
resolve_input() {
  if [[ -f "$INPUT" ]]; then
    cat "$INPUT"
  else
    printf '%s' "$INPUT"
  fi
}

# ── Ensure roadmap file exists ─────────────────────────────────────────────────
ensure_roadmap() {
  if [[ ! -f "$ROADMAP" ]]; then
    mkdir -p "$(dirname "$ROADMAP")"
    cat > "$ROADMAP" <<'HEADER'
# Development Roadmap

<!-- Auto-managed by issue-manager.sh — append phases below -->

HEADER
    echo "Created $ROADMAP" >&2
  fi
}

# ── Detect the next available phase number ─────────────────────────────────────
next_phase_number() {
  local max=0
  local n
  while IFS= read -r line; do
    if [[ "$line" =~ ^##\ Phase\ ([0-9]+) ]]; then
      n="${BASH_REMATCH[1]}"
      if (( n > max )); then
        max=$n
      fi
    fi
  done < "$ROADMAP"
  echo $(( max + 1 ))
}

# ── Shared constraints injected into every model prompt ───────────────────────
system_constraints() {
  cat <<'CONSTRAINTS'
MANDATORY constraints — apply to every single task generated, no exceptions:
- Effort: S  (polish tasks are always small)
- Phase type: polish
- Every commit message MUST include the [L1] tag
- Commit format: <type>(<scope>): <short description> [L1]
- Allowed commit types: fix, style, chore, refactor, docs
- Scope: the UI component, page, or file affected
- Do NOT introduce new features — scope strictly to the notes provided
CONSTRAINTS
}

# ══════════════════════════════════════════════════════════════════════════════
# HAIKU MODE — delegate everything to Claude Haiku via MCP tools
# ══════════════════════════════════════════════════════════════════════════════
run_haiku() {
  local notes="$1"
  local phase_num="$2"

  # Build gh repo flag if GITHUB_REPO is set
  local repo_hint=""
  [[ -n "${GITHUB_REPO:-}" ]] && repo_hint="Target GitHub repository: ${GITHUB_REPO}"

  local prompt
  prompt=$(cat <<PROMPT
You are a developer task formatter for the Codelogiic software factory.

$(system_constraints)

## Your task

Transform the raw UI notes below into atomic developer tasks, then:
1. Use the File System MCP tool to read \`${ROADMAP}\` and confirm the current highest phase number.
2. Append a new Phase ${phase_num} block to \`${ROADMAP}\` using the exact format specified below.
3. Use the GitHub MCP tool to create one issue per task.

${repo_hint}

## Roadmap phase format

Append this exact markdown structure to \`${ROADMAP}\` — no surrounding prose, only the block:

## Phase ${phase_num} — UI Polish

| Order | Type | Commit Message | Files Affected | Goal |
|-------|------|----------------|----------------|------|
| 1 | fix | fix(scope): description [L1] | path/to/file.ext | One-line plain-English goal |

Rules for the table:
- Decompose compound notes into atomic, single-file tasks (one row each)
- Commit Message: conventional commit format, MUST end with [L1]
- Files Affected: best-guess relative path(s) based on what the note describes
- Goal: one plain-English sentence stating the desired outcome after the fix

## GitHub issue format

For each table row, create a GitHub issue:
- Title:  the commit message value without the [L1] tag
- Body:
  **Goal:** {Goal}

  **Commit:** \`{Commit Message}\`

  **Effort:** S
  **Phase:** ${phase_num} (polish)
  **Tag:** [L1]
- Label:  polish

## Rules

- Read the roadmap file first to verify phase numbering before appending.
- Do not modify any existing content in the file.
- Append exactly one blank line before the new phase block.

## Input notes

${notes}
PROMPT
)

  echo "Running Haiku (${HAIKU_MODEL}) with MCP tools..." >&2
  # --dangerously-skip-permissions required: Haiku needs write access (File System MCP)
  # and GitHub API access (GitHub MCP) without interactive prompts
  claude -p "$prompt" \
    --dangerously-skip-permissions \
    --model "$HAIKU_MODEL"
}

# ══════════════════════════════════════════════════════════════════════════════
# LOCAL MODE — Ollama generates text; bash handles all file I/O and gh calls
# ══════════════════════════════════════════════════════════════════════════════

check_ollama() {
  local http_code
  http_code=$(curl --silent --output /dev/null \
    --write-out "%{http_code}" \
    --connect-timeout "$CONNECT_TIMEOUT" \
    --max-time "$CONNECT_TIMEOUT" \
    "${OLLAMA_HOST_URL}/api/tags" 2>/dev/null) || true

  if [[ "$http_code" != "200" ]]; then
    echo "CIRCUIT_BREAKER: Ollama at ${OLLAMA_HOST_URL} returned HTTP ${http_code} (or timed out after ${CONNECT_TIMEOUT}s)" >&2
    exit 3
  fi
}

call_ollama() {
  local prompt="$1"

  local payload
  payload=$(jq -n \
    --arg model "$OLLAMA_MODEL" \
    --arg prompt "$prompt" \
    '{"model": $model, "prompt": $prompt, "stream": false, "options": {"temperature": 0.1}}')

  local response
  response=$(curl --silent --fail --show-error \
    --connect-timeout "$CONNECT_TIMEOUT" \
    --max-time "$REQUEST_TIMEOUT" \
    -X POST \
    -H "Content-Type: application/json" \
    "${OLLAMA_HOST_URL}/api/generate" \
    -d "$payload" 2>/dev/null) || {
    echo "CIRCUIT_BREAKER: Ollama request to ${OLLAMA_HOST_URL} failed or timed out" >&2
    exit 3
  }

  local output
  output=$(echo "$response" | jq -r '.response' 2>/dev/null) || {
    echo "BAD_OUTPUT: Could not parse Ollama response envelope" >&2
    exit 4
  }

  if [[ -z "$output" ]] || [[ "$output" == "null" ]]; then
    echo "BAD_OUTPUT: Ollama returned empty response" >&2
    exit 4
  fi

  echo "$output"
}

build_local_prompt() {
  local notes="$1"
  local phase_num="$2"

  cat <<PROMPT
You are a developer task formatter. Transform raw UI notes into actionable developer tasks.

$(system_constraints)

## Output rules (STRICT — no deviations)

Your entire output must consist of exactly two sections separated by the delimiter line below.
Do NOT add any explanation, preamble, or text outside these two sections.

---

SECTION 1 — Markdown roadmap phase block.

Output the phase block with this exact structure:

## Phase ${phase_num} — UI Polish

| Order | Type | Commit Message | Files Affected | Goal |
|-------|------|----------------|----------------|------|
| 1 | fix | fix(scope): description [L1] | path/to/file.ext | One-line goal |

Rules:
- One row per atomic task. Decompose compound notes.
- Commit Message column: MUST end with [L1], use conventional commit format.
- Files Affected: infer a plausible relative path from the note's context.
- Goal: one plain-English sentence describing the desired outcome.

---

Then output this exact delimiter on its own line (nothing before or after it on the line):
${JSON_DELIMITER}

---

SECTION 2 — JSON array of GitHub issues. Each object must have exactly these keys:

{
  "title": "commit message WITHOUT the [L1] tag",
  "body": "**Goal:** {Goal}\n\n**Commit:** \`{Commit Message}\`\n\n**Effort:** S\n**Phase:** ${phase_num} (polish)\n**Tag:** [L1]",
  "labels": ["polish"]
}

Rules:
- Output ONLY the raw JSON array — no markdown fences, no prose.
- One object per table row from Section 1.
- Escape any double-quotes inside string values with a backslash.

---

## UI notes to process

${notes}
PROMPT
}

# Parse the model's combined output into markdown and JSON sections.
# Outputs the parsed content (same delimiter-separated format) to stdout.
# Returns exit code 4 on parse failure (errors to stderr).
parse_local_output() {
  local raw="$1"

  # Split on the JSON_DELIMITER
  local md_section
  local json_section
  md_section=$(printf '%s' "$raw" | sed -n "1,/${JSON_DELIMITER}/{ /${JSON_DELIMITER}/d; p }")
  json_section=$(printf '%s' "$raw" | sed -n "/${JSON_DELIMITER}/,\${ /${JSON_DELIMITER}/d; p }")

  # Strip any markdown code fences the model may have wrapped around JSON
  json_section=$(printf '%s' "$json_section" | sed '/^```/d')

  # Trim leading/trailing blank lines from each section
  md_section=$(printf '%s' "$md_section" | sed '/^[[:space:]]*$/{ 1d; $d }')
  json_section=$(printf '%s' "$json_section" | sed '/^[[:space:]]*$/{ 1d; $d }')

  if [[ -z "$md_section" ]]; then
    echo "BAD_OUTPUT: No markdown section found before the delimiter '${JSON_DELIMITER}'" >&2
    return 4
  fi

  if [[ -z "$json_section" ]]; then
    echo "BAD_OUTPUT: No JSON section found after the delimiter '${JSON_DELIMITER}'" >&2
    return 4
  fi

  if ! printf '%s' "$json_section" | jq . >/dev/null 2>&1; then
    echo "BAD_OUTPUT: Content after delimiter is not valid JSON" >&2
    echo "--- Raw JSON section ---" >&2
    printf '%s\n' "$json_section" >&2
    echo "--- End raw JSON section ---" >&2
    return 4
  fi

  # Emit both sections separated by the delimiter so the caller can re-split
  printf '%s\n%s\n%s\n' "$md_section" "$JSON_DELIMITER" "$json_section"
  return 0
}

run_local() {
  local notes="$1"
  local phase_num="$2"

  check_ollama

  local prompt
  prompt=$(build_local_prompt "$notes" "$phase_num")

  echo "Calling Ollama (${OLLAMA_MODEL}) at ${OLLAMA_HOST_URL}..." >&2

  local raw parsed
  local attempt=1

  while true; do
    raw=$(call_ollama "$prompt")

    if parsed=$(parse_local_output "$raw"); then
      break
    fi

    if (( attempt >= 2 )); then
      echo "BAD_OUTPUT: Model returned unparseable output on attempt ${attempt}. Giving up." >&2
      echo "Full raw output:" >&2
      printf '%s\n' "$raw" >&2
      exit 4
    fi

    echo "WARN: Attempt ${attempt} produced bad output — retrying once..." >&2
    attempt=$(( attempt + 1 ))
  done

  # Re-split the validated parsed output
  local md_section
  local json_section
  md_section=$(printf '%s' "$parsed" | sed -n "1,/${JSON_DELIMITER}/{ /${JSON_DELIMITER}/d; p }")
  json_section=$(printf '%s' "$parsed" | sed -n "/${JSON_DELIMITER}/,\${ /${JSON_DELIMITER}/d; p }")

  # ── Append markdown phase to roadmap ──────────────────────────────────────
  {
    echo ""
    printf '%s\n' "$md_section"
  } >> "$ROADMAP"
  echo "Appended Phase ${phase_num} to ${ROADMAP}" >&2

  # ── Create GitHub issues ───────────────────────────────────────────────────
  local issue_count
  issue_count=$(printf '%s' "$json_section" | jq 'length')
  echo "Creating ${issue_count} GitHub issue(s)..." >&2

  # Build optional --repo flag
  local repo_flag=()
  [[ -n "${GITHUB_REPO:-}" ]] && repo_flag=(--repo "$GITHUB_REPO")

  local created=0
  while IFS= read -r issue; do
    local title body

    title=$(printf '%s' "$issue" | jq -r '.title')
    body=$(printf '%s' "$issue" | jq -r '.body')

    # Build --label flags from the labels array
    local label_flags=()
    while IFS= read -r lbl; do
      label_flags+=(--label "$lbl")
    done < <(printf '%s' "$issue" | jq -r '.labels[]')

    echo "  Creating: ${title}" >&2
    if gh issue create \
        --title "$title" \
        --body "$body" \
        "${label_flags[@]}" \
        "${repo_flag[@]}" 2>&1; then
      created=$(( created + 1 ))
    else
      echo "WARN: gh issue create failed for: ${title}" >&2
    fi
  done < <(printf '%s' "$json_section" | jq -c '.[]')

  echo "Done. ${created}/${issue_count} issue(s) created." >&2
}

# ── Main ────────────────────────────────────────────────────────────────────────
check_prereqs
ensure_roadmap

NOTES=$(resolve_input)
PHASE_NUM=$(next_phase_number)

echo "═══════════════════════════════════════════════════════" >&2
echo "Issue Manager" >&2
echo "Model:   ${MODEL}" >&2
echo "Roadmap: ${ROADMAP}" >&2
echo "Phase:   ${PHASE_NUM} (next available)" >&2
echo "Input:   $(printf '%s' "$NOTES" | wc -l | tr -d ' ') line(s)" >&2
[[ -n "${GITHUB_REPO:-}" ]] && echo "Repo:    ${GITHUB_REPO}" >&2
echo "═══════════════════════════════════════════════════════" >&2

case "$MODEL" in
  haiku) run_haiku "$NOTES" "$PHASE_NUM" ;;
  local) run_local "$NOTES" "$PHASE_NUM" ;;
esac
