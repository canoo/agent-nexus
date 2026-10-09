# Next release plan: Desktop Companion private preview

**Planned version:** v0.3.0. v0.2.2 shipped as a stability release. An opt-in
browser extension, native host, desktop controls, and new activity storage are
a new product surface, so a minor version is clearer than another v0.2.x patch.
The existing v0.3.0 milestone already contains the shared-settings and dynamic
router foundation. Companion becomes its headline; routing is supporting work.

This is a plan, not a claim that Companion is available in a release.
Implementation evidence and remaining gates are tracked in
[v0.3.0-work-tracker.md](v0.3.0-work-tracker.md).
[PR #115](https://github.com/canoo/agent-nexus/pull/115) integrates the rebased PR #110
privacy/storage foundation and remains under review. Shared project memory is scoped
in v0.3.0 as CLI/TUI now, with a possible typed CLI JSON bridge for desktop later; #100
internal-package extraction is a separate follow-up, not required for the existing memory feature.
Memory is user-authored context, separate from consent-gated Companion activity (no automatic
decision capture, synchronization, or GUI memory view exists yet).

## Private-preview scope

1. Merge the disabled-by-default consent, retention, and allowlisted activity
   store from [#104](https://github.com/canoo/agent-nexus/issues/104) / #110.
   Unknown or sensitive fields must be rejected before persistence; activity
   must stay separate from the MCP Task Log.
2. Add a Chrome/Edge Manifest V3 extension with per-tool consent and preview
   ([#105](https://github.com/canoo/agent-nexus/issues/105)). Observe only
   selected-tab transitions for supported origins. No content scripts or
   blanket URL permission.
3. Add an allowlisted native-messaging host with bounded, fixed-envelope
   messages, diagnostics, and uninstall behavior
   ([#106](https://github.com/canoo/agent-nexus/issues/106)).
4. Show activity in its own TUI view, with clear source and disabled states;
   never infer task correlation from timestamps
   ([#107](https://github.com/canoo/agent-nexus/issues/107)).
5. Connect Linux/macOS desktop consent, pause, and host controls, including a
   no-tray Linux fallback ([#108](https://github.com/canoo/agent-nexus/issues/108)).
6. Validate Flatpak permissions, native-host packaging, and the supported
   desktop/browser matrix before advertising those combinations
   ([#109](https://github.com/canoo/agent-nexus/issues/109)).

## Desktop product direction

The desktop application should make NEXUS useful without requiring terminal
knowledge. The user experience standard is a polished application such as
1Password: clear onboarding, understandable status, explicit actions, and
consistent behavior across desktop and CLI interfaces. This does not imply
that NEXUS uses the same desktop framework as 1Password.

Deliver the workflows incrementally:

| Workflow | Existing foundation | Desktop work |
|---|---|---|
| Privacy and browser integration | Shared consent store and native host | Explicit consent, pause/resume, retention and data controls; first preview gate |
| Project memory | Local Markdown CLI and TUI in `a914ae7` | Project selection, search, reading and explicit saving through the same Go operations |
| Readiness and setup | TUI configuration and health checks | Guided readiness view and clear remediation; model checks from #35/#43 |
| Tasks and cost | SQLite task log and #113 cost summary | Read-only summaries with routing costs separate from Companion activity |
| Prompt routing | #102/#103 router and shared settings | Visible local/cloud destination and explicit execution; cloud fallback remains an explicit choice |

These are desktop delivery steps, not a claim that every workflow is already
implemented or a commitment to full CLI parity in the first preview. Confirm
usable scope and tests before advertising a workflow in the release.
Project memory is intentional user-authored content; the prohibition on project
names and arbitrary browser content applies to Companion activity collection,
not to a separate memory screen explicitly opened by the user.

Keep the Go CLI/TUI operations as the source of behavior for their workflows;
the Tauri shell can invoke narrow typed interfaces. The complete #100 command
and internal-package extraction is recommended for v0.3.1, after these
interfaces are exercised. Small interface changes needed by a v0.3.0 workflow
can ship with that workflow without attempting the full reorganization.
This recommendation does not change GitHub milestones without approval.

CLI access remains a supported interface for AI agents and scripts. Existing
`nexus status --json`, routing dry-run JSON, and project-memory commands remain
available as desktop workflows are added. Shared operations should expose
structured input/output and meaningful exit codes, with explicit consent for
side effects; the GUI must not become the only way to operate NEXUS. Memory
currently returns text, so its future desktop bridge needs structured output.

## Release gates

- Collection starts disabled and can only be enabled by explicit per-tool
  consent. Pausing and revoking consent stop collection immediately.
- Companion activity never stores, logs, displays, or exports URLs, titles,
  DOM content, prompts, responses, accounts, project names, or arbitrary browser
  payloads. User-authored project memory remains a separate explicit workflow.
- Negative tests cover unknown senders, malformed or oversized messages,
  unsupported origins, and disabled/paused states.
- Linux and macOS install, status, pause, quit, and uninstall paths are tested.
  Unsupported Flatpak/native-host combinations fail closed and are documented.
- Task Log, Companion activity, and optional Tokscale aggregates remain
  explicitly separate in the TUI.

## Milestone cleanup (applied)

| Milestone | Change |
|---|---|
| #2 `v0.3.0` | Rename to `v0.3.0 — Desktop Companion Private Preview`; describe Companion as the release gate and shared settings/router as supporting work. Move #104–#109 and PR #110 here. |
| #8 `v0.2.5 — Universal Sync Layer` | Rename to `v0.3.1 — Universal Sync Layer` so the next release can be v0.3.0 without an out-of-order version. |
| New `Backlog — unscheduled` | Keep open work visible without making it a v0.3.0 release gate. |
| #5 `v0.2.1 — CLI Usage Ingestion` | Reassign all 25 open issues as below, then close the stale milestone. No issues are closed. |
| #11 `v0.2.2 — NEXUS Companion Private Preview` | Move #104–#109 to #2, then close this stale milestone. |

Reassign the 25 open issues from #5:

| Destination | Issues | Reason |
|---|---|---|
| v0.3.0 Companion/router | #35, #40, #43 | Runtime settings and model readiness support routing. |
| v0.3.1 Universal Sync | #44, #45, #47, #61 | Machine-readable health, doctor, symlink verification, and install efficiency support adoption. |
| v0.4.0 Personas | #48, #59, #60 | Core instructions and persona roster/content work. |
| Backlog — unscheduled | #22–#26, #38, #49, #50, #52, #53, #55, #56, #58, #63, #71 | Tokscale follow-ups, TUI/installer fixes, and release/test hygiene need triage but are not Companion gates. |

Move existing #13, #14, #28, #29, #64, and #69 from v0.3.0 to the backlog;
they are routing and quality follow-ups. Leave #100 on v0.3.0 until the
#56-versus-#100 design decision. Keep #12 and #99 as the router foundation.

The milestone changes above were approved and applied. The two stale milestones
were closed only after their open issues moved; no issues were closed.
