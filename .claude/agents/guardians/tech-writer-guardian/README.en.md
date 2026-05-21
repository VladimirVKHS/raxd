# Guardian `tech-writer-guardian`

## Purpose
Read-only quality gate for the `tech-writer` documentation (`docs/**`). Checks alignment with the
real code (nothing made up), presence of the OEM TECH author, correctness of command examples,
coverage completeness (install/commands/MCP/troubleshooting), and clarity.

## When invoked
Automatically as the final gate after `tech-writer`. Explicit: `@tech-writer-guardian`.

## Input → Output
- Input: `docs/**`, `specs/<task-id>/docs-outline.md`, contract `.claude/agents/tech-writer/tech-writer.md`,
  `STACK.ru.md`, source code (for verification).
- Output: a report (verdict `pass|needs-changes|blocked`) the orchestrator saves to
  `specs/<task-id>/guardians/tech-writer-guardian.md`.

## Tools (scope) and why
`Read, Grep, Glob` — **Verifier** tier. No `Write/Edit/Bash/Skill`: review independence is
guaranteed at the access level, not by promise.

## Red lines
Changes nothing; `pass` only when there is no fabrication, examples are correct, and coverage is
complete; no nitpicking on taste.

> Note: the canonical guardian prompt (`tech-writer-guardian.md`) is in Russian by project decision.
