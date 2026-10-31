# CLI routing

`nexus --goal "write a commit message"` and `nexus route "refactor this parser"`
route before starting the TUI. No arguments or `--tui` open the TUI.
Use `--dry-run` to preview and `--allow-cloud-fallback` to permit local failure
escalation. Security/authentication, architecture, deployment and ambiguous work
select the interactive `agy --prompt-interactive` agent. Control callers must
set `allowCloud: true` to authorize cloud execution, including fallback.

Simple commit/boilerplate work uses the supervisor model. Test scaffolding,
lint and logic work use the logic model. Per-task model overrides are retained.

Settings use the existing literal `.env` contract; process environment wins.

| Setting | Default | Validation |
| --- | --- | --- |
| `NEXUS_ROUTE_TIMEOUT` | `120s` | Go duration, 1ms through 10m per attempt |
| `NEXUS_ROUTE_RETRIES` | `0` | integer 0 through 3 additional attempts |
| `NEXUS_ROUTE_<TASK>_KEYWORDS` | built-in rules | comma-separated, nonempty keywords |

TASK is `COMMIT_MSG`, `BOILERPLATE`, `TEST_SCAFFOLD`, `LINT_FIX`, or
`LOGIC_REFACTOR`. Custom keywords add deterministic rules in that order;
sensitive/architecture/deployment rules take precedence. Unknown CLI options
are errors. Previews validate configuration but do not call providers.

Before local execution, `/api/tags` on `OLLAMA_HOST_URL` must return HTTP 200.
Each attempt has a deadline, and retries wait 100ms, 200ms, then 400ms.
Failed attempts discard provider output. Unix cancellation kills the delegation
process group. Caller cancellation never triggers cloud fallback.

After exhausted failures, `.nexus/route-failure` stores only a versioned ownership marker and timestamp. For
30 seconds subsequent requests avoid another slow local attempt; preview
explains the cooldown and execution requires consent before using cloud.
Success clears the timestamp. Timing logs contain only task, attempt number,
elapsed milliseconds, status and fallback consent, never prompts or output.
This is a short local circuit, not a learned model ranking or a cloud rate-limit
queue. There is no durable prompt queue.

Verification: build `tools/tui`, then run
`python3 tests/test-router-control.py /path/to/nexus`. This launches the actual
binary with a fake `agy`, checks local preview, cloud consent denial, successful
interactive cloud execution, preflight failure, consented fallback and invalid
options. It uses no listener. Local success, health responses and cancellation
are covered with injected HTTP transport and a real shell child in Go tests.
