# Changelog

All notable changes to NEXUS are documented here. Releases use
[Semantic Versioning](https://semver.org/) and entries follow
[Keep a Changelog](https://keepachangelog.com/) categories.

## [Unreleased]

### Added

- Explicit desktop removal of one browser's native-host registration, preserving
  collection/consent settings, local history and other browser registrations.

- Explicit Companion first-run setup in the desktop and
  `nexus companion initialize --confirm --json`. Owned migrations create only a
  missing private store with collection off and no grants; existing files and
  journal remnants are preserved. Dashboard/status reads remain read-only.

- Native Companion development artifact builds: Linux `.deb` and unsigned macOS
  `.app`, separate host/launcher setup payload, integrity manifest and preview
  installation guide. CI archives are build evidence, pending live installation.

- Installer-facing Companion launchers that validate the staged payload and pin
  an external Node runtime for browser hosts and desktop helpers. Configured
  desktop runtime paths fail closed when missing; live installation gates remain.

- Separate Companion native-host staging with an allowlisted payload, owned
  migrations, shared data helper and deterministic SHA-256/mode manifest.
  Relocated executable and explicit registration/uninstall tests run in
  Linux/macOS CI; live browser and distributable desktop packaging gates remain.

- Shared routing settings and a deterministic CLI prompt router with explicit
  opt-in before a failed local route falls back to `agy` (#102/#103).
- Desktop Companion foundation: disabled-by-default activity storage,
  Chrome/Edge extension, bounded native host, separate TUI Tool Activity view,
  and a Tauri privacy-status window. This is development work for the v0.3.0
  private preview; packaging and end-to-end release gates remain outstanding.
- Explicit Companion browser consent controls and pause/resume lifecycle in the
  desktop shell: per-tool/browser consent granting for Chrome and Edge with
  privacy disclosure acknowledgement, atomic consent updates that do not enable
  collection, pause retaining grants, resume requiring active browser grants,
  read-only desktop status, fail-closed UI controls on store error, and safe error
  filtering (#108).
- Project memory CLI and interactive TUI screen for user-authored Markdown
  notes (`~/.config/nexus/agent-memory/<project>/`), including `nexus memory list`,
  `show`, `save`, `search`, and `init` commands alongside a two-pane TUI browser.
  Memory is user-authored project context separate from consent-gated Desktop
  Companion activity; no automatic decision capture, synchronization, or GUI
  memory view exists yet.

- Shared Companion raw-history controls for CLI and desktop: JSON status,
  retention (0–365 days, default 14), pruning, and explicitly confirmed clear.
  Native-host cleanup runs at startup, periodically while active, and before
  incoming spans. Zero days keeps no raw history; MCP tasks and project memory
  are preserved. This remains unreleased pending browser/runtime/package gates.

### Changed

- Split the TUI into screen and helper files within the existing Go package,
  preserving its behavior and CLI entrypoint (#56).

### Fixed

- MCP startup rejects empty or malformed band/per-task model overrides with a
  setting-specific stderr diagnostic before opening storage; defaults and valid
  override precedence are preserved (#35).

- Companion setup, dashboard reads, consent and data controls run on background
  workers; startup and tray disable actions also avoid waiting on SQLite or
  helper subprocesses on the GUI event thread.

- Desktop register/remove helpers run outside the GUI thread with a 15-second
  deadline and suppressed subprocess output; failures return a fixed safe error.

- Companion data helper recognizes its entry point through symlinked directory
  aliases, including macOS temporary paths, while imports remain side-effect free.

- Companion native hosts and registration/removal refuse detectable Flatpak
  contexts and unsupported operating systems before activity, cleanup or
  registration writes. Unsupported hosts return only a fixed negative reply.

- Companion popup/options merge one-tool consent changes through the worker,
  preserving simultaneous edits and keeping permission requests tied to user
  gestures. Pending grants fail closed after newer revocations.

- Serialize Companion browser state events, preserve revocation boundaries during
  asynchronous work, and keep stalled native delivery from blocking consent cleanup.
- Companion one-shot native hosts flush a fixed acknowledgement and close input,
  allowing the extension's `sendNativeMessage` call to complete. Replies contain
  no activity data or error text; malformed replies fail closed in the extension.
- Observability store construction rejects unknown options before a mistyped
  database-path option can silently fall back to the default local database.
- Companion spans cannot include time before the latest explicit resume or tool
  consent: crossing spans are discarded whole, exact UTC boundaries are accepted,
  and invalid/future timestamps fail closed. Migration 005 pauses older enabled
  preview stores once while preserving history and grants; explicit resume is
  required after upgrade. Live browser/desktop lifecycle verification remains open.
- Release publication waits for successful CI checks on the tagged commit (#49).
- TUI installer closes Ollama reachability HTTP response body before running model pulls (#50).
- TUI configuration editor Backspace removes a complete UTF-8 character,
  preserving accented text, CJK, and emoji (#53).

- Task Log reports route bands and subtracts actual local cost when calculating
  estimated cloud savings (#113).
- Companion upgrades preserve v0.2.2 tasks and import bookkeeping; its new
  schema uses migration 004 after the shipped store-metadata migration.
- Desktop builds include the window icon required by Tauri.

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
