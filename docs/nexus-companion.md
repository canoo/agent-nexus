# NEXUS Companion — Activity Signals Design

## Purpose

NEXUS Companion is an optional, local-only integration that records which
supported AI-tool surfaces are active. It extends NEXUS observability without
turning NEXUS into a browser scraper, request interceptor, or prompt archive.

It answers questions such as "which configured AI tools did I use this week?"
and "was this a browser or desktop session?" It does not answer what a user
asked an AI tool, what the tool returned, or how many tokens a browser session
used.

## Scope and non-goals

Companion records short activity spans, not AI tasks. A selected ChatGPT tab,
for example, proves only that the configured surface was active; it does not
prove that a prompt was sent, that a model was selected, or that the user read a
response.

Out of scope by design:

- prompt, response, DOM, network, clipboard, screenshot, or source-code capture
- browser page titles, full URLs, URL paths, query strings, account identifiers,
  project paths, installed-extension lists, API keys, and arbitrary metadata
- injecting UI into an AI product, automating a provider, or intercepting API
  traffic
- inferring token usage, model choice, cost, or a relationship to a CLI task
- cloud sync or analytics uploads

Tokscale remains the source for post-hoc CLI token and cost data. NEXUS-native
routing metrics, Tokscale usage, and Companion activity must remain visibly
separate in the dashboard.

## Data contract

Each event represents a bounded activity span. The allowed envelope is:

```json
{
  "tool_id": "chatgpt",
  "surface": "browser",
  "started_at": "2026-10-02T18:00:00Z",
  "ended_at": "2026-10-02T18:04:12Z",
  "detector": "selected-browser-tab",
  "confidence": "surface-active",
  "browser_family": "chrome",
  "platform": "linux",
  "schema_version": 1,
  "consent_policy_version": 1
}
```

`tool_id` is selected from NEXUS's allowlisted tool registry. The current
ingestion boundary recognizes only `chatgpt`, `claude`, `gemini`, `copilot`, and
`perplexity`; adding a tool requires a source and test change. The extension
maps a selected tab's origin to this fixed identifier locally and sends no URL
or title to the host. The native host accepts only this schema, applies a small
payload limit, and rejects unknown fields.

Activity data belongs in a dedicated `tool_activity` table rather than the
existing `tasks` table: a task represents an execution/routing event and
requires a model, while an activity span does neither. Activity can be linked to
a synthetic Companion session for display, but it must never be correlated to a
CLI task merely because the timestamps overlap.

## Consent, storage, and retention

Companion is disabled by default. Enabling it requires an explicit choice for
each browser and each supported tool. Desktop adapters require a separate,
equally explicit choice. Consent is device-local and is never copied by
`nexus sync` or `nexus adopt`.

All data stays under NEXUS's local observability directory with restrictive
permissions. The migration-owned local settings foundation defaults to disabled
collection, 14 days for raw spans, and 90 days for daily local aggregates.
The retention worker, local export, and immediate local deletion controls remain
subsequent Companion work. The TUI now has a read-only activity-history preview
that reads only the fixed `tool_activity` fields from the migration-owned SQLite
database; it has no JSONL fallback, export path, or task correlation. The
TUI uses the pure-Go `modernc.org/sqlite` driver for portable Linux/macOS
read-only access without requiring a system SQLite binary or cgo. The
extension's current UI shows only a fixed-field envelope preview, never
collected activity data.

The shared local ingestion boundary fails closed even for a schema-valid
envelope: it writes only when `companion_settings.collection_enabled` is on and
the derived local adapter/tool pair has a current enabled row in
`companion_tool_consents`. Missing, disabled, mismatched, or policy-version
mismatched consent produces no activity row.

"Installed/configured" and "active" are distinct states. A background process
or an allowlisted site open in a background tab is not active use.

## Platform design

### Browser MVP: Chrome and Edge

The first release is a Manifest V3 extension paired with a native-messaging
host on Linux and macOS.

- The extension requests optional host permissions only for user-selected
  supported AI domains; it must never request `<all_urls>`.
- The service worker observes the selected tab and emits an activity transition
  only when its origin matches an enabled tool. There are no content scripts.
- The native host accepts messages only from NEXUS's published extension IDs,
  validates the fixed envelope, and writes through the shared NEXUS
  observability ingestion boundary.
- The browser UI includes visible per-tool toggles and a fixed-field envelope
  preview before collection begins; it does not show captured activity data.

Firefox follows only after a separate native-messaging and MV3 compatibility
test pass. Safari needs a packaged macOS extension/application and is a
separate milestone.

### Desktop Companion and adapters

The initial Companion desktop application targets Linux and macOS. It provides
a visible top-bar/menu-bar status icon with privacy state, dashboard access,
pause, and quit controls; it is not a hidden collector. Tauri v2 is the chosen
cross-platform shell because it supports macOS menu-bar and Linux tray surfaces,
platform-specific configuration, native messaging, and Flatpak packaging.

Linux tray support is desktop-environment dependent. The Companion therefore
also provides a launcher/CLI fallback, always creates a tray menu where one is
available, and does not rely on unsupported Linux click or tooltip behavior.
The Flatpak build is a separate verification target: its sandbox and browser
native-messaging interaction must be explicitly tested before release.

Desktop detection is adapter-specific, never generic process scraping. macOS
may use a disclosed foreground application/bundle-ID adapter. Browser detection
is the reliable Linux baseline; on Wayland, a desktop adapter is supported only
where an official compositor IPC API exists. Windows belongs after the planned
cross-platform work in v1.0.0.

## Delivery sequence

1. Complete v0.2.1's Tokscale ingestion and introduce one migration-owned
   observability ingestion/store boundary. The current JSONL writer and reader
   must not be bypassed by a second Companion store.
2. Migrate the documented SQLite schema, add `tool_activity`, retention, and
   consent configuration.
3. Build the Chrome/Edge extension and strict native host; add contract tests
   that prove fake prompts, titles, and URLs cannot reach storage or exports.
   The extension private-preview scaffold now lives in
   [`apps/companion-browser-extension/`](../apps/companion-browser-extension/):
   it is disabled by default, has no content scripts or `<all_urls>`
   permission, and emits only the fixed envelope through native messaging. Its
   local-only host foundation is in
   [`apps/companion-native-host/`](../apps/companion-native-host/): each
   Chrome/Edge launcher fixes the adapter before input, validates bounded
   native-message frames, and writes only through the shared store after its
   existing collection and consent gates pass. It is unregistered by default;
   explicit per-browser manifest installation with published extension IDs is
   still required. Retention controls and exports remain unimplemented.
4. Add the Linux/macOS Companion desktop shell and a separate TUI Tool Activity
   screen. The TUI screen is now a read-only SQLite view that labels Companion
   surface activity separately from NEXUS routing and Tokscale usage; it does
   not infer task relationships from timestamps. The shell supplies the
   native-messaging boundary and visible privacy controls; Flatpak packaging is
   validated separately on Linux.
5. Integrate installation with the v0.2.5 tool registry while retaining
   device-local consent; then consider Firefox and supported desktop adapters.

## Acceptance criteria for the private preview

- An enabled, configured AI-tool site produces one local activity span when its
  tab is selected; disabled tools and all other sites produce none.
- The native host rejects unknown fields, oversized messages, and non-allowlisted
  senders.
- Tests inject representative prompts, page titles, and URLs and prove none are
  present in logs, SQLite rows, or exports.
- The dashboard labels Companion activity separately from NEXUS task routing and
  Tokscale token/cost data.
- Uninstall removes native-host registration and leaves a documented choice to
  retain or remove local activity history.
