# NEXUS Roadmap

This document outlines the vision and planned milestones for NEXUS. For detailed tracking, see the [GitHub Issues](https://github.com/canoo/agent-nexus/issues) and [Milestones](https://github.com/canoo/agent-nexus/milestones).

## v0.2.0 — Observability (Core) [Released]

Make routing decisions visible. Without data, we can't improve. This milestone covers what NEXUS controls directly: routing decisions, local model performance, and cost savings from local delegation.

- **Session logging** — track which model handled each task, response time, and estimated cost (#9) [DONE]
- **TUI dashboard** — live view of model usage and routing stats (#10) [DONE]
- **Cost tracker** — monitor cloud API spend vs local compute savings; estimate "what this would have cost on cloud" for locally-routed tasks (#11) [DONE]

See [docs/observability-schema.md](docs/observability-schema.md) for the SQLite schema and telemetry architecture.

## v0.2.1 — CLI Usage Ingestion (via Tokscale)

Extend observability to AI CLI tools by integrating [Tokscale](https://github.com/junhoyeo/tokscale) as an optional data provider. NEXUS shells out to `tokscale --json` and renders the data in its own TUI — Tokscale's TUI is never launched, and its social/leaderboard features are not integrated.

- **Tokscale adapter** — Go module that calls `tokscale models --json`, parses response into NEXUS structs (#22)
- **Unified dashboard** — merge Tokscale CLI usage data with NEXUS-native metrics in the TUI (#23)
- **Health check integration** — detect Tokscale installation, show version and supported CLIs (#24)
- **Graceful degradation** — if Tokscale is not installed, dashboard shows NEXUS-native metrics only with an install hint (#25)
- **Session retention guidance** — warn if Claude Code cleanup is set to < 30 days (data loss risk) (#26)

**Automated Maintenance**
- **Recurring Chores** — GitHub Action running twice-weekly for dependency updates, security audits, and cleanup [DONE]

**What Tokscale provides (data only):** Token counts, cost estimates, and model breakdowns from 20+ CLI tools including Claude Code, Antigravity CLI / Gemini CLI, Cursor, Codex, Copilot, Amp, OpenClaw, and more. Real-time pricing via LiteLLM.

**What NEXUS still owns:** Routing decisions (cloud vs local), Ollama MCP latency, persona-to-task mapping, cost *savings* from local routing, and session-to-task correlation. The combined view shows total cloud CLI spend alongside NEXUS routing savings.

**Why Tokscale over custom parsers:** Rust-native core (10x faster), 1,000+ tests, 54 releases, MIT license, zero-config install (`bunx tokscale@latest`). When new CLIs emerge or change data formats, Tokscale handles it upstream.

## v0.3.0 — NEXUS Companion (Private Preview)

Add opt-in, local activity signals for configured AI-tool surfaces without
capturing conversation content. This is deliberately sequenced after the v0.2.1
observability-store work: Companion must write through the same migration-owned
storage boundary rather than create a second telemetry pipeline.

- **Privacy and data contract** — activity spans with fixed `tool_id`, surface,
  time range, detector, and consent-policy version; never prompts, responses,
  URLs, titles, source code, account IDs, or arbitrary metadata
- **Shared activity store** — SQLite migration, `tool_activity` schema,
  device-local consent, retention, local export, and deletion controls
- **Chrome/Edge Companion** — Manifest V3 extension with optional per-tool host
  permissions and a strict native-messaging host; no content scripts or
  `<all_urls>` permission
- **Linux/macOS desktop shell** — Tauri-based Companion with a visible tray or
  menu-bar status interface, dashboard launch, pause, and quit controls;
  Linux includes a launcher fallback and a separately verified Flatpak track
- **Tool Activity dashboard** — separate TUI view for Companion activity, never
  conflated with NEXUS routing tasks or Tokscale token/cost data
- **Privacy regression suite** — contract tests prove prompts, page titles, and
  full URLs cannot be persisted or exported

See [docs/nexus-companion.md](docs/nexus-companion.md) for the design,
platform boundaries, and acceptance criteria. See
[docs/release-process.md](docs/release-process.md) for version, website, and
community-release gates.

## v0.3.1 — Universal Sync Layer [Planned]

One config, every tool: adopt existing tool setups, keep them in sync, and
project NEXUS state into each tool's native format.

- **`nexus adopt`** — import an existing tool's config into NEXUS (TBD)
- **`nexus sync`** — propagate personas, steering files, and routing rules to all configured tools (TBD)
- **AGENTS.md projection** — generate `AGENTS.md` from NEXUS state (TBD)
- **Tool driver system** — driver registry (`tools/compat/`) for per-tool config dialects (TBD)
- **nexus-context MCP** — context projection over MCP (TBD)
- **Smithery MCP registry** — discover/install community MCP servers (TBD)
- **Compatibility matrix** — per-tool feature support table (TBD)


## v0.3.0 supporting foundation — Dynamic Routing & Antigravity CLI Integration

Transform NEXUS from a config manager into an intelligent execution runtime.

- **Dynamic Command Router** — CLI command interception and transparent execution handoff to Google Antigravity CLI (`agy`) or local SLMs via `syscall.Exec` / `os/exec`
- **Dynamic routing** — auto-select model based on task complexity (#12)
- **Latency-based fallback** — transparent failover between local and cloud (#13)
- **Chain-of-models** — multi-step orchestration: draft → review → apply (#14)
- **Modular CLI architecture** — refactor `tools/tui/` into subcommands (`cmd/root.go`, `cmd/tui.go`) and internal router packages

## v0.3.5 — Community Benchmarks [Planned]

Crowd-sourced picture of how local models perform on real hardware.

- **Benchmark schema** — standard format for benchmark results (TBD)
- **Hardware-tiered test runner** — run the same tasks across GPU tiers (TBD)
- **Community submission pipeline** — submit results back to the project (TBD)
- **Results showcase** — browse community results by hardware tier (TBD)

## v0.4.0 — Persona Ecosystem & Registry

Build the community persona layer powered by the [`canoo/Nexus-Personas`](https://github.com/canoo/Nexus-Personas) registry.

- **Persona package manager** — discover and install community personas via `nexus persona install <name>` directly from the public registry (`registry.json`) over HTTPS (#15)
- **Native AGY skill formatting** — project installed personas into `~/.gemini/` as progressive disclosure skills (`SKILL.md` with YAML frontmatter)
- **Persona composition** — combine traits from multiple personas (#16)
- **Persona auto-update** — check registry for upstream persona updates while respecting local customizations (#32)
- **Plugin system** — extensible MCP tools defined as YAML/JSON specs (#17)

## v1.0.0 — Stable Release

Production-ready with team support.

- **Cross-platform** — Windows, Docker, Homebrew (#18)
- **Team features** — shared personas, usage analytics, policy enforcement (#19)
- **Stable API** — frozen interfaces, semantic versioning guarantees, migration guide (#20)

## Contributing

Pick an issue from any milestone and open a PR. Issues labeled [`good first issue`](https://github.com/canoo/agent-nexus/labels/good%20first%20issue) are a great starting point.

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.
