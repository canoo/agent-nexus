#!/usr/bin/env bash
set -e

echo "Setting up NEXUS Framework..."

# Define paths
NEXUS_REPO="$HOME/repos/agent-nexus"
GEMINI_DIR="$HOME/.gemini"
CONFIG_NEXUS_DIR="$HOME/.config/nexus"

# Ensure target directories exist
mkdir -p "$GEMINI_DIR"
mkdir -p "$CONFIG_NEXUS_DIR"

# Backup existing GEMINI.md if it's not our symlink
if [ -e "$GEMINI_DIR/GEMINI.md" ] && [ ! -L "$GEMINI_DIR/GEMINI.md" ]; then
    echo "Backing up existing GEMINI.md to GEMINI.md.bak"
    mv "$GEMINI_DIR/GEMINI.md" "$GEMINI_DIR/GEMINI.md.bak"
elif [ -L "$GEMINI_DIR/GEMINI.md" ]; then
    rm "$GEMINI_DIR/GEMINI.md"
fi

# Link core NEXUS logic
ln -s "$NEXUS_REPO/core/NEXUS.md" "$GEMINI_DIR/GEMINI.md"
echo "Symlinked Core NEXUS.md -> ~/.gemini/GEMINI.md"

# Link directories to ~/.config/nexus
for dir in personas tools prompts mcp-configs agent-memory; do
    if [ -L "$CONFIG_NEXUS_DIR/$dir" ]; then
        rm "$CONFIG_NEXUS_DIR/$dir"
    elif [ -e "$CONFIG_NEXUS_DIR/$dir" ]; then
        echo "Backing up existing $CONFIG_NEXUS_DIR/$dir"
        mv "$CONFIG_NEXUS_DIR/$dir" "$CONFIG_NEXUS_DIR/${dir}.bak"
    fi
    ln -s "$NEXUS_REPO/$dir" "$CONFIG_NEXUS_DIR/$dir"
    echo "Symlinked $dir -> $CONFIG_NEXUS_DIR/$dir"
done

echo "NEXUS setup complete! Local directories are now managed from ~/repos/agent-nexus."
