# Plan: file-upload — MCP-инструмент `upload_file` для безопасной записи файла на хост

Автор плана: architect (raxd). Вход: spec.md (AC1–AC20, pm-guardian pass), research.md, context.md,
ADR-001 (os.Root traversal-safe), ADR-002 (атомарная запись temp+rename + права по fd), образец
command-exec (plan.md, ADR-004, реальный `internal/cmdexec/`, `internal/mcp/exec_tool.go`,
`internal/server/audit.go`, `internal/mcp/server.go`, `internal/config/config.go`, keystore.writeDB).
Автор продукта: Vladimir Kovalev, OEM TECH. Размер обусловлен 20 плотными security-AC и неделимостью
задачи (spec §«Примечание о размере»): traversal, лимит, overwrite, права, атомарность, аудит — единый
контракт. Развилки Q1–Q5 закрыты ниже конкретными числами/политиками + новым ADR-003 (mode-политика).

## Chosen Approach
Логику записи выносим в новый чистый пакет **`internal/fileupload`** (валидация + атомарная запись через
`os.Root`, без MCP/логирования — юнит-тестируем офлайн), MCP-обёртку (`UploadInput`/`UploadOutput` +
handler с собственным аудитом) — в `internal/mcp/upload_tool.go`. Регистрация — той же точкой
`sdkmcp.AddTool` в `NewHandler`. **Аудит upload особый (как ADR-004 у exec):** `upload_file` НЕ
оборачивается generic `withAudit`; handler сам пишет РОВНО одну upload-аудит-запись во всех ветках
(success/deny/fail) + отдельный root-WARN. Traversal-safety — целиком на `os.Root` (ADR-001):
`OpenRoot(uploadRoot)` → `Root.MkdirAll` → temp (`crypto/rand`-имя, `O_CREATE|O_EXCL`) → chmod по fd →
write → Sync → `Root.Rename` → fsync-dir; temp очищается на любой ошибке (ADR-002). base64 декодируется
с проверкой размера ДО записи. Всё на stdlib — новых зависимостей нет (AC20). Альтернативы — в Trade-offs.

## Modules
- `internal/fileupload/upload.go` — чистый писатель `Write(cfg, in) (Result, error)`: открытие `os.Root`,
  лексический ранний отказ (`filepath.IsLocal`), `Root.MkdirAll`, temp+chmod-fd+write+Sync, проверка
  overwrite/каталог (`Root.Stat`), `Root.Rename`, fsync-dir, очистка temp. Без MCP/SDK/логов (AC20).
- `internal/fileupload/config.go` — `Config` пакета (поля секции `upload`); маппится из `config.UploadConfig`.
- `internal/fileupload/mode.go` — `ParseMode(s) (fs.FileMode, error)` и `validateMode`: парс восьмеричной
  строки, запрет опасных битов (ADR-003). Без I/O — юнит-тестируем.
- `internal/mcp/upload_tool.go` — `UploadInput`/`UploadOutput` (json/jsonschema-теги), `uploadTool()`
  (дескриптор), `uploadHandler(cfg fileupload.Config, audit server.AuditFn)` (base64-декод+лимит,
  root-WARN, маппинг `Result`→`UploadOutput`, собственный upload-аудит во всех ветках; ADR-002/ADR-004-стиль).
- `internal/config/config.go` — **расширить** `Config` секцией `Upload UploadConfig` + viper-дефолты
  (см. §Config); без env-оверрайдов. `internal/config/paths.go` потребляется для дефолта корня.
- `internal/mcp/server.go` — **точка интеграции**: `sdkmcp.AddTool(s, uploadTool(), uploadHandler(uplCfg, audit))`
  БЕЗ `withAudit`; сигнатура `NewHandler` расширяется параметром `uplCfg fileupload.Config`.
- `internal/server/audit.go` — **расширить** `AuditRecord` (поля `Path string`, `Size int64`) и `writeAudit`
  (ветка `isUpload := rec.Tool == "upload_file"`, зеркально `isExec`) — Q5/AC12. Exec-ветки не трогаем.
- `internal/cli/serve.go` — **точка интеграции**: собрать `fileupload.Config` из `cfg.Upload` (резолв
  пустого `UploadRoot` к дефолту `<paths.StateDir>/uploads`, `EnsureDirs`-аналог 0700), передать в `NewHandler`.

## Contracts
- `fileupload.Write(cfg Config, in Input) (Result, error)` (`upload.go`)
  - параметры: `cfg` — корень/лимит/дефолтный режим; `in Input{RelPath string; Data []byte; Overwrite bool;
    Mode fs.FileMode}` (`Data` уже декодирован handler'ом; `Mode` уже распарсен/провалидирован, либо дефолт).
  - возврат при успехе: `Result{RelPath string; Size int64; Overwritten bool; Mode fs.FileMode}` (RelPath —
    очищенный относительный путь как принят; Size — len(Data); Mode — фактический режим).
  - ошибки (handler → `isError:true` + deny/fail-аудит): `ErrTraversal` (абс. путь / `..`-escape / симлинк
    наружу — от `Root`; AC4 → deny), `ErrExists` (цель есть, overwrite=false; AC8 → deny), `ErrIsDir`
    (цель — каталог; AC14 → deny), `ErrTooLarge` (Size > MaxFileBytes — дублирующая страховка после handler;
    AC7 → deny), `ErrBadMode` (недопустимый режим — если валидация не сделана раньше; AC14 → deny), прочее
    I/O (диск полон/ошибка записи; AC14 → fail). Temp очищается на ЛЮБОЙ ошибке (defer; AC7/AC10). Не паникует (AC14).
  - привилегии: НЕ chown/setuid — файл под UID/GID демона как есть (AC9); chmod ТОЛЬКО по fd до записи (ADR-002).
- `fileupload.ParseMode(s string) (fs.FileMode, error)` (`mode.go`) — парсит восьмеричную строку («0600»);
  пустая → ошибка (handler подставляет дефолт ДО вызова); неоктал/вне диапазона/опасные биты → `ErrBadMode` (ADR-003).
- `UploadInput` (`upload_tool.go`): `Path string \`json:"path"\`` (обязателен), `Content string \`json:"content"\``
  (обязателен, base64), `Overwrite bool \`json:"overwrite,omitempty"\``, `Mode string \`json:"mode,omitempty"\``.
  Поля абсолютного пути/владельца НЕТ (AC2). `additionalProperties:false` — инференцией SDK; **закрепить тестом**
  «лишнее поле → isError» (AC2).
- `UploadOutput` (`upload_tool.go`): `Path string \`json:"path"\``, `Size int64 \`json:"size"\``,
  `Overwritten bool \`json:"overwritten"\``, `Mode string \`json:"mode"\`` — AC3. Абсолютный путь НЕ включается.
- `uploadHandler(cfg fileupload.Config, audit server.AuditFn) sdkmcp.ToolHandlerFor[UploadInput, UploadOutput]`
  (`upload_tool.go`) — **сам владеет upload-аудитом; generic withAudit не применяется (ADR-004-стиль).**
  - root-детекция (AC11): `os.Geteuid()==0` → отдельная WARN-запись (`Result:"warn"`, reason="running-as-root")
    при КАЖДОМ вызове; при `cfg.DenyRoot` — затем deny-запись + isError (политика — Q3, ниже).
  - ранний фильтр размера (AC7/AC16): `base64.StdEncoding.DecodedLen(len(Content)) > MaxFileBytes` → deny
    без декодирования; затем `DecodeString` (`CorruptInputError` → deny, AC6) и точная `len(decoded) ≤ MaxFileBytes`.
  - режим: пусто → `cfg.DefaultMode`; иначе `fileupload.ParseMode(Mode)` (невалидный → deny, AC14).
  - зовёт `fileupload.Write`; маппит `Result`→`UploadOutput` + text-резюме (`path=… size=…B overwritten=…`).
  - **upload-аудит (Q5/AC12/AC19):** fp=`server.FingerprintFromContext`, remote=`server.RemoteAddrFromContext`.
    success → `AuditRecord{Tool:"upload_file", Result:"success", Path:relPath, Size:size, Fingerprint, RemoteAddr}`;
    deny (traversal/exists/isdir/too-large/bad-base64/bad-mode) → `Result:"deny"` + Path(если известен)+Reason;
    fail (I/O/диск полон) → `Result:"fail"` + Path+Reason. РОВНО одна основная запись на вызов (+root-WARN).
    СОДЕРЖИМОЕ (`Content`/decoded) НЕ логируется НИКОГДА (AC12/AC13).
- `NewHandler(ver string, audit server.AuditFn, execCfg cmdexec.Config, uplCfg fileupload.Config) (http.Handler, error)`
  (`server.go`, **расширение сигнатуры**) — добавлен `uplCfg`; регистрирует `upload_file` рядом с exec. Обновить serve.go + mcp-тесты.
- **`AuditRecord` (расширение)** — добавить опц. `Path string`, `Size int64` (логируются только при `Tool=="upload_file"`).
- **`writeAudit` (расширение)** — `isUpload`-ветка: в success логировать `path=`,`size=`; в deny/fail —
  `path=`(если есть)+`reason=`. Не-upload записи не меняются (новые поля только когда `isUpload`).

## Config (`internal/config/config.go`, секция `upload`, viper-дефолты, без env-оверрайдов)
- `upload.root string` — дефолт **пусто** → serve.go резолвит к `<StateDir>/uploads` (Q1: согласован с
  XDG-раскладкой keys.db/tls, права `0700`; пустой/невалидный → безопасный дефолт, AC5a; НЕ `/`, НЕ `/root`).
- `upload.max_file_bytes int64` — дефолт **`716800` (700 KiB)** (Q4: укладывается под `max_body_bytes`=1 MiB
  с учётом base64 ×4/3 + JSON-RPC overhead; потолок ≈ (1 MiB−overhead)×3/4 ≈ 785 KiB; 700 KiB — с запасом, AC16).
- `upload.default_mode string` — дефолт **`"0600"`** (AC9).
- `upload.overwrite_default bool` — дефолт **`false`** (AC8/AC15).
- `upload.deny_root bool` — дефолт **`false`** (Q3: отдельный флаг, только WARN; ниже).
Все — `v.SetDefault` в `Load`, чтение в `buildConfig` в `Config.Upload UploadConfig`; невалидный
`max_file_bytes`(≤0 или > выводимого из `max_body_bytes` потолка) / `default_mode` → ошибка на старте (AC15).

## AC → реализация
| AC | Где |
|---|---|
| AC1 | `server.go` AddTool(upload_file) за той же цепочкой; tools/list содержит инструмент; CLI-подкоманды нет |
| AC2 | `UploadInput` struct → additionalProperties:false (тест на лишнее поле); полей abs-path/owner нет |
| AC3 | `UploadOutput` (path/size/overwritten/mode); абсолютный путь не включается |
| AC4 | `os.Root` (ADR-001): абс/`..`/симлинк наружу/TOCTOU → ErrTraversal → deny |
| AC5a | serve.go резолв пустого/невалидного root → `<StateDir>/uploads` (0700) |
| AC5b | `Root.MkdirAll(dir(rel), 0700)` — подкаталоги только внутри корня |
| AC6 | `base64.DecodeString` → CorruptInputError → deny |
| AC7 | DecodedLen ранний фильтр + точная len ≤ MaxFileBytes; temp не остаётся |
| AC8 | `Root.Stat` цель + overwrite=false → ErrExists deny; overwrite=true → Rename поверх (атомарно) |
| AC9 | chmod по fd ДО записи (ADR-002), дефолт 0600; UID/GID демона (без chown/setuid) |
| AC10 | temp(`crypto/rand`,O_EXCL)→Sync→`Root.Rename`→fsync-dir; temp очищается (defer) |
| AC11 | `uploadHandler` os.Geteuid()==0 → WARN каждый вызов; opt deny_root |
| AC12/AC19 | handler сам пишет upload-AuditRecord (success/deny/fail) path+size+fp+remote+result; ровно одна |
| AC13 | fingerprint вместо ключа; content не логируется; нейтральные ошибки (без abs-путей) |
| AC14 | цель-каталог→ErrIsDir; диск полон→fail; невалидный mode→ErrBadMode; не паникует, сервер жив |
| AC15 | секция `upload` с дефолтами; невалидные значения отвергаются на старте |
| AC16 | max_file_bytes=700 KiB ≤ потолка из max_body_bytes; тело > max_body_bytes → 413 транспортом |
| AC17/AC18 | auth/rate-limit транспорта ДО инструмента (не переписываются) |
| AC20 | сборка/тесты `-mod=vendor` в Docker; `internal/fileupload` юнит-тестируется офлайн; новых зависимостей нет |

## Trade-offs
- Выбрали **отдельный пакет `internal/fileupload`** вместо логики в `internal/mcp` (как cmdexec): цена —
  +пакет и адаптер-маппинг MCP↔fileupload; взамен — чистый, MCP-независимый писатель, юнит-тесты без HTTP/SDK.
- Выбрали **собственный upload-аудит в handler (ADR-004-стиль)** вместо generic `withAudit`: цена —
  ещё один инструмент с собственным аудит-путём (асимметрия с ping/server_info); взамен — нет двойной
  записи, deny/fail внутри Write надёжно покрыты, типизированные path/size доступны напрямую (AC19).
- Выбрали **upload_root = `<StateDir>/uploads`** вместо статичного `/var/lib/raxd/uploads`: цена — путь
  зависит от XDG/HOME демона (для системного развёртывания админ задаёт `upload.root` явно); взамен —
  консистентность с раскладкой keys.db/tls (D3, STACK §Раскладка), работает и для не-root демона (baseline §3).
- Выбрали **max_file_bytes=700 KiB БЕЗ подъёма `max_body_bytes`**: цена — потолок одного файла ~700 KiB
  (большие файлы → Out of Scope, chunked); взамен — не трогаем транспорт (подъём max_body_bytes — глобальная
  смена DoS-границы, затрагивает все эндпоинты), запас под base64+overhead гарантирует «нет 413 у границы» (AC16).
- Выбрали **отдельный `upload.deny_root`** (не переиспользование `exec.deny_root`): цена — +1 флаг конфига;
  взамен — независимая политика (запись файла и запуск команды — разный риск; админ управляет раздельно).
  Механизм детекции (`os.Geteuid()==0`) общий/повторяется, как в exec. **Подтверждает security.**
- Выбрали **mode-политику: запрет setuid/setgid/sticky и world-writable, маска `0777`** (ADR-003): цена —
  клиент не может выставить эти биты (для них — Out of Scope chmod/спецфайлы); взамен — нельзя создать
  setuid-файл по сети (защита от эскалации). **Подтверждает security.**
- Новых внешних зависимостей **нет**: всё на stdlib (`os`+`os.Root`, `path/filepath`, `encoding/base64`,
  `crypto/rand`, `io/fs`, `os`) + уже вендоренные `charmbracelet/log`/go-sdk — сверено со STACK.ru.md (stdlib предпочтительна).

## Открытые зависимости (подтверждает security в threat-model.md)
- **Ограничения `os.Root` (ADR-001):** не блокирует traversal через **mount points** (только симлинки/`..`/
  TOCTOU) — низкий риск в контейнере baseline §6; и `Root.Chmod`-по-имени race на Unix (обойдён chmod по fd,
  ADR-002). Принятие риска и фиксация — **security**.
- **mode-политика (ADR-003, Q2):** запрет setuid·setgid·sticky·world-writable, маска `0777`, дефолт 0600 —
  достаточность подтверждает **security**.
- **Политика root-демона (Q3/AC11):** отдельный `upload.deny_root`, дефолт WARN — риск записи от root +
  смягчение (раскладка не-root baseline §3, контейнер §6) подтверждает **security**.
- **Числа (Q4/AC16):** `max_file_bytes=700 KiB` под `max_body_bytes=1 MiB` (base64 ×4/3 + overhead) —
  достаточность/корректность потолка подтверждает **security**.
- **Представление аудита (Q5/AC12):** опц. поля `AuditRecord.Path/Size`, логируемые только для upload_file
  (зеркало isExec) — без слома формата прочих записей — подтверждает **security**.
