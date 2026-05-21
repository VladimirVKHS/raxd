# Agent `developer`

## Purpose
Writes `raxd` Go code strictly from the approved artifacts (`spec.md`, `plan.md`,
`security-requirements.md`, `ux-spec.md`, `mcp-spec.md`, `service-design.md`) with tests and
atomic commits on a feature branch per git-flow. Implementation step of the `raxd` pipeline.

## When invoked
- **Auto**: "implement the feature…", "write the code per the plan…", "code up key create".
- **Explicit**: `@developer implement task X`.

## Input → Output
- Input: all task artifacts (`spec.md`, `plan.md`, `security-requirements.md`, `ux-spec.md`,
  `mcp-spec.md`, `service-design.md`), `.claude/reference/*`, `guides/GIT-FLOW-GUIDE.ru.md`, repo code.
- Output: code + tests on a feature branch (atomic commits per git-flow) and
  `specs/<task-id>/impl-notes.md` (template `templates/impl-notes.template.md`).

## Tools (scope) and why
`Read, Write, Edit, Bash, Grep, Glob, Skill`. **Builder** tier: beyond the md artifact it writes and
edits code (`Edit`) and runs commands (`Bash`) — build, tests, git per flow.

## Connected skills
`superpowers:test-driven-development`, `superpowers:systematic-debugging`,
`superpowers:verification-before-completion`, `compound-engineering:ce-work`.

## Red lines
No silent deviation from `plan` (infeasibility → escalate); no functionality outside `spec` ("while
I'm at it" forbidden); dependencies only from `STACK`/`plan`; tests before commit — green, no `skip`;
security per `SECURITY-BASELINE` (crypto/rand + SHA-256 + salt + constant-time, `exec.Command` with
no shell, timeouts, audit, secret files `0600`); branch/commit per git-flow; never edit `spec`/`plan`.

## Pipeline position
… → security → (cli-ux ‖ mcp-engineer ‖ system-dev) → **developer** → (devops ‖ qa) → reviewer → …
Verifying guardian: **developer-guardian**.

> Note: the canonical agent prompt (`developer.md`) is in Russian by project decision.
