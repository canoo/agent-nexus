# Companion functional controls

The long-term target is full NEXUS desktop control, with companion foundations first.

The companion's collection policy and history controls can be operated independently
of the desktop dashboard. This is a functional command surface, not a new TUI screen
or a replacement for the rest of NEXUS configuration.

## Commands

Use the updated `nexus` binary with its matching repository/packaged helper root:

```sh
nexus companion status --json
nexus companion pause --json
nexus companion resume --json
nexus companion disable --confirm --json
nexus companion consent grant --adapter browser-chrome --tool chatgpt --acknowledge-policy 1 --json
nexus companion consent revoke --adapter browser-chrome --tool chatgpt --json
nexus companion data --json
nexus companion retention --days 14 --json
nexus companion prune --json
nexus companion clear --confirm --json
```

Policy 1 acknowledgement means consent to observe only fixed supported tool presence,
surface and start/end timestamps. No URL, title, prompt, response, page content,
account or project identifier is collected. Browser origin permission must still be
granted separately in the extension. A grant does not enable collection. Resume
requires at least one current supported grant. Chromium uses `browser-chrome` and
shares its desktop consent with Chrome; Edge uses `browser-edge`.

Supported tool IDs are `chatgpt`, `claude`, `gemini`, `copilot` and `perplexity`.
Desktop foreground collection is not implemented and cannot be enabled by these
commands. Unsupported adapters/tools and stale policy acknowledgements are rejected.

Pause preserves grants. Disable requires confirmation and revokes all grants in the
same transaction. Every explicit resume creates a fresh collection boundary; the
host rejects a whole span that began before resume or the latest tool consent.
Revoking one tool does not change another tool's grant or delete history.

Status is a fixed, bounded JSON snapshot: global collection, current policy, the ten
allowlisted browser/tool grants, retention and stored-span count. It returns no
activity records, paths or raw errors. It does not initialize, migrate or prune the
store. Privacy controls require an existing current schema-005 store and never
silently upgrade it. Use the existing documented upgrade path for old stores.

`NEXUS_COMPANION_NODE`, when set, must be an absolute existing executable. Invalid
configured runtimes fail without falling back to PATH. Packaged launchers should
pin the actual runtime rather than a version-manager shim. Data commands honor the
same override. Helpers have a 15-second process limit, bounded output and sanitized
errors. SQLite controls use a five-second busy timeout and immediate transactions.

## Remaining functional work before desktop design

| Capability | Evidence / remaining work |
| --- | --- |
| Chromium selected-tool collection | Live ChatGPT delivery, pause and resume verified on Saturn |
| Fine-grained policy controls | CLI/helper status, per-tool grants/revokes, pause/resume and confirmed disable; isolated ingestion/rollback tests |
| History controls | Existing count, retention, prune, clear and owned migrations |
| Runtime independence | Pinned Node, relocated packaged helper, no Node on PATH tested |
| Permission lifecycle | Worker-owned permission completion passes popup-destruction, denial, expiry and revocation tests; live prompt/regrant checks remain |
| Restarts and focus | Automated lifecycle tests exist; real browser/worker restart, app quit/reopen and multi-window checks remain |
| Additional tools | Five IDs implemented; live origin prompts and delivery beyond ChatGPT remain unverified |
| Firefox | Separate transport-only worktree prototype; production adapter/store identity, permissions and packaging still required |
| TUI integration | New commands are available to the CLI; companion activity screen remains read-only, no new interactive control screen |
| Full NEXUS desktop parity | Shared versioned backend now exposes every TUI menu area plus routing; desktop presentation remains separate |

The narrow collection core works on one native Linux/Chromium setup. The shared backend now covers the listed NEXUS domains; this does not imply that
every desktop view or live lifecycle scenario is complete. See [the shared protocol](nexus-control-protocol.md). Finish functional controls, permission persistence and
restart coverage before designing desktop workflows. Flatpak and macOS signing are
separate distribution work, not prerequisites for native Saturn use.

## Verification — 2026-10-28

- `cd tools/tui && go build ./... && go vet ./... && go test ./...`.
- `cd tools/tui && go test -run 'TestCompanionControls' -v .` covers a built CLI,
  relocated host payload, empty PATH, explicit consent, pause/resume, disable and
  fixed-response rejection in an isolated home.
- `cd tools/mcp && npm ci --cache /tmp/nexus-npm-cache && npm test` (80 tests).
- `node --test tools/mcp/test/companion-controls.test.mjs` covers old-store refusal,
  read-only status, explicit grants, real ingestion boundaries and transaction rollback.
- `cd apps/companion-native-host && npm test` (40 tests).
- MCP Zod pin updated to patched `4.6.7`; `npm audit` reports zero vulnerabilities.

No live user consent/history was modified by these automated tests. No desktop
Rust/UI files were edited for this work; existing local desktop edits are preserved.
