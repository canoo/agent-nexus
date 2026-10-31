# Chrome publication gate — v0.3.0

## Build the review artifact
From the repository root, run:

```sh
node tools/automation/build-chrome-extension.mjs --version 0.3.0-extension.1 --output /tmp/nexus-extension-store
```

Use a new absolute output directory outside the checkout. The deterministic ZIP
has `manifest.json` at its root and includes only the runtime allowlist. `build.json`
records its SHA-256 and leaves publication and live GUI verification false. The
adjacent `extension/` directory can be loaded unpacked for review. This creates no
desktop artifact and does not register a native host or upload anything.

## Implemented controls
The options page is the NEXUS Control Center: Install, Configure, Health, Task Log,
Usage, Update, Uninstall, Memory and Routing plus Companion setup, pause/resume,
retention/prune/clear/disable, native policy consent and Chrome host registration.
All call the existing core control workers through a bounded, fixed operation API.
No core implementation is duplicated. Route preview is local; execution requires
confirmation, and cloud use requires a separate unchecked acknowledgement.

The native host must already be installed and registered by the Companion package
before any extension control can connect. The extension cannot bootstrap an absent
native host. Install/update/uninstall additionally require the full trusted NEXUS
checkout and `~/.local/bin/nexus` with the v1 control protocol. An unavailable host
produces a visible recovery message, never simulated success. Long operations need
the Control Center to remain open. Native unregister disconnects subsequent calls.

## Permission audit
Required permissions remain exactly `storage` (local per-tool browser consent and
session span lifecycle) and `nativeMessaging` (fixed Companion host only).
Six optional supported AI origins are requested per explicit tool gesture and
revoked when switched off. No broad host grant, `tabs`, content scripts, scripting,
webRequest, remote script, externally connectable endpoint or web accessible
resource is added. Chrome exposes tab URLs only for granted origins. Provider
logos are bundled; there are no tracking requests or remotely loaded assets.
The same browser activity privacy exclusions remain in effect. Explicit routing
and memory forms send the user-authored content to the local core; cloud routing
may transmit task content only after acknowledgement.

Boundary requests reject unknown fields/operations, arbitrary paths and shell
arguments, invalid component names, out-of-range retention and unconfirmed
mutations. Native invocation uses a fixed executable with stdin JSON, no shell,
a timeout and output cap. Replies are versioned, operation correlated and bounded;
rendering uses DOM text nodes rather than host HTML. Chrome control operations are
rejected by the Edge host. Activity validation and consent lifecycle are unchanged.

## Automated evidence (2026-10-30)
- Focused extension, control-adapter and native-message checks: 60 passed.
- Combined extension/native suite: 93 tests, 67 passed, 26 failed. The same 26
  existing subprocess cases fail in the baseline: `spawnSync` reports `EPERM`
  in this restricted environment, including runtime verification. No new failing
  cases were introduced; this is not a green integration-suite result.
- Four in-process native packaging checks pass, including relocated imports of
  the Chrome adapter and its validator without the source checkout.
- Deterministic store ZIP construction passes its regression test. Independent
  Python ZIP validation checks all entry CRCs, the root manifest and permissions.
- Actual Chrome rendering and native-host interactions remain unverified because
  browser access is unavailable in this session. Mock DOM tests cover navigation,
  confirmations, local/cloud defaults and control dispatch, not visual acceptance.

## Required before publishing (Emi owns release approval)
- Emi chooses publisher identity and Chrome Web Store account; no identity assumed.
- Finish a clean live Chrome GUI test: load packaged extension, install/register the
  native host for its real extension ID, first permission click/popup dismissal,
  restart browser and worker, revoke/regrant every tool, offline/invalid-host errors,
  companion collection and retention, each lifecycle view in an isolated installation,
  memory CRUD, route preview/local run and explicit cloud acknowledgement.
- Confirm accessible keyboard navigation, focus, contrast and screen-reader labels
  in actual Chrome; collect store screenshots and promotional assets.
- Review product icon, logo trademark usage and license attribution. Six provider
  marks are copied from the repository Simple Icons set. Local Qwen/Llama models
  use the Ollama runtime logo with the model name and runtime tooltip. Unknown
  models use a neutral fallback; add licensed family logos as support expands.
- Approve public privacy policy URL and accurate store data-use declarations,
  support/contact URL, descriptions, category, countries and store listing assets.
- Verify native package signing/distribution and document supported Linux/Chrome
  runtime prerequisites. A Web Store ZIP does not contain or install a native host.
- Re-run build/tests on Saturn outside restricted subprocess environments, finish
  Emi's live GUI gate, and approve the exact artifact checksum.
- Explicitly approve publication. This track creates an artifact only; it does not
  submit a store listing, create a PR, merge #115 or merge to main.
