# Impl Notes: file-upload

## Что реализовано

- **`internal/fileupload/config.go`** — структура `Config{UploadRoot, MaxFileBytes, DefaultMode, DenyRoot}`, передаётся в `Write` и `uploadHandler`.

- **`internal/fileupload/mode.go`** — `ParseMode(s string) (fs.FileMode, error)`: разбирает восьмеричную строку, маскирует `0777`, запрещает special bits (`0o7000`) и world-writable (`0o002`); возвращает `ErrBadMode`. Реализует контракт ADR-003.

- **`internal/fileupload/upload.go`** — `Write(cfg Config, in Input) (Result, error)`: traversal-safe запись через `os.Root` (ADR-001); атомарная схема temp(crypto/rand+O_EXCL) → chmod-by-fd → Write → Sync → Root.Rename → fsync-dir (ADR-002); ранняя size-проверка до декодирования; `filepath.IsLocal` до открытия Root; defer-cleanup temp на любой ошибке. Ошибки: `ErrTraversal`, `ErrExists`, `ErrIsDir`, `ErrTooLarge`.

- **`internal/config/config.go`** — добавлена `UploadConfig{Root, MaxFileBytes, DefaultMode, OverwriteDefault, DenyRoot}` с viper-defaults (`upload.max_file_bytes=716800`, `upload.default_mode="0600"`, `upload.deny_root=false`); валидация: `max_file_bytes` > 0 и ≤ `floor((maxBodyBytes-1024)*3/4)`; проверка `DefaultMode` через `parseModeStr` (inline, без import fileupload — исключает циклическую зависимость).

- **`internal/server/audit.go`** — `AuditRecord` расширен полями `Path string` и `Size int64`; в `writeAudit` добавлена переменная `isUpload := rec.Tool == "upload_file"` и отдельный `else if isUpload` блок в **каждом** case (`success/warn/fail/deny`) для логирования `tool=upload_file` + `path=`/`size=` (success) или `reason=`/`path=` (warn/deny/fail). Общий `else` не используется для upload (SR-79 F-2).

- **`internal/mcp/upload_tool.go`** — `UploadInput{Path, Content, Overwrite, Mode}`, `UploadOutput{Path, Size, Overwritten, Mode}`; `uploadHandler(cfg, auditFn)` реализует: обнаружение root (SR-77, DenyRoot), ранний size-filter через `base64.StdEncoding.DecodedLen` (SR-75), `DecodeString` → детект `CorruptInputError`, точная проверка `len(decoded) > MaxFileBytes`, `ParseMode`, вызов `fileupload.Write`, маппинг ошибок на deny/fail; ровно **одна** audit-запись в каждой ветке (SR-78); `fingerprint = sha256(decoded)[:8]hex` без content/abs-path в ответе/логе (SR-80).

- **`internal/mcp/server.go`** — `NewHandler` расширен параметром `uplCfg fileupload.Config`; `sdkmcp.AddTool(s, uploadTool(), uploadHandler(uplCfg, audit))` регистрирует инструмент.

- **`internal/cli/serve.go`** — резолв `uploadRoot` (конфиг или `state-dir/uploads`), `os.MkdirAll(uploadRoot, 0o700)`, сборка `fileupload.Config`, передача в `NewHandler`.

## Отклонения/эскалации

- **`Root.CreateTemp` отсутствует в Go 1.25** — подтверждено в research. Temp-имя генерируется через `crypto/rand` + hex-кодирование, файл открывается через `root.OpenFile(tmpRel, O_CREATE|O_EXCL|O_WRONLY, 0o600)`. Это соответствует ADR-002 (temp генерируем сами).
- **Circular dependency `config` → `fileupload`** — для проверки `DefaultMode` в `config.buildConfig` логика `ParseMode` продублирована как `parseModeStr` (10 строк, без import fileupload). Отклонения от plan нет: plan не требовал повторного использования функции.
- Прочих отклонений нет. Все модули и сигнатуры строго по `plan.md`.

## Тесты

**Пакет `internal/fileupload` (21 тест):**
- `TestParseMode_ValidModes`, `TestParseMode_SetuidBit/SetgidBit/StickyBit`, `TestParseMode_WorldWritable`, `TestParseMode_InvalidString` — ADR-003;
- `TestWriteSuccess_BasicFile/BinaryContent/Subdirectory/NestedSubdirectories` — AC3, AC5;
- `TestWriteTraversal_DotDotEscape/AbsolutePath/MultipleEscape/Symlink` — AC4/SR-69;
- `TestWriteTooLarge`, `TestWriteOverwrite_Denied/Allowed`, `TestWriteTargetIsDirectory` — AC7, AC8, AC14;
- `TestWriteMode_Default0600/Custom0700`, `TestAtomicity_NoTempOnError` — AC9, ADR-002.

**Пакет `internal/mcp` (52 теста, из них upload-специфичные — 21):**
- `TestUploadFileInToolsList` — AC1;
- `TestUploadFile_ExtraFieldDenied` — AC2;
- `TestUploadFile_OutputFormat` — AC3;
- `TestUploadFile_TraversalDotDot/Absolute/Multiple` — AC4;
- `TestUploadFile_Subdirectory` — AC5b;
- `TestUploadFile_InvalidBase64/BinaryContent` — AC6;
- `TestUploadFile_TooLarge` — AC7;
- `TestUploadFile_OverwriteFalse/OverwriteTrue` — AC8;
- `TestUploadFile_ModeDefault/SetuidDenied/WorldWritableDenied` — AC9;
- `TestUploadFile_NoTempLeft` — AC10;
- `TestUploadFile_AuditSuccess/AuditDeny` — AC12/AC19;
- `TestUploadFile_NoSecretsInAuditOrResponse` — AC13/SR-80;
- `TestUploadFile_TargetIsDirectory/NoRequiredFields` — AC14;
- `TestUploadFile_ExactlyOneAuditRecord` — SR-78;
- `TestUploadFile_PathLogfmtInjection` (3 вектора: space+eq, quote, newline) — SR-79 (F-1).

**Пакет `internal/config` (дополнительно после F-3):**
- `TestUploadConfigDefaults` — SR-81 (дефолты без config.yaml);
- `TestUploadMaxFileBytesZeroIsError/NegativeIsError` — SR-76 (max_file_bytes ≤ 0);
- `TestUploadMaxFileBytesExceedsCeilingIsError` — SR-76 (max_file_bytes > потолка);
- `TestUploadMaxFileBytesAtCeilingIsOK` — SR-76 (граничное значение);
- `TestUploadDefaultModeSetuidIsError/SetgidIsError/WorldWritableIsError/WorldWritable0777IsError` — ADR-003;
- `TestUploadDefaultModeValidIsOK` — ADR-003 (валидные моды).

**Пакет `internal/mcp` (дополнительно после F-2):**
- `TestUploadRootWarnAuditRecord/unit_warn_writeAudit` — SR-77 (unit);
- `TestUploadRootWarnAuditRecord/warn_when_root` — SR-77 (реальный MCP при euid==0);
- `TestUploadRootWarnAuditRecord/deny_root_upload` — SR-77 (deny_root=true+euid==0).

**Команда запуска (только в Docker):**
```
docker build --target test -t raxd-test . && docker run --rm raxd-test
```

**Результат:** все пакеты PASS, включая race-прогон; нет `skip`/`t.Skip`/закомментированных тестов.

```
ok  github.com/vladimirvkhs/raxd                       0.023s
ok  github.com/vladimirvkhs/raxd/internal/banner       0.001s
ok  github.com/vladimirvkhs/raxd/internal/cli          0.055s
ok  github.com/vladimirvkhs/raxd/internal/cmdexec      1.185s
ok  github.com/vladimirvkhs/raxd/internal/config       0.005s
ok  github.com/vladimirvkhs/raxd/internal/fileupload   0.027s
ok  github.com/vladimirvkhs/raxd/internal/keystore     0.102s
ok  github.com/vladimirvkhs/raxd/internal/mcp          3.732s
ok  github.com/vladimirvkhs/raxd/internal/server       2.213s
ok  github.com/vladimirvkhs/raxd/internal/version      0.001s
(+ race-прогон: cmdexec 2.181s, keystore 1.151s, server 3.977s, mcp 5.344s)
```

## Безопасность

- **SR-69 / ADR-001** — ВСЕ FS-операции записи через `os.OpenRoot` / методы `Root`: `MkdirAll`, `OpenFile`, `Rename`, `Stat`, `Remove`. `os.OpenFile` / `os.MkdirAll` на абсолютных путях внутри `Write` не вызываются. Файл: `internal/fileupload/upload.go`.

- **SR-73** — `chmod` выполняется по дескриптору (`tmpFile.Chmod(in.Mode)`), а не по имени через `Root.Chmod`. Не требует `root`-прав, не подвержен TOCTOU. Файл: `internal/fileupload/upload.go`, `atomicWrite`.

- **SR-74 / ADR-002** — схема: `randomTmpName()` (crypto/rand) → `O_CREATE|O_EXCL` (атомарное создание) → `Chmod` → `Write` → `Sync` → `Close` → `Root.Rename`; `defer cleanup(committed)` удаляет temp при любой ошибке. Fsync dir — best-effort. Файл: `internal/fileupload/upload.go`.

- **SR-75** — ранний фильтр `base64.StdEncoding.DecodedLen(len(in.Content)) > cfg.MaxFileBytes` до `DecodeString`; после декодирования — точная проверка `len(decoded) > cfg.MaxFileBytes`. Предотвращает декодирование огромных payload в RAM. Файл: `internal/mcp/upload_tool.go`.

- **SR-77** — `os.Geteuid() == 0` → `WARN` audit (`reason=running-as-root-upload`); если `cfg.DenyRoot == true` → `deny` audit + ошибка клиенту. Файл: `internal/mcp/upload_tool.go`.

- **SR-78** — `uploadHandler` пишет ровно одну `auditFn(rec)` в каждой ветке; `withAudit`-обёртка не используется (нет двойной записи). Файл: `internal/mcp/upload_tool.go`.

- **SR-79** — `else if isUpload` блок в КАЖДОМ case `writeAudit`; значения `path` передаются через charmbracelet/log, который автоматически экранирует logfmt-спецсимволы — предотвращает logfmt-инъекцию. Файл: `internal/server/audit.go`.

- **SR-80** — `content` и `decoded` не попадают в audit-запись и MCP-ответ никогда; в ответе — `path` (relative, не absolute), `size`, `overwritten`, `mode`; в audit — `fingerprint = hex(sha256(decoded))[:8]`. Файл: `internal/mcp/upload_tool.go`.

- **ADR-003** — `ParseMode` запрещает setuid (`04000`), setgid (`02000`), sticky (`01000`) и world-writable (`0002`); маска `0777`. Файл: `internal/fileupload/mode.go`.

- **Crypto/rand** — temp-имена файлов генерируются через `crypto/rand.Read`, не `math/rand`. Файл: `internal/fileupload/upload.go`, `randomTmpName()`.

- **Права директории** — `os.MkdirAll(uploadRoot, 0o700)` в CLI; `root.MkdirAll(dir, 0o700)` внутри `Write`; temp-файл создаётся с `0o600`, затем chmod по fd. Не создаются файлы с широкими правами.

- **Без shell-интерполяции** — upload_tool не вызывает никаких внешних команд.

- **Аудит-лог** — каждый вызов `upload_file` пишет ровно одну запись: timestamp, fingerprint ключа, tool, result, path, size/reason, remote. Файл: `internal/mcp/upload_tool.go` + `internal/server/audit.go`.
