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
	// Result is "success", "fail", "deny", "warn", or "rate-limited".
	// "warn" используется для предупреждений (напр. root-WARN SR-55), команда при этом может продолжиться.
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

	// --- upload-специфичные поля (SR-79/plan §Contracts) ---
	// Логируются ТОЛЬКО при Tool=="upload_file"; не-upload и не-exec записи не меняются.
	// SECURITY (SR-80): только относительный путь; содержимое файла НЕ логируется НИКОГДА.

	// Path — относительный путь назначения внутри upload root (SR-79/SR-80).
	// НЕ абсолютный путь. Пусто если путь неизвестен (ранний deny до парса пути).
	Path string

	// Size — число записанных байт (только в success-ветке; SR-78/SR-79).
	// В deny/fail — нерелевантен (запись не выполнена).
	Size int64
}

// AuditFn is the function signature for writing an audit record.
// Implemented by writeAudit; injectable for testing.
type AuditFn func(rec AuditRecord)

// writeAudit writes a structured audit record via charmbracelet/log.
// Format: time=<UTC> level=<LEVEL> msg=<LABEL> fp=<fingerprint> remote=<IP:port> [tool=<name>] [reason=<text>]
// Levels and msg labels per ux-spec §3:
//   - success (Tool=="") → INFO / AUTH
//   - success (Tool!="") → INFO / MCP  (SR-36: MCP-layer tool call success)
//   - warn    → WARN / WARN  (предупреждение; напр. root-WARN при euid==0, SR-55)
//   - fail    → WARN / FAIL
//   - deny    → WARN / DENY
//   - rate-limited → WARN / RATE
//
// SR-21: this function MUST NOT log any key body, Authorization header value,
// hash, salt, or private TLS key material.
// SR-36: tool= is logged ONLY when rec.Tool != "" — non-MCP records are unchanged.
// SR-55: Result:"warn" — отдельный уровень для root-WARN (семантически отличен от deny).
// SR-59/ADR-002: exec-поля (command/args/exit_code/duration/timed_out) логируются
// ТОЛЬКО при Tool=="execute_command" — не-exec записи не меняются.
// SR-79: upload-поля (path/size) логируются ТОЛЬКО при Tool=="upload_file" — через
// ветку isUpload в КАЖДОМ case. Не-upload и не-exec записи не меняются.
// SECURITY (SR-80): содержимое файла (content/decoded) НЕ логируется НИКОГДА.
func writeAudit(logger *clog.Logger, rec AuditRecord) {
	fp := rec.Fingerprint
	if fp == "" {
		fp = "-"
	}

	isExec := rec.Tool == "execute_command"
	isUpload := rec.Tool == "upload_file"

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
			} else if isUpload {
				// SR-79: upload-поля в success-ветке (tool/result/path/size).
				// SECURITY (SR-80): содержимое файла НЕ логируется.
				logger.Info("MCP",
					"fp", fp,
					"remote", rec.RemoteAddr,
					"tool", rec.Tool,
					"result", "ok",
					"path", rec.Path,
					"size", rec.Size,
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
	case "warn":
		// Result:"warn" — предупреждение (не отказ): команда/запись может продолжить.
		// Используется для root-WARN (SR-55/SR-77): raxd запущен с euid==0.
		// Семантически отличен от "deny": действие ещё не отклонено/не выполнено на этом уровне.
		if isExec {
			logger.Warn("WARN",
				"fp", fp,
				"remote", rec.RemoteAddr,
				"tool", rec.Tool,
				"reason", rec.Reason,
				"command", rec.Command,
				"args", formatArgs(rec.Args),
			)
		} else if isUpload {
			// SR-79: upload warn (root-предупреждение); path если известен.
			// ключа result= нет (mcp-spec §2.3.1).
			if rec.Path != "" {
				logger.Warn("WARN",
					"fp", fp,
					"remote", rec.RemoteAddr,
					"tool", rec.Tool,
					"reason", rec.Reason,
					"path", rec.Path,
				)
			} else {
				logger.Warn("WARN",
					"fp", fp,
					"remote", rec.RemoteAddr,
					"tool", rec.Tool,
					"reason", rec.Reason,
				)
			}
		} else {
			logger.Warn("WARN",
				"fp", fp,
				"remote", rec.RemoteAddr,
				"reason", rec.Reason,
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
		} else if isUpload {
			// SR-79: upload fail (I/O-ошибка); path если известен.
			// ключа result= нет.
			if rec.Path != "" {
				logger.Warn("FAIL",
					"fp", fp,
					"remote", rec.RemoteAddr,
					"tool", rec.Tool,
					"reason", rec.Reason,
					"path", rec.Path,
				)
			} else {
				logger.Warn("FAIL",
					"fp", fp,
					"remote", rec.RemoteAddr,
					"tool", rec.Tool,
					"reason", rec.Reason,
				)
			}
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
		} else if isUpload {
			// SR-79: upload deny (traversal/exists/isdir/too-large/bad-base64/bad-mode/deny_root).
			// ключа result= нет.
			if rec.Path != "" {
				logger.Warn("DENY",
					"fp", fp,
					"remote", rec.RemoteAddr,
					"tool", rec.Tool,
					"reason", rec.Reason,
					"path", rec.Path,
				)
			} else {
				logger.Warn("DENY",
					"fp", fp,
					"remote", rec.RemoteAddr,
					"tool", rec.Tool,
					"reason", rec.Reason,
				)
			}
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
