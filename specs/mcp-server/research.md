# Research: MCP Server — официальный Go SDK vs stdlib, Streamable HTTP, версия протокола

Автор research: research-analyst (raxd). Дата: 2026-05-21. Язык: русский.
Вход: `specs/mcp-server/spec.md`, `.claude/reference/{MCP-INTEGRATION,STACK,SECURITY-BASELINE}.ru.md`,
`specs/tls-transport/{spec,plan,research}.md` + ADR-001 (HTTP/TLS), ADR-002 (Origin/Host),
глобальный `specs/key-management/decisions/ADR-002-vendoring-offline-builds.md`.

> Это рекомендации для **architect** (он выбирает финальную архитектуру) и факты для developer.
> Каждый нетривиальный факт сопровождён URL. Код здесь не пишется. Часть фактов про MCP-спеку и
> SDK уже подтверждена в `specs/tls-transport/research.md` — здесь они переиспользуются и углубляются
> до конкретики вендоринга и протокольных структур.

## Вопросы

- **Q1 (главный, реализуемость):** официальный Go MCP SDK (`github.com/modelcontextprotocol/go-sdk`,
  экспортирует `StreamableHTTPHandler` как `http.Handler`) ПРОТИВ минимальной реализации MCP на stdlib
  (`net/http` + `encoding/json`, JSON-RPC 2.0). Главное ограничение: сборка/тесты — только в Docker,
  где `proxy.golang.org` НЕДОСТУПЕН; проект вендорится (`vendor/` в git, `-mod=vendor`), а
  `go mod vendor` выполняется на ХОСТЕ (ADR-002). Можно ли надёжно завендорить SDK офлайн: размер
  дерева зависимостей, лицензия, стабильность API, CGO. (Спека Q1.)
- **Q2 (маршрут эндпоинта):** делегировано architect. Дефолт PM `/mcp`. Здесь — факт о том, что
  спека требует ЕДИНЫЙ путь под POST+GET.
- **Q3 (версия протокола MCP):** актуальная версия спеки и точная строка `protocolVersion` в
  `initialize`; правило согласования версии. (Спека Q3.)
- **Q4 (Resources/Prompts):** подтвердить отсрочку фактом (что объявление только `tools` capability
  — валидно по спеке).
- **Q5 (Origin для браузерных MCP-клиентов):** что именно требует MCP-спека по Origin и как это
  ложится на готовый Origin/Host-middleware транспорта (ADR-002 tls-transport). (Спека Q5.)
- **Технические факты** для AC1/AC3/AC4/AC5/AC7/AC15: семантика Streamable HTTP (POST/GET, Accept,
  `MCP-Session-Id`, `MCP-Protocol-Version`, коды ответов), структуры initialize/tools/list/tools/call
  JSON-RPC 2.0, совместимость подключения MCP-клиента с self-signed TLS + Bearer.

---

## Найдено (факт → источник URL)

### A. Версия MCP-спеки и согласование версии (Q3)

- **Актуальная (последняя) версия спецификации — `2025-11-25`.** Это последняя датированная ревизия;
  предыдущая — `2025-06-18`. Changelog 2025-11-25 описывает изменения «since the previous revision,
  2025-06-18». → https://modelcontextprotocol.io/specification/2025-11-25/changelog
  - Соответствует ориентиру `MCP-INTEGRATION.ru.md` («ориентир 2025-11-25») и подтверждено в
    `specs/tls-transport/research.md`. Дефолт PM из spec Q3 (`2025-11-25`) — актуален.
- **`protocolVersion` — строка-дата.** В `initialize` клиент шлёт `"protocolVersion": "2025-11-25"`;
  сервер при поддержке этой версии ОБЯЗАН ответить той же строкой; иначе ОБЯЗАН вернуть другую
  поддерживаемую версию (SHOULD — самую свежую). → https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle
- **HTTP-заголовок версии:** при HTTP-транспорте клиент ОБЯЗАН слать `MCP-Protocol-Version:
  <version>` на всех запросах ПОСЛЕ initialize. Если заголовок отсутствует и сервер не может иначе
  определить версию — он SHOULD считать `2025-03-26`. Невалидная/неподдерживаемая версия → сервер
  ОБЯЗАН вернуть `400 Bad Request`. → https://modelcontextprotocol.io/specification/2025-11-25/basic/transports

### B. Streamable HTTP — точная семантика транспорта (Q2, AC1)

- **Единый эндпоинт под POST и GET.** «The server MUST provide a single HTTP endpoint path … that
  supports both POST and GET methods. For example … `https://example.com/mcp`».
  → https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
- **POST (клиент→сервер):** клиент ОБЯЗАН использовать POST для JSON-RPC-сообщений и ОБЯЗАН включать
  `Accept` с ОБОИМИ типами `application/json` И `text/event-stream`. Тело — один JSON-RPC *request*,
  *notification* или *response*. → там же
  - Если на вход пришёл *response*/*notification* и сервер принял — он ОБЯЗАН вернуть **202 Accepted
    без тела**; если не может принять — HTTP-ошибку (например **400**), тело МОЖЕТ быть JSON-RPC-error
    без `id`. → там же
  - Если на вход пришёл *request* — сервер ОБЯЗАН вернуть ЛИБО `Content-Type: text/event-stream`
    (открыть SSE-стрим), ЛИБО `Content-Type: application/json` (один JSON-объект). Клиент ОБЯЗАН
    поддерживать оба случая. → там же
    - **Вывод для нас:** для `ping`/`server_info` достаточно ответа `application/json` (один объект),
      SSE не обязателен; SSE-режим — опционален и нужен для server→client стримов (вне scope v1).
- **GET (сервер→клиент стрим):** клиент МОЖЕТ сделать GET, ОБЯЗАН включить `Accept: text/event-stream`.
  Сервер ОБЯЗАН вернуть ЛИБО `Content-Type: text/event-stream`, ЛИБО **405 Method Not Allowed** (=
  «SSE на этом эндпоинте не предлагаю»). → там же
  - **Вывод для нас:** в v1 без server-инициированных сообщений допустимо отвечать **405** на GET.
- **Управление сессией (`MCP-Session-Id`):** сервер МОЖЕТ присвоить session ID при инициализации,
  вернув заголовок `MCP-Session-Id` в ответе с `InitializeResult`. Если присвоен — клиент ОБЯЗАН
  слать его во всех последующих запросах. Если сервер ТРЕБУЕТ session ID, на запрос без него (кроме
  initialize) он SHOULD вернуть **400**; на завершённую сессию — **404 Not Found** (клиент тогда
  ОБЯЗАН переинициализироваться). DELETE с `MCP-Session-Id` — явное завершение сессии (сервер МОЖЕТ
  ответить **405**, если не разрешает). → https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
  - **Вывод для нас:** session ID — `MAY` (не обязателен). Для минимального ping/server_info можно
    работать stateless (не выдавать `MCP-Session-Id`) — это валидно по спеке.
- **Origin (Security Warning, MUST):** «Servers MUST validate the `Origin` header on all incoming
  connections to prevent DNS rebinding attacks. If the `Origin` header is present and invalid, servers
  MUST respond with HTTP 403 Forbidden»; «When running locally, servers SHOULD bind only to localhost
  (127.0.0.1)»; «Servers SHOULD implement proper authentication for all connections».
  → https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
  - Формулировка «if present and invalid» означает: 403 обязателен при наличии заголовка и
    несовпадении; поведение при ОТСУТСТВИИ Origin спека не предписывает (не-браузерные клиенты Origin
    не шлют). Это в точности поведение готового middleware из `tls-transport` ADR-002.

### C. JSON-RPC 2.0 структуры (AC3/AC4/AC5/AC7)

- **`initialize` (запрос):** `method: "initialize"`, `params: {protocolVersion, capabilities,
  clientInfo:{name,version,...}}`. → https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle
- **`initialize` (ответ):** `result: {protocolVersion, capabilities, serverInfo:{name,version,...},
  instructions?}`. Сервер с инструментами ОБЯЗАН объявить `tools` capability:
  `{"capabilities": {"tools": {"listChanged": true}}}` (`listChanged` — рассылает ли уведомления об
  изменении списка). → https://modelcontextprotocol.io/specification/2025-11-25/server/tools ,
  https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle
  - **Для нас:** в `serverInfo` — `name: "raxd"`, `version: <версия raxd>`. Объявляем ТОЛЬКО `tools`
    capability (без `resources`/`prompts`/`logging`) — это закрывает Q4 (отсрочка валидна).
- **`notifications/initialized` (нотификация клиента):** `{"jsonrpc":"2.0","method":
  "notifications/initialized"}` — клиент шлёт после успешного initialize. → lifecycle (там же)
- **`tools/list` (запрос):** `method: "tools/list"`, `params: {cursor?}` (пагинация опциональна).
  Ответ: `result: {tools: [{name, title?, description, inputSchema, outputSchema?}], nextCursor?}`.
  → https://modelcontextprotocol.io/specification/2025-11-25/server/tools
  - **`inputSchema` ОБЯЗАН быть валидным JSON Schema-объектом (НЕ `null`).** Для инструмента без
    параметров (наш `ping`/`server_info`) рекомендуется
    `{"type":"object","additionalProperties":false}` (явно принимает только пустой объект).
    → там же
- **`tools/call` (запрос):** `method: "tools/call"`, `params: {name, arguments?}`. Ответ:
  `result: {content: [{type:"text", text:"…"}], isError?, structuredContent?}`. Для структурированного
  результата — `structuredContent` (JSON-объект); для обратной совместимости SHOULD дублировать в
  text-блоке. → там же
  - **Для `ping`:** `result.content = [{"type":"text","text":"pong"}]`, `isError:false`.
  - **Для `server_info`:** удобно вернуть `structuredContent` (версия raxd + базовые сведения, БЕЗ
    секретов) + дублирующий text-блок. Можно объявить `outputSchema` для валидации.
- **Ошибки протокола vs ошибки исполнения (AC7):** ДВА механизма.
  1. **Protocol Errors** (стандартный JSON-RPC `error`) — для неизвестного инструмента, неправильного
     запроса (не удовлетворяет схеме `CallToolRequest`), серверных ошибок. Пример: неизвестный tool →
     `{"error":{"code":-32602,"message":"Unknown tool: …"}}`. → там же
  2. **Tool Execution Errors** — внутри `result` с `isError:true` (бизнес-ошибки, валидация ввода).
     Спека 2025-11-25 уточнила: ошибки валидации ВВОДА SHOULD возвращать как Tool Execution Errors
     (`isError:true`), а не как Protocol Errors, чтобы модель могла самокорректироваться.
     → https://modelcontextprotocol.io/specification/2025-11-25/changelog
  - **Для AC7:** несуществующий tool → JSON-RPC `error` (`-32602` или `-32601`); невалидный JSON-RPC →
    стандартная JSON-RPC-ошибка (`-32700` parse error / `-32600` invalid request) — НЕ паника и НЕ 501.
- **Пример ошибки версии (initialize):** `{"error":{"code":-32602,"message":"Unsupported protocol
  version","data":{"supported":[…],"requested":…}}}`. → lifecycle (там же)

### D. Официальный Go SDK — версия, API, стабильность, CGO (Q1)

- **Версия и статус:** `github.com/modelcontextprotocol/go-sdk` — **v1.6.0, опубликован 30.04.2026**;
  стабильный мажор (v1.x), активно сопровождается «in collaboration with Google».
  → https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp ,
  https://github.com/modelcontextprotocol/go-sdk
  - (`STACK.ru.md` указывает «официальный, v1.x» — уточнено до конкретной версии. Подтверждено и в
    `specs/tls-transport/research.md`.)
- **Минимальная версия Go для SDK:** `go.mod` SDK на теге v1.6.0 объявляет `go 1.25.0`, то есть SDK
  требует Go ≥1.25.0. Это требование УЖЕ удовлетворено проектом raxd (см. раздел E и H ниже:
  `go.mod` raxd — `go 1.25.0`, Dockerfile — `golang:1.25`). НЕ блокер сборки.
  → https://raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.6.0/go.mod
- **Streamable HTTP в SDK — это `http.Handler`:** `func NewStreamableHTTPHandler(getServer
  func(*http.Request) *Server, opts *StreamableHTTPOptions) *StreamableHTTPHandler` с методом
  `ServeHTTP(w, req)`. То есть MCP-эндпоинт монтируется в существующий `http.ServeMux` за готовой
  middleware-цепочкой транспорта. → https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp
- **Базовый API сервера:** `mcp.NewServer(impl *Implementation, opts *ServerOptions) *Server`;
  типизированная регистрация инструмента `func AddTool[In, Out any](s *Server, t *Tool,
  h ToolHandlerFor[In, Out])`; `Tool{Name, Title, Description, InputSchema *jsonschema.Schema,
  OutputSchema *jsonschema.Schema}`; `Implementation{Name, Version, ...}`; `CallToolResult{Content,
  …}`. → https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp
  - Совпадает с каркасом из `MCP-INTEGRATION.ru.md` (там пример с `mcp.NewServer`, `mcp.AddTool`).
- **CGO:** в документации SDK CGO не упоминается; модуль — чистый Go (см. дерево зависимостей ниже —
  все элементы pure Go). `CGO_ENABLED=0` из `STACK.ru.md` сохраняется.
  → https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp
- **Лицензия:** Apache-2.0 (новый код) + MIT (legacy) + CC-BY-4.0 (доки) — все permissive, для
  вендоринга/дистрибуции ограничений нет. © «Model Context Protocol a Series of LF Projects, LLC».
  → https://github.com/modelcontextprotocol/go-sdk/blob/v1.6.0/LICENSE

### E. Дерево зависимостей SDK и реальный объём вендоринга офлайн (Q1 — РЕШАЮЩЕЕ)

- **`go.mod` SDK на теге v1.6.0 (`go 1.25.0`)** объявляет 7 прямых + 2 indirect зависимости
  (цитата зафиксирована по тегу v1.6.0, не по ветке `main`):
  ```
  require (
    github.com/golang-jwt/jwt/v5 v5.3.1
    github.com/google/go-cmp v0.7.0
    github.com/google/jsonschema-go v0.4.3
    github.com/segmentio/encoding v0.5.4
    github.com/yosida95/uritemplate/v3 v3.0.2
    golang.org/x/oauth2 v0.35.0
    golang.org/x/tools v0.42.0
  )
  require (
    github.com/segmentio/asm v1.1.3 // indirect
    golang.org/x/sys v0.41.0 // indirect
  )
  ```
  → https://raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.6.0/go.mod
  (зеркало: https://github.com/modelcontextprotocol/go-sdk/blob/v1.6.0/go.mod)
- **КЛЮЧЕВОЙ факт: пакет `mcp` (который мы импортируем) НЕ тянет в импорт-граф большинство этих
  зависимостей.** По списку imports пакета `mcp` его ЕДИНСТВЕННЫЕ внешние (non-stdlib) зависимости —
  `github.com/google/jsonschema-go/jsonschema` и `github.com/yosida95/uritemplate/v3` (+ internal-
  пакеты самого SDK: `jsonrpc`, `auth`, `internal/jsonrpc2`, `internal/json`, `internal/util` и т.п.).
  НЕ импортируются пакетом `mcp`: `golang.org/x/tools`, `github.com/google/go-cmp`,
  `golang.org/x/oauth2`, `github.com/golang-jwt/jwt`, `github.com/segmentio/encoding`.
  → https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp?tab=imports
  - Тяжёлый `golang.org/x/tools`, `go-cmp`, `segmentio/encoding`+`segmentio/asm`, `oauth2`, `jwt`
    нужны SDK для генерации кода/тестов/OAuth-подпакетов (`auth`/`oauthex`), а НЕ для сборки
    нашего бинаря, если мы используем только `mcp` (+ при необходимости `jsonrpc`).
- **Поведение `go mod vendor` подтверждает экономию:** «`go mod vendor` … containing copies of all
  packages needed to build and test packages in the **main module**. Packages that are only imported
  by tests of packages outside the main module are not included.» То есть в `vendor/` попадут только
  пакеты, реально импортируемые нашим кодом и его тестами — НЕ всё дерево go.mod SDK.
  → https://go.dev/ref/mod (раздел «go mod vendor»)
  - **Следствие:** при `import …/go-sdk/mcp` реальное вендорённое дерево ≈ SDK (пакеты `mcp`+
    зависимые internal) + `google/jsonschema-go` + `yosida95/uritemplate/v3`. Это МАЛОЕ дерево, а не
    «7+2 модуля». (Точные пакеты в `vendor/` developer подтвердит фактическим `go mod vendor` на
    хосте — деталь реализации, не блокер, см. OQ-1.)
- **Транзитивные зависимости — все pure Go, без CGO, поддерживают amd64+arm64:**
  - `github.com/google/jsonschema-go/jsonschema` — **zero внешних зависимостей**, только stdlib.
    → https://pkg.go.dev/github.com/google/jsonschema-go/jsonschema?tab=imports
  - `github.com/yosida95/uritemplate/v3` — pure Go, без CGO, лицензия **BSD-3-Clause**; v3.0.2
    стабилен (релиз 2022, изменений мало — зрелый, не заброшенный по смыслу: используется как
    зависимость официального SDK). → https://github.com/yosida95/uritemplate
  - `github.com/segmentio/asm` (если попадёт через `segmentio/encoding`) — pure Go + assembly `.s`
    (НЕ CGO), amd64+arm64, есть generic-fallback и build-tag `purego`; лицензия MIT-0.
    → https://github.com/segmentio/asm . `segmentio/encoding` — pure Go, без CGO, MIT.
    → https://github.com/segmentio/encoding
    - Примечание: `segmentio/encoding`+`asm` входят в импорт-граф `mcp` ТОЛЬКО если SDK использует их
      внутри `internal/json`; по imports пакета `mcp` они в списке внешних НЕ значатся — то есть в
      наш `vendor/` они, скорее всего, не попадут. Подтверждение — фактический `go mod vendor`.
- **Вывод по Q1-реализуемости (офлайн-вендоринг SDK):** SDK вендорится надёжно офлайн. Все элементы —
  permissive-лицензии, pure Go, без CGO, amd64+arm64; реальное вендорённое дерево для пакета `mcp`
  малое (SDK + jsonschema-go + uritemplate/v3), а не всё go.mod-замыкание. Это совместимо с ADR-002
  (`go mod vendor` на хосте → коммит `vendor/` → `-mod=vendor` в Docker, `CGO_ENABLED=0`).

### F. Минимальная stdlib-реализация MCP (Q1, альтернатива)

- **Что минимально нужно для Streamable HTTP на stdlib** (по фактам B/C выше):
  1. Один `http.Handler` на маршрут `/mcp`, различающий POST/GET (GET → 405, если SSE не нужен).
  2. POST: распарсить JSON-RPC 2.0 (`encoding/json`), диспетчеризовать по `method`:
     `initialize`, `notifications/initialized` (→202), `tools/list`, `tools/call`, `ping` (utility).
  3. Сформировать корректные JSON-RPC-ответы и ошибки (`-32700/-32600/-32601/-32602`).
  4. Объявить `protocolVersion: "2025-11-25"`, `capabilities.tools`, `serverInfo{name,version}`.
  5. (Опц.) `MCP-Session-Id` можно НЕ выдавать (stateless валиден); `MCP-Protocol-Version` — читать,
     при невалидной вернуть 400.
  Все факты — спека 2025-11-25 (см. B/C). Ничего из этого не требует внешних зависимостей: JSON-RPC
  2.0 поверх `net/http`+`encoding/json`. → https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
- **Объём:** ориентировочно несколько сотен строк (роутер метода + 4-5 хендлеров + типы запрос/ответ
  + JSON Schema инструментов как литералы). Точная оценка LOC — за architect/developer; источником не
  подтверждается, поэтому как «факт» не выдаётся.
- **Риски ручной реализации:** (а) нужно самим держать соответствие спеке при её эволюции (2025-06-18
  → 2025-11-25 показывает темп изменений → maintenance-нагрузка) → https://modelcontextprotocol.io/specification/2025-11-25/changelog ;
  (б) совместимость с конкретными клиентами (Claude/inspector) надо проверять руками — SDK эту
  совместимость гарантирует как референс-реализация; (в) при подключении `command-exec`/`file-upload`
  и, возможно, Resources/Prompts ручной код придётся расширять, тогда как SDK даёт это «из коробки».
- **Плюс stdlib:** НОЛЬ новых вендорённых зависимостей (полное соответствие духу STACK: минимализм —
  ручной XBG-резолвинг, lipgloss отложен), минимальный `vendor/`-диф, нет завязки на темп релизов SDK.

### G. Совместимость подключения MCP-клиента: self-signed TLS + Bearer (AC15, docs)

- **Self-signed TLS — известная боль Node-клиентов:** MCP Inspector через Streamable HTTP падает на
  self-signed серте с `self-signed certificate in certificate chain` (Node.js по умолчанию отвергает
  self-signed). Issue #584 закрыт; типовой обходной путь — `NODE_TLS_REJECT_UNAUTHORIZED=0` (только
  dev, небезопасно для прода). → https://github.com/modelcontextprotocol/inspector/issues/584
  - **Для docs/AC15:** инструкция подключения должна явно оговорить self-signed: либо доверить серт
    (добавить в trust store / указать CA-файл, где клиент это поддерживает), либо в dev отключить
    проверку (`NODE_TLS_REJECT_UNAUTHORIZED=0` для Node-клиентов), с предупреждением о небезопасности.
- **Передача `Authorization: Bearer` клиентами:** у части клиентов есть известные баги/ограничения с
  кастомными заголовками при установлении MCP-сессии (Inspector #879/#826/#829; Claude Code #29562 —
  `-H` не отправлялся на этапе health/initialize). → https://github.com/modelcontextprotocol/inspector/issues/879 ,
  https://github.com/anthropics/claude-code/issues/29562
  - **Для docs/AC15:** документировать заголовок `Authorization: Bearer rax_live_…`, но предупредить,
    что отдельные клиенты/версии могут не пробрасывать кастомные заголовки на этапе initialize —
    проверять версию клиента; для гарантированного теста использовать `curl`/SDK, которые шлют
    заголовок штатно. (Это ограничения КЛИЕНТОВ, не нашего сервера; сервер требует Bearer по AC2.)
- **Связь с готовым транспортом:** ключ извлекается из `Authorization: Bearer` middleware транспорта
  ДО MCP-обработки (ADR-001 tls-transport, AC2/AC4 mcp-server) — MCP-уровень про auth не знает, что
  согласуется с MCP best-practice «MCP Servers MUST NOT use sessions for authentication» (auth —
  отдельный слой, не MCP-сессия). → https://modelcontextprotocol.io/specification/2025-11-25/basic/security_best_practices

### H. Сверка со STACK и готовым транспортом (контекст для рекомендации)

- **STACK тяготеет к минимуму зависимостей:** `adrg/xdg` отклонён в пользу ручного резолвинга,
  lipgloss v2 отложен, транспорт сделан на stdlib (`net/http`+`crypto/tls`).
  → `.claude/reference/STACK.ru.md` (строки про XDG/lipgloss/TLS)
- **НО STACK ЯВНО включает MCP SDK как выбранную библиотеку:** строка «MCP-сервер →
  `github.com/modelcontextprotocol/go-sdk/mcp` | официальный, v1.x». То есть SDK — уже принятое
  стеком решение, а не новая внеплановая зависимость. → `.claude/reference/STACK.ru.md`
- **Версия Go в проекте УЖЕ удовлетворяет требованию SDK (Go ≥1.25.0):** `go.mod` raxd объявляет
  `go 1.25.0`, а Dockerfile использует базовый образ `golang:1.25`. Значит требование SDK v1.6.0
  (go 1.25.0) выполнено — НЕ блокер. → `go.mod` (строка `go 1.25.0`), `Dockerfile` (`FROM golang:1.25`)
  - **Замечание для дирижёра (рекомендация, не блокер):** упоминание «Go 1.22+» в `STACK.ru.md`
    (строка про `crypto/tls`) — это устаревший/нестрогий ориентир, относящийся к TLS stdlib, а не к
    реальному минимуму проекта. Фактический минимум проекта — Go 1.25 (go.mod + Dockerfile). Стоит
    обновить `STACK.ru.md` до «Go 1.25», чтобы ориентир соответствовал реальности и требованию
    MCP SDK. → `.claude/reference/STACK.ru.md` (строка про `crypto/tls`, «Go 1.22+»)
- **ADR-002 (вендоринг) считал MCP SDK в оценке ~37 зависимостей / ~19 МБ** как часть стека — то есть
  его вендоринг уже заложен в принятую политику. → `specs/key-management/decisions/ADR-002-vendoring-offline-builds.md`
- **ADR-001 (tls-transport) спроектировал точку расширения именно под SDK:** `dispatchHandler` +
  `http.ServeMux` так, чтобы mcp-server подключил `StreamableHTTPHandler` (`http.Handler`) как
  маршрут `/mcp` за той же middleware-цепочкой, без переписывания транспорта.
  → `specs/tls-transport/plan.md` (раздел «Точки расширения»), `specs/tls-transport/decisions/ADR-001-http-tls-over-raw-tcp.md`

---

## Варианты по Q1 (A/B: плюсы/минусы)

> Метод: оба варианта сгенерированы и подвергнуты критике; survivor выносится в рекомендацию.

- **A: Официальный Go MCP SDK (`github.com/modelcontextprotocol/go-sdk/mcp`).**
  - Плюсы: референс-реализация → соответствие спеке 2025-11-25 и совместимость с клиентами «из
    коробки»; `StreamableHTTPHandler` — `http.Handler`, монтируется в готовый mux за существующей
    middleware-цепочкой (ADR-001 спроектирован под это); типизированная регистрация инструментов
    (`AddTool[In,Out]`) и автогенерация JSON Schema — меньше ручного кода и ошибок; прямой путь к
    `command-exec`/`file-upload`/Resources/Prompts без переписывания; **уже выбран STACK.ru.md** и
    учтён в ADR-002. Офлайн-вендоринг РЕАЛИЗУЕМ: permissive-лицензии, pure Go, без CGO, amd64+arm64,
    реальное вендорённое дерево пакета `mcp` малое (SDK + jsonschema-go + uritemplate/v3). Источники:
    https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp ,
    https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp?tab=imports ,
    https://raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.6.0/go.mod ,
    https://go.dev/ref/mod
  - Минусы: +новая ветка зависимостей в `vendor/` (хоть и малая), рост `vendor/`-дифа; завязка на
    темп релизов SDK (при апдейте — `go mod vendor` на хосте + коммит); для минимального ping/
    server_info часть мощи SDK избыточна.
  - **Вывод критики:** survivor. Главный риск (офлайн-вендоринг) ОПРОВЕРГНУТ фактами E. Требование
    SDK по Go ≥1.25.0 удовлетворено (проект уже на Go 1.25, см. H) — НЕ риск.
- **B: Минимальная stdlib-реализация MCP (`net/http` + `encoding/json`, JSON-RPC 2.0).**
  - Плюсы: НОЛЬ новых вендорённых зависимостей (максимальный минимализм, в духе stdlib-транспорта и
    отказа от `adrg/xdg`); минимальный `vendor/`-диф; нет завязки на релизы SDK; полный контроль над
    форматом ответов. Источник по достаточности stdlib: спека 2025-11-25
    https://modelcontextprotocol.io/specification/2025-11-25/basic/transports ,
    https://modelcontextprotocol.io/specification/2025-11-25/server/tools
  - Минусы: ручное соответствие спеке и его поддержка при эволюции (06-18 → 11-25 — заметный темп
    изменений) → https://modelcontextprotocol.io/specification/2025-11-25/changelog ; совместимость с
    Claude/inspector надо валидировать руками (SDK даёт её как референс); ручная JSON Schema и
    JSON-RPC-обработка ошибок (риск багов в edge-cases AC7); расширение под `command-exec`/
    `file-upload`/Resources/Prompts — снова свой код; идёт ВРАЗРЕЗ с уже принятым STACK (SDK там
    выбран явно) → `.claude/reference/STACK.ru.md`.
  - **Вывод критики:** жизнеспособен и дёшев по зависимостям, но противоречит STACK и перекладывает
    spec-compliance/maintenance на нас. Оправдан, ТОЛЬКО если офлайн-вендоринг SDK был бы ненадёжен —
    но он надёжен (факты E). Запасной вариант, не основной.

---

## Рекомендация (для architect, не финальный выбор)

- **Q1 → вариант A (официальный Go MCP SDK).** Главный аргумент: критическое ограничение —
  офлайн-вендоринг — РЕАЛИЗУЕМО и безопасно. Реальное вендорённое дерево пакета `mcp` малое (SDK +
  `jsonschema-go` (zero-deps) + `uritemplate/v3`), всё pure Go без CGO, permissive-лицензии,
  amd64+arm64; `go mod vendor` копирует только импортируемые нашим кодом пакеты, а не всё go.mod-
  замыкание SDK. Это снимает страх «тяжёлого дерева». Требование SDK по версии Go (≥1.25.0)
  удовлетворено: проект уже на Go 1.25 (go.mod + Dockerfile). При этом SDK уже выбран STACK.ru.md,
  учтён в ADR-002, а ADR-001 tls-transport спроектировал точку расширения именно под
  `StreamableHTTPHandler` (`http.Handler`). SDK даёт spec-compliance и клиентскую совместимость «из
  коробки» и прямой путь к `command-exec`/`file-upload`. stdlib (вариант B) остаётся честным
  запасным, если бы вендоринг был ненадёжен — но он надёжен, поэтому отклонять минимализм-аргумент
  здесь оправдано (SDK = принятый стек, а не новая внеплановая зависимость). Финальный выбор и форма
  монтажа — за architect.
- **Q2 → маршрут `/mcp` подтверждён фактом:** спека требует ЕДИНЫЙ путь под POST+GET; пример спеки —
  `…/mcp`. Дефолт PM валиден. → https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
- **Q3 → `protocolVersion: "2025-11-25"`** — актуальная последняя версия; формат — строка-дата;
  сервер отвечает той же строкой при поддержке. Читать `MCP-Protocol-Version` заголовок, при
  невалидном → 400. → https://modelcontextprotocol.io/specification/2025-11-25/changelog ,
  https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle ,
  https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
- **Q4 → отсрочка Resources/Prompts ПОДТВЕРЖДЕНА фактом:** объявление только `tools` capability —
  валидно по спеке (capabilities согласуются, объявляешь лишь поддерживаемое). Resources/Prompts
  добавляются отдельной задачей через ту же capability-негоциацию. → https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle
- **Q5 → Origin для MCP ложится на готовый middleware транспорта (ADR-002 tls-transport).** Спека MCP
  требует Origin-валидацию как MUST (403 при present&invalid; поведение при отсутствии не предписано),
  что в точности совпадает с уже реализованным «лёгким» Origin/Host-middleware (403 при present&
  invalid, пропуск при отсутствии, Host-allowlist localhost/127.0.0.1/::1). Для браузерных
  MCP-клиентов architect/security должен решить, нужен ли явный allowlist Origin (значения origin
  доверенных браузерных клиентов) — но базовый MUST уже покрыт транспортом. Bearer-auth идёт ДО MCP и
  не использует MCP-сессию (best-practice: «MUST NOT use sessions for authentication»). Источники:
  https://modelcontextprotocol.io/specification/2025-11-25/basic/transports ,
  https://modelcontextprotocol.io/specification/2025-11-25/basic/security_best_practices ,
  `specs/tls-transport/decisions/ADR-002-origin-validation-timing.md`

### Технические тезисы для architect/developer (кратко, с фактами)

1. **Маршрут/метод:** один `http.Handler` на `/mcp`, POST для JSON-RPC, GET → 405 (SSE не нужен в
   v1). https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
2. **POST-семантика:** клиент шлёт `Accept: application/json, text/event-stream`; на JSON-RPC
   *request* отвечаем `Content-Type: application/json` (один объект); на *notification*/*response* —
   202 без тела. Там же.
3. **Версия:** `protocolVersion:"2025-11-25"` в initialize-ответе; `serverInfo{name:"raxd",
   version:…}`; объявить только `capabilities.tools{listChanged:…}`. lifecycle + server/tools (URL выше).
4. **Инструменты:** `ping` → `content:[{type:"text",text:"pong"}]`, `isError:false`; `server_info` →
   `structuredContent` (версия+сведения без секретов) + дублирующий text-блок; `inputSchema` =
   `{"type":"object","additionalProperties":false}` (нет параметров). server/tools (URL выше).
5. **Ошибки (AC7):** неизвестный tool / битый JSON-RPC → JSON-RPC `error` (`-32601/-32602/-32700/
   -32600`), НЕ 501/паника; ошибки валидации ВВОДА tool — как `isError:true` (не protocol error).
   server/tools + changelog (URL выше).
6. **Auth/Origin (AC2/AC12):** Bearer-извлечение и Origin/Host-проверка — готовый middleware
   транспорта ДО MCP-обработки; MCP про auth не знает (auth ≠ MCP-сессия).
   security_best_practices + ADR-001/ADR-002 tls-transport.
7. **Если SDK (вариант A):** импортировать `github.com/modelcontextprotocol/go-sdk/mcp`; на хосте
   `go mod vendor` + коммит `vendor/`+`go.sum` (ADR-002); собирать `-mod=vendor`, `CGO_ENABLED=0`.
   Монтаж: `mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(getServer, opts))` за middleware-цепочкой.
   Версия Go проекта (1.25) уже удовлетворяет требованию SDK (go 1.25.0).
   https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp
8. **Если stdlib (вариант B):** границы — НЕ реализуем Resources/Prompts/Sampling/logging/completions
   (вне scope, Q4); session ID (`MCP-Session-Id`) можно НЕ выдавать (stateless валиден); SSE/GET → 405.
9. **Подключение (AC15/docs):** URL `https://127.0.0.1:<port>/mcp`, `Authorization: Bearer
   rax_live_…`; self-signed — доверить серт или (dev) `NODE_TLS_REJECT_UNAUTHORIZED=0` для Node-
   клиентов с предупреждением; учесть баги клиентов с пробросом кастомных заголовков на initialize.
   inspector#584/#879, claude-code#29562 (URL выше).

---

## Открытые вопросы

- **OQ-1 (деталь реализации, НЕ блокер) — точный состав `vendor/` после `go mod vendor` с SDK.**
  По imports пакета `mcp` внешние зависимости — `jsonschema-go` + `uritemplate/v3` (+ internal SDK);
  `segmentio/encoding`/`asm`, `oauth2`, `jwt`, `go-cmp`, `x/tools` в граф `mcp`, по pkg.go.dev,
  НЕ входят. Фактический список пакетов в `vendor/` подтверждается прогоном `go mod vendor` на хосте
  (developer/devops) — источником это точно зафиксировать нельзя до прогона. На выбор Q1 не влияет
  (вендоринг реализуем в любом случае). → https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp?tab=imports

- **OQ-2 (деталь реализации, НЕ блокер) — нужен ли `MCP-Session-Id` для совместимости с целевыми
  клиентами.** Спека делает session ID `MAY` (stateless валиден), но конкретные клиенты (Claude
  Desktop / inspector / Claude Code) могут вести себя по-разному. Если выбран SDK — он сам решает
  сессии; если stdlib — проверить на этапе qa, достаточно ли stateless для целевого клиента. На выбор
  Q1 не влияет. → https://modelcontextprotocol.io/specification/2025-11-25/basic/transports

- **OQ-3 (зрелость зависимости) — `yosida95/uritemplate/v3` (v3.0.2, релиз 2022).** Подтверждено:
  pure Go, BSD-3-Clause, используется официальным SDK как зависимость. Низкая частота коммитов может
  читаться и как «зрелая стабильность», и как «низкая активность»; критичности нет (мы её получаем
  транзитивно через SDK, прямо не вызываем). Помечаю как «давний релиз», не как риск-блокер.
  → https://github.com/yosida95/uritemplate

- Замечания НА РЕШЕНИЕ architect (проектные развилки, не открытые факты):
  - форма ответа на GET `/mcp` (405 vs пустой SSE) — в v1 рекомендуется 405 (server→client не нужен);
  - объявлять ли `MCP-Session-Id` (stateless vs сессии) — связано с OQ-2 и выбором SDK/stdlib;
  - `outputSchema` для `server_info` (объявлять для строгой валидации vs только `content`) — на усмотрение.

- Рекомендация дирижёру (не блокер задачи): обновить `STACK.ru.md` — заменить нестрогий ориентир
  «Go 1.22+» (строка про `crypto/tls`) на фактический минимум проекта «Go 1.25», который совпадает с
  требованием MCP SDK v1.6.0 (go.mod SDK = `go 1.25.0`). → `.claude/reference/STACK.ru.md` ;
  https://raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.6.0/go.mod
