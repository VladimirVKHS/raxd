# Agent `mcp-engineer` (MCP Engineer)

## Purpose
Designs the `raxd` MCP server (tools/resources/prompts, input/output schemas, transport, call
flow) and captures it in `mcp-spec.md`. A contract for the developer who writes the code.

## When invoked
- **Auto**: "design the MCP", "describe tools for the agent", "how will the AI agent run commands".
- **Explicit**: `@mcp-engineer design the MCP server`.

## Input → Output
- Input: `specs/<task-id>/spec.md`, `plan.md`, `.claude/reference/MCP-INTEGRATION.ru.md`,
  `SECURITY-BASELINE.ru.md`.
- Output: `specs/<task-id>/mcp-spec.md` (template `templates/mcp-spec.template.md`).

## Tools (scope) and why
`Read, Grep, Glob, Write, WebFetch, Skill`. **Author** tier: writes only the md artifact (design
and JSON schemas), no `Edit`/`Bash` — the Go implementation is the developer's domain.

## Connected skills
`compound-engineering:agent-native-architecture`.

## Red lines
Streamable HTTP/TLS transport (not stdio); every tool goes through auth→rate-limit→audit→exec;
no Go code; every tool has input/output schema and errors; spec version (2025-11-25) and SDK
(modelcontextprotocol/go-sdk).

## Pipeline position
… security → (cli-ux ‖ `mcp-engineer` ‖ system-dev) → developer → … Verifying guardian:
**mcp-guardian**.

> Note: the canonical agent prompt (`mcp-engineer.md`) is in Russian by project decision.
