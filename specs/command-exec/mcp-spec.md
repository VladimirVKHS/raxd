# MCP Spec: command-exec — MCP-инструмент `execute_command` (безопасное выполнение команд на хосте)

Автор спецификации: mcp-engineer (raxd). Автор продукта: **Vladimir Kovalev, OEM TECH**.
Вход: `spec.md` (AC1–AC18, pm-guardian pass), `plan.md` (architect), `decisions/ADR-001..004`,
`security-requirements.md` (SR-40…SR-67, ОБЯЗАТЕЛЬНЫ), `threat-model.md` (R-1…R-15, П-1..3, ОР-1..6),
`.claude/reference/MCP-INTEGRATION.ru.md`, `specs/mcp-server/mcp-spec.md` (стиль и наследуемый
контракт), готовый код `internal/mcp/{server,tools,audit}.go`, `internal/server/{audit,auth}.go`,
завендоренный `github.com/modelcontextprotocol/go-sdk/mcp` (`tool.go`/`server.go`).

> Этот документ — **контракт дизайна** для developer (реализует `internal/cmdexec/*`,
> `internal/mcp/exec_tool.go`, расширения `internal/mcp/server.go`, `internal/server/audit.go` на Go
> SDK) и для qa/reviewer (проверяют соответствие). Здесь только дизайн и схемы (JSON) — **БЕЗ
> Go-реализации**. Тела хендлеров и раннер пишет developer; формы вход/выход, поток вызова,
> error-mapping и точку регистрации фиксирует этот документ.
>
> **Что отдаём ИИ-агенту (одно предложение):** новый MCP-инструмент `execute_command`, позволяющий
> аутентифицированному агенту выполнить НЕинтерактивную команду «бинарь + аргументы» на хосте `raxd`
> (без shell, с таймаутом, лимитами вывода/входа, опциональным allowlist, контролируемыми cwd/env и
> аудитом каждого вызова без секретов) и получить структурированный результат (stdout/stderr/exit
> code/длительность/флаги усечения и таймаута) — поверх уже готовых TLS-транспорта и MCP-сервера.

> **Связь с `specs/mcp-server/mcp-spec.md` (§10 «Точки расширения»).** Тот документ предсказал ровно
> эту задачу: «`command-exec` добавляет инструмент в ТОТ ЖЕ сервер БЕЗ изменения транспорта, маршрута
> `/mcp`, middleware-цепочки». Здесь это исполняется — с одним сознательным отступлением от §10 п.3:
> `execute_command` **НЕ** оборачивается generic `withAudit`, а ведёт собственный аудит в handler
> (ADR-004/SR-57). Это единственное отличие от шаблона `ping`/`server_info`; обосновано ниже (§2.3).

---

## 0. Версии (spec/SDK) — наследуются без изменений

- **Версия спецификации MCP:** `2025-11-25` — БЕЗ изменений (та же, что у `mcp-server`; ADR-002,
  `internal/mcp/tools.go:protocolVersion`). `execute_command` не меняет `protocolVersion` и не трогает
  согласование версии — это забота SDK и наследуемого `mcp-server`.
- **SDK:** `github.com/modelcontextprotocol/go-sdk/mcp` — официальный, тот же, что зарегистрировал
  `ping`/`server_info` (MCP-INTEGRATION предписывает официальный SDK; community `mark3labs/mcp-go` НЕ
  используется). Новых SDK/внешних зависимостей command-exec НЕ вводит (SR-67; plan §Trade-offs).
- **Транспорт SDK:** тот же `mcp.NewStreamableHTTPHandler(...)` → `http.Handler` на маршруте `/mcp`;
  `Stateless:true`, `JSONResponse:true` (как в `internal/mcp/server.go`). Не пересоздаётся.
- **Статус ADR (контекст принятия решений).** Спека опирается на `decisions/ADR-001..004` со статусом
  **accepted** — они ратифицированы гейтами **security-guardian + architect-guardian**. Связанные
  отклонения от буквы baseline — **П-1** (формат аудита logfmt вместо JSON), **П-2** (политика root =
  WARN-дефолт + обязательный `exec.deny_root`), **П-3** (логирование argv дословно) — **приняты
  security** в `threat-model.md` (red line 4: владелец baseline фиксирует риск+обоснование+смягчение).
  Ниже эти решения трактуются как **принятые контрактные**, а не как предложения на рассмотрении.

---

## 1. Транспорт

- **Тип:** **Streamable HTTP поверх TLS 1.3** (НЕ stdio — `raxd` обслуживает удалённых сетевых
  MCP-клиентов; MCP-INTEGRATION §«Транспорт»). Наследуется от `tls-transport`/`mcp-server` БЕЗ
  изменений (SR-40; spec AC1).
- **Эндпоинт (единый):** `https://127.0.0.1:<port>/mcp` — тот же путь, порт, TLS и слушающий сокет, что
  у `ping`/`server_info`. Отдельного не-`/mcp` сетевого эндпоинта исполнения и CLI-подкоманды
  `raxd exec` НЕТ (SR-40; spec AC1, Out of Scope; threat-model R-1). Регистрация инструмента НЕ
  добавляет маршрутов и слушающих сокетов.
- **HTTP-семантика:** идентична `mcp-server/mcp-spec.md §1.1` (POST = JSON-RPC request → `application/
  json`; notification → 202; GET `/mcp` → 405; невалидная `MCP-Protocol-Version` → 400). `execute_command`
  её НЕ меняет — это та же `StreamableHTTPHandler`-обвязка. v1 stateless: вывод команды отдаётся целиком
  ПОСЛЕ завершения (с учётом лимитов), без server→client SSE-стриминга (spec Out of Scope).
- **Аутентификация/Origin/Host наследуются** транспортными middleware ДО `/mcp`-handler (см. §2):
  exec-слой своего канала аутентификации НЕ вводит и `keystore.Verify` НЕ вызывает (SR-41; наследует
  SR-27/SR-28).

---

## 2. Аутентификация и поток вызова (end-to-end)

Каждый вызов `execute_command` проходит ту же наследуемую цепочку, что и `ping`/`server_info`:
**аутентификация → Origin/Host → rate-limit → транспортный аудит соединения → SDK-dispatch →
execHandler → cmdexec.Run → собственный exec-аудит результата**. Первые звенья — наследуемый транспорт
(`tls-transport`/`mcp-server`, НЕ переписывается; SR-40/SR-41/SR-42). Последние — новый exec-слой
(`internal/mcp/exec_tool.go` + `internal/cmdexec`).

```
TLS 1.3 (внешний слой, MinVersion TLS13; насл. SR-1/SR-2)
  └─ bodyLimit            (лимит тела запроса MaxBodyBytes ~1 MiB; внешняя граница argv-DoS; насл. SR-24)
     └─ recover           (паника tool-хендлера → 500, сервер жив; насл.; страховка SR-64)
        └─ Host/Origin    (Origin present&invalid → 403; Host вне allowlist → 403; насл. SR-14/SR-16)
           └─ auth        (Bearer → keystore.Verify constant-time; нет/неизвестен/отозван → 401;
              │             ErrCorrupt → 403; success → fingerprint+remote в ctx; SR-41 насл. SR-27/SR-28)
              └─ rate-limit (per-key + per-IP token bucket; превышение → 429 ДО handler; SR-42 насл. SR-17)
                 └─ authSuccessAudit (транспортный success-аудит СОЕДИНЕНИЯ; насл. SR-19)
                    └─ mux → /mcp  (StreamableHTTPHandler от SDK; Stateless, JSONResponse)
                       └─ SDK диспетчеризует JSON-RPC по method:
                          ├─ initialize / tools/list / tools/call (прочие методы — как в mcp-server)
                          ├─ неизвестный метод → JSON-RPC −32601 (SDK ErrMethodNotFound; команда не запускается)
                          └─ tools/call:
                                ├─ неизвестное имя инструмента (напр. "exec") → JSON-RPC −32602
                                │     (SDK jsonrpc.CodeInvalidParams, server.go:749; команда не запускается)
                                └─ name="execute_command":
                                ├─ [SDK] unmarshal ExecInput + валидация по inputSchema
                                │        (additionalProperties:false, типы) → невалидно: isError:true
                                │        (НЕ доходит до execHandler; §4 строка #9 «лишнее/битый тип»)
                                └─ execHandler(execCfg, audit)  [БЕЗ generic withAudit — ADR-004/SR-57]
                                     ├─ fingerprint = server.FingerprintFromContext(ctx)   (НЕ тело ключа)
                                     ├─ remote      = server.RemoteAddrFromContext(ctx)
                                     ├─ root-детекция: os.Geteuid()==0 → отдельная WARN-аудит-запись
                                     │   Result:"warn" КАЖДЫЙ вызов (SR-55); если cfg.DenyRoot → доп. deny (SR-56)
                                     ├─ ВХОДНЫЕ ЛИМИТЫ ДО запуска (SR-51 уже сделал SDK; SR-52/SR-46 здесь):
                                     │   len(args)>MaxArgs | len(arg)>MaxArgLen | TimeoutMs>MaxTimeoutMs
                                     │     → isError:true + exec-аудит Result:"deny" (команда НЕ запущена)
                                     ├─ резолв cwd (пусто→DefaultCwd) и timeout (0→DefaultTimeoutMs)
                                     ├─ ctx2 = context.WithTimeout(ctx, eff)  → cmdexec.Run(ctx2, cfg, in)
                                     │     ├─ allowlist строго ДО LookPath → deny → ErrNotAllowed (SR-48)
                                     │     ├─ валидация cwd (Stat+IsDir) → ErrBadCwd (SR-50)
                                     │     ├─ exec.CommandContext без shell; ErrDot/ErrNotFound (SR-43/44/45)
                                     │     ├─ Setpgid + Cancel→killGroup + WaitDelay (SR-47)
                                     │     ├─ Env=whitelist; Dir=cwd; capped-writers вывода (SR-49/53)
                                     │     └─ Result{Stdout,Stderr,ExitCode,Duration,TimedOut,*Truncated}
                                     ├─ маппинг Result→ExecOutput + Content(text-итог) (§3, §5)
                                     └─ exec-аудит РОВНО один раз основной (success | deny | fail; SR-57/58),
                                          плюс отдельная "warn"-запись при euid==0 (SR-55):
                                          AuditRecord{Tool:"execute_command", Result, Command, Args,
                                          ExitCode, Duration, TimedOut, Fingerprint, RemoteAddr, Reason}
                                          → AuditFn → writeAudit (logfmt; ADR-002/SR-59/60)
```

> **Примечание про отмену контекста (AC6 / SR-47) — обрыв соединения, не только таймаут.** Завершение
> дерева процессов срабатывает на ЛЮБУЮ отмену переданного `ctx` (`cmd.Cancel`/`killGroup`), а не только
> по таймауту. Есликлиент обрывает HTTP-соединение во время исполнения, наследуемый транспорт отменяет
> request-context → `cmdexec.Run` через `cmd.Cancel`→`killGroup` (`syscall.Kill(-pgid, SIGKILL)`) +
> ненулевой `cmd.WaitDelay` гарантированно убивает головной процесс И всё его дерево потомков. Результат
> клиенту в этом случае НЕ возвращается (соединения нет), но **developer ОБЯЗАН обеспечить отсутствие
> осиротевших процессов** после возврата (AC6-тест: ни одного живого PID группы; SR-47/ADR-001).

### 2.1. Где какой отказ (карта кодов; расширяет таблицу `mcp-server/mcp-spec §2.1`)

| Условие | Код / форма | Слой | SR / AC |
|---|---|---|---|
| Нет `Authorization: Bearer` / неизвестный / отозванный ключ | **401** | transport auth | SR-41 (насл. SR-27) / AC12 |
| Повреждение keystore в рантайме (`ErrCorrupt`) | **403** | transport auth | SR-41 (насл. SR-27) / AC12 |
| `Origin` present И вне allowlist; Host вне allowlist | **403** | transport host/origin | насл. SR-14/SR-16 |
| Превышение rate-limit (per-key/per-IP) | **429** | transport rate-limit | SR-42 (насл. SR-17) / AC16 |
| Битый JSON / невалидный JSON-RPC request | **JSON-RPC −32700 / −32600** | MCP (SDK) | SR-64 (насл. SR-30) / AC17 |
| Неизвестный JSON-RPC метод | **JSON-RPC −32601** (SDK `ErrMethodNotFound`) | MCP (SDK) | SR-64 (насл. SR-30) / AC17 |
| Неизвестный инструмент в `tools/call` (напр. `exec` вместо `execute_command`) | **JSON-RPC −32602** (SDK `jsonrpc.CodeInvalidParams`, `server.go:749`) | MCP (SDK) | SR-64 (насл. SR-30) / AC17 |
| Ошибка валидации ВВОДА `execute_command` (лишнее поле, неверный тип) | **`isError:true`** в `CallToolResult` | MCP (SDK, до handler) | SR-51/SR-64 / AC3/AC17 |
| deny по allowlist / превышение входных лимитов / `deny_root` | **`isError:true`** + exec-аудит `deny` | execHandler/cmdexec | SR-48/SR-52/SR-56 / AC7 |
| несуществующий бинарь (`ErrNotFound`/`ErrDot`) / невалидный cwd (`ErrBadCwd`) | **`isError:true`** + exec-аудит `fail` | execHandler/cmdexec | SR-44/SR-45/SR-50 / AC8/AC10 |
| превышение `timeout_ms` сверх `max_timeout_ms` | **`isError:true`** + exec-аудит `deny` (НЕ запущено) | execHandler | SR-46 / AC5 |
| демон работает от root (`euid==0`), `deny_root=false` | **НЕ отказ**: команда выполняется + отдельная exec-аудит `warn` | execHandler | SR-55 / AC9 |
| **Команда завершилась с ненулевым exit code** | **НЕ ошибка**: `isError` отсутствует/false, `exit_code != 0` в результате | execHandler | SR-64 / AC4 |
| **Команда прервана по таймауту** | **НЕ ошибка**: `isError` отсутствует/false, `timed_out:true` + частичный вывод | execHandler/cmdexec | SR-46 / AC5 |
| Паника tool-хендлера (теоретически; раннер не паникует) | **500**, сервер жив | transport recover | SR-64 (насл. SR-30) |

**Важно (SR-40/SR-41/SR-42):** при любом отказе транспортного звена (401/403/429) запрос **НЕ
доходит** до SDK-диспетчера — `execute_command` НЕ вызывается, команда НЕ запускается, аудит-DENY/FAIL/
RATE пишет транспорт. exec-аудит появляется ТОЛЬКО когда запрос дошёл до handler (§2.3).

### 2.2. Граница «ошибка протокола vs ошибка инструмента» (критично — SR-64, AC5/AC7/AC8/AC17)

Подтверждено по завендоренному SDK (`mcp/tool.go` §«ToolHandlerFor», `mcp/server.go:315-354` и
`server.go:743-752`):

- **Protocol error (JSON-RPC `error`, НЕ HTTP)** — формирует SDK на уровне диспетчера ДО инструмента.
  Это РАЗНЫЕ ситуации с РАЗНЫМИ кодами (developer/qa берут код для тестов отсюда — он однозначен):
  - битый JSON → **−32700** (Parse error);
  - не валидный JSON-RPC request → **−32600** (Invalid Request);
  - **неизвестный JSON-RPC метод** → **−32601** (Method not found; SDK `ErrMethodNotFound`);
  - **неизвестный инструмент в `tools/call`** → **−32602** (Invalid params; SDK `jsonrpc.CodeInvalidParams`,
    `server.go:748-750` возвращает `unknown tool %q`).
  Никакая команда НЕ исполняется.
- **Tool Execution Error (`isError:true` ВНУТРИ `result`)** — два источника:
  1. **Валидация ВВОДА самим SDK ДО handler** (`server.go:322-327`/`329-337`): нарушение `inputSchema`
     (лишнее поле при `additionalProperties:false`, неверный тип) → SDK кладёт `CallToolResult.SetError`
     с `IsError:true`. Команда НЕ запускается, handler НЕ вызывается → **exec-аудит НЕ пишется** для
     этой ветки (handler до неё не дошёл; запись отсутствует — это нормально, см. примечание ниже).
  2. **`error`, возвращённый execHandler/cmdexec** (`server.go:345-353`): обычный `error` (не
     `*jsonrpc.Error`) пакуется в `CallToolResult` с `IsError:true`. Сюда попадают deny по allowlist,
     `ErrNotFound`/`ErrDot` (несуществующий бинарь), `ErrBadCwd`, превышение входных лимитов/таймаута,
     `deny_root`.
- **НЕ ошибка** (результат `isError:false`, обычный `result`): ненулевой `exit_code` команды и таймаут
  исполнения (`timed_out:true`). Это нормальные исходы — агент видит их в structuredContent и решает
  сам (rich output, agent-native). Раннер НИКОГДА не паникует (SR-64).

> **Примечание про exec-аудит и SDK-валидацию входа.** Ветка (1) (нарушение `inputSchema`) перехватывается
> SDK ДО `execHandler`, поэтому exec-аудит-запись по ней НЕ пишется (handler не вызван). Это согласуется
> с SR-57 («handler пишет запись во всех СВОИХ ветках») и SR-51 (отвержение лишних полей — на уровне SDK,
> не handler). Записи о таком отказе достаточно на транспортном уровне (соединение зафиксировано
> `authSuccessAudit`). developer/qa: тест SR-51 проверяет `isError:true` + «команда не запущена», НЕ
> наличие exec-аудит-записи. Если security потребует фиксировать и эту ветку в exec-аудите — это
> потребует низкоуровневого `ToolHandler` вместо `ToolHandlerFor` (см. Открытые вопросы Q-EXEC-3).

### 2.3. Аудит вызова — собственный, в execHandler (ADR-004 / SR-57 / SR-58)

`execute_command` — **единственный** инструмент с собственным аудит-путём (асимметрия с
`ping`/`server_info`, которые остаются на generic `withAudit`; ADR-004, статус accepted). Причины (из
ADR-004): generic `withAudit` (`internal/mcp/audit.go`) пишет запись ПОСЛЕ handler и видит только tool
name + fingerprint + remote + success/fail — он НЕ имеет доступа к типизированным полям `ExecOutput`
(exit code, duration, truncated) и не различает deny внутри `cmdexec.Run`; оставить его + писать запись
в handler = ДВОЙНАЯ запись.

Контракт (handler пишет РОВНО одну ОСНОВНУЮ exec-запись за вызов + отдельную `warn`-запись при euid==0):

- **fingerprint** = `server.FingerprintFromContext(ctx)` (12 hex, необратим; «-» если ключа нет; тело
  ключа exec-слою недоступно — SR-62);
- **remote** = `server.RemoteAddrFromContext(ctx)` (IP:port без DNS).

#### 2.3.1. Два РАЗНЫХ уровня: значение поля `AuditRecord.Result` vs рендер в logfmt (FINDING-NEW-1)

Нужно строго различать (это снимает рассогласование между нормативными таблицами и примерами §6):

- **(а) Значение Go-поля `AuditRecord.Result`, которое выставляет `execHandler`** — это ВХОД в
  `writeAudit`. Допустимые значения теперь: **`"success"` / `"deny"` / `"fail"` / `"warn"` /
  `"rate-limited"`** (как определено в `internal/server/audit.go`; `"rate-limited"` — транспортная
  ветка, не exec). Таймаут — это `Result:"success"` + `TimedOut:true` (НЕ отдельное значение
  `"timed_out"`). `"warn"` — root-предупреждение при euid==0 (отдельная запись помимо основной).
- **(б) Как `writeAudit` (`internal/server/audit.go`) РЕНДЕРИТ это поле в logfmt-строку лога** —
  через `switch rec.Result` в РАЗНЫЕ метки `msg` и РАЗНЫЙ набор ключей:

| `AuditRecord.Result` (поле, вход) | `level` / `msg` (рендер) | Ключ `result=` в логе | Ключ `reason=` в логе | Источник |
|---|---|---|---|---|
| `"success"` (+ `Tool!=""`) | `INFO` / `msg=MCP` | **есть: `result=ok`** (буквально «ok») | нет | `audit.go` case "success" |
| `"warn"` (exec) | `WARN` / `msg=WARN` | **нет** | есть: `reason=running-as-root..` | `audit.go:117-136` case "warn" |
| `"deny"` | `WARN` / `msg=DENY` | **нет** | есть: `reason=..` | `audit.go` case "deny" |
| `"fail"` | `WARN` / `msg=FAIL` | **нет** | есть: `reason=..` | `audit.go` case "fail" |
| (`"rate-limited"`, не exec) | `WARN` / `msg=RATE` | нет | есть: `reason=..` | `audit.go` case "rate-limited" |

**Вывод для developer/qa:** в Go-коде `execHandler` выставляет `Result:"success"|"deny"|"fail"|"warn"`;
в ЛОГЕ для деления веток смотрят на **метку `msg`** (`MCP`/`WARN`/`DENY`/`FAIL`), а НЕ на ключ
`result=`. Ключ `result=ok` присутствует ТОЛЬКО в success-ветке (`msg=MCP`). У warn/deny/fail ключа
`result=` НЕТ — там метка в `msg` и текст в `reason=`. **Не писать в логе `result=deny`/`result=fail`/
`result=warn`** как ключ — текущий `writeAudit` так НЕ рендерит.

#### 2.3.2. Ветки exec-аудита (поле `AuditRecord.Result` + рендер msg)

| Ветка | Триггер | `AuditRecord.Result` (поле) | Рендер в логе (`writeAudit`) | SR |
|---|---|---|---|---|
| **success** | команда выполнилась (любой exit code) | `"success"` (+ `Tool:"execute_command"`, `Command`, `Args`, `ExitCode`, `Duration`, `TimedOut:false`, `Fingerprint`, `RemoteAddr`) | `INFO MCP … result=ok … timed_out=false` | SR-57/SR-58 |
| **success (таймаут)** | команда прервана по таймауту | **`"success"`** (НЕ `"timed_out"`) + `TimedOut:true` (+ `Command`, `Args`, `ExitCode`, `Duration`, `Fingerprint`, `RemoteAddr`) | `INFO MCP … result=ok … timed_out=true` | SR-46/SR-57 |
| **warn (root-предупреждение)** | `os.Geteuid()==0` при ЛЮБОМ вызове (отдельная запись помимо основной; reason=`running-as-root`) | `"warn"` (+ `Tool:"execute_command"`, `Command`, `Args`, `Reason`, `Fingerprint`, `RemoteAddr`; без exit/duration) | `WARN WARN fp=.. remote=.. tool=execute_command reason=running-as-root command=.. args=..` (ключа `result=` нет) | SR-55 / AC9 |
| **deny** | allowlist deny / `len(args)>MaxArgs` / `len(arg)>MaxArgLen` / `TimeoutMs>MaxTimeoutMs` / `deny_root` | `"deny"` (+ `Command`, `Args`, `Reason`, `Fingerprint`, `RemoteAddr`; без exit/duration — не запускалось) | `WARN DENY … reason=.. command=.. args=..` (ключа `result=` нет) | SR-48/SR-52/SR-56/SR-57/SR-58 |
| **fail** | несуществующий бинарь (`ErrNotFound`/`ErrDot`) / невалидный cwd (`ErrBadCwd`) | `"fail"` (+ `Command`, `Args`, `Reason`, `Fingerprint`, `RemoteAddr`) | `WARN FAIL … reason=.. command=.. args=..` (ключа `result=` нет) | SR-44/SR-45/SR-50/SR-57/SR-58 |

> **Двойная запись при euid==0 (SR-55/SR-56) — две ОТДЕЛЬНЫЕ записи за вызов.** При `euid==0` ВСЕГДА
> пишется `"warn"`-запись (root-предупреждение), затем:
> - **`deny_root=false`** (дефолт): команда ВЫПОЛНЯЕТСЯ → дальше идёт ОСНОВНАЯ запись исхода
>   (`success`/`fail`/таймаут). Итого две записи: `warn` + основная.
> - **`deny_root=true`**: пишется `"warn"`, ЗАТЕМ `"deny"` (причина «execution as root forbidden by
>   policy»), команда НЕ запускается. Итого две записи: `warn` + `deny` (порядок: сначала `warn`,
>   потом `deny`).
> При euid≠0 `"warn"`-записи нет — только одна основная запись (success/deny/fail).

> **Новые exec-поля логируются writeAudit во ВСЕХ exec-ветках.** `command=`/`args=` пишутся и в success,
> и в warn, и в deny, и в fail (вместе с `reason=` у warn/deny/fail); `exit_code=`/`duration=`/
> `timed_out=` — там, где заполнены (success/таймаут; у warn/deny/fail команда не запускалась → их нет).
> Все exec-поля логируются ТОЛЬКО при `Tool=="execute_command"` — формат не-exec записей
> (`AUTH`/`FAIL`/`DENY`/`RATE`/`WARN`/`MCP`-ping) НЕ ломается (SR-59; наследуемые тесты
> `tls-transport`/`mcp-server` зелёные).

- **Формат:** строгий logfmt через `writeAudit` (тот же канал/логгер, что транспорт/MCP; ADR-002/
  SR-59/SR-60).
- **Без секретов (SR-62):** ни в `stdout`/`stderr`-результате, ни в тексте `isError`, ни в exec-аудите
  НЕТ тела API-ключа/хэша/соли/raw `Authorization`/приватного TLS-ключа — вместо ключа fingerprint;
  сообщения об ошибках нейтральны.
- **Аргументы — дословно (SR-63/П-3, принято security):** `args` в аудите соответствуют присланным (без
  маскирования — надёжное определение секрета в произвольном argv невозможно, полнота аудита критична).
  Компенсирующий контроль — предупреждение оператору в доке (обязанность tech-writer): «не передавайте
  секреты в argv».

---

## 3. Capabilities / tools/list

- **protocolVersion:** `2025-11-25` — БЕЗ изменений (§0). `execute_command` не влияет на согласование
  версии.
- **capabilities:** по-прежнему объявляется ТОЛЬКО `tools`. Resources, Prompts, Sampling, Logging,
  Completions — НЕ объявляются (см. §7). Никаких новых ресурсов/промптов command-exec НЕ вводит.
- **tools/list возвращает теперь ТРИ инструмента:** `ping`, `server_info`, **`execute_command`**
  (SR-40; spec AC1). Регистрация — той же точкой расширения `sdkmcp.AddTool` в `NewHandler`
  (`internal/mcp/server.go`), рядом с `ping`/`server_info`. Список статичен (`tools.listChanged:false`).

**Фрагмент ответа `tools/list` (целевая форма для `execute_command`; точную сериализацию даёт SDK из
Go-структур `ExecInput`/`ExecOutput`, developer обязан добиться именно этих форм, qa проверяет):**

```json
{
  "name": "execute_command",
  "description": "Выполнить НЕинтерактивную команду на хосте raxd формой «бинарь + список аргументов» (БЕЗ shell: метасимволы ;|$()&&>` трактуются как литеральные аргументы). Возвращает stdout/stderr/код возврата/длительность/флаги усечения и таймаута. Ограничения: обязательный таймаут (по умолчанию из конфига, есть жёсткий максимум); вывод и аргументы лимитированы; рабочая директория и окружение контролируются сервером; при включённом allowlist разрешены только перечисленные команды. НЕ принимает переменные окружения от клиента. Каждый вызов проходит аутентификацию и аудит.",
  "inputSchema": {
    "type": "object",
    "additionalProperties": false,
    "required": ["command"],
    "properties": {
      "command": {
        "type": "string",
        "description": "Имя бинаря (резолвится по серверному PATH через LookPath) или абсолютный путь. Относительный путь из текущего каталога отвергается (exec.ErrDot)."
      },
      "args": {
        "type": "array",
        "items": { "type": "string" },
        "description": "Аргументы команды (опц.). Передаются как литеральные argv (без shell-интерпретации). Логируются в аудите дословно — не передавайте секреты в argv."
      },
      "timeout_ms": {
        "type": "integer",
        "description": "Таймаут в миллисекундах (опц.). 0/отсутствие → дефолт из конфига. Значение сверх жёсткого максимума из конфига → отказ (команда не запускается)."
      },
      "cwd": {
        "type": "string",
        "description": "Рабочая директория (опц.). Пусто → безопасный дефолт из конфига. Должна существовать и быть каталогом, иначе отказ."
      }
    }
  },
  "outputSchema": {
    "type": "object",
    "additionalProperties": false,
    "required": ["stdout", "stderr", "exit_code", "duration_ms", "timed_out", "stdout_truncated", "stderr_truncated"],
    "properties": {
      "stdout": { "type": "string", "description": "Захваченный stdout (обрезан до max_output_bytes; см. stdout_truncated)." },
      "stderr": { "type": "string", "description": "Захваченный stderr (обрезан до max_output_bytes; см. stderr_truncated)." },
      "exit_code": { "type": "integer", "description": "Код возврата процесса. Ненулевой — НЕ ошибка инструмента." },
      "duration_ms": { "type": "integer", "description": "Длительность исполнения в миллисекундах." },
      "timed_out": { "type": "boolean", "description": "true — команда прервана по таймауту (kill всего дерева процессов); вывод частичный." },
      "stdout_truncated": { "type": "boolean", "description": "true — stdout достиг лимита и обрезан." },
      "stderr_truncated": { "type": "boolean", "description": "true — stderr достиг лимита и обрезан." }
    }
  }
}
```

> Замечания по SDK-генерации схем (по завендоренному SDK):
> - **`additionalProperties:false` на `inputSchema` ГАРАНТИРУЕТСЯ инференцией SDK из Go-struct**
>   (подтверждено в vendor `github.com/google/jsonschema-go/jsonschema/infer.go:245-248`: для
>   `reflect.Struct` устанавливается `s.AdditionalProperties = falseSchema()`). **developer НЕ обязан
>   выставлять `additionalProperties` явно** в дескрипторе `Tool` — достаточно объявить `ExecInput` как
>   struct. **qa ОБЯЗАН закрепить тестом** «лишнее поле → isError, команда не запущена» как регрессию
>   против возможных изменений SDK в `vendor/` (SR-51). Контекст: дефолтное поведение
>   `additionalProperties` обсуждалось в апстриме (issue #892) — это потенциальный будущий регресс,
>   а не текущая проблема; тест-регрессия его покрывает.
> - `command` обязателен (поле без `omitempty`); `args`/`timeout_ms`/`cwd` опциональны (`omitempty`) →
>   `required:["command"]`.
> - `outputSchema` объявляется (результат структурный) — SDK валидирует `structuredContent` против неё
>   (`server.go:380`). Семь полей точно соответствуют AC4/SR-65.
> - Поля `env` в схеме НЕТ (SR-49/SR-51; spec AC3 Out of Scope) — приём окружения от клиента отклонён.

---

## 4. Error mapping (таблица «ситуация → код/isError → что в ответе») — критично

Сводная таблица (источник истины по разделению — SDK `mcp/server.go:315-354` и `743-752`, §2.2). Коды в
этой таблице ОДНОЗНАЧНЫ — developer/qa берут их для тестов напрямую. Колонка «exec-аудит» указывает
ЗНАЧЕНИЕ поля `AuditRecord.Result` (его рендер в логе — §2.3.1: success→`msg=MCP result=ok`,
warn→`msg=WARN reason=`, deny→`msg=DENY reason=`, fail→`msg=FAIL reason=`):

| # | Ситуация | Где ловится | Код / `isError` | Что в ответе клиенту | `AuditRecord.Result` | SR / AC |
|---|---|---|---|---|---|---|
| 1 | Нет/неизвестный/отозванный Bearer-ключ | transport auth | **HTTP 401** | транспортный отказ, до MCP не доходит | нет (транспорт) | SR-41 / AC12 |
| 2 | `ErrCorrupt` keystore | transport auth | **HTTP 403** | транспортный отказ | нет | SR-41 / AC12 |
| 3 | `Origin` present&invalid / Host вне allowlist | transport | **HTTP 403** | транспортный отказ | нет | насл. SR-14/16 |
| 4 | Превышение rate-limit | transport | **HTTP 429** | транспортный отказ | нет (RATE транспорта) | SR-42 / AC16 |
| 5 | Битый JSON | SDK | **JSON-RPC −32700** | `error{code,message}` | нет | SR-64 / AC17 |
| 6 | Не валидный JSON-RPC request | SDK | **JSON-RPC −32600** | `error{code,message}` | нет | SR-64 / AC17 |
| 7 | Неизвестный JSON-RPC метод | SDK | **JSON-RPC −32601** (`ErrMethodNotFound`) | `error{code,message}` | нет | SR-64 / AC17 |
| 8 | Неизвестный инструмент в `tools/call` (опечатка имени) | SDK (`server.go:749`) | **JSON-RPC −32602** (`jsonrpc.CodeInvalidParams`) | `error{code,message:"unknown tool …"}` | нет | SR-64 / AC17 |
| 9 | Лишнее поле во входе (`additionalProperties:false`); неверный тип; присутствует `env` | SDK до handler | **`isError:true`** | `result{content:[text:"…validating arguments…"], isError:true}` | нет (handler не вызван — см. §2.2 примечание) | SR-51/SR-64 / AC3/AC17 |
| 10 | `len(args) > max_args` или `len(arg) > max_arg_len` | execHandler до запуска | **`isError:true`** | `result{content:[text:нейтральное], isError:true}` | `"deny"` → `msg=DENY reason=` | SR-52 / AC11 |
| 11 | `timeout_ms > max_timeout_ms` | execHandler до запуска | **`isError:true`** | `result{…, isError:true}` | `"deny"` → `msg=DENY reason=` | SR-46 / AC5 |
| 12 | `deny_root:true` И `euid==0` | execHandler до запуска | **`isError:true`** | `result{…, isError:true}` | `"warn"` + `"deny"` (две записи; §2.3.2) | SR-55/SR-56 / AC9 |
| 13 | `euid==0` И `deny_root:false` | execHandler | **НЕ отказ** (команда выполняется) | результат исхода команды (success/fail) | `"warn"` + основная (`success`/`fail`) | SR-55 / AC9 |
| 14 | allowlist включён И команда вне списка | cmdexec до LookPath | **`isError:true`** | `result{…, isError:true}` | `"deny"` → `msg=DENY reason=` | SR-48 / AC7 |
| 15 | несуществующий/недоступный бинарь (`ErrNotFound`) | cmdexec | **`isError:true`** | `result{content:[text:нейтральное "command not found"], isError:true}` | `"fail"` → `msg=FAIL reason=` | SR-45 / AC8 |
| 16 | относительный путь бинаря из cwd (`ErrDot`) | cmdexec | **`isError:true`** | `result{…, isError:true}` | `"fail"` → `msg=FAIL reason=` | SR-44 / AC2/AC8 |
| 17 | невалидный `cwd` (не существует / не каталог) (`ErrBadCwd`) | cmdexec до запуска | **`isError:true`** | `result{…, isError:true}` | `"fail"` → `msg=FAIL reason=` | SR-50 / AC10 |
| 18 | **ненулевой exit code команды** | execHandler | **НЕ ошибка** (`isError` отсутствует/false) | `result{content+structuredContent{exit_code≠0,…}}` | `"success"` → `msg=MCP result=ok` | SR-64 / AC4 |
| 19 | **таймаут исполнения** (команда дольше эффективного таймаута) | cmdexec | **НЕ ошибка** (`isError` отсутствует/false) | `result{…, structuredContent{timed_out:true, частичный вывод}}` | `"success"` + `TimedOut:true` → `msg=MCP result=ok timed_out=true` | SR-46 / AC5 |
| 20 | превышение лимита вывода (stdout/stderr) | cmdexec | **НЕ ошибка** | `result{…, structuredContent{stdout_truncated/stderr_truncated:true}}` | `"success"` → `msg=MCP result=ok` | SR-53 / AC11 |
| 21 | паника раннера (контрактно невозможна) | transport recover | **HTTP 500**, сервер жив | транспортный 500 | нет | SR-64 |

Краткая логика для developer:
- **Транспортная ошибка** = HTTP 401/403/429 (транспорт): неверный ключ/Origin/rate-limit. Команда не
  запускается; до SDK не доходит.
- **Протокольная ошибка (JSON-RPC `error`)** = −32700 (битый JSON) / −32600 (не request) / **−32601
  (неизвестный метод)** / **−32602 (неизвестный инструмент или неверная форма params)**. Команда не
  запускается; до execHandler не доходит.
- **`isError:true` в результате** = ошибка валидации входа (SDK, #9) ИЛИ `error` из execHandler/cmdexec
  (deny / несуществующий бинарь / невалидный cwd / превышение лимитов входа / `deny_root`). Сервер жив,
  следующий валидный вызов отрабатывает (SR-45/SR-64).
- **НЕ ошибка** = ненулевой exit code, `timed_out:true`, root-`warn` при `deny_root:false` — нормальный
  результат с `isError:false` (команда выполняется).

---

## 5. Инструмент `execute_command` (схемы вход/выход)

Принципы дизайна (agent-native / mcp-tool-design): `execute_command` — **примитив** (запусти бинарь),
а НЕ workflow (никакой бизнес-логики «что и как делать» в инструменте — это решает агент); вход —
**данные** (что запустить), не решения; имя описывает возможность; выход — **rich** (агент видит exit
code/вывод/таймаут/усечение и сам решает следующий шаг). `name` — латиница (стабильный идентификатор);
описания на русском (язык артефактов). Это «опасный примитив» (RCE уровня SSH), поэтому ограничения
безопасности §3 — НЕ опциональны и реализуются на сервере, а не в схеме.

### 5.1. Вход — `ExecInput`

| Поле | Тип | Обяз. | JSON-тег (контракт для developer) | Семантика / валидация |
|---|---|---|---|---|
| `command` | string | **да** | `json:"command"` | имя/путь бинаря. Пусто → `isError` (SDK: required). Относительный путь из cwd → `ErrDot` (SR-44) |
| `args` | []string | нет | `json:"args,omitempty"` | argv (литерально, без shell, SR-43). До запуска: `len(args)>max_args` или `len(arg)>max_arg_len` → deny (SR-52) |
| `timeout_ms` | integer | нет | `json:"timeout_ms,omitempty"` | 0/нет → `default_timeout_ms`. `>max_timeout_ms` → deny (SR-46) |
| `cwd` | string | нет | `json:"cwd,omitempty"` | пусто → `default_cwd`; задан → Stat+IsDir, иначе `ErrBadCwd` (SR-50) |

- **`additionalProperties:false`** (строгая схема): любое неизвестное поле → `isError` (SR-51; SDK до
  handler, §2.2). Гарантируется инференцией SDK из struct (`infer.go:245-248`); **закрепить тестом** как
  регрессию (research Q9): лишнее поле → `isError`, команда не запущена.
- **Поля `env` НЕТ** (SR-49/SR-51; spec AC3, Out of Scope; threat-model R-7/ОР-6): окружение задаётся
  ТОЛЬКО серверным whitelist; присланный `env` отвергается как лишнее поле (ветка #9).
- **Валидация входных лимитов ДО запуска** (SR-52/SR-46, ADR-003): `max_args` (дефолт 256),
  `max_arg_len` (дефолт 128 KiB), `max_timeout_ms` (дефолт 5 мин) — превышение → `isError:true` +
  exec-аудит `deny`, команда НЕ запускается (защита памяти демона до ядерного `E2BIG`; threat-model
  R-5/R-6). Часть этих проверок (число/длина) делает execHandler ПОСЛЕ SDK-валидации схемы.

**Целевая входная JSON-схема** — см. `inputSchema` в §3.

### 5.2. Выход — `ExecOutput` (AC4 / SR-65)

`execHandler` маппит `cmdexec.Result` → `ExecOutput` (structuredContent) + краткий text-блок (Content).

| Поле | Тип | JSON-тег | Семантика |
|---|---|---|---|
| `Stdout` | string | `json:"stdout"` | захваченный stdout (≤ `max_output_bytes`) |
| `Stderr` | string | `json:"stderr"` | захваченный stderr (≤ `max_output_bytes`) |
| `ExitCode` | int | `json:"exit_code"` | код возврата (ненулевой — НЕ ошибка) |
| `DurationMs` | int | `json:"duration_ms"` | длительность, мс |
| `TimedOut` | bool | `json:"timed_out"` | прерван по таймауту (kill дерева) |
| `StdoutTruncated` | bool | `json:"stdout_truncated"` | stdout достиг лимита |
| `StderrTruncated` | bool | `json:"stderr_truncated"` | stderr достиг лимита |

- **`content` (text-блок) — краткое РЕЗЮМЕ для модели** (не полный дамп): рекомендованная форма —
  `"exit=<code> duration=<ms>ms timed_out=<bool> stdout=<N>B stderr=<M>B[ truncated]"`. Полный вывод —
  в `structuredContent`. Резюме без секретов raxd (SR-62); вывод команды может содержать что угодно от
  процесса — это данные результата, не секреты raxd (тело ключа/TLS в выводе появиться не может — exec-слой
  к ним доступа не имеет).
- **`structuredContent`** = `ExecOutput` целиком (7 полей; SDK валидирует против `outputSchema`,
  `server.go:380`). Форма ОБЯЗАНА соответствовать §3 (SR-65; qa проверяет семь полей).
- **`isError`** отсутствует/false при успехе и при таймауте/ненулевом exit code (§4 #18/#19); true —
  только в ветках deny/fail (§4 #10–#17).

### 5.3. Ошибки инструмента (перечень — обязателен по red line «каждый tool: вход+выход+ошибки»)

Полная таблица — §4 (#1–#21). Кратко по источнику:
- **до tool (транспорт):** 401/403/429 — команда не запускается (SR-41/SR-42).
- **протокол (SDK):** −32700 (битый JSON) / −32600 (не request) / −32601 (неизвестный метод) / −32602
  (неизвестный инструмент) — команда не запускается (SR-64).
- **валидация входа (SDK):** лишнее поле/тип/`env` → `isError:true` (SR-51).
- **deny (execHandler/cmdexec):** allowlist / лимиты входа / `deny_root` → `isError:true` + аудит
  `Result:"deny"` (рендер `msg=DENY reason=`; SR-48/SR-52/SR-56).
- **fail (cmdexec):** несуществующий бинарь / `ErrDot` / невалидный cwd → `isError:true` + аудит
  `Result:"fail"` (рендер `msg=FAIL reason=`; SR-44/SR-45/SR-50). Сообщения нейтральны, без секретов
  (SR-45/SR-62).
- **warn (root-предупреждение, НЕ ошибка):** `euid==0` → отдельная аудит-запись `Result:"warn"` (рендер
  `msg=WARN reason=running-as-root`); при `deny_root:false` команда ВЫПОЛНЯЕТСЯ (SR-55).
- **НЕ ошибки:** ненулевой exit code, `timed_out:true`, усечение вывода → `isError:false` (SR-64/SR-46/
  SR-53).

---

## 6. Примеры (JSON-RPC tools/call) — согласованы с тем, что вернёт go-sdk

> Транспортно — POST на `https://127.0.0.1:<port>/mcp` с заголовками `Authorization: Bearer
> rax_live_…`, `Content-Type: application/json`, `Accept: application/json, text/event-stream`,
> `MCP-Protocol-Version: 2025-11-25`. Формы `result`/`error` соответствуют SDK (`ToolHandlerFor`).
> Аудит-строки ниже приведены в ТОЧНОМ соответствии с фактическим рендером `writeAudit`
> (`internal/server/audit.go`; см. §2.3.1): success→`msg=MCP … result=ok`; warn→`msg=WARN … reason=`
> (без `result=`); deny→`msg=DENY … reason=` (без `result=`); fail→`msg=FAIL … reason=` (без `result=`).

### 6.1. Успех (ненулевой exit code — НЕ ошибка)

Запрос:
```json
{
  "jsonrpc": "2.0",
  "id": 10,
  "method": "tools/call",
  "params": {
    "name": "execute_command",
    "arguments": { "command": "ls", "args": ["-la", "/nope"], "timeout_ms": 5000 }
  }
}
```
Ответ (команда выполнилась, но вернула код ≠ 0 → `isError:false`):
```json
{
  "jsonrpc": "2.0",
  "id": 10,
  "result": {
    "content": [
      { "type": "text", "text": "exit=2 duration=14ms timed_out=false stdout=0B stderr=41B" }
    ],
    "structuredContent": {
      "stdout": "",
      "stderr": "ls: cannot access '/nope': No such file or directory\n",
      "exit_code": 2,
      "duration_ms": 14,
      "timed_out": false,
      "stdout_truncated": false,
      "stderr_truncated": false
    },
    "isError": false
  }
}
```
`AuditRecord.Result = "success"`. Лог (рендер writeAudit):
`INFO MCP fp=<fp> remote=<ip> tool=execute_command result=ok command=ls args=[-la,/nope] exit_code=2 duration=14ms timed_out=false`
(формат logfmt; ключ `result=ok` есть только в success-ветке; точная сериализация массива `args` — за writeAudit/ADR-002).

### 6.2. Deny по allowlist (allowlist включён, команда вне списка)

Запрос:
```json
{
  "jsonrpc": "2.0",
  "id": 11,
  "method": "tools/call",
  "params": { "name": "execute_command", "arguments": { "command": "rm", "args": ["-rf", "/"] } }
}
```
Ответ (`isError:true`, команда НЕ запущена; SR-48):
```json
{
  "jsonrpc": "2.0",
  "id": 11,
  "result": {
    "content": [ { "type": "text", "text": "command not allowed" } ],
    "isError": true
  }
}
```
`AuditRecord.Result = "deny"`. Лог (рендер writeAudit — метка `DENY`, есть `reason=`, ключа `result=` НЕТ):
`WARN DENY fp=<fp> remote=<ip> tool=execute_command reason=not-allowed command=rm args=[-rf,/]`

### 6.3. Несуществующий бинарь (`isError:true`, сервер жив; SR-45/AC8)

Запрос:
```json
{
  "jsonrpc": "2.0",
  "id": 12,
  "method": "tools/call",
  "params": { "name": "execute_command", "arguments": { "command": "definitely-not-a-binary-xyz" } }
}
```
Ответ:
```json
{
  "jsonrpc": "2.0",
  "id": 12,
  "result": {
    "content": [ { "type": "text", "text": "command not found" } ],
    "isError": true
  }
}
```
`AuditRecord.Result = "fail"`. Лог (рендер writeAudit — метка `FAIL`, есть `reason=`, ключа `result=` НЕТ):
`WARN FAIL fp=<fp> remote=<ip> tool=execute_command reason=not-found command=definitely-not-a-binary-xyz`
Сообщение нейтрально (без путей/секретов; SR-45/SR-62). Следующий валидный вызов отрабатывает штатно.
(Невалидный `cwd` → `ErrBadCwd` рендерится так же: `WARN FAIL … reason=bad-cwd command=..`; §4 #17.)

### 6.4. Таймаут исполнения (`timed_out:true` — НЕ ошибка; SR-46/AC5)

Запрос:
```json
{
  "jsonrpc": "2.0",
  "id": 13,
  "method": "tools/call",
  "params": { "name": "execute_command", "arguments": { "command": "sleep", "args": ["60"], "timeout_ms": 1000 } }
}
```
Ответ (процесс и дерево убиты, частичный вывод, `isError:false`):
```json
{
  "jsonrpc": "2.0",
  "id": 13,
  "result": {
    "content": [ { "type": "text", "text": "exit=-1 duration=1003ms timed_out=true stdout=0B stderr=0B" } ],
    "structuredContent": {
      "stdout": "",
      "stderr": "",
      "exit_code": -1,
      "duration_ms": 1003,
      "timed_out": true,
      "stdout_truncated": false,
      "stderr_truncated": false
    },
    "isError": false
  }
}
```
`AuditRecord.Result = "success"` (таймаут НЕ ошибка) + `TimedOut:true`. Лог (рендер writeAudit — метка `MCP`, `result=ok`):
`INFO MCP fp=<fp> remote=<ip> tool=execute_command result=ok command=sleep args=[60] exit_code=-1 duration=1003ms timed_out=true`

> Примечание (для developer/qa): значение `exit_code` при убийстве сигналом не определено
> POSIX-однозначно (Go отдаёт −1 при `ExitCode()` для процесса, завершённого сигналом). Контракт
> результата — флаг `timed_out:true` (он определяющий), а `exit_code` при таймауте трактуется как
> «нерелевантен» (агент ориентируется на `timed_out`). Конкретное число (−1 / 137 / иное) — деталь
> реализации раннера, qa фиксирует только `timed_out:true` + частичный вывод (AC5).

### 6.5. Демон от root (`euid==0`) — отдельная `warn`-запись (SR-55/AC9)

При `euid==0` и `deny_root=false` команда ВЫПОЛНЯЕТСЯ как обычно (ответ клиенту — как в §6.1), но в
аудит пишутся ДВЕ записи за вызов: сначала `warn` (root-предупреждение), затем основная (success/fail).
Лог (рендер writeAudit — метка `WARN`, есть `reason=`, ключа `result=` НЕТ):
`WARN WARN fp=<fp> remote=<ip> tool=execute_command reason=running-as-root: raxd executing commands as root (euid==0); ensure raxd runs as non-root command=ls args=[-la]`
`INFO MCP fp=<fp> remote=<ip> tool=execute_command result=ok command=ls args=[-la] exit_code=0 duration=8ms timed_out=false`

При `deny_root=true` и `euid==0` команда НЕ выполняется; пишутся `warn`, затем `deny` (ответ клиенту —
`isError:true`; §4 #12):
`WARN WARN fp=<fp> remote=<ip> tool=execute_command reason=running-as-root.. command=ls args=[-la]`
`WARN DENY fp=<fp> remote=<ip> tool=execute_command reason=execution as root is forbidden by policy (deny_root=true) command=ls args=[-la]`

### 6.6. Неизвестный инструмент (для контраста — protocol error −32602, НЕ исполнение)

Если клиент опечатался в имени инструмента (`exec` вместо `execute_command`) — это **JSON-RPC −32602**
(SDK `jsonrpc.CodeInvalidParams`, `server.go:748-750`), команда НЕ исполняется. Это ОТЛИЧАЕТСЯ от
−32601 (неизвестный JSON-RPC метод): здесь метод (`tools/call`) валиден, но имя инструмента в `params`
неизвестно:
```json
{ "jsonrpc": "2.0", "id": 14, "error": { "code": -32602, "message": "unknown tool \"exec\"" } }
```

### 6.7. Лишнее поле во входе (валидация SDK → `isError:true`; SR-51/AC3)

Запрос с неизвестным полем `env` (и `shell`):
```json
{
  "jsonrpc": "2.0",
  "id": 15,
  "method": "tools/call",
  "params": { "name": "execute_command", "arguments": { "command": "echo", "env": {"X":"1"}, "shell": true } }
}
```
Ответ (SDK отверг по `additionalProperties:false` ДО handler; команда не запущена):
```json
{
  "jsonrpc": "2.0",
  "id": 15,
  "result": {
    "content": [ { "type": "text", "text": "validating \"arguments\": additional properties not allowed: env, shell" } ],
    "isError": true
  }
}
```
> Текст сообщения генерирует SDK (`server.go:325`) — приведён как ориентир; qa проверяет `isError:true`
> + «команда не запущена», не дословный текст. exec-аудит по этой ветке НЕ пишется (handler не вызван;
> §2.2 примечание).

---

## 7. Resources / Prompts

**None — command-exec НЕ вводит ресурсов и промптов** (SR-40; spec AC1 — поверхность только tool
`execute_command`). Обоснование: задача — единственный новый инструмент за наследуемой цепочкой;
никакого нового read-only контекста (resource) или шаблона (prompt) AC не требуют. capability
`resources`/`prompts` в `initialize` НЕ объявляется (как и в `mcp-server`, §3). Потенциальный будущий
resource `capabilities` (отдать агенту: активен ли allowlist, лимиты, дефолтный таймаут) — НЕ в scope
этой задачи (отдельной задачей через capability-негоциацию; те же правила «без секретов» — не отдавать
пути/порт/окружение, см. `mcp-server/mcp-spec §8`).

---

## 8. Точка интеграции (для developer — без реализации, только формы; источник истины — plan §Contracts)

> Дублирует plan §Contracts в терминах MCP-дизайна. Сигнатуры Go — в `plan.md`.

- **Регистрация (та же точка, что `ping`/`server_info`):** в `internal/mcp/server.go:NewHandler` рядом с
  существующими `sdkmcp.AddTool(s, pingTool(), withAudit(...))` добавить
  `sdkmcp.AddTool(s, execTool(), execHandler(execCfg, audit))` — **БЕЗ** `withAudit` (ADR-004/SR-57).
  Сигнатура `NewHandler` расширяется параметром `execCfg cmdexec.Config`:
  `NewHandler(ver string, audit server.AuditFn, execCfg cmdexec.Config) (http.Handler, error)`. Все
  вызовы (`internal/cli/serve.go:87`, mcp-тесты) обновляются.
- **`execTool() *sdkmcp.Tool`** — дескриптор: `Name:"execute_command"`, `Description:` (как §3; для
  ИИ-агента: что делает + ключевые ограничения). Схемы вход/выход SDK выводит из `ExecInput`/`ExecOutput`.
- **`execHandler(cfg cmdexec.Config, audit server.AuditFn) sdkmcp.ToolHandlerFor[ExecInput, ExecOutput]`**
  — адаптер MCP↔cmdexec: достаёт fingerprint/remote из ctx; root-детекция (SR-55/SR-56: при euid==0 —
  `warn`-запись, при `deny_root` ещё и `deny`); входные лимиты (SR-52/SR-46); резолв cwd/timeout;
  `context.WithTimeout`; `cmdexec.Run`; маппинг `Result`→`ExecOutput` + Content; собственный exec-аудит
  (success/deny/fail + warn; ADR-004/SR-57/SR-58). Возврат `error` из handler → SDK пакует в
  `isError:true` (server.go:345-353).
- **`AuditRecord` / `writeAudit` (расширение, ADR-002/SR-59) — разграничение «поле Result vs рендер»:**
  - **Поле `AuditRecord.Result`**, которое выставляет `execHandler`, принимает значения
    `"success"` / `"deny"` / `"fail"` / `"warn"` (вход в `writeAudit`; `"rate-limited"` — транспортная
    ветка, не exec). Таймаут — это `Result:"success"` + `TimedOut:true` (не отдельное `"timed_out"`);
    `"warn"` — отдельная root-запись помимо основной (§2.3.1/§2.3.2).
  - **`writeAudit` РЕНДЕРИТ** это поле через существующий `switch rec.Result` в метку `msg` + набор
    ключей (НЕ менять рендер не-exec записей): `success`(+`Tool!=""`) → `level=INFO msg=MCP … result=ok`;
    `warn` → `level=WARN msg=WARN … reason=` (БЕЗ `result=`); `deny` → `level=WARN msg=DENY … reason=`
    (БЕЗ `result=`); `fail` → `level=WARN msg=FAIL … reason=` (БЕЗ `result=`). developer **РАСШИРЯЕТ
    существующий `writeAudit`**, добавляя новые exec-поля во ВСЕ exec-ветки (включая case "warn"),
    сохраняя метки и существующий рендер не-exec записей (AC14/SR-59).
  - **Новые опц. поля `AuditRecord`:** `Command string`, `Args []string`, `ExitCode *int`,
    `Duration time.Duration`, `TimedOut bool`. Логируются (`command=`/`args=`/`exit_code=`/`duration=`/
    `timed_out=`) ТОЛЬКО при `Tool=="execute_command"`: `command=`/`args=` — во всех exec-ветках
    (success/warn/deny/fail); `exit_code=`/`duration=`/`timed_out=` — где заполнены (success/таймаут; у
    warn/deny/fail команда не запускалась). Не-exec записи (AUTH/FAIL/DENY/RATE/WARN/MCP-ping) формат
    сохраняют — наследуемые тесты `tls-transport`/`mcp-server` зелёные.
- **Контекстные обёртки (готовы):** `server.FingerprintFromContext(ctx)` (`internal/server/auth.go:36`),
  `server.RemoteAddrFromContext(ctx)` (`auth.go:56`) — exec-слою тело ключа недоступно (SR-62).
- **Config (секция `exec`, plan §Config / SR-66):** `allowlist`(`[]`), `default_timeout_ms`(30000),
  `max_timeout_ms`(300000), `default_cwd`(`/tmp`), `env_whitelist`(`["PATH","HOME","LANG","TERM"]`),
  `max_args`(256), `max_arg_len`(131072), `max_output_bytes`(1048576), `deny_root`(`false`). Дизайн
  инструмента опирается на эти дефолты; задаёт их developer в `internal/config`.

---

## 9. Перечень tools (сводка после command-exec)

| name | тип | описание | вход | выход | ошибки |
|---|---|---|---|---|---|
| `ping` | read-only primitive | проверка живости MCP-канала | `{}` | `content:[text:"pong"]` | насл. (mcp-server §12) |
| `server_info` | read-only primitive | версия+сведения о raxd без секретов | `{}` | `structuredContent:{name,version,protocolVersion}` | насл. (mcp-server §12) |
| **`execute_command`** | **state-changing primitive (RCE)** | **запуск команды на хосте без shell, с таймаутом/лимитами/аудитом** | `{command, args?, timeout_ms?, cwd?}` | `structuredContent:ExecOutput(7 полей)` + text-резюме | §4 (#1–#21): транспорт 401/403/429; SDK −32700/−32600/−32601/−32602; валидация входа/deny/fail → isError; ненулевой exit/таймаут/root-warn → НЕ ошибка |

Приоритет первой (и единственной по spec) итерации: один инструмент `execute_command`, целиком
закрывающий 18 AC и SR-40…SR-67. Группировки/отсрочки внутри не требуется — задача неделима (spec
§«Примечание о размере»).

---

## Открытые вопросы

- [ ] **Q-EXEC-1 (значение `exit_code` при таймауте/сигнале — деталь реализации, НЕ блокер).** Контракт
  фиксирует определяющим флаг `timed_out:true`; конкретное число `exit_code` при kill сигналом
  (Go отдаёт −1, ОС-зависимо может быть 137/иное) оставлено раннеру. qa проверяет `timed_out:true` +
  частичный вывод, не точное число (§6.4). Не блокирует developer (дефолт: что вернёт `Cmd.ProcessState`).
- [ ] **Q-EXEC-2 (форма text-резюме в `content` — дефолт зафиксирован, НЕ блокер).** Предложен формат
  `exit=… duration=…ms timed_out=… stdout=…B stderr=…B[ truncated]` (§5.2). Полный вывод — в
  `structuredContent`. Если qa/клиент захотят включить «голову» stdout в text-блок для удобства модели —
  допустимо при условии тех же лимитов и «без секретов raxd». Не блокирует developer.
- [ ] **Q-EXEC-3 (аудит SDK-валидации входа — для security; НЕ блокер контракта).** Ветка «лишнее поле/
  неверный тип» (§4 #9) перехватывается SDK ДО `execHandler`, поэтому exec-аудит-запись по ней НЕ
  пишется (соединение зафиксировано транспортным `authSuccessAudit`; SR-51 проверяет лишь
  `isError`+«не запущено»). Если **security** сочтёт нужным фиксировать и эту ветку как exec-`deny`,
  потребуется перейти на низкоуровневый `ToolHandler` (вместо `ToolHandlerFor`) с ручной валидацией и
  аудитом — это усложнение ради одной ветки. Рекомендация mcp-engineer: оставить как есть (SR-57
  покрывает ветки handler'а; SDK-валидация — до него). Нужно подтверждение security.

> **Нестыковок spec / plan / security НЕ обнаружено.** ADR-001..004 (статус **accepted**, ратифицированы
> гейтами security-guardian + architect-guardian), threat-model (отклонения П-1/П-2/П-3 приняты security)
> и security-requirements (SR-40…SR-67) согласованы между собой и с поведением завендоренного go-sdk
> (`ToolHandlerFor`: ошибка handler → `isError`; валидация входа → `isError` до handler; неизвестный
> метод → −32601; неизвестный инструмент → −32602, `server.go:749`) и фактического `writeAudit`
> (`internal/server/audit.go`: success→`msg=MCP result=ok`, warn→`msg=WARN reason=`, deny→`msg=DENY
> reason=`, fail→`msg=FAIL reason=`; допустимые значения `AuditRecord.Result`:
> success/deny/fail/warn/rate-limited). Развилки spec (root-политика, env-whitelist, числовые пороги,
> формат аудита) ЗАКРЫТЫ в ADR-003/ADR-002 и подтверждены security в threat-model (П-1/П-2/П-3,
> SR-56/SR-66) — здесь зафиксированы как принятые контрактные, не открытые. Открыты лишь три
> НЕблокирующих детали реализации/аудита выше (Q-EXEC-1..3).
