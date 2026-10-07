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

No other origin can produce an event. The only native-message envelope is the
schema described in [`docs/nexus-companion.md`](../../docs/nexus-companion.md):
`tool_id`, surface, bounded timestamps, detector/confidence, browser family,
optional supported platform, and schema/consent-policy versions. URLs, titles,
DOM/page text, prompts, responses, account identifiers, query strings, source
code, and arbitrary metadata are never copied into the event or active-span
state.

The options page shows a fixed-field schema preview, not captured activity
data. It lists the only fields that could leave the browser if the separate
native host is installed and collection is enabled.

## Load for local development

1. Open `chrome://extensions` in Chrome or `edge://extensions` in Edge.
2. Enable developer mode, choose **Load unpacked**, and select this directory.
3. Open the extension popup or options page. Each tool is off until you toggle
   it and approve that tool's small optional-origin request.

The extension attempts native messaging only when a selected enabled surface
ends. Until the separately installed and explicitly browser-registered NEXUS
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

A one-shot host replies only with `schema_version:1` and a boolean `ok`, never
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
Chrome/Edge service-worker suspension or permission prompts. Popup/options
consent writes still need coordination across simultaneous UI instances.
