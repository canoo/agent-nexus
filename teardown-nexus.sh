#!/usr/bin/env bash
set -e

echo "Initiating NEXUS Framework Teardown..."

GEMINI_DIR="$HOME/.gemini"
CONFIG_NEXUS_DIR="$HOME/.config/nexus"

# 1. Restore the core GEMINI.md file
if [ -L "$GEMINI_DIR/GEMINI.md" ]; then
    rm "$GEMINI_DIR/GEMINI.md"
    echo "Severed symlink: $GEMINI_DIR/GEMINI.md"
fi

if [ -e "$GEMINI_DIR/GEMINI.md.bak" ]; then
    mv "$GEMINI_DIR/GEMINI.md.bak" "$GEMINI_DIR/GEMINI.md"
    echo "Restored original GEMINI.md from backup."
fi

# 2. Restore Claude Code CLAUDE.md
CLAUDE_DIR="$HOME/.claude"
if [ -L "$CLAUDE_DIR/CLAUDE.md" ]; then
    rm "$CLAUDE_DIR/CLAUDE.md"
    echo "Severed symlink: $CLAUDE_DIR/CLAUDE.md"
fi
if [ -e "$CLAUDE_DIR/CLAUDE.md.bak" ]; then
    mv "$CLAUDE_DIR/CLAUDE.md.bak" "$CLAUDE_DIR/CLAUDE.md"
    echo "Restored original CLAUDE.md from backup."
fi

# 3. Restore Kiro CLI nexus-orchestrator.md
KIRO_STEERING_DIR="$HOME/.kiro/steering"
if [ -L "$KIRO_STEERING_DIR/nexus-orchestrator.md" ]; then
    rm "$KIRO_STEERING_DIR/nexus-orchestrator.md"
    echo "Severed symlink: $KIRO_STEERING_DIR/nexus-orchestrator.md"
fi
if [ -e "$KIRO_STEERING_DIR/nexus-orchestrator.md.bak" ]; then
    mv "$KIRO_STEERING_DIR/nexus-orchestrator.md.bak" "$KIRO_STEERING_DIR/nexus-orchestrator.md"
    echo "Restored original nexus-orchestrator.md from backup."
fi

# 4. Restore the configuration directories
for dir in personas tools prompts mcp-configs agent-memory; do
    TARGET="$CONFIG_NEXUS_DIR/$dir"

    if [ -L "$TARGET" ]; then
        rm "$TARGET"
        echo "Severed symlink: $TARGET"
    fi

    if [ -e "${TARGET}.bak" ]; then
        mv "${TARGET}.bak" "$TARGET"
        echo "Restored original $dir from backup."
    fi
done

echo "Teardown complete! The system has been cleanly restored to its pre-NEXUS state."