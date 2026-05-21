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
}

// AuditFn is the function signature for writing an audit record.
// Implemented by writeAudit; injectable for testing.
type AuditFn func(rec AuditRecord)

// writeAudit writes a structured audit record via charmbracelet/log.
// Format: time=<UTC> level=<LEVEL> msg=<LABEL> fp=<fingerprint> remote=<IP:port> [reason=<text>]
// Levels and msg labels per ux-spec §3:
//   - success → INFO / AUTH
//   - fail    → WARN / FAIL
//   - deny    → WARN / DENY
//   - rate-limited → WARN / RATE
//
// SR-21: this function MUST NOT log any key body, Authorization header value,
// hash, salt, or private TLS key material.
func writeAudit(logger *clog.Logger, rec AuditRecord) {
	fp := rec.Fingerprint
	if fp == "" {
		fp = "-"
	}

	switch rec.Result {
	case "success":
		logger.Info("AUTH",
			"fp", fp,
			"remote", rec.RemoteAddr,
		)
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
