# Companion platform and packaging evidence

This document outlines platform, browser, and packaging readiness for the unreleased v0.3.0 private preview on [PR #115](https://github.com/canoo/agent-nexus/pull/115).

## Architecture & Staged Payload Scope

The native host staged payload contains:

- Browser-specific `.mjs` launchers and registration helpers
- Shared store and data helpers
- All owned database migrations
- `LICENSE` and source `README.md`/package metadata

**Constraints & Prerequisites:**

- Requires external Node.js `>=22.13`. Source `.mjs` launchers use PATH; generated installed launchers pin an absolute runtime and desktop/helper paths.
- **Not bundled:** Node runtime, GUI binary, browser extension, npm dependencies, logs, secrets, or configuration files.
- **Staging guarantees:** Executed via explicit `--version` and absolute non-existing `--output` outside the source checkout; creates deterministic payload hashes/modes manifest; executes no browser registration, collection changes, or DB access during build.
- **CLI/TUI:** Source Go build, vet, and tests pass. Automated source Rust/Go and install cycle CI pass on Linux/macOS at `fad51fb` (does not verify live GUI/browsers; staged payload tests also pass on both at `3d652cb`).

Staged-process tests verify standalone executable and migration behavior in isolation. They do **not** represent live browser or desktop integration evidence. No platform or browser combination is advertised as live verified without concrete evidence.

---

## Support Matrix

| Desktop/browser packaging | Current evidence | Preview status |
| :--- | :--- | :--- |
| **Linux (Native) + Chrome** | Staged executable tests pass in isolated `$HOME` and relocated path with spaces; migrations initialize disabled; test grant verifies fixture ingestion; helper status reads 1 span. | Automated only; live use unverified |
| **Linux (Native) + Edge** | Same staged-process execution and migration test suite passes as Chrome. | Automated only; live use unverified |
| **macOS (Native) + Chrome / Edge** | Automated staged launchers, packaged migrations/helper and registration/removal tests pass at `3d652cb`. Live browser/GUI runtime and signed distribution remain unverified. | Automated only; live use unverified |
| **Flatpak (Companion & Browsers)** | Recipe placeholder only (empty `finish-args` and `modules`; not distributable). Host and registration helper refuse detectable Flatpak contexts in automated tests. | Unsupported |
| **Windows** | Registration and GUI preview not supported. | Unsupported |

---

## Sandbox & Platform Restrictions

- **Flatpak runtime gate:** `FLATPAK_ID` or `/.flatpak-info` causes host rejection before ingestion/cleanup; registration and removal return `native_host_environment_unsupported`. No activity or policy is changed. Automated tests simulate the environment; no actual Flatpak/browser runtime is verified.
- **Flatpak sandbox boundaries:** Flatpak browser and native-host pairings are unsupported. No host permission expansions, blanket home/network/D-Bus access, or `flatpak-spawn --host` workarounds are approved or implemented. Consult [Flatpak Sandbox Permissions](https://docs.flatpak.org/en/latest/sandbox-permissions.html).
- **Windows:** Current native host registration and GUI preview do not support Windows environments.

---

## Remaining Gates Before Live Verification

1. **Linux Native Host & GUI:** Verify real Chrome/Edge native messaging permission prompts, installed host invocation via the browser, focus/pause/quit lifecycle, and GNOME/KDE/Wayland system tray integrations.
2. **macOS Packaging:** Complete runtime verification in Chrome/Edge and signed/notarized DMG builds.
3. **Native Messaging Compliance:** End-to-end confirmation matching [Chrome Native Messaging Documentation](https://developer.chrome.com/docs/extensions/develop/concepts/native-messaging).

For implementation details and milestone tracking, see [../apps/companion-native-host/README.md](../apps/companion-native-host/README.md) and [v0.3.0-work-tracker.md](v0.3.0-work-tracker.md).


## Automated CI evidence

The Linux and macOS Companion jobs pass at `3d652cb` in
[CI run 37673809401](https://github.com/canoo/agent-nexus/actions/runs/37673809401).
This includes 27 extension tests, 29 host tests (staging, runtime gates and
registration/removal included), and desktop frontend tests/audit. The runner
executes test subprocesses; it does not operate installed browsers or the GUI.

## Installed launcher contract

The install-time generator validates staged payload hashes and probes the chosen
Node runtime before creating browser and desktop shell launchers. Linux tests
verify quoted paths, literal argument forwarding, Chrome/Edge ingestion and
shared helper reads with Node absent from PATH. A fixture executable stands in
for the desktop; this does not verify a real GUI installation. The generator
bundles neither Node nor a desktop binary, performs no browser registration, and
leaves history/consent unchanged. Signed packages and live platform/browser
checks remain required.

## Native development artifacts

The development builder produces a Linux `.deb` or unsigned macOS `.app` beside
the separate `host/` payload and `installer/` launcher generator. It builds in an
isolated source copy with locked dependencies; source version metadata remains
unchanged. Node is external. The manifest records hashes/modes and marks unsigned,
non-live-verified output explicitly. Hashes do not establish publisher identity.

Local Linux release packaging and archive verification pass: the verifier checks
the packaged executable, binds the installed launcher tools to it and runs both
browser wrappers against a missing store in a temporary HOME. It does not install
or launch the GUI. CI adds equivalent native artifact builds and verification to
both Linux/macOS Rust jobs. Build output does not establish GNOME/KDE/Wayland,
real browser or macOS runtime support. See the
[preview installation guide](companion-preview-install.md); fresh-install GUI
initialization and signed/notarized distribution remain outstanding.

## First-run initialization

The installed helper can now explicitly create a missing store from the GUI
acknowledgement/button or CLI `initialize --confirm` path. Source Rust/Node/Go
integration and desktop VM tests verify creation with disabled collection and
no grants, while rejecting existing databases, journal remnants and redirected
paths. The native artifact verifier also exercises packaged initialization in an
isolated HOME. Dashboard reads remain non-mutating. This implements the setup
operation; it does not establish a live GUI/browser installation result. Runtime
binding is still explicit and Node remains external.

## Packaged upgrade evidence

Both Chrome/Edge packaged executables are tested against schema 003 (v0.2.2)
and schema 004 (older Companion preview). Tests preserve tasks, sessions, import
receipts/markers, grants and retained activity, while schema 005 pauses an older
enabled preview once. Read-only status and refused initialization do not upgrade
or replace existing stores. Subsequent starts preserve an explicitly resumed
boundary. The native artifact verifier repeats the cases against its exact host
payload in temporary homes. Source and local Linux artifact checks pass, including
minimum Node 22.13; live installed GUI/browser upgrade checks remain outstanding.
