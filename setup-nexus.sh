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

# node >= 22.13 is required: the MCP config merges below run `node -e`, and the
# nexus-ollama MCP server itself uses node:sqlite. Fail early with a clear
# message instead of dying mid-script on a missing node.
require_node() {
    command -v node &>/dev/null || {
        echo "ERROR: Node.js 22.13+ is required (the nexus-ollama MCP server uses node:sqlite)."
        echo "Install it from https://nodejs.org and re-run setup."
        exit 1
    }
    local ver have want
    ver="$(node --version | sed 's/^v//')"
    want="22.13.0"
    have="$(printf '%s\n%s\n' "$want" "$ver" | sort -V | head -n1)"
    if [ "$have" != "$want" ]; then
        echo "ERROR: Node.js 22.13+ is required, found v$ver."
        echo "Upgrade from https://nodejs.org and re-run setup."
        exit 1
    fi
    command -v npm &>/dev/null || {
        echo "ERROR: npm is required but was not found alongside node."
        echo "Reinstall Node.js from https://nodejs.org and re-run setup."
        exit 1
    }
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

require_node

echo ""
echo "Linking core files..."
safe_link "$NEXUS_REPO/core/NEXUS.md"               "$GEMINI_DIR/GEMINI.md"
safe_link "$NEXUS_REPO/core/CLAUDE.md"               "$CLAUDE_DIR/CLAUDE.md"
safe_link "$NEXUS_REPO/core/kiro-nexus-steering.md"  "$KIRO_STEERING_DIR/nexus-orchestrator.md"

echo ""
echo "Linking config directories..."
# core is linked because core/CLAUDE.md imports ~/.config/nexus/core/NEXUS.md.
for dir in core personas tools prompts mcp-configs agent-memory; do
    safe_link "$NEXUS_REPO/$dir" "$CONFIG_NEXUS_DIR/$dir"
done

ERRORS=0

# Install the MCP server's npm dependencies. node_modules is not committed, so
# without this step server.mjs fails to import @modelcontextprotocol/sdk.
# Skip when node_modules is already in sync with the lockfile.
MCP_DIR="$NEXUS_REPO/tools/mcp"
echo ""
echo "Installing MCP server dependencies..."
if ! command -v npm &>/dev/null; then
    echo "  SKIPPED: npm not found. Install Node.js 22.13+ to use the nexus-ollama MCP server."
elif [ -f "$MCP_DIR/node_modules/.package-lock.json" ] && \
     [ ! "$MCP_DIR/package-lock.json" -nt "$MCP_DIR/node_modules/.package-lock.json" ]; then
    echo "  Already installed: $MCP_DIR/node_modules (skipped)"
elif (cd "$MCP_DIR" && npm ci --omit=dev --no-audit --no-fund --loglevel=error); then
    echo "  Installed: $MCP_DIR/node_modules"
else
    echo "  WARNING: npm ci failed in $MCP_DIR. The nexus-ollama MCP server will not start until it succeeds."
fi

# Configure MCP server for Kiro CLI.
# Kiro reads MCP config from ~/.kiro/settings/mcp.json (not symlinked — it's
# a standalone JSON file that references the server script via the symlinked path).
KIRO_SETTINGS_DIR="$HOME/.kiro/settings"
KIRO_MCP_FILE="$KIRO_SETTINGS_DIR/mcp.json"
MCP_SERVER_PATH="$CONFIG_NEXUS_DIR/tools/mcp/server.mjs"

echo ""
echo "Configuring Kiro MCP..."
mkdir -p "$KIRO_SETTINGS_DIR"

if [ -f "$KIRO_MCP_FILE" ] && grep -q '"nexus-ollama"' "$KIRO_MCP_FILE" 2>/dev/null; then
    echo "  Already configured: nexus-ollama in $KIRO_MCP_FILE (skipped)"
else
    # Merge nexus-ollama into existing config (or create new).
    # Uses Node since it's already a dependency for the MCP server.
    node -e "
      const fs = require('fs');
      const path = '$KIRO_MCP_FILE';
      let config = { mcpServers: {} };
      let parseFailed = false;
      if (fs.existsSync(path)) {
        const raw = fs.readFileSync(path, 'utf8').trim();
        if (raw.length > 0) {
          try {
            config = JSON.parse(raw);
          } catch (err) {
            parseFailed = true;
            console.error('  ERROR: Failed to parse ' + path + ': ' + err.message);
          }
        }
      }
      if (!parseFailed) {
        if (!config || typeof config !== 'object' || Array.isArray(config)) config = {};
        if (!config.mcpServers || typeof config.mcpServers !== 'object' || Array.isArray(config.mcpServers)) config.mcpServers = {};
        config.mcpServers['nexus-ollama'] = {
          command: 'node',
          args: ['$MCP_SERVER_PATH']
        };
        try {
          fs.writeFileSync(path, JSON.stringify(config, null, 2) + '\n');
        } catch (err) {
          try {
            fs.chmodSync(path, 0o644);
            fs.writeFileSync(path, JSON.stringify(config, null, 2) + '\n');
          } catch (err2) {
            console.error('  ERROR: Failed to write ' + path + ': ' + err2.message);
          }
        }
      }
    "
    if grep -q '"nexus-ollama"' "$KIRO_MCP_FILE" 2>/dev/null; then
        echo "  Configured: nexus-ollama in $KIRO_MCP_FILE"
    else
        echo "  ERROR: Failed to configure nexus-ollama in $KIRO_MCP_FILE"
        ERRORS=$((ERRORS + 1))
    fi
fi

# Configure MCP server for Antigravity CLI (Gemini).
# Antigravity CLI reads MCP config from ~/.gemini/config/mcp_config.json.
GEMINI_CONFIG_DIR="$GEMINI_DIR/config"
GEMINI_MCP_FILE="$GEMINI_CONFIG_DIR/mcp_config.json"

echo ""
echo "Configuring Antigravity CLI (Gemini) MCP..."
mkdir -p "$GEMINI_CONFIG_DIR"

if [ -f "$GEMINI_MCP_FILE" ] && grep -q '"nexus-ollama"' "$GEMINI_MCP_FILE" 2>/dev/null; then
    echo "  Already configured: nexus-ollama in $GEMINI_MCP_FILE (skipped)"
else
    # Merge nexus-ollama into existing config (or create new).
    # Uses Node since it's already a dependency for the MCP server.
    node -e "
      const fs = require('fs');
      const path = '$GEMINI_MCP_FILE';
      let config = { mcpServers: {} };
      let parseFailed = false;
      if (fs.existsSync(path)) {
        const raw = fs.readFileSync(path, 'utf8').trim();
        if (raw.length > 0) {
          try {
            config = JSON.parse(raw);
          } catch (err) {
            parseFailed = true;
            console.error('  ERROR: Failed to parse ' + path + ': ' + err.message);
          }
        }
      }
      if (!parseFailed) {
        if (!config || typeof config !== 'object' || Array.isArray(config)) config = {};
        if (!config.mcpServers || typeof config.mcpServers !== 'object' || Array.isArray(config.mcpServers)) config.mcpServers = {};
        config.mcpServers['nexus-ollama'] = {
          command: 'node',
          args: ['$MCP_SERVER_PATH']
        };
        try {
          fs.writeFileSync(path, JSON.stringify(config, null, 2) + '\n');
        } catch (err) {
          try {
            fs.chmodSync(path, 0o644);
            fs.writeFileSync(path, JSON.stringify(config, null, 2) + '\n');
          } catch (err2) {
            console.error('  ERROR: Failed to write ' + path + ': ' + err2.message);
          }
        }
      }
    "
    if grep -q '"nexus-ollama"' "$GEMINI_MCP_FILE" 2>/dev/null; then
        echo "  Configured: nexus-ollama in $GEMINI_MCP_FILE"
    else
        echo "  ERROR: Failed to configure nexus-ollama in $GEMINI_MCP_FILE"
        ERRORS=$((ERRORS + 1))
    fi
fi

# Configure MCP server for Claude Code (user scope, stored in ~/.claude.json).
# Use the claude CLI rather than editing ~/.claude.json: that file holds Claude's
# own state and a running session may rewrite it concurrently.
echo ""
echo "Configuring Claude Code MCP..."
if ! command -v claude &>/dev/null; then
    echo "  SKIPPED: claude CLI not found."
elif claude mcp get nexus-ollama &>/dev/null; then
    echo "  Already configured: nexus-ollama in Claude Code (skipped)"
elif claude mcp add --scope user nexus-ollama -- node "$MCP_SERVER_PATH" >/dev/null; then
    echo "  Configured: nexus-ollama in Claude Code (user scope)"
else
    echo "  ERROR: Failed to configure nexus-ollama in Claude Code"
    ERRORS=$((ERRORS + 1))
fi

# Build and install the TUI binary.
NEXUS_BIN_DIR="$HOME/.local/bin"
NEXUS_BIN="$NEXUS_BIN_DIR/nexus"
TUI_SRC="$NEXUS_REPO/tools/tui"

echo ""
echo "Building NEXUS TUI..."
if command -v go &>/dev/null; then
    mkdir -p "$NEXUS_BIN_DIR"
    # Inject the release version like GoReleaser does (-X main.version).
    # Falls back to "dev" when the clone has no tags.
    NEXUS_VERSION="$(git -C "$NEXUS_REPO" describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')"
    [ -z "$NEXUS_VERSION" ] && NEXUS_VERSION="dev"
    echo "  Version: $NEXUS_VERSION"
    if (cd "$TUI_SRC" && go build -ldflags "-s -w -X main.version=$NEXUS_VERSION" -o "$NEXUS_BIN" .); then
        echo "  Installed: $NEXUS_BIN"
        # Hint if ~/.local/bin isn't in PATH
        if ! echo "$PATH" | tr ':' '\n' | grep -qx "$NEXUS_BIN_DIR"; then
            echo "  NOTE: Add $NEXUS_BIN_DIR to your PATH to run 'nexus' from anywhere."
        fi
    else
        echo "  WARNING: TUI build failed. You can still use the bash scripts directly."
    fi
else
    echo "  SKIPPED: Go not found. Install Go 1.25+ to build the TUI."
    echo "  You can still use setup-nexus.sh and teardown-nexus.sh directly."
fi

# Post-setup validation: make sure every symlink actually resolves.
echo ""
echo "Verifying all symlinks..."
for link in \
    "$GEMINI_DIR/GEMINI.md" \
    "$CLAUDE_DIR/CLAUDE.md" \
    "$KIRO_STEERING_DIR/nexus-orchestrator.md" \
    "$CONFIG_NEXUS_DIR/core" \
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
    echo "Setup completed with $ERRORS error(s). Check the output above."
    exit 1
fi
echo "NEXUS setup complete. All symlinks verified."
if [ -x "$NEXUS_BIN" ]; then
    echo ""
    echo "  Run 'nexus' to manage your NEXUS installation via TUI."
fi
