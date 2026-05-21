# Agent `cli-ux` (CLI UX)

## Purpose
Designs the beautiful console output of `raxd`: the author banner, the install status block, tables
(`key list`), colors/style, command and error texts. Layouts are ASCII, based on the charm/tablewriter
stack.

## When invoked
- **Auto**: "design the output for…", "how should the banner/status/table look", "error texts for…".
- **Explicit**: `@cli-ux design the output of X`.

## Input → Output
- Input: `specs/<task-id>/spec.md`, `specs/<task-id>/plan.md`, `.claude/reference/STACK.ru.md`.
- Output: `specs/<task-id>/ux-spec.md` (template `templates/ux-spec.template.md`).

## Tools (scope) and why
`Read, Grep, Glob, Write, WebFetch, Skill`. **Author** tier: writes only the md artifact (output/texts/
ASCII layouts), no `Edit`/`Bash` — implementation is out of scope.

## Connected skills
`compound-engineering:frontend-design`.

## Red lines
Author "Vladimir Kovalev, OEM TECH" is mandatory in the banner; account for `NO_COLOR` and narrow
terminals; no implementation Go code; rely on the charm/tablewriter stack; clear error texts.

## Pipeline position
architect → `cli-ux` (in parallel with mcp-engineer/system-dev) → developer (implements). Verifying
guardian: **cli-ux-guardian**.

> Note: the canonical agent prompt (`cli-ux.md`) is in Russian by project decision.
