# Engineering Rules

These rules apply whenever working with source code files.

## Code Quality

**Write only what is needed.** No speculative abstractions, no hypothetical future
requirements. Three similar lines of code is better than a premature abstraction.

**Do not add unprompted.** A bug fix does not need surrounding code cleaned up.
A simple feature does not need extra configurability. Do not add docstrings,
comments, or type annotations to code you did not change.

**No backwards-compatibility hacks.** No renaming to `_old`, no re-exporting removed
types, no `// removed` comments. If something is unused, delete it completely.

**Error handling at system boundaries only.** Only validate user input, external API
responses, and filesystem/network operations.

## Security

Never introduce: SQL injection, XSS, command injection, path traversal,
insecure deserialization, or hardcoded credentials. If insecure code is written,
fix it immediately — do not leave it and note it for later.

## Git Workflow

Commit message format: Conventional commits (`feat:`, `fix:`, `refactor:`, etc.).
The message body explains *why*, not what.

## Delegation Reminders

When implementing a task that crosses these domains, delegate rather than implement directly:
- Database schema design → `engineering-database-optimizer`
- Infrastructure / CI-CD → `engineering-devops-automator`
- Security review → `engineering-security-engineer`
- Code review → `engineering-code-reviewer`
- Mobile → `engineering-mobile-app-builder`
