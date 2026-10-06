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
- Claude Code loads the NEXUS orchestrator and starts `nexus-ollama` on a fresh
  install ([#111](https://github.com/canoo/agent-nexus/pull/111)).
- `recurring-chores.yml` granted push permissions — the dependency-bot 403
  that starved all updates is fixed.

### Security

- `proxy-addr` critical advisory resolved via the MCP SDK 1.32.1 resync.
- Go toolchain 1.26.3 → 1.27.1 (known stdlib CVEs on the old toolchain).
- `npm audit`: 0 vulnerabilities.

## [0.2.1] - 2026-10-02

### Added

- Migration-owned local SQLite observability store with compatibility JSONL
  dual-write and an idempotent legacy JSONL import path.
- Optional [Tokscale](https://github.com/junhoyeo/tokscale) CLI usage adapter
  for local model/cost aggregates, with gratitude to its maintainers for the
  upstream multi-tool usage ingestion work.
- Usage & Cost Dashboard showing Tokscale aggregates separately from NEXUS
  routing metrics, with optional Tokscale health and session-retention guidance.

### Changed

- MCP runtime now requires Node.js 22.13 or newer for the built-in SQLite API.

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
