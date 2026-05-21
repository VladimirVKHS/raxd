# Guardian `architect-guardian`

## Purpose
Read-only quality gate for the `architect` artifact (`plan.md`). Checks that exactly one approach is
chosen, modules have concrete paths, contracts carry types and error handling, there are no function
bodies, spec AC are unchanged, and new dependencies are justified and checked against STACK.

## When invoked
Automatically as a gate after `architect`, before security/cli-ux/mcp-engineer/system-dev/developer.
Explicit: `@architect-guardian`.

## Input → Output
- Input: `specs/<task-id>/plan.md`, `specs/<task-id>/spec.md`, contract
  `.claude/agents/architect/architect.md`, `STACK.ru.md`.
- Output: a report (verdict `pass|needs-changes|blocked`) the orchestrator saves to
  `specs/<task-id>/guardians/architect-guardian.md`.

## Tools (scope) and why
`Read, Grep, Glob` — **Verifier** tier. No `Write/Edit/Bash/Skill`: review independence is
guaranteed at the access level, not by promise.

## Red lines
Changes nothing; `pass` only with a single approach, no function bodies, and unchanged AC; no
nitpicking on taste.

> Note: the canonical guardian prompt (`architect-guardian.md`) is in Russian by project decision.
