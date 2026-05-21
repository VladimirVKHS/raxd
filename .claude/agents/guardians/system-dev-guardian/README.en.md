# Guardian `system-dev-guardian`

## Purpose
Read-only quality gate for the `system-dev` artifact (`service-design.md` + service files). Checks
coverage of both OSes (systemd+launchd), non-root/capabilities, the build matrix (4 targets), the
lifecycle with auto-restart, the git-flow branch, plan compliance, and the stack.

## When invoked
Automatically as a gate after `system-dev`, before developer/devops. Explicit:
`@system-dev-guardian`.

## Input → Output
- Input: `specs/<task-id>/service-design.md` + service files, `plan.md`, contract
  `.claude/agents/system-dev/system-dev.md`, `STACK.ru.md`, `SECURITY-BASELINE.ru.md`.
- Output: a report (verdict `pass|needs-changes|blocked`) the orchestrator saves to
  `specs/<task-id>/guardians/system-dev-guardian.md`.

## Tools (scope) and why
`Read, Grep, Glob` — **Verifier** tier. No `Write/Edit/Bash/Skill`: review independence is
guaranteed at the access level, not by promise.

## Red lines
Changes nothing; `pass` only when mandatory items are satisfied; no nitpicking on taste.

> Note: the canonical guardian prompt (`system-dev-guardian.md`) is in Russian by project decision.
