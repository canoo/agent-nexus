# Saturn companion integration verification — 2026-10-27

Starting branch: `feat/v0.3.0-integration`.
Fetched head: `309b278ec0ff761a09ae0961589111cd8f7cd541` (unchanged from the
reported remote head). The stale local branch was fast-forwarded by 22 commits
before analysis. The initial working tree was clean.

## Branch audit

Actual `git merge --no-commit --no-ff origin/<branch>` trials ran in a disposable
detached worktree at the fetched head, in the order below. Every trial returned
`Already up to date.` No feature merge or conflict resolution was needed.

| Branch | Integrated tip |
| --- | --- |
| feat/companion-consent-controls | adfd1d3 |
| fix/companion-consent-writes | fad51fb |
| feat/companion-retention | 9394d67 |
| feat/companion-retention-verified | 0516b5e |
| fix/companion-pause-boundary | 36b22fa |
| fix/companion-browser-events | d0218f8 |
| fix/companion-native-acknowledgement | e51aade |
| test/companion-native-host-integration | ba03764 |
| feat/companion-host-package | 8e80496 |
| feat/companion-runtime-launchers | 0684f43 |
| fix/companion-unsupported-host-runtime | 9752f3e |
| fix/companion-helper-entry-path | aa56444 |
| feat/companion-store-setup | c6e1613 |
| test/companion-packaged-upgrade | 37c6fe2 |
| feat/companion-desktop-artifacts | ed99b64 |
| feat/companion-v0.3-integration | dbeaba3 |
| feat/companion-extension-artifact | 72db9b8 |
| feat/companion-host-removal | ef49050 |
| fix/companion-background-controls | 240260f |
| docs/companion-next-release-plan | abd5b25 |

The historical `feat/v0.2.1-observability-companion-foundation` tip `3ad81ac`
was skipped as superseded: six of nine commits are patch-identical according
to `git cherry HEAD origin/feat/v0.2.1-observability-companion-foundation`.
The remaining architecture, store and dashboard commits have evolved integrated
counterparts (`fdbde9f`, `4e4dde6`, `68b6f67`). No missing Linux feature was found.
PR #110 was not reopened or merged. PRs #116 and #119 were not merged.

## Verification

On Saturn (Linux amd64), Go 1.27.3, Node v25.7.0, npm 11.10.1, Cargo 1.99.1:

- In `tools/tui`: `go build ./...`, `go vet ./...`, `go test ./...` passed.
- In `tools/tui`: `go test -run '^TestMemoryBinaryEndToEnd$' -v .` passed,
  including actual CLI initialization, save, list, show and search in an isolated home.
- In `apps/companion-browser-extension`: `npm test` passed (31 tests).
- In `apps/companion-native-host`: `npm test` passed (38 tests), including
  relocated packages, launchers, upgrade boundaries and unsupported Flatpak runtime.
- In `tools/mcp`: `npm test` passed (71 tests).
- In `apps/companion-desktop`: `npm ci --cache /tmp/nexus-npm-cache`,
  `npm test` (17 tests), and `npm run check` passed. npm reported no vulnerabilities.
- From the repository root: `cargo test --manifest-path apps/companion-desktop/src-tauri/Cargo.toml --locked`
  passed (15 tests), including desktop-to-Node collection boundary enforcement.

Initial sandbox restrictions prevented writing the Go cache and fetching npm;
the same commands succeeded with approved access to the existing cache/network.

From the repository root, the Linux artifact build passed:

```sh
CARGO_TARGET_DIR=/tmp/nexus-linux-preview-target node tools/automation/build-companion-preview.mjs --version 0.3.0-dev.1 --output /tmp/nexus-linux-preview-20261027
node tools/automation/verify-companion-preview.mjs /tmp/nexus-linux-preview-20261027
```

The verifier passed with approved execution outside the sandbox after its first
sandboxed archive-extraction subprocess timed out. It verified hashes and modes,
extension policy/version, the packaged executable, runtime binding, fail-closed
host/helper behavior, acknowledged fresh setup and old-store upgrades. It did not
launch the GUI or install/register the package in the user's environment.

Artifacts are under `/tmp/nexus-linux-preview-20261027`, including
`desktop/NEXUS Companion_0.3.0-dev.1_amd64.deb` (4.33 MiB), `host/`, `extension/`,
`installer/` and `preview-manifest.json`. These are temporary local development
artifacts; `/tmp` may be cleared at reboot. No release was published.

## Distribution scope

The Flatpak manifest in `apps/companion-desktop/flatpak/` has empty modules and
permissions and is explicitly a placeholder. Current branch documentation
requires native-host compatibility and minimal portal validation before a real
recipe. This verification uses the documented native Linux `.deb` preview path.
It does not establish Flatpak support or live GUI/browser compatibility.

No macOS-only source files were changed. Live Linux GUI/browser lifecycle,
desktop-environment coverage, extension publication identity, and Flatpak native
messaging design remain release gates. macOS verification remains with the Mac agent.
