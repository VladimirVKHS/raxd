# Guardian `reviewer-guardian`

## Purpose
Read-only quality gate for the `reviewer` artifact (`review.md`). Checks that the review covered all AC
and contracts, the verdict is honest, issues follow the Where/Why/What-to-do form, and nothing is
blocked on style.

## When invoked
Automatically as a gate after `reviewer`, before tech-writer / return to developer.
Explicit: `@reviewer-guardian`.

## Input → Output
- Input: `specs/<task-id>/review.md`, `spec.md`, `plan.md`, contract `.claude/agents/reviewer/reviewer.md`.
- Output: a report (verdict `pass|needs-changes|blocked`) the orchestrator saves to
  `specs/<task-id>/guardians/reviewer-guardian.md`.

## Tools (scope) and why
`Read, Grep, Glob` — **Verifier** tier. No `Write/Edit/Bash/Skill`: review independence is
guaranteed at the access level, not by promise.

## Red lines
Changes nothing and does not rewrite the review; `pass` only with a full AC/contract sweep and an honest
verdict; no nitpicking on taste (including the report's own style).

> Note: the canonical guardian prompt (`reviewer-guardian.md`) is in Russian by project decision.
