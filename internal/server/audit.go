package server

import (
	"time"

	clog "github.com/charmbracelet/log"
)

// AuditRecord holds structured data for a single connection audit event.
// Fields mirror the ux-spec key=value format.
// SECURITY (SR-21): NO key body, raw Authorization header, hash, salt, or
// private TLS key material must appear in any field of this struct.
type AuditRecord struct {
	// TS is the UTC timestamp of the event.
	TS time.Time
	// Fingerprint is keystore.Fingerprint(key) — 12 hex chars.
	// Set to "-" when no key was presented.
	Fingerprint string
	// RemoteAddr is the client IP:port without DNS resolution.
	RemoteAddr string
	// Result is "success", "fail", "deny", or "rate-limited".
	Result string
	// Reason describes the failure; empty on success.
	Reason string
	// Tool is the MCP tool name for MCP-layer audit records.
	// Empty for non-MCP connection records (auth/deny/rate).
	// SECURITY (SR-36): Tool holds the tool name (not a secret); key body MUST NOT be stored.
	Tool string
}

// AuditFn is the function signature for writing an audit record.
// Implemented by writeAudit; injectable for testing.
type AuditFn func(rec AuditRecord)

// writeAudit writes a structured audit record via charmbracelet/log.
// Format: time=<UTC> level=<LEVEL> msg=<LABEL> fp=<fingerprint> remote=<IP:port> [tool=<name>] [reason=<text>]
// Levels and msg labels per ux-spec §3:
//   - success (Tool=="") → INFO / AUTH
//   - success (Tool!="") → INFO / MCP  (SR-36: MCP-layer tool call success)
//   - fail    → WARN / FAIL
//   - deny    → WARN / DENY
//   - rate-limited → WARN / RATE
//
// SR-21: this function MUST NOT log any key body, Authorization header value,
// hash, salt, or private TLS key material.
// SR-36: tool= is logged ONLY when rec.Tool != "" — non-MCP records are unchanged.
func writeAudit(logger *clog.Logger, rec AuditRecord) {
	fp := rec.Fingerprint
	if fp == "" {
		fp = "-"
	}

	switch rec.Result {
	case "success":
		if rec.Tool != "" {
			// SR-36: MCP tool call success — emit "MCP" label with tool= field.
			// result=ok per mcp-spec §2.2 format.
			logger.Info("MCP",
				"fp", fp,
				"remote", rec.RemoteAddr,
				"tool", rec.Tool,
				"result", "ok",
			)
		} else {
			// Non-MCP connection success — preserve existing format.
			logger.Info("AUTH",
				"fp", fp,
				"remote", rec.RemoteAddr,
			)
		}
	case "fail":
		logger.Warn("FAIL",
			"fp", fp,
			"remote", rec.RemoteAddr,
			"reason", rec.Reason,
		)
	case "deny":
		logger.Warn("DENY",
			"fp", fp,
			"remote", rec.RemoteAddr,
			"reason", rec.Reason,
		)
	case "rate-limited":
		logger.Warn("RATE",
			"fp", fp,
			"remote", rec.RemoteAddr,
			"reason", rec.Reason,
		)
	default:
		logger.Warn("DENY",
			"fp", fp,
			"remote", rec.RemoteAddr,
			"reason", rec.Reason,
		)
	}
}

// NewAuditFn creates an AuditFn backed by the given logger.
// Used by both production code (serve.go) and tests.
// SR-21: the returned function MUST NOT be called with key body, Authorization header,
// hash, salt, or private TLS key material in any AuditRecord field.
func NewAuditFn(logger *clog.Logger) AuditFn {
	return func(rec AuditRecord) {
		writeAudit(logger, rec)
	}
}

// NewAuditFnForTest is an alias for NewAuditFn for use in tests.
// Kept for backward compatibility with test files.
// NOT semantically different from NewAuditFn.
func NewAuditFnForTest(logger *clog.Logger) AuditFn {
	return NewAuditFn(logger)
}
