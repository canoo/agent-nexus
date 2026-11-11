# Saturn functional rollout — 2026-10-29

The target is full NEXUS desktop functionality backed by the same operations as
the TUI, with companion foundations completed before desktop workflow design.

## Shared implementation

Integration branch `feat/v0.3.0-integration` includes:

- `87c3b7c`: worker-owned browser permission completion across popup dismissal.
- `0ce69f3`: bounded status, per-tool consent, pause/resume and confirmed disable
  commands, alongside the existing history controls.
- `5e4932f`: versioned Go control transport and backend-only Tauri bridge.
- `035378d`: patched Zod 4.6.7; installed MCP dependencies audit clean.

The catalog has 17 operations covering all nine current TUI menu areas plus
routing. It delegates to existing TUI workers/stores rather than implementing a
second install/configuration/memory/routing engine. Tests require every TUI menu
area to have a capability. This guards availability, not automatic desktop layout
or complete acceptance coverage of every future subfeature.

Desktop views for the broader feature set have not been designed or connected
to individual controls yet. The command transport is available for that work.
See [the operation contract](nexus-control-protocol.md) and
[companion command details](companion-functional-controls.md).

## Installation and live evidence

The installed desktop is:
`~/.local/share/nexus-companion-preview/0.3.0-dev.4/desktop/nexus-companion`.
`~/.local/bin/nexus-companion` points to its runtime launcher, which pins:

- `NEXUS_CONTROL_BINARY=~/.local/share/nexus-control/0.3.0-dev.4/nexus`
- `NEXUS_CONTROL_ROOT=/home/cano/repos/agent-nexus`
- The actual Node executable rather than a mise shim.

The actual process binding was checked without dumping other environment values.
Hyprland reported a mapped native Wayland window titled NEXUS Companion, 588×649,
with no startup error; the only initial log was the ayatana-appindicator
deprecation warning. The build includes the three pre-existing local renderer,
window and styling edits exactly as provided. Those edits remain uncommitted and
were not rewritten or included in the backend commits.

Pinned user commands are installed at `~/.local/bin/nexus` and
`~/.local/bin/nexus-control`. For example:

```sh
nexus companion status --json
printf '%s\n' '{"schemaVersion":1,"operation":"capabilities"}' | nexus-control --json
```

The updated extension was copied to its existing loaded Chromium directory:
`~/.local/share/nexus-companion-preview/0.3.0-dev.2/extension`.
This preserves its assigned ID. The old files were verified against the original
manifest and backed up before replacement under
`~/.local/share/nexus-companion-extension-backups/dev2-before-dev4-20261029`.
The dev.4 installation contains `installed-extension-update.json` with the new
source manifest hash and installed extension hashes. The original dev.2 preview
manifest remains a record of the original artifact, not the updated live files.

User-assisted live checks:

| Step | Observed outcome |
| --- | --- |
| Desktop update/restart | Collection and ChatGPT consent remained enabled; stored spans stayed at 4 |
| Reload extension, disable/re-enable ChatGPT, approve prompt, reopen popup | User confirmed the checkbox stayed checked |
| Focus ChatGPT and switch away after extension reload | Stored spans increased from 4 to 6 |
| Close all Chromium windows, reopen, repeat observation | User confirmed consent persisted; stored spans increased from 6 to 7 |

Only ChatGPT consent is enabled. Collection was left enabled following the user's
explicit resume. No real history was cleared, retention changed, or extra tools
granted. Production Chrome-family collection remains separate from the Firefox
experiment.

## Verification

Previously completed code checks for this rollout:

- `cd tools/tui && go build ./... && go vet ./... && go test ./...` passed,
  including memory, relocated packaged privacy CLI, shared protocol, configuration,
  real memory stores, and isolated install/teardown fixtures.
- `cd tools/mcp && npm test`: 80 passed.
- `cd apps/companion-native-host && npm test`: 40 passed.
- `cd apps/companion-browser-extension && npm test`: 40 passed.
- `cd apps/companion-desktop && npm test`: 18 passed; formatting check passed.

Final checks on the actual installed backend/source combination:

```sh
CARGO_TARGET_DIR=/tmp/nexus-linux-preview-target node tools/automation/build-companion-preview.mjs --version 0.3.0-dev.4 --output /tmp/nexus-linux-preview-20261028-dev4
node tools/automation/verify-companion-preview.mjs /tmp/nexus-linux-preview-20261028-dev4
NEXUS_CONTROL_TEST_BINARY=/home/cano/.local/share/nexus-control/0.3.0-dev.4/nexus NEXUS_CONTROL_TEST_ROOT=/home/cano/repos/agent-nexus cargo test --manifest-path apps/companion-desktop/src-tauri/Cargo.toml --locked
cd tools/mcp && npm ci --cache /tmp/nexus-npm-cache && npm audit
```

The Linux `.deb` build and artifact verification passed; Rust passed 24 tests,
including real Rust-to-installed-Go capability transport and process-group
cancellation. npm reports zero vulnerabilities. Destructive lifecycle/provider
actions were not executed against the user's installation for these tests.

Before integration, an actual `git merge --no-commit --no-ff` succeeded in a
disposable worktree. The finish branch was fast-forwarded, existing GUI changes
restored and compared against their backup, and the clean controls worktree pulled
with rebase immediately before the normal push to PR #115.

## Firefox and remaining work

Firefox transport is isolated on `origin/codex/companion-firefox` at `dc422b5`.
Its nine tests passed after rebasing onto the shared-backend integration. It was
not merged into production. The staged native Firefox 158 probe has only the
`nativeMessaging` permission and sends a fixed constant message; it has no tab,
origin or storage access and no production activity/database code.

The temporary add-on source is
`~/.local/share/nexus-firefox-probe/0.0.1/extension/manifest.json`.
Its separate native host is registered as
`com.codelogiic.nexus.firefox_probe`, with the fixed experimental Gecko ID only.
This does not enable Firefox collection. Live probe confirmation is tracked
separately from the passing process/VM transport tests.

Remaining foundation checks include independent multi-tool permissions/delivery,
multiple browser windows, installed browser-host removal/re-registration, and
additional desktop environments. Production Firefox still needs its own activity
family, independent consent adapter, validation/upgrade integration, registration,
packaging and live collection tests. Full desktop presentation, Flatpak and macOS
distribution remain distinct workstreams.
