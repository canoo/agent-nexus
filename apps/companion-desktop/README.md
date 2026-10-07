# NEXUS Companion Desktop

This is the deliberately small Linux/macOS desktop shell for NEXUS Companion.
It starts with collection disabled and makes that state visible in its tray or
menu-bar surface and in a local dashboard.

It does **not** collect browser or desktop activity. It contains no browser
integration, content script, desktop/process detector, analytics, activity
store, or observability writer. It reads and manages the existing
migration-owned consent and settings rows in
`~/.config/nexus/logs/observability.sqlite`; it never creates, migrates, or
replaces that database. The native host remains the only activity writer via
the shared Node observability store.

## Current behavior

- Creates a Tauri v2 tray/menu-bar item. Its tooltip and menu state reflect the
  current local collection status where platform support permits it. If tray
  creation fails (such as in headless or minimal desktop environments), the
  dashboard window continues running without crashing.
- Provides a dashboard/no-tray fallback that displays fixed allowlisted
  adapter/tool consent states and native-host registration states. It discloses
  that only tool presence, surface, and start/end timestamps are captured. It
  never renders activity history, titles, URLs, prompts, responses,
  account/project identifiers, source code, or arbitrary metadata.
- Requires an explicit, initially unchecked acknowledgement checkbox confirming
  the privacy disclosure before any per-tool/browser grant button can be clicked.
- Supports per-tool/browser consent grants and revocations for Chrome and Edge
  across the five supported tools (`chatgpt`, `claude`, `gemini`, `copilot`,
  `perplexity`) targeting consent policy version 1. Consent updates are atomic
  upserts into `companion_tool_consents` and intentionally do **not** enable
  collection.
- Desktop foreground adapter is not implemented in this preview and is displayed
  as strictly read-only.
- Provides separate **Pause collection** and **Resume collection** actions:
  - **Pause**: atomically sets `collection_enabled = 0` in `companion_settings`
    while preserving all granted tool consents.
  - **Resume**: executes an immediate transaction requiring an existing
    settings row and at least one enabled current-policy browser consent
    before setting `collection_enabled = 1`. Stale, unknown, desktop-only, or
    absent consents are rejected.
- Offers an explicit **Disable collection and revoke all consents** action that
  atomically zeroes `collection_enabled` and revokes all tool consents in SQLite.
- Disables competing controls during in-flight actions to prevent conflicting
  state transitions. If store refresh fails, controls fail closed.
- Restricts error responses to fixed, safe messages, falling back to a generic
  error for any unexpected condition. Applies a bounded SQLite busy timeout
  (5 seconds) to prevent hangs.
- Opening the app or rendering the dashboard never mutates collection state or
  tool consents.
- Can invoke the existing browser-specific native-host registration helper only
  after a user submits a published Chrome-format extension ID and an existing
  absolute host path. The installer must deliberately configure
  `NEXUS_COMPANION_NATIVE_HOST_REGISTRATION_HELPER` to the helper script; when
  it is absent, the control is visibly unavailable. Registration does not
  enable collection or request browser access.
- Uses a generated in-memory tray status icon and a matching packaged PNG
  window icon required by Tauri's compile-time context generation.

## Remaining gates for release

This desktop shell is a preview control surface. Shipped release requires:

- **Packaged data controls**: verify the installed Node.js/shared-helper path and
  cleanup lifecycle in distributable builds. Raw-span cleanup and CLI/GUI controls
  have automated tests; the aggregate-retention setting is reserved and no daily
  aggregates are generated yet.
- **Pause-boundary spans**: verify resumed activity never counts time spent paused.
- **Browser extension & native host integration**: end-to-end integration tests
  verifying extension-to-host messaging and store consent enforcement in real
  Chrome and Edge installations.
- **Platform runtime validation**: verifying tray, windowing, and notification
  behavior across Linux desktop environments (GNOME, KDE, Wayland) and macOS.
- **Packaging and distribution**: Developer ID signing, notarization, and DMG
  assembly on macOS; sandboxed Flatpak permissions and native messaging design
  on Linux.

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
the Rust formatting check. `cargo test --manifest-path src-tauri/Cargo.toml --locked`
additionally covers consent request validation, migration 004 schema verification,
grant-without-enabling, pause/resume rules, and rollback behavior. To run the full
Companion test suite locally:

```sh
# Browser extension unit tests
cd apps/companion-browser-extension && npm test

# Native host unit tests
cd apps/companion-native-host && npm test

# Desktop frontend tests & security audit
cd apps/companion-desktop && npm ci && npm audit && npm test

# Desktop Rust format check & tests (requires Rust stable and platform prerequisites)
cd apps/companion-desktop && cargo +stable fmt --manifest-path src-tauri/Cargo.toml -- --check
cd apps/companion-desktop && cargo +stable test --manifest-path src-tauri/Cargo.toml --locked
```

Full compilation requires the Tauri and bundled SQLite crates to be available
locally.

## Linux behavior

Linux tray visibility depends on the desktop environment. The app uses Tauri's
tray/status-notifier path where available, but does not promise a top-bar icon
or working tray tooltip on every GNOME or Wayland setup. If tray creation fails,
the app logs a message and keeps running. The application launcher and
dashboard window remain the no-tray fallback. This foundation does not yet
provide the planned CLI status command or a desktop activity adapter.

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

## Local data controls (v0.3.0 development)

The dashboard offers a raw-history count, retention setting (0–365 days), pruning,
and confirmed deletion. The default is 14 days. Shortening retention removes expired
spans immediately; zero days keeps no raw activity. Clear requires an unchecked-by-
default acknowledgement and does not change collection/consent policy, MCP task
history, or project memory.

Both desktop and CLI invoke `tools/mcp/companion-data.mjs`, which owns mutation
through the observability store. The desktop finds this helper beneath an absolute
`NEXUS_REPO`, otherwise `$HOME/.config/nexus/repo`, and requires Node.js 22.13+.
Missing helper, runtime, database, or valid settings disables the data controls.
Opening the dashboard only reads data. The CLI and desktop wrappers use fixed arguments, a bounded
JSON reply, and a 15-second deadline; errors never display paths or SQL.

```bash
nexus companion data --json
nexus companion retention --days 14 --json
nexus companion prune --json
nexus companion clear --confirm --json
```

The native host prunes at startup, every 30 minutes while running, and before
validated incoming spans. With no host running, use the prune command. Expiry uses
UTC `ended_at`, retaining spans exactly at the cutoff. Daily aggregates and export
are not implemented. Real browser/GUI and packaged runtime validation remain open.
