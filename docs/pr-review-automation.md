# PR Review Automation

NEXUS uses free and open-source review automation where possible. The goal is to
keep pull request feedback deterministic, transparent, and easy to run locally.

## Enabled

- **reviewdog + ShellCheck** annotates shell script issues on pull requests.
- **reviewdog + actionlint** annotates GitHub Actions workflow issues on pull
  requests.
- Existing CI still runs Go build, `go vet`, `go test`, ShellCheck, and the
  install-cycle test.

The reviewdog GitHub Actions are pinned to immutable commit SHAs in
`.github/workflows/reviewdog.yml`.

## Disabled

CodeRabbit automatic reviews are disabled in `.coderabbit.yaml`. To fully
disconnect CodeRabbit, remove the GitHub App installation from the repository or
organization settings.

## Candidate Additions

- **Semgrep OSS** for security and bug-pattern scanning.
- **PR-Agent / Qodo Merge OSS** only if it can be routed through a free or local
  model provider.
- **Danger JS** for repository policy checks that are awkward to express as
  linters.
