# Changelog

All notable changes to NEXUS are documented here. Releases use
[Semantic Versioning](https://semver.org/) and entries follow
[Keep a Changelog](https://keepachangelog.com/) categories.

## [Unreleased]

### Added

- Migration-owned local SQLite observability store with compatibility JSONL
  dual-write and idempotent legacy JSONL import.
- Optional [Tokscale](https://github.com/junhoyeo/tokscale) CLI usage adapter
  for local model/cost aggregates, with gratitude to its maintainers for the
  upstream multi-tool usage ingestion work.
- NEXUS Companion privacy contract and initial Linux/macOS desktop direction.

### Changed

- MCP runtime now requires Node.js 22.13 or newer for the built-in SQLite API.

### Fixed

- Claude Code now loads the NEXUS orchestrator: setup links `core/` into
  `~/.config/nexus`, so the `@~/.config/nexus/core/NEXUS.md` import in
  `CLAUDE.md` resolves.
- Setup installs the `nexus-ollama` MCP server's npm dependencies, so the
  server starts on a fresh install.
- The TUI installer and `setup-nexus.sh` register `nexus-ollama` with the same
  tools: Kiro, Gemini/Antigravity CLI, and Claude Code (user scope, through
  `claude mcp`). Teardown removes the Claude Code entry.
- The TUI no longer drops other MCP servers' settings or overwrites an
  unparseable MCP config file when adding `nexus-ollama`.
- Health Check reports dangling symlinks as broken instead of linked.
- `zod` is declared as a direct dependency of the MCP server.

### Security

- Observability rejects prompt, response, URL, source, and arbitrary metadata
  fields; provider failures are persisted only as fixed safe error categories.

## [0.2.0] - 2026-06-28

### Added

- Local routing observability, task metrics, and estimated cloud-cost savings.

## [0.1.1] - 2026-04-27

### Fixed

- macOS checksum verification and Linux/macOS installation compatibility.

## [0.1.0] - 2026-04-13

### Added

- Initial NEXUS TUI, local Ollama MCP server, persona system, and config
  projection for supported AI CLIs.
