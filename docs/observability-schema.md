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

The existing MCP JSONL log remains a compatibility source during migration:

```text
~/.config/nexus/logs/mcp-tasks.jsonl
```

The MCP writer prefers SQLite when the Node runtime exposes `node:sqlite`.
Readers may fall back to JSONL until the migration is complete. JSONL writes
continue during the transition so older TUI builds can still display task
history.

## Entity Model

```text
sessions 1 -> many tasks
tasks    1 -> many routing_decisions
```

`sessions` describe where a group of tasks came from. `tasks` describe what was
executed and how it performed. `routing_decisions` describe why a route/model was
selected, including rejected alternatives and fallback details.

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
```

## Field Mapping From MCP JSONL

Current `mcp-tasks.jsonl` entries look like:

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

1. Add a small SQLite writer used by the MCP server for new task rows. [DONE]
2. Keep JSONL writes temporarily so older TUI builds can still display task
   history. [DONE]
3. Add a one-time importer that reads `mcp-tasks.jsonl` and writes missing rows
   into SQLite using deterministic task IDs.
4. Update the TUI Task Log and dashboard screens to prefer SQLite and fall back
   to JSONL when the database is absent.
5. Remove JSONL writes only after one release cycle with SQLite enabled.

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

`input_hash` can be used to correlate repeated tasks without retaining the input
itself. Hashes should be treated as metadata, not as a security boundary.

SQLite retention should mirror the current JSONL rotation behavior at first:
keep recent local history bounded, document the default, and make longer
retention an explicit setting later.

## Compatibility Notes

- Keep timestamps in UTC RFC3339 text in SQLite. Legacy millisecond timestamps
  are converted during import.
- Keep unknown values nullable rather than inventing fake token or cost numbers.
- Store routing alternatives as JSON text so Go, Node.js, and shell tooling can
  read/write the database without a custom extension.
- Do not require network access for observability storage.
