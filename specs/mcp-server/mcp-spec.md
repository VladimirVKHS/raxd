# MCP Spec: MCP-сервер raxd поверх готового HTTP/TLS-транспорта

Автор спецификации: mcp-engineer (raxd). Автор продукта: **Vladimir Kovalev, OEM TECH**.
Вход: `spec.md` (AC1–AC15), `plan.md` (architect), `research.md` + ADR-001/002/003, `security-requirements.md`
(SR-27…SR-39), `.claude/reference/MCP-INTEGRATION.ru.md`, `.claude/reference/STACK.ru.md`, готовый код
`internal/server/*` (`tls-transport`) и `internal/version`.

> Этот документ — **контракт дизайна** для developer (реализует `internal/mcp/*` на Go SDK) и для
> qa/reviewer (проверяют соответствие). Здесь только дизайн и схемы (JSON) — БЕЗ Go-реализации.
> Тела хендлеров пишет developer; формы вход/выход и поток вызова фиксируются здесь.
>
> **Что отдаём ИИ-агенту:** проверяемый рабочий MCP-канал к `raxd` (initialize → tools/list →
> tools/call) с двумя read-only инструментами (`ping`, `server_info`) поверх уже аутентифицированного
> TLS-транспорта — чтобы агент мог убедиться, что канал жив, и узнать версию демона, не задевая хост и
> не завися от ещё не реализованных `command-exec`/`file-upload`.

---

## 0. Версии (spec/SDK) — фиксируются вверху

- **Версия спецификации MCP:** `2025-11-25` (последняя датированная ревизия; ADR-002; research §A).
- **SDK:** `github.com/modelcontextprotocol/go-sdk/mcp` — официальный, **v1.6.0** (ADR-001; research §D).
  Community-вариант `mark3labs/mcp-go` НЕ используется (MCP-INTEGRATION предписывает официальный SDK).
- **Транспорт SDK:** `mcp.NewStreamableHTTPHandler(...)` → `http.Handler` (монтируется в готовый mux).
- **Согласование версии:** в `initialize` сервер отвечает `protocolVersion:"2025-11-25"`, если клиент
  прислал её же; если клиент прислал другую — сервер отвечает поддерживаемой (SHOULD — самой свежей),
  это поведение обеспечивает SDK. После `initialize` клиент шлёт заголовок `MCP-Protocol-Version`;
  невалидную/неподдерживаемую версию SDK отклоняет **400** (research §A; ADR-002).

---

## 1. Транспорт

- **Тип:** **Streamable HTTP поверх TLS 1.3** (НЕ stdio — `raxd` обслуживает удалённых сетевых
  MCP-клиентов; MCP-INTEGRATION §«Транспорт», STACK).
- **Эндпоинт (единый, маршрут Q2):** `https://127.0.0.1:<port>/mcp` — один путь под POST и GET (спека
  требует единый endpoint path; research §B). Порт и TLS — те же, что у `raxd serve` (AC11/SR-29);
  отдельного процесса/порта/слушающего сокета для MCP НЕТ.
- **Реализация:** `internal/mcp.NewHandler(...)` строит `*mcp.Server` и возвращает `http.Handler` от
  `mcp.NewStreamableHTTPHandler(...)`. Этот handler заменяет 501-заглушку `dispatchHandler` на
  маршруте `/mcp` ВНУТРИ той же middleware-цепочки транспорта (plan «Modules»). Транспорт НЕ
  переписывается — только потребляется (CLAUDE.md red line, spec Out of Scope).

### 1.1. HTTP-семантика (что должен делать developer/проверять qa)

| Метод | Условие | Поведение | Источник |
|---|---|---|---|
| **POST** | тело — JSON-RPC *request* (`initialize`/`tools/list`/`tools/call`) | ответ `Content-Type: application/json`, один JSON-объект (для `ping`/`server_info` SSE не нужен) | research §B |
| **POST** | тело — JSON-RPC *notification*/*response* (напр. `notifications/initialized`) | **202 Accepted** без тела | research §B |
| **POST** | тело не принято/битое на уровне HTTP | HTTP-ошибка (напр. **400**), тело МОЖЕТ быть JSON-RPC-error без `id` | research §B |
| **GET** | `Accept: text/event-stream` (попытка открыть server→client SSE) | **405 Method Not Allowed** (v1 stateless, SSE вне scope — см. 1.3) | research §B; plan §Contracts |
| любой | невалидная/неподдерживаемая `MCP-Protocol-Version` (после initialize) | **400 Bad Request** | research §A; ADR-002 |

- **POST Accept:** клиент ОБЯЗАН слать `Accept: application/json, text/event-stream` (оба типа); это
  забота клиента. Сервер на JSON-RPC *request* отвечает `application/json` (research §B).
- **Заголовки версии:** `MCP-Protocol-Version: 2025-11-25` клиент шлёт на запросах ПОСЛЕ `initialize`.
  Обработку заголовка и согласование версии обеспечивает SDK (research §A; ADR-002).

### 1.2. Аутентификация наследуется от транспорта (НЕ от MCP-сессии)

- **Bearer-аутентификация выполняется транспортным `authMiddleware` ДО `/mcp`-handler** (SR-27/SR-28):
  токен из `Authorization: Bearer rax_live_…` → `keystore.Verify` (constant-time) → fingerprint
  кладётся в `context` запроса. MCP-слой про auth НЕ знает, своего канала аутентификации НЕ вводит,
  `keystore.Verify` сам НЕ вызывает.
- **MCP-сессия (`MCP-Session-Id`) НЕ используется для аутентификации** (best-practice «MUST NOT use
  sessions for authentication»; SR-28; research §G). В v1 сервер `MCP-Session-Id` не выдаёт (stateless;
  см. 1.3), идентичность — только транспортный fingerprint.
- **Origin/Host-валидация наследуется** от транспортного `hostOriginMiddleware` ДО `/mcp` (SR-32;
  ADR-003): `Origin` present И вне allowlist → **403**; `Origin` отсутствует → пропуск (curl/SDK-клиенты
  Origin не шлют); Host вне allowlist (`localhost`/`127.0.0.1`/`::1`) → **403**. MCP-слой это НЕ
  переопределяет.

### 1.3. Выбор по GET: 405 (SSE НЕ предлагаем) — зафиксировано

**Решение: GET `/mcp` → 405 Method Not Allowed.** Сервер в v1 работает **stateless**, server→client
SSE-стримов не открывает, `MCP-Session-Id` не выдаёт.

- Обоснование: спека разрешает на GET вернуть ЛИБО `text/event-stream`, ЛИБО **405** (= «SSE на этом
  эндпоинте не предлагаю»); для `ping`/`server_info` сервер-инициированные сообщения не нужны
  (research §B; plan §Contracts; ADR research «Замечания на решение architect»).
- Цена: нет server→client уведомлений (вне scope v1). Достаточность stateless для целевых клиентов
  (Claude Desktop / Inspector / Claude Code) проверяет qa (research OQ-2 — деталь реализации, не блокер).

---

## 2. Аутентификация и поток вызова (end-to-end)

Каждый MCP-вызов проходит цепочку **аутентификация → Origin/Host → rate-limit → аудит → исполнение
инструмента → аудит результата**. Первые звенья — наследуемый транспорт (`tls-transport`, НЕ
переписывается), последние — MCP-слой (`internal/mcp`).

```
TLS 1.3 (внешний слой, MinVersion TLS13)
  └─ bodyLimit            (лимит тела запроса; SR-25 насл.)
     └─ recover           (паника → 500; SR-25 насл.)
        └─ Host/Origin    (present&invalid Origin → 403; Host вне allowlist → 403; SR-32/AC12)
           └─ auth        (Bearer → keystore.Verify; нет/неизвестен/отозван → 401; ErrCorrupt → 403;
              │             success → fingerprint в ctx; SR-27/AC2/AC8)
              └─ rate-limit (per-key + per-IP; превышение → 429; SR-17 насл.)
                 └─ authSuccessAudit (транспортный success-аудит СОЕДИНЕНИЯ; одна запись; SR-19 насл.)
                    └─ mux → /mcp  (StreamableHTTPHandler от SDK)
                       └─ SDK диспетчеризует JSON-RPC по method:
                          ├─ initialize        → capabilities/serverInfo (AC3)
                          ├─ notifications/initialized → 202 (нотификация)
                          ├─ tools/list        → [ping, server_info] (AC4)
                          └─ tools/call        → диспетч инструмента под withAudit:
                                ├─ withAudit достаёт fingerprint = server.FingerprintFromContext(ctx)
                                ├─ вызывает tool-хендлер (ping / server_info)
                                ├─ пишет MCP-аудит: AuditRecord{Tool:<имя>, Fingerprint, Result,
                                │   RemoteAddr, Reason} → AuditFn → writeAudit (tool=<имя>+result; AC9)
                                └─ возвращает CallToolResult (или isError:true) → JSON-RPC-ответ
```

### 2.1. Где какой отказ (карта кодов; расширяет таблицу `tls-transport`)

| Условие | Код / форма | Слой | SR |
|---|---|---|---|
| Нет `Authorization: Bearer` / неизвестный / отозванный ключ | **401** | transport auth | SR-27 (насл. SR-9) |
| Повреждение `keys.db` в рантайме (`ErrCorrupt`) | **403** | transport auth | SR-27 (насл. SR-13) |
| `Origin` present И вне allowlist; Host вне allowlist | **403** | transport host/origin | SR-32 (насл. SR-16) |
| Превышение rate-limit (per-key/per-IP) | **429** | transport rate-limit | насл. SR-17 |
| Битый JSON / невалидный JSON-RPC request | **JSON-RPC −32700 / −32600** | MCP (SDK) | SR-30 |
| Неизвестный метод / неизвестный инструмент / неверные параметры | **JSON-RPC −32601 / −32602** | MCP (SDK) | SR-30, SR-37 |
| Ошибка валидации ВВОДА инструмента | **`isError:true`** в `CallToolResult` (НЕ protocol error) | MCP tool | SR-30 |
| GET `/mcp` (попытка SSE) | **405** | MCP (SDK) | SR-30 |
| Невалидная/неподдерживаемая `MCP-Protocol-Version` | **400** | MCP (SDK) | SR-30 |

**Важно (SR-27/SR-28/SR-37):** при любом отказе транспортного звена (401/403/429) запрос **НЕ
доходит** до SDK-диспетчера — инструмент НЕ вызывается, аудит-DENY/FAIL/RATE пишет транспорт.
Неизвестный инструмент (`execute_command`, `upload_file` в v1) → JSON-RPC-ошибка, НЕ исполнение.

### 2.2. Аудит каждого вызова (без секретов)

- `withAudit` (`internal/mcp/audit.go`) оборачивает КАЖДЫЙ tool-хендлер. После вызова формирует
  `AuditRecord{TS, Fingerprint: server.FingerprintFromContext(ctx), Tool: <имя инструмента>,
  Result: "success"|"fail", RemoteAddr, Reason}` и зовёт инжектированный `AuditFn` (SR-35/AC9).
- **Fingerprint берётся из ctx (НЕ из тела ключа).** MCP-слою тело ключа недоступно в принципе:
  `authMiddleware` кладёт в ctx только `keystore.Fingerprint` (необратимый, 12 hex), экспонируется
  новой обёрткой `server.FingerprintFromContext(ctx)` (`"-"` если ключа нет). SR-34: ни в `content`/
  `structuredContent`, ни в JSON-RPC `error.message`/`data`, ни в аудит-записи НЕТ полного ключа, его
  хэша, соли, raw `Authorization` и приватного TLS-ключа (проверка подстрокой; AC10).
- **Формат аудит-записи MCP-успеха** (расширение транспортного аудита; plan/SR-36):
  `INFO MCP fp=<fp> remote=<ip> tool=ping result=ok`. Поле `Tool` логируется ТОЛЬКО при `Tool != ""`,
  поэтому существующие connection-записи (`AUTH`/`FAIL`/`DENY`/`RATE`) формат не меняют.

---

## 3. initialize / capabilities

### 3.1. Что объявляет сервер

- **serverInfo:** `name: "raxd"`, `version: <internal/version.Version>` (через
  `mcp.Implementation{Name:"raxd", Version: version.Version}`; AC3/SR-31). При сборке без ldflags
  `version.Version == "dev"` — это валидно (отдаём как есть, не секрет).
- **capabilities:** объявляется **ТОЛЬКО `tools`**. Resources, Prompts, Sampling, Logging,
  Completions — **НЕ объявляются** (Q4 — отсрочка валидна по спеке: объявляешь только поддерживаемое;
  research §C/§D, ADR research §Q4; SR-31). `tools.listChanged` — на усмотрение SDK; в v1 список
  инструментов статичен.
- **protocolVersion:** `2025-11-25` (ADR-002).

### 3.2. Пример обмена `initialize` (JSON-RPC)

> Транспортно это POST на `https://127.0.0.1:<port>/mcp` с заголовками `Authorization: Bearer
> rax_live_…`, `Content-Type: application/json`, `Accept: application/json, text/event-stream`.
> Точная форма `result` (поля capabilities) формируется SDK; ниже — ожидаемая структура для проверки.

**Запрос (client → server):**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "2025-11-25",
    "capabilities": {},
    "clientInfo": { "name": "mcp-inspector", "version": "0.x" }
  }
}
```

**Ответ (server → client):**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2025-11-25",
    "capabilities": {
      "tools": { "listChanged": false }
    },
    "serverInfo": {
      "name": "raxd",
      "version": "1.0.0"
    }
  }
}
```

**Нотификация после initialize (client → server):**
```json
{ "jsonrpc": "2.0", "method": "notifications/initialized" }
```
→ сервер отвечает **HTTP 202 Accepted** без тела (это notification, не request; research §B).

---

## 4. tools/list

`tools/list` возвращает **РОВНО** два инструмента — `ping` и `server_info` (AC4/SR-31). Сам по себе
`tools/list` — встроенный механизм обнаружения возможностей (capability discovery): агент узнаёт
доступные действия из списка, не зная заранее имён. `inputSchema` у обоих НЕ `null` и НЕ пустой —
`{"type":"object","additionalProperties":false}` (инструменты без параметров; research §C).

**Запрос:**
```json
{ "jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {} }
```

**Ответ (ожидаемая структура; точную форму генерирует SDK из типов вход/выход):**
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "tools": [
      {
        "name": "ping",
        "description": "Проверка живости MCP-канала к raxd. Возвращает \"pong\". Без побочных эффектов на хосте.",
        "inputSchema": { "type": "object", "additionalProperties": false }
      },
      {
        "name": "server_info",
        "description": "Версия и базовые сведения о демоне raxd (без секретов): имя продукта, версия, версия протокола MCP.",
        "inputSchema": { "type": "object", "additionalProperties": false },
        "outputSchema": {
          "type": "object",
          "additionalProperties": false,
          "required": ["name", "version", "protocolVersion"],
          "properties": {
            "name": { "type": "string" },
            "version": { "type": "string" },
            "protocolVersion": { "type": "string" }
          }
        }
      }
    ]
  }
}
```

> Примечание: `outputSchema` для `ping` НЕ объявляется (выход — только text-блок `pong`, без
> структурированного результата). Для `server_info` `outputSchema` объявляется для строгой валидации
> `structuredContent` (research «на усмотрение» → принято: объявляем, т.к. результат структурный).

---

## 5. Инструменты (схемы вход/выход)

Принципы дизайна (agent-native, mcp-tool-design): оба инструмента — **примитивы read-only**, без
бизнес-логики; descriptive-имена описывают возможность; вход — данные (пустой объект), не решения;
выход — rich (достаточно агенту, чтобы убедиться в живости/версии); ошибки — через `isError`. Имена и
описания — на русском (язык артефактов), но `name` инструмента — латиница (стабильный идентификатор для
клиентов).

> **Замечание про SDK-генерацию схем.** В Go SDK схемы вход/выход выводятся типизированно из Go-структур
> (`AddTool[In, Out]`): пустой вход → `InputSchema = {"type":"object","additionalProperties":false}`;
> структура выхода → `OutputSchema` + дублирующий text-блок. Ниже зафиксированы ЦЕЛЕВЫЕ JSON-схемы как
> контракт; точную сериализацию даёт SDK, developer обязан добиться именно этих форм (qa проверяет).

### 5.1. tool `ping`

- **Описание (1-2 строки):** Проверка живости MCP-канала к `raxd`. Возвращает `pong`. Без побочных
  эффектов на хосте (никаких команд, файлов, сети — AC5/SR-31).
- **Назначение для агента:** убедиться, что MCP-цикл (transport → auth → SDK → tool) рабочий
  end-to-end.

**Входная схема (JSON):**
```json
{ "type": "object", "additionalProperties": false, "properties": {} }
```
> Вход пустой. `echo` НЕ добавляется в v1 (см. Открытые вопросы Q-MCP-1; дефолт — без параметров,
> минимально и достаточно для AC5). `additionalProperties:false` отвергает любой неожиданный ввод.

**Выходная схема (JSON) — форма `result` для `tools/call`:**
```json
{
  "content": [ { "type": "text", "text": "pong" } ],
  "isError": false
}
```
> `outputSchema` не объявляется (нет `structuredContent`). Выход — фиксированный text-блок `pong`.

**Ошибки:**
- Транспортные (до tool, см. 2.1): 401 / 403 / 429 — инструмент НЕ вызывается.
- Лишний/некорректный ввод (нарушает `additionalProperties:false`) → ошибка валидации ВВОДА →
  `isError:true` (Tool Execution Error, спека 2025-11-25; SR-30). НЕ protocol error.
- Внутренний сбой (теоретический; `ping` не имеет I/O) → `isError:true` с нейтральным сообщением
  (без секретов; SR-34). Паника невозможна по контракту; если случится — наследуемый
  `recoverMiddleware` отдаёт 500, сервер жив (SR-30).

### 5.2. tool `server_info`

- **Описание (1-2 строки):** Версия и базовые сведения о демоне `raxd` без секретов. Закрывает
  потребность в `version` как поле результата (отдельный тривиальный tool не плодим — spec §Требуемые
  инструменты).
- **Назначение для агента:** узнать, с какой версией `raxd` и версией протокола MCP он работает.

**Входная схема (JSON):**
```json
{ "type": "object", "additionalProperties": false, "properties": {} }
```
> Вход пустой.

**Выходная схема (JSON) — `structuredContent` + дублирующий text-блок:**
```json
{
  "content": [
    { "type": "text", "text": "raxd 1.0.0 (MCP 2025-11-25)" }
  ],
  "structuredContent": {
    "name": "raxd",
    "version": "1.0.0",
    "protocolVersion": "2025-11-25"
  },
  "isError": false
}
```

**`outputSchema` (объявляется в `tools/list`):**
```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["name", "version", "protocolVersion"],
  "properties": {
    "name": { "type": "string", "description": "Название продукта — всегда \"raxd\"" },
    "version": { "type": "string", "description": "Версия из internal/version.Version" },
    "protocolVersion": { "type": "string", "description": "Версия протокола MCP — \"2025-11-25\"" }
  }
}
```

**Какие поля БЕЗОПАСНЫ к отдаче (SR-33/SR-34, AC6/AC10) — исчерпывающий список:**

| Поле | Значение | Источник | Почему безопасно |
|---|---|---|---|
| `name` | `"raxd"` | константа | публичное имя продукта |
| `version` | напр. `"1.0.0"` / `"dev"` | `internal/version.Version` | build-метаданные, не секрет |
| `protocolVersion` | `"2025-11-25"` | константа протокола | публичная версия спеки |

**Что ЗАПРЕЩЕНО включать (SR-33/SR-34) — выход формируется ТОЛЬКО из переданной версии и констант,
БЕЗ чтения секретов/конфига/окружения:**

- тела API-ключей, их хэши, соль, fingerprint чужих ключей;
- приватный TLS-ключ, его путь, содержимое серта;
- путь к `keys.db` / `config.yaml` / TLS-директории, любые пути ФС;
- порт прослушивания, bind-адрес, host/origin allowlist, rate-limit-параметры;
- переменные окружения, hostname машины, версия ОС, аптайм, PID, число ключей.

> Поля `commit`/`date` из `internal/version` (build-метаданные) сами по себе НЕ секреты, но в v1 НЕ
> включаются: минимальность результата + отсутствие необходимости для AC6. См. Открытые вопросы
> Q-MCP-2. Если решат добавить — допустимо (build-метаданные), но строго без путей/окружения.

**Ошибки:** идентично `ping` (см. 5.1): транспортные до tool (401/403/429), лишний ввод → `isError`,
внутренний сбой → `isError` без секретов. `server_info` не делает I/O и не читает секреты, поэтому
fail-ветка практически недостижима, но контракт ошибки фиксируется для единообразия.

---

## 6. tools/call — поток end-to-end (пример `ping`)

Полный путь см. §2. Здесь — JSON-RPC обмен на уровне MCP (после того как транспорт пропустил запрос).

**Запрос (client → server):**
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": { "name": "ping", "arguments": {} }
}
```

**Что происходит:**
1. Транспорт уже выполнил auth/Origin/rate-limit (§2); fingerprint в ctx; запрос дошёл до `/mcp`.
2. SDK диспетчеризует `tools/call` → инструмент `ping` (обёрнут `withAudit`).
3. `withAudit` достаёт `fingerprint = server.FingerprintFromContext(ctx)`, вызывает `pingHandler`.
4. `pingHandler` возвращает `CallToolResult{Content:[{type:"text",text:"pong"}], IsError:false}`.
5. `withAudit` пишет `AuditRecord{Tool:"ping", Fingerprint:<fp>, Result:"success", RemoteAddr:<ip>}`
   → `AuditFn` → `writeAudit` → лог-запись `INFO MCP fp=<fp> remote=<ip> tool=ping result=ok` (AC9).
6. SDK сериализует JSON-RPC-ответ.

**Ответ (server → client):**
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "content": [ { "type": "text", "text": "pong" } ],
    "isError": false
  }
}
```

**Пример `tools/call server_info` (ответ):**
```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "result": {
    "content": [ { "type": "text", "text": "raxd 1.0.0 (MCP 2025-11-25)" } ],
    "structuredContent": { "name": "raxd", "version": "1.0.0", "protocolVersion": "2025-11-25" },
    "isError": false
  }
}
```

---

## 7. Ошибки протокола (JSON-RPC) vs ошибки инструмента

Два РАЗНЫХ механизма (research §C; SR-30; AC7). SDK реализует оба — developer не пишет JSON-RPC-коды
руками, но обязан добиться правильного разделения.

### 7.1. Protocol Errors — стандартный JSON-RPC `error` (НЕ транспортный HTTP-код)

| Код | Условие | Пример |
|---|---|---|
| **−32700** Parse error | тело — не валидный JSON | битый JSON в POST |
| **−32600** Invalid Request | JSON валиден, но не валидный JSON-RPC request (нет `method` и т.п.) | `{"jsonrpc":"2.0"}` без `method` |
| **−32601** Method not found | неизвестный JSON-RPC-метод | `method:"foo/bar"` |
| **−32602** Invalid params | неизвестный инструмент в `tools/call` или неверные params | `tools/call name:"execute_command"` |

**Пример: вызов несуществующего инструмента (SR-37 — НЕ исполнение!):**

Запрос:
```json
{ "jsonrpc": "2.0", "id": 5, "method": "tools/call", "params": { "name": "execute_command", "arguments": {} } }
```
Ответ:
```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "error": { "code": -32602, "message": "Unknown tool: execute_command" }
}
```
> Никакая команда НЕ исполняется (в v1 `execute_command`/`upload_file` не зарегистрированы; AC13/SR-37).
> Сервер остаётся работоспособным; после ошибки валидный `ping` снова отдаёт `pong` (SR-30/AC7).

**Пример: битый JSON-RPC:**
```json
{ "jsonrpc": "2.0", "id": null, "error": { "code": -32700, "message": "Parse error" } }
```

### 7.2. Tool Execution Errors — `isError:true` ВНУТРИ `result`

Бизнес-/валидационные ошибки инструмента возвращаются как `result` с `isError:true` (НЕ как protocol
error) — чтобы модель-агент могла самокорректироваться (спека 2025-11-25; research §C; SR-30):

```json
{
  "jsonrpc": "2.0",
  "id": 6,
  "result": {
    "content": [ { "type": "text", "text": "invalid arguments for ping" } ],
    "isError": true
  }
}
```
> Сообщение об ошибке — нейтральное, БЕЗ секретов (SR-34). Ошибка валидации ВВОДА инструмента
> (нарушение `additionalProperties:false`) → именно сюда, НЕ в −32602.

### 7.3. Никогда не 501 и не паника

Транспортная 501-заглушка (`dispatchHandler`) на маршруте `/mcp` ЗАМЕНЕНА MCP-handler'ом — любой
MCP-запрос получает либо корректный JSON-RPC-ответ, либо корректную JSON-RPC-ошибку, но НЕ 501 (AC1/
AC7). Паника tool-хендлера страхуется наследуемым `recoverMiddleware` (500, сервер жив; SR-30).

---

## 8. Resources / Prompts

**None — в v1 НЕ объявляются и НЕ реализуются** (Q4 — отсрочка). Обоснование:

- spec Out of Scope прямо исключает MCP Resources и Prompts из этой итерации; объявление только
  `tools` capability валидно по спеке (research §C/§D; SR-31).
- Потенциальные кандидаты на будущее (НЕ обязательство): resource `status` (состояние демона/аптайм) и
  resource `capabilities` (список доступных инструментов/ограничений) из MCP-INTEGRATION §«Предлагаемый
  набор» — добавляются ОТДЕЛЬНОЙ задачей через ту же capability-негоциацию (объявить `resources` в
  `initialize` + зарегистрировать в `internal/mcp`). При добавлении `status`/`capabilities` — те же
  правила «без секретов» (SR-33/SR-34): аптайм/число ключей в v1 признаны чувствительными и не
  отдаются (см. §5.2).

---

## 9. Параметры подключения для пользователя (для docs/проверки — AC15)

> Это вход для `tech-writer` (AC15) и для qa-проверки. Финальную документацию пишет tech-writer.

- **URL:** `https://127.0.0.1:<port>/mcp` (порт из `raxd config port` / дефолт; тот же, что у `serve`).
- **Заголовок аутентификации:** `Authorization: Bearer rax_live_<...>` (ключ из `raxd key create`,
  показывается один раз). Без него — **401** (запрос не доходит до MCP).
- **Версия протокола:** клиент после `initialize` шлёт `MCP-Protocol-Version: 2025-11-25`.
- **TLS self-signed:** серт самоподписанный → клиент должен либо доверять серту (CA-файл / trust
  store, где поддерживается), либо в dev отключить проверку:
  - `curl` — флаг `-k` (`--insecure`);
  - Node-клиенты (MCP Inspector и т.п.) — `NODE_TLS_REJECT_UNAUTHORIZED=0` (ТОЛЬКО dev, небезопасно;
    research §G, inspector#584).
- **Известные ограничения клиентов (предупредить в docs, НЕ дефект сервера):** часть клиентов/версий
  не пробрасывает кастомные заголовки (`Authorization`) на этапе health/initialize (inspector#879,
  claude-code#29562; research §G). Для гарантированной проверки канала — `curl`/SDK, которые шлют
  заголовок штатно.

### 9.1. Проверка `curl` (smoke-test канала)

```bash
# initialize
curl -k https://127.0.0.1:<port>/mcp \
  -H "Authorization: Bearer rax_live_<...>" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"curl","version":"1"}}}'

# tools/call ping
curl -k https://127.0.0.1:<port>/mcp \
  -H "Authorization: Bearer rax_live_<...>" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "MCP-Protocol-Version: 2025-11-25" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ping","arguments":{}}}'
```

### 9.2. Набросок конфига MCP Inspector / Claude Desktop

> Иллюстрация для tech-writer; точный синтаксис — на стороне клиента и его версии. raxd —
> Streamable-HTTP-сервер, поэтому подключается как remote/HTTP-сервер, НЕ как stdio-команда.

```json
{
  "mcpServers": {
    "raxd": {
      "type": "streamable-http",
      "url": "https://127.0.0.1:<port>/mcp",
      "headers": { "Authorization": "Bearer rax_live_<...>" }
    }
  }
}
```
В dev для self-signed серта Node-клиенту нужно окружение `NODE_TLS_REJECT_UNAUTHORIZED=0`
(с предупреждением о небезопасности) либо доверить серт системно.

---

## 10. Точки расширения (как добавятся `execute_command` / `upload_file`)

Зарегистрированы РОВНО `ping` + `server_info`; `execute_command`/`upload_file` НЕ регистрируются
(SR-37/AC13). Будущие задачи `command-exec`/`file-upload` добавляют инструменты в ТОТ ЖЕ сервер БЕЗ
изменения транспорта, маршрута `/mcp`, middleware-цепочки, `NewHandler`-сигнатуры:

1. **Регистрация:** новый файл инструментов в `internal/mcp` (напр. `exec.go`), типы вход/выход +
   `mcp.AddTool(server, &mcp.Tool{Name:"execute_command", ...}, withAudit("execute_command", handler, audit))`
   в той же точке, где регистрируются `ping`/`server_info`.
2. **Та же auth/Origin/rate-limit:** инструмент сидит за той же наследуемой цепочкой (§2) — отдельной
   аутентификации не вводится (SR-28).
3. **Тот же аудит:** обязательно оборачивается `withAudit` → `tool=execute_command` в логе (AC9; та же
   `AuditRecord.Tool`/`writeAudit`).
4. **Контроли исполнения — обязанность тех задач (НЕ этой):** baseline §3 (exec без shell, таймаут,
   allowlist, рабочая директория, демон не от root) — обязательны к выполнению в `command-exec`/
   `file-upload` (SR-37; threat-model ОР-М2). Здесь — только структурная точка регистрации.
5. **Resources/Prompts (Q4):** добавляются объявлением соответствующей capability в `initialize` +
   регистрацией в `internal/mcp` — отдельной задачей.

> Полнота CRUD (agent-native): в v1 инструменты read-only (`ping`/`server_info` ничего не создают).
> Когда `command-exec`/`file-upload` введут операции с состоянием (выполнить команду, загрузить файл),
> их задачи отвечают за полноту операций (напр. для файлов — upload/list/read/delete) — отмечено как
> требование к будущим задачам, не к этой.

---

## 11. Контракты на код (для developer — без реализации, только формы)

> Дублируют plan §Contracts в терминах MCP-дизайна. Источник истины по сигнатурам Go — `plan.md`.

- `mcp.NewHandler(ver string, audit server.AuditFn) (http.Handler, error)` — строит `*mcp.Server`
  (`Implementation{Name:"raxd", Version: ver}`), объявляет ТОЛЬКО `tools`, регистрирует `ping`/
  `server_info` под `withAudit`, возвращает `http.Handler` от `NewStreamableHTTPHandler`. Ошибка —
  только при невозможности построить сервер/схемы (фатально для `serve`), без паники.
- `pingHandler(...) → CallToolResult{Content:[{type:"text",text:"pong"}], IsError:false}` — без I/O,
  без побочных эффектов (AC5).
- `serverInfoHandler(...) → ServerInfo{Name, Version, ProtocolVersion}` (structuredContent + text),
  без секретов (AC6/SR-33), источник версии — переданный `ver`.
- `withAudit(name, handler, audit)` — оборачивает хендлер, пишет `AuditRecord{Tool:name, Fingerprint
  из ctx, Result, RemoteAddr, Reason}` (AC9/SR-35).
- `server.FingerprintFromContext(ctx) string` — НОВАЯ экспортируемая обёртка над `fingerprintFromCtx`
  (возвращает fingerprint из ctx; `"-"` если ключа нет; тело ключа НЕ экспонируется; SR-35).
- `AuditRecord` — расширить полем `Tool string`; `writeAudit` — логировать `tool=` во всех ветках
  только при `Tool != ""` (msg-label MCP-успеха — `MCP`); не ломает формат не-MCP записей (SR-36).
- `server.New(cfg, paths, store, logger, mcpHandler http.Handler)` — добавлен последний параметр;
  `nil` → поведение как сейчас (501); не nil → `/mcp` за цепочкой ДО catch-all (SR-29/AC11).

---

## 12. Перечень tools (сводка)

| name | тип | описание | вход | выход | ошибки |
|---|---|---|---|---|---|
| `ping` | read-only primitive | проверка живости MCP-канала, без побочных эффектов | `{}` (пусто) | `content:[{text:"pong"}]`, `isError:false` | транспорт 401/403/429; лишний ввод → `isError`; SR-30 |
| `server_info` | read-only primitive | версия+сведения о raxd без секретов | `{}` (пусто) | `structuredContent:{name,version,protocolVersion}` + text; `isError:false` | как `ping`; SR-33/34 «без секретов» |

Приоритет: оба — первая (и единственная в v1) итерация. Набор минимален намеренно — доказывает
протокол + транспорт + аутентификацию + аудит end-to-end без зависимости от невыполненных фич.

---

## Открытые вопросы

- [ ] **Q-MCP-1 (дизайн `ping`, дефолт зафиксирован).** Добавлять ли в `ping` необязательный параметр
  `echo` (вход `{echo?: string}`, выход — эхо вместо `pong`)? **Дефолт: НЕ добавлять** — spec прямо
  говорит «вход пустой, выход pong» (§Требуемые инструменты, AC5); `echo` — лишняя сущность без
  потребности. Оставляю как вопрос на случай, если qa/клиент попросит echo для диагностики latency.
  Решение не блокирует developer (дефолт = без параметров).
- [ ] **Q-MCP-2 (объём `server_info`, дефолт зафиксирован).** Включать ли `commit`/`date` из
  `internal/version` в результат `server_info`? **Дефолт: НЕ включать** (минимальность; AC6 требует
  лишь версию+базовые сведения). Допустимо добавить позже как build-метаданные (НЕ секрет), строго без
  путей/окружения (SR-33). Согласовать с security при добавлении. Не блокирует developer.
- [ ] **Q-MCP-3 (наследовано из research OQ-2, НЕ блокер — проверяет qa).** Достаточно ли stateless
  (GET→405, без `MCP-Session-Id`) для целевых клиентов (Claude Desktop / Inspector / Claude Code)?
  Дефолт дизайна — stateless (§1.3). Если конкретный клиент потребует сессию — пересмотр отдельной
  правкой (SDK умеет сессии); на v1-контракт инструментов не влияет.

> Развилки Q1 (SDK), Q2 (маршрут `/mcp`), Q3 (версия `2025-11-25`), Q4 (Resources/Prompts — отсрочка),
> Q5 (Origin) — РЕШЕНЫ в ADR-001/002/003 и plan; здесь они зафиксированы как принятые, не открыты.
