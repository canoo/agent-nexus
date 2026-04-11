#!/bin/bash
# ollama-delegate.sh — Route L1 tasks to local Ollama node
#
# Usage:
#   ollama-delegate.sh <task-type> <context-file>
#
# Task types:
#   commit-msg      Generate a conventional commit message from a git diff
#   boilerplate     Generate component/route/model boilerplate
#   test-scaffold   Generate test file scaffolding (describe blocks, test names)
#   lint-fix        Suggest fixes for lint errors
#   logic-refactor  Suggest refactor for a given code block
#
# Env vars:
#   OLLAMA_HOST_URL   Base URL of Ollama server (default: http://localhost:11434)
#                     Set to your PC's address, e.g. http://192.168.1.50:11434
#
# Exit codes:
#   0  — Success, output written to stdout
#   3  — CIRCUIT_BREAKER: Ollama unreachable or timed out (do NOT fallback silently)
#   4  — BAD_OUTPUT: Model returned empty or malformed response (retry once, then exit 4)
#   1  — Usage error

set -euo pipefail

TASK_TYPE="${1:-}"
CONTEXT_FILE="${2:-}"
OLLAMA_HOST_URL="${OLLAMA_HOST_URL:-http://localhost:11434}"
CONNECT_TIMEOUT=5    # seconds to wait for initial connection
REQUEST_TIMEOUT=120  # seconds for full response generation

# ── Validation ────────────────────────────────────────────────────────────────
if [ -z "$TASK_TYPE" ] || [ -z "$CONTEXT_FILE" ]; then
  echo "Usage: ollama-delegate.sh <task-type> <context-file>" >&2
  echo "Task types: commit-msg, boilerplate, test-scaffold, lint-fix, logic-refactor" >&2
  exit 1
fi

if [ ! -f "$CONTEXT_FILE" ]; then
  echo "ERROR: Context file not found: $CONTEXT_FILE" >&2
  exit 1
fi

# ── Model routing table ───────────────────────────────────────────────────────
# SINGLE-NODE MODE: All tasks → gemma4:e4b (8GB VRAM constraint).
# To add a second node (Mac Mini, second GPU), pattern:
#   Add OLLAMA_HOST_URL_NODE2 env var, then route specific task types
#   to NODE2 by swapping OLLAMA_HOST_URL before the curl call.
#
# Future routing example (do not enable until second node is available):
#   logic-refactor → NODE2 with a larger model (e.g., qwen2.5-coder:32b)
#   commit-msg     → NODE1 gemma4:e4b  (fast, small context)

get_model() {
  case "$TASK_TYPE" in
    commit-msg)      echo "gemma4:e4b" ;;
    boilerplate)     echo "gemma4:e4b" ;;
    test-scaffold)   echo "gemma4:e4b" ;;
    lint-fix)        echo "gemma4:e4b" ;;
    logic-refactor)  echo "gemma4:e4b" ;;
    *)
      echo "ERROR: Unknown task type: $TASK_TYPE" >&2
      echo "Valid types: commit-msg, boilerplate, test-scaffold, lint-fix, logic-refactor" >&2
      exit 1
      ;;
  esac
}

# ── Prompt templates ──────────────────────────────────────────────────────────
build_prompt() {
  local context
  context=$(cat "$CONTEXT_FILE")

  case "$TASK_TYPE" in
    commit-msg)
      cat <<PROMPT
You are a git commit message writer. Given the following git diff, write a single conventional commit message.

Rules:
- Format: <type>(<optional-scope>): <short description>
- Types: feat, fix, refactor, chore, docs, style, test, perf
- Under 72 characters total
- Lowercase
- No period at the end
- Return ONLY the commit message — no explanation, no alternatives

Git diff:
${context}
PROMPT
      ;;

    boilerplate)
      cat <<PROMPT
You are a code boilerplate generator. Generate a clean, minimal boilerplate file based on the specification below.

Rules:
- Return ONLY the file content — no explanation, no markdown fences
- Follow the framework and patterns shown in the example/context
- No placeholder comments like "// TODO: implement"
- Production-quality structure, empty function bodies are fine

Specification:
${context}
PROMPT
      ;;

    test-scaffold)
      cat <<PROMPT
You are a test scaffolding generator. Given the source file below, generate a test file scaffold.

Rules:
- Return ONLY the test file content — no explanation, no markdown fences
- Include describe blocks and test names that reflect real behavior
- Leave test bodies empty (no implementation) — just the structure
- Use the framework implied by the file extension and imports (jest/vitest/pytest/etc.)
- Import the module under test correctly

Source file:
${context}
PROMPT
      ;;

    lint-fix)
      cat <<PROMPT
You are a lint error fixer. Given the file content and lint errors below, return the corrected file.

Rules:
- Return ONLY the corrected file content — no explanation, no markdown fences
- Fix ONLY the reported lint errors — do not refactor or reformat unrelated code
- Preserve all logic and behavior exactly

File and lint errors:
${context}
PROMPT
      ;;

    logic-refactor)
      cat <<PROMPT
You are a code refactoring assistant. Refactor the code below to improve clarity and maintainability.

Rules:
- Return ONLY the refactored code — no explanation, no markdown fences
- Preserve all existing behavior exactly
- Do not change function signatures or public interfaces
- Do not add new features or error handling beyond what exists

Code to refactor:
${context}
PROMPT
      ;;
  esac
}

# ── Circuit breaker: check Ollama reachability ────────────────────────────────
check_ollama() {
  local health_url="${OLLAMA_HOST_URL}/api/tags"
  local http_code

  http_code=$(curl --silent --output /dev/null \
    --write-out "%{http_code}" \
    --connect-timeout "$CONNECT_TIMEOUT" \
    --max-time "$CONNECT_TIMEOUT" \
    "$health_url" 2>/dev/null) || true

  if [ "$http_code" != "200" ]; then
    echo "CIRCUIT_BREAKER: Ollama at ${OLLAMA_HOST_URL} returned HTTP ${http_code} (or timed out after ${CONNECT_TIMEOUT}s)" >&2
    exit 3
  fi
}

# ── Call Ollama ───────────────────────────────────────────────────────────────
call_ollama() {
  local model="$1"
  local prompt="$2"

  local payload
  payload=$(jq -n \
    --arg model "$model" \
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
    echo "BAD_OUTPUT: Could not parse Ollama response" >&2
    exit 4
  }

  if [ -z "$output" ] || [ "$output" = "null" ]; then
    echo "BAD_OUTPUT: Ollama returned empty response" >&2
    exit 4
  fi

  echo "$output"
}

# ── Main ──────────────────────────────────────────────────────────────────────
check_ollama

MODEL=$(get_model)
PROMPT=$(build_prompt)

echo "Delegating ${TASK_TYPE} to ${MODEL} at ${OLLAMA_HOST_URL}..." >&2

RESULT=$(call_ollama "$MODEL" "$PROMPT")

# Strip any accidental markdown fences the model added
RESULT=$(echo "$RESULT" | sed '/^```/d')

echo "$RESULT"
