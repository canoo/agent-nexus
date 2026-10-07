# NEXUS Companion native host

This is the local-only, fail-closed Chrome/Edge native-messaging boundary for
the NEXUS Companion browser extension. It accepts the extension's fixed
activity envelope over Chromium's length-prefixed stdio protocol and writes an
accepted span only through the shared `ObservabilityStore.recordToolActivity`
API.

It does not capture browser content, URLs, titles, prompts, responses,
arbitrary metadata, account identifiers, or arbitrary browser data. Invalid
frames and envelopes are never persisted. Stdout contains only one length-prefixed
acknowledgement: `{"schema_version":1,"ok":true}` for acceptance or the same shape
with `ok:false` for rejection. No activity fields, internal codes, or error text
are returned or logged.

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

## One-shot response lifecycle

The launchers implement `runtime.sendNativeMessage`: one request per process,
one fixed acknowledgement, then close input only after the reply is flushed.
Additional batched frames are ignored; persistent `connectNative` ports are not
supported by these launchers. The extension validates both acknowledgement fields
and rejects unexpected replies without logging, fallback, or a retry queue.

This follows [Chrome's native-messaging response protocol](https://developer.chrome.com/docs/extensions/develop/concepts/native-messaging).
An executable regression keeps the caller's stdin open until the host responds
and exits, proving delivery does not depend on the caller closing its input first.
Native-host cleanup runs at each start/ingestion; these one-shot hosts do not
remain running between browser events. Use the explicit prune command when idle.

## Stage the separate host payload

From a source checkout, create a new directory outside the checkout:

```sh
node apps/companion-native-host/bin/nexus-companion-native-host-package.mjs \
  --version 0.3.0-dev.1 --output /absolute/new/path/nexus-companion-host
```

The output preserves the host/store relative layout and includes all owned SQL
migrations, the shared data helper, license and supporting docs. It needs no npm
install and includes no Node runtime, desktop binary or browser extension.
`package-manifest.json` records the supplied version, sorted file paths, SHA-256
hashes and modes; staging identical source produces identical payload/manifest
bytes. This is a staged directory, not a signed archive or published release.
The private npm package metadata retains its source development version; the
manifest records the requested staging version.

The command requires a new absolute destination outside the source root. It
rejects missing, unexpected or symlinked source files before creating output.
It never replaces existing output, reads a user database, registers a browser
host or changes consent. A failed write may leave a partial output directory;
use a new destination after correcting the failure. It emits a fixed JSON
summary or error code, without filesystem diagnostics.

Keep the entire staged directory at a stable location. Register the matching
executable explicitly using its packaged registration helper:

```sh
node /absolute/path/nexus-companion-host/apps/companion-native-host/bin/nexus-companion-native-host-registration.mjs install \
  --browser chrome --extension-id <published-extension-id> \
  --host-path /absolute/path/nexus-companion-host/apps/companion-native-host/bin/nexus-companion-native-host-chrome.mjs
```

Use the Edge launcher and `--browser edge` for Edge. The launchers use
`/usr/bin/env node`; the browser's launch environment must find Node.js 22.13+.
Moving or deleting the staged directory invalidates existing registrations.
Removal of the exact manifest through the helper leaves local history intact.
Source tests execute both packaged launchers, packaged migrations/helper and
registration/uninstall against isolated HOME directories, including a package
path containing spaces. They do not establish live browser compatibility.

See [platform evidence](../../docs/companion-support-matrix.md). Desktop data
controls can find the staged shared helper via `NEXUS_REPO` set to its absolute
root; live desktop invocation and installer configuration remain release gates.
