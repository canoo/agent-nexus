# Saturn Linux live preview setup — 2026-10-28

## Installed preview

The Chromium-enabled `0.3.0-dev.2` preview is installed under:

```
/home/cano/.local/share/nexus-companion-preview/0.3.0-dev.2
```

Open **NEXUS Companion** from the application launcher or run:

```sh
/home/cano/.local/bin/nexus-companion
```

The user desktop entry is `~/.local/share/applications/nexus-companion.desktop`.
This is a user-owned Arch/Omarchy preview installation, extracted from the
verified Linux `.deb`; no Debian package manager or system package was installed.
It has no autostart entry. The generated launcher pins the actual Node executable
instead of the mise shim, and binds helpers to the installed payload.

The dashboard launched successfully on Hyprland/Wayland: the compositor reported
an actual mapped native Wayland window titled **NEXUS Companion**, 520×390.
This proves window launch, not visual correctness, tray interaction or browser delivery.
The only initial log message was an ayatana-appindicator deprecation warning.
Launch diagnostics are in the installation directory's `launch.log`.

The existing local database has migrations 001–005 and collection disabled.
No database initialization, policy change, history deletion or browser registration
was performed as part of installation.

## Browser steps

1. In native Chromium, open `chrome://extensions`, turn on **Developer mode**,
   choose **Load unpacked**, and select:
   `/home/cano/.local/share/nexus-companion-preview/0.3.0-dev.2/extension`.
2. Copy the assigned 32-character extension ID. Keep this directory stable.
3. In the companion dashboard, select **Chromium (Linux)** and supply that ID
   and this executable host path:
   `/home/cano/.local/share/nexus-companion-preview/0.3.0-dev.2/launchers/nexus-companion-native-host-chromium`.
   Choose **Register this native host**. This changes only Chromium's manifest.
4. Read the disclosure and explicitly grant the desired tool under
   **Chrome / Chromium**. These desktop consent grants are shared by the
   Chrome-family adapter; extension origin permissions remain separate per browser.
5. Open the extension popup/options and enable the same tool, approving its
   requested origin permission. Then explicitly **Resume collection** in the dashboard.
6. Visit the supported tool in a focused tab for a few seconds, then switch to
   another tab/window. Delivery happens when the observed span ends. Ask the agent to read the
   installed helper’s status and check that the stored-span count increases.
7. Pause collection and repeat the transition: the count must stay unchanged.
   Resume, start a fresh observation, switch away, and confirm delivery returns.
   Test revoke/regrant and browser restart similarly before claiming live support.

Do not paste URLs, page titles or conversation content for verification. The
companion stores only fixed tool identifiers, surface and bounded timestamps.
The browser ID can be supplied to the agent to complete registration; collection
consent and browser origin grants remain explicit user actions.

## Code and artifact checks

Base: `776672f98492061d31f4817280205c30b1585bd6`. The intervening main merge
changed Go dependencies, the MCP lockfile and a workflow; it did not change companion
source or macOS files. No remote companion collision was observed before editing.

Passed commands:

- `cd tools/tui && go build ./... && go vet ./... && go test ./...` (includes memory end-to-end).
- `cd tools/mcp && npm test` (71 tests).
- `cd apps/companion-native-host && npm test` (40 tests).
- `cd apps/companion-desktop && npm test` (18 tests).
- `cargo test --manifest-path apps/companion-desktop/src-tauri/Cargo.toml --locked` (16 tests).
- `cargo fmt --manifest-path apps/companion-desktop/src-tauri/Cargo.toml -- --check`.
- `git diff --check`.
- `CARGO_TARGET_DIR=/tmp/nexus-linux-preview-target node tools/automation/build-companion-preview.mjs --version 0.3.0-dev.2 --output /tmp/nexus-linux-preview-20261028`.
- `node tools/automation/verify-companion-preview.mjs /tmp/nexus-linux-preview-20261028`.

The verifier additionally exercises the generated Linux Chromium wrapper. It checks
hashes/modes, extension policy, isolated setup/upgrades and fail-closed behavior.
Chromium uses the existing Chrome protocol adapter; no new database adapter,
permissions or macOS Chromium target was added.

The installed helper’s read-only `status` command returned `storedSpans: 0` and
`retentionDays: 14`; `desktop-file-validate` accepted the desktop entry.

Live Chromium extension loading, permission prompts, real delivery, pause/resume,
tray interactions and restart behavior still need user-assisted verification.
Flatpak remains unsupported and is unnecessary for this native installation.
