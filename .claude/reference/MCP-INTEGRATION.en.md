# raxd — MCP Integration (source of truth)

> Contract for `mcp-engineer` (designs) and `developer` (implements). `reviewer` verifies.
> Keep roles separate: mcp-engineer writes `mcp-spec.md`; developer writes the code.

## What MCP means here

Model Context Protocol is the standard by which an AI agent (client) asks a server for tools
and data. Spec version target: **2025-11-25**. Core concepts:
- **Tools** — actions the agent can call (here: run a command, upload a file).
- **Resources** — readable data/context (here: daemon status, command/capability list).
- **Prompts** — templates (optional, if needed).

## SDK

Official Go SDK: `github.com/modelcontextprotocol/go-sdk/mcp` (prefer over community
`mark3labs/mcp-go`). Skeleton:

```go
import "github.com/modelcontextprotocol/go-sdk/mcp"

server := mcp.NewServer(&mcp.Implementation{Name: "raxd", Version: version}, nil)

type ExecInput struct {
    Command string   `json:"command"`
    Args    []string `json:"args,omitempty"`
}
func execHandler(ctx context.Context, req *mcp.CallToolRequest, in ExecInput) (
    *mcp.CallToolResult, any, error) { /* exec.Command, timeout, audit */ }

mcp.AddTool(server, &mcp.Tool{Name: "exec", Description: "Run a command on the host"}, execHandler)
```

## Transport (important)

`raxd` serves remote network clients → **stdio is unsuitable**. Use **Streamable HTTP over TLS**:
- single endpoint, e.g. `https://<host>:<port>/mcp`;
- POST — client requests (JSON-RPC); GET — SSE server→client stream;
- stateless-friendly (sessions via `Mcp-Session-Id`);
- MANDATORY: TLS (see SECURITY-BASELINE), `Origin` validation, API-key auth
  (header, e.g. `Authorization: Bearer rax_live_…`) BEFORE executing any tool.

## Proposed tools/resources (first iteration)

- tool `exec` — run a command (in: command, args, opt. timeout; out: stdout/stderr/exit).
- tool `upload_file` — upload a file (in: path, content/base64 or stream; out: status, size).
- resource `status` — daemon state/version/uptime.
- resource `capabilities` — available commands/limits (incl. whether allowlist is active).

Every tool call flows: authenticate → rate-limit → audit-log → execute → audit result.

## Where to find details

Stack and paths — `STACK.en.md`. Security rules (keys, TLS, exec, audit) — `SECURITY-BASELINE.en.md`.
When spec/SDK currency is in doubt, `research-analyst` re-checks via WebFetch.
