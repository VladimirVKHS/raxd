package mcp

// exec_tool.go — MCP-инструмент execute_command.
//
// Реализует handler для безопасного выполнения команд на хосте по MCP.
// ADR-004/SR-57: execute_command НЕ оборачивается generic withAudit;
// execHandler сам пишет РОВНО одну exec-аудит-запись во всех ветках.
//
// Поток (mcp-spec §2):
//   handler → root-детекция → входные лимиты → резолв cwd/timeout →
//   cmdexec.Run → маппинг Result→ExecOutput → exec-аудит.
//
// БЕЗОПАСНОСТЬ:
//   - SR-41: аутентификация выполнена транспортом ДО этого handler'а.
//   - SR-62: fingerprint из ctx (не тело ключа); сообщения нейтральны.
//   - SR-54: Credential не задаётся в cmdexec (наследование uid/gid).
//   - SR-55: root-WARN при каждом вызове при euid==0.
//   - SR-56: deny_root=true → отказ при euid==0.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vladimirvkhs/raxd/internal/cmdexec"
	"github.com/vladimirvkhs/raxd/internal/server"
)

// ExecInput — входные данные инструмента execute_command.
// AC3: command обязателен; args/timeout_ms/cwd опциональны; env НЕТ (Out of Scope).
// additionalProperties:false гарантируется инференцией SDK из struct.
type ExecInput struct {
	// Command — имя/путь бинаря (обязателен).
	Command string `json:"command"`
	// Args — аргументы (опц., литеральные; SR-43).
	Args []string `json:"args,omitempty"`
	// TimeoutMs — таймаут в мс (опц.; 0 → DefaultTimeoutMs; AC5).
	TimeoutMs int `json:"timeout_ms,omitempty"`
	// Cwd — рабочая директория (опц.; пусто → DefaultCwd; AC10).
	Cwd string `json:"cwd,omitempty"`
}

// ExecOutput — структурированный результат (AC4/SR-65).
// Семь полей: stdout/stderr/exit_code/duration_ms/timed_out/stdout_truncated/stderr_truncated.
type ExecOutput struct {
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	DurationMs      int    `json:"duration_ms"`
	TimedOut        bool   `json:"timed_out"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
}

// execTool возвращает дескриптор MCP-инструмента execute_command.
// Описание — для ИИ-агента (mcp-spec §3).
func execTool() *sdkmcp.Tool {
	return &sdkmcp.Tool{
		Name: "execute_command",
		Description: `Выполнить НЕинтерактивную команду на хосте raxd формой «бинарь + список аргументов» ` +
			`(БЕЗ shell: метасимволы ;|$()&&>` + "`" + ` трактуются как литеральные аргументы). ` +
			`Возвращает stdout/stderr/код возврата/длительность/флаги усечения и таймаута. ` +
			`Ограничения: обязательный таймаут (по умолчанию из конфига, есть жёсткий максимум); ` +
			`вывод и аргументы лимитированы; рабочая директория и окружение контролируются сервером; ` +
			`при включённом allowlist разрешены только перечисленные команды. ` +
			`НЕ принимает переменные окружения от клиента. Каждый вызов проходит аутентификацию и аудит.`,
	}
}

// execHandler возвращает ToolHandlerFor[ExecInput, ExecOutput] для execute_command.
//
// ADR-004/SR-57: НЕ оборачивается withAudit. Пишет exec-аудит самостоятельно во всех ветках.
// Ветки:
//   - success/таймаут → AuditRecord{Result:"success", ...exec-поля...}
//   - deny (allowlist/лимиты входа/deny_root) → AuditRecord{Result:"deny", ...}
//   - fail (несуществующий бинарь/bad cwd) → AuditRecord{Result:"fail", ...}
//
// root-WARN (SR-55) — отдельная WARN-запись при euid==0, помимо основной.
func execHandler(cfg cmdexec.Config, audit server.AuditFn) sdkmcp.ToolHandlerFor[ExecInput, ExecOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input ExecInput) (*sdkmcp.CallToolResult, ExecOutput, error) {
		// Извлекаем fingerprint и remote из ctx (установлены authMiddleware; SR-62).
		fp := server.FingerprintFromContext(ctx)
		remote := server.RemoteAddrFromContext(ctx)

		// --- Root-детекция (SR-55/AC9) ---
		// При euid==0 — отдельная WARN-аудит-запись при КАЖДОМ вызове (обязательна всегда).
		// Рендерится writeAudit как WARN (через Result:"deny" с root-reason — это отдельная запись).
		if os.Geteuid() == 0 {
			// Отдельная WARN-запись (помимо основной exec-записи; mcp-spec §2.3.2).
			audit(server.AuditRecord{
				TS:          time.Now().UTC(),
				Fingerprint: fp,
				RemoteAddr:  remote,
				Result:      "deny",
				Tool:        "execute_command",
				Reason:      "root-warn: executing commands as root (euid==0); ensure raxd runs as non-root",
				Command:     input.Command,
				Args:        input.Args,
			})
			// SR-56: deny_root=true → hard-fail ПОСЛЕ root-WARN.
			if cfg.DenyRoot {
				return nil, ExecOutput{}, fmt.Errorf("execution as root is forbidden by policy")
			}
		}

		// --- Входные лимиты ДО запуска (SR-52/ADR-003) ---
		if len(input.Args) > cfg.MaxArgs {
			reason := fmt.Sprintf("too many arguments: %d > %d", len(input.Args), cfg.MaxArgs)
			audit(server.AuditRecord{
				TS:          time.Now().UTC(),
				Fingerprint: fp,
				RemoteAddr:  remote,
				Result:      "deny",
				Tool:        "execute_command",
				Reason:      reason,
				Command:     input.Command,
				Args:        input.Args,
			})
			return nil, ExecOutput{}, fmt.Errorf("%s", reason)
		}
		for _, arg := range input.Args {
			if len(arg) > cfg.MaxArgLen {
				reason := fmt.Sprintf("argument too long: %d > %d", len(arg), cfg.MaxArgLen)
				audit(server.AuditRecord{
					TS:          time.Now().UTC(),
					Fingerprint: fp,
					RemoteAddr:  remote,
					Result:      "deny",
					Tool:        "execute_command",
					Reason:      reason,
					Command:     input.Command,
					Args:        input.Args,
				})
				return nil, ExecOutput{}, fmt.Errorf("%s", reason)
			}
		}

		// --- Проверка таймаута (SR-46/AC5) ---
		effTimeoutMs := input.TimeoutMs
		if effTimeoutMs == 0 {
			effTimeoutMs = cfg.DefaultTimeoutMs
		}
		if effTimeoutMs > cfg.MaxTimeoutMs {
			reason := fmt.Sprintf("timeout_ms %d exceeds max %d", effTimeoutMs, cfg.MaxTimeoutMs)
			audit(server.AuditRecord{
				TS:          time.Now().UTC(),
				Fingerprint: fp,
				RemoteAddr:  remote,
				Result:      "deny",
				Tool:        "execute_command",
				Reason:      reason,
				Command:     input.Command,
				Args:        input.Args,
			})
			return nil, ExecOutput{}, fmt.Errorf("%s", reason)
		}

		// --- Резолв cwd (SR-50/AC10) ---
		cwd := input.Cwd
		if cwd == "" {
			cwd = cfg.DefaultCwd
		}

		// --- Запуск через cmdexec.Run с таймаутом (SR-46/AC5) ---
		timeout := time.Duration(effTimeoutMs) * time.Millisecond
		runCtx, runCancel := context.WithTimeout(ctx, timeout)
		defer runCancel()

		runIn := cmdexec.Input{
			Command: input.Command,
			Args:    input.Args,
			Cwd:     cwd,
		}

		res, runErr := cmdexec.Run(runCtx, cfg, runIn)

		if runErr != nil {
			// Определяем ветку: deny vs fail.
			auditResult := "fail"
			reason := "command execution failed"
			if errors.Is(runErr, cmdexec.ErrNotAllowed) {
				auditResult = "deny"
				reason = "not-allowed"
			} else if errors.Is(runErr, cmdexec.ErrBadCwd) {
				reason = "bad-cwd"
			} else {
				reason = "not-found"
			}

			audit(server.AuditRecord{
				TS:          time.Now().UTC(),
				Fingerprint: fp,
				RemoteAddr:  remote,
				Result:      auditResult,
				Tool:        "execute_command",
				Reason:      reason,
				Command:     input.Command,
				Args:        input.Args,
			})

			if auditResult == "deny" {
				return nil, ExecOutput{}, fmt.Errorf("command not allowed")
			}
			return nil, ExecOutput{}, fmt.Errorf("command not found")
		}

		// --- Маппинг Result → ExecOutput (AC4/SR-65) ---
		out := ExecOutput{
			Stdout:          string(res.Stdout),
			Stderr:          string(res.Stderr),
			ExitCode:        res.ExitCode,
			DurationMs:      int(res.Duration.Milliseconds()),
			TimedOut:        res.TimedOut,
			StdoutTruncated: res.StdoutTruncated,
			StderrTruncated: res.StderrTruncated,
		}

		// --- text-резюме для модели (mcp-spec §5.2/Q-EXEC-2) ---
		stdoutSize := len(res.Stdout)
		stderrSize := len(res.Stderr)
		textSummary := fmt.Sprintf("exit=%d duration=%dms timed_out=%v stdout=%dB stderr=%dB",
			res.ExitCode, int(res.Duration.Milliseconds()), res.TimedOut, stdoutSize, stderrSize)
		if res.StdoutTruncated || res.StderrTruncated {
			textSummary += " truncated"
		}

		// --- Exec-аудит: success/таймаут (SR-57/SR-58/ADR-004) ---
		exitCode := res.ExitCode
		audit(server.AuditRecord{
			TS:          time.Now().UTC(),
			Fingerprint: fp,
			RemoteAddr:  remote,
			Result:      "success",
			Tool:        "execute_command",
			Command:     input.Command,
			Args:        input.Args,
			ExitCode:    &exitCode,
			Duration:    res.Duration,
			TimedOut:    res.TimedOut,
		})

		// Возвращаем toolResult (isError=false для success/таймаут/ненулевой exit).
		toolResult := &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: textSummary},
			},
		}
		return toolResult, out, nil
	}
}
