# Plan: MCP Server — MCP-эндпоинт поверх готового HTTP/TLS-транспорта raxd

Автор плана: architect (raxd). Вход: spec.md (AC1–AC15), research.md, ADR-001 (SDK), ADR-002 (версия
2025-11-25), ADR-003 (Origin/auth). Опора на готовый код `internal/server/*` (`tls-transport`).
Автор продукта: Vladimir Kovalev, OEM TECH.

## Chosen Approach
Реализуем MCP на **официальном Go SDK `github.com/modelcontextprotocol/go-sdk/mcp` (v1.6.0)**: новый
пакет `internal/mcp` конструирует `*mcp.Server` (`Implementation{Name:"raxd", Version: version.Version}`),
типизированно регистрирует `ping`/`server_info` через `mcp.AddTool`, оборачивает их аудит-декоратором и
отдаёт `http.Handler` от `mcp.NewStreamableHTTPHandler`. Этот handler монтируется на маршрут `/mcp`
ВМЕСТО 501-заглушки ВНУТРИ той же middleware-цепочки (bodyLimit→recover→Host/Origin→auth→rate-limit→
authSuccessAudit→mux) — auth/Origin/rate-limit/audit отрабатывают ДО MCP. ADR-001/002/003 → `accepted`.
Выбор обоснован: SDK даёт spec-compliance и совместимость клиентов «из коробки», `StreamableHTTPHandler`
— это `http.Handler` (ADR-001 tls-transport спроектировал точку расширения именно под него), офлайн-
вендоринг реализуем (research §E). stdlib-реализация отклонена (Trade-offs).

## Modules
- `internal/mcp/server.go` — `NewHandler`: строит `*mcp.Server`, объявляет capability `tools` (только
  tools, не resources/prompts — Q4), берёт версию из `internal/version`, регистрирует инструменты,
  возвращает `http.Handler` (Streamable HTTP). Точка расширения (AC13).
- `internal/mcp/tools.go` — определения `ping` и `server_info`: типы вход/выход, `*mcp.Tool` (имя,
  описание, `InputSchema` = `{"type":"object","additionalProperties":false}`), хендлеры (тела — developer).
- `internal/mcp/audit.go` — декоратор `withAudit`, оборачивающий tool-хендлер: достаёт fingerprint из
  контекста запроса, пишет аудит MCP-вызова (имя инструмента + результат) через инжектированный `AuditFn`.
- `internal/server/audit.go` — **расширить** `AuditRecord` (поле `Tool`) и `writeAudit` (логировать
  `tool=` во ВСЕХ ветках, включая success). См. Contracts; ЛОМАЮЩЕЕ изменение контракта транспортного аудита.
- `internal/server/server.go` — расширить `New`: принять опциональный `mcpHandler http.Handler`; если
  не nil — `mux.Handle("/mcp", mcpHandler)` ДО catch-all `dispatchHandler`; экспортировать
  `FingerprintFromContext` (publ. обёртка над `fingerprintFromCtx`) для `internal/mcp`.
- `internal/cli/serve.go` — собрать `internal/mcp`-handler и передать его в `server.New` (AC11).
- `vendor/` — добавить SDK + транзитивные (developer, `go mod vendor` на хосте; AC14, ADR-002).

## Contracts
- `mcp.NewHandler(ver string, audit server.AuditFn) (http.Handler, error)` (`internal/mcp/server.go`)
  - параметры: `ver` — версия raxd (`version.Version`) для `server_info`; `audit` — функция аудита из транспорта;
  - возврат: `http.Handler` (от `mcp.NewStreamableHTTPHandler`), монтируемый на `/mcp`; SDK-handler конкурентен;
  - ошибки: возвращает ошибку только при невозможности построить сервер/схемы (фатально для `serve`), не паникует.
- `pingHandler(ctx, req *mcp.CallToolRequest, in PingInput) (*mcp.CallToolResult, PingOutput, error)`
  - вход `PingInput{}` (пустой); выход: `CallToolResult.Content=[{type:"text",text:"pong"}]`, `IsError:false` (AC5);
  - без побочных эффектов на хосте; ошибки SDK сериализует как JSON-RPC error.
- `serverInfoHandler(ctx, req *mcp.CallToolRequest, in InfoInput) (*mcp.CallToolResult, ServerInfo, error)`
  - выход `ServerInfo{Name string; Version string; ProtocolVersion string}` (через `structuredContent` +
    дублирующий text-блок); БЕЗ секретов: нет тел ключей/хэшей/salt/приватного TLS-ключа (AC6/AC10);
  - источник версии — переданный `ver` (из `internal/version.Version`), НЕ чтение секретов.
- **`AuditRecord` (расширение, ЛОМАЮЩЕЕ)** — добавить поле `Tool string` («имя MCP-инструмента/метод;
  пусто для не-MCP записей»). Сохраняет инвариант SR-21 (имя инструмента — не секрет; тело ключа НЕ хранится).
- **`writeAudit` (расширение, ЛОМАЮЩЕЕ)** — логировать `tool=<rec.Tool>` во ВСЕХ ветках, ВКЛЮЧАЯ `success`,
  только если `rec.Tool != ""` (для существующих connection-записей `tool` отсутствует → формат AUTH/FAIL/
  DENY/RATE не меняется). Пример success MCP-записи: `INFO MCP fp=<fp> remote=<ip> tool=ping result=ok`.
  Затрагивает: `internal/server/audit.go` + тесты аудита tls-transport (`internal/server/server_test.go`,
  `internal/server/server_qa_test.go`) — они проверяют подстроки (`AUTH`, `fp=`, `fp=-`), новое поле их
  не ломает; qa добавляет ассерт на `tool=` для MCP-success. msg-label для MCP-успеха — `MCP` (новый).
- `(internal/mcp) withAudit(name string, h ToolHandler, audit server.AuditFn) ToolHandler`
  - оборачивает хендлер: после вызова формирует `AuditRecord{Fingerprint: server.FingerprintFromContext(ctx),
    Tool: name, Result: "success"|"fail", RemoteAddr, Reason}` и зовёт `audit`; имя инструмента — из MCP-вызова
    (аргумент `name`/`req`), fingerprint — из ctx (AC9), тело ключа НЕ извлекается (AC10).
- `server.FingerprintFromContext(ctx context.Context) string` (`internal/server`, новая экспортируемая
  обёртка) — возвращает fingerprint, положенный `authMiddleware` в ctx; это РОВНО значение `keystore.Fingerprint`
  (тело ключа MCP-слою недоступно, AC10 соблюдён); `"-"` если ключа нет. Без секретов.
- `server.New(cfg, paths, store, logger, mcpHandler http.Handler) (*Server, error)` — добавлен последний
  параметр; `nil` → поведение как сейчас (501 на не-health). Не nil → `/mcp` зарегистрирован за цепочкой.
- Обработка ошибок (SDK, AC7): неизвестный инструмент → JSON-RPC `-32601/-32602`; битый JSON → `-32700`;
  невалидный request → `-32600`; ошибки валидации ВВОДА инструмента → `isError:true` (не protocol error).
  GET `/mcp` без server→client стрима → 405 (v1 stateless, SSE вне scope). Версия — `2025-11-25` (ADR-002).

## Поток MCP-вызова (end-to-end)
TLS 1.3 → bodyLimit → recover → Host/Origin (403 при present&invalid, AC12) → auth: Bearer→`store.Verify`
(401 нет/неизвестен, 403 ErrCorrupt, AC2/AC8) кладёт fingerprint в ctx → rate-limit (429) →
authSuccessAudit (транспортный success-аудит соединения) → `mux` → `/mcp` (StreamableHTTPHandler) →
SDK диспетчеризует `initialize`(AC3)/`tools.list`(AC4)/`tools.call`(AC5/AC6) → `withAudit` пишет
MCP-аудит-запись (fingerprint из ctx + `tool=<имя>` + success/fail через расширенный `writeAudit`) →
JSON-RPC-ответ. Bearer уживается с MCP: auth — на транспорте (ДО MCP), MCP про auth не знает, сессия
НЕ используется для auth (ADR-003).

## Привязка к AC
| AC | Модуль/контракт |
|---|---|
| AC1 | `mcp.NewHandler`→`NewStreamableHTTPHandler`; POST→JSON, GET→405 |
| AC2/AC8/AC12 | существующие `authMiddleware`/`hostOriginMiddleware` ДО `/mcp` (не переписываются) |
| AC3 | `mcp.NewServer` `Implementation{Name:"raxd",Version}`, capability `tools` |
| AC4 | `tools.go`: регистрация `ping`+`server_info`, `inputSchema` непустой |
| AC5 | `pingHandler` → `pong`, без побочных эффектов |
| AC6/AC10 | `serverInfoHandler` `ServerInfo` без секретов; `withAudit`/audit без тела ключа |
| AC7 | SDK JSON-RPC ошибки `-32700/-32600/-32601/-32602`; input-ошибки → `isError` |
| AC9 | `withAudit` + расширенные `AuditRecord.Tool`/`writeAudit` (fingerprint+tool+результат в success) |
| AC11 | `cli/serve.go` собирает handler и передаёт в `server.New`; тот же порт/TLS |
| AC13 | `internal/mcp/server.go` — точка регистрации новых tools (`AddTool`) |
| AC14 | тесты `-mod=vendor` в Docker (qa) |
| AC15 | docs (tech-writer): URL `/mcp`, Bearer, self-signed |

## Trade-offs
- Выбрали **официальный SDK** вместо **stdlib JSON-RPC** (research §F): цена — +ветка зависимостей в
  `vendor/` (`go-sdk/mcp` + `google/jsonschema-go` + `yosida95/uritemplate/v3`) и завязка на темп
  релизов SDK; взамен — spec-compliance/клиентская совместимость «из коробки», типизированный `AddTool`,
  меньше ручного кода для AC3/AC4/AC7, прямой путь к `command-exec`/`file-upload` (AC13).
- Расширили **`server.New` доп. параметром** `mcpHandler` вместо отдельного метода-сеттера: цена —
  правка сигнатуры и всех вызовов `New` (тесты `tls-transport`); взамен — MCP за той же цепочкой без
  дублирования middleware и без второго пути auth (ADR-003).
- Расширили **`AuditRecord`/`writeAudit`** новым полем `Tool` вместо отдельного MCP-only логгера: цена —
  ломающая правка контракта транспортного аудита + правка тестов аудита tls-transport; взамен — единый
  формат и канал аудита, имя инструмента попадает в лог во ВСЕХ ветках включая success (AC9 выполним),
  без дублирующей инфраструктуры логирования. Отвергнут отдельный success-audit-механизм (две системы аудита).
- **Stateless, GET→405, без `MCP-Session-Id`** в v1 (research B): цена — нет server→client стримов
  (вне scope); взамен — простота. OQ-2 (нужна ли сессия конкретному клиенту) проверяет qa.
- Зависимость **`github.com/modelcontextprotocol/go-sdk`** — УЖЕ в STACK.ru.md и учтена ADR-002;
  developer выполняет `go mod vendor` на ХОСТЕ + коммит `vendor/`/`go.mod`/`go.sum` (offline, AC14).
  Точный состав `vendor/` подтверждается прогоном (OQ-1). Go 1.25 проекта удовлетворяет SDK — не блокер.

## Тестируемость в Docker (для qa)
Сборка/тесты `-mod=vendor` в Docker (baseline §6/AC14), без `go mod download`; `httptest.NewServer` с
полной цепочкой + `/mcp`. Покрыть AC3–AC7 (initialize/tools.list/ping/server_info/JSON-RPC-ошибки),
AC2/AC8/AC12 (auth/Origin до MCP), AC9 (лог success-вызова содержит fingerprint+`tool=`+результат), AC10
(предъявленный ключ НЕ встречается подстрокой в логе и теле MCP-ответов). Детализацию определяет qa.

## Точки расширения
`command-exec`/`file-upload` добавляют `execute_command`/`upload_file` тем же `mcp.AddTool` в
`internal/mcp` (новые файлы tools), оборачивая их `withAudit`; `NewHandler`/`server.New`/маршрут `/mcp`
и middleware-цепочка не меняются. Resources/Prompts (Q4) — отдельная задача через ту же capability-негоциацию.
