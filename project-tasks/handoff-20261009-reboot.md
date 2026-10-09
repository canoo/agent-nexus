# NEXUS reboot handoff — 2026-10-09

## Current work

- Repository: `canoo/agent-nexus`.
- Integration PR: https://github.com/canoo/agent-nexus/pull/115
- Published implementation branch: `feat/v0.3.0-integration`.
- Latest implementation commit: `72db9b8aad35265379e91b1e862bbf856840aa8c`
  (`feat: include unpacked extension in Companion previews`).
- Local checkout: `feat/companion-preview`, same implementation commit.
- Main has not been merged or released by this work.
- Preserve the untracked `.aws` directory; do not inspect or stage it.
- Preserve the separate `preserved-102-103-rebase` worktree.

## Completed recent work

- `240260f`: move Companion data/status/setup/consent operations and tray work
  off the GUI event thread.
- `917cb03`: #35 model override validation before MCP storage/startup.
- `a0de525`: #43 selected-model readiness before MCP/shell inference;
  bounded `/api/show`, no automatic download/fallback, safe diagnostics.
- `72db9b8`: include allowlisted unpacked extension in native preview archives;
  preserve permissions, stamp staged version metadata, update installation guide.

Implementation CI is fully green at `72db9b8`:
https://github.com/canoo/agent-nexus/actions/runs/37834234809

Local verification: Go build/vet/tests; 71 MCP tests and zero audit vulnerabilities;
31 extension tests; four new extension staging cases on minimum Node 22.13;
real Linux dev.4 build plus artifact hashes/modes, extension policy/version,
runtime binding, isolated setup and packaged old-store upgrades. Earlier
integration evidence includes 15 Rust tests, 17 desktop UI tests and 38 host tests.

## User's next action: live preview validation

The user is rebooting the PC to test again and also intends to test on a Mac.
Automated process/VM/build evidence is not live GUI/browser validation.
Computer-use inventory in this session exposed no apps or browsers.

Apple Silicon artifact (unsigned, no live macOS claim):
https://github.com/canoo/agent-nexus/actions/runs/37834234809/artifacts/11574902357

Its CI label is `0.3.0-dev.1`, but its source is commit `72db9b8`.
Download the ZIP, extract its inner tar archive and follow its README. The
archive includes desktop `.app`, `extension/`, `host/` and `installer/`.
External Node 22.13+ and generated runtime-bound launchers are required.
The current artifact is ARM64; Intel Macs need a matching build.

Persistent Linux preview saved before reboot:
`/home/cano/workspace/nexus-previews/0.3.0-dev.4-linux-x64`

Run after reboot:
`/home/cano/workspace/nexus-previews/0.3.0-dev.4-linux-x64/launchers/nexus-companion`

Load the extension from:
`/home/cano/workspace/nexus-previews/0.3.0-dev.4-linux-x64/artifact/extension`

Browser host paths are in the same `launchers/` directory, named
`nexus-companion-native-host-chrome` and `nexus-companion-native-host-edge`.
The complete artifact was copied from the verified dev.4 build; the `.deb` was
extracted into `runtime/` and runtime-bound launchers regenerated against these
persistent paths. No GUI launch, browser registration or user-store access was
performed. No Debian package manager was used on Omarchy/Arch.

Old `/tmp/nexus-companion-linux-preview-dev4` and
`/tmp/nexus-preview-run.HrruJV` paths may disappear during reboot; use the
persistent paths above. For another machine, use the CI artifact or rebuild
following docs/companion-preview-install.md and apps/companion-desktop/README.md.

Validation sequence:
1. Open the generated Companion launcher; check window/no-tray usability.
2. Collection stays off. If missing, explicitly initialize the store; no grants.
3. Load archived `extension/` in native Chrome/Edge developer mode.
4. Use the actual per-browser extension ID and generated browser host launcher
   path to register through the dashboard. Registration does not enable collection.
5. Enable one tool and its origin permission in the extension; acknowledge and
   grant the same tool/browser in the desktop; explicitly resume collection.
6. Select the supported website for ~10 seconds, switch to an ordinary tab,
   wait for delivery, then pause in the desktop to refresh its stored count.
7. Verify paused/revoked states suppress new spans, removal preserves history,
   and the window remains responsive. Pause and remove registration when finished.

The normal launch uses the normal NEXUS store. Avoid clearing history or
shortening retention unless deletion is intended. Host startup applies owned
migrations and retention. A past bad fixture migrated the local store and paused
collection; no test activity was inserted/deleted. Subsequent automated tests use
isolated HOME/database fixtures exclusively.

## Remaining work and constraints

- Collect actual Linux/macOS GUI/browser permission, identity, worker lifecycle,
  pause/revoke, installation/upgrade/uninstall and runtime observations.
- macOS signing/notarization and browser-store/published identity remain gates.
- Audit #12's full routing scope separately from the implemented basic router.
- #56 screen split and `a914ae7` project memory are integrated. #100 full package
  extraction is recommended for v0.3.1; milestone reassignment remains unapproved.
- Use Agy for narrow routine generation, text-only prompts; review before tests.
- Work in branches with conventional commits. Never merge main without asking;
  never file/close issues. No release publication is authorized.
- Required checks: Go build/vet/tests in tools/tui; npm test/audit in tools/mcp.
- See docs/v0.3.0-work-tracker.md, docs/companion-release-plan.md and
  docs/companion-support-matrix.md for detailed evidence and scope.
