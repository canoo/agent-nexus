# Companion platform and packaging evidence

This document outlines platform, browser, and packaging readiness for the unreleased v0.3.0 private preview on [PR #115](https://github.com/canoo/agent-nexus/pull/115).

## Architecture & Staged Payload Scope

The native host staged payload contains:

- Browser-specific `.mjs` launchers and registration helpers
- Shared store and data helpers
- All owned database migrations
- `LICENSE` and source `README.md`/package metadata

**Constraints & Prerequisites:**

- Requires external Node.js `>=22.13`; system environment `PATH` must resolve `node`.
- **Not bundled:** Node runtime, GUI binary, browser extension, npm dependencies, logs, secrets, or configuration files.
- **Staging guarantees:** Executed via explicit `--version` and absolute non-existing `--output` outside the source checkout; creates deterministic payload hashes/modes manifest; executes no browser registration, collection changes, or DB access during build.
- **CLI/TUI:** Source Go build, vet, and tests pass. Automated source Rust/Go and install cycle CI pass on Linux/macOS at `fad51fb` (does not verify live GUI/browsers; upcoming CI will test staged payloads on both).

Staged-process tests verify standalone executable and migration behavior in isolation. They do **not** represent live browser or desktop integration evidence. No platform or browser combination is advertised as live verified without concrete evidence.

---

## Support Matrix

| Desktop/browser packaging | Current evidence | Preview status |
| :--- | :--- | :--- |
| **Linux (Native) + Chrome** | Staged executable tests pass in isolated `$HOME` and relocated path with spaces; migrations initialize disabled; test grant verifies fixture ingestion; helper status reads 1 span. | Automated only; live use unverified |
| **Linux (Native) + Edge** | Same staged-process execution and migration test suite passes as Chrome. | Automated only; live use unverified |
| **macOS (Native) + Chrome / Edge** | Automated source Rust/Go and install cycle CI pass at `fad51fb`. Staged payload execution and live runtime unverified. | In Development |
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
2. **macOS Packaging:** Complete CI staged payload testing, runtime verification in Chrome/Edge, and signed/notarized DMG builds.
3. **Native Messaging Compliance:** End-to-end confirmation matching [Chrome Native Messaging Documentation](https://developer.chrome.com/docs/extensions/develop/concepts/native-messaging).

For implementation details and milestone tracking, see [../apps/companion-native-host/README.md](../apps/companion-native-host/README.md) and [v0.3.0-work-tracker.md](v0.3.0-work-tracker.md).

