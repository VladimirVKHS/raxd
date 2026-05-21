# Agent `pm` (Product Manager)

## Purpose
Turns a user request into a `spec.md` with verifiable acceptance criteria and explicit scope.
First step of the `raxd` pipeline.

## When invoked
- **Auto**: "describe requirements for…", "what should X do", "write acceptance criteria for…".
- **Explicit**: `@pm spec out task X`.

## Input → Output
- Input: user request, repo code, `.claude/reference/STACK.ru.md`, `SECURITY-BASELINE.ru.md`.
- Output: `specs/<task-id>/spec.md` (template `templates/spec.template.md`).

## Tools (scope) and why
`Read, Grep, Glob, Write, WebFetch, Skill`. **Author** tier: writes only the md artifact, no
`Edit`/`Bash` — code and architecture are out of scope.

## Connected skills
`superpowers:brainstorming`, `compound-engineering:ce-brainstorm`, `superpowers:writing-plans`.

## Red lines
No code in spec; no architecture decisions; ambiguity → `Open Questions`; only verifiable AC.

## Pipeline position
`pm` → research-analyst → architect → … Verifying guardian: **pm-guardian**.

> Note: the canonical agent prompt (`pm.md`) is in Russian by project decision.
