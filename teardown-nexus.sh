#!/usr/bin/env bash
set -e

echo "Initiating NEXUS Framework Teardown..."

GEMINI_DIR="$HOME/.gemini"
CLAUDE_DIR="$HOME/.claude"
KIRO_STEERING_DIR="$HOME/.kiro/steering"
CONFIG_NEXUS_DIR="$HOME/.config/nexus"

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
echo "Unlinking core files..."
safe_unlink "$GEMINI_DIR/GEMINI.md"
safe_unlink "$CLAUDE_DIR/CLAUDE.md"
safe_unlink "$KIRO_STEERING_DIR/nexus-orchestrator.md"

echo ""
echo "Unlinking config directories..."
for dir in personas tools prompts mcp-configs agent-memory; do
    safe_unlink "$CONFIG_NEXUS_DIR/$dir"
done

# Remove nexus-ollama from Kiro MCP config.
KIRO_MCP_FILE="$HOME/.kiro/settings/mcp.json"
echo ""
echo "Cleaning up Kiro MCP config..."
if [ -f "$KIRO_MCP_FILE" ] && grep -q '"nexus-ollama"' "$KIRO_MCP_FILE" 2>/dev/null; then
    # Remove the nexus-ollama key, preserving other servers.
    node -e "
      const fs = require('fs');
      const path = '$KIRO_MCP_FILE';
      const config = JSON.parse(fs.readFileSync(path, 'utf8'));
      delete config.mcpServers['nexus-ollama'];
      if (Object.keys(config.mcpServers).length === 0) {
        fs.unlinkSync(path);
        console.log('  Removed: $KIRO_MCP_FILE (no servers remaining)');
      } else {
        fs.writeFileSync(path, JSON.stringify(config, null, 2) + '\n');
        console.log('  Removed nexus-ollama from $KIRO_MCP_FILE (other servers preserved)');
      }
    "
elif [ -f "$KIRO_MCP_FILE" ]; then
    echo "  No nexus-ollama entry found (skipped)"
else
    echo "  No Kiro MCP config found (skipped)"
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
for d in "$CONFIG_NEXUS_DIR" "$KIRO_STEERING_DIR" "$HOME/.kiro/settings" "$HOME/.kiro"; do
    if [ -d "$d" ] && [ -z "$(ls -A "$d")" ]; then
        rmdir "$d"
        echo "  Removed empty directory: $d"
    fi
done

echo ""
echo "Teardown complete. System restored to pre-NEXUS state."
