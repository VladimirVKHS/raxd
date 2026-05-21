# Guardian `qa-guardian`

## Purpose
Read-only quality gate for the `qa` artifacts (`test-plan.md` + tests). Checks completeness of the
`AC → test` matrix, absence of skips/disabling, and coverage of security cases and the install-flow.

## When invoked
Automatically as a gate after `qa`, before reviewer. Explicit: `@qa-guardian`.

## Input → Output
- Input: `specs/<task-id>/test-plan.md` + tests, `spec.md`, contract `.claude/agents/qa/qa.md`,
  `SECURITY-BASELINE.ru.md`.
- Output: a report (verdict `pass|needs-changes|blocked`) the orchestrator saves to
  `specs/<task-id>/guardians/qa-guardian.md`.

## Tools (scope) and why
`Read, Grep, Glob` — **Verifier** tier. No `Write/Edit/Bash/Skill`: review independence is
guaranteed at the access level, not by promise.

## Red lines
Changes nothing and writes no tests; `pass` only with a full AC matrix, covered security/install
cases and no skips; no nitpicking on taste.

> Note: the canonical guardian prompt (`qa-guardian.md`) is in Russian by project decision.
