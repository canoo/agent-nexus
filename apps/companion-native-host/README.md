# NEXUS Companion native host

This is the local-only, fail-closed Chrome/Edge native-messaging boundary for
the NEXUS Companion browser extension. It accepts the extension's fixed
activity envelope over Chromium's length-prefixed stdio protocol and writes an
accepted span only through the shared `ObservabilityStore.recordToolActivity`
API.

It does not capture browser content, URLs, titles, prompts, responses,
metadata, account identifiers, or any arbitrary browser data. Invalid frames
and envelopes produce no stdout/stderr host diagnostics and are never persisted.

## Current foundation status

The host is intentionally unregistered by default. The browser extension stays
inactive and fails closed until an operator explicitly registers a browser-
specific manifest with the ID of a published NEXUS extension. The source does
not know or accept a fallback extension ID, and neither package setup nor tests
write a native-host registration.

The two host launchers fix their adapter before they read stdin:

- `bin/nexus-companion-native-host-chrome.mjs` accepts only Chrome envelopes
  and therefore writes through the fixed `browser-chrome` consent path.
- `bin/nexus-companion-native-host-edge.mjs` accepts only Edge envelopes and
  therefore writes through the fixed `browser-edge` consent path.

The `browser_family` field must match the selected launcher. No browser payload
can select an adapter ID.

## Explicit registration lifecycle

Use only the registration command below after the desktop installer has placed
the matching launcher at a stable, executable absolute path. Repeat
`--extension-id` for each published extension ID that should be able to invoke
this host. IDs must be Chrome-format 32-character IDs (`a` through `p`).

```sh
cd apps/companion-native-host

# Dry run: prints a manifest and writes nothing.
node bin/nexus-companion-native-host-registration.mjs print-manifest \
  --browser chrome \
  --extension-id <published-extension-id> \
  --host-path /absolute/path/to/nexus-companion-native-host-chrome.mjs

# Explicitly register only Chrome, then repeat with the Edge launcher for Edge.
node bin/nexus-companion-native-host-registration.mjs install \
  --browser chrome \
  --extension-id <published-extension-id> \
  --host-path /absolute/path/to/nexus-companion-native-host-chrome.mjs

# Explicitly remove only Chrome's NEXUS manifest; local observability history remains.
node bin/nexus-companion-native-host-registration.mjs uninstall --browser chrome
```

Registrations remain distinct:

| Platform | Chrome manifest directory | Edge manifest directory |
| --- | --- | --- |
| Linux | `~/.config/google-chrome/NativeMessagingHosts` | `~/.config/microsoft-edge/NativeMessagingHosts` |
| macOS | `~/Library/Application Support/Google/Chrome/NativeMessagingHosts` | `~/Library/Application Support/Microsoft Edge/NativeMessagingHosts` |

The desktop installer will own launcher placement and this lifecycle in a later
milestone. Flatpak registration is not implemented or implied by this project.

## Test

```sh
cd apps/companion-native-host
npm test
```

## Executable integration tests

`npm test` also sends envelopes built by the actual extension module through
the Chrome and Edge launchers into isolated SQLite stores. These tests verify
privacy rejection, consent/collection boundaries, fixed browser adapters,
framing limits, and separation from MCP tasks and legacy JSONL.
Live browser permission, host registration and response-lifecycle checks remain
release gates; subprocess tests do not establish published-extension support.
