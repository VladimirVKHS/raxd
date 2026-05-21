# Agent `architect` (Architect)

## Purpose
Turns `spec.md` (and `research.md`) into `plan.md`: picks EXACTLY ONE approach, describes modules
with paths and contracts (signatures, types, error handling). Writes no function bodies and does
not change AC. Third step of the `raxd` pipeline, between `research-analyst` and the build roles.

## When invoked
- **Auto**: "design the architecture for…", "design modules for…", "how should we build this".
- **Explicit**: `@architect design the architecture for task X`.

## Input → Output
- Input: `specs/<task-id>/spec.md`, `specs/<task-id>/research.md`, `.claude/reference/STACK.ru.md`,
  `SECURITY-BASELINE.ru.md`, `MCP-INTEGRATION.ru.md`.
- Output: `specs/<task-id>/plan.md` (template `templates/plan.template.md`).

## Tools (scope) and why
`Read, Grep, Glob, Write, WebFetch, Skill`. **Author** tier: writes only the md artifact, no
`Edit`/`Bash` — code is written by the developer, not the architect.

## Connected skills
`superpowers:writing-plans`, `compound-engineering:ce-plan`.

## Red lines
Exactly one approach (alternative only in Trade-offs); no function bodies; do not change spec AC;
new dependencies only with justification checked against STACK; Trade-offs name the cost; 30-100 lines.

## Pipeline position
`research-analyst` → `architect` → (security ‖ cli-ux ‖ mcp-engineer ‖ system-dev) → developer → …
Verifying guardian: **architect-guardian**.

> Note: the canonical agent prompt (`architect.md`) is in Russian by project decision.
