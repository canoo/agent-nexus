# NEXUS Companion Desktop

This is the deliberately small Linux/macOS desktop shell for NEXUS Companion.
It starts with collection disabled and makes that state visible in its tray or
menu-bar surface and in a local dashboard.

It does **not** collect browser or desktop activity. It contains no browser
integration, content script, desktop/process detector, analytics, activity
store, or observability writer. It reads and can explicitly revoke the existing
migration-owned consent/settings rows in
`~/.config/nexus/logs/observability.sqlite`; it never creates, migrates, or
replaces that database. The native host remains the only activity writer via
the shared Node observability store.

## Current behavior

- Creates a Tauri v2 tray/menu-bar item. Its tooltip reflects the current local
  collection state where platform support permits it.
- Provides a dashboard/no-tray fallback that shows only the fixed allowlisted
  adapter/tool consent states and native-host registration states. It never
  renders activity history, titles, URLs, prompts, responses, account/project
  identifiers, source code, or arbitrary metadata.
- Offers an explicit **Disable collection and revoke all consents** action. It
  updates `companion_settings` and `companion_tool_consents` in one SQLite
  transaction. There is intentionally no enable action in this app.
- Offers `Open Dashboard`, explicit disable, and `Quit` in the tray/menu-bar
  menu. Opening the app or dashboard never changes a collection setting.
- Can invoke the existing browser-specific native-host registration helper only
  after a user submits a published Chrome-format extension ID and an existing
  absolute host path. The installer must deliberately configure
  `NEXUS_COMPANION_NATIVE_HOST_REGISTRATION_HELPER` to the helper script; when
  it is absent, the control is visibly unavailable. Registration does not
  enable collection or request browser access.
- Uses a generated in-memory tray status icon and a matching packaged PNG
  window icon required by Tauri's compile-time context generation.
- Never enables collection through the UI. There is no hidden or automatic
  collection mode.

The dashboard is a privacy-status/control surface, not a task, token, cost, or
activity view. The existing CLI and Go TUI remain independent of this app.

## Bootstrap and local development

This project ships Rust and npm lockfiles, but does not vendor JavaScript or
Rust dependencies. After reviewing the dependency versions, install the
JavaScript and Rust build requirements locally:

```sh
cd apps/companion-desktop
npm ci
npm run dev
```

`npm run build -- --no-bundle` compiles a local development binary once the
Tauri v2 prerequisites for the host platform are installed. It does not create
a releasable package.

Required toolchains:

- Node.js 22 or later and npm.
- Rust stable with Cargo (the crate declares Rust 1.77 as its minimum).
- The platform prerequisites documented by the [Tauri v2 prerequisites guide](https://v2.tauri.app/start/prerequisites/), including a supported WebKitGTK
  development stack on Linux and Xcode command-line tools on macOS.

Use `npm test` to run the frontend unit tests, and `npm run check` for
the Rust formatting check. `cargo test` additionally covers fixed
extension-ID validation and the unavailable-store fail-closed state. Full
compilation requires the Tauri and bundled SQLite crates to be available
locally.

## Linux behavior

Linux tray visibility depends on the desktop environment. The app uses Tauri's
tray/status-notifier path where available, but does not promise a top-bar icon
or working tray tooltip on every GNOME or Wayland setup. The application
launcher and dashboard window remain the no-tray fallback. This foundation
does not yet provide the planned CLI status command or a desktop activity
adapter.

Do not use tray presence, absence, clicks, or tooltips as activity signals.
Linux tray events are not consistently supported by Tauri desktop backends.

## macOS packaging

The `tauri.macos.conf.json` overlay reserves the macOS application/DMG target.
The shell can report browser-specific native-host manifest state using the
standard per-user Chrome/Edge locations, but does not detect desktop apps.
Signed distribution is future work. Do not publish an unsigned build. Release
packaging requires Developer ID signing, hardened runtime, notarization,
stapling, and real menu-bar launch testing before it can be released.

## Flatpak status

[`flatpak/com.codelogiic.NexusCompanion.yml`](flatpak/com.codelogiic.NexusCompanion.yml)
is a no-permissions packaging placeholder only. It intentionally has no
`finish-args`, build modules, browser permissions, native-messaging registration,
or host filesystem access. It is not a buildable or distributable Flatpak yet.

Flatpak browser native messaging is explicitly unimplemented. A future
Flatpak-specific validation must establish a minimal, auditable host
registration design; it must not expand permissions or assume that all browser
installations can use a sandboxed host.
