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
