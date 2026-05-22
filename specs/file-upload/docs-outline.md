# Docs Outline: file-upload — MCP-инструмент `upload_file`

> План документации продуктовой фичи `upload_file` (запись файла на хост через MCP). Источник истины —
> КОД (`internal/mcp/upload_tool.go`, `internal/fileupload/{upload,config,mode}.go`,
> `internal/config/config.go`, `internal/server/audit.go`, `internal/cli/serve.go`), сверенный со
> spec/mcp-spec/security-requirements/threat-model и review.md. Язык docs/** — английский (как все docs);
> этот outline — русский. Автор продукта: **Vladimir Kovalev, OEM TECH**.

## Карта затронутых файлов docs/

| Файл | Действие | Что внесено |
|---|---|---|
| `docs/mcp.md` | изменён | Новый раздел `upload_file` (вход/выход/error-mapping/аудит/curl). Обновлены: таблица свойств (4 tool), §Tools (теперь четыре, не «нет upload_file»), §Behaviour (убрана фраза «upload_file не реализован»), §Scope (upload_file → реализован), §Related (ссылка на security-гайд) |
| `docs/configuration.md` | изменён | Новая секция `upload` (ключи + дефолты + валидация на старте). Кросс-ссылки |
| `docs/file-upload-security.md` | СОЗДАН | Security-гайд (по образцу `execute-command-security.md`): mount points, секреты в путях, root, нет disk-quota, mode-политика, остаточные риски |
| `docs/troubleshooting.md` | изменён | Новая секция «The `upload_file` tool» (типовые ошибки/причины). Обновлены упоминания «upload_file не реализован» |
| `docs/commands.md` | изменён | Строка про `upload_file` в списке MCP-инструментов `serve` (§MCP server, §Out of scope, §Audit) |
| `README.md` | изменён | `upload_file` в «What works today», статусной врезке, §MCP, §Documentation; строка автора сохранена |

## На каждый документ

### `docs/mcp.md` — раздел `upload_file`
- **Цель:** дать интегратору полный контракт MCP-инструмента записи файла (вход, выход, ошибки, аудит, curl).
- **Аудитория:** интегратор ИИ-агентов (MCP-клиент), оператор.
- **Ключевые секции:**
  - Назначение: безопасная запись ОДНОГО обычного файла в upload root через `/mcp`.
  - Вход `UploadInput` — 4 поля: `path` (rel, req), `content` (base64, req), `overwrite` (опц., false),
    `mode` (опц., восьмеричная строка). Строгость схемы инференцируется SDK из typed handler
    (лишнее поле → `isError:true`, файл не создан).
  - Выход `UploadOutput` — РОВНО 4 поля: `path`, `size`, `overwritten`, `mode`. НЕТ абсолютного пути, НЕТ содержимого.
  - Error-mapping: traversal/абс./симлинк наружу → deny; mode вне 0777 или world-writable → deny; exists без
    overwrite → deny; цель-каталог → deny; невалидный base64 → deny; too-large → deny; I/O → fail.
  - Аудит-строки (точные форматы из `audit.go`): success → `msg=MCP … tool=upload_file result=ok path= size=`;
    deny → `msg=DENY … tool=upload_file reason= [path=]`; fail → `msg=FAIL …`; root → `msg=WARN … reason=running-as-root`
    (БЕЗ `path=` — root-WARN до парсинга пути).
  - curl-примеры: (a) успех mode 0600; (b) traversal `../etc/...` → isError; (c) too-large → isError;
    (d) setuid `04000` → isError.
  - **Note/Caveat 4xx/400:** превышение `max_body_bytes` → транспортный 4xx (фактически 400 «failed to read
    body» от go-sdk на MaxBytesReader, НЕ 413); too-large в пределах тела → deny с isError. Файл не создаётся.

### `docs/configuration.md` — секция `upload`
- **Цель:** документировать конфиг-секцию `upload` с дефолтами и стартовой валидацией.
- **Аудитория:** оператор.
- **Ключевые секции:** ключи `upload.root` / `max_file_bytes` / `default_mode` / `overwrite_default` /
  `deny_root` с реальными дефолтами из кода; дефолт root = `<StateDir>/uploads` (0700); стартовая валидация
  (`max_file_bytes>0` и ≤ потолка из `max_body_bytes`; `default_mode` проходит mode-политику).

### `docs/file-upload-security.md` — НОВЫЙ security-гайд
- **Цель:** обязательные предупреждения по безопасности записи файла по сети.
- **Аудитория:** оператор/безопасник перед включением против реального хоста.
- **Ключевые секции:** mount points/bind-mount (SR-70/ОР-U2); секреты в путях (путь логируется);
  запись от root (WARN + `deny_root`, ОР-U1); нет disk-quota (ОР-U3); mode-политика (ADR-003, пост-F-1);
  что инструмент НЕ делает; остаточные риски; кросс-ссылки.

### `docs/troubleshooting.md` — секция `upload_file`
- **Цель:** диагностика типовых отказов записи.
- **Аудитория:** интегратор/оператор.
- **Ключевые секции:** too-large (per-file vs тело 4xx/400), bad-mode, exists без overwrite, traversal/абс.,
  is-dir, root-WARN, permission denied на upload root; что проверить (`upload.root`, права 0700, лимиты).

### `docs/commands.md` и `README.md`
- **Цель:** упомянуть `upload_file` рядом с ping/server_info/execute_command (точечно).
- **Аудитория:** все.
- **Ключевые секции:** список MCP-инструментов `serve`; статус «Working»; ссылка на `mcp.md#upload_file`.

## Примеры команд (проверяемые, из реального CLI / mcp-spec §6 / кода)
- `raxd key create --name laptop` — выпуск ключа (источник Bearer для `/mcp`).
- curl успех: `{"name":"upload_file","arguments":{"path":"notes/hello.txt","content":"aGVsbG8K"}}` → `path/size=6/overwritten/mode=0600`.
- curl traversal: `{"path":"../etc/passwd","content":"eA=="}` → `isError:true`.
- curl setuid: `{"path":"tool","content":"eA==","mode":"04000"}` → `isError:true` (mode вне 0777).

## Об авторе (OEM TECH)
**Vladimir Kovalev, OEM TECH** — автор raxd. Размещение: блок «Author» в README (сохранён) и в
`docs/file-upload-security.md` (как у `execute-command-security.md`/`mcp.md`).

## Mandatory-предупреждения (чек-лист для tech-writer-guardian)
- [x] **4xx/400 при превышении тела** — `docs/mcp.md` §upload_file (Note/Caveat «Size limit vs transport
      body limit») + `docs/troubleshooting.md` §upload_file. Документирован ФАКТ из кода/review.md (400
      «failed to read body» от go-sdk, НЕ 413); расхождение со spec/mcp-spec (413) отмечено явно.
- [x] **Mount points / bind-mount внутри upload root** — `docs/file-upload-security.md` §1 (SR-70/ОР-U2).
      Рекомендация: upload root — выделенный каталог без точек монтирования.
- [x] **Секреты в путях** — `docs/file-upload-security.md` §2 (путь логируется в аудит как logfmt-значение;
      содержимое НЕ логируется). Аналогия «no secrets in argv» из execute-command-security.md.

## Правки по гейту tech-writer-guardian (verdict needs-changes, 2 minor — оба ЗАКРЫТЫ)
- [x] **Issue 1 — `path=` в WARN-строке аудита.** Факт по коду: `audit.go:157-175` (ветка
      `case "warn"/isUpload`) пишет `path=` ТОЛЬКО при `rec.Path != ""`; root-WARN
      (`upload_tool.go:109-120`) эмитится с пустым `Path` → реальная WARN-строка при euid==0 НЕ содержит
      `path=`. Исправлено: в таблицах формата аудита `path=` для WARN помечен опциональным
      (`[path=<rel>] only if the path is already known`) + добавлено примечание «root-WARN предшествует
      парсингу/валидации пути, поэтому поля `path=` не содержит; при `deny_root=true` следующая отдельная
      DENY-запись уже содержит `path=<rel>`». Места: `docs/mcp.md` §«upload_file audit records» (таблица +
      bullet, + пометка под root-примером) и `docs/file-upload-security.md` §6 (таблица + bullet).
      Inline-примеры WARN были корректны (без `path=`) и оставлены.
- [x] **Issue 2 — `additionalProperties:false` как явное свойство схемы.** Факт: ручной JSON Schema с этим
      полем в коде НЕТ; строгость даёт go-sdk при typed handler `ToolHandlerFor[UploadInput, UploadOutput]`,
      инференцируя схему из Go-структуры. Исправлено: в `docs/mcp.md` §«Input — `UploadInput`»
      переформулировано — больше не утверждается явное поле схемы; теперь «the SDK derives a strict schema
      (no additional properties) by inference from the typed handler
      (`ToolHandlerFor[UploadInput, UploadOutput]`)». Раздел `execute_command` НЕ тронут (его формулировка —
      принятая проектная конвенция, scope не расширялся).

## Открытые вопросы
- None. Каждое утверждение docs сверено с кодом.
  - Замечание (не блок): `impl-notes.md` §15 описывает fingerprint как `sha256(decoded)[:8]hex`, но КОД
    (`upload_tool.go`) использует `server.FingerprintFromContext(ctx)` (fingerprint ключа из ctx, НЕ хэш
    содержимого) — это и задокументировано (код — источник истины). Расхождение в impl-notes косметическое,
    на контракт не влияет; зафиксировано здесь для reviewer/guardian, доком не «чинится».
  - Замечание (не блок): F-1 закрыт в коде (`mode.go`/`config.go`: `mode&^0o777 != 0`) — документируется
    строгая политика «любой бит вне 0777 запрещён».
