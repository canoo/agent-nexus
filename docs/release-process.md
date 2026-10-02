# Release Process

Every NEXUS version is a coordinated product release. A tag is created only
after the code, release notes, website, and community announcement are ready.

## Release gates

1. **Scope:** the milestone's acceptance criteria and supported platforms pass.
2. **Verification:** run the focused Node and Go tests, install-cycle tests, and
   platform packaging checks appropriate to the release.
3. **Release log:** move verified entries from `CHANGELOG.md`'s `Unreleased`
   section into a dated version heading. The GitHub Release body summarizes the
   same user-visible changes, upgrades, limitations, and rollback notes.
4. **Website:** update `../nexus-site` in a separate, reviewable commit:
   release/changelog page, supported-platform claims, installation instructions,
   and any new product surface. Record the site commit SHA in the NEXUS release
   PR or release notes.
5. **Community:** prepare a concise Discord `#announcements`/`#changelog` post
   and any approved social copy. It must link the GitHub Release and mention
   breaking changes, privacy-impacting changes, and platform availability.
6. **Publish:** merge the NEXUS release PR, tag `vX.Y.Z`, let the release
   workflow create artifacts, then publish the already reviewed site update and
   send the prepared announcements.
7. **Verify:** install the published artifact on every declared platform,
   confirm the website links resolve, and record any follow-up issue rather than
   silently revising release claims.

No credentials, private user activity, prompts, or browser data belong in a
changelog, site copy, GitHub Release, Discord post, or social post.

## Desktop Companion release requirements

The initial desktop target is Linux and macOS only.

- **macOS:** the menu-bar icon and app bundle must be tested on supported macOS
  versions. Public distribution requires code signing, notarization, and
  stapling before it is advertised as a supported install path.
- **Linux:** test a visible tray menu on supported desktop environments. A
  launcher/CLI fallback is required because some Linux environments disable or
  omit tray support. Flatpak is a supported distribution track only after its
  sandbox permissions and native-messaging behavior have an automated/manual
  verification record.
- **All platforms:** the menu exposes status, privacy capture state, a link to
  the dashboard, pause/quit controls, and an explanation of what is collected.
  It must never display conversation content.

## Version mapping

| Version | Release condition |
|---|---|
| v0.2.1 | SQLite/JSONL observability migration, Tokscale ingestion, dashboard, health, and retention guidance are complete. |
| v0.2.2 | Companion private preview: consented browser signals plus the Linux/macOS desktop shell and status interface pass privacy and packaging gates. |
| v0.2.5 | Tool sync/adopt integration can install Companion configuration without copying device-local consent or history. |

## Release ownership

- NEXUS repository: implementation, `CHANGELOG.md`, GitHub Release, artifacts.
- `nexus-site` repository: product, platform, install, and changelog-page copy.
- Discord/social: release announcement based on approved notes; publish only
  after the GitHub Release is live.

The two repository commits remain independent but each release record links
both SHAs so site content and shipped artifacts can be audited together.
