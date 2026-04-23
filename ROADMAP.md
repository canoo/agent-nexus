# NEXUS Roadmap

This document outlines the vision and planned milestones for NEXUS. For detailed tracking, see the [GitHub Issues](https://github.com/canoo/agent-nexus/issues) and [Milestones](https://github.com/canoo/agent-nexus/milestones).

## v0.2.0 — Observability (Core)

Make routing decisions visible. Without data, we can't improve. This milestone covers what NEXUS controls directly: routing decisions, local model performance, and cost savings from local delegation.

- **Session logging** — track which model handled each task, response time, and estimated cost (#9)
- **TUI dashboard** — live view of model usage and routing stats (#10)
- **Cost tracker** — monitor cloud API spend vs local compute savings; estimate "what this would have cost on cloud" for locally-routed tasks (#11)

## v0.2.1 — CLI Usage Ingestion (via Tokscale)

Extend observability to AI CLI tools by integrating [Tokscale](https://github.com/junhoyeo/tokscale) as an optional data provider. NEXUS shells out to `tokscale --json` and renders the data in its own TUI — Tokscale's TUI is never launched, and its social/leaderboard features are not integrated.

- **Tokscale adapter** — Go module that calls `tokscale models --json`, parses response into NEXUS structs (#22)
- **Unified dashboard** — merge Tokscale CLI usage data with NEXUS-native metrics in the TUI (#23)
- **Health check integration** — detect Tokscale installation, show version and supported CLIs (#24)
- **Graceful degradation** — if Tokscale is not installed, dashboard shows NEXUS-native metrics only with an install hint (#25)
- **Session retention guidance** — warn if Claude Code cleanup is set to < 30 days (data loss risk) (#26)

**What Tokscale provides (data only):** Token counts, cost estimates, and model breakdowns from 20+ CLI tools including Claude Code, Gemini CLI, Cursor, Codex, Copilot, Amp, OpenClaw, and more. Real-time pricing via LiteLLM.

**What NEXUS still owns:** Routing decisions (cloud vs local), Ollama MCP latency, persona-to-task mapping, cost *savings* from local routing, and session-to-task correlation. The combined view shows total cloud CLI spend alongside NEXUS routing savings.

**Why Tokscale over custom parsers:** Rust-native core (10x faster), 1,000+ tests, 54 releases, MIT license, zero-config install (`bunx tokscale@latest`). When new CLIs emerge or change data formats, Tokscale handles it upstream.

## v0.3.0 — Dynamic Routing

Transform NEXUS from a config manager into a runtime.

- **Dynamic routing** — auto-select model based on task complexity (#12)
- **Latency-based fallback** — transparent failover between local and cloud (#13)
- **Chain-of-models** — multi-step orchestration: draft → review → apply (#14)

## v0.4.0 — Ecosystem

Build the community layer.

- **Persona marketplace** — discover, share, and install community personas (#15)
- **Persona composition** — combine traits from multiple personas (#16)
- **Plugin system** — extensible MCP tools defined as YAML/JSON specs (#17)

## v1.0.0 — Stable Release

Production-ready with team support.

- **Cross-platform** — Windows, Docker, Homebrew (#18)
- **Team features** — shared personas, usage analytics, policy enforcement (#19)
- **Stable API** — frozen interfaces, semantic versioning guarantees, migration guide (#20)

## Contributing

Pick an issue from any milestone and open a PR. Issues labeled [`good first issue`](https://github.com/canoo/agent-nexus/labels/good%20first%20issue) are a great starting point.

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.
