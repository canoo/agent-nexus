#!/usr/bin/env bash
set -e

# Derive repo root from this script's location, not a hardcoded path.
NEXUS_REPO="$(cd "$(dirname "$0")" && pwd)"

echo "Setting up NEXUS Framework from: $NEXUS_REPO"

# Validate that the repo looks correct before touching anything.
for required in core/NEXUS.md core/CLAUDE.md core/kiro-nexus-steering.md personas tools prompts mcp-configs agent-memory; do
    if [ ! -e "$NEXUS_REPO/$required" ]; then
        echo "ERROR: Missing required path: $NEXUS_REPO/$required"
        echo "Is this a complete agent-nexus clone? Aborting."
        exit 1
    fi
done

# Define targets
GEMINI_DIR="$HOME/.gemini"
CLAUDE_DIR="$HOME/.claude"
KIRO_STEERING_DIR="$HOME/.kiro/steering"
CONFIG_NEXUS_DIR="$HOME/.config/nexus"

# Helper: create a symlink with backup logic.
# Usage: safe_link <source> <target>
safe_link() {
    local source="$1"
    local target="$2"
    local target_dir
    target_dir="$(dirname "$target")"

    mkdir -p "$target_dir"

    if [ -L "$target" ]; then
        local existing_target
        existing_target="$(readlink "$target")"
        if [ "$existing_target" = "$source" ]; then
            echo "  Already linked: $target -> $source (skipped)"
            return 0
        fi
        rm "$target"
        echo "  Removed stale symlink: $target (was -> $existing_target)"
    elif [ -e "$target" ]; then
        mv "$target" "${target}.bak"
        echo "  Backed up: $target -> ${target}.bak"
    fi

    ln -s "$source" "$target"
    echo "  Linked: $target -> $source"
}

# Verify a symlink actually resolves after creation.
verify_link() {
    local target="$1"
    local label="$2"
    if [ ! -e "$target" ]; then
        echo "ERROR: $label symlink is broken — $target does not resolve."
        echo "This likely means the repo was moved after setup. Re-run setup-nexus.sh from the new location."
        exit 1
    fi
}

echo ""
echo "Linking core files..."
safe_link "$NEXUS_REPO/core/NEXUS.md"               "$GEMINI_DIR/GEMINI.md"
safe_link "$NEXUS_REPO/core/CLAUDE.md"               "$CLAUDE_DIR/CLAUDE.md"
safe_link "$NEXUS_REPO/core/kiro-nexus-steering.md"  "$KIRO_STEERING_DIR/nexus-orchestrator.md"

echo ""
echo "Linking config directories..."
for dir in personas tools prompts mcp-configs agent-memory; do
    safe_link "$NEXUS_REPO/$dir" "$CONFIG_NEXUS_DIR/$dir"
done

# Post-setup validation: make sure every symlink actually resolves.
echo ""
echo "Verifying all symlinks..."
ERRORS=0
for link in \
    "$GEMINI_DIR/GEMINI.md" \
    "$CLAUDE_DIR/CLAUDE.md" \
    "$KIRO_STEERING_DIR/nexus-orchestrator.md" \
    "$CONFIG_NEXUS_DIR/personas" \
    "$CONFIG_NEXUS_DIR/tools" \
    "$CONFIG_NEXUS_DIR/prompts" \
    "$CONFIG_NEXUS_DIR/mcp-configs" \
    "$CONFIG_NEXUS_DIR/agent-memory"; do
    if [ ! -e "$link" ]; then
        echo "  BROKEN: $link -> $(readlink "$link")"
        ERRORS=$((ERRORS + 1))
    else
        echo "  OK: $link"
    fi
done

echo ""
if [ "$ERRORS" -gt 0 ]; then
    echo "Setup completed with $ERRORS broken symlink(s). Check the paths above."
    exit 1
fi
echo "NEXUS setup complete. All symlinks verified."
