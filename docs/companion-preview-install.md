# NEXUS Preview Artifact Guide

## Scope & Prerequisites
These preview artifacts are **development build evidence**, not a v0.3.0 release; they carry no tested live OS/browser compatibility claims. macOS signing/notarization, and GUI/browser validation remain gating requirements.

- **Artifacts:** Linux `.deb` or unsigned/unnotarized macOS `.app`, standalone `host/` source payload, and `installer/` CLI.
- **Node.js:** External Node 22.13+ required. The desktop binary is **not** bundled into the host payload.
- **Local store:** Opening the dashboard creates nothing. With runtime/helper paths bound, acknowledge setup and choose **Create local store** to initialize a missing store with collection off and no grants. Existing stores are never replaced. Browser registration and consent remain separate explicit actions; live first-run GUI validation remains a gate.

## Installation & Configuration

1. Extract `host/` and `installer/` side-by-side into a stable, trusted absolute directory outside the source repo.
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
5. Register Chrome or Edge explicitly using the host registration helper, supplying an explicitly verified extension ID and pointing to the matching generated browser launcher path.

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
