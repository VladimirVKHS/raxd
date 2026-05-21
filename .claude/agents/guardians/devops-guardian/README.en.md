# Guardian `devops-guardian`

## Purpose
Read-only quality gate for the `devops` work (`install.sh`, `.goreleaser.yaml`, CI +
`release-plan.md`). Checks installer safety, build-matrix completeness, service registration, macOS
quarantine handling, absence of secrets, and git-flow.

## When invoked
Automatically as a gate after `devops`, before reviewer/tech-writer. Explicit: `@devops-guardian`.

## Input → Output
- Input: `install.sh`, `.goreleaser.yaml`, `specs/<task-id>/release-plan.md`,
  `.claude/reference/SECURITY-BASELINE.ru.md`, contract `.claude/agents/devops/devops.md`,
  `guides/GIT-FLOW-GUIDE.ru.md`.
- Output: a report (verdict `pass|needs-changes|blocked`) the orchestrator saves to
  `specs/<task-id>/guardians/devops-guardian.md`.

## Tools (scope) and why
`Read, Grep, Glob` — **Verifier** tier. No `Write/Edit/Bash/Skill`: review independence is
guaranteed at the access level, not by promise.

## Red lines
Changes nothing; `pass` only when mandatory items are satisfied (SHA256 check, no secrets, full
matrix); no nitpicking on taste.

> Note: the canonical guardian prompt (`devops-guardian.md`) is in Russian by project decision.
