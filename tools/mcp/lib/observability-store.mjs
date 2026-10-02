/**
 * The MCP observability boundary intentionally accepts metadata only.  Prompt
 * and response content is used by the MCP server to calculate estimates, but
 * must never be handed to this module or written to either persistence target.
 */
import { appendFileSync, chmodSync, existsSync, mkdirSync, readFileSync, readdirSync } from "node:fs";
import { DatabaseSync } from "node:sqlite";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createHash, randomUUID } from "node:crypto";

const DEFAULT_LOG_DIR = join(homedir(), ".config", "nexus", "logs");
export const DEFAULT_DATABASE_PATH = join(DEFAULT_LOG_DIR, "observability.sqlite");
export const DEFAULT_JSONL_PATH = join(DEFAULT_LOG_DIR, "mcp-tasks.jsonl");

const MIGRATIONS_DIR = join(dirname(fileURLToPath(import.meta.url)), "..", "migrations");
const MCP_TOOLS = new Set([
  "ollama_commit_msg",
  "ollama_boilerplate",
  "ollama_test_scaffold",
  "ollama_lint_fix",
  "ollama_logic_refactor",
]);
const REQUIRED_EVENT_FIELDS = new Set([
  "tool",
  "model",
  "routing",
  "tokens_in",
  "tokens_out",
  "cloud_cost_equivalent",
  "ms",
  "ok",
  "ts",
]);
const OPTIONAL_EVENT_FIELDS = new Set(["error"]);
const LEGACY_EVENT_FIELDS = new Set([
  ...REQUIRED_EVENT_FIELDS,
  "error",
  "id",
]);
const MODEL_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$/;
const SAFE_ERROR_CODES = new Set([
  "ollama_unreachable",
  "ollama_http_error",
  "ollama_empty_response",
  "ollama_request_failed",
]);
const COMPANION_ACTIVITY_REQUIRED_FIELDS = new Set([
  "tool_id",
  "surface",
  "started_at",
  "ended_at",
  "detector",
  "confidence",
  "schema_version",
  "consent_policy_version",
]);
const COMPANION_ACTIVITY_OPTIONAL_FIELDS = new Set(["browser_family", "platform"]);
const COMPANION_SURFACES = new Set(["browser", "desktop"]);
const COMPANION_DETECTORS = new Set(["selected-browser-tab", "foreground-app"]);
const COMPANION_CONFIDENCE = new Set(["surface-active"]);
const COMPANION_BROWSER_FAMILIES = new Set(["chrome", "edge"]);
const COMPANION_PLATFORMS = new Set(["linux", "macos"]);
const COMPANION_BROWSER_ADAPTERS = Object.freeze({
  chrome: "browser-chrome",
  edge: "browser-edge",
});
const COMPANION_DESKTOP_ADAPTER = "desktop-foreground-app";
const RFC3339_UTC = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,3})?Z$/;

/**
 * The initial Companion tool registry. Adapter code maps a local surface to
 * one of these identifiers; callers can never submit an origin, title, or a
 * provider-supplied name in its place.
 */
export const COMPANION_TOOL_IDS = Object.freeze([
  "chatgpt",
  "claude",
  "gemini",
  "copilot",
  "perplexity",
]);
const COMPANION_TOOL_ID_SET = new Set(COMPANION_TOOL_IDS);

function tryMakePrivate(path, mode) {
  if (process.platform === "win32") return;
  try { chmodSync(path, mode); } catch {}
}

function ensurePrivateDirectory(directory) {
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  tryMakePrivate(directory, 0o700);
}

function defaultDatabaseFactory(path) {
  return new DatabaseSync(path, { enableForeignKeyConstraints: true });
}

function defaultAppendJsonl(path, line) {
  appendFileSync(path, line);
}

function assertSafeInteger(value, field) {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new TypeError(`${field} must be a non-negative safe integer`);
  }
}

function assertFiniteNonNegative(value, field) {
  if (typeof value !== "number" || !Number.isFinite(value) || value < 0) {
    throw new TypeError(`${field} must be a finite non-negative number`);
  }
}

function normalizedUtcTimestamp(value, field) {
  if (typeof value !== "string" || !RFC3339_UTC.test(value)) {
    throw new TypeError(`${field} must be a UTC RFC3339 timestamp`);
  }
  const timestamp = new Date(value);
  if (Number.isNaN(timestamp.getTime())) {
    throw new TypeError(`${field} must be a valid UTC RFC3339 timestamp`);
  }
  return timestamp.toISOString();
}

/**
 * Rejects unknown fields instead of silently dropping them.  This makes a
 * future accidental call such as recordMcpTask({ prompt }) fail before either
 * SQLite or compatibility JSONL can receive sensitive content.
 */
export function normalizeMcpTaskEvent(rawEvent) {
  if (!rawEvent || typeof rawEvent !== "object" || Array.isArray(rawEvent)) {
    throw new TypeError("MCP task event must be an object");
  }

  for (const key of Object.keys(rawEvent)) {
    if (!REQUIRED_EVENT_FIELDS.has(key) && !OPTIONAL_EVENT_FIELDS.has(key)) {
      throw new TypeError(`MCP task event contains unsupported field: ${key}`);
    }
  }
  for (const key of REQUIRED_EVENT_FIELDS) {
    if (!(key in rawEvent)) throw new TypeError(`MCP task event is missing ${key}`);
  }

  const { tool, model, routing, tokens_in, tokens_out, cloud_cost_equivalent, ms, ok, ts, error } = rawEvent;
  if (typeof tool !== "string" || !MCP_TOOLS.has(tool)) {
    throw new TypeError("tool must be an allowlisted MCP tool");
  }
  if (typeof model !== "string" || !MODEL_PATTERN.test(model)) {
    throw new TypeError("model must be a safe model identifier");
  }
  if (routing !== "local" && routing !== "deterministic") {
    throw new TypeError("routing must be local or deterministic");
  }
  if ((model === "fast-path") !== (routing === "deterministic")) {
    throw new TypeError("routing must match the model route");
  }
  assertSafeInteger(tokens_in, "tokens_in");
  assertSafeInteger(tokens_out, "tokens_out");
  assertFiniteNonNegative(cloud_cost_equivalent, "cloud_cost_equivalent");
  assertSafeInteger(ms, "ms");
  assertSafeInteger(ts, "ts");
  if (typeof ok !== "boolean") throw new TypeError("ok must be a boolean");
  if (error !== undefined && (typeof error !== "string" || !SAFE_ERROR_CODES.has(error))) {
    throw new TypeError("error must be an allowlisted safe error code");
  }

  const timestamp = new Date(ts);
  if (Number.isNaN(timestamp.getTime())) throw new TypeError("ts must be a valid Unix timestamp");

  return Object.freeze({
    id: randomUUID(),
    tool,
    model,
    routing,
    tokens_in,
    tokens_out,
    cloud_cost_equivalent,
    ms,
    ok,
    ts,
    ...(error === undefined ? {} : { error }),
    timestamp: timestamp.toISOString(),
  });
}

/**
 * Normalizes the only activity envelope accepted by the local observability
 * boundary. It intentionally has no generic metadata field and rejects every
 * unknown key before opening SQLite, including prompt/response/URL/title and
 * account or source identifiers.
 */
export function normalizeToolActivityEvent(rawEvent) {
  if (!rawEvent || typeof rawEvent !== "object" || Array.isArray(rawEvent)) {
    throw new TypeError("Companion activity event must be an object");
  }

  for (const key of Object.keys(rawEvent)) {
    if (!COMPANION_ACTIVITY_REQUIRED_FIELDS.has(key) && !COMPANION_ACTIVITY_OPTIONAL_FIELDS.has(key)) {
      throw new TypeError(`Companion activity event contains unsupported field: ${key}`);
    }
  }
  for (const key of COMPANION_ACTIVITY_REQUIRED_FIELDS) {
    if (!(key in rawEvent)) throw new TypeError(`Companion activity event is missing ${key}`);
  }

  const {
    tool_id: toolId,
    surface,
    started_at: startedAt,
    ended_at: endedAt,
    detector,
    confidence,
    browser_family: browserFamily,
    platform,
    schema_version: schemaVersion,
    consent_policy_version: consentPolicyVersion,
  } = rawEvent;
  if (typeof toolId !== "string" || !COMPANION_TOOL_ID_SET.has(toolId)) {
    throw new TypeError("tool_id must be an allowlisted Companion tool");
  }
  if (!COMPANION_SURFACES.has(surface)) {
    throw new TypeError("surface must be browser or desktop");
  }
  if (!COMPANION_DETECTORS.has(detector)) {
    throw new TypeError("detector must be an allowlisted Companion detector");
  }
  if (!COMPANION_CONFIDENCE.has(confidence)) {
    throw new TypeError("confidence must be surface-active");
  }
  if (!Number.isSafeInteger(schemaVersion) || schemaVersion !== 1) {
    throw new TypeError("schema_version must be the supported version");
  }
  if (!Number.isSafeInteger(consentPolicyVersion) || consentPolicyVersion !== 1) {
    throw new TypeError("consent_policy_version must be the supported version");
  }
  if (platform !== undefined && !COMPANION_PLATFORMS.has(platform)) {
    throw new TypeError("platform must be linux or macos");
  }

  if (surface === "browser") {
    if (detector !== "selected-browser-tab") {
      throw new TypeError("browser activity must use the selected-browser-tab detector");
    }
    if (typeof browserFamily !== "string" || !COMPANION_BROWSER_FAMILIES.has(browserFamily)) {
      throw new TypeError("browser_family must be chrome or edge for browser activity");
    }
  } else {
    if (detector !== "foreground-app") {
      throw new TypeError("desktop activity must use the foreground-app detector");
    }
    if (browserFamily !== undefined) {
      throw new TypeError("browser_family is not permitted for desktop activity");
    }
  }

  const normalizedStartedAt = normalizedUtcTimestamp(startedAt, "started_at");
  const normalizedEndedAt = normalizedUtcTimestamp(endedAt, "ended_at");
  if (Date.parse(normalizedEndedAt) <= Date.parse(normalizedStartedAt)) {
    throw new TypeError("ended_at must be after started_at");
  }

  return Object.freeze({
    id: randomUUID(),
    toolId,
    surface,
    startedAt: normalizedStartedAt,
    endedAt: normalizedEndedAt,
    detector,
    confidence,
    ...(browserFamily === undefined ? {} : { browserFamily }),
    ...(platform === undefined ? {} : { platform }),
    schemaVersion,
    consentPolicyVersion,
  });
}

/**
 * Consent is checked against an adapter selected from the already-validated
 * fixed envelope. The ingestion caller cannot name an arbitrary adapter or
 * smuggle an identifier through the content-free activity contract.
 */
function adapterIdForToolActivity(event) {
  if (event.surface === "browser") return COMPANION_BROWSER_ADAPTERS[event.browserFamily];
  return COMPANION_DESKTOP_ADAPTER;
}

function migrationsFrom(directory) {
  const migrations = readdirSync(directory)
    .map((name) => {
      const match = /^(\d+)_[-A-Za-z0-9]+\.sql$/.exec(name);
      if (!match) return null;
      return { version: Number(match[1]), name, sql: readFileSync(join(directory, name), "utf8") };
    })
    .filter(Boolean)
    .sort((left, right) => left.version - right.version);

  if (migrations.some((migration, index) => index > 0 && migration.version === migrations[index - 1].version)) {
    throw new Error("observability migrations contain duplicate versions");
  }
  return migrations;
}

function appliedMigrationVersions(database) {
  try {
    return new Set(database.prepare("SELECT version FROM schema_migrations").all().map(({ version }) => version));
  } catch (error) {
    // Version 1 creates this table, so its absence is expected on a new file.
    if (/no such table: schema_migrations/i.test(String(error.message))) return new Set();
    throw error;
  }
}

function applyMigrations(database, migrations) {
  const applied = appliedMigrationVersions(database);
  for (const migration of migrations) {
    if (applied.has(migration.version)) continue;
    database.exec("BEGIN IMMEDIATE");
    try {
      database.exec(migration.sql);
      database.prepare("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)")
        .run(migration.version, new Date().toISOString());
      database.exec("COMMIT");
    } catch (error) {
      try { database.exec("ROLLBACK"); } catch {}
      throw error;
    }
  }
}

function routeBandFor(tool, model) {
  if (model === "fast-path") return "fast-path";
  return tool === "ollama_lint_fix" || tool === "ollama_logic_refactor" ? "logic" : "supervisor";
}

function sha256(value) {
  return createHash("sha256").update(value).digest("hex");
}

function legacyRowIdentity(sourcePath, rawLine, occurrence) {
  const rawLineHash = sha256(rawLine);
  const taskId = `legacy-mcp-${sha256(JSON.stringify([sourcePath, rawLine, occurrence]))}`;
  return { rawLineHash, taskId };
}

/**
 * Legacy logs predate the strict writer contract. They may omit routing,
 * tokens, or cloud-cost estimates, which remain null rather than guessed.
 */
function normalizeLegacyMcpTaskRow(rawRow) {
  if (!rawRow || typeof rawRow !== "object" || Array.isArray(rawRow)) {
    throw new TypeError("legacy row must be an object");
  }
  for (const key of Object.keys(rawRow)) {
    if (!LEGACY_EVENT_FIELDS.has(key)) {
      throw new TypeError(`legacy row contains unsupported field: ${key}`);
    }
  }

  const { tool, model, routing, tokens_in, tokens_out, cloud_cost_equivalent, ms, ok, ts, error } = rawRow;
  if (typeof tool !== "string" || !MCP_TOOLS.has(tool)) {
    throw new TypeError("legacy tool must be an allowlisted MCP tool");
  }
  if (typeof model !== "string" || !MODEL_PATTERN.test(model)) {
    throw new TypeError("legacy model must be a safe model identifier");
  }
  const expectedRouting = model === "fast-path" ? "deterministic" : "local";
  if (routing !== undefined && routing !== expectedRouting) {
    throw new TypeError("legacy routing must match the model route");
  }
  if (tokens_in !== undefined) assertSafeInteger(tokens_in, "legacy tokens_in");
  if (tokens_out !== undefined) assertSafeInteger(tokens_out, "legacy tokens_out");
  if (cloud_cost_equivalent !== undefined) {
    assertFiniteNonNegative(cloud_cost_equivalent, "legacy cloud_cost_equivalent");
  }
  assertSafeInteger(ms, "legacy ms");
  assertSafeInteger(ts, "legacy ts");
  if (typeof ok !== "boolean") throw new TypeError("legacy ok must be a boolean");
  if (error !== undefined && (typeof error !== "string" || !SAFE_ERROR_CODES.has(error))) {
    throw new TypeError("legacy error must be an allowlisted safe error code");
  }

  const timestamp = new Date(ts);
  if (Number.isNaN(timestamp.getTime())) throw new TypeError("legacy ts must be a valid Unix timestamp");
  return {
    tool,
    model,
    routing: expectedRouting,
    tokensIn: tokens_in ?? null,
    tokensOut: tokens_out ?? null,
    cloudCostEquivalent: cloud_cost_equivalent ?? null,
    ms,
    ok,
    error: error ?? null,
    timestamp: timestamp.toISOString(),
  };
}

/**
 * Migration-owned, local-only MCP persistence.  Dependency injection is
 * limited to file/database operations so consumers cannot bypass validation.
 */
export class ObservabilityStore {
  constructor({
    databasePath = DEFAULT_DATABASE_PATH,
    jsonlPath = DEFAULT_JSONL_PATH,
    migrationsDir = MIGRATIONS_DIR,
    databaseFactory = defaultDatabaseFactory,
    appendJsonl = defaultAppendJsonl,
  } = {}) {
    this.databasePath = databasePath;
    this.jsonlPath = jsonlPath;
    this.migrationsDir = migrationsDir;
    this.databaseFactory = databaseFactory;
    this.appendJsonl = appendJsonl;
  }

  migrate() {
    const database = this.#openDatabase();
    try {
      applyMigrations(database, migrationsFrom(this.migrationsDir));
    } finally {
      database.close();
    }
  }

  recordMcpTask(rawEvent) {
    const event = normalizeMcpTaskEvent(rawEvent);
    const result = { id: event.id, sqlite: { ok: false }, jsonl: { ok: false } };

    // These are deliberately independent attempts: neither compatibility log
    // degradation nor SQLite degradation gets to suppress the other writer.
    try {
      this.#writeSqlite(event);
      result.sqlite.ok = true;
    } catch (error) {
      result.sqlite.error = error instanceof Error ? error.message : String(error);
    }

    try {
      this.#writeJsonl(event);
      result.jsonl.ok = true;
    } catch (error) {
      result.jsonl.error = error instanceof Error ? error.message : String(error);
    }
    return result;
  }

  /**
   * Persist a validated Companion span in the existing local SQLite store.
   * Companion activity is intentionally not serialized into mcp-tasks.jsonl:
   * that append-only compatibility log remains exclusively MCP task history.
   * The shared store itself fails closed unless global collection and the
   * envelope-derived adapter/tool consent are both currently enabled.
   */
  recordToolActivity(rawEvent) {
    const event = normalizeToolActivityEvent(rawEvent);
    const result = { id: event.id, sqlite: { ok: false } };
    try {
      this.#writeToolActivity(event);
      result.sqlite.ok = true;
    } catch (error) {
      result.sqlite.error = error instanceof Error ? error.message : String(error);
    }
    return result;
  }

  /**
   * Import one local JSONL snapshot without invoking recordMcpTask(), because
   * that public ingestion API intentionally emits a compatibility JSONL line.
   * Receipts make preserved snapshots and normal append-only growth safe to
   * import repeatedly.
   */
  importLegacyMcpJsonl({ inputPath = DEFAULT_JSONL_PATH } = {}) {
    if (typeof inputPath !== "string" || inputPath.length === 0 || inputPath.startsWith("file:")) {
      throw new TypeError("inputPath must be a local filesystem path");
    }
    const sourcePath = resolve(inputPath);

    // Migration must happen before opening the input so all recovery imports
    // see a ready receipt schema even when input parsing later fails.
    this.migrate();
    const sourceSnapshot = readFileSync(sourcePath, "utf8");
    const snapshotHash = sha256(sourceSnapshot);
    const lines = sourceSnapshot.split("\n");
    if (lines.at(-1) === "") lines.pop();
    const result = {
      sourcePath,
      snapshotHash,
      totalLines: lines.length,
      imported: 0,
      alreadyImported: 0,
      skippedBlank: 0,
      skippedMalformed: 0,
      skippedUnsafe: 0,
    };
    const occurrences = new Map();

    for (const sourceLine of lines) {
      const rawLine = sourceLine.endsWith("\r") ? sourceLine.slice(0, -1) : sourceLine;
      if (rawLine.trim() === "") {
        result.skippedBlank += 1;
        continue;
      }
      const occurrence = (occurrences.get(rawLine) ?? 0) + 1;
      occurrences.set(rawLine, occurrence);

      let rawRow;
      try {
        rawRow = JSON.parse(rawLine);
      } catch {
        result.skippedMalformed += 1;
        continue;
      }

      let event;
      try {
        event = normalizeLegacyMcpTaskRow(rawRow);
      } catch {
        result.skippedUnsafe += 1;
        continue;
      }

      const identity = legacyRowIdentity(sourcePath, rawLine, occurrence);
      if (this.#writeLegacyMcpTask({ sourcePath, snapshotHash, occurrence, ...identity, event })) {
        result.imported += 1;
      } else {
        result.alreadyImported += 1;
      }
    }
    return Object.freeze(result);
  }

  #openDatabase() {
    ensurePrivateDirectory(dirname(this.databasePath));
    const isNew = !existsSync(this.databasePath);
    const database = this.databaseFactory(this.databasePath);
    if (isNew) tryMakePrivate(this.databasePath, 0o600);
    return database;
  }

  #writeSqlite(event) {
    const database = this.#openDatabase();
    try {
      applyMigrations(database, migrationsFrom(this.migrationsDir));
      const sessionId = `mcp-${event.timestamp.slice(0, 10)}`;
      const sessionStart = `${event.timestamp.slice(0, 10)}T00:00:00.000Z`;
      const provider = event.model === "fast-path" ? "fast-path" : "ollama";
      const reason = `mcp:${event.tool}`;

      database.exec("BEGIN IMMEDIATE");
      try {
        database.prepare(`INSERT INTO sessions (
          id, start_time, status, cli_tool, nexus_mode
        ) VALUES (?, ?, ?, ?, ?)
        ON CONFLICT(id) DO NOTHING`).run(sessionId, sessionStart, "active", "mcp", "hybrid");

        database.prepare(`INSERT INTO tasks (
          id, session_id, timestamp, source, tool, task_type, model,
          model_provider, route_band, routing, routing_reason, tokens_in,
          tokens_out, total_tokens, latency_ms, cloud_cost_equivalent, ok, error
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
          .run(
            event.id, sessionId, event.timestamp, "mcp-tool", event.tool, event.tool,
            event.model, provider, routeBandFor(event.tool, event.model), event.routing,
            reason, event.tokens_in, event.tokens_out, event.tokens_in + event.tokens_out,
            event.ms, event.cloud_cost_equivalent, event.ok ? 1 : 0, event.error ?? null,
          );

        database.prepare(`INSERT INTO routing_decisions (
          id, task_id, decided_at, reason, classifier_version, circuit_breaker_triggered
        ) VALUES (?, ?, ?, ?, ?, ?)`)
          .run(randomUUID(), event.id, event.timestamp, reason, "rules-v1", 0);
        database.exec("COMMIT");
      } catch (error) {
        try { database.exec("ROLLBACK"); } catch {}
        throw error;
      }
    } finally {
      database.close();
    }
  }

  #writeToolActivity(event) {
    const database = this.#openDatabase();
    try {
      applyMigrations(database, migrationsFrom(this.migrationsDir));
      const adapterId = adapterIdForToolActivity(event);

      // A single immediate transaction prevents a concurrently revoked local
      // consent from being observed between the checks and the insert.
      database.exec("BEGIN IMMEDIATE");
      try {
        const collection = database.prepare(`SELECT collection_enabled
          FROM companion_settings WHERE id = 1`).get();
        if (collection?.collection_enabled !== 1) {
          throw new Error("companion_collection_disabled");
        }

        const consent = database.prepare(`SELECT 1 FROM companion_tool_consents
          WHERE adapter_id = ? AND tool_id = ? AND enabled = 1
            AND consent_policy_version = ?`).get(
          adapterId, event.toolId, event.consentPolicyVersion,
        );
        if (!consent) throw new Error("companion_tool_consent_missing");

        database.prepare(`INSERT INTO tool_activity (
          id, session_id, tool_id, surface, started_at, ended_at, detector,
          confidence, browser_family, platform, schema_version, consent_policy_version
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
          .run(
            event.id, null, event.toolId, event.surface, event.startedAt, event.endedAt,
            event.detector, event.confidence, event.browserFamily ?? null, event.platform ?? null,
            event.schemaVersion, event.consentPolicyVersion,
          );
        database.exec("COMMIT");
      } catch (error) {
        try { database.exec("ROLLBACK"); } catch {}
        throw error;
      }
    } finally {
      database.close();
    }
  }

  #writeLegacyMcpTask({ sourcePath, snapshotHash, rawLineHash, occurrence, taskId, event }) {
    const database = this.#openDatabase();
    try {
      applyMigrations(database, migrationsFrom(this.migrationsDir));
      const sessionId = `mcp-${event.timestamp.slice(0, 10)}`;
      const sessionStart = `${event.timestamp.slice(0, 10)}T00:00:00.000Z`;
      const provider = event.model === "fast-path" ? "fast-path" : "ollama";
      const reason = `mcp:${event.tool}`;

      database.exec("BEGIN IMMEDIATE");
      try {
        const receipt = database.prepare(`SELECT 1 FROM legacy_import_receipts
          WHERE source_path = ? AND raw_line_hash = ? AND occurrence = ?`)
          .get(sourcePath, rawLineHash, occurrence);
        if (receipt) {
          database.exec("COMMIT");
          return false;
        }

        database.prepare(`INSERT INTO sessions (
          id, start_time, status, cli_tool, nexus_mode
        ) VALUES (?, ?, ?, ?, ?)
        ON CONFLICT(id) DO NOTHING`).run(sessionId, sessionStart, "active", "mcp", "hybrid");

        database.prepare(`INSERT INTO tasks (
          id, session_id, timestamp, source, tool, task_type, model,
          model_provider, route_band, routing, routing_reason, tokens_in,
          tokens_out, total_tokens, latency_ms, cloud_cost_equivalent, ok, error
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
          .run(
            taskId, sessionId, event.timestamp, "mcp-tool", event.tool, event.tool,
            event.model, provider, routeBandFor(event.tool, event.model), event.routing,
            reason, event.tokensIn, event.tokensOut,
            event.tokensIn === null || event.tokensOut === null ? null : event.tokensIn + event.tokensOut,
            event.ms, event.cloudCostEquivalent, event.ok ? 1 : 0, event.error,
          );
        database.prepare(`INSERT INTO routing_decisions (
          id, task_id, decided_at, reason, classifier_version, circuit_breaker_triggered
        ) VALUES (?, ?, ?, ?, ?, ?)`)
          .run(randomUUID(), taskId, event.timestamp, reason, "rules-v1", 0);
        database.prepare(`INSERT INTO legacy_import_receipts (
          source_path, snapshot_hash, raw_line_hash, occurrence, task_id, imported_at
        ) VALUES (?, ?, ?, ?, ?, ?)`)
          .run(sourcePath, snapshotHash, rawLineHash, occurrence, taskId, new Date().toISOString());
        database.exec("COMMIT");
        return true;
      } catch (error) {
        try { database.exec("ROLLBACK"); } catch {}
        throw error;
      }
    } finally {
      database.close();
    }
  }

  #writeJsonl(event) {
    ensurePrivateDirectory(dirname(this.jsonlPath));
    // Keep the established compatibility fields and their familiar ordering;
    // `id` is deliberately additive for older JSONL readers.
    const jsonlEvent = {
      tool: event.tool,
      model: event.model,
      routing: event.routing,
      tokens_in: event.tokens_in,
      tokens_out: event.tokens_out,
      cloud_cost_equivalent: event.cloud_cost_equivalent,
      ms: event.ms,
      ok: event.ok,
      ts: event.ts,
      ...(event.error === undefined ? {} : { error: event.error }),
      id: event.id,
    };
    this.appendJsonl(this.jsonlPath, `${JSON.stringify(jsonlEvent)}\n`);
  }
}

export function createObservabilityStore(options) {
  return new ObservabilityStore(options);
}
