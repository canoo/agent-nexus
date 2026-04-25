# NEXUS

[![Release](https://img.shields.io/github/v/release/canoo/agent-nexus?style=flat-square)](https://github.com/canoo/agent-nexus/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/canoo/agent-nexus/ci.yml?branch=main&style=flat-square&label=CI)](https://github.com/canoo/agent-nexus/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg?style=flat-square)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev)
[![Linux](https://img.shields.io/badge/Linux-supported-FCC624?style=flat-square&logo=linux&logoColor=black)](https://github.com/canoo/agent-nexus)
[![macOS](https://img.shields.io/badge/macOS-supported-000000?style=flat-square&logo=apple&logoColor=white)](https://github.com/canoo/agent-nexus)
[![CodeRabbit Reviews](https://img.shields.io/coderabbit/prs/github/canoo/agent-nexus?style=flat-square&labelColor=171717&color=FF570A&label=CodeRabbit+Reviews)](https://coderabbit.ai)

**Network of EXperts, Unified in Strategy** — one config for every AI tool you use. NEXUS unifies Claude Code, Gemini CLI, Kiro CLI, and any future AI CLI under a single configuration layer. Define personas once, route micro-tasks to local models, and track costs across your entire toolkit — all managed through an interactive TUI.

<p align="center">
  <img src="demo.gif" alt="NEXUS TUI Demo" width="700">
</p>

## Quick Start

```bash
curl -sSL https://raw.githubusercontent.com/canoo/agent-nexus/main/install.sh | bash
nexus
```

The first command downloads the `nexus` binary and clones the repo. The second launches the TUI to walk you through setup.

> Make sure `~/.local/bin` is in your `PATH`: `export PATH="$HOME/.local/bin:$PATH"`

## Philosophy

You use three AI tools. You manage three configs. When a new tool ships, you set up a fourth. NEXUS makes it one.

- **Subtract, don't add.** NEXUS replaces per-tool configuration with a single source of truth. One set of personas, one routing config, one place to look. Fewer files, fewer conflicts, less drift.
- **Bring your own tools.** Claude Code, Gemini CLI, Kiro CLI, Cursor, Codex, OpenClaw, or whatever ships next month. NEXUS works with what you already use — it doesn't lock you in.
- **Bring your own models.** Cloud APIs, local Ollama, or both. NEXUS routes tasks to the right tier based on complexity, not vendor lock-in.
- **Observe everything.** Track usage, costs, and routing decisions across your entire AI toolkit — not just one tool's silo.

NEXUS doesn't replace your tools. It amplifies them.

## What NEXUS Does

NEXUS is the configuration and routing layer between you and your AI CLI tools. It:

1. **Unifies config** — write personas and routing rules once, share them across every AI CLI via symlinks
2. **Routes micro-tasks locally** — commit messages, scaffolds, and lint fixes go to Ollama on your machine instead of burning cloud API tokens
3. **Detects your hardware** — auto-selects the right local model based on your GPU and available VRAM
4. **Manages everything via TUI** — install, configure, health-check, and update from a single terminal interface

## Architecture

```mermaid
graph TD
    classDef user fill:#6c5ce7,stroke:#333,stroke-width:2px,color:#fff;
    classDef orchestrator fill:#00b894,stroke:#333,stroke-width:2px,color:#fff;
    classDef cli fill:#a29bfe,stroke:#333,stroke-width:2px,color:#fff;
    classDef mcp fill:#00cec9,stroke:#333,stroke-width:2px,color:#fff;
    classDef local fill:#d63031,stroke:#333,stroke-width:2px,color:#fff;

    A[User Request]:::user --> CLI{AI CLI Tool}:::cli
    CLI -->|Claude Code| CC[~/.claude/CLAUDE.md]:::cli
    CLI -->|Gemini CLI| GC[~/.gemini/GEMINI.md]:::cli
    CLI -->|Kiro CLI| KC[~/.kiro/steering/nexus-orchestrator.md]:::cli

    CC --> B(NEXUS Orchestrator):::orchestrator
    GC --> B
    KC --> B

    B -->|Deep Work| Cloud[Cloud Models<br/>Claude / Gemini]:::cli
    B -->|Micro-Tasks| MCP[nexus-ollama MCP]:::mcp
    MCP --> Local[Local Ollama Models]:::local
```

## Installation

### One-liner (recommended)

```bash
curl -sSL https://raw.githubusercontent.com/canoo/agent-nexus/main/install.sh | bash
```

Downloads the pre-built `nexus` binary for your platform and clones the repo to `~/.config/nexus/repo`.

### From source

```bash
git clone https://github.com/canoo/agent-nexus.git
cd agent-nexus
bash setup-nexus.sh
```

Requires Go 1.25+ to build the TUI binary.

## NEXUS TUI

```
⚡ NEXUS Framework Manager
   v0.1.1

▸ Install NEXUS
  Configure
  Health Check
  Uninstall NEXUS

j/k: navigate • enter: select • q: quit
```

| Screen | What it does |
|--------|-------------|
| **Install** | Step-by-step wizard: validates repo, creates symlinks, configures MCP, checks deps, pulls Ollama models |
| **Configure** | Edit Ollama host URL and model overrides inline, saves to `.env` |
| **Health Check** | Verifies Ollama reachability and symlink status |
| **Uninstall** | Removes all symlinks and the binary (with confirmation) |

## Local LLM Configuration

NEXUS defaults to `http://localhost:11434` for Ollama. To use a dedicated GPU server:

```bash
cp .env.example .env
# Edit OLLAMA_HOST_URL to your server's address
```

Or configure interactively via the TUI's **Configure** screen.

### Default Models (4GB VRAM)

| Band | Model | Use Case | Speed |
|------|-------|----------|-------|
| Supervisor | `qwen2.5-coder:1.5b` | Formatting, JSON, boilerplate | >120 t/s |
| Logic | `llama3.2:3b` | Code generation, refactoring | ~75 t/s |

See [docs/model-configuration.md](docs/model-configuration.md) for hardware-specific presets (RTX 3060–5090, MacBook M3/M3 Max, multi-GPU).

### MCP Server

The MCP server lets AI CLI tools route micro-tasks to local Ollama automatically:

```json
{
  "mcpServers": {
    "nexus-ollama": {
      "command": "node",
      "args": ["~/.config/nexus/tools/mcp/server.mjs"]
    }
  }
}
```

Tools available: `ollama_commit_msg`, `ollama_lint_fix`, `ollama_logic_refactor`.

## Project Structure

```
core/           Core orchestrator instructions (NEXUS.md, CLAUDE.md)
personas/       Agent persona definitions
tools/tui/      NEXUS TUI (Go / Bubbletea v2)
tools/mcp/      Ollama MCP server (Node.js)
prompts/        Engineering rules and quality gates
mcp-configs/    MCP configuration templates
docs/           Documentation and hardware presets
tests/          Integration tests
```

## Prerequisites

**Required:** Bash, one or more AI CLI tools ([Claude Code](https://docs.anthropic.com/en/docs/claude-code), [Gemini CLI](https://github.com/google-gemini/gemini-cli), or [Kiro CLI](https://kiro.dev))

**Optional:** [Node.js](https://nodejs.org/) (for MCP server), [Ollama](https://ollama.com/) (for local LLM delegation)

> No Go toolchain needed — the `nexus` binary is distributed as a pre-built download.

## Testing

```bash
# Go unit tests
cd tools/tui && go test ./...

# Full install/uninstall cycle (isolated temp $HOME)
bash tests/test-install-cycle.sh
```

## Uninstallation

From the TUI: select **Uninstall NEXUS**.

Or: `bash teardown-nexus.sh`

Teardown removes only what setup created — your files are never touched.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, PR guidelines, and commit conventions.

## Security

See [SECURITY.md](SECURITY.md) for vulnerability reporting.

## License

[MIT](LICENSE) © 2026 [Codelogiic](https://www.codelogiic.com)
