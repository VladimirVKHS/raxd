# Guardian `mcp-guardian`

## Purpose
Read-only quality gate for the `mcp-engineer` artifact (`mcp-spec.md`). Checks the transport
(Streamable HTTP/TLS), presence of schemas and errors for every tool, the call flow, spec/SDK
versions, absence of Go code, and security coverage.

## When invoked
Automatically as a gate after `mcp-engineer`, before developer. Explicit: `@mcp-guardian`.

## Input → Output
- Input: `specs/<task-id>/mcp-spec.md`, contract `.claude/agents/mcp-engineer/mcp-engineer.md`,
  `MCP-INTEGRATION.ru.md`, `SECURITY-BASELINE.ru.md`.
- Output: a report (verdict `pass|needs-changes|blocked`) the orchestrator saves to
  `specs/<task-id>/guardians/mcp-guardian.md`.

## Tools (scope) and why
`Read, Grep, Glob` — **Verifier** tier. No `Write/Edit/Bash/Skill`: review independence is
guaranteed at the access level, not by promise.

## Red lines
Changes nothing; `pass` only when mandatory items are satisfied; no nitpicking on taste.

> Note: the canonical guardian prompt (`mcp-guardian.md`) is in Russian by project decision.
