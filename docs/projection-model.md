# Tool projection and ownership

The core work branch implements project-local `nexus adopt`, `nexus sync`, and
`nexus rollback`. `.nexus/source.md` is the canonical instruction Markdown.
AGENTS.md is always the primary projection target. Driver outputs, original
imports, validation findings, and ownership hashes are explicit; settings are
not equivalent to instruction text.

```sh
nexus adopt --dir ./project --dry-run
nexus adopt --dir ./project
# Review conflicting instructions and edit project/.nexus/source.md.
nexus sync --dir ./project --dry-run
nexus sync --dir ./project
nexus rollback --dir ./project --dry-run
nexus rollback --dir ./project
```

All commands default to the current directory. No command reads or changes the
user's global tool configuration. `--dry-run` validates and prints proposed paths
without creating files or directories. Detection uses project artifacts and known executable names on PATH (without
executing them during adopt/sync). File-level behavior is tested; live
vendor loading is unverified.

## Source, outputs, and ownership

Adoption concatenates supported existing instructions into source sections with
path provenance. `.nexus/projection.json` records versioned source SHA-256 hashes
and managed-block ownership hashes. It then projects the source into AGENTS.md
and each detected supported target. Edit `.nexus/source.md`, then run sync.

Managed blocks include this exact header:

```html
<!-- Managed by NEXUS — edit .nexus/source.md, not this file -->
```

The header sits between `<!-- nexus:instructions:start -->` and
`<!-- nexus:instructions:end -->`. Existing text outside that block is preserved.
The whole file is not owned by NEXUS. This deliberately protects local work:
changed, missing, duplicated, or malformed managed blocks fail instead of being
silently overwritten. Removing the header is a block edit and causes failure.
The manifest is required to validate ownership; the generated validation report
is also ownership-hashed and refuses intervening edits. There is no force flag.

Imported originals remain in place, so imported text can appear both outside and
inside the block until manually reconciled. Contradictory policies require human
review. Repeated adoption leaves an existing canonical file alone and rejects
changed sources; repeated sync with unchanged inputs is idempotent. Existing
unowned dedicated Cursor/Kiro output files fail instead of being overwritten.
Symlinks inside the resolved project root are rejected. Platform/root aliases
(such as macOS `/var`) are resolved once before validating paths.

## Driver capabilities

Every listed output carries project-wide instruction text. Existing vendor settings,
MCP registration, credentials, personas, and routing configuration are not merged.

| Driver | Output | Detection | Import |
|---|---|---|---|
| AGENTS | `AGENTS.md` | Always primary | Yes |
| Claude | `CLAUDE.md` | Existing file, `.claude/`, or `claude` on PATH | Yes; compatibility output |
| Gemini | `GEMINI.md` | Existing file, `.gemini/`, or `gemini` on PATH | Yes; compatibility output |
| GitHub Copilot | `.github/copilot-instructions.md` | Existing target or `copilot` on PATH | Yes |
| Windsurf | `.windsurfrules` | Existing file, `.windsurf/`, or `windsurf` on PATH | Yes, legacy plain file |
| Cline | `.clinerules` | Existing target or `cline` on PATH | Yes, legacy plain file |
| Kiro | `.kiro/steering/nexus-project.md` | `.kiro/`, `kiro-cli`, or `kiro` on PATH | Other steering Markdown only if unconditional |
| Cursor | `.cursor/rules/nexus-project.mdc` | `.cursor/` or `cursor` on PATH | Other MDC files only with `alwaysApply: true` |

Kiro output uses `inclusion: always`; Cursor output uses `alwaysApply: true`.
Changed output frontmatter fails validation. Scoped, conditional, unknown
frontmatter and unresolved vendor file references are reported as unsupported
rather than flattened into AGENTS.md. Unconditional Kiro steering Markdown and
always-apply Cursor MDC are imported with recognized frontmatter removed; intact
originals are archived. Legacy `.cursorrules` and `.qrules` are import-only.
Copilot `.github/instructions/*.md` files are inventoried; scoped frontmatter is
unsupported. Modern directory-form Cline/Windsurf rules, nested AGENTS.md scope,
and unlisted tools lack projection drivers. Continue.dev/Aider settings are
inventoried but not projected. This does not change installer behavior or
remove legacy CLAUDE.md support.

## Validation and non-equivalent configuration

`AGENTS.validation.json`, alongside AGENTS.md, lists detected importable and
unsupported paths, reasons, and the validation contract. The CLI also prints
`review <path>: <reason>` for unsupported files. No model is called and no source
content is sent to a provider. The report always requires manual review: format
validation cannot establish semantic policy equivalence or resolve contradictory
instructions. Intelligent model-assisted merging and interactive conflict
resolution from issue #82 remain future work.

Claude/Gemini settings, Cursor/Kiro MCP settings, Continue.dev configuration,
root `config.yaml`, and Aider config are detected by path. Their contents are
never imported, archived, or included in the report: hooks, MCP environment
secrets, sandbox permissions, session state and settings must be validated with
the originating tool. Unknown JSON bodies in instruction files are rejected as
non-equivalent configuration. Tool-specific `@file`/Kiro file references require
manual resolution. Absence of a finding is not proof that every vendor feature
was detected; this is a conservative supported-file inventory.

Before trusting the result:

1. Review `.nexus/source.md` for contradictions and remove obsolete imported text.
2. Inspect `AGENTS.validation.json` and reconcile every unsupported construct in
   its original tool; do not paste settings or secrets into instruction Markdown.
3. Run `nexus sync --dry-run`, then sync and inspect the Git diff.
4. Verify the target tool actually loads the intended instructions in a live
   session. The CLI does not certify vendor behavior.

## Archives, snapshots, and rollback

Adoption copies the exact original bytes of imported instruction files under
`.nexus/.adopted/<original-relative-path>` before modifying any projections.
Archives are not overwritten, and settings/secrets are not archived. Original
files are retained, not moved. Newly created archive directories use mode 0700
and archive files mode 0600.

Before a mutating adopt/sync, changed files are snapshotted to a private JSON
bundle under `.nexus/.snapshots/` (0700 directories, 0600 bundles). Bundles store
old bytes as base64, existence and permission bits, and the expected post-sync
hash. `latest.json` selects the most recent operation; timestamped bundles retain
history. No-op sync and dry-run create no snapshot.

Rollback validates every expected post-sync hash first. Any intervening edit,
including surrounding user text, causes a conflict and no writes. Otherwise it
restores old bytes/permissions and removes files created by that operation.
It consumes the latest marker so repeated rollback cannot silently undo another
operation. It does not walk snapshot history automatically. Archives and protective `.nexus/.gitignore` rules remain as
safety measures. A source file edited before sync remains edited: rollback restores
the files sync actually changed, not earlier manual source edits.

All targets are preflighted before writes; detected content conflicts cause no
writes. Each replacement is atomic, but the multi-file operation is not
transactional. Disk/permission failures can leave earlier files written/restored;
recover using the snapshot and version control. The ownership manifest is written
last. Concurrent runs/edits are unsupported. No symlink or local-edit bypass flag
is provided.

## Team usage

Commit `.nexus/source.md`, `.nexus/projection.json`, `.nexus/.gitignore`, generated
instruction targets, and `AGENTS.validation.json`. Adopt/sync add `.snapshots/`
and `.adopted/` to `.nexus/.gitignore`, preserving any existing content there and
leaving the root `.gitignore` alone. Keep private snapshots and archives out of
Git; they may contain sensitive historical instruction text. Settings and
machine-specific secrets belong in tool-local ignored files, never canonical
instructions or version control.

## Compatibility catalog

`tools/compat/tools.json` (schema version 1) records the eight supported file
contracts. `nexus compat --dir ./project` and `nexus compat --json` show project
markers separately from installed executables and detected versions. Version
queries invoke only known binaries with literal `--version`, with time and output
bounds. An installed executable does not certify its instruction loading behavior.
Minimum vendor versions and tested ranges are unknown; every vendor version is
unverified. Missing executables and file-only partial detection remain visible.
The registry accepts optional stable numeric `x.y.z` minimum versions and inclusive
`x.y.z..x.y.z` tested ranges. Unknown, partial, prerelease and build-suffixed
versions remain unverified; below-minimum and outside-range versions warn. Being
inside a recorded range establishes only its file contract, not full vendor
feature support. The bundled catalog has no vendor version evidence.

The Tool Compatibility TUI screen loads locally in an asynchronous worker. `r`
rechecks local state, `u` explicitly refreshes the official registry, and `s` runs
sync for the project directory captured from the working directory on entry.
Sync reports ownership conflicts and requires an adopted canonical source; it
does not auto-adopt. Opening the screen performs no network request.

`nexus compat update` (also `nexus update --compat`) explicitly fetches the fixed
official HTTPS registry into a separate user cache. This alias updates only the
compatibility catalog; it is separate from binary installation/update. Redirects,
oversize responses, unknown schemas/drivers and unsupported evidence are rejected.
Failed refresh leaves existing data unchanged. The checkout is never overwritten;
a checksum detects user edits to the cached catalog and refresh refuses to replace
them. Preserve edited data elsewhere before removing the cache to retry. Before
the new registry exists on main, the official endpoint may return 404; that live
refresh path remains unverified. No vendor version claims follow from a successful
registry download.

TUI architecture decision: retain #56's completed screen-file split and defer
#100's broader package extraction. This screen adds one screen file and thin root
wiring; a wider refactor needs separate evidence and scope.
