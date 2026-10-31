# Shared NEXUS control protocol (version 1)

The desktop's target is the full NEXUS feature set. Both interfaces use the same
operations and stores; a desktop-specific implementation of install, configuration,
memory or routing is deliberately avoided. `nexus control --json` is a local,
one-request-per-process transport over stdin/stdout. It opens no HTTP listener,
accepts no shell expressions and creates no background service.

```sh
printf '%s\n' '{"schemaVersion":1,"operation":"capabilities"}' | nexus control --json
printf '%s\n' '{"schemaVersion":1,"operation":"config.get"}' | nexus control --json
```

Every response has `schemaVersion`, `ok` and either `operation`/`result` or a fixed
`error` code. Requests are limited to 64 KiB, results to 4 MiB. Unknown fields,
parameters, protocol versions, trailing JSON and unconfirmed destructive operations
are rejected. There is no arbitrary command runner or caller-provided script path.
A client must not silently set confirmation or cloud acknowledgement on behalf of
its user. A process is allowed to finish an explicit operation; lifecycle operations
may take minutes, so a desktop should keep them off its UI thread.

## Operations

| Operation | Parameters | Existing implementation shared with TUI/CLI |
| --- | --- | --- |
| `capabilities` | none | Versioned operation catalog; menu-coverage test prevents forgotten TUI areas |
| `status.get` | none | Local executable/persona inventory |
| `config.get` | none | The five current TUI configuration settings, defaults and environment precedence |
| `config.set` | `values`: map of owned setting names to strings; `confirm:true` | Existing atomic settings writer and retention normalization; preserves unowned settings |
| `compat.get` | optional `dir` (defaults to cwd) | Local file-contract registry, project markers and bounded known CLI version probes; no registry refresh or sync |
| `health.check` | none | Ollama, GPU, link status, optional Tokscale and Claude-retention guidance |
| `tasks.list` | none | Same latest-50-row SQLite mapping through a read-only connection |
| `usage.get` | none | Native task metrics and optional Tokscale aggregates, kept separate; no raw session identifiers |
| `activity.list` | none | Existing allowlisted read-only tool/surface/time view, at most 50 rows |
| `companion.command` | `args`: existing companion CLI arguments including `--json` | Privacy/data helpers, explicit policy acknowledgement and clear/disable confirmation |
| `memory.command` | `args`: existing memory CLI args; optional `input` for stdin content | Project list/init, list/read/search, create/import and fixed component validation |
| `memory.delete` | `project`, `name`; `confirm:true` | Same TUI delete helper and component checks |
| `install.run` | none; `confirm:true` | All existing ordered install workers and critical-step stop rules |
| `update.check` | none | Existing release-version worker |
| `update.apply` | `version`; `confirm:true` | Existing checksum-verified release installer |
| `uninstall.run` | none; `confirm:true` | Existing fixed repository teardown script |
| `route.preview` | `prompt`; optional `allowCloud` | Existing classifier, no provider request |
| `route.run` | `prompt`, optional `allowCloud`; `confirm:true` | Existing routing command; cloud-classified work and fallback additionally require `allowCloud:true` |

Confirmation is a top-level request field, not a parameter. For example:

```json
{"schemaVersion":1,"operation":"config.set","confirm":true,"params":{"values":{"NEXUS_LOCAL_AI":"false"}}}
```

`memory.command` intentionally exposes the existing local file-import command;
content is read or written only through an explicit memory request. It is not an
automatic scanner. `companion.command` returns the underlying command's fixed JSON
and exit code; collection consent remains separate from general lifecycle permission.

## Desktop bridge and runtime binding

The backend-only Tauri command is `nexus_control`. It invokes a pinned
`NEXUS_CONTROL_BINARY` with exactly `control --json` and sends one JSON request over
stdin. `NEXUS_CONTROL_ROOT` must identify the trusted full NEXUS repository; a
standalone native-host payload is insufficient for installer/configuration/routing
features. It never uses the native host's payload root as the full NEXUS repo.

The shell command bridge does not provide a new GUI layout. Existing dashboard
controls continue to work while full NEXUS views can be added against this protocol.
Rust/Go unit and process tests cover protocol behavior; destructive lifecycle and
real provider actions are verified only in isolated fixtures, not run against the
user installation during development.

## Extending the shared surface

Add the operation once beside the worker that implements it, register its capability,
and cover its request/response contract. TUI and desktop adapters should call that
same worker rather than fork business logic. Every TUI menu area must have a
catalog entry; tests fail if a new menu area lacks shared coverage. This is a parity
guard for operation availability, not a claim that every desktop view already exists.
Future TUI updates still need operation-level acceptance tests and an explicit
client presentation; catalog discovery does not automatically design a screen.

The worker now owns permission completion across popup dismissal, with bounded
requests and tests for denial, expiry and revocation. This is automated evidence;
remaining foundation gates are live first-click permission persistence, browser and
worker restart, revoke/regrant and other-tool coverage. Firefox remains an isolated
transport experiment until its own adapter, independent consent, native manifests,
packaging and live browser tests are implemented. Flatpak and macOS distribution
remain separate from native Linux functionality.
