# Guardian `research-guardian`

## Purpose
Read-only quality gate for the `research-analyst` artifacts (`research.md` and ADRs). Checks that
every fact has a URL, that options are compared with a recommendation, that ADRs are complete, that
architecture was not chosen for the architect, and that source recency is accounted for.

## When invoked
Automatically as a gate after `research-analyst`, before architect. Explicit: `@research-guardian`.

## Input → Output
- Input: `specs/<task-id>/research.md`, `specs/<task-id>/decisions/*`, contract
  `.claude/agents/research-analyst/research-analyst.md`, `STACK.ru.md`, `SECURITY-BASELINE.ru.md`.
- Output: a report (verdict `pass|needs-changes|blocked`) the orchestrator saves to
  `specs/<task-id>/guardians/research-guardian.md`.

## Tools (scope) and why
`Read, Grep, Glob` — **Verifier** tier. No `Write/Edit/Bash/Skill`: review independence is
guaranteed at the access level, not by promise.

## Red lines
Changes nothing; `pass` only when facts carry URLs and ADRs are complete; no nitpicking on taste.

> Note: the canonical guardian prompt (`research-guardian.md`) is in Russian by project decision.
