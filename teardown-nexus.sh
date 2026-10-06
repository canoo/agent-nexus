#!/usr/bin/env bash
set -e

echo "Initiating NEXUS Framework Teardown..."

GEMINI_DIR="$HOME/.gemini"
CLAUDE_DIR="$HOME/.claude"
KIRO_STEERING_DIR="$HOME/.kiro/steering"
CONFIG_NEXUS_DIR="$HOME/.config/nexus"

# Repo root, derived from this script's location (for scripts/mcp-remove.js).
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Helper: remove a symlink and restore its backup if one exists.
# Usage: safe_unlink <target>
safe_unlink() {
    local target="$1"

    if [ -L "$target" ]; then
        rm "$target"
        echo "  Removed symlink: $target"
    elif [ -e "$target" ]; then
        echo "  Skipped (not a symlink): $target"
        return 0
    else
        echo "  Skipped (does not exist): $target"
    fi

    if [ -e "${target}.bak" ]; then
        mv "${target}.bak" "$target"
        echo "  Restored backup: ${target}.bak -> $target"
    fi
}

echo ""
# Canonical MCP config removal. Both removals (Kiro, Gemini) go through
# scripts/mcp-remove.js so every writer shares one fail-closed,
# full-document-preserving implementation -- see the spec comment in that file.
# Prints one of: removed | removed-file | unchanged. Exits non-zero on refusal.
# Usage: nexus_mcp_remove <config-file>
nexus_mcp_remove() {
    node "$SCRIPT_DIR/scripts/mcp-remove.js" "$1"
}

echo "Unlinking core files..."
safe_unlink "$GEMINI_DIR/GEMINI.md"
safe_unlink "$CLAUDE_DIR/CLAUDE.md"
safe_unlink "$KIRO_STEERING_DIR/nexus-orchestrator.md"

echo ""
echo "Unlinking config directories..."
for dir in core personas tools prompts mcp-configs agent-memory; do
    safe_unlink "$CONFIG_NEXUS_DIR/$dir"
done

# Remove nexus-ollama from Claude Code (user scope).
echo ""
echo "Cleaning up Claude Code MCP config..."
if ! command -v claude &>/dev/null; then
    echo "  SKIPPED: claude CLI not found."
elif claude mcp get nexus-ollama &>/dev/null; then
    if claude mcp remove --scope user nexus-ollama >/dev/null; then
        echo "  Removed nexus-ollama from Claude Code"
    else
        echo "  ERROR: Failed to remove nexus-ollama from Claude Code"
    fi
else
    echo "  No nexus-ollama entry found (skipped)"
fi

# Remove nexus-ollama from Kiro MCP config.
KIRO_MCP_FILE="$HOME/.kiro/settings/mcp.json"
echo ""
echo "Cleaning up Kiro MCP config..."
if [ ! -f "$KIRO_MCP_FILE" ]; then
    echo "  No Kiro MCP config found (skipped)"
elif remove_status=$(nexus_mcp_remove "$KIRO_MCP_FILE" 2>&1); then
    case "$remove_status" in
        removed)      echo "  Removed nexus-ollama from $KIRO_MCP_FILE (other servers preserved)" ;;
        removed-file) echo "  Removed: $KIRO_MCP_FILE (no servers remaining)" ;;
        unchanged)    echo "  No nexus-ollama entry found (skipped)" ;;
        *)            echo "  Updated: $KIRO_MCP_FILE" ;;
    esac
else
    echo "  ERROR: Failed to update $KIRO_MCP_FILE (left untouched)"
    printf '%s\n' "$remove_status" | sed 's/^/  /'
fi

# Remove nexus-ollama from Antigravity CLI (Gemini) MCP config.
GEMINI_MCP_FILE="$GEMINI_DIR/config/mcp_config.json"
echo ""
echo "Cleaning up Antigravity CLI (Gemini) MCP config..."
if [ ! -f "$GEMINI_MCP_FILE" ]; then
    echo "  No Antigravity CLI (Gemini) MCP config found (skipped)"
elif remove_status=$(nexus_mcp_remove "$GEMINI_MCP_FILE" 2>&1); then
    case "$remove_status" in
        removed)      echo "  Removed nexus-ollama from $GEMINI_MCP_FILE (other servers preserved)" ;;
        removed-file) echo "  Removed: $GEMINI_MCP_FILE (no servers remaining)" ;;
        unchanged)    echo "  No nexus-ollama entry found (skipped)" ;;
        *)            echo "  Updated: $GEMINI_MCP_FILE" ;;
    esac
else
    echo "  ERROR: Failed to update $GEMINI_MCP_FILE (left untouched)"
    printf '%s\n' "$remove_status" | sed 's/^/  /'
fi

# Remove the TUI binary.
NEXUS_BIN="$HOME/.local/bin/nexus"
echo ""
echo "Removing NEXUS TUI binary..."
if [ -f "$NEXUS_BIN" ]; then
    rm "$NEXUS_BIN"
    echo "  Removed: $NEXUS_BIN"
else
    echo "  Skipped (does not exist): $NEXUS_BIN"
fi

# Clean up empty directories that setup created.
echo ""
echo "Cleaning up empty directories..."
for d in "$CONFIG_NEXUS_DIR" "$KIRO_STEERING_DIR" "$HOME/.kiro/settings" "$HOME/.kiro" "$GEMINI_DIR/config" "$GEMINI_DIR"; do
    if [ -d "$d" ] && [ -z "$(ls -A "$d")" ]; then
        rmdir "$d"
        echo "  Removed empty directory: $d"
    fi
done

echo ""
echo "Teardown complete. System restored to pre-NEXUS state."
