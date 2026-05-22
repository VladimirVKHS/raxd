// Package mcp implements the MCP (Model Context Protocol) server layer for raxd.
//
// It builds an MCP server using the official Go SDK
// (github.com/modelcontextprotocol/go-sdk/mcp) and mounts it as an http.Handler
// on /mcp inside the existing middleware chain of the tls-transport layer.
//
// Security contract (SR-27/SR-28):
//   - Authentication, Origin/Host validation, and rate-limiting are performed by
//     the transport middleware BEFORE requests reach this handler.
//   - This package MUST NOT import internal/keystore or call keystore.Verify.
//   - Only the fingerprint (from context via server.FingerprintFromContext) is used
//     in audit records — the key body is never accessible here.
//
// SR-39: All tests run in Docker from vendor/ (-mod=vendor).
package mcp

import (
	"net/http"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vladimirvkhs/raxd/internal/cmdexec"
	"github.com/vladimirvkhs/raxd/internal/server"
)

// NewHandler builds an MCP server with ping, server_info, and execute_command tools,
// and returns an http.Handler from NewStreamableHTTPHandler suitable for mounting at /mcp.
//
// Parameters:
//   - ver: raxd version string (from internal/version.Version) used in serverInfo.
//   - audit: the AuditFn from the transport layer (same channel as auth audit).
//   - execCfg: configuration for the execute_command tool (command-exec task).
//
// Returns an error only if the server cannot be constructed (fatal for serve).
// Does not panic.
//
// Contract (plan.md §Contracts):
//   mcp.NewHandler(ver string, audit server.AuditFn, execCfg cmdexec.Config) (http.Handler, error)
// SR-28: no second auth channel; SR-29: same port/TLS as transport (mounted by caller).
// ADR-004: execute_command is NOT wrapped with withAudit — execHandler owns its own audit.
func NewHandler(ver string, audit server.AuditFn, execCfg cmdexec.Config) (http.Handler, error) {
	// Build MCP server (AC3: serverInfo name=raxd, version=ver).
	s := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "raxd",
		Version: ver,
	}, nil)

	// Register tools:
	// - ping + server_info: wrapped with withAudit (SR-35/AC9).
	// - execute_command: NOT wrapped with withAudit (ADR-004/SR-57);
	//   execHandler writes its own exec-audit record in all branches.
	sdkmcp.AddTool(s, pingTool(), withAudit("ping", pingHandler, audit))
	sdkmcp.AddTool(s, serverInfoTool(ver), withAudit("server_info", serverInfoHandler(ver), audit))
	sdkmcp.AddTool(s, execTool(), execHandler(execCfg, audit))

	// Build StreamableHTTPHandler (AC1: Streamable HTTP; GET→405 per plan §Contracts).
	// Stateless=true: no MCP-Session-Id issued (v1 stateless; SR-28/mcp-spec §1.3).
	// JSONResponse=true: responses are application/json for request-response (not SSE).
	h := sdkmcp.NewStreamableHTTPHandler(func(_ *http.Request) *sdkmcp.Server {
		return s
	}, &sdkmcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})

	return h, nil
}
