# Firefox transport feasibility experiment — 2026-10-28

Firefox is feasible as a separate browser port. It is not currently a supported
Companion collection adapter. This experiment demonstrates an isolated Firefox-
shaped extension event calling a native-message fixture, without production
activity ingestion, new collection permissions or a Chrome identity alias.

Branch: `codex/companion-firefox`, based on integration commit `126aa0f`.
All changes are confined to `experiments/companion-firefox/` in the dedicated
worktree. No desktop/macOS code, production schemas, user browser registrations,
user profile or existing database is modified.

## Concrete staged prototype and evidence

From the worktree root, with Node 22.13+ on native little-endian Linux:

```sh
node --test experiments/companion-firefox/test/*.test.mjs
node experiments/companion-firefox/stage.mjs --output /tmp/nexus-firefox-probe-20261028
```

The first command passed **9/9 tests** on Saturn. The second created:

- `/tmp/nexus-firefox-probe-20261028/extension/manifest.json` and `background.js`;
- `host/nexus-firefox-probe`, a launcher pinned to the real Node executable;
- `host/fixture-host.mjs` and the existing bounded native-message codec;
- `isolated-home/.mozilla/native-messaging-hosts/com.codelogiic.nexus.firefox_probe.json`.

The generator requires a fresh absolute output outside the checkout. It writes
registration fixtures only beneath that output, never the real user's `.mozilla`.
Staged paths are pinned; regenerate at a new destination after relocation.
Outputs are temporary and may disappear after reboot. No browser was launched
or extension installed during this experiment. Do not treat the staged home as
a registered user installation.

The extension has just `nativeMessaging`, a strict subset of the current
extension's permissions. It has no origin permissions, content scripts, tabs
access, storage, popup or observation listeners. A toolbar action sends one
constant `transport-probe` request. The fixture accepts only its exact three
fields, requires `browser_family: "firefox"`, and rejects all activity envelopes,
unknown fields and other add-on IDs. Its acknowledgement contains a fixed
`fixture_only: true` flag; an `OK` badge would mean only transport succeeded.
The fixed probe contains no browsing/user information, so this prototype declares
no data collection. That declaration must be revisited for any real collector.

Tests execute the staged event script in a Node VM with an API shim; its
`sendNativeMessage` shim invokes the **real staged executable** with Firefox's
manifest-path/add-on-ID arguments and framed stdin/stdout. This verifies
synchronous event registration, strict reply validation, pinned launch paths
containing shell punctuation, bounded concurrent probes, malformed/oversize
frames, identity rejection and no `.config/nexus` creation in the isolated home.
Production validators also demonstrably reject Firefox instead of accepting it
as Chrome. This is **process/VM transport evidence**, not real Firefox API,
manifest installation, suspension, permission-prompt or activity-delivery evidence.

## Required changes for a production Firefox adapter

| Area | Current gap and next implementation |
| --- | --- |
| Activity identity | `lib/activity.js`, native-host `lib/host.mjs` and store normalization accept only `chrome`/`edge`. Add an explicit `firefox` family and fixed `browser-firefox` adapter together, with independent consent. Do not reuse `browser-chrome`. |
| Storage and upgrades | Current SQL uses text family/adapter fields, but changing JS allowlists alone would bypass complete policy integration. Review owned migrations, existing-store upgrades, CLI/TUI/dashboard allowlists, resume eligibility and consent/pause boundary tests before enabling ingestion. This branch makes none of those changes. |
| Background execution | The production manifest is Chrome service-worker-only. Firefox needs a dedicated `background.scripts` event-page manifest; listeners must register synchronously and suspended state must remain fail closed. Recheck `storage.session`, focus/tab events, incognito policy and permission revocation in real Firefox. |
| Extension identity | Choose and stabilize a real Gecko add-on ID and separate signed-distribution policy. The prototype ID `nexus-firefox-probe@codelogiic.local` is experimental, not a published production identity. |
| Native registration | Add a Firefox-specific manifest with `allowed_extensions`, not Chrome `allowed_origins`, and a dedicated executable binding `firefox` before stdin. Linux per-user registration lives in `~/.mozilla/native-messaging-hosts/`. Explicit install/remove must preserve Chrome/Chromium/Edge manifests and history. |
| Packaging | Extend host payload and launcher allowlists, extension staging, artifact verification and documentation; choose separate Firefox/Chromium manifests or a reviewed cross-browser generation step. The experiment's fixture host must never enter a production package. Native Linux only first; Flatpak/Snap and macOS are separate work. |
| Consent | Keep existing optional tool-origin policy and explicit grants; Firefox access and desktop collection consent remain separate gates. Define Firefox's data-use categories for native activity transfer rather than copying this probe's `none` declaration. |
| Verification | Add real Firefox tests for optional origins, stable ID, native host allowlist, suspension/restart, focused-tab delivery, missing/revoked consent, pause/resume, malformed payload, retention and upgrades. Then run a disposable-profile browser test before user-profile verification. |

Mozilla documents that Firefox uses event pages rather than extension background
service workers. This prototype therefore declares `background.scripts` and
registers its action listener immediately. No production manifest is changed.
[Background manifest reference](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/manifest.json/background).

Mozilla's native-messaging guide requires explicit add-on IDs and
`allowed_extensions`; it describes one-shot `sendNativeMessage` and the
manifest-path/add-on-ID process arguments used by the fixture tests. Its framing
uses native byte order; reusing the current little-endian codec is limited to
this little-endian Linux prototype.
[Native messaging](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/Native_messaging).
The Linux registration fixture mirrors the documented per-user Mozilla location.
[Native manifest locations](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/Native_manifests).

The prototype targets Firefox 140+ and specifies a Gecko ID plus the no-data
manifest declaration. New production Firefox submissions must accurately declare
their data handling. Mozilla explicitly includes native-application data transfer
in consent guidance, so actual activity collection needs a fresh classification
and explicit opt-in; localhost-only operation does not settle that review.
[Firefox data-consent guidance](https://extensionworkshop.com/documentation/develop/firefox-builtin-data-consent/),
[Consent best practices](https://extensionworkshop.com/documentation/develop/best-practices-for-collecting-user-data-consents/).

## First-click permission persistence: Firefox implications

The current Chromium UI calls `permissions.request` synchronously from the
checkbox gesture, awaits it in the popup, then sends `set-tool-consent` to the
background worker. If the popup unloads during that wait, the continuation may
never send the consent mutation, leaving browser permission granted but the
local tool checkbox false. This is a code-derived hypothesis for the reported
first-click symptom, not a reproduced or confirmed cause on either browser.

Firefox's popup document is destroyed when its popup closes; clicking outside
closes it. The same architectural risk therefore applies to a direct port.
[Mozilla popup lifecycle](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/user_interface/Popups).
Permission requests must remain directly tied to a user action, and their
promises distinguish success/failure. Moving the request behind awaited work
or blindly inferring tool consent from an externally granted permission would
weaken that contract.
[Permission request requirements](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/API/permissions/request).

Next investigate in a disposable Firefox profile with the existing optional
origin list: request from a persistent options tab, observe popup closure and
`permissions.onAdded`, and compare the worker's explicit consent state after
reopening. Any eventual fix must persist an explicit user intent safely, finish
or reject the queued mutation despite UI closure, and keep permission removal
and concurrent revocation authoritative. Do not automatically enable collection
or grant consent merely because an origin permission exists. This transport
probe requests no origins and intentionally cannot validate that interaction.
