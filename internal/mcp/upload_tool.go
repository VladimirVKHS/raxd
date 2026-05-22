package mcp

// upload_tool.go — MCP-инструмент upload_file.
//
// Реализует handler для безопасной записи файла на хост по MCP.
// ADR-004-стиль/SR-78: upload_file НЕ оборачивается generic withAudit;
// uploadHandler сам пишет РОВНО одну upload-аудит-запись во всех ветках.
//
// Поток (mcp-spec §2):
//   handler → root-детекция → входные проверки (размер/base64/mode) →
//   fileupload.Write → маппинг Result→UploadOutput → upload-аудит success.
//
// БЕЗОПАСНОСТЬ:
//   - SR-68: аутентификация выполнена транспортом ДО этого handler'а.
//   - SR-69 (ADR-001): traversal-safety через fileupload.Write/os.Root.
//   - SR-73 (ADR-003): режим через fileupload.ParseMode.
//   - SR-74 (ADR-002): атомарность в fileupload.Write.
//   - SR-75: ранний фильтр размера ДО декодирования + точная проверка.
//   - SR-77: root-WARN при каждом вызове при euid==0.
//   - SR-78: upload-аудит во всех ветках; generic withAudit НЕ применяется.
//   - SR-80: fingerprint из ctx (не тело ключа); содержимое НЕ логируется.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vladimirvkhs/raxd/internal/fileupload"
	"github.com/vladimirvkhs/raxd/internal/server"
)

// UploadInput — входные данные инструмента upload_file.
// AC2: path и content обязательны; overwrite и mode опциональны.
// additionalProperties:false гарантируется инференцией SDK из struct.
// SECURITY (SR-80): поля абсолютного пути и владельца НЕТ (spec Out of Scope/AC2).
type UploadInput struct {
	// Path — относительный путь назначения внутри upload root (обязателен).
	// Абсолютный / ..escape → ErrTraversal deny (SR-69/AC4).
	Path string `json:"path"`
	// Content — содержимое файла в base64 (обязателен).
	// Невалидный base64 → deny (SR-75/AC6).
	// Декодированный размер > max_file_bytes → deny (SR-75/AC7).
	Content string `json:"content"`
	// Overwrite — разрешить замену существующего файла (опц., дефолт false).
	// false + существующий файл → ErrExists deny (SR-72/AC8).
	Overwrite bool `json:"overwrite,omitempty"`
	// Mode — восьмеричная строка прав файла (опц.).
	// Пусто → cfg.DefaultMode. Непарсимый/setuid·setgid·sticky/world-writable → ErrBadMode deny (SR-73/AC9).
	Mode string `json:"mode,omitempty"`
}

// UploadOutput — структурированный результат upload_file (AC3/SR-80).
// Четыре поля: без абсолютного пути, без содержимого файла.
// SECURITY (SR-80): абсолютный путь НЕ включается; содержимое НЕ включается.
type UploadOutput struct {
	// Path — записанный относительный путь (как принят сервером).
	Path string `json:"path"`
	// Size — число записанных байт декодированного содержимого.
	Size int64 `json:"size"`
	// Overwritten — true если существовавший файл был заменён.
	Overwritten bool `json:"overwritten"`
	// Mode — фактический режим созданного файла восьмеричной строкой.
	Mode string `json:"mode"`
}

// uploadTool возвращает дескриптор MCP-инструмента upload_file.
// Описание — для ИИ-агента (mcp-spec §3).
func uploadTool() *sdkmcp.Tool {
	return &sdkmcp.Tool{
		Name: "upload_file",
		Description: `Записать ОДИН обычный файл на хост raxd в безопасный каталог загрузок (upload root). ` +
			`Путь задаётся относительным к upload root; запись возможна ТОЛЬКО внутрь корня — ` +
			`попытка выйти наружу через '..', абсолютный путь или симлинк наружу отклоняется. ` +
			`Содержимое передаётся в кодировке base64. Размер декодированного содержимого ограничен серверным лимитом. ` +
			`По умолчанию существующий файл НЕ перезаписывается (нужен overwrite:true). ` +
			`Права создаваемого файла контролируются (по умолчанию 0600; биты setuid/setgid/sticky и world-writable запрещены). ` +
			`Инструмент создаёт только обычный файл, не повышает привилегии и не меняет владельца. ` +
			`Возвращает записанный относительный путь, размер, флаг перезаписи и итоговый режим. ` +
			`Каждый вызов проходит аутентификацию и аудит; содержимое файла в аудит не пишется.`,
	}
}

// uploadHandler возвращает ToolHandlerFor[UploadInput, UploadOutput] для upload_file.
//
// ADR-004-стиль/SR-78: НЕ оборачивается withAudit. Пишет upload-аудит самостоятельно во всех ветках.
// Ветки:
//   - success        → AuditRecord{Result:"success", Tool:"upload_file", Path, Size, fp, remote}
//   - warn (root)    → AuditRecord{Result:"warn", reason="running-as-root", Tool:"upload_file"}
//   - deny (traversal/exists/isdir/too-large/bad-base64/bad-mode/deny_root)
//     → AuditRecord{Result:"deny", Tool:"upload_file", Path(если известен), Reason}
//   - fail (I/O)     → AuditRecord{Result:"fail", Tool:"upload_file", Path, Reason}
//
// root-WARN (SR-77) — отдельная WARN-запись при euid==0, помимо основной.
// SECURITY (SR-80): содержимое (Content/decoded bytes) НЕ передаётся в AuditRecord НИКОГДА.
func uploadHandler(cfg fileupload.Config, audit server.AuditFn) sdkmcp.ToolHandlerFor[UploadInput, UploadOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input UploadInput) (*sdkmcp.CallToolResult, UploadOutput, error) {
		// Извлекаем fingerprint и remote из ctx (установлены authMiddleware; SR-80).
		fp := server.FingerprintFromContext(ctx)
		remote := server.RemoteAddrFromContext(ctx)

		// --- Root-детекция (SR-77/AC11) ---
		// При euid==0 ВСЕГДА эмитируется отдельная WARN-запись (Result:"warn", reason="running-as-root").
		// Случай (а): deny_root=false → WARN, запись продолжается дальше.
		// Случай (б): deny_root=true  → WARN + реальный deny (Result:"deny"), запись НЕ выполняется.
		if os.Geteuid() == 0 {
			// Отдельная WARN-запись (Result:"warn"; mcp-spec §2.3.2 / SR-77).
			// path ещё не валидирован на этом этапе — передаём пустым.
			audit(server.AuditRecord{
				TS:          time.Now().UTC(),
				Fingerprint: fp,
				RemoteAddr:  remote,
				Result:      "warn",
				Tool:        "upload_file",
				Reason:      "running-as-root: raxd writing files as root (euid==0); ensure raxd runs as non-root",
			})
			// SR-77: deny_root=true → реальный deny ПОСЛЕ root-WARN.
			if cfg.DenyRoot {
				denyReason := "upload as root is forbidden by policy (deny_root=true)"
				audit(server.AuditRecord{
					TS:          time.Now().UTC(),
					Fingerprint: fp,
					RemoteAddr:  remote,
					Result:      "deny",
					Tool:        "upload_file",
					Path:        input.Path,
					Reason:      denyReason,
				})
				return nil, UploadOutput{}, fmt.Errorf("upload as root is forbidden by policy")
			}
		}

		// --- Входные проверки ДО записи (SR-75/SR-73/SR-76) ---

		// (1) Ранний фильтр размера по DecodedLen ДО декодирования (защита памяти; SR-75/R-U5).
		if base64.StdEncoding.DecodedLen(len(input.Content)) > int(cfg.MaxFileBytes) {
			reason := "file too large: exceeds max_file_bytes"
			audit(server.AuditRecord{
				TS:          time.Now().UTC(),
				Fingerprint: fp,
				RemoteAddr:  remote,
				Result:      "deny",
				Tool:        "upload_file",
				Path:        input.Path,
				Reason:      reason,
			})
			return nil, UploadOutput{}, fmt.Errorf("%s", reason)
		}

		// (2) Декодирование base64 (SR-75/AC6).
		decoded, err := base64.StdEncoding.DecodeString(input.Content)
		if err != nil {
			reason := "invalid base64 content"
			audit(server.AuditRecord{
				TS:          time.Now().UTC(),
				Fingerprint: fp,
				RemoteAddr:  remote,
				Result:      "deny",
				Tool:        "upload_file",
				Path:        input.Path,
				Reason:      reason,
			})
			return nil, UploadOutput{}, fmt.Errorf("%s", reason)
		}

		// (3) Точная проверка размера после декодирования (SR-75/AC7).
		if int64(len(decoded)) > cfg.MaxFileBytes {
			reason := "file too large: exceeds max_file_bytes"
			audit(server.AuditRecord{
				TS:          time.Now().UTC(),
				Fingerprint: fp,
				RemoteAddr:  remote,
				Result:      "deny",
				Tool:        "upload_file",
				Path:        input.Path,
				Reason:      reason,
			})
			return nil, UploadOutput{}, fmt.Errorf("%s", reason)
		}

		// (4) Разрешение режима файла (SR-73/ADR-003/AC9).
		// Пусто → cfg.DefaultMode (подставляется handler'ом ДО вызова ParseMode).
		mode := cfg.DefaultMode
		if input.Mode != "" {
			parsedMode, modeErr := fileupload.ParseMode(input.Mode)
			if modeErr != nil {
				reason := "invalid file mode"
				audit(server.AuditRecord{
					TS:          time.Now().UTC(),
					Fingerprint: fp,
					RemoteAddr:  remote,
					Result:      "deny",
					Tool:        "upload_file",
					Path:        input.Path,
					Reason:      reason,
				})
				return nil, UploadOutput{}, fmt.Errorf("%s", reason)
			}
			mode = parsedMode
		}

		// --- Запись через fileupload.Write (SR-69/ADR-001/ADR-002) ---
		writeIn := fileupload.Input{
			RelPath:   input.Path,
			Data:      decoded,
			Overwrite: input.Overwrite,
			Mode:      mode,
		}

		res, writeErr := fileupload.Write(cfg, writeIn)
		if writeErr != nil {
			// Определяем deny vs fail по типу ошибки.
			auditResult := "fail"
			reason := "write failed"

			switch {
			case errors.Is(writeErr, fileupload.ErrTraversal):
				auditResult = "deny"
				reason = "traversal"
			case errors.Is(writeErr, fileupload.ErrExists):
				auditResult = "deny"
				reason = "file already exists"
			case errors.Is(writeErr, fileupload.ErrIsDir):
				auditResult = "deny"
				reason = "target is a directory"
			case errors.Is(writeErr, fileupload.ErrTooLarge):
				auditResult = "deny"
				reason = "file too large"
			case errors.Is(writeErr, fileupload.ErrBadMode):
				auditResult = "deny"
				reason = "invalid file mode"
			}

			audit(server.AuditRecord{
				TS:          time.Now().UTC(),
				Fingerprint: fp,
				RemoteAddr:  remote,
				Result:      auditResult,
				Tool:        "upload_file",
				Path:        input.Path,
				Reason:      reason,
			})

			// Нейтральные сообщения (SR-80): без абсолютных путей.
			switch auditResult {
			case "deny":
				return nil, UploadOutput{}, fmt.Errorf("%s", writeErr.Error())
			default:
				return nil, UploadOutput{}, fmt.Errorf("write failed")
			}
		}

		// --- Успешная запись: маппинг Result → UploadOutput (AC3/SR-80) ---
		out := UploadOutput{
			Path:        res.RelPath,
			Size:        res.Size,
			Overwritten: res.Overwritten,
			Mode:        fmt.Sprintf("%04o", res.Mode),
		}

		// text-резюме для модели (mcp-spec §5.2; суффикс B ТОЛЬКО здесь, не в structuredContent).
		textSummary := fmt.Sprintf("path=%s size=%dB overwritten=%v mode=%s",
			res.RelPath, res.Size, res.Overwritten, out.Mode)

		// --- Upload-аудит: success (SR-78/SR-79/AC12) ---
		// SECURITY (SR-80): Path — только относительный; содержимое НЕ логируется.
		audit(server.AuditRecord{
			TS:          time.Now().UTC(),
			Fingerprint: fp,
			RemoteAddr:  remote,
			Result:      "success",
			Tool:        "upload_file",
			Path:        res.RelPath,
			Size:        res.Size,
		})

		toolResult := &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: textSummary},
			},
		}
		return toolResult, out, nil
	}
}
