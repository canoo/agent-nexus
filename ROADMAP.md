# NEXUS Roadmap

This document outlines the vision and planned milestones for NEXUS. For detailed tracking, see the [GitHub Issues](https://github.com/canoo/agent-nexus/issues) and [Milestones](https://github.com/canoo/agent-nexus/milestones).

## v0.2.0 — Observability

Make routing decisions visible. Without data, we can't improve.

- **Session logging** — track which model handled each task, response time, and estimated cost (#9)
- **TUI dashboard** — live view of model usage and routing stats (#10)
- **Cost tracker** — monitor cloud API spend vs local compute savings (#11)

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
