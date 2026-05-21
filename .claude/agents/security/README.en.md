# Agent `security` (Security)

## Purpose
Builds the threat model and writes verifiable security requirements for `raxd` based on the
mandatory `SECURITY-BASELINE.ru.md`. `raxd` runs arbitrary commands over the network — every risk
is recorded with a mitigation.

## When invoked
- **Auto**: "threat model for…", "security requirements for…", "how to protect keys/TLS/exec/audit".
- **Explicit**: `@security spec out threats and requirements for X`.

## Input → Output
- Input: `specs/<task-id>/spec.md`, `specs/<task-id>/plan.md`, `.claude/reference/SECURITY-BASELINE.ru.md`,
  `.claude/reference/MCP-INTEGRATION.ru.md`.
- Output: `specs/<task-id>/threat-model.md` + `specs/<task-id>/security-requirements.md`
  (templates `templates/threat-model.template.md`, `templates/security-requirements.template.md`).

## Tools (scope) and why
`Read, Grep, Glob, Write, WebFetch, Skill`. **Author** tier: writes only md artifacts (requirements),
no `Edit`/`Bash` — implementation is out of scope.

## Connected skills
`security-review`.

## Red lines
No "simplifying" security for speed; every risk has a mitigation; requirements are verifiable; no
implementation code; an infeasible baseline item → risk + mitigation + escalation.

## Pipeline position
architect → `security` → developer/system-dev/devops/mcp-engineer (must comply); checked by
reviewer and security-guardian. Verifying guardian: **security-guardian**.

> Note: the canonical agent prompt (`security.md`) is in Russian by project decision.
