# Changelog

All notable changes to NEXUS are documented here. Releases use
[Semantic Versioning](https://semver.org/) and entries follow
[Keep a Changelog](https://keepachangelog.com/) categories.

## [Unreleased]

## [0.2.2] - 2026-10-06

### Added

- SQLite Task Log in the TUI: reads the observability database directly
  (pure-Go driver, no CGO), with all 26 task columns populated — real costs,
  byte counts, content hashes, task type, and model/provider.
- Run-once legacy JSONL import at MCP server startup: pre-SQLite history is
  imported idempotently, and an import failure can never block startup.

### Changed

- Task Log reads SQLite exclusively; the JSONL compatibility dual-write is
  removed. One source of truth, no drift between formats.
- Installers now enforce Node.js 22.13+ and git prerequisites before doing
  any work.
- The five MCP config writers are unified into one canonical merger that
  preserves unknown keys and per-entry extras.
- Session-retention normalization consolidated into a single function.
- Install contract no longer references the dead `mcp-configs/` directory.

### Fixed

- `configureMCP` nil-map panic on null MCP configs.
- TUI self-update now verifies `install.sh` via checksums (GoReleaser
  `checksum.extra_files`).
- `setup-nexus.sh` injects the real version via ldflags instead of building
  as permanent `"dev"`.
- `install.sh` installs the MCP server's npm dependencies (was the only
  setup path missing it).
- `recurring-chores.yml` granted push permissions — the dependency-bot 403
  that starved all updates is fixed.

### Security

- `proxy-addr` critical advisory resolved via the MCP SDK 1.32.1 resync.
- Go toolchain 1.26.3 → 1.27.1 (known stdlib CVEs on the old toolchain).
- `npm audit`: 0 vulnerabilities.

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
