# NEXUS Preview Artifact Guide

## Scope & Prerequisites
These preview artifacts are **development build evidence**, not a v0.3.0 release; they carry no tested live OS/browser compatibility claims. macOS signing/notarization, and GUI/browser validation remain gating requirements.

- **Artifacts:** Linux `.deb` or unsigned/unnotarized macOS `.app`, standalone `host/` source payload, and `installer/` CLI.
- **Node.js:** External Node 22.13+ required. The desktop binary is **not** bundled into the host payload.
- **Existing Setup:** Requires an existing NEXUS SQLite database. Fresh-install GUI standalone initialization remains a gate. This artifact does **not** auto-initialize missing databases, collect telemetry by default, grant user consent, or register browsers automatically.

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

4. The generated `nexus-companion` launcher binds runtime helpers. Register Chrome or Edge explicitly using the host registration helper, supplying an explicitly verified extension ID and pointing to the matching generated browser launcher path.

On Linux the installed executable is typically `/usr/bin/nexus-companion`; on macOS use `/Applications/NEXUS Companion.app/Contents/MacOS/nexus-companion`. Supply the actual existing executable path. These unsigned development artifacts do not replace signed/notarized distribution. The Linux build targets Ubuntu 24.04 CI; other distributions and desktop environments remain unverified. Keep all paths stable after binding and re-register if they change. Removing browser registrations or launchers preserves history.
