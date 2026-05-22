# MCP Spec: file-upload — MCP-инструмент `upload_file` (безопасная запись файла на хост)

Автор спецификации: mcp-engineer (raxd). Автор продукта: **Vladimir Kovalev, OEM TECH**.
Вход: `spec.md` (AC1–AC20, pm-guardian pass), `plan.md` (architect), `decisions/ADR-001..003`,
`security-requirements.md` (SR-68…SR-82, ОБЯЗАТЕЛЬНЫ), `threat-model.md` (R-U1…R-U13, П-U1/П-U2,
ОР-U1…ОР-U6), `context.md`, `.claude/reference/MCP-INTEGRATION.ru.md`,
`specs/command-exec/mcp-spec.md` (образец стиля, error-mapping, разграничение «поле
`AuditRecord.Result` vs рендер `writeAudit` §2.3.1»), готовый код
`internal/mcp/{server,tools,exec_tool}.go`, `internal/server/{audit,middleware,auth}.go`,
`internal/cli/serve.go`, завендоренный `github.com/modelcontextprotocol/go-sdk/mcp`.

> Этот документ — **контракт дизайна** для developer (реализует `internal/fileupload/*`,
> `internal/mcp/upload_tool.go`, расширения `internal/mcp/server.go`, `internal/server/audit.go`,
> `internal/config/config.go`, `internal/cli/serve.go` на Go SDK) и для qa/reviewer (проверяют
> соответствие). Здесь только дизайн и схемы (JSON) — **БЕЗ Go-реализации**. Тело handler'а и
> пакет-писатель пишет developer; формы вход/выход, поток вызова, error-mapping, точку регистрации и
> представление аудита фиксирует этот документ.
>
> **Что отдаём ИИ-агенту (одно предложение):** новый MCP-инструмент `upload_file`, позволяющий
> аутентифицированному агенту записать ОДИН обычный файл на хост `raxd` по относительному пути внутри
> безопасного upload root (без выхода наружу через `..`/абсолютный путь/симлинк, с лимитом размера, с
> контролируемыми правами, без перезаписи по умолчанию, атомарно и с аудитом каждой загрузки без
> логирования содержимого) и получить структурированный результат (записанный относительный путь,
> размер, флаг перезаписи, итоговый режим) — поверх уже готовых TLS-транспорта и MCP-сервера.

> **Связь с `specs/command-exec/mcp-spec.md` (§10/§9 «Точки расширения»).** Тот документ предсказал
> ровно такую задачу: добавить инструмент в ТОТ ЖЕ сервер БЕЗ изменения транспорта, маршрута `/mcp`,
> middleware-цепочки. Здесь это исполняется — с тем же сознательным отступлением, что у
> `execute_command`: `upload_file` **НЕ** оборачивается generic `withAudit`, а ведёт собственный аудит
> в handler (ADR-004-стиль/SR-78). Это второй (после `execute_command`) инструмент с собственным
> аудит-путём; обосновано ниже (§2.3) и наследовано как «планка command-exec» (spec AC19).

---

## 0. Версии (spec/SDK) — наследуются без изменений

- **Версия спецификации MCP:** `2025-11-25` — БЕЗ изменений (та же, что у `mcp-server`/`command-exec`;
  `internal/mcp/tools.go:protocolVersion`). `upload_file` не меняет `protocolVersion` и не трогает
  согласование версии — это забота SDK и наследуемого `mcp-server`.
- **SDK:** `github.com/modelcontextprotocol/go-sdk/mcp` — официальный, тот же, что зарегистрировал
  `ping`/`server_info`/`execute_command` (MCP-INTEGRATION предписывает официальный SDK; community
  `mark3labs/mcp-go` НЕ используется). Новых SDK/внешних зависимостей file-upload НЕ вводит (SR-82;
  plan §Trade-offs — всё на stdlib `os`+`os.Root`, `path/filepath`, `encoding/base64`, `crypto/rand`,
  `io/fs`, `strconv` + уже вендоренные `charmbracelet/log`/go-sdk).
- **Транспорт SDK:** тот же `mcp.NewStreamableHTTPHandler(...)` → `http.Handler` на маршруте `/mcp`;
  `Stateless:true`, `JSONResponse:true` (как в `internal/mcp/server.go`). Не пересоздаётся.
- **Статус ADR (контекст принятия решений).** Спека опирается на `decisions/ADR-001..003`. ADR-001
  (`os.Root` traversal-safe), ADR-002 (атомарная запись temp→rename + права по fd), ADR-003
  (mode-политика: маска `0777`, запрет setuid/setgid/sticky/world-writable, дефолт `0600`). Содержание
  ADR ратифицировано через security в `threat-model.md` («Решения по зависимостям architect» №1–5,
  П-U1/П-U2 приняты security как владельцем baseline, red line 4). Ниже эти решения трактуются как
  **принятые контрактные**. **Расхождение по строке статуса ADR** (в задаче ADR-003 назван «accepted»,
  в файлах все три ADR помечены `proposed`) вынесено в Открытые вопросы (Q-UPL-1) — на сам контракт
  не влияет, т.к. содержание подтверждено security в threat-model.

---

## 1. Транспорт

- **Тип:** **Streamable HTTP поверх TLS 1.3** (НЕ stdio — `raxd` обслуживает удалённых сетевых
  MCP-клиентов; MCP-INTEGRATION §«Транспорт»). Наследуется от `tls-transport`/`mcp-server` БЕЗ
  изменений (SR-68; наследует SR-1/SR-2/SR-27; spec AC1/AC17).
- **Эндпоинт (единый):** `https://127.0.0.1:<port>/mcp` — тот же путь, порт, TLS и слушающий сокет,
  что у `ping`/`server_info`/`execute_command`. Отдельного не-`/mcp` сетевого эндпоинта записи и
  CLI-подкоманды `raxd upload` НЕТ (SR-68; spec AC1, Out of Scope; threat-model R-U1). Регистрация
  инструмента НЕ добавляет маршрутов и слушающих сокетов.
- **HTTP-семантика:** идентична `command-exec/mcp-spec §1` и `mcp-server §1.1` (POST = JSON-RPC
  request → `application/json`; notification → 202; GET `/mcp` → 405; невалидная
  `MCP-Protocol-Version` → 400). `upload_file` её НЕ меняет — это та же `StreamableHTTPHandler`-обвязка.
  v1 stateless: один файл целиком в теле одного POST-запроса (без server→client SSE-стриминга,
  без chunked/докачиваемой загрузки — spec Out of Scope, AC16; threat-model ОР-U6).
- **Лимит тела запроса (внешняя граница, наследуется):** `bodyLimitMiddleware` оборачивает тело в
  `http.MaxBytesReader(w, r.Body, MaxBodyBytes)` (`internal/server/middleware.go`, дефолт `MaxBodyBytes`
  = 1 MiB). Это **внешний слой ДО** `/mcp`-handler: тело запроса (несущее base64-`content`) больше
  `MaxBodyBytes` → **413** ДО инструмента, файл НЕ создаётся (SR-76; spec AC16). `max_file_bytes`
  (700 KiB) согласован НИЖЕ выводимого из `MaxBodyBytes` потолка одного файла (§ниже / SR-76).
- **Аутентификация/Origin/Host наследуются** транспортными middleware ДО `/mcp`-handler (см. §2):
  upload-слой своего канала аутентификации НЕ вводит и `keystore.Verify` НЕ вызывает (SR-68; наследует
  SR-27/SR-28).

---

## 2. Аутентификация и поток вызова (end-to-end)

Каждый вызов `upload_file` проходит ту же наследуемую цепочку, что и `ping`/`server_info`/
`execute_command`: **лимит тела → recover → Host/Origin → аутентификация → rate-limit → транспортный
аудит соединения → SDK-dispatch → uploadHandler → fileupload.Write → собственный upload-аудит
результата**. Первые звенья — наследуемый транспорт (`tls-transport`/`mcp-server`, НЕ переписывается;
SR-68 наследует SR-27/SR-28/SR-17/SR-18). Последние — новый upload-слой (`internal/mcp/upload_tool.go`
+ `internal/fileupload`). Порядок middleware взят из фактического кода
`internal/server/middleware.go`.

```
TLS 1.3 (внешний слой, MinVersion TLS13; насл. SR-1/SR-2)
  └─ bodyLimit            (MaxBytesReader на MaxBodyBytes ~1 MiB; тело>лимит → 413 ДО handler; насл. SR-24/SR-25; AC16/SR-76)
     └─ recover           (паника tool-хендлера → 500, сервер жив; насл. SR-30; страховка R-U8)
        └─ Host/Origin    (Origin present&invalid → 403; Host вне allowlist → 403; насл. SR-14/SR-16)
           └─ auth        (Bearer → keystore.Verify constant-time; нет/неизвестен/отозван → 401;
              │             ErrCorrupt → 403; success → fingerprint+remote в ctx; SR-68 насл. SR-27/SR-28)
              └─ rate-limit (per-key + per-IP token bucket; превышение → 429 ДО handler; SR-68 насл. SR-17/SR-18)
                 └─ authSuccessAudit (транспортный success-аудит СОЕДИНЕНИЯ; насл. SR-19)
                    └─ mux → /mcp  (StreamableHTTPHandler от SDK; Stateless, JSONResponse)
                       └─ SDK диспетчеризует JSON-RPC по method:
                          ├─ initialize / tools/list / tools/call (прочие методы — как в mcp-server)
                          ├─ неизвестный метод → JSON-RPC −32601 (SDK ErrMethodNotFound; запись не создаётся)
                          └─ tools/call:
                                ├─ неизвестное имя инструмента (напр. "upload") → JSON-RPC −32602
                                │     (SDK jsonrpc.CodeInvalidParams, server.go unknown tool; запись не создаётся)
                                └─ name="upload_file":
                                ├─ [SDK] unmarshal UploadInput + валидация по inputSchema
                                │        (additionalProperties:false, типы, required) → невалидно: isError:true
                                │        (НЕ доходит до uploadHandler; §4 строка #10 «лишнее/битый тип/нет required»)
                                └─ uploadHandler(uplCfg, audit)  [БЕЗ generic withAudit — ADR-004-стиль/SR-78]
                                     ├─ fingerprint = server.FingerprintFromContext(ctx)   (НЕ тело ключа; SR-80)
                                     ├─ remote      = server.RemoteAddrFromContext(ctx)
                                     ├─ root-детекция: os.Geteuid()==0 → отдельная WARN-аудит-запись
                                     │   Result:"warn" КАЖДЫЙ вызов (SR-77); если cfg.DenyRoot → доп. deny (SR-77)
                                     ├─ ВХОДНЫЕ ПРОВЕРКИ ДО ЗАПИСИ (SR-75/SR-73/SR-76):
                                     │   1) ранний фильтр: base64.DecodedLen(len(content)) > MaxFileBytes → deny (без декода)
                                     │   2) base64.StdEncoding.DecodeString(content): CorruptInputError → deny (AC6/SR-75)
                                     │   3) точная len(decoded) > MaxFileBytes → deny (AC7/SR-75)
                                     │   4) режим: пусто → cfg.DefaultMode; иначе fileupload.ParseMode(mode);
                                     │            непарсимый/setuid/setgid/sticky/world-writable → deny (AC9/AC14/SR-73)
                                     │     → любое → isError:true + upload-аудит Result:"deny" (запись НЕ выполнена)
                                     ├─ fileupload.Write(uplCfg, Input{RelPath, Data, Overwrite, Mode}):
                                     │     ├─ os.OpenRoot(uploadRoot)               (traversal-safety; ADR-001/SR-69)
                                     │     ├─ filepath.IsLocal ранний лексич. отказ (абс/.. → ErrTraversal; SR-69)
                                     │     ├─ Root.MkdirAll(dir(rel),0700)          (подкаталоги внутри корня; AC5b/SR-71)
                                     │     ├─ Root.Stat(target): существует+!overwrite → ErrExists (deny; AC8/SR-72)
                                     │     │                      каталог → ErrIsDir (deny; AC14/SR-72)
                                     │     ├─ temp(crypto/rand-имя, O_CREATE|O_EXCL) → (*os.File).Chmod по fd (ADR-002/SR-73)
                                     │     ├─ write(Data) → Sync → Root.Rename(tmp,target) → fsync-dir (атомарно; ADR-002/SR-74)
                                     │     ├─ temp очищается на ЛЮБОЙ ошибке (defer Root.Remove; SR-74)
                                     │     └─ Result{RelPath, Size, Overwritten, Mode}
                                     ├─ маппинг Result→UploadOutput + Content(text-резюме) (§3, §5)
                                     └─ upload-аудит РОВНО один раз основной (success | deny | fail; SR-78),
                                          плюс отдельная "warn"-запись при euid==0 (SR-77):
                                          AuditRecord{Tool:"upload_file", Result, Path, Size, Fingerprint,
                                          RemoteAddr, Reason} → AuditFn → writeAudit (logfmt; ветка isUpload; SR-79)
```

> **Примечание про отмену контекста (обрыв соединения).** Если клиент обрывает HTTP-соединение во
> время записи, наследуемый транспорт отменяет request-context. `fileupload.Write` атомарен (temp →
> Rename): при обрыве/ошибке ДО `Root.Rename` целевой файл НЕ появляется, а temp удаляется `defer
> Root.Remove` (SR-74/AC10). Результат клиенту в этом случае НЕ возвращается (соединения нет), но
> developer ОБЯЗАН обеспечить отсутствие частичного целевого и брошенного temp-файла после возврата
> (тест SR-74: ни частичного, ни temp-файла; ADR-002).

### 2.1. Где какой отказ (карта кодов; расширяет таблицу `command-exec/mcp-spec §2.1`)

| Условие | Код / форма | Слой | SR / AC |
|---|---|---|---|
| Тело запроса > `MaxBodyBytes` (раздутое base64) | **HTTP 413** | transport bodyLimit | SR-76 (насл. SR-24/SR-25) / AC16 |
| Нет `Authorization: Bearer` / неизвестный / отозванный ключ | **401** | transport auth | SR-68 (насл. SR-27) / AC17 |
| Повреждение keystore в рантайме (`ErrCorrupt`) | **403** | transport auth | SR-68 (насл. SR-27) / AC17 |
| `Origin` present И вне allowlist; Host вне allowlist | **403** | transport host/origin | насл. SR-14/SR-16 |
| Превышение rate-limit (per-key/per-IP) | **429** | transport rate-limit | SR-68 (насл. SR-17/SR-18) / AC18 |
| Битый JSON / невалидный JSON-RPC request | **JSON-RPC −32700 / −32600** | MCP (SDK) | насл. SR-30 / AC14 |
| Неизвестный JSON-RPC метод | **JSON-RPC −32601** (SDK `ErrMethodNotFound`) | MCP (SDK) | насл. SR-30 / AC14 |
| Неизвестный инструмент в `tools/call` (напр. `upload` вместо `upload_file`) | **JSON-RPC −32602** (SDK `jsonrpc.CodeInvalidParams`) | MCP (SDK) | насл. SR-30 / AC14 |
| Ошибка валидации ВВОДА `upload_file` (лишнее поле; неверный тип; нет required `path`/`content`) | **`isError:true`** в `CallToolResult` | MCP (SDK, до handler) | SR-68/наследует SR-30 / AC2/AC14 |
| traversal (`..`/абс./симлинк наружу/TOCTOU) | **`isError:true`** + upload-аудит `deny` | fileupload (os.Root) | SR-69 / AC4 |
| цель существует И `overwrite:false`; цель — каталог | **`isError:true`** + upload-аудит `deny` | fileupload (`Root.Stat`) | SR-72 / AC8/AC14 |
| декодированный размер > `max_file_bytes` (ранний фильтр или точная len) | **`isError:true`** + upload-аудит `deny` | uploadHandler до записи | SR-75/SR-76 / AC7 |
| невалидный base64 (`CorruptInputError`) | **`isError:true`** + upload-аудит `deny` | uploadHandler до записи | SR-75 / AC6 |
| невалидный `mode` (непарсим / setuid·setgid·sticky / world-writable) | **`isError:true`** + upload-аудит `deny` | uploadHandler (`ParseMode`) | SR-73 / AC9/AC14 |
| `deny_root:true` И `euid==0` | **`isError:true`** + upload-аудит `warn` + `deny` (две записи) | uploadHandler до записи | SR-77 / AC11 |
| `euid==0` И `deny_root:false` | **НЕ отказ**: файл пишется + отдельная upload-аудит `warn` | uploadHandler | SR-77 / AC11 |
| диск полон / ошибка записи I/O (после старта записи) | **`isError:true`** + upload-аудит `fail` | fileupload | SR-74 / AC14 |
| Паника tool-хендлера (теоретически; писатель не паникует) | **500**, сервер жив | transport recover | насл. SR-30 / AC14 |

**Важно (SR-68):** при любом отказе транспортного звена (413/401/403/429) запрос **НЕ доходит** до
SDK-диспетчера — `upload_file` НЕ вызывается, файл НЕ создаётся, аудит DENY/RATE/413 пишет транспорт.
upload-аудит появляется ТОЛЬКО когда запрос дошёл до handler (§2.3).

### 2.2. Граница «ошибка протокола vs ошибка инструмента» (критично — SR-68, AC2/AC4/AC14)

Подтверждено по завендоренному SDK (как и для `execute_command`, см.
`command-exec/mcp-spec §2.2`; `mcp/server.go` диспетчер `tools/call` и `ToolHandlerFor`):

- **Protocol error (JSON-RPC `error`, НЕ HTTP)** — формирует SDK на уровне диспетчера ДО инструмента.
  Коды РАЗНЫЕ (developer/qa берут код для тестов отсюда — он однозначен):
  - битый JSON → **−32700** (Parse error);
  - не валидный JSON-RPC request → **−32600** (Invalid Request);
  - **неизвестный JSON-RPC метод** → **−32601** (Method not found; SDK `ErrMethodNotFound`);
  - **неизвестный инструмент в `tools/call`** (напр. `upload` вместо `upload_file`) → **−32602**
    (Invalid params; SDK `jsonrpc.CodeInvalidParams`, возвращает `unknown tool %q`).
  Файл НЕ записывается.
- **Tool Execution Error (`isError:true` ВНУТРИ `result`)** — два источника:
  1. **Валидация ВВОДА самим SDK ДО handler:** нарушение `inputSchema` (лишнее поле при
     `additionalProperties:false`; неверный тип; отсутствует required `path`/`content`) → SDK кладёт
     `CallToolResult` с `IsError:true`. Файл НЕ записывается, handler НЕ вызывается → **upload-аудит
     НЕ пишется** для этой ветки (handler до неё не дошёл; запись отсутствует — это нормально, см.
     примечание ниже).
  2. **`error`, возвращённый uploadHandler/fileupload:** обычный `error` (не `*jsonrpc.Error`)
     пакуется в `CallToolResult` с `IsError:true`. Сюда попадают: traversal (`ErrTraversal`),
     существующий файл без overwrite (`ErrExists`), цель-каталог (`ErrIsDir`), превышение
     `max_file_bytes` (`ErrTooLarge`/ранний фильтр), невалидный base64 (`CorruptInputError`),
     невалидный `mode` (`ErrBadMode`), `deny_root`, а также I/O-ошибка/диск полон (`fail`).
- **НЕ ошибка инструмента (deny vs fail — разнесение):**
  - **deny** = отказ ДО или вместо записи по правилу безопасности/политике (traversal, exists,
    isdir, too-large, bad-base64, bad-mode, deny_root) — файл НЕ создаётся по решению контроля.
  - **fail** = ошибка среды при выполнении записи (диск полон, I/O-ошибка) — запись начата, но не
    удалась; temp очищается (SR-74). Это НЕ отказ-по-политике, а сбой.
  Оба → `isError:true`, но в аудите РАЗНОЕ значение `Result` (`deny` vs `fail`) и РАЗНАЯ метка `msg`
  (`DENY` vs `FAIL`; §2.3.1). Писатель НИКОГДА не паникует (SR-68/наследует SR-30; R-U8).

> **Примечание про upload-аудит и SDK-валидацию входа.** Ветка (1) (нарушение `inputSchema`)
> перехватывается SDK ДО `uploadHandler`, поэтому upload-аудит-запись по ней НЕ пишется (handler не
> вызван). Это согласуется с SR-78 («handler пишет запись во всех СВОИХ ветках») — отвержение лишних
> полей/типов на уровне SDK, не handler; соединение зафиксировано транспортным `authSuccessAudit`.
> developer/qa: тест AC2 проверяет `isError:true` + «файл не создан», НЕ наличие upload-аудит-записи
> по этой ветке. Если security потребует фиксировать и эту ветку в upload-аудите — это потребует
> низкоуровневого `ToolHandler` вместо `ToolHandlerFor` (см. Открытые вопросы Q-UPL-2; зеркало
> command-exec Q-EXEC-3).

### 2.3. Аудит вызова — собственный, в uploadHandler (ADR-004-стиль / SR-78 / SR-79)

`upload_file` — второй (после `execute_command`) инструмент с собственным аудит-путём (асимметрия с
`ping`/`server_info`, которые остаются на generic `withAudit`). Причина (наследует ADR-004): generic
`withAudit` (`internal/mcp/audit.go`) пишет запись ПОСЛЕ handler и видит только tool name +
fingerprint + remote + success/fail — он НЕ имеет доступа к типизированным полям `UploadOutput` (путь,
размер, overwritten) и НЕ различает deny внутри `fileupload.Write` (traversal/exists/isdir); оставить
его + писать запись в handler = ДВОЙНАЯ запись. Планка наследуется от command-exec (spec AC19).

Контракт (handler пишет РОВНО одну ОСНОВНУЮ upload-запись за вызов + отдельную `warn`-запись при
euid==0):

- **fingerprint** = `server.FingerprintFromContext(ctx)` (12 hex, необратим; «-» если ключа нет; тело
  ключа upload-слою недоступно — SR-80);
- **remote** = `server.RemoteAddrFromContext(ctx)` (IP:port без DNS);
- **path** = относительный путь как принят/очищен сервером (НЕ абсолютный; SR-79/SR-80);
- **size** = число записанных байт (только в success-ветке; deny/fail — НЕ логируется);
- **СОДЕРЖИМОЕ файла (`content`/декодированные байты) в аудит НЕ пишется НИКОГДА** (SR-78/SR-80;
  R-U12).

#### 2.3.1. Два РАЗНЫХ уровня: значение поля `AuditRecord.Result` vs рендер в logfmt

Строго различать (как и для `execute_command`, см. `command-exec/mcp-spec §2.3.1`; снимает
рассогласование между нормативными таблицами и примерами §6):

- **(а) Значение Go-поля `AuditRecord.Result`, которое выставляет `uploadHandler`** — это ВХОД в
  `writeAudit`. Для upload используются: **`"success"` / `"deny"` / `"fail"` / `"warn"`**. (`"rate-
  limited"` — транспортная ветка, не upload.) `"warn"` — root-предупреждение при euid==0 (отдельная
  запись помимо основной). У upload НЕТ значения «таймаут» (запись файла либо успешна, либо
  deny/fail).
- **(б) Как `writeAudit` (`internal/server/audit.go`) РЕНДЕРИТ это поле в logfmt-строку** — через
  существующий `switch rec.Result` в РАЗНЫЕ метки `msg` и РАЗНЫЙ набор ключей. **ВАЖНО: текущий
  `writeAudit` логирует `tool=` и exec-поля только в ветке `if isExec` (где `isExec := rec.Tool ==
  "execute_command"`); общий `else` поле `tool=` НЕ пишет (сверено с `internal/server/audit.go`
  case "success"/"warn"/"deny"/"fail").** Для upload developer ДОБАВЛЯЕТ в КАЖДЫЙ `case` ОТДЕЛЬНЫЙ
  блок `else if isUpload` (где `isUpload := rec.Tool == "upload_file"`), логирующий `tool=` + `path=`/
  `size=`/`reason=` по ветке (SR-79). Подробное указание для developer — §8 «КРИТИЧНО — рендер
  upload-веток». Не-upload и не-exec записи не меняются:

| `AuditRecord.Result` (поле, вход) | `level` / `msg` (рендер) | Ключ `tool=` | Ключ `result=` | Прочие ключи upload (ветка `else if isUpload`) | Источник |
|---|---|---|---|---|---|
| `"success"` (+ `Tool=="upload_file"`) | `INFO` / `msg=MCP` | **`tool=upload_file`** | **есть: `result=ok`** (буквально «ok») | **`path=`, `size=`** | `audit.go` case "success" (ветка isUpload) |
| `"warn"` (upload, euid==0) | `WARN` / `msg=WARN` | **`tool=upload_file`** | **нет** | `reason=running-as-root`, `path=` (если известен) | `audit.go` case "warn" (ветка isUpload) |
| `"deny"` | `WARN` / `msg=DENY` | **`tool=upload_file`** | **нет** | `reason=`, `path=` (если известен) | `audit.go` case "deny" (ветка isUpload) |
| `"fail"` | `WARN` / `msg=FAIL` | **`tool=upload_file`** | **нет** | `reason=`, `path=` (если известен) | `audit.go` case "fail" (ветка isUpload) |

**Вывод для developer/qa:** в Go-коде `uploadHandler` выставляет
`Result:"success"|"deny"|"fail"|"warn"` + поля `Path`/`Size`; в ЛОГЕ для деления веток смотрят на
**метку `msg`** (`MCP`/`WARN`/`DENY`/`FAIL`), а НЕ на ключ `result=`. Ключ `result=ok` присутствует
ТОЛЬКО в success-ветке (`msg=MCP`). У warn/deny/fail ключа `result=` НЕТ — там метка в `msg` и текст в
`reason=`. **Не писать в логе `result=deny`/`result=fail`/`result=warn`** как ключ — `writeAudit` так
НЕ рендерит. Во ВСЕХ четырёх upload-ветках присутствует `tool=upload_file` (через `else if isUpload` —
НЕ оставлять upload-запись в общем `else`, иначе `tool=` пропадёт). `size=` логируется ТОЛЬКО в
success (в warn/deny/fail запись либо не выполнена, либо размер нерелевантен).

> **logfmt-инъекция через путь ЗАКРЫТА кодировщиком (SR-79; R-U13).** Путь логируется как ЗНАЧЕНИЕ
> key/value (`path=<rel>`) через `charmbracelet/log`→`go-logfmt/logfmt`, который автоматически
> квотирует/экранирует значения со спецсимволами (пробел, `=`, `"`, `\n`, `\r`, управляющие < 0x20,
> DEL): подделать пару `result=`/новую строку лога через путь НЕВОЗМОЖНО. developer НЕ делает ручной
> конкатенации пути в строку лога — только передаёт `Path` как отдельное поле `AuditRecord`,
> логируемое как key/value (qa закрепляет тестом SR-79: путь со спецсимволами квотирован, запись
> остаётся одной парсимой logfmt-строкой).

#### 2.3.2. Ветки upload-аудита (поле `AuditRecord.Result` + рендер msg)

| Ветка | Триггер | `AuditRecord.Result` (поле) | Рендер в логе (`writeAudit`, ветка isUpload) | SR |
|---|---|---|---|---|
| **success** | файл записан/заменён | `"success"` (+ `Tool:"upload_file"`, `Path`, `Size`, `Fingerprint`, `RemoteAddr`) | `INFO MCP fp=.. remote=.. tool=upload_file result=ok path=<rel> size=<N>` | SR-78/SR-79 |
| **warn (root-предупреждение)** | `os.Geteuid()==0` при ЛЮБОМ вызове (отдельная запись помимо основной; reason=`running-as-root`) | `"warn"` (+ `Tool:"upload_file"`, `Path`(если известен), `Reason`, `Fingerprint`, `RemoteAddr`; без size) | `WARN WARN fp=.. remote=.. tool=upload_file reason=running-as-root.. [path=<rel>]` (ключа `result=` нет) | SR-77 / AC11 |
| **deny** | traversal / exists / isdir / too-large / bad-base64 / bad-mode / deny_root | `"deny"` (+ `Path`(если известен), `Reason`, `Fingerprint`, `RemoteAddr`; без size — запись не выполнена) | `WARN DENY fp=.. remote=.. tool=upload_file reason=.. [path=<rel>]` (ключа `result=` нет) | SR-72/SR-73/SR-75/SR-77/SR-78 |
| **fail** | диск полон / ошибка записи I/O | `"fail"` (+ `Path`(если известен), `Reason`, `Fingerprint`, `RemoteAddr`; без size) | `WARN FAIL fp=.. remote=.. tool=upload_file reason=.. [path=<rel>]` (ключа `result=` нет) | SR-74/SR-78 |

> **Двойная запись при euid==0 (SR-77) — две ОТДЕЛЬНЫЕ записи за вызов.** При `euid==0` ВСЕГДА
> пишется `"warn"`-запись (root-предупреждение), затем:
> - **`deny_root=false`** (дефолт): запись ВЫПОЛНЯЕТСЯ → дальше идёт ОСНОВНАЯ запись исхода
>   (`success`/`deny`/`fail`). Итого две записи: `warn` + основная.
> - **`deny_root=true`**: пишется `"warn"`, ЗАТЕМ `"deny"` (причина «upload as root forbidden by
>   policy»), файл НЕ создаётся. Итого две записи: `warn` + `deny` (порядок: сначала `warn`, потом
>   `deny`).
> При euid≠0 `"warn"`-записи нет — только одна основная запись (success/deny/fail). Зеркало
> command-exec §2.3.2.

> **Новые upload-поля (`Path`/`Size`) логируются writeAudit ТОЛЬКО при `Tool=="upload_file"`.**
> `path=` пишется во всех upload-ветках, где путь известен (success — всегда; warn/deny/fail — если
> путь успели определить ДО отказа); `size=` — только в success (в warn/deny/fail запись не выполнена
> / размер нерелевантен). `tool=upload_file` пишется во ВСЕХ четырёх upload-ветках (через `else if
> isUpload`; §2.3.1). Все upload-поля логируются ТОЛЬКО при `Tool=="upload_file"` — формат
> не-upload и exec-записей (`AUTH`/`FAIL`/`DENY`/`RATE`/`WARN`/`MCP`-ping/exec) НЕ ломается (SR-79;
> наследуемые тесты `tls-transport`/`mcp-server`/`command-exec` зелёные).

- **Формат:** строгий logfmt через `writeAudit` (тот же канал/логгер `charmbracelet/log` +
  `LogfmtFormatter`, что транспорт/MCP/exec; П-U1 наследует command-exec П-1; SR-79).
- **Без секретов (SR-80):** ни в `UploadOutput`/Content, ни в тексте `isError`, ни в upload-аудите
  НЕТ тела API-ключа/хэша/соли/raw `Authorization`/приватного TLS-ключа/ДЕКОДИРОВАННОГО содержимого
  файла/абсолютного пути хоста — вместо ключа fingerprint; в результате/аудите только относительный
  путь; сообщения об ошибках нейтральны.

---

## 3. Capabilities / tools/list

- **protocolVersion:** `2025-11-25` — БЕЗ изменений (§0). `upload_file` не влияет на согласование
  версии.
- **capabilities:** по-прежнему объявляется ТОЛЬКО `tools`. Resources, Prompts, Sampling, Logging,
  Completions — НЕ объявляются (см. §7). Никаких новых ресурсов/промптов file-upload НЕ вводит.
- **tools/list возвращает теперь ЧЕТЫРЕ инструмента:** `ping`, `server_info`, `execute_command`,
  **`upload_file`** (SR-68; spec AC1). Регистрация — той же точкой расширения `sdkmcp.AddTool` в
  `NewHandler` (`internal/mcp/server.go`), рядом с остальными. Список статичен
  (`tools.listChanged:false`).

**Фрагмент ответа `tools/list` (целевая форма для `upload_file`; точную сериализацию даёт SDK из
Go-структур `UploadInput`/`UploadOutput`, developer обязан добиться именно этих форм, qa проверяет):**

```json
{
  "name": "upload_file",
  "description": "Записать ОДИН обычный файл на хост raxd в безопасный каталог загрузок (upload root). Путь задаётся относительным к upload root; запись возможна ТОЛЬКО внутрь корня — попытка выйти наружу через '..', абсолютный путь или симлинк наружу отклоняется. Содержимое передаётся в кодировке base64. Размер декодированного содержимого ограничен серверным лимитом. По умолчанию существующий файл НЕ перезаписывается (нужен overwrite:true). Права создаваемого файла контролируются (по умолчанию 0600; биты setuid/setgid/sticky и world-writable запрещены). Инструмент создаёт только обычный файл, не повышает привилегии и не меняет владельца. Возвращает записанный относительный путь, размер, флаг перезаписи и итоговый режим. Каждый вызов проходит аутентификацию и аудит; содержимое файла в аудит не пишется.",
  "inputSchema": {
    "type": "object",
    "additionalProperties": false,
    "required": ["path", "content"],
    "properties": {
      "path": {
        "type": "string",
        "description": "Относительный путь назначения внутри upload root (напр. \"scripts/deploy.sh\"). Абсолютный путь и сегменты '..', выводящие за корень, отклоняются. Недостающие промежуточные подкаталоги внутри корня создаются автоматически."
      },
      "content": {
        "type": "string",
        "description": "Содержимое файла в кодировке base64 (standard, с паддингом). Невалидный base64 отклоняется. Декодированный размер ограничен серверным max_file_bytes."
      },
      "overwrite": {
        "type": "boolean",
        "description": "Разрешить замену существующего файла (опц., по умолчанию false). При false и существующем файле запись отклоняется; существующий файл не изменяется. При true файл заменяется атомарно."
      },
      "mode": {
        "type": "string",
        "description": "Права создаваемого файла восьмеричной строкой (опц., напр. \"0600\"; по умолчанию серверный default_mode). Допустимы только биты прав в маске 0777; setuid/setgid/sticky и world-writable запрещены и приводят к отказу."
      }
    }
  },
  "outputSchema": {
    "type": "object",
    "additionalProperties": false,
    "required": ["path", "size", "overwritten", "mode"],
    "properties": {
      "path": { "type": "string", "description": "Записанный относительный путь внутри upload root (как принят сервером). Абсолютный путь НЕ возвращается." },
      "size": { "type": "integer", "description": "Число записанных байт (декодированного содержимого)." },
      "overwritten": { "type": "boolean", "description": "true — существовавший файл был заменён (overwrite:true)." },
      "mode": { "type": "string", "description": "Фактический режим созданного файла восьмеричной строкой (напр. \"0600\")." }
    }
  }
}
```

> Замечания по SDK-генерации схем (по завендоренному SDK, как для `execute_command`):
> - **`additionalProperties:false` на `inputSchema` ГАРАНТИРУЕТСЯ инференцией SDK из Go-struct**
>   (для `reflect.Struct` SDK устанавливает `AdditionalProperties = falseSchema()`; подтверждено в
>   `command-exec/mcp-spec §3` по vendor `jsonschema-go/jsonschema/infer.go`). **developer НЕ обязан
>   выставлять `additionalProperties` явно** в дескрипторе `Tool` — достаточно объявить `UploadInput`
>   как struct. **qa ОБЯЗАН закрепить тестом** «лишнее поле → isError, файл не создан» как регрессию
>   против изменений SDK в `vendor/` (SR-68/AC2).
> - `path` и `content` обязательны (поля без `omitempty`); `overwrite`/`mode` опциональны
>   (`omitempty`) → `required:["path","content"]`.
> - `outputSchema` объявляется (результат структурный) — SDK валидирует `structuredContent` против
>   неё. Четыре поля точно соответствуют AC3/SR-80.
> - Полей абсолютного пути и владельца (owner/uid/gid/chown) в схеме НЕТ (AC2/AC9; spec Out of Scope;
>   threat-model R-U13): приём абсолютного пути и смена владельца отклонены конструкцией схемы.

---

## 4. Error mapping (таблица «ситуация → код/isError → что в ответе») — критично

Сводная таблица (источник истины по разделению протокол vs инструмент — SDK-диспетчер `tools/call`,
§2.2). Коды ОДНОЗНАЧНЫ — developer/qa берут их для тестов напрямую. Колонка «upload-аудит» указывает
ЗНАЧЕНИЕ поля `AuditRecord.Result` (его рендер в логе — §2.3.1: success→`msg=MCP result=ok`,
warn→`msg=WARN reason=`, deny→`msg=DENY reason=`, fail→`msg=FAIL reason=`):

| # | Ситуация | Где ловится | Код / `isError` | Что в ответе клиенту | `AuditRecord.Result` | SR / AC |
|---|---|---|---|---|---|---|
| 1 | Тело > `MaxBodyBytes` (раздутое base64) | transport bodyLimit | **HTTP 413** | транспортный отказ, до MCP не доходит | нет (транспорт) | SR-76 / AC16 |
| 2 | Нет/неизвестный/отозванный Bearer-ключ | transport auth | **HTTP 401** | транспортный отказ | нет (транспорт) | SR-68 / AC17 |
| 3 | `ErrCorrupt` keystore | transport auth | **HTTP 403** | транспортный отказ | нет | SR-68 / AC17 |
| 4 | `Origin` present&invalid / Host вне allowlist | transport | **HTTP 403** | транспортный отказ | нет | насл. SR-14/16 |
| 5 | Превышение rate-limit (per-key/per-IP) | transport | **HTTP 429** | транспортный отказ | нет (RATE транспорта) | SR-68 / AC18 |
| 6 | Битый JSON | SDK | **JSON-RPC −32700** | `error{code,message}` | нет | насл. SR-30 / AC14 |
| 7 | Не валидный JSON-RPC request | SDK | **JSON-RPC −32600** | `error{code,message}` | нет | насл. SR-30 / AC14 |
| 8 | Неизвестный JSON-RPC метод | SDK | **JSON-RPC −32601** (`ErrMethodNotFound`) | `error{code,message}` | нет | насл. SR-30 / AC14 |
| 9 | Неизвестный инструмент в `tools/call` (опечатка имени) | SDK | **JSON-RPC −32602** (`jsonrpc.CodeInvalidParams`) | `error{code,message:"unknown tool …"}` | нет | насл. SR-30 / AC14 |
| 10 | Лишнее поле (`additionalProperties:false`); неверный тип; нет required `path`/`content` | SDK до handler | **`isError:true`** | `result{content:[text:"…validating arguments…"], isError:true}` — **текст генерирует SDK (ориентир); qa проверяет только `isError:true` + факт «файл не создан», НЕ дословный текст** | нет (handler не вызван — §2.2 примечание) | SR-68 / AC2/AC14 |
| 11 | traversal: `..`-escape / абсолютный / симлинк наружу / TOCTOU | fileupload (os.Root) | **`isError:true`** | `result{content:[text:нейтральное], isError:true}` | `"deny"` → `msg=DENY reason=` | SR-69 / AC4 |
| 12 | цель существует И `overwrite:false` | fileupload (`Root.Stat`) | **`isError:true`** | `result{…, isError:true}` | `"deny"` → `msg=DENY reason=` | SR-72 / AC8 |
| 13 | цель указывает на существующий КАТАЛОГ | fileupload (`Root.Stat`+IsDir) | **`isError:true`** | `result{…, isError:true}` | `"deny"` → `msg=DENY reason=` | SR-72 / AC14 |
| 14 | декодированный размер > `max_file_bytes` (ранний фильтр или точная len) | uploadHandler до записи | **`isError:true`** | `result{…, isError:true}` | `"deny"` → `msg=DENY reason=` | SR-75/SR-76 / AC7 |
| 15 | невалидный base64 (`CorruptInputError`) | uploadHandler до записи | **`isError:true`** | `result{content:[text:"invalid base64"], isError:true}` | `"deny"` → `msg=DENY reason=` | SR-75 / AC6 |
| 16 | невалидный `mode` (непарсим / setuid·setgid·sticky / world-writable) | uploadHandler (`ParseMode`) | **`isError:true`** | `result{…, isError:true}` | `"deny"` → `msg=DENY reason=` | SR-73 / AC9/AC14 |
| 17 | `deny_root:true` И `euid==0` | uploadHandler до записи | **`isError:true`** | `result{…, isError:true}` | `"warn"` + `"deny"` (две записи; §2.3.2) | SR-77 / AC11 |
| 18 | `euid==0` И `deny_root:false` | uploadHandler | **НЕ отказ** (файл пишется) | результат записи (success) | `"warn"` + основная (`success`) | SR-77 / AC11 |
| 19 | диск полон / ошибка записи I/O | fileupload | **`isError:true`** | `result{content:[text:"write failed"], isError:true}` | `"fail"` → `msg=FAIL reason=` | SR-74 / AC14 |
| 20 | **успешная запись/замена** | uploadHandler/fileupload | **НЕ ошибка** (`isError` отсутствует/false) | `result{content+structuredContent{path,size,overwritten,mode}}` | `"success"` → `msg=MCP result=ok path= size=` | SR-78 / AC3 |
| 21 | паника писателя (контрактно невозможна) | transport recover | **HTTP 500**, сервер жив | транспортный 500 | нет | насл. SR-30 / AC14 |

Краткая логика для developer:
- **Транспортная ошибка** = HTTP 413/401/403/429 (транспорт): тело>лимит / неверный ключ / Origin /
  rate-limit. Файл не записывается; до SDK не доходит.
- **Протокольная ошибка (JSON-RPC `error`)** = −32700 (битый JSON) / −32600 (не request) / −32601
  (неизвестный метод) / −32602 (неизвестный инструмент). Файл не записывается; до handler не доходит.
- **`isError:true` в результате** = ошибка валидации входа (SDK, #10) ИЛИ `error` из uploadHandler/
  fileupload: **deny** (traversal / exists / isdir / too-large / bad-base64 / bad-mode / deny_root) —
  отказ-по-контролю; **fail** (диск полон / I/O) — сбой записи. Сервер жив, следующий валидный вызов
  отрабатывает (SR-68/наследует SR-30; R-U8).
- **НЕ ошибка** = успешная запись (#20); root-`warn` при `deny_root:false` (#18) — нормальный
  результат с `isError:false` (файл пишется, `warn` — отдельная аудит-запись).

---

## 5. Инструмент `upload_file` (схемы вход/выход)

Принципы дизайна (agent-native / mcp-tool-design): `upload_file` — **примитив** (запиши обычный файл),
а НЕ workflow (никакой бизнес-логики «куда и зачем класть» в инструменте — это решает агент); вход —
**данные** (путь + содержимое + флаги), не решения; имя описывает возможность; выход — **rich** (агент
видит итоговый путь/размер/был ли перезаписан/режим и сам решает следующий шаг). `name` — латиница
(стабильный идентификатор); описания на русском (язык артефактов). Это «опасный примитив» (запись в
ФС хоста по сети), поэтому ограничения безопасности §3 (traversal-safety, лимит, mode-политика,
overwrite-дефолт, атомарность, root-детекция, аудит) — НЕ опциональны и реализуются на сервере, а не в
схеме (принцип «API as Validator»: схема не пытается выразить traversal/mode-политику типами — это
проверяет писатель/handler и возвращает `isError`).

> **Замечание по CRUD-полноте (mcp-tool-design «CRUD Completeness»).** v1 file-upload даёт ТОЛЬКО
> Create/Replace (запись/перезапись файла). Read (`download_file`/чтение ФС) и Delete (удаление файла)
> — **вне scope v1** (spec Out of Scope: «Скачивание/чтение файлов — отдельная задача»). Это
> сознательное ограничение поверхности записи (не пробел дизайна): полный CRUD по файлам — предмет
> будущих задач (`download_file` и т.п.), здесь зафиксирован как Out of Scope, а не как недостаток
> (см. §7 и Открытые вопросы Q-UPL-3).

### 5.1. Вход — `UploadInput`

| Поле | Тип | Обяз. | JSON-тег (контракт для developer) | Семантика / валидация |
|---|---|---|---|---|
| `path` | string | **да** | `json:"path"` | относительный путь внутри upload root. Пусто → `isError` (SDK: required). Абсолютный / `..`-escape / симлинк наружу → `ErrTraversal` deny (SR-69/AC4) |
| `content` | string | **да** | `json:"content"` | base64 (standard). Невалидный base64 → `CorruptInputError` deny (SR-75/AC6). Декодированный размер > `max_file_bytes` → deny (SR-75/SR-76/AC7) |
| `overwrite` | bool | нет | `json:"overwrite,omitempty"` | разрешить замену существующего файла. Пусто → `false` (дефолт, AC8). `false`+существует → `ErrExists` deny (SR-72) |
| `mode` | string | нет | `json:"mode,omitempty"` | восьмеричная строка прав. Пусто → `cfg.DefaultMode`. Непарсим / setuid·setgid·sticky / world-writable → `ErrBadMode` deny (SR-73/AC9/AC14) |

- **`additionalProperties:false`** (строгая схема): любое неизвестное поле → `isError` (SR-68; SDK до
  handler, §2.2). Гарантируется инференцией SDK из struct; **закрепить тестом** как регрессию (AC2):
  лишнее поле → `isError`, файл не создан.
- **Полей абсолютного пути и владельца НЕТ** (AC2/AC9; spec Out of Scope; threat-model R-U13):
  абсолютный путь не принимается отдельным полем; смена владельца (owner/uid/gid/chown) не
  предусмотрена конструкцией.
- **Валидация входа ДО записи** (uploadHandler, ПОСЛЕ SDK-валидации схемы): (1) ранний фильтр размера
  по `base64.DecodedLen` ДО декодирования (защита памяти; SR-75/R-U5/R-U8); (2) `DecodeString`
  (CorruptInputError → deny; SR-75); (3) точная `len(decoded) ≤ max_file_bytes` (SR-75); (4) режим
  через `fileupload.ParseMode` (SR-73). Все четыре → `isError:true` + upload-аудит `deny`, файл и temp
  НЕ создаются (SR-74).

**Целевая входная JSON-схема** — см. `inputSchema` в §3.

### 5.2. Выход — `UploadOutput` (AC3 / SR-80)

`uploadHandler` маппит `fileupload.Result` → `UploadOutput` (structuredContent) + краткий text-блок
(Content).

| Поле | Тип | JSON-тег | Семантика |
|---|---|---|---|
| `Path` | string | `json:"path"` | записанный относительный путь (как принят сервером; НЕ абсолютный, SR-80) |
| `Size` | int64 | `json:"size"` | число записанных байт декодированного содержимого (целое число, БЕЗ суффикса) |
| `Overwritten` | bool | `json:"overwritten"` | true — существовавший файл заменён (overwrite:true) |
| `Mode` | string | `json:"mode"` | фактический режим созданного файла (восьмеричная строка, напр. «0600») |

- **`content` (text-блок) — краткое РЕЗЮМЕ для модели** (не дамп содержимого): рекомендованная форма —
  `"path=<rel> size=<N>B overwritten=<bool> mode=<oct>"`. **Суффикс `B` (например `size=8B`) — ТОЛЬКО
  в этом human-ориентированном text-блоке для агента.** В `structuredContent.size` — целое число без
  суффикса; в logfmt-логе `writeAudit` поле `size=` — тоже целое число без суффикса (`size=8`, §6.1).
  Полные поля — в `structuredContent`. Резюме без секретов и без содержимого файла (SR-80); абсолютный
  путь НЕ включается (SR-80/R-U13).
- **`structuredContent`** = `UploadOutput` целиком (4 поля; SDK валидирует против `outputSchema`).
  Форма ОБЯЗАНА соответствовать §3 (SR-80; qa проверяет четыре поля).
- **`isError`** отсутствует/false при успешной записи и при root-`warn` (`deny_root:false`, #18); true
  — только в ветках deny/fail (§4 #11–#17, #19).

### 5.3. Ошибки инструмента (перечень — обязателен по red line «каждый tool: вход+выход+ошибки»)

Полная таблица — §4 (#1–#21). Кратко по источнику:
- **до tool (транспорт):** 413 (тело>лимит) / 401/403 (auth) / 403 (Origin/Host) / 429 (rate-limit)
  — файл не записывается (SR-76/SR-68).
- **протокол (SDK):** −32700 (битый JSON) / −32600 (не request) / −32601 (неизвестный метод) / −32602
  (неизвестный инструмент) — файл не записывается (наследует SR-30).
- **валидация входа (SDK):** лишнее поле / неверный тип / нет required `path`/`content` → `isError:true`
  (SR-68; handler не вызван — upload-аудит по этой ветке не пишется, §2.2).
- **deny (uploadHandler/fileupload):** traversal (`ErrTraversal`) / exists (`ErrExists`) / isdir
  (`ErrIsDir`) / too-large (`ErrTooLarge`/ранний фильтр) / bad-base64 (`CorruptInputError`) / bad-mode
  (`ErrBadMode`) / deny_root → `isError:true` + аудит `Result:"deny"` (рендер `msg=DENY reason=`;
  SR-69/SR-72/SR-73/SR-75/SR-77). Сообщения нейтральны, без абсолютных путей/секретов (SR-80).
- **fail (fileupload):** диск полон / I/O-ошибка → `isError:true` + аудит `Result:"fail"` (рендер
  `msg=FAIL reason=`; SR-74). temp очищается (SR-74). Сообщения нейтральны (SR-80).
- **warn (root-предупреждение, НЕ ошибка):** `euid==0` → отдельная аудит-запись `Result:"warn"`
  (рендер `msg=WARN reason=running-as-root`); при `deny_root:false` запись ВЫПОЛНЯЕТСЯ (SR-77).
- **НЕ ошибка:** успешная запись/замена → `isError:false` (SR-78/AC3).

---

## 6. Примеры (JSON-RPC tools/call) — согласованы с тем, что вернёт go-sdk

> Транспортно — POST на `https://127.0.0.1:<port>/mcp` с заголовками `Authorization: Bearer
> rax_live_…`, `Content-Type: application/json`, `Accept: application/json, text/event-stream`,
> `MCP-Protocol-Version: 2025-11-25`. Формы `result`/`error` соответствуют SDK (`ToolHandlerFor`).
> Аудит-строки приведены в соответствии с фактическим рендером `writeAudit`
> (`internal/server/audit.go`, ветка `isUpload`, которую добавляет developer; см. §2.3.1):
> success→`msg=MCP … result=ok path= size=`; warn→`msg=WARN … reason=` (без `result=`);
> deny→`msg=DENY … reason=` (без `result=`); fail→`msg=FAIL … reason=` (без `result=`). Во ВСЕХ
> upload-ветках присутствует `tool=upload_file`. (Значения `<fp>`/`<ip>` — fingerprint и remote из
> ctx; точная сериализация — за writeAudit.)

### 6.1. Успех (новый файл записан — `isError:false`)

Запрос (`content` — base64 от строки `echo hi\n`):
```json
{
  "jsonrpc": "2.0",
  "id": 20,
  "method": "tools/call",
  "params": {
    "name": "upload_file",
    "arguments": { "path": "scripts/deploy.sh", "content": "ZWNobyBoaQo=", "mode": "0700" }
  }
}
```
Ответ (на успехе SDK опускает поле `isError` — как в command-exec; consumers трактуют отсутствие/false
одинаково):
```json
{
  "jsonrpc": "2.0",
  "id": 20,
  "result": {
    "content": [
      { "type": "text", "text": "path=scripts/deploy.sh size=8B overwritten=false mode=0700" }
    ],
    "structuredContent": {
      "path": "scripts/deploy.sh",
      "size": 8,
      "overwritten": false,
      "mode": "0700"
    }
  }
}
```
> Суффикс `B` (`size=8B`) есть ТОЛЬКО в text-блоке `content` (для модели); в `structuredContent.size`
> и в logfmt-логе ниже — целое число без суффикса (`8` / `size=8`). См. §5.2 (F-4).

`AuditRecord.Result = "success"`. Лог (рендер writeAudit, ветка isUpload):
`INFO MCP fp=<fp> remote=<ip> tool=upload_file result=ok path=scripts/deploy.sh size=8`
(формат logfmt; ключ `result=ok` есть только в success-ветке; `size=` числовое без суффикса; содержимое
файла в логе ОТСУТСТВУЕТ; абсолютный путь ОТСУТСТВУЕТ — только относительный).

### 6.2. Traversal — deny (запись наружу отклонена; SR-69/AC4)

Запрос:
```json
{
  "jsonrpc": "2.0",
  "id": 21,
  "method": "tools/call",
  "params": { "name": "upload_file", "arguments": { "path": "../etc/passwd", "content": "eA==" } }
}
```
Ответ (`isError:true`, файл вне корня НЕ создан; SR-69):
```json
{
  "jsonrpc": "2.0",
  "id": 21,
  "result": {
    "content": [ { "type": "text", "text": "path is outside the upload root" } ],
    "isError": true
  }
}
```
`AuditRecord.Result = "deny"`. Лог (рендер writeAudit — метка `DENY`, есть `tool=`/`reason=`, ключа
`result=` НЕТ; путь логируется квотированным как key/value, SR-79):
`WARN DENY fp=<fp> remote=<ip> tool=upload_file reason=traversal path=../etc/passwd`
(те же векторы `"/etc/passwd"`, `"a/../../b"`, симлинк наружу → так же deny; сообщение нейтрально, без
абсолютного пути; SR-80.)

### 6.3. Файл существует без overwrite — deny (SR-72/AC8)

Запрос (повторная загрузка по существующему пути, `overwrite` не задан → дефолт false):
```json
{
  "jsonrpc": "2.0",
  "id": 22,
  "method": "tools/call",
  "params": { "name": "upload_file", "arguments": { "path": "data/config.yaml", "content": "Zm9vOiBiYXIK" } }
}
```
Ответ (`isError:true`, существующий файл НЕ изменён; SR-72):
```json
{
  "jsonrpc": "2.0",
  "id": 22,
  "result": {
    "content": [ { "type": "text", "text": "file already exists (set overwrite to replace)" } ],
    "isError": true
  }
}
```
`AuditRecord.Result = "deny"`. Лог:
`WARN DENY fp=<fp> remote=<ip> tool=upload_file reason=exists path=data/config.yaml`
(с `overwrite:true` тот же путь → файл заменён атомарно, `overwritten:true`, ответ как §6.1 с
`overwritten:true`. Цель-каталог → так же deny: `reason=is-directory`.)

### 6.4. Превышение размера — deny (SR-75/SR-76/AC7)

Запрос (декодированный размер > `max_file_bytes`; `content` укорочён для примера):
```json
{
  "jsonrpc": "2.0",
  "id": 23,
  "method": "tools/call",
  "params": { "name": "upload_file", "arguments": { "path": "big.bin", "content": "<~1 MiB base64 ≤ MaxBodyBytes, decoded > max_file_bytes>" } }
}
```
Ответ (`isError:true`, файл и temp НЕ остаются; SR-75/SR-74):
```json
{
  "jsonrpc": "2.0",
  "id": 23,
  "result": {
    "content": [ { "type": "text", "text": "file too large" } ],
    "isError": true
  }
}
```
`AuditRecord.Result = "deny"`. Лог:
`WARN DENY fp=<fp> remote=<ip> tool=upload_file reason=too-large path=big.bin`
> Граница с транспортом (AC16/SR-76): если base64-ТЕЛО запроса само превышает `MaxBodyBytes` (1 MiB) —
> отказ наступит РАНЬШЕ, на транспорте: **HTTP 413** ДО инструмента (строка #1 §4), upload-аудит НЕ
> пишется (handler не вызван). Этот пример (#23) — про случай, когда тело укладывается в
> `MaxBodyBytes`, но ДЕКОДИРОВАННЫЙ размер > `max_file_bytes`.

### 6.5. Невалидный mode — deny (SR-73/AC9/AC14)

Запрос (`mode="04755"` — setuid-бит запрещён ADR-003):
```json
{
  "jsonrpc": "2.0",
  "id": 24,
  "method": "tools/call",
  "params": { "name": "upload_file", "arguments": { "path": "x.sh", "content": "eA==", "mode": "04755" } }
}
```
Ответ (`isError:true`, файл НЕ создан; SR-73):
```json
{
  "jsonrpc": "2.0",
  "id": 24,
  "result": {
    "content": [ { "type": "text", "text": "invalid file mode" } ],
    "isError": true
  }
}
```
`AuditRecord.Result = "deny"`. Лог:
`WARN DENY fp=<fp> remote=<ip> tool=upload_file reason=bad-mode path=x.sh`
(те же `"02755"` setgid / `"01777"` sticky / world-writable / непарсимый → так же deny.
Невалидный base64 → `reason=bad-base64`; диск полон → `fail`: `WARN FAIL … reason=write-failed path=…`.)

### 6.6. Демон от root (`euid==0`) — отдельная `warn`-запись (SR-77/AC11)

При `euid==0` и `deny_root=false` файл ПИШЕТСЯ как обычно (ответ клиенту — как §6.1), но в аудит
пишутся ДВЕ записи за вызов: сначала `warn` (root-предупреждение), затем основная (success). Лог
(рендер writeAudit — метка `WARN`, есть `tool=upload_file`/`reason=`, ключа `result=` НЕТ; `tool=`
пишется через `else if isUpload` в `case "warn"`, §8):
`WARN WARN fp=<fp> remote=<ip> tool=upload_file reason=running-as-root: raxd writing files as root (euid==0); ensure raxd runs as non-root path=scripts/deploy.sh`
`INFO MCP fp=<fp> remote=<ip> tool=upload_file result=ok path=scripts/deploy.sh size=8`

При `deny_root=true` и `euid==0` файл НЕ пишется; пишутся `warn`, затем `deny` (ответ клиенту —
`isError:true`; §4 #17):
`WARN WARN fp=<fp> remote=<ip> tool=upload_file reason=running-as-root.. path=scripts/deploy.sh`
`WARN DENY fp=<fp> remote=<ip> tool=upload_file reason=upload as root is forbidden by policy (deny_root=true) path=scripts/deploy.sh`

### 6.7. Неизвестный инструмент (для контраста — protocol error −32602, НЕ запись)

Если клиент опечатался в имени (`upload` вместо `upload_file`) — это **JSON-RPC −32602** (SDK
`jsonrpc.CodeInvalidParams`), файл НЕ пишется. Отличается от −32601 (неизвестный JSON-RPC метод):
здесь метод (`tools/call`) валиден, но имя инструмента в `params` неизвестно:
```json
{ "jsonrpc": "2.0", "id": 25, "error": { "code": -32602, "message": "unknown tool \"upload\"" } }
```

### 6.8. Лишнее поле / нет required во входе (валидация SDK → `isError:true`; SR-68/AC2)

Запрос с неизвестным полем `owner` (попытка задать владельца — поля нет в схеме):
```json
{
  "jsonrpc": "2.0",
  "id": 26,
  "method": "tools/call",
  "params": { "name": "upload_file", "arguments": { "path": "x", "content": "eA==", "owner": "root" } }
}
```
Ответ (SDK отверг по `additionalProperties:false` ДО handler; файл не создан):
```json
{
  "jsonrpc": "2.0",
  "id": 26,
  "result": {
    "content": [ { "type": "text", "text": "validating \"arguments\": additional properties not allowed: owner" } ],
    "isError": true
  }
}
```
> Текст сообщения генерирует SDK — приведён как ориентир; qa проверяет `isError:true` + «файл не
> создан», не дословный текст. Аналогично: пропуск required `path`/`content` → `isError:true` (SDK).
> upload-аудит по этой ветке НЕ пишется (handler не вызван; §2.2 примечание).

---

## 7. Resources / Prompts

**None — file-upload НЕ вводит ресурсов и промптов** (SR-68; spec AC1 — поверхность только tool
`upload_file`). Обоснование: задача — единственный новый инструмент за наследуемой цепочкой; никакого
нового read-only контекста (resource) или шаблона (prompt) AC не требуют. capability
`resources`/`prompts` в `initialize` НЕ объявляется (как в `mcp-server`/`command-exec`, §3).
Потенциальный будущий resource `capabilities` (отдать агенту: путь/лимит upload root, `max_file_bytes`,
дефолтный режим, политика overwrite) — НЕ в scope этой задачи (отдельной задачей через
capability-негоциацию; те же правила «без секретов» — НЕ отдавать абсолютный путь корня/раскладку ФС,
ср. `command-exec/mcp-spec §7`; threat-model R-U13/SR-80). `download_file`/удаление файла (полный CRUD)
— тоже отдельные задачи (spec Out of Scope; §5 «CRUD-полнота»).

---

## 8. Точка интеграции (для developer — без реализации, только формы; источник истины — plan §Contracts/§Modules)

> Дублирует plan §Contracts в терминах MCP-дизайна. Сигнатуры Go — в `plan.md`.

- **Регистрация (та же точка, что `ping`/`server_info`/`execute_command`):** в
  `internal/mcp/server.go:NewHandler` рядом с существующими `sdkmcp.AddTool(...)` добавить
  `sdkmcp.AddTool(s, uploadTool(), uploadHandler(uplCfg, audit))` — **БЕЗ** `withAudit` (ADR-004-стиль/
  SR-78). Сигнатура `NewHandler` расширяется параметром `uplCfg fileupload.Config`:
  `NewHandler(ver string, audit server.AuditFn, execCfg cmdexec.Config, uplCfg fileupload.Config)
  (http.Handler, error)`. Все вызовы (`internal/cli/serve.go`, mcp-тесты) обновляются.
- **`uploadTool() *sdkmcp.Tool`** — дескриптор: `Name:"upload_file"`, `Description:` (как §3; для
  ИИ-агента: что делает + ключевые ограничения — только внутрь корня, лимит размера, без перезаписи по
  умолчанию, mode-политика, без эскалации). Схемы вход/выход SDK выводит из `UploadInput`/`UploadOutput`.
- **`uploadHandler(cfg fileupload.Config, audit server.AuditFn) sdkmcp.ToolHandlerFor[UploadInput,
  UploadOutput]`** — адаптер MCP↔fileupload: достаёт fingerprint/remote из ctx (SR-80); root-детекция
  (SR-77: при euid==0 — `warn`-запись, при `deny_root` ещё и `deny`); ранний фильтр размера +
  base64-декод + точная проверка размера (SR-75/SR-76); резолв/валидация `mode` (`ParseMode`, SR-73);
  `fileupload.Write`; маппинг `Result`→`UploadOutput` + Content; собственный upload-аудит
  (success/deny/fail + warn; SR-78). Возврат `error` из handler → SDK пакует в `isError:true`.
- **`fileupload.Write(cfg, in) (Result, error)`** (`internal/fileupload/upload.go`) — чистый писатель
  через `os.Root` (ADR-001/ADR-002): `OpenRoot` → `filepath.IsLocal` ранний отказ → `Root.MkdirAll` →
  `Root.Stat` (overwrite/каталог) → temp(`crypto/rand`,`O_EXCL`) → chmod по fd → write → Sync →
  `Root.Rename` → fsync-dir; temp очищается на любой ошибке (defer). Ошибки: `ErrTraversal`,
  `ErrExists`, `ErrIsDir`, `ErrTooLarge`, `ErrBadMode`, прочее I/O (fail). Без MCP/SDK/логов
  (юнит-тестируем; SR-82).
- **`AuditRecord` / `writeAudit` (расширение, SR-79) — разграничение «поле Result vs рендер»:**
  - **Поле `AuditRecord.Result`**, которое выставляет `uploadHandler`, принимает значения
    `"success"` / `"deny"` / `"fail"` / `"warn"` (вход в `writeAudit`).
  - **Новые опц. поля `AuditRecord`:** `Path string`, `Size int64` (логируются ТОЛЬКО при
    `Tool=="upload_file"`). Текущий `AuditRecord` (`internal/server/audit.go`) их ещё НЕ содержит —
    **developer добавляет** (plan §Modules).
  - **КРИТИЧНО — рендер upload-веток в `writeAudit` (F-2; сверено с фактическим
    `internal/server/audit.go`).** В текущем `writeAudit` КАЖДЫЙ `case` (`"success"`, `"warn"`,
    `"deny"`, `"fail"`) имеет структуру `if isExec { …логирует tool=/command=/… } else { …БЕЗ tool=
    … }`. В частности, **`case "warn"` (строки ~117-136): ветка `if isExec` пишет `tool=`/`reason=`/
    `command=`, а общий `else` поле `tool=` НЕ пишет.** Для `upload_file` `isExec=false`, поэтому
    **upload-запись НЕЛЬЗЯ оставлять в общем `else`** — иначе она выйдет БЕЗ `tool=upload_file` и
    qa-тесты (§6.6 и др.) упадут. developer ОБЯЗАН в КАЖДОМ из четырёх `case` добавить ОТДЕЛЬНЫЙ блок
    `else if isUpload { … }` (где `isUpload := rec.Tool == "upload_file"`), логирующий `tool=rec.Tool`
    и upload-поля по ветке — по аналогии с тем, как ветка `if isExec` логирует `tool=`/`reason=`/
    `command=`. Итоговая структура каждого `case`: `if isExec { … } else if isUpload { … } else { … }`.
    Конкретно по веткам:
    - `case "success"` (+isUpload) → `INFO msg=MCP`, ключи `tool=upload_file`, `result=ok`, `path=`,
      `size=` (целое, без суффикса).
    - `case "warn"` (+isUpload) → `WARN msg=WARN`, ключи `tool=upload_file`, `reason=`, `path=` (если
      известен); БЕЗ `result=`, БЕЗ `size=`. **Это та ветка, что в общем `else` теряла бы `tool=`.**
    - `case "deny"` (+isUpload) → `WARN msg=DENY`, ключи `tool=upload_file`, `reason=`, `path=` (если
      известен); БЕЗ `result=`, БЕЗ `size=`.
    - `case "fail"` (+isUpload) → `WARN msg=FAIL`, ключи `tool=upload_file`, `reason=`, `path=` (если
      известен); БЕЗ `result=`, БЕЗ `size=`.
    НЕ оставлять НИ ОДНУ upload-ветку в общем `else` (там `tool=` не пишется). Проверка (qa): warn/
    deny/fail/success-запись `upload_file` содержит `tool=upload_file` (а не теряет его). developer
    **РАСШИРЯЕТ существующий `writeAudit`**, сохраняя метки и рендер не-upload/не-exec записей (SR-79;
    наследуемые тесты `tls-transport`/`mcp-server`/`command-exec` зелёные).
  - **Безопасность пути в логе (SR-79):** `Path` передаётся как key/value-значение
    (`logger.Warn("DENY", …, "tool", rec.Tool, "reason", rec.Reason, "path", rec.Path)`), НЕ ручной
    конкатенацией — logfmt квотирует спецсимволы (R-U13).
- **Контекстные обёртки (готовы):** `server.FingerprintFromContext(ctx)`
  (`internal/server/auth.go`), `server.RemoteAddrFromContext(ctx)` — upload-слою тело ключа недоступно
  (SR-80).
- **Config (секция `upload`, plan §Config / SR-81):** `root`(пусто→резолв `<StateDir>/uploads`, 0700),
  `max_file_bytes`(716800 = 700 KiB), `default_mode`(`"0600"`), `overwrite_default`(`false`),
  `deny_root`(`false`). Дизайн инструмента опирается на эти дефолты; задаёт их developer в
  `internal/config`; невалидные значения отвергаются на старте (SR-81; AC15). Согласование
  `max_file_bytes` ≤ потолка из `MaxBodyBytes` — проверка на старте (SR-76).
- **serve.go (точка интеграции):** собрать `fileupload.Config` из `cfg.Upload` (резолв пустого `root`
  к `<StateDir>/uploads` с правами 0700) и передать в `NewHandler` (plan §Modules; SR-71).

---

## 9. Перечень tools (сводка после file-upload)

| name | тип | описание | вход | выход | ошибки |
|---|---|---|---|---|---|
| `ping` | read-only primitive | проверка живости MCP-канала | `{}` | `content:[text:"pong"]` | насл. (mcp-server) |
| `server_info` | read-only primitive | версия+сведения о raxd без секретов | `{}` | `structuredContent:{name,version,protocolVersion}` | насл. (mcp-server) |
| `execute_command` | state-changing primitive (RCE) | запуск команды на хосте без shell, с таймаутом/лимитами/аудитом | `{command, args?, timeout_ms?, cwd?}` | `structuredContent:ExecOutput(7 полей)` + text-резюме | насл. (command-exec §4) |
| **`upload_file`** | **state-changing primitive (запись в ФС)** | **запись обычного файла внутрь upload root: traversal-safe, лимит размера, без перезаписи по умолчанию, mode-политика, атомарно, аудит без содержимого** | `{path, content(base64), overwrite?, mode?}` | `structuredContent:UploadOutput(4 поля: path/size/overwritten/mode)` + text-резюме | §4 (#1–#21): транспорт 413/401/403/429; SDK −32700/−32600/−32601/−32602; валидация входа → isError; deny (traversal/exists/isdir/too-large/bad-base64/bad-mode/deny_root) / fail (I/O) → isError; успешная запись / root-warn → НЕ ошибка |

Приоритет первой (и единственной по spec) итерации: один инструмент `upload_file`, целиком
закрывающий 20 AC и SR-68…SR-82. Группировки/отсрочки внутри не требуется — задача неделима (spec
§«Примечание о размере»). CRUD по файлам не полон сознательно (только Create/Replace; Read/Delete —
Out of Scope, §5/§7).

---

## Открытые вопросы

- [ ] **Q-UPL-1 (статус ADR — расхождение строки `Статус`, НЕ блокер).** Постановка задачи называет
  ADR-003 «accepted», но в файлах `decisions/ADR-001..003` строка статуса = `proposed` (все три). На
  СОДЕРЖАНИЕ контракта это не влияет: решения ADR-001/002/003 подтверждены security в `threat-model.md`
  («Решения по зависимостям architect» №1–5, П-U1/П-U2 — приняты как владельцем baseline) и
  зафиксированы в SR-69/SR-73/SR-74. Рекомендация mcp-engineer: architect/дирижёру синхронизировать
  строку `Статус` ADR на `accepted` (как у command-exec ADR-001..004) ДО реализации, чтобы избежать
  разнобоя «proposed в файле / accepted в задаче». Не блокирует developer.
- [ ] **Q-UPL-2 (аудит SDK-валидации входа — для security; НЕ блокер контракта; зеркало command-exec
  Q-EXEC-3).** Ветка «лишнее поле / неверный тип / нет required» (§4 #10) перехватывается SDK ДО
  `uploadHandler`, поэтому upload-аудит-запись по ней НЕ пишется (соединение зафиксировано
  транспортным `authSuccessAudit`; SR-68 проверяет лишь `isError`+«файл не создан»). Если **security**
  сочтёт нужным фиксировать и эту ветку как upload-`deny`, потребуется перейти на низкоуровневый
  `ToolHandler` (вместо `ToolHandlerFor`) с ручной валидацией и аудитом — усложнение ради одной ветки.
  Рекомендация mcp-engineer: оставить как есть (SR-78 покрывает ветки handler'а; SDK-валидация — до
  него), консистентно с принятым решением command-exec. Нужно подтверждение security.
- [ ] **Q-UPL-3 (полнота CRUD по файлам — фиксация Out of Scope, НЕ блокер).** v1 даёт только
  Create/Replace; Read (`download_file`/чтение ФС) и Delete (удаление файла) — Out of Scope (spec). Это
  сознательное сужение поверхности записи, не пробел дизайна. Фиксируется как ориентир для будущих
  задач (полный CRUD по файлам), не требует действий в file-upload.

> **Нестыковок spec / plan / security НЕ обнаружено** (кроме строки статуса ADR — Q-UPL-1, не влияет
> на содержание). spec (AC1–AC20), plan (модули/контракты/§Config), security-requirements
> (SR-68…SR-82), threat-model (R-U1…R-U13, П-U1/П-U2, решения architect №1–5) и ADR-001/002/003
> согласованы между собой и с поведением завендоренного go-sdk (`ToolHandlerFor`: ошибка handler →
> `isError`; валидация входа → `isError` до handler; неизвестный метод → −32601; неизвестный
> инструмент → −32602) и с фактическим кодом аудита (`internal/server/audit.go`: значения
> `AuditRecord.Result` = success/deny/fail/warn/rate-limited; рендер success→`msg=MCP result=ok`,
> warn→`msg=WARN reason=`, deny→`msg=DENY reason=`, fail→`msg=FAIL reason=`; `tool=`/exec-поля только в
> ветке `if isExec`, общий `else` без `tool=` — образец и предупреждение для добавляемой ветки
> `else if isUpload`). Развилки spec (Q1–Q5) закрыты в plan §Config и ADR-001/002/003 и подтверждены
> security в threat-model — здесь зафиксированы как принятые контрактные. Открыты лишь три
> НЕблокирующих пункта выше (Q-UPL-1..3).
