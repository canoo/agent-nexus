#!/bin/bash
# gemini-audit.sh — Pre-implementation full-repo audit via Gemini 3.1 Pro
#
# Usage:
#   gemini-audit.sh <roadmap-path> <project-root>
#
# Env vars required:
#   GEMINI_API_KEY   — Google AI Studio API key
#
# Exit codes:
#   0  — PASS or WARN (safe to proceed)
#   1  — FAIL (critical risks or conflicts found — block implementation)
#   2  — API error (could not reach Gemini or malformed response)
#
# Output:
#   Writes structured JSON audit report to stdout.
#   Human-readable summary written to stderr.
#   Full audit saved to .gemini-audit-report.json in project root.

set -euo pipefail

ROADMAP="${1:-}"
PROJECT_ROOT="${2:-$(pwd)}"
GEMINI_MODEL="gemini-3.1-pro-preview-customtools"
API_BASE="https://generativelanguage.googleapis.com/v1beta/models"
MAX_CONTEXT_CHARS=3200000  # ~800K tokens safety margin (1M token window)
REPORT_FILE="${PROJECT_ROOT}/.gemini-audit-report.json"

# ── Validation ────────────────────────────────────────────────────────────────
if [ -z "$ROADMAP" ]; then
  echo "Usage: gemini-audit.sh <roadmap-path> <project-root>" >&2
  exit 2
fi

if [ ! -f "$ROADMAP" ]; then
  echo "ERROR: Roadmap not found at $ROADMAP" >&2
  exit 2
fi

if [ -z "${GEMINI_API_KEY:-}" ]; then
  echo "ERROR: GEMINI_API_KEY is not set" >&2
  exit 2
fi

if ! command -v jq &>/dev/null; then
  echo "ERROR: jq is required (brew install jq)" >&2
  exit 2
fi

# ── Context assembly ──────────────────────────────────────────────────────────
# Strategy: roadmap + configs + all source files, pruned to stay under token limit.
# Exclusion order if too large: test files → docs → source (never exclude roadmap).

assemble_context() {
  local context=""
  local char_count=0

  append_file() {
    local path="$1"
    local label="$2"
    if [ -f "$path" ]; then
      local content
      content=$(cat "$path" 2>/dev/null || true)
      local addition="
=== FILE: ${label} ===
${content}
"
      local addition_len=${#addition}
      if (( char_count + addition_len < MAX_CONTEXT_CHARS )); then
        context+="$addition"
        char_count=$(( char_count + addition_len ))
        return 0
      else
        echo "WARN: Skipping $label — context budget exhausted" >&2
        return 1
      fi
    fi
  }

  # Always include: roadmap, CLAUDE.md, package.json / pyproject.toml, config files
  append_file "$ROADMAP" "docs/dev-roadmap.md"
  append_file "${PROJECT_ROOT}/CLAUDE.md" "CLAUDE.md"
  append_file "${PROJECT_ROOT}/package.json" "package.json"
  append_file "${PROJECT_ROOT}/pyproject.toml" "pyproject.toml"
  append_file "${PROJECT_ROOT}/requirements.txt" "requirements.txt"
  append_file "${PROJECT_ROOT}/tsconfig.json" "tsconfig.json"
  append_file "${PROJECT_ROOT}/vite.config.ts" "vite.config.ts"
  append_file "${PROJECT_ROOT}/vite.config.js" "vite.config.js"
  append_file "${PROJECT_ROOT}/astro.config.mjs" "astro.config.mjs"
  append_file "${PROJECT_ROOT}/tailwind.config.js" "tailwind.config.js"
  append_file "${PROJECT_ROOT}/tailwind.config.ts" "tailwind.config.ts"
  append_file "${PROJECT_ROOT}/docs/architecture-log.md" "docs/architecture-log.md"
  append_file "${PROJECT_ROOT}/docs/design.md" "docs/design.md"

  # Source files (highest priority after configs)
  for dir in src app lib components pages api; do
    if [ -d "${PROJECT_ROOT}/${dir}" ]; then
      while IFS= read -r -d '' file; do
        rel="${file#${PROJECT_ROOT}/}"
        append_file "$file" "$rel" || break 2
      done < <(find "${PROJECT_ROOT}/${dir}" \
        -type f \
        \( -name "*.ts" -o -name "*.tsx" -o -name "*.js" -o -name "*.jsx" \
           -o -name "*.astro" -o -name "*.vue" -o -name "*.svelte" \
           -o -name "*.py" -o -name "*.go" -o -name "*.rs" \
           -o -name "*.css" -o -name "*.scss" \) \
        -not -path "*/node_modules/*" \
        -not -path "*/.git/*" \
        -not -path "*/dist/*" \
        -not -path "*/__pycache__/*" \
        -print0 2>/dev/null)
    fi
  done

  # Test files (lower priority — drop first if context too large)
  for dir in tests test __tests__ spec; do
    if [ -d "${PROJECT_ROOT}/${dir}" ]; then
      while IFS= read -r -d '' file; do
        rel="${file#${PROJECT_ROOT}/}"
        append_file "$file" "$rel" || break 2
      done < <(find "${PROJECT_ROOT}/${dir}" \
        -type f \( -name "*.ts" -o -name "*.js" -o -name "*.py" \) \
        -not -path "*/node_modules/*" \
        -print0 2>/dev/null)
    fi
  done

  echo "$context"
}

# ── Build Gemini request payload ──────────────────────────────────────────────
build_payload() {
  local context="$1"

  local system_prompt
  system_prompt="You are a senior code auditor performing a pre-implementation review. You have been given a development roadmap and the current codebase. Your job is to identify risks BEFORE implementation begins.

Return ONLY valid JSON — no markdown, no prose, no code fences. Return exactly this structure:

{
  \"verdict\": \"PASS\" | \"WARN\" | \"FAIL\",
  \"risks\": [
    {
      \"severity\": \"critical\" | \"high\" | \"medium\" | \"low\",
      \"phase\": <integer>,
      \"commit\": \"<commit message>\",
      \"description\": \"<what could go wrong>\",
      \"recommendation\": \"<what to change in the roadmap>\"
    }
  ],
  \"missing_considerations\": [
    \"<something the roadmap does not account for>\"
  ],
  \"conflicts\": [
    {
      \"roadmap_commit\": \"<commit message>\",
      \"existing_file\": \"<path>\",
      \"conflict_type\": \"overwrites existing logic\" | \"breaks dependency\" | \"duplicates existing code\",
      \"description\": \"<details>\"
    }
  ],
  \"summary\": \"<2-3 sentence overall assessment>\"
}

Verdict rules:
- PASS: zero critical risks, zero conflicts
- WARN: no critical risks, but has high/medium risks or missing considerations
- FAIL: any critical-severity risk OR any conflict of type 'breaks dependency'

Here is the codebase and roadmap:

"

  # jq handles all JSON escaping natively — no python3 dependency, safe for any content
  jq -n \
    --arg text "${system_prompt}${context}" \
    '{
      contents: [{ parts: [{ text: $text }] }],
      generationConfig: {
        thinkingConfig: { thinkingLevel: "high" },
        temperature: 0.1,
        responseMimeType: "application/json"
      }
    }'
}

# ── Call Gemini API ───────────────────────────────────────────────────────────
call_gemini() {
  local payload="$1"
  local response

  response=$(curl --silent --fail --show-error \
    --max-time 120 \
    -X POST \
    -H "Content-Type: application/json" \
    "https://generativelanguage.googleapis.com/v1beta/models/${GEMINI_MODEL}:generateContent?key=${GEMINI_API_KEY}" \
    -d "$payload" 2>&1) || {
    echo "ERROR: Gemini API request failed: $response" >&2
    exit 2
  }

  # Extract the text content from Gemini's response envelope.
  # Thinking models return multiple parts: one thoughtSignature part and one text part.
  # Select the first part that has a non-null .text field.
  local audit_json
  audit_json=$(echo "$response" | jq -r '
    .candidates[0].content.parts
    | map(select(.text != null))
    | first
    | .text
  ' 2>/dev/null) || {
    echo "ERROR: Could not parse Gemini response. Raw response:" >&2
    echo "$response" >&2
    exit 2
  }

  if [ -z "$audit_json" ] || [ "$audit_json" = "null" ]; then
    echo "ERROR: Gemini returned empty audit content. Raw response:" >&2
    echo "$response" >&2
    exit 2
  fi

  # Validate it's parseable JSON
  echo "$audit_json" | jq . > /dev/null 2>&1 || {
    echo "ERROR: Gemini response is not valid JSON: $audit_json" >&2
    exit 2
  }

  echo "$audit_json"
}

# ── Main ──────────────────────────────────────────────────────────────────────
echo "Assembling codebase context..." >&2
CONTEXT=$(assemble_context)
CHAR_COUNT=${#CONTEXT}
echo "Context assembled: ${CHAR_COUNT} chars (~$((CHAR_COUNT / 4)) tokens)" >&2

echo "Calling Gemini ${GEMINI_MODEL}..." >&2
PAYLOAD=$(build_payload "$CONTEXT")
AUDIT_JSON=$(call_gemini "$PAYLOAD")

# Save full report
echo "$AUDIT_JSON" | jq . > "$REPORT_FILE"
echo "Full audit report saved to $REPORT_FILE" >&2

# Extract verdict
VERDICT=$(echo "$AUDIT_JSON" | jq -r '.verdict')
SUMMARY=$(echo "$AUDIT_JSON" | jq -r '.summary')
CRITICAL_COUNT=$(echo "$AUDIT_JSON" | jq '[.risks[] | select(.severity=="critical")] | length')
HIGH_COUNT=$(echo "$AUDIT_JSON" | jq '[.risks[] | select(.severity=="high")] | length')
CONFLICT_COUNT=$(echo "$AUDIT_JSON" | jq '.conflicts | length')

# Human-readable summary to stderr
echo "" >&2
echo "═══════════════════════════════════════════════════════" >&2
echo "GEMINI AUDIT RESULT: ${VERDICT}" >&2
echo "═══════════════════════════════════════════════════════" >&2
echo "Summary:   ${SUMMARY}" >&2
echo "Critical:  ${CRITICAL_COUNT}  High: ${HIGH_COUNT}  Conflicts: ${CONFLICT_COUNT}" >&2
echo "═══════════════════════════════════════════════════════" >&2

if [ "$VERDICT" = "FAIL" ]; then
  echo "" >&2
  echo "BLOCKED — Critical risks or dependency conflicts found." >&2
  echo "Review $REPORT_FILE and revise docs/dev-roadmap.md before proceeding." >&2
  echo "" >&2
  echo "Critical risks:" >&2
  echo "$AUDIT_JSON" | jq -r '.risks[] | select(.severity=="critical") | "  Phase \(.phase): \(.commit)\n    → \(.description)\n    Fix: \(.recommendation)"' >&2
  echo "" >&2
  echo "Conflicts:" >&2
  echo "$AUDIT_JSON" | jq -r '.conflicts[] | "  \(.existing_file): \(.conflict_type)\n    → \(.description)"' >&2
fi

# Emit structured JSON to stdout for the orchestrator to consume
echo "$AUDIT_JSON"

# Exit code
case "$VERDICT" in
  PASS|WARN) exit 0 ;;
  FAIL)      exit 1 ;;
  *)         echo "ERROR: Unexpected verdict: $VERDICT" >&2; exit 2 ;;
esac
