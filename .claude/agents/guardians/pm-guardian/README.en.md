# Guardian `pm-guardian`

## Purpose
Read-only quality gate for the `pm` artifact (`spec.md`). Checks section completeness, AC
verifiability, absence of code/architecture, and security coverage.

## When invoked
Automatically as a gate after `pm`, before research-analyst/architect. Explicit: `@pm-guardian`.

## Input → Output
- Input: `specs/<task-id>/spec.md`, contract `.claude/agents/pm/pm.md`, `SECURITY-BASELINE.ru.md`.
- Output: a report (verdict `pass|needs-changes|blocked`) the orchestrator saves to
  `specs/<task-id>/guardians/pm-guardian.md`.

## Tools (scope) and why
`Read, Grep, Glob` — **Verifier** tier. No `Write/Edit/Bash/Skill`: review independence is
guaranteed at the access level, not by promise.

## Red lines
Changes nothing; `pass` only when mandatory items are satisfied; no nitpicking on taste.

> Note: the canonical guardian prompt (`pm-guardian.md`) is in Russian by project decision.
