package mcp

// audit.go implements the withAudit decorator for MCP tool handlers.
//
// SR-35/AC9: every tools/call writes an audit record with:
//   - Fingerprint: from server.FingerprintFromContext(ctx) — NOT the key body
//   - Tool: MCP tool name (not a secret)
//   - Result: "success" or "fail"
//   - RemoteAddr: from server.RemoteAddrFromContext(ctx) — stored by authMiddleware
//
// SR-34: key body, hash, salt, and private TLS key MUST NOT appear in audit records.
// SR-28: this package does not call keystore.Verify.

import (
	"context"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vladimirvkhs/raxd/internal/server"
)

// withAudit wraps a ToolHandlerFor with pre/post audit logging.
//
// It uses server.FingerprintFromContext and server.RemoteAddrFromContext to
// retrieve values stored in the request context by authMiddleware. The SDK
// forwards req.Context() into every tool handler, so both values are available.
// The key body is NEVER accessible here (SR-35/SR-34).
//
// Parameters:
//   - name: MCP tool name (logged as Tool in AuditRecord; not a secret).
//   - h: the typed ToolHandlerFor to wrap.
//   - audit: the AuditFn injected from the transport layer.
func withAudit[In, Out any](name string, h sdkmcp.ToolHandlerFor[In, Out], audit server.AuditFn) sdkmcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *sdkmcp.CallToolRequest, input In) (*sdkmcp.CallToolResult, Out, error) {
		// SR-35/AC9: fingerprint and remote address from context — both stored by
		// authMiddleware before the request reaches the MCP SDK handler. The SDK
		// forwards req.Context() into tool handlers, so both values are available here.
		fp := server.FingerprintFromContext(ctx)
		remote := server.RemoteAddrFromContext(ctx)

		result, out, err := h(ctx, req, input)

		resultStr := "success"
		reason := ""
		if err != nil || (result != nil && result.IsError) {
			resultStr = "fail"
			if err != nil {
				reason = err.Error()
			}
		}

		// SR-35/SR-36: write audit record with tool name, fingerprint, result.
		// SECURITY: Reason is a short status string — no key body, no secrets.
		audit(server.AuditRecord{
			TS:          time.Now().UTC(),
			Fingerprint: fp,
			RemoteAddr:  remote,
			Result:      resultStr,
			Reason:      reason,
			Tool:        name,
		})

		return result, out, err
	}
}

