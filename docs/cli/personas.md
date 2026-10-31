# Persona packages

`nexus persona list` lists the official `canoo/Nexus-Personas` registry. Add
`--json` for its versioned catalog (`version`, `count`, `personas`). The registry
is read from the repository's default branch through the GitHub contents API.

```sh
nexus persona list --json
nexus persona install bubbletea-tui
nexus persona install bubbletea-tui --global --json
```

Installation defaults to the current directory's
`.agents/skills/<id>/SKILL.md`. `--global` instead writes
`~/.config/nexus/personas/<id>.md`, the existing NEXUS persona registry location.
It does not edit AGENTS.md, project configuration, or tool permissions. Repeating
an installation with identical bytes succeeds without changes. A different
existing file is preserved and causes an error: save your local changes and
explicitly remove the destination if you intend to replace it. There is no
force-overwrite or automatic update operation.

The downloaded persona Markdown is preserved byte for byte. This is a package
installation, not a translation into a harness's skill schema or a guarantee that
a particular harness loads it. Registry display names and `allowedTools` metadata
may have different meanings or constraints in individual tools. In particular,
`allowedTools` is not converted into enforceable Codex permissions. Review a
persona and the consuming harness's requirements before enabling or using it.
The installer neither executes downloaded content nor automatically activates it.

The current upstream registry schema is version `1.0.0`, with `count` and a
`personas` array. Entries contain `id`, `name`, `description`, `path`, `rawUrl`,
and optional display/tool metadata. IDs must use lowercase letters, digits and
hyphens; paths must be exactly `personas/<id>.md`; raw URLs must use HTTPS and
name the matching file in `canoo/Nexus-Personas` on `raw.githubusercontent.com`.
The installer validates required YAML `name`/`description` frontmatter against
the registry, checks known frontmatter field types, and requires a nonempty
Markdown body. It accommodates v1's registry builder preserving quoted
description escapes and truncating physically multiline descriptions to their
first source line. Optional display colors are preserved as strings: current
upstream entries use `red`, `orange`, and `yellow` even though the upstream
schema's enum excludes them. Installation does not assert full conformity with
that inconsistent display enum. Unsupported registry
versions, duplicate IDs, unsafe paths, mismatched metadata, or symlink
destinations are rejected before writing.

The current registry has no checksum field. The installer obtains file bytes
and Git blob SHA metadata through the official GitHub contents API and verifies
the Git SHA-1 blob hash (including Git's blob header). If an entry additionally
supplies `sha256`, that digest is also required to match. These checks detect
inconsistent/corrupt bytes; they are not publisher signatures or independent
proof of authenticity. Trust rests on HTTPS and the official GitHub repository.
Registry and persona reads resolve the default branch separately; a concurrent
upstream edit can cause validation to fail safely. Files are not pinned as a
cross-file release snapshot.

HTTP requests have a 15-second timeout, the command has a 30-second deadline,
and redirects must remain on the official repository's HTTPS contents API
(up to three requests). API responses are capped at 2 MiB and decoded files at
1 MiB. Rate limits, offline operation, invalid metadata and integrity failures
return a nonzero exit status without installing a file. There is no offline
cache or authentication/token handling in this initial version.

`--json` emits a catalog for `list`, or `{id,path,changed,integrity}` for a
successful `install`. Failures emit `{"error":"..."}` and exit 1 (source or
installation failure) or 2 (invalid arguments). Human-readable errors go to
stderr. Installation JSON reports `git-blob-sha1` integrity; a supplied SHA-256
is verified in addition.
