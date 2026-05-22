# Docs Outline: raxd MCP Server (задача mcp-server)

Автор продукта: **Vladimir Kovalev, OEM TECH**.

Контекст: `raxd serve` теперь, помимо TLS-транспорта и health-check, поднимает MCP-сервер на маршруте
`/mcp` (AC15: docs дают параметры подключения MCP-клиента). Раньше `/mcp` отдавал `501`. Документация
обновлена строго по коду (`internal/mcp/*`, `internal/server/server.go`, `internal/cli/serve.go`,
`internal/config/config.go`, `internal/server/audit.go`); каждое утверждение сверено с реализацией.

## Структура docs/

Файлы документации продукта (английский) и их назначение:

- `README.md` — точка входа: что такое raxd, что работает сегодня, быстрый старт. **Обновлён**: MCP
  server (`ping`/`server_info`) внесён в «What works today»; «Coming next» поправлен (база MCP есть;
  command execution / file ops / больше MCP-tools / mTLS / installer — planned); добавлен пример
  подключения к MCP + ссылка на `docs/mcp.md`.
- `docs/mcp.md` — **новый**. MCP integration guide: эндпоинт `/mcp`, параметры подключения,
  инструменты `ping`/`server_info`, аутентификация, поведение/ошибки, curl smoke-test, конфиг
  MCP-клиента, аудит, scope/ограничения.
- `docs/commands.md` — command reference. **Обновлён**: секция `raxd serve` теперь описывает `/mcp`
  как рабочий маршрут (раньше `/mcp` был `501`); обновлены request pipeline, response codes, audit
  stream (запись `MCP`), ссылки на `docs/mcp.md`; в сводной таблице — примечание про MCP.
- `docs/troubleshooting.md` — **обновлён**: исправлена устаревшая запись «`/mcp` → 501»; добавлен
  раздел «MCP server (`/mcp`)» (GET→405, JSON-RPC-ошибки tools/call, клиент не пробрасывает заголовки);
  TLS-раздел дополнен Node-клиентами (`NODE_TLS_REJECT_UNAUTHORIZED=0`).
- `docs/configuration.md` — без изменений (порт/allowlists/rate-limit актуальны; MCP их наследует, не
  меняет). Проверено grep'ом: устаревших упоминаний MCP/501 нет.
- `docs/development.md` — **обновлён** (минимально и по коду): добавлен пакет `internal/mcp` в layout,
  исправлено описание маршрутизации («/mcp → MCP handler, иначе 501» вместо «всё остальное → 501»),
  MCP внесён в race-тесты, MCP SDK добавлен в таблицу зависимостей.
- man-страницы (`man/raxd.1` и т.п.): **None — не выпускаются.** В репозитории нет man-каталога и нет
  задачи на man-страницы; CLI самодокументирован через cobra (`raxd --help`, `raxd <cmd> --help`).
  Документировать несуществующее нельзя — заводить man-страницы вне scope этой задачи.
- install (`curl | sh`) / `install.sh`: **None — установщика нет.** `curl | sh` относится к задаче
  `distribution` (вне scope; README уже честно отмечает «installer not implemented»). Файла
  `install.sh` в репозитории нет — отдельный install-гайд не пишется, чтобы не документировать
  несуществующее.

## На каждый документ

### `docs/mcp.md` (новый)

- **Цель**: дать интегратору ИИ-агента всё, чтобы подключить MCP-клиент к `raxd` и проверить канал.
- **Аудитория**: интегратор ИИ-агентов / разработчик MCP-клиента; вторично — оператор raxd.
- **Ключевые секции**:
  - What the raxd MCP server is — за тем же `raxd serve`, маршрут `/mcp`, Streamable HTTP над TLS 1.3,
    протокол `2025-11-25`, официальный Go MCP SDK, stateless (нет `MCP-Session-Id`, GET→405).
  - Connection parameters — URL `https://127.0.0.1:<port>/mcp` (порт из config, дефолт `7822`),
    `Authorization: Bearer rax_live_…`, заголовки `Content-Type`/`Accept`/`MCP-Protocol-Version`;
    подраздел Self-signed TLS (доверить серт или `curl -k` / `NODE_TLS_REJECT_UNAUTHORIZED=0` для dev,
    с предупреждением о небезопасности).
  - Tools — `ping` (вход пустой → `pong`); `server_info` (вход пустой → ровно `{name, version,
    protocolVersion}` + text-строка `raxd <ver> (MCP 2025-11-25)`); таблица «что НЕ раскрывается».
  - Authentication — наследуется от транспорта (Bearer→Verify ДО MCP); сессия не используется для
    auth; таблица отказов (401/403/429), которые не доходят до MCP.
  - Behaviour and error handling — initialize/tools.list/tools.call; notifications/initialized→202;
    GET→405; JSON-RPC ошибки `-32700/-32600/-32601/-32602`; input-ошибка инструмента → `isError:true`;
    неизвестный инструмент не исполняется; `/mcp` больше не `501`.
  - curl smoke-test — initialize + tools/call ping + server_info (с `-k`, Bearer, заголовками).
  - Connecting an MCP client — конфиг `type: streamable-http`, `url`, `headers Authorization`;
    оговорки про self-signed и клиентов, не пробрасывающих заголовки.
  - Audit — `INFO MCP fp=<fp> remote=<ip> tool=<name> result=ok`, две строки (`AUTH`+`MCP`) на вызов;
    без секретов.
  - Scope and limitations — реализованы только `ping`/`server_info`; `execute_command`/`upload_file`,
    Resources/Prompts, mTLS, сессии — нет; запуск только в Docker.
  - Author — Vladimir Kovalev, OEM TECH.

### `README.md` (обновлён)

- **Цель**: первый контакт; что работает сегодня и куда движется продукт.
- **Аудитория**: новый пользователь, оценивающий проект.
- **Изменения**: статус-блок и «What is raxd» упоминают рабочий MCP; таблица «What works today» —
  строки про MCP server и MCP audit как Working, а command execution / file upload / больше MCP-tools /
  Resources/Prompts / mTLS как Not implemented; новый пример «connecting to the MCP server» (curl
  ping); «Coming next» переписан (база MCP есть, остальное planned); ссылка на `docs/mcp.md`.

### `docs/commands.md` (обновлён)

- **Цель**: точный справочник по CLI и поведению `serve`.
- **Аудитория**: оператор raxd.
- **Изменения**: примечание «MCP не CLI-команда, хостится `serve` на `/mcp`»; в `raxd serve` — `/mcp`
  как рабочий маршрут (scope, request pipeline, response codes, «Calling the endpoints»); audit stream
  описывает запись `MCP` (две строки на tools/call); scope note в `raxd key` упоминает MCP-аутентификацию;
  ссылки на `docs/mcp.md`.

### `docs/troubleshooting.md` (обновлён)

- **Цель**: типовые проблемы и решения.
- **Аудитория**: оператор / интегратор.
- **Изменения**: убрана устаревшая фраза «`/mcp` → 501» (теперь `/mcp` рабочий); добавлен раздел «MCP
  server (`/mcp`)»; TLS-раздел охватывает `/mcp` и Node-клиентов.

### `docs/development.md` (обновлён)

- **Цель**: сборка/тесты в Docker и устройство кода.
- **Аудитория**: контрибьютор.
- **Изменения**: пакет `internal/mcp` в layout; верное описание маршрутизации; MCP в race-тестах; MCP
  SDK в таблице зависимостей + примечание про офлайн-вендоринг.

## Примеры команд (проверяемые, из реального CLI/кода)

- `raxd key create --name production-key` — выпуск API-ключа (`rax_live_…`, показывается один раз).
- `raxd key list` — таблица ключей (полный 16-hex id, label, created, last used).
- `raxd key delete <id>` — отзыв ключа (soft revoke).
- `raxd serve` — старт TLS-сервера + MCP на `/mcp` (дефолт `127.0.0.1:7822`).
- `curl -k -H "Authorization: Bearer $KEY" https://127.0.0.1:7822/healthz` → `pong`.
- `curl -k https://127.0.0.1:7822/mcp -H "Authorization: Bearer $KEY" -H "Content-Type: application/json"
  -H "Accept: application/json, text/event-stream"
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ping","arguments":{}}}'`
  → `{"...":"result":{"content":[{"type":"text","text":"pong"}],"isError":false}}`.
- `raxd config port 8080` → `error: config port: not implemented yet` (stub; порт менять правкой
  `config.yaml`).

## Об авторе (OEM TECH)

Обязательный блок **Vladimir Kovalev, OEM TECH** размещён в:
- `README.md` — статус-блок («Author:»), раздел «Author», и баннер в примерах.
- `docs/mcp.md` — раздел «Author» в конце.
Включает имя и организацию. Контакты/лицензия не указаны: лицензионного файла в репозитории нет
(README честно фиксирует «No license file … yet»), выдумывать не стали.

## Сверка код ↔ документация (ключевые факты, все подтверждены)

| Утверждение | Источник в коде | Статус |
|---|---|---|
| Порт по умолчанию `7822`, bind `127.0.0.1` | `internal/config/config.go` (`SetDefault`) | подтверждено |
| `/mcp` смонтирован MCP-handler ДО catch-all; иначе `501` | `internal/server/server.go` `New(...)` | подтверждено |
| Catch-all `/` по-прежнему `501 "not implemented"` | `internal/server/handlers.go` `dispatchHandler` | подтверждено |
| Транспорт: Streamable HTTP, `Stateless:true, JSONResponse:true` | `internal/mcp/server.go` `NewHandler` | подтверждено |
| Протокол `2025-11-25` | `internal/mcp/tools.go` `protocolVersion` | подтверждено |
| serverInfo `Name:"raxd", Version:ver` | `internal/mcp/server.go` (Implementation) | подтверждено |
| `ping` → text `pong`, без I/O | `internal/mcp/tools.go` `pingHandler` | подтверждено |
| `server_info` → ровно `{name, version, protocolVersion}` + text | `internal/mcp/tools.go` `ServerInfo`/`serverInfoHandler` | подтверждено |
| Аудит `INFO MCP fp=… remote=… tool=… result=ok`; `tool=` только при `Tool!=""` | `internal/server/audit.go` `writeAudit` | подтверждено |
| MCP-аудит берёт fingerprint+remote из ctx, не тело ключа | `internal/mcp/audit.go`, `internal/server/auth.go` | подтверждено |
| serve строит `NewHandler(version.Version, auditFn)`, общий канал аудита | `internal/cli/serve.go` `runServe` | подтверждено |
| Аутентификация Bearer ДО MCP; `internal/mcp` не импортирует keystore | `internal/server/auth.go`, `internal/mcp/*` | подтверждено |
| GET `/mcp` → 405 (stateless) | `internal/mcp/server.go` (Stateless) + test-plan `TestMCPGetReturns405` | подтверждено |
| Неизвестный инструмент → JSON-RPC error, не исполнение | mcp-spec §7.1 + test-plan `TestUnknownToolNotExecuted` | подтверждено |
| MCP SDK `go-sdk v1.6.0` — прямая зависимость, вендорится офлайн | `go.mod`, impl-notes INFO-1 | подтверждено |

## Открытые вопросы

- None. Расхождений «спека ≠ код» не выявлено. Все 15 AC + SR-27..SR-39 подтверждены кодом и
  test-plan; устаревшее «`/mcp` → 501» в `docs/troubleshooting.md` и `docs/development.md` исправлено.

### Учтённые решения (зафиксированы, не открыты)

- Q-MCP-1 (`echo` в `ping`): НЕ добавлен в коде → в доке `ping` описан без параметров.
- Q-MCP-2 (`commit`/`date` в `server_info`): НЕ включены в коде → в доке `server_info` ровно 3 поля.
- Man-страницы и install-гайд: не существуют в репозитории → разделы помечены `None` с причиной, не
  выдуманы (red line: документировать только реально существующее).
