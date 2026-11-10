# Dev Roadmap

**Goal:** Build the first executable, migration-owned local SQLite observability ingestion boundary for existing MCP task logs while retaining JSONL compatibility.
**Mode:** Greenfield
**Generated:** 2026-10-02
**Stack:** Node.js ESM MCP server (Node 22 CI), native `node:sqlite`, JSONL compatibility log, Go Bubble Tea TUI consumer.

## Scope and boundary

This roadmap implements only the v0.2.1 observability foundation that Companion will share:

- one local SQLite store at `~/.config/nexus/logs/observability.sqlite`;
- ordered, source-controlled migrations applied by the store itself;
- one MCP task-ingestion API that owns SQLite persistence and compatibility JSONL emission;
- an idempotent import path for existing `mcp-tasks.jsonl` history; and
- automated contract coverage for migrations, normal writes, degradation, and import.

It does **not** build a browser extension, native-messaging host, desktop detector, Companion consent UI, activity screen, retention job, TUI SQLite reader, or Tokscale ingestion. `tool_activity` is deliberately deferred to a later migration: this first store must be stable before a second producer is introduced.

## Implementation contract

- The migration runner is the sole authority for creating or changing the database schema. No caller may issue ad-hoc DDL.
- `recordMcpTask()` (final name to be chosen in implementation) is the sole application ingestion entry point for MCP task events. `tools/mcp/server.mjs` must stop appending JSONL directly.
- The boundary receives only the existing safe task metadata (`tool`, `model`, routing, estimated token/cost fields, latency, status, timestamp, and error). It must never accept or persist prompts, responses, source content, environment variables, or keys.
- Each newly emitted compatibility JSONL event receives a generated event ID. Existing TUI versions ignore the additive field; SQLite uses it as its task ID/idempotency key.
- Every record attempt retains the current JSONL emission behavior during the compatibility window. A SQLite failure is reported only on stderr or an internal result value and must never alter an MCP tool response. A JSONL failure must likewise not prevent the tool response.
- Legacy imports are append-only to SQLite and never modify, rotate, delete, or rename `mcp-tasks.jsonl`. Re-running the importer is idempotent.
- SQLite storage uses UTC RFC3339 timestamps, synthetic UTC daily MCP sessions (`mcp-YYYY-MM-DD`) for history without a session ID, `mcp-tool` as source, and the field mappings in `docs/observability-schema.md`.
- The store creates the log directory with owner-only permissions where the platform permits it, and creates the database with owner-readable/writable permissions. It remains fully local and network-free.

## Phases

## Phase 0 — Runtime Contract

**Effort:** S
**Phase type:** infrastructure

| Order | Routing | Type | Commit Message | Files Affected | Goal |
|---|---|---|---|---|---|
| 1 | [L1] | chore | `chore(mcp): declare native SQLite runtime` | `tools/mcp/package.json`, `tools/mcp/package-lock.json` | Declare the supported Node runtime (`>=22.13.0`) and add a `node --test` script, relying on the maintained built-in `node:sqlite` API rather than a native third-party SQLite package. |

Implementation notes:

- Keep the CI major on Node 22; the implementation must verify the actual `node:sqlite` API used is available on the supported minimum version.
- Do not add `better-sqlite3`, `sqlite3`, Python, or a second database process. A native dependency would add platform build and release risk before a storage abstraction has proven necessary.

## Phase 1 — Migration-Owned MCP Ingestion

**Effort:** M
**Phase type:** architecture

| Order | Routing | Type | Commit Message | Files Affected | Goal |
|---|---|---|---|---|---|
| 2 |  | feat | `feat(mcp): add migration-owned observability store` | `tools/mcp/lib/observability-store.mjs`, `tools/mcp/migrations/001_observability.sql` | Add the owned store, transactional ordered migration runner, initial MCP tables/indexes, safe event validation/normalization, deterministic sessions, and SQLite task/routing-decision persistence. |
| 3 |  | refactor | `refactor(mcp): route task logs through observability store` | `tools/mcp/server.mjs`, `tools/mcp/lib/observability-store.mjs` | Replace direct JSONL append calls with the store’s one ingestion API, preserving the current JSONL file, fields, and non-blocking logging semantics. |

`001_observability.sql` is limited to the current MCP foundation: `schema_migrations`, `sessions`, `tasks`, `routing_decisions`, their required indexes, and an import-receipt/idempotency table if needed by the importer. It must not create `tool_activity` or Companion consent/retention tables yet. The migration runs in a transaction, records its version only after successful DDL, and is safe to run repeatedly.

The store maps `fast-path` to deterministic/fast-path and all other current MCP model routes to local/ollama. It writes nullable unknown token and cost values rather than fabricating values. The existing task-entry calculation may continue supplying its current estimates; this phase must not change routing or model invocation behavior.

## Phase 2 — Legacy JSONL Import

**Effort:** S
**Phase type:** feature

| Order | Routing | Type | Commit Message | Files Affected | Goal |
|---|---|---|---|---|---|
| 4 | [L1] | feat | `feat(mcp): import legacy JSONL task history` | `tools/mcp/scripts/import-mcp-jsonl.mjs`, `tools/mcp/lib/observability-store.mjs` | Provide an explicit, local-only importer for `~/.config/nexus/logs/mcp-tasks.jsonl` that normalizes legacy rows and records them exactly once in SQLite. |

The importer’s default source is the documented MCP JSONL file and it may accept an explicit local input path for tests and recovery. It must:

- migrate the database before reading input;
- skip blank or malformed JSON lines without aborting the remaining import, while returning a count of skipped lines;
- preserve repeated valid legacy records using a stable row identity derived from the raw line plus its occurrence in that import source;
- use a receipt/idempotency record so a second invocation imports zero additional copies of the same source rows;
- convert legacy millisecond `ts` values to UTC RFC3339; and
- leave the source file byte-for-byte untouched.

The implementation must document its recovery behavior for a truncated/rotated JSONL file in the command help or module documentation. It must never claim exactly-once semantics for two historically indistinguishable raw records after an external rewrite; the receipt model is exactly-once for a preserved source snapshot and safe to re-run.

## Phase 3 — Contract Verification

**Effort:** M
**Phase type:** polish

| Order | Routing | Type | Commit Message | Files Affected | Goal |
|---|---|---|---|---|---|
| 5 |  | test | `test(mcp): cover SQLite observability ingestion` | `tools/mcp/test/observability-store.test.mjs`, `tools/mcp/test/fixtures/mcp-tasks.jsonl`, `tools/mcp/test/fixtures/mcp-tasks-malformed.jsonl` | Lock down migration idempotency, dual-write compatibility, privacy allowlisting, failure isolation, and legacy import behavior using temporary local directories. |

## Acceptance criteria

1. Starting the MCP server on a clean local profile creates `~/.config/nexus/logs/observability.sqlite`, applies migration version 1 once, and creates no schema outside the migration runner.
2. Every existing MCP tool call continues producing a parseable JSONL line at `~/.config/nexus/logs/mcp-tasks.jsonl`. The pre-existing fields (`tool`, `model`, `routing`, `tokens_in`, `tokens_out`, `cloud_cost_equivalent`, `ms`, `ok`, `ts`, and optional `error`) retain their meanings; an additive event ID is permitted.
3. The same call creates exactly one SQLite `tasks` row, one appropriate synthetic/current `sessions` row, and one `routing_decisions` row with the documented source/provider/routing mapping.
4. The store rejects unknown event fields and values outside the safe task contract before they reach SQLite or JSONL. Tests prove strings resembling a prompt, response, URL, title, API key, and source diff are not stored.
5. A forced SQLite open/write failure does not change the MCP result and still attempts the compatibility JSONL write. A forced JSONL write failure does not change the MCP result or roll back a successful SQLite task row.
6. Running the importer twice against the supplied fixture yields the same SQLite task/session/decision counts after the second run, reports malformed lines, and leaves the fixture unchanged.
7. Imported legacy rows have UTC RFC3339 timestamps, correct fast-path/local mappings, stable daily sessions, and no invented token/cost values beyond fields present in the input.
8. `cd tools/mcp && npm test` passes on the declared Node runtime. Existing `cd tools/tui && go test ./...` still passes without a TUI code change.
9. No network request is made by migrations, ingestion, or import; no browser/desktop/Companion code is introduced.

## Master Commit List

1. [L1] `chore(mcp): declare native SQLite runtime`
2. `feat(mcp): add migration-owned observability store`
3. `refactor(mcp): route task logs through observability store`
4. [L1] `feat(mcp): import legacy JSONL task history`
5. `test(mcp): cover SQLite observability ingestion`

## Phase Dependencies

| Phase | Depends On | Can Run In Parallel With | Shared Files (conflict reason) |
|---|---|---|---|
| Phase 0 — Runtime Contract | Phase 0 | — | `tools/mcp/package.json` defines the runtime/test contract used by all later MCP work. |
| Phase 1 — Migration-Owned MCP Ingestion | Phase 0 — Runtime Contract | — | `tools/mcp/server.mjs` must switch only after the store exists; `tools/mcp/lib/observability-store.mjs` is introduced then extended by Phase 2. |
| Phase 2 — Legacy JSONL Import | Phase 1 — Migration-Owned MCP Ingestion | — | `tools/mcp/lib/observability-store.mjs` is extended with the migration-owned import API. |
| Phase 3 — Contract Verification | Phase 2 — Legacy JSONL Import | — | Test fixtures exercise the final store/import API and cannot be finalized before the Phase 2 source contract. |

## Collision scan

Two source files recur across planned work:

- `tools/mcp/lib/observability-store.mjs` is created in Phase 1 and extended in Phase 2. This is a hard dependency.
- `tools/mcp/server.mjs` is touched only in Phase 1. The current Go TUI remains untouched because it is the compatibility consumer of JSONL in this foundation.

The `package.json` runtime/test contract belongs exclusively to Phase 0; subsequent phases must not alter it unless the native API verification reveals a concrete compatibility defect, in which case that correction is a new sequential infrastructure commit.

## Notes

- The existing worktree has uncommitted changes to `README.md`, `ROADMAP.md`, and `docs/observability-schema.md`, plus the new `docs/nexus-companion.md`. They are deliberately excluded from this roadmap’s implementation commits.
- `docs/architecture-log.md` is absent in this repository, so no dated architecture-log decision was appended. Creating it is outside this scoped documentation task.
- There is no `.gemini-audit-report.json`; Greenfield mode applies.
- The existing TUI currently reads and rotates JSONL. Moving it to SQLite is intentionally deferred until the dual-write/import contract is proven. Its future SQLite reader must consume the same store/schema rather than create another database path.
- Browser extension and native-host work depend on Phase 3 passing. They must submit Companion activity through this store’s future typed activity API and a later migration, never by a direct SQLite connection or a second JSONL file.

> **Ready to execute?** Run `scripts/run-pipeline.sh` from the project root.
> The pipeline uses Opus to orchestrate, spawns parallel `agents-orchestrator` instances per phase (isolated worktrees), and tracks state via the native Task system.
> Flags: `--resume` (continue from last completed task), `--audit-only` (Gemini review without executing), `--skip-audit` (bypass gate for fast iteration), `--max-phases N`.
> `git-workflow-master` handles all branching, commits, and rebase hygiene.

---

## Addendum — v0.2.1 Issue #22: Optional Tokscale CLI Adapter

**Goal:** Add a local, read-only Go adapter that obtains model-usage aggregates
from an installed Tokscale CLI for the later unified TUI dashboard.

**Scope:** This addendum is intentionally limited to issue #22. It is sequenced
after the SQLite foundation above, but does not write to that database, change
JSONL ingestion, add a dashboard screen, install Tokscale, check its version,
or implement the health-check and graceful-degradation issues (#23–#26). The
adapter exposes a query result for those follow-on issues to render.

### Source contract

The production adapter executes exactly `tokscale models --json` through
`exec.CommandContext`. It does not invoke Tokscale's interactive TUI, use a
shell, interpolate user-supplied arguments, read AI-client logs itself, or make
network requests. The child CLI remains the sole collector for its supported
AI-client data.

Tokscale's current output is a JSON object with a `groupBy` string and an
`entries` array. NEXUS reads only these allowlisted entry fields:

| Tokscale field | NEXUS field | Notes |
|---|---|---|
| `client` | `Client` | CLI/tool identifier |
| `provider` | `Provider` | provider identifier |
| `model` | `Model` | model identifier |
| `input`, `output` | `InputTokens`, `OutputTokens` | counts |
| `cacheRead`, `cacheWrite`, `reasoning` | corresponding token counts | counts |
| `messageCount` | `MessageCount` | count |
| `cost` | `CostUSD` | cost aggregate |
| `sessionId` | `SessionID` | optional; empty when omitted |

Unknown top-level and per-entry fields must be ignored. This lets a newer
Tokscale add metadata without breaking the dashboard, while preventing NEXUS
from persisting, displaying, or logging that metadata. The initial command uses
Tokscale's default grouping; a later product decision may add fixed grouping
arguments, but #22 must not infer sessions from a model-only report.

### Adapter interface and failure contract

Create `tools/tui/tokscale.go` with a small internal seam that tests can
replace:

```go
type commandRunner interface {
	Output(context.Context, string, ...string) ([]byte, error)
}

type execCommandRunner struct{}

type TokscaleAdapter struct {
	Runner commandRunner
	Binary string // defaults to "tokscale"
}

type TokscaleReport struct {
	GroupBy string
	Entries []TokscaleUsage
}

type TokscaleUsage struct {
	Client, Provider, Model string
	InputTokens, OutputTokens int64
	CacheReadTokens, CacheWriteTokens, ReasoningTokens int64
	MessageCount int64
	CostUSD float64
	SessionID string
}
```

`execCommandRunner.Output` is the only production implementation and must call
`exec.CommandContext(ctx, name, args...).Output()`. `TokscaleAdapter.Load(ctx)`
uses the default binary when `Binary` is blank and passes the fixed arguments
`models`, `--json`.

Use `json.Decoder.UseNumber` plus narrow custom numeric decoding so integer
counts and decimal cost do not lose precision before validation. Accept a
missing optional `sessionId`; reject negative counts, negative cost, non-finite
numbers, numeric overflow, and a report whose top-level shape is not the
documented object with an `entries` array. Do not use `DisallowUnknownFields`.

Expose sentinel errors such as `ErrTokscaleUnavailable`,
`ErrTokscaleCommand`, and `ErrTokscaleMalformedOutput` (wrapping the underlying
error where safe). Behaviour is deliberately non-fatal to the TUI caller:

- a missing executable (`errors.Is(err, exec.ErrNotFound)`) returns an empty
  report plus `ErrTokscaleUnavailable`; a future UI can show NEXUS-native data
  and its install hint;
- a non-zero command exit or context cancellation returns no partial report and
  `ErrTokscaleCommand`;
- malformed JSON, an invalid envelope, or an invalid allowlisted value returns
  no partial report and `ErrTokscaleMalformedOutput`;
- diagnostics must never include raw stdout/stderr. This avoids accidentally
  surfacing newly introduced upstream fields in a terminal, JSONL, SQLite row,
  or error log.

### Atomic implementation plan

## Phase T1 — Adapter Boundary

**Effort:** S
**Phase type:** feature

| Order | Routing | Type | Commit Message | Files Affected | Goal |
|---|---|---|---|---|---|
| 6 |  | feat | `feat(tui): add optional Tokscale usage adapter` | `tools/tui/tokscale.go` | Introduce the typed, context-bound local command adapter, its strict allowlist parser, and privacy-safe error classification. |

The first implementation commit is intentionally source-only. It gives #23 one
stable query API before any Bubble Tea message, menu, health-check, or SQLite
integration can couple to the external CLI.

## Phase T2 — Adapter Contract Tests

**Effort:** S
**Phase type:** polish

| Order | Routing | Type | Commit Message | Files Affected | Goal |
|---|---|---|---|---|---|
| 7 | [L1] | test | `test(tui): cover Tokscale adapter failures` | `tools/tui/tokscale_test.go` | Use an injectable fake runner to lock down command arguments, parsing, forward compatibility, and non-fatal failure states without requiring Tokscale or network access. |

Test cases must cover:

1. the default runner receives `tokscale`, `models`, and `--json` with the
   supplied context (the production runner is separately constrained to
   `exec.CommandContext`);
2. a current `groupBy`/`entries` fixture maps every allowlisted field, including
   absent and present `sessionId` values;
3. unknown top-level and entry fields are ignored rather than retained in a
   NEXUS type or error;
4. a fake returning `exec.ErrNotFound` yields `ErrTokscaleUnavailable` and no
   entries;
5. fake non-zero/cancelled-command errors yield `ErrTokscaleCommand` and no
   entries; and
6. malformed JSON, wrong envelopes, negative/overflow/invalid numeric values
   yield `ErrTokscaleMalformedOutput`, no partial report, and an error string
   that does not contain fixture output.

### Acceptance criteria

1. `TokscaleAdapter.Load(context.Context)` runs only the fixed non-interactive
   command and accepts no dynamic CLI arguments.
2. A valid current report maps all listed metrics exactly and treats
   `sessionId` as optional.
3. Newly added Tokscale fields do not break parsing and cannot enter any NEXUS
   model, display value, log, or storage path.
4. Missing Tokscale, a failed invocation, cancellation, and malformed output
   are distinguishable to a later UI yet return no usage data and do not crash
   the TUI.
5. The adapter adds no network client, dependency, installer, persistence
   writer, interactive process, prompt/response capture, URL/title capture, or
   raw-output logging.
6. `cd tools/tui && go test ./...` passes without Tokscale installed.

### Dependencies and collision scan

| Phase | Depends On | Can Run In Parallel With | Shared Files (conflict reason) |
|---|---|---|---|
| Phase T1 — Adapter Boundary | Phase 3 — Contract Verification | — | `tools/tui/tokscale.go` is introduced here. |
| Phase T2 — Adapter Contract Tests | Phase T1 — Adapter Boundary | — | `tools/tui/tokscale_test.go` exercises the API created in Phase T1. |

There are no collisions with the SQLite foundation commits because this adapter
does not touch `tools/mcp/` or the existing TUI files. The two Tokscale phases
are sequential: tests codify the exported boundary introduced by Phase T1.

### Tokscale addendum commit list

6. `feat(tui): add optional Tokscale usage adapter`
7. [L1] `test(tui): cover Tokscale adapter failures`

---

## Addendum — NEXUS Companion Desktop and Release Operations

**Product direction:** NEXUS is one local-first system with four optional
surfaces: CLI, TUI, desktop Companion, and browser extension. The initial
desktop Companion supports Linux and macOS. It provides a visible top-bar or
menu-bar control when installed, while the CLI and TUI remain useful without
the app or extension.

This addendum plans the desktop shell and the operating discipline required for
versioned releases. It does not expand the Companion privacy contract in
[`docs/nexus-companion.md`](nexus-companion.md), modify the browser extension
scope, or authorize capture of browser content.

### Architecture decision — Tauri v2

Use **Tauri v2** for the initial Companion desktop shell. It gives Linux and
macOS builds a small native application, native menu/tray APIs, auto-start and
notifications where appropriate, and a Rust boundary suitable for the strict
native-messaging host. The existing Go TUI remains independent; do not embed it
in the desktop app or make Tauri a prerequisite for CLI/TUI operation.

The Tauri application has three deliberately narrow responsibilities:

1. show a local tray/menu-bar status surface and launch the TUI or a small
   local settings window;
2. own installation, enablement, diagnostics, and removal of the browser
   native-messaging host; and
3. submit only validated Companion activity envelopes through the existing
   migration-owned observability ingestion boundary.

It must not inject browser content scripts, inspect DOM/network/clipboard,
read titles or URLs, scrape generic processes, calculate browser token use, or
correlate Companion spans with tasks based on timestamps. The extension remains
responsible only for local origin-to-allowlisted-`tool_id` mapping, and the host
continues to reject unknown fields and senders.

### Tray and desktop expectations

On macOS, the app presents a conventional menu-bar item. It must clearly show
whether Companion collection is disabled, enabled with no active configured
surface, or recording an allowed surface; it must never display a site title,
URL, prompt, response, account, or project name.

On Linux, tray visibility is desktop-environment dependent. Modern GNOME does
not provide a universal legacy tray, and Wayland compositors differ in their
StatusNotifierItem/AppIndicator support. Therefore the app must:

- implement the supported Tauri v2 tray/status-notifier path, with documented
  testing on a KDE and an AppIndicator-capable Linux desktop;
- treat a visible top-bar icon as best effort on unsupported GNOME/Wayland
  setups, rather than promising one everywhere;
- retain a discoverable launcher/settings window and CLI status command when
  the tray is absent; and
- never use compositor/process inspection as a fallback for activity
  collection. Only documented, user-enabled adapters from the Companion design
  may emit events.

The app's visible state is a privacy control, not an activity feed. A detailed
local activity view belongs in the TUI after the dedicated `tool_activity`
schema, consent, retention, export, and deletion work are complete.

### Distribution tracks

#### macOS

Ship signed universal or architecture-specific `.app`/DMG artifacts through
GitHub Releases after obtaining the required Apple Developer credentials. The
release pipeline must use Developer ID Application signing, hardened runtime,
notarization, and stapling before publication. Store signing/notarization
credentials only in release CI secrets; never commit them or place them in
NEXUS configuration. Verify launch, tray/menu-bar behavior, first-run consent,
and native-messaging registration on both supported macOS architectures before
promoting a release.

#### Linux

Continue the existing GoReleaser `linux` CLI/TUI archives as one distribution
track. Add a separately tested Flatpak track for the Tauri Companion, beginning
with a Flathub-ready manifest and reproducible release artifact. Flatpak
sandboxing and browser native messaging require explicit design validation:
the packaged app must use only the minimal portals/permissions needed for its
own local data and documented host registration. Do not assume a sandboxed
Flatpak can register a native host in every browser installation. If browser
native messaging requires host-level registration beyond the Flatpak sandbox,
document that limitation and provide a separately packaged, audited host
installer rather than widening permissions or silently falling back to capture.

The Flatpak acceptance matrix covers at least one AppIndicator-capable desktop
and one tray-limited GNOME/Wayland desktop, including no-tray recovery through
the launcher and CLI. A successful Flatpak build alone is not evidence that
browser integration works.

### Milestone sequence

| Milestone | Deliverable | Exit gate |
|---|---|---|
| v0.2.1 completion | Shared SQLite ingestion and Tokscale/TUI work | Existing store and TUI tests pass; no desktop producer bypasses the store. |
| v0.2.2 private preview | Companion schema, consent/retention, Chrome/Edge extension, strict host, Tool Activity TUI | Privacy regression suite proves prompts, titles, URLs, and arbitrary fields cannot reach storage/export. |
| v0.2.2 desktop preview | Tauri v2 Linux/macOS shell with status controls and native-host lifecycle | Tray/no-tray matrix passes; macOS app launches; Linux package works without making tray visibility a collection dependency. |
| distribution preview | Signed/notarized macOS artifact and Flatpak beta | macOS signing/notarization verification and Flatpak sandbox/native-host compatibility decisions are recorded. |
| stable desktop release | Versioned public release | All release gates below pass, with matching user-facing documentation. |

Desktop adapters beyond the currently documented macOS foreground-app adapter
and compositor-specific Linux adapters remain later, separate milestones. No
generic process detector is a substitute for those adapters. Windows stays in
the v1.0.0 cross-platform scope already recorded in `ROADMAP.md`.

### Release operations

Every release candidate uses a single version, release date, and concise list
of user-visible changes across the release record, website, and announcements.
Do release preparation in small, reviewable, atomic commits: one logical change
per commit, tests with the code they verify, and no incidental formatting or
unrelated local work. Because `agent-nexus` and `nexus-site` are separate Git
repositories, commits cannot be atomic across both repositories; use an atomic
commit in each repository and record the paired commit SHAs in the release
checklist before publishing.

Before tagging or publishing, the release owner must complete these gates:

1. Run the relevant automated tests and platform packaging/smoke checks; verify
   the generated artifacts and checksums from `.goreleaser.yml` for the Go
   CLI/TUI, plus Companion-specific macOS/Flatpak checks when applicable.
2. Update `CHANGELOG.md` (create a conventional Keep a Changelog-style file if
   one is not yet present) with version, date, notable changes, upgrade notes,
   privacy-impact statement, and known platform limitations. Keep the generated
   GoReleaser GitHub changelog as supplemental metadata, not the sole release
   log.
3. Create the GitHub Release and attach the validated artifacts, checksums,
   install/update notes, and a link to the matching changelog section. Mark a
   preview/prerelease explicitly until its promotion gate is met.
4. Update the `nexus-site` repository with the same version, release notes,
   supported-platform/install instructions, Companion privacy statement, and
   known tray/Flatpak/browser-host limitations; deploy and verify the public
   page after its own review/commit.
5. Prepare and publish a concise Discord announcement and approved social
   posts only after the GitHub Release and website are live. State the version,
   primary benefit, supported platforms, install link, privacy boundary, and
   any preview caveat. Never imply universal Linux tray support or content
   capture.

The GitHub Release, website change, Discord/social copy, and changelog should
be drafted together, but social publication is the final gate so users always
land on working release documentation. Preserve release copy in the repository
(for example under `docs/releases/`) or in the GitHub Release body so each
announcement remains auditable and reproducible.

### Acceptance criteria

1. Linux and macOS Companion builds make the user-visible status/control
   available through the supported tray/menu-bar path, with an equally usable
   no-tray fallback on Linux.
2. Collection remains disabled by default and is governed by the existing
   per-browser/per-tool, device-local consent model; desktop installation alone
   produces no activity data.
3. The desktop app, extension, and host all write only through the typed,
   migration-owned observability boundary and cannot add arbitrary capture
   fields.
4. macOS public artifacts are signed, notarized, and stapled; Linux Companion
   Flatpaks have a documented sandbox/native-messaging support matrix and do
   not silently expand permissions.
5. Each published version has a changelog/release log, GitHub Release, deployed
   `nexus-site` update, and Discord/social announcement that agree on version,
   scope, platform support, and privacy guarantees.
