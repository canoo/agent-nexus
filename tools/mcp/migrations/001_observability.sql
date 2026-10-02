CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    start_time TEXT NOT NULL,
    end_time TEXT,
    status TEXT,
    cli_tool TEXT,
    persona TEXT,
    nexus_mode TEXT,
    project_path_hash TEXT,
    nexus_version TEXT,
    metadata_json TEXT
);

CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    parent_task_id TEXT REFERENCES tasks(id),
    timestamp TEXT NOT NULL,
    source TEXT NOT NULL,
    tool TEXT,
    task_type TEXT,
    model TEXT NOT NULL,
    model_provider TEXT,
    route_band TEXT,
    routing TEXT NOT NULL,
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
    quality_rating INTEGER,
    ok INTEGER NOT NULL DEFAULT 1,
    error TEXT
);

CREATE TABLE IF NOT EXISTS routing_decisions (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    decided_at TEXT NOT NULL,
    reason TEXT NOT NULL,
    alternatives_considered TEXT,
    classifier_version TEXT,
    fallback_from TEXT,
    fallback_to TEXT,
    circuit_breaker_triggered INTEGER NOT NULL DEFAULT 0,
    latency_budget_ms INTEGER
);

CREATE INDEX IF NOT EXISTS idx_sessions_start_time ON sessions(start_time);
CREATE INDEX IF NOT EXISTS idx_tasks_session_id ON tasks(session_id);
CREATE INDEX IF NOT EXISTS idx_tasks_timestamp ON tasks(timestamp);
CREATE INDEX IF NOT EXISTS idx_tasks_model ON tasks(model);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(ok);
CREATE INDEX IF NOT EXISTS idx_tasks_routing ON tasks(routing);
CREATE INDEX IF NOT EXISTS idx_routing_decisions_task_id ON routing_decisions(task_id);
