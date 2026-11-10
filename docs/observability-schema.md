# Observability Schema

NEXUS observability starts with the local routing events it owns directly:
MCP tool calls, routing choices, local model latency, and estimated cloud-cost
equivalents. The storage layer should support the v0.2.0 TUI dashboard while
leaving room for proxy-intercepted requests in v0.3.0.

## Goals

- Track each task handled by NEXUS with model, route, latency, token counts, and
  cost estimates.
- Group tasks into sessions so CLI, MCP, and future proxy activity can be
  inspected together.
- Preserve routing rationale separately from task facts so the classifier/rules
  engine can evolve without rewriting historical task rows.
- Keep the database local-first under `~/.config/nexus/logs/`.
- Provide stable aggregate queries for the TUI dashboard and cost tracker.

## Storage

The primary database is:

```text
~/.config/nexus/logs/observability.sqlite
```

The old MCP JSONL log is a frozen legacy artifact:

```text
~/.config/nexus/logs/mcp-tasks.jsonl
```

It is no longer written and nothing reads it. Its historical rows were
absorbed into SQLite by the one-time import
(`ensureLegacyJsonlImported()` in `tools/mcp/lib/observability-store.mjs`,
run at MCP server startup); the manual importer
(`tools/mcp/scripts/import-mcp-jsonl.mjs`) remains available for preserved
snapshots. The existing file is kept on disk but untouched.

## Entity Model

```text
sessions      1 -> many tasks
tasks         1 -> many routing_decisions
sessions      1 -> many tool_activity spans (optional)
```

`sessions` describe where a group of tasks came from. `tasks` describe what was
executed and how it performed. `routing_decisions` describe why a route/model was
selected, including rejected alternatives and fallback details.

`tool_activity` represents a privacy-preserving signal that an enabled AI-tool
surface was active. It is not a task and must not be joined to a task merely by
timestamp: activity does not demonstrate a request, model selection, token use,
or response.

## Schema

```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    start_time TEXT NOT NULL,
    end_time TEXT,
    status TEXT,
    cli_tool TEXT,       -- claude-code, gemini-cli, kiro-cli, proxy, mcp
    persona TEXT,
    nexus_mode TEXT,     -- hybrid, cloud, personas-only
    project_path_hash TEXT,
    nexus_version TEXT,
    metadata_json TEXT
);

CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    parent_task_id TEXT REFERENCES tasks(id),
    timestamp TEXT NOT NULL,
    source TEXT NOT NULL,       -- mcp-tool, proxy-intercept, manual
    tool TEXT,
    task_type TEXT,
    model TEXT NOT NULL,
    model_provider TEXT,        -- ollama, fast-path, openai, anthropic
    route_band TEXT,            -- supervisor, logic, fast-path, cloud
    routing TEXT NOT NULL,      -- cloud, local, deterministic
    routing_reason TEXT,
    trace_id TEXT,
    span_id TEXT,
    parent_span_id TEXT,
    client_request_id TEXT,
    upstream_session_id TEXT,
    upstream_tool TEXT,
    idempotency_key TEXT,
    input_bytes INTEGER,
    output_bytes INTEGER,
    input_hash TEXT,
    tokens_in INTEGER,
    tokens_out INTEGER,
    total_tokens INTEGER,
    latency_ms INTEGER,
    cost_usd REAL,
    cloud_cost_equivalent REAL,
    quality_rating INTEGER,     -- null, 1 helpful, 0 not helpful
    ok INTEGER NOT NULL DEFAULT 1,
    error TEXT
);

CREATE TABLE IF NOT EXISTS routing_decisions (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    decided_at TEXT NOT NULL,
    reason TEXT NOT NULL,
    alternatives_considered TEXT, -- JSON array of {model, reason_rejected}
    classifier_version TEXT,      -- rules-v1, ml-v1
    fallback_from TEXT,
    fallback_to TEXT,
    circuit_breaker_triggered INTEGER NOT NULL DEFAULT 0,
    latency_budget_ms INTEGER
);

CREATE TABLE IF NOT EXISTS tool_activity (
    id TEXT PRIMARY KEY,
    session_id TEXT REFERENCES sessions(id),
    tool_id TEXT NOT NULL,             -- allowlisted identifier, e.g. chatgpt
    surface TEXT NOT NULL,             -- browser, desktop
    started_at TEXT NOT NULL,
    ended_at TEXT NOT NULL,
    detector TEXT NOT NULL,            -- selected-browser-tab, foreground-app
    confidence TEXT NOT NULL,          -- surface-active; never request-sent
    browser_family TEXT,
    platform TEXT,
    schema_version INTEGER NOT NULL,
    consent_policy_version INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_start_time
    ON sessions(start_time);

CREATE INDEX IF NOT EXISTS idx_tasks_session_id
    ON tasks(session_id);

CREATE INDEX IF NOT EXISTS idx_tasks_timestamp
    ON tasks(timestamp);

CREATE INDEX IF NOT EXISTS idx_tasks_model
    ON tasks(model);

CREATE INDEX IF NOT EXISTS idx_tasks_status
    ON tasks(ok);

CREATE INDEX IF NOT EXISTS idx_tasks_routing
    ON tasks(routing);

CREATE INDEX IF NOT EXISTS idx_routing_decisions_task_id
    ON routing_decisions(task_id);

CREATE INDEX IF NOT EXISTS idx_tool_activity_started_at
    ON tool_activity(started_at);

CREATE INDEX IF NOT EXISTS idx_tool_activity_tool_id
    ON tool_activity(tool_id);
```

The same migration also creates device-local Companion configuration tables:

- `companion_settings` has one disabled-by-default row with the initial
  14-day raw-span and 90-day aggregate-retention defaults.
- `companion_tool_consents` reserves explicit per-adapter, per-tool consent;
  the strict native host can write only after the existing collection and
  matching device-local consent gates are explicitly enabled; a consent UI and
  lifecycle integration remain future work.

These tables are local configuration only and are never inputs to `nexus sync`
or `nexus adopt`.

## Field Mapping From MCP JSONL

Legacy `mcp-tasks.jsonl` entries looked like:

```json
{"tool":"ollama_commit_msg","model":"qwen2.5-coder:1.5b","ms":42,"ok":true,"ts":1713890000000}
```

When importing those rows:

| JSONL field | SQLite field |
|---|---|
| `tool` | `tasks.tool` |
| `model` | `tasks.model` |
| `ms` | `tasks.latency_ms` |
| `ok` | `tasks.ok` |
| `error` | `tasks.error` |
| `ts` | `tasks.timestamp` |

Imported MCP rows should use:

| Field | Value |
|---|---|
| `sessions.cli_tool` | `mcp` |
| `sessions.nexus_mode` | `hybrid` |
| `tasks.source` | `mcp-tool` |
| `tasks.routing` | `local`, or `deterministic` when `model = fast-path` |
| `tasks.routing_reason` | `mcp:<tool-name>` |
| `tasks.model_provider` | `ollama`, or `fast-path` when `model = fast-path` |
| `routing_decisions.classifier_version` | `rules-v1` |

The importer may create a synthetic daily session for legacy JSONL rows, for
example `mcp-2026-06-27`, until real session identifiers are emitted.

## Dashboard Queries

Total tasks and success rate:

```sql
SELECT
    COUNT(*) AS total,
    SUM(CASE WHEN ok = 1 THEN 1 ELSE 0 END) AS successes,
    SUM(CASE WHEN ok = 0 THEN 1 ELSE 0 END) AS failures
FROM tasks;
```

Average latency by model:

```sql
SELECT model, AVG(latency_ms) AS avg_latency_ms, COUNT(*) AS task_count
FROM tasks
GROUP BY model
ORDER BY task_count DESC, model ASC;
```

P95 latency can be computed in the TUI from the ordered `latency_ms` values
until NEXUS adds a dedicated aggregate helper.

Local routing savings:

```sql
SELECT
    SUM(COALESCE(cloud_cost_equivalent, 0)) - SUM(COALESCE(cost_usd, 0))
        AS estimated_savings_usd
FROM tasks
WHERE routing IN ('local', 'deterministic');
```

Recent session activity:

```sql
SELECT
    s.id,
    s.start_time,
    s.cli_tool,
    COUNT(t.id) AS tasks,
    AVG(t.latency_ms) AS avg_latency_ms
FROM sessions s
LEFT JOIN tasks t ON t.session_id = s.id
GROUP BY s.id
ORDER BY s.start_time DESC
LIMIT 20;
```

## Migration Plan

1. ✅ Add a small SQLite writer used by the MCP server for new task rows.
   (Done: `tools/mcp/lib/observability-store.mjs`.)
2. ✅ Keep JSONL writes temporarily so older TUI builds can still display task
   history. (Done: the JSONL writer is unchanged and still written on every
   task event.)
3. ✅ Add a one-time importer that reads `mcp-tasks.jsonl` and writes missing rows
   into SQLite using deterministic task IDs. (Done:
   `ObservabilityStore.ensureLegacyJsonlImported()` runs at MCP server
   startup, guarded by the `legacy_jsonl_imported` marker in `store_meta`
   (migration 003); the `legacy_import_receipts` table keeps re-runs and
   concurrent startups exactly-once, and the manual importer stays available
   for preserved snapshots.)
4. ✅ Update the TUI Task Log and dashboard screens to prefer SQLite and fall back
   to JSONL when the database is absent. (Done — and since tightened:
   `tools/tui/tasklog_sqlite.go` reads `observability.sqlite` via
   `modernc.org/sqlite` in WAL mode with no JSONL fallback; a missing or
   unreadable database yields an empty log view. Both the Task Log screen
   and the Usage & Cost Dashboard go through `loadTaskLog()`, so both inherit
   the SQLite-only source.)
5. ✅ Remove JSONL writes only after one release cycle with SQLite enabled.
   (Done: `recordMcpTask()` in `tools/mcp/lib/observability-store.mjs` writes
   SQLite only, and the TUI `loadTaskLogEntries()` reads SQLite only. The
   JSONL file is frozen — never written, never read — and the run-once import
   in step 3 absorbed its history.)

## Cost Estimation

For local tasks, `cost_usd` should normally be `0`. `cloud_cost_equivalent`
should estimate what the same request would have cost on a configured cloud
model. Early versions can use static pricing tables; later versions may ingest
pricing from LiteLLM/Tokscale.

Token counts may be null until NEXUS can measure them reliably. Cost queries
must treat null values as unknown rather than zero unless the route is explicitly
local or deterministic.

## Privacy And Retention

Observability must not become a secret sink. By default, do not store raw
prompts, diffs, source files, generated code, environment variables, or provider
API keys. Store sizes, hashes, model names, routing reasons, latency, status, and
cost metadata instead.

Companion activity capture is disabled by default and requires separate
device-local consent for each browser/desktop adapter and enabled tool. Its rows
must never contain browser titles, URLs or URL fragments, DOM or network data,
account identifiers, project paths, clipboard content, prompts, or responses.
An activity span states only that a configured surface was active. It must not
be used as evidence that a model request was sent or correlated automatically
with a CLI task. See [NEXUS Companion — Activity Signals Design](nexus-companion.md)
for the fixed event envelope, retention policy, and native-host boundary.

`input_hash` can be used to correlate repeated tasks without retaining the input
itself. Hashes should be treated as metadata, not as a security boundary.

The migration stores initial Companion retention defaults (14 raw-span days and
90 aggregate days) locally. A deletion job and activity export/deletion controls
are not implemented yet, so no consumer may claim active retention enforcement.

## Compatibility Notes

- Keep timestamps in UTC RFC3339 text in SQLite. Legacy millisecond timestamps
  are converted during import.
- Keep unknown values nullable rather than inventing fake token or cost numbers.
- Store routing alternatives as JSON text so Go, Node.js, and shell tooling can
  read/write the database without a custom extension.
- Do not require network access for observability storage.
