# Contributing to NEXUS

Thanks for your interest in contributing! This guide covers everything you need to get started.

## Quick Start

```bash
git clone https://github.com/canoo/agent-nexus.git
cd agent-nexus
bash setup-nexus.sh
```

To build the TUI from source (requires Go 1.25+):

```bash
cd tools/tui
go build -o nexus .
```

## Development Workflow

1. Fork the repo and create a branch from `main`
2. Name your branch: `feat/description`, `fix/description`, or `docs/description`
3. Make your changes
4. Run tests: `cd tools/tui && go test ./...`
5. Run the install cycle test: `bash tests/test-install-cycle.sh`
6. Commit using [Conventional Commits](#commit-conventions)
7. Open a PR against `main`

## Commit Conventions

We use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add new persona for database specialist
fix: correct symlink resolution on macOS
docs: update MCP server configuration guide
chore: update Go dependencies
test: add install cycle edge case
```

Scope is optional but encouraged:

```
feat(tui): add model override screen
fix(mcp): handle missing OLLAMA_HOST_URL
```

## Pull Request Guidelines

- Keep PRs focused — one feature or fix per PR
- Include a clear description of what changed and why
- Ensure CI passes (build, vet, tests, shellcheck)
- Update documentation if your change affects user-facing behavior
- Add tests for new functionality

## What to Contribute

- **Personas** — new agent personas in `personas/`
- **Bug fixes** — especially around symlink handling, cross-platform support
- **Documentation** — improvements to README, inline docs, or new guides
- **TUI improvements** — new screens, better UX, accessibility
- **MCP tools** — new Ollama delegation tools in `tools/mcp/`
- **Tests** — more coverage for install/teardown edge cases

## Project Structure

```
core/           Core orchestrator instructions
personas/       Agent persona definitions
tools/tui/      NEXUS TUI (Go / Bubbletea v2)
tools/mcp/      Ollama MCP server (Node.js)
prompts/        Engineering rules and quality gates
tests/          Integration tests
```

## Code Style

- **Go**: `gofmt` and `go vet` must pass. No external linter required.
- **Shell**: Must pass `shellcheck`.
- **Markdown**: Personas follow the existing format in `personas/`.

## Testing

The CI pipeline runs on every PR:

- `go build` and `go vet` for the TUI
- `go test ./...` for Go unit tests
- `shellcheck` for all shell scripts
- `test-install-cycle.sh` for end-to-end install/teardown validation

Run locally before pushing:

```bash
cd tools/tui && go test ./... && go vet ./...
shellcheck setup-nexus.sh teardown-nexus.sh install.sh
bash tests/test-install-cycle.sh
```

## Questions?

Open an issue — we're happy to help.
