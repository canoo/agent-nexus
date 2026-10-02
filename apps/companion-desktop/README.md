# NEXUS Companion Desktop

This is the deliberately small Linux/macOS desktop shell for NEXUS Companion.
It starts with collection disabled and makes that state visible in its tray or
menu-bar menu and in the local dashboard placeholder.

It does **not** collect browser or desktop activity. It contains no native
messaging host, browser integration, content script, desktop/process detector,
analytics, activity storage, consent persistence, or observability writer.
Those features require the later schema, consent, retention, and strict-host
milestones in [`docs/nexus-companion.md`](../../docs/nexus-companion.md).

## Current behavior

- Creates a Tauri v2 tray/menu-bar item with `Collection: Disabled`.
- Offers `Open Dashboard` (a local placeholder window) and `Quit`.
- Uses a generated in-memory status icon, so no downloaded or packaged icon
  asset is needed for this foundation.
- Never enables collection through the UI. There is intentionally no hidden or
  automatic collection mode.

The dashboard is a status placeholder, not a task, token, cost, or activity
view. The existing CLI and Go TUI remain independent of this app.

## Bootstrap and local development

This project deliberately ships without installed dependencies or a lockfile.
After reviewing the dependency versions, install the JavaScript and Rust build
requirements locally:

```sh
cd apps/companion-desktop
npm install
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

Use `npm run check` for the Rust formatting check. Full compilation has not
been run in this repository because dependencies have intentionally not been
installed by this change.

## Linux behavior

Linux tray visibility depends on the desktop environment. The app uses Tauri's
tray/status-notifier path where available, but does not promise a top-bar icon
on every GNOME or Wayland setup. The application launcher and its dashboard
window remain the no-tray fallback. This foundation does not yet provide the
planned CLI status command.

Do not use tray presence, absence, clicks, or tooltips as activity signals.
Linux tray events are not consistently supported by Tauri desktop backends.

## macOS packaging

The `tauri.macos.conf.json` overlay reserves the macOS application/DMG target,
but signed distribution is future work. Do not publish an unsigned build.
Release packaging requires Developer ID signing, hardened runtime, notarization,
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
