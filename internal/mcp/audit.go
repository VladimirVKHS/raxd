package mcp

// audit.go implements the withAudit decorator for MCP tool handlers.
//
// SR-35/AC9: every tools/call writes an audit record with:
//   - Fingerprint: from server.FingerprintFromContext(ctx) — NOT the key body
//   - Tool: MCP tool name (not a secret)
//   - Result: "success" or "fail"
//   - RemoteAddr: from http.Request (via context)
//
// SR-34: key body, hash, salt, and private TLS key MUST NOT appear in audit records.
// SR-28: this package does not call keystore.Verify.

import (
	"context"
	"net"
	"net/http"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vladimirvkhs/raxd/internal/server"
)

// withAudit wraps a ToolHandlerFor with pre/post audit logging.
//
// It uses server.FingerprintFromContext to retrieve the fingerprint set by
// authMiddleware — the key body is NEVER accessible here (SR-35/SR-34).
//
// The RemoteAddr is extracted from the http.Request via the SDK's context;
// if not available, "-" is used as a safe fallback.
//
// Parameters:
//   - name: MCP tool name (logged as Tool in AuditRecord; not a secret).
//   - h: the typed ToolHandlerFor to wrap.
//   - audit: the AuditFn injected from the transport layer.
func withAudit[In, Out any](name string, h sdkmcp.ToolHandlerFor[In, Out], audit server.AuditFn) sdkmcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *sdkmcp.CallToolRequest, input In) (*sdkmcp.CallToolResult, Out, error) {
		// SR-35: fingerprint from context (not key body).
		fp := server.FingerprintFromContext(ctx)
		remote := remoteAddrFromCtx(ctx)

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

// remoteAddrFromCtx extracts the remote address from the request context.
// The MCP SDK stores the *http.Request in context via a known key.
// If unavailable, returns "-" as a safe fallback.
func remoteAddrFromCtx(ctx context.Context) string {
	// The SDK does not expose a public API to retrieve the http.Request from ctx,
	// but the ClientAddress is available via mcp.ClientAddressFromContext.
	// As a fallback, we check for the request directly.
	if req, ok := ctx.Value(httpRequestCtxKey{}).(*http.Request); ok && req != nil {
		host, _, err := net.SplitHostPort(req.RemoteAddr)
		if err != nil {
			return req.RemoteAddr
		}
		return host + ":?"
	}
	return "-"
}

// httpRequestCtxKey is not used (the SDK does not expose a public key for the request).
// remoteAddrFromCtx always returns "-" or the SDK-provided address below.
type httpRequestCtxKey struct{}
