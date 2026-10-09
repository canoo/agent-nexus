# NEXUS Preview Artifact Guide

## Scope & Prerequisites
These preview artifacts are **development build evidence**, not a v0.3.0 release; they carry no tested live OS/browser compatibility claims. macOS signing/notarization, and GUI/browser validation remain gating requirements.

- **Artifacts:** Linux `.deb` or unsigned/unnotarized macOS `.app`, standalone `host/` source payload, `extension/` unpacked development extension, and `installer/` CLI.
- **Node.js:** External Node 22.13+ required. The desktop binary is **not** bundled into the host payload.
- **Local store:** Opening the dashboard creates nothing. With runtime/helper paths bound, acknowledge setup and choose **Create local store** to initialize a missing store with collection off and no grants. Existing stores are never replaced. Browser registration and consent remain separate explicit actions; live first-run GUI validation remains a gate.

## Installation & Configuration

1. Extract `host/`, `extension/` and `installer/` side-by-side into a stable, trusted absolute directory outside the source repo.
2. Install the platform desktop package (`.deb` or `.app`).
3. Run the launcher generator (all four flags require absolute paths; `--output` must not exist):

```bash
/absolute/node /absolute/preview/installer/apps/companion-native-host/bin/nexus-companion-runtime-launchers.mjs \
  --runtime-root /absolute/preview/host \
  --output /absolute/new/launchers \
  --node-path /absolute/node \
  --desktop-path /absolute/installed/desktop
```

4. Open the generated `nexus-companion` launcher. If the store is missing, explicitly acknowledge setup and choose **Create local store**. Existing stores use the normal NEXUS upgrade path.
5. Load the archived `extension/` directory as an unpacked development extension in Chrome or Edge (see below). Keep its path stable, note the actual ID shown by that browser, and verify it belongs to this preview.
6. In the dashboard, select that browser, supply its verified extension ID and the matching generated browser launcher path, and choose **Register this native host**. The registration helper CLI remains available for scripts.
7. Browser origin permissions/tool consent and desktop consent/resume are separate explicit actions. Confirm both before testing collection; installing or registering alone enables nothing.

On Linux the installed executable is typically `/usr/bin/nexus-companion`; on macOS use `/Applications/NEXUS Companion.app/Contents/MacOS/nexus-companion`. Supply the actual existing executable path. These unsigned development artifacts do not replace signed/notarized distribution. The Linux build targets Ubuntu 24.04 CI; other distributions and desktop environments remain unverified. Keep all paths stable after binding and re-register if they change. Removing browser registrations or launchers preserves history.

## Existing-store upgrades

Initialization refuses an existing store. Owned migrations apply when a native
host starts, or when you explicitly run `nexus companion prune --json` with the
updated NEXUS CLI. Pruning and host startup also apply your raw-history retention
policy; MCP tasks, import markers and tool grants are retained. Older enabled
preview stores pause once on migration 005 because they lack a trusted resume
boundary. Review status and explicitly resume after upgrading. Packaged process
regressions cover this path; the live installed GUI/browser flow remains unverified.

## Remove a browser registration

In the dashboard, select Chrome or Edge and choose **Remove this native host**.
This removes only that browser's NEXUS manifest. Collection state, grants, history,
and other browser registration remain unchanged. Pause/revoke separately to stop
collection. Remove registrations before deleting payload or launcher files.

## Unpacked development extension

The archive includes the extension runtime and its README; it excludes tests,
package tooling, npm dependencies and user data. Its browser version is `0.3.0`
and its displayed version name identifies the development build. Only version
metadata changes during staging; permission/origin policy is preserved.

Follow the official [Chrome unpacked-extension instructions](https://developer.chrome.com/docs/extensions/get-started/tutorial/hello-world#load-unpacked)
or [Edge sideloading instructions](https://learn.microsoft.com/en-us/microsoft-edge/extensions/getting-started/extension-sideloading):
open `chrome://extensions` or `edge://extensions`, enable Developer mode, select
**Load unpacked**, and choose the archive's `extension/` directory. Use the ID
shown in that browser when registering its host. No publisher key or store
identity is embedded, so do not assume both browsers or relocated directories
have the same ID. Moving it can require removal and re-registration.

This is a development install, not a Chrome Web Store or Edge Add-ons release.
No live installation or published extension identity is claimed. Browser-store
publication/identity and installed GUI/browser lifecycle checks remain release gates.
