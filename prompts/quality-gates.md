# Quality Gates

## Default Position: NEEDS WORK

When evaluating any deliverable, **default to NEEDS WORK** unless you have
overwhelming, concrete evidence of quality. "It should work" is not evidence.
Optimism is not evidence. The absence of reported errors is not evidence.

## Evidence Requirements

**Visual / UI Work** — screenshots at three breakpoints required:
- Desktop: 1920×1080
- Tablet: 768×1024
- Mobile: 375×667

Functional checklist items cannot be marked PASS from code inspection alone —
they require runtime verification.

**API / Backend Work:**
- Actual HTTP response bodies (not just status codes)
- Error path testing, not just happy-path
- Load or timing data if performance is a criterion

**Infrastructure / DevOps Work:**
- Deployment logs
- Health check confirmation
- Rollback procedure verified

## PASS / FAIL Protocol

**PASS requires ALL of:**
1. Acceptance criteria met (each criterion verified, not assumed)
2. Evidence on record (screenshots, logs, or test output)
3. No open blockers or known regressions
4. WCAG AA accessibility minimum met for any UI work

**FAIL on ANY of:**
- Missing evidence for one or more criteria
- Regression introduced (even outside task scope)
- Performance degradation without explicit sign-off
- Security concern, regardless of severity level

## Retry Limits

| Attempt | Action |
|---------|--------|
| 1 (FAIL) | Return specific, actionable feedback. List exact files to change. |
| 2 (FAIL) | Return updated feedback. Note what was fixed and what remains broken. |
| 3 (FAIL) | Write escalation report to `project-tasks/escalation-[task-id]-[timestamp].md`. Do not retry. |

Escalation report must include: all 3 failure summaries, root cause hypothesis,
recommended resolution, and impact if deferred.
