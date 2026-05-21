# Guardian `cli-ux-guardian`

## Purpose
Read-only quality gate for the `cli-ux` artifact (`ux-spec.md`). Checks the presence of output states
with ASCII layouts, the author in the banner, `NO_COLOR`/narrow-terminal handling, reliance on the
charm/tablewriter stack, the absence of Go code, and the clarity of error texts.

## When invoked
Automatically as a gate after `cli-ux`, before developer. Explicit: `@cli-ux-guardian`.

## Input → Output
- Input: `specs/<task-id>/ux-spec.md`, contract `.claude/agents/cli-ux/cli-ux.md`, `STACK.ru.md`.
- Output: a report (verdict `pass|needs-changes|blocked`) the orchestrator saves to
  `specs/<task-id>/guardians/cli-ux-guardian.md`.

## Tools (scope) and why
`Read, Grep, Glob` — **Verifier** tier. No `Write/Edit/Bash/Skill`: review independence is
guaranteed at the access level, not by promise.

## Red lines
Changes nothing; `pass` only when states are covered, the author is in the banner, and there is no Go
code; no nitpicking on design taste.

> Note: the canonical guardian prompt (`cli-ux-guardian.md`) is in Russian by project decision.
