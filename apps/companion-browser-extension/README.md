# NEXUS Companion Browser Extension

This is the private-preview Chrome and Edge Manifest V3 surface for NEXUS
Companion. It is disabled by default and can only map a selected tab's local
origin to one fixed, enabled NEXUS tool identifier. It has no content scripts,
no `<all_urls>` permission, and no browser storage or network fallback for
activity events.

## Supported optional origins

Each origin is requested only after its matching tool is switched on in the
popup or options page. Switching a tool off revokes its matching optional host
permission.

| Tool | Optional host permissions |
| --- | --- |
| ChatGPT | `https://chatgpt.com/*`, `https://chat.openai.com/*` |
| Claude | `https://claude.ai/*` |
| Gemini | `https://gemini.google.com/*` |
| Copilot | `https://copilot.microsoft.com/*` |
| Perplexity | `https://www.perplexity.ai/*` |

No other origin can produce an event. The activity native-message envelope is the
schema described in [`docs/nexus-companion.md`](../../docs/nexus-companion.md):
`tool_id`, surface, bounded timestamps, detector/confidence, browser family,
optional supported platform, and schema/consent-policy versions. URLs, titles,
DOM/page text, prompts, responses, account identifiers, query strings, source
code, and arbitrary metadata are never copied into the event or active-span
state.

The Chrome options page is the graphical NEXUS Control Center. It provides
Install, Configure, Health, Task Log, Usage, Update, Uninstall, Memory and Routing
views, plus Companion controls and browser/native consent management. Explicit
user-authored memory and routing requests use a separate closed control protocol;
they are never derived from tab content. See [publication gates and permission
audit](PUBLICATION.md).

## Load for local development

1. Open `chrome://extensions` in Chrome or `edge://extensions` in Edge.
2. Enable developer mode, choose **Load unpacked**, and select this directory.
3. Open the extension popup or options page. Each tool is off until you toggle
   it and approve that tool's small optional-origin request.

Activity delivery attempts native messaging when a selected enabled surface
ends. Opening the Control Center or activating its buttons also sends explicit
control requests to the same host. Until the separately installed and explicitly browser-registered NEXUS
native host exists, delivery fails closed: it does not log, queue, retry, or
send the envelope elsewhere. The strict host foundation lives in
[`apps/companion-native-host/`](../companion-native-host/), but it has no
automatic registration and accepts only a manifest's supplied published
extension IDs. This directory still does not implement desktop collection,
arbitrary capture, publishing, or store writes.

Run the policy/envelope and mocked browser-worker tests with:

```sh
cd apps/companion-browser-extension
npm test
```

## Native acknowledgement

For activity ingestion, a one-shot host replies only with `schema_version:1` and a boolean `ok`, never
activity data or error text. Dispatch succeeds only for an exact positive reply;
missing, negative or malformed replies fail closed without logs, fallback, or
retry queues. Host executable tests cover this lifecycle, including a caller
keeping stdin open, but live browser permission/registration tests remain open.

## Concurrent browser events

Tab, focus, window, consent and permission changes share one asynchronous state
queue. Event timestamps are taken when the event arrives. A disabled-consent
snapshot discards its in-progress spans even if the user immediately grants
consent again. Revocations block new dispatch immediately; an older pending
permission check cannot restore consent after a newer revocation.

Native delivery runs outside that queue, with at most four calls in flight.
Further deliveries are dropped while those calls remain pending, without retries
or payload storage. A stalled host therefore cannot block consent cleanup.
Automated worker tests use synthetic browser APIs; they do not verify real
Chrome/Edge service-worker suspension or permission prompts. Popup/options submit one-tool consent changes to this worker queue, so
concurrent UI instances cannot overwrite each other's unrelated settings.

The worker accepts only a fixed consent message from its own popup/options pages,
rechecks required origin permissions before granting, and returns only the five
consent booleans and an applied flag. UI pages never write shared consent objects
directly. A grant now sends `request-tool-grant` synchronously from the checkbox
gesture. Its validated worker listener starts `permissions.request` before any
storage or queue await, and the worker owns the permission-result continuation.
Destroying the popup while its permission prompt is open therefore cannot discard
the subsequent consent write. The worker uses the explicit asynchronous response
channel described in Chrome's [message-passing documentation](https://developer.chrome.com/docs/extensions/develop/concepts/messaging).

There is at most one pending grant per fixed tool, expiring after 60 seconds.
Denied, failed, expired, superseded or revoked requests cannot enable consent.
The worker rechecks the required origins and repairs a canceled write before
releasing the state queue. A prompt completing after expiry may leave browser
permission granted, but tool consent stays off; another explicit checkbox click
is required. Browser `permissions.onAdded` events never imply a tool grant.
Pending grants are not persisted for replay after an unexpected worker shutdown;
that failure remains closed. No manifest permissions, origin list or capture
fields changed. Collection still requires separate native policy consent, now
available in the Chrome Control Center as well as through the existing CLI.

Chromium's upstream `UserGestureFromExtensionPage` and
`UserGestureFromContentScript` browser tests verify gesture propagation to an MV3
worker in its [messaging API tests](https://chromium.googlesource.com/chromium/src/+/refs/heads/main/chrome/browser/extensions/api/messaging/messaging_apitest.cc).
Chrome's [worker lifecycle documentation](https://developer.chrome.com/docs/extensions/develop/concepts/service-workers/lifecycle)
also describes the longer-lived `permissions.request` API prompt. Local mocked
regressions verify popup-response loss, denial, late results, expiry during
storage writes, concurrent tools and revocation. They do not establish gesture
propagation or first-click behavior in the user's installed Chromium; that needs
a fresh user-assisted permission prompt after updating the installed extension.

## Build the Chrome store artifact

```sh
node tools/automation/build-chrome-extension.mjs --version 0.3.0-extension.1 --output /tmp/nexus-extension-store
```

Run from the repository root. Review [PUBLICATION.md](PUBLICATION.md) before any
submission. Building a ZIP does not approve publishing or complete the live GUI gate.
