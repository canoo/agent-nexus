#!/usr/bin/env bash
#
# test-install-cycle.sh — End-to-end install/uninstall test for NEXUS.
#
# Runs setup and teardown in an isolated fake $HOME to verify the full
# new-user experience without touching real config.
#
# Usage:
#   bash tests/test-install-cycle.sh           # from repo root
#   bash tests/test-install-cycle.sh --verbose  # show all assertions
#
set -euo pipefail

VERBOSE=0
[[ "${1:-}" == "--verbose" ]] && VERBOSE=1

# ── Locate the repo ──────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# ── Test scaffolding ─────────────────────────────────────────────────
PASS=0
FAIL=0

pass() {
    PASS=$((PASS + 1))
    [[ "$VERBOSE" -eq 1 ]] && echo "  PASS: $1"
}

fail() {
    FAIL=$((FAIL + 1))
    echo "  FAIL: $1"
}

assert_link_exists() {
    local path="$1" label="${2:-$1}"
    if [ -L "$path" ]; then pass "$label is a symlink"
    else fail "$label is not a symlink"; fi
}

assert_link_resolves() {
    local path="$1" label="${2:-$1}"
    if [ -e "$path" ]; then pass "$label resolves"
    else fail "$label is broken (dangling symlink)"; fi
}

assert_link_target() {
    local path="$1" expected="$2" label="${3:-$1}"
    local actual
    actual="$(readlink "$path" 2>/dev/null || echo "")"
    if [ "$actual" = "$expected" ]; then pass "$label -> $expected"
    else fail "$label points to '$actual', expected '$expected'"; fi
}

assert_not_exists() {
    local path="$1" label="${2:-$1}"
    if [ ! -e "$path" ] && [ ! -L "$path" ]; then pass "$label does not exist"
    else fail "$label still exists"; fi
}

assert_file_exists() {
    local path="$1" label="${2:-$1}"
    if [ -f "$path" ]; then pass "$label exists"
    else fail "$label missing"; fi
}

assert_dir_exists() {
    local path="$1" label="${2:-$1}"
    if [ -d "$path" ]; then pass "$label exists"
    else fail "$label missing"; fi
}

# ── Create isolated HOME ────────────────────────────────────────────
FAKE_HOME="$(mktemp -d)"
trap 'rm -rf "$FAKE_HOME"' EXIT

# Clone the repo into the fake HOME at an unusual path to prove
# that hardcoded paths would break.
FAKE_REPO="$FAKE_HOME/my-stuff/nexus"
mkdir -p "$(dirname "$FAKE_REPO")"
cp -r "$REPO_ROOT" "$FAKE_REPO"

# ── Test 1: Fresh install ───────────────────────────────────────────
echo ""
echo "=== Test 1: Fresh install ==="
HOME="$FAKE_HOME" bash "$FAKE_REPO/setup-nexus.sh"

echo ""
echo "Checking symlinks..."

# Core files
for pair in \
    "$FAKE_HOME/.gemini/GEMINI.md:$FAKE_REPO/core/NEXUS.md" \
    "$FAKE_HOME/.claude/CLAUDE.md:$FAKE_REPO/core/CLAUDE.md" \
    "$FAKE_HOME/.kiro/steering/nexus-orchestrator.md:$FAKE_REPO/core/kiro-nexus-steering.md"; do
    path="${pair%%:*}"
    target="${pair##*:}"
    label="$(basename "$(dirname "$path")")/$(basename "$path")"
    assert_link_exists  "$path"   "$label"
    assert_link_target  "$path"   "$target" "$label"
    assert_link_resolves "$path"  "$label"
done

# Config directories
for dir in personas tools prompts mcp-configs agent-memory; do
    path="$FAKE_HOME/.config/nexus/$dir"
    assert_link_exists   "$path"              "config/$dir"
    assert_link_target   "$path" "$FAKE_REPO/$dir" "config/$dir"
    assert_link_resolves "$path"              "config/$dir"
done

# ── Test 2: Idempotency ────────────────────────────────────────────
echo ""
echo "=== Test 2: Idempotent re-run ==="
HOME="$FAKE_HOME" bash "$FAKE_REPO/setup-nexus.sh"

# Same checks — nothing should have broken.
for pair in \
    "$FAKE_HOME/.gemini/GEMINI.md:$FAKE_REPO/core/NEXUS.md" \
    "$FAKE_HOME/.claude/CLAUDE.md:$FAKE_REPO/core/CLAUDE.md" \
    "$FAKE_HOME/.kiro/steering/nexus-orchestrator.md:$FAKE_REPO/core/kiro-nexus-steering.md"; do
    path="${pair%%:*}"
    target="${pair##*:}"
    label="idempotent $(basename "$path")"
    assert_link_exists   "$path"   "$label"
    assert_link_target   "$path"   "$target" "$label"
done

for dir in personas tools prompts mcp-configs agent-memory; do
    path="$FAKE_HOME/.config/nexus/$dir"
    assert_link_exists   "$path"              "idempotent config/$dir"
    assert_link_target   "$path" "$FAKE_REPO/$dir" "idempotent config/$dir"
done

# ── Test 3: Backup & restore of pre-existing files ──────────────────
echo ""
echo "=== Test 3: Backup and restore ==="

# Tear down first, then plant fake pre-existing configs.
HOME="$FAKE_HOME" bash "$FAKE_REPO/teardown-nexus.sh" > /dev/null 2>&1

mkdir -p "$FAKE_HOME/.gemini"
echo "user gemini config" > "$FAKE_HOME/.gemini/GEMINI.md"
mkdir -p "$FAKE_HOME/.claude"
echo "user claude config" > "$FAKE_HOME/.claude/CLAUDE.md"
mkdir -p "$FAKE_HOME/.kiro/steering"
echo "user kiro config" > "$FAKE_HOME/.kiro/steering/nexus-orchestrator.md"

# Setup should back these up.
HOME="$FAKE_HOME" bash "$FAKE_REPO/setup-nexus.sh"

assert_file_exists "$FAKE_HOME/.gemini/GEMINI.md.bak"               "GEMINI.md backup"
assert_file_exists "$FAKE_HOME/.claude/CLAUDE.md.bak"               "CLAUDE.md backup"
assert_file_exists "$FAKE_HOME/.kiro/steering/nexus-orchestrator.md.bak" "kiro backup"

# Teardown should restore them.
HOME="$FAKE_HOME" bash "$FAKE_REPO/teardown-nexus.sh"

assert_not_exists "$FAKE_HOME/.gemini/GEMINI.md.bak"    "GEMINI.md.bak removed after restore"
assert_not_exists "$FAKE_HOME/.claude/CLAUDE.md.bak"    "CLAUDE.md.bak removed after restore"

# Restored content should match the original.
if [ "$(cat "$FAKE_HOME/.gemini/GEMINI.md")" = "user gemini config" ]; then
    pass "GEMINI.md content restored"
else
    fail "GEMINI.md content not restored"
fi

if [ "$(cat "$FAKE_HOME/.claude/CLAUDE.md")" = "user claude config" ]; then
    pass "CLAUDE.md content restored"
else
    fail "CLAUDE.md content not restored"
fi

# ── Test 4: Clean teardown from fresh install ───────────────────────
echo ""
echo "=== Test 4: Clean teardown (no pre-existing config) ==="

# Wipe and re-install fresh.
rm -rf "$FAKE_HOME/.gemini" "$FAKE_HOME/.claude" "$FAKE_HOME/.kiro" "$FAKE_HOME/.config/nexus"
HOME="$FAKE_HOME" bash "$FAKE_REPO/setup-nexus.sh"
HOME="$FAKE_HOME" bash "$FAKE_REPO/teardown-nexus.sh"

assert_not_exists "$FAKE_HOME/.gemini/GEMINI.md"                       "GEMINI.md removed"
assert_not_exists "$FAKE_HOME/.claude/CLAUDE.md"                       "CLAUDE.md removed"
assert_not_exists "$FAKE_HOME/.kiro/steering/nexus-orchestrator.md"    "kiro steering removed"

for dir in personas tools prompts mcp-configs agent-memory; do
    assert_not_exists "$FAKE_HOME/.config/nexus/$dir" "config/$dir removed"
done

# Empty dirs should be cleaned up.
assert_not_exists "$FAKE_HOME/.config/nexus" "~/.config/nexus cleaned up"
assert_not_exists "$FAKE_HOME/.kiro"         "~/.kiro cleaned up"

# ── Test 5: Setup fails gracefully on incomplete repo ───────────────
echo ""
echo "=== Test 5: Incomplete repo detection ==="

BROKEN_REPO="$FAKE_HOME/broken-nexus"
mkdir -p "$BROKEN_REPO"
cp "$FAKE_REPO/setup-nexus.sh" "$BROKEN_REPO/"

if HOME="$FAKE_HOME" bash "$BROKEN_REPO/setup-nexus.sh" 2>&1; then
    fail "Setup should have failed on incomplete repo"
else
    pass "Setup correctly rejected incomplete repo"
fi

# ── Results ─────────────────────────────────────────────────────────
echo ""
echo "════════════════════════════════════════"
echo "  Results: $PASS passed, $FAIL failed"
echo "════════════════════════════════════════"

if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
