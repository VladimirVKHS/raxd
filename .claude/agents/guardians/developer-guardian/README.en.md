# Guardian `developer-guardian`

## Purpose
Read-only quality gate for the `developer` work (code on the feature branch + `impl-notes.md`).
Checks conformance to `plan.md`, absence of out-of-scope functionality, presence of green tests,
code security, and git-flow.

## When invoked
Automatically as a gate after `developer`, before reviewer/qa. Explicit: `@developer-guardian`.

## Input → Output
- Input: changed code, `specs/<task-id>/impl-notes.md`, `plan.md`, `security-requirements.md`,
  `.claude/reference/SECURITY-BASELINE.ru.md`, contract `.claude/agents/developer/developer.md`,
  `guides/GIT-FLOW-GUIDE.ru.md`.
- Output: a report (verdict `pass|needs-changes|blocked`) the orchestrator saves to
  `specs/<task-id>/guardians/developer-guardian.md`.

## Tools (scope) and why
`Read, Grep, Glob` — **Verifier** tier. No `Write/Edit/Bash/Skill`: review independence is
guaranteed at the access level, not by promise.

## Red lines
Changes nothing; `pass` only when mandatory items are satisfied (green tests, security, plan
conformance); no nitpicking on taste.

> Note: the canonical guardian prompt (`developer-guardian.md`) is in Russian by project decision.
