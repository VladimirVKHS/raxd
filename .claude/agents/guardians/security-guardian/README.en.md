# Guardian `security-guardian`

## Purpose
Read-only quality gate for the `security` artifacts (`threat-model.md`, `security-requirements.md`).
Checks coverage of all SECURITY-BASELINE sections, a mitigation for every risk, requirement
verifiability, and the absence of code and security "shortcuts".

## When invoked
Automatically as a gate after `security`, before developer/system-dev/devops/mcp-engineer.
Explicit: `@security-guardian`.

## Input → Output
- Input: `specs/<task-id>/threat-model.md` + `security-requirements.md`, contract
  `.claude/agents/security/security.md`, `SECURITY-BASELINE.ru.md`.
- Output: a report (verdict `pass|needs-changes|blocked`) the orchestrator saves to
  `specs/<task-id>/guardians/security-guardian.md`.

## Tools (scope) and why
`Read, Grep, Glob` — **Verifier** tier. No `Write/Edit/Bash/Skill`: review independence is
guaranteed at the access level, not by promise.

## Red lines
Changes nothing; `pass` only when baseline sections are covered and every risk has a mitigation; no
nitpicking on taste.

> Note: the canonical guardian prompt (`security-guardian.md`) is in Russian by project decision.
