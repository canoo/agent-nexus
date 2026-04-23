# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | ✅ Current release |

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly.

**Do not open a public issue.**

Instead, email **security@codelogiic.com** with:

- A description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

You should receive an acknowledgment within 48 hours. We'll work with you to understand the issue and coordinate a fix before any public disclosure.

## Scope

This policy covers:

- The `nexus` TUI binary
- The `install.sh` and `setup-nexus.sh` scripts (which modify dotfiles and create symlinks)
- The `nexus-ollama` MCP server (`tools/mcp/server.mjs`)
- Any credential or secret handling in configuration

## Out of Scope

- Vulnerabilities in upstream dependencies (Ollama, Node.js, Go) — report those to their respective projects
- Issues with AI model outputs — NEXUS routes prompts but does not control model behavior
