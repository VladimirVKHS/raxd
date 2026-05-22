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

	// --- exec-специфичные поля (SR-59/ADR-002) ---
	// Логируются ТОЛЬКО при Tool=="execute_command"; не-exec записи не меняются.

	// Command — имя команды (exec-аудит; SR-58/SR-63: логируется дословно, без маскирования).
	// SECURITY (SR-62): не содержит тело ключа/TLS — exec-слой к ним доступа не имеет.
	Command string

	// Args — аргументы команды (дословно; SR-63/П-3).
	Args []string

	// ExitCode — код возврата процесса (* — присутствует только при success/таймауте).
	ExitCode *int

	// Duration — длительность исполнения (только при success/таймауте).
	Duration time.Duration

	// TimedOut — true если команда была прервана по таймауту.
	TimedOut bool
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
// SR-59/ADR-002: exec-поля (command/args/exit_code/duration/timed_out) логируются
// ТОЛЬКО при Tool=="execute_command" — не-exec записи не меняются.
func writeAudit(logger *clog.Logger, rec AuditRecord) {
	fp := rec.Fingerprint
	if fp == "" {
		fp = "-"
	}

	isExec := rec.Tool == "execute_command"

	switch rec.Result {
	case "success":
		if rec.Tool != "" {
			// SR-36: MCP tool call success — emit "MCP" label with tool= field.
			// result=ok per mcp-spec §2.3.1 format.
			if isExec {
				// SR-59: exec-поля в success-ветке (command/args/exit_code/duration/timed_out).
				args := formatArgs(rec.Args)
				exitCode := 0
				if rec.ExitCode != nil {
					exitCode = *rec.ExitCode
				}
				logger.Info("MCP",
					"fp", fp,
					"remote", rec.RemoteAddr,
					"tool", rec.Tool,
					"result", "ok",
					"command", rec.Command,
					"args", args,
					"exit_code", exitCode,
					"duration", rec.Duration,
					"timed_out", rec.TimedOut,
				)
			} else {
				logger.Info("MCP",
					"fp", fp,
					"remote", rec.RemoteAddr,
					"tool", rec.Tool,
					"result", "ok",
				)
			}
		} else {
			// Non-MCP connection success — preserve existing format.
			logger.Info("AUTH",
				"fp", fp,
				"remote", rec.RemoteAddr,
			)
		}
	case "fail":
		if isExec {
			// SR-59: exec-поля в fail-ветке (command/args — exit_code/duration нет, не запускалось).
			logger.Warn("FAIL",
				"fp", fp,
				"remote", rec.RemoteAddr,
				"tool", rec.Tool,
				"reason", rec.Reason,
				"command", rec.Command,
				"args", formatArgs(rec.Args),
			)
		} else {
			logger.Warn("FAIL",
				"fp", fp,
				"remote", rec.RemoteAddr,
				"reason", rec.Reason,
			)
		}
	case "deny":
		if isExec {
			// SR-59: exec-поля в deny-ветке (command/args — exit_code/duration нет, не запускалось).
			logger.Warn("DENY",
				"fp", fp,
				"remote", rec.RemoteAddr,
				"tool", rec.Tool,
				"reason", rec.Reason,
				"command", rec.Command,
				"args", formatArgs(rec.Args),
			)
		} else {
			logger.Warn("DENY",
				"fp", fp,
				"remote", rec.RemoteAddr,
				"reason", rec.Reason,
			)
		}
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

// formatArgs форматирует срез аргументов для логирования в стиле [a,b,c].
// SR-63/П-3: аргументы логируются дословно без маскирования.
func formatArgs(args []string) string {
	if len(args) == 0 {
		return "[]"
	}
	return "[" + joinStrings(args, ",") + "]"
}

// joinStrings объединяет строки через разделитель (без импорта strings для простоты).
func joinStrings(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
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
