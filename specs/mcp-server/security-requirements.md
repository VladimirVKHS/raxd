# Security Requirements: MCP Server — MCP-эндпоинт поверх готового TLS-транспорта raxd

> Каждое требование ПРОВЕРЯЕМО (ответ «выполнено/нет» одним вопросом: тест / grep / инспекция) и
> ссылается на пункт `SECURITY-BASELINE.ru.md`, на контракт `plan.md` и на риск из `threat-model.md`.
> Эти требования ОБЯЗАНЫ выполнить `developer` (`internal/mcp/*`, расширение `internal/server/audit.go`
> и `internal/server/server.go`, `internal/cli/serve.go`), `mcp-engineer` (контракт инструментов/точка
> расширения), `devops` (вендоринг SDK офлайн, CI в Docker) и `qa` (тесты). Соответствие проверяют
> `reviewer` и `security-guardian`. Способ проверки везде: тесты гоняются в Docker из `vendor/`
> (`-mod=vendor`, baseline §6, AC14).
>
> **Нумерация.** SR-1…SR-26 принадлежат `tls-transport` (транспортные контроли — НАСЛЕДУЮТСЯ, см.
> раздел «Наследуемые требования»). Новые требования MCP-слоя нумеруются с **SR-27**, чтобы не
> пересекаться.
>
> **Терминология.** «Полный ключ» = строка `rax_live_<base64url>` целиком (заголовок `Authorization:
> Bearer`). «Fingerprint» = `keystore.Fingerprint(...)` — 12 hex sha256 ключа, необратим; в аудите и
> MCP-слое используется ВМЕСТО ключа. «MCP-поверхность» = ответы `initialize`/`tools.list`/`tools.call`
> (`content`/`structuredContent`), JSON-RPC-ошибки и MCP-аудит-записи. MCP-слой НЕ имеет доступа к телу
> ключа: транспортный `authMiddleware` кладёт в контекст только `Fingerprint`
> (`server.FingerprintFromContext`, plan).
>
> **Маппинг кодов отказа MCP-слоя (расширяет таблицу `tls-transport`; обязателен к соблюдению):**
> | Условие | Код | Где |
> |---|---|---|
> | MCP-запрос без `Authorization: Bearer` / неизвестный / отозванный ключ | **401** | SR-27 (насл. SR-9) |
> | Повреждение `keys.db` в рантайме (`ErrCorrupt`) | **403** | SR-27 (насл. SR-13) |
> | `Origin` present И вне allowlist | **403** | SR-32 (насл. SR-16) |
> | Битый JSON / невалидный JSON-RPC request | **JSON-RPC -32700 / -32600** | SR-30 |
> | Неизвестный метод / неизвестный инструмент / неверные параметры | **JSON-RPC -32601 / -32602** | SR-30, SR-33 |
> | Ошибка валидации ВВОДА инструмента | **`isError:true`** (Tool Execution Error) | SR-30 |
> | GET `/mcp` без server→client стрима | **405** | SR-30 |
> | Невалидная/неподдерживаемая `MCP-Protocol-Version` | **400** | SR-30 |

## Аутентификация MCP-запросов (baseline §1)

- [ ] **SR-27. Аутентификация КАЖДОГО MCP-запроса наследуется от транспорта и выполняется ДО
  MCP-обработки.** MCP-handler смонтирован `mux.Handle("/mcp", mcpHandler)` ВНУТРИ единой
  middleware-цепочки `bodyLimit→recover→Host/Origin→auth→rate-limit→authSuccessAudit→mux`; `auth`
  (`keystore.Verify`) отрабатывает ДО `/mcp`. Запрос без валидного ключа НЕ достигает SDK-диспетчера.
  Маппинг: нет/не-`Bearer`/пустой токен → **401**; `Verify→(_,false,nil)` (неизвестный/отозванный) →
  **401**; `Verify→error`/`ErrCorrupt` → **403**. Проверка: тест — `POST /mcp` с `initialize` БЕЗ
  `Authorization` → 401 (нет JSON-RPC-ответа `initialize`, инструмент не вызван); неизвестный/отозванный
  ключ → 401; битый `keys.db` → 403. (baseline §1; spec AC2/AC8; plan «auth ДО MCP»; threat-model R-M1;
  наследует `tls-transport` SR-8/SR-9/SR-13)

- [ ] **SR-28. MCP-handler НЕ вводит собственного канала аутентификации и НЕ использует MCP-сессию для
  auth.** SDK-handler не активирует свои OAuth/`auth`-подпакеты как путь аутентификации; единственный
  источник идентичности — транспортный `authMiddleware` (Bearer→`keystore.Verify`); MCP-сессия
  (`MCP-Session-Id`, в v1 не выдаётся) НЕ участвует в аутентификации (best-practice «MUST NOT use
  sessions for authentication»). Проверка: инспекция `internal/mcp` и монтажа в `internal/server` — нет
  второго пути auth, нет проверки ключа/сессии внутри SDK-handler; grep — `mcp`-пакет не вызывает
  `keystore.Verify` сам (auth уже на транспорте). (baseline §1; spec AC2; plan ADR-003; threat-model
  R-M1; research §G)

- [ ] **SR-29. MCP-эндпоинт поднимается тем же `raxd serve` (тот же порт/TLS); отдельного процесса/
  порта/mux для MCP НЕТ.** `internal/cli/serve.go` строит `internal/mcp`-handler и передаёт его в
  `server.New(..., mcpHandler)`; при `mcpHandler != nil` маршрут `/mcp` зарегистрирован в ТОМ ЖЕ mux за
  той же цепочкой (ДО любого catch-all). `server.FingerprintFromContext` экспонирует fingerprint из ctx
  для MCP-аудита (тело ключа не экспонируется). Проверка: тест — после `serve` MCP доступен на
  `https://127.0.0.1:<port>/mcp` за той же auth; второго слушающего сокета/порта нет; `nil`-handler →
  поведение как 501-заглушка (обратная совместимость). (baseline §2; spec AC11; plan
  `server.New(...,mcpHandler)`/`serve.go`/`FingerprintFromContext`; threat-model R-M1)

## Валидация ввода и ошибки протокола (baseline §4 «устойчивость»)

- [ ] **SR-30. Некорректный JSON-RPC-ввод → корректная ошибка, НЕ паника и НЕ 501; сервер остаётся
  работоспособным.** Битый JSON → `-32700`; невалидный request (нарушенная структура JSON-RPC) →
  `-32600`; неизвестный метод → `-32601`; неизвестный инструмент / неверные параметры → `-32601/-32602`;
  ошибка валидации ВВОДА инструмента → `isError:true` (Tool Execution Error, спека 2025-11-25, не
  protocol error); GET `/mcp` без стрима → 405; невалидная `MCP-Protocol-Version` → 400. `ping`/
  `server_info` объявляют непустую `inputSchema` (`{"type":"object","additionalProperties":false}`) и не
  принимают опасного ввода. Наследуемый `recoverMiddleware` страхует от паники. Проверка: тест —
  неизвестный инструмент → JSON-RPC error (не паника/501); битый JSON-RPC → `-32700`; ПОСЛЕ некорректного
  запроса валидный `ping` всё ещё возвращает `pong` (сервер жив). (baseline §4 «устойчивость»; spec AC7;
  plan «Обработка ошибок SDK»; threat-model R-M4)

- [ ] **SR-31. `initialize`/`tools/list`/`tools/call` соответствуют объявленному контракту без опасного
  поведения.** `initialize` → `protocolVersion:"2025-11-25"` (ADR-002), `serverInfo{name:"raxd",
  version:version.Version}`, объявлена ТОЛЬКО `tools` capability (без resources/prompts/logging — Q4);
  `tools/list` → РОВНО `ping`+`server_info` с непустыми `inputSchema`; `tools/call ping` → `content:
  [{type:"text",text:"pong"}]`, `isError:false`, без побочных эффектов на хосте. Проверка: тест —
  initialize даёт capabilities+version; tools.list = {ping, server_info}; ping → pong без побочных
  эффектов. (baseline §1/§4; spec AC3/AC4/AC5; plan tools.go/server.go; threat-model R-M5; research C)

## DNS-rebinding / Origin (baseline §2)

- [ ] **SR-32. Origin-валидация для браузерных MCP-клиентов наследуется от транспорта (present&invalid
  → 403).** MCP Security Warning (MUST) по Origin покрывается наследуемым `hostOriginMiddleware`
  (`tls-transport`, ADR-003): `Origin` present И вне `cfg.OriginAllow` → **403**; `Origin` отсутствует
  → не отклонять (curl/SDK-клиенты Origin не шлют); + bind `127.0.0.1` + Host-allowlist + auth.
  MCP-слой эту проверку НЕ переопределяет и НЕ ослабляет. Проверка: тест — `POST /mcp` с present&invalid
  `Origin` → 403, не доходит до MCP-обработки; отсутствующий Origin → проходит до auth. (baseline §2;
  spec AC12/Q5; plan ADR-003; threat-model R-M-Origin; наследует `tls-transport` SR-14/SR-16)

## Раскрытие информации: MCP-поверхность без секретов (baseline §4/§1)

- [ ] **SR-33. `server_info` отдаёт только нечувствительные сведения; никаких секретов/инфраструктуры.**
  `serverInfoHandler` возвращает `ServerInfo{Name:"raxd", Version: version.Version, ProtocolVersion:
  "2025-11-25"}` — источник версии `internal/version`, НЕ чтение секретов/конфига/окружения; БЕЗ тел
  ключей, хэшей, соли, приватного TLS-ключа, путей к секретам, переменных окружения. Проверка: тест —
  результат `server_info` содержит только {Name, Version, ProtocolVersion}; предъявленный ключ и
  приватный TLS-ключ как ПОДСТРОКА в нём ОТСУТСТВУЮТ; нет полей с путями/конфигом. (baseline §4 «никаких
  секретов», §1; spec AC6; plan `serverInfoHandler`; threat-model R-M2)

- [ ] **SR-34. Результаты инструментов, JSON-RPC-ошибки и MCP-аудит НЕ содержат секретов (проверка
  подстрокой).** Ни в одном теле MCP-ответа (`initialize`/`tools.list`/`tools.call` `content`/
  `structuredContent`, JSON-RPC `error.message`/`data`) и ни в одной MCP-аудит-записи НЕТ полного ключа,
  его хэша, соли, raw `Authorization` и приватного TLS-ключа. MCP-слой не имеет доступа к телу ключа
  (только `Fingerprint` из ctx). Проверка: тест — предъявленный полный ключ как ПОДСТРОКА ОТСУТСТВУЕТ в
  захваченных телах MCP-ответов И в захваченном аудит-логе; приватный TLS-ключ не встречается; grep по
  `internal/mcp` — нет логирования/возврата raw `Authorization`/тела ключа/приватного ключа. (baseline §4
  «никаких секретов в логах», §1; spec AC10; plan `withAudit`/`serverInfoHandler`; threat-model R-M2;
  наследует `tls-transport` SR-21)

## Аудит MCP-вызова (baseline §4)

- [ ] **SR-35. Каждый `tools/call` пишет аудит-запись: fingerprint (не ключ) + имя инструмента +
  результат.** Декоратор `withAudit` (`internal/mcp/audit.go`) оборачивает КАЖДЫЙ tool-хендлер и после
  вызова формирует `AuditRecord{Fingerprint: server.FingerprintFromContext(ctx), Tool: <имя
  инструмента>, Result: success|fail, RemoteAddr, Reason}` и зовёт инжектированный `AuditFn`; fingerprint
  берётся из ctx (НЕ ключ), имя инструмента — из MCP-вызова, тело ключа НЕ извлекается. Проверка: тест —
  после `tools/call ping` в аудит-логе есть запись с непустым `fp=<fingerprint>`, `tool=ping` и
  результатом (success). (baseline §4 «аудит каждого действия: timestamp, fingerprint, адрес»; spec AC9;
  plan `withAudit`/`FingerprintFromContext`; threat-model R-M3)

- [ ] **SR-36. `AuditRecord` расширен полем `Tool`; `writeAudit` логирует `tool=` во всех ветках, не
  ломая формат не-MCP записей.** В `internal/server/audit.go` к `AuditRecord` добавлено поле
  `Tool string` («имя MCP-инструмента/метода; пусто для не-MCP записей»); `writeAudit` выводит
  `tool=<rec.Tool>` ТОЛЬКО при `rec.Tool != ""` (существующие connection-записи AUTH/FAIL/DENY/RATE без
  `tool` сохраняют прежний формат — наследуемые тесты `tls-transport` не ломаются); для MCP-успеха
  msg-label `MCP`. Поле `Tool` — имя инструмента, не секрет; инвариант «тело ключа не хранится» (SR-21)
  сохранён. Проверка: тест — MCP-success запись содержит `tool=ping`; не-MCP AUTH-запись `tool=` НЕ
  содержит; существующие подстроки (`AUTH`, `fp=`, `fp=-`) на месте. (baseline §4; spec AC9; plan
  «AuditRecord/writeAudit расширение»; threat-model R-M3; совместимо с `tls-transport` SR-19/SR-21)

## Scope: инструменты исполнения вне этой задачи (baseline §3)

- [ ] **SR-37. `execute_command`/`upload_file` НЕ реализованы; неизвестный инструмент → JSON-RPC-ошибка,
  не исполнение.** В `internal/mcp` зарегистрированы РОВНО `ping`+`server_info`; `execute_command`/
  `upload_file` НЕ регистрируются (только точка расширения `mcp.AddTool`, AC13); вызов любого
  незарегистрированного инструмента → JSON-RPC-ошибка (`-32601/-32602`), НЕ исполнение и без побочных
  эффектов; их отсутствие не ломает `tools/list`. Контроли baseline §3 (exec без shell, таймаут,
  allowlist, рабочая директория, демон не от root) — обязательны к выполнению в задачах
  `command-exec`/`file-upload` (threat-model ОР-М2). Проверка: тест — `tools/list`={ping, server_info};
  `tools/call execute_command` → JSON-RPC error, никакой команды не исполнено. (baseline §3 —
  наследуемая точка расширения; spec AC4/AC13; plan «Точки расширения»; threat-model R-M5/ОР-М2)

## Supply chain / среда (baseline §6)

- [ ] **SR-38. MCP SDK и транзитивные зависимости вендорятся офлайн; целостность `go.sum`; без CGO;
  permissive-лицензии.** `github.com/modelcontextprotocol/go-sdk/mcp` (v1.6.0) + транзитивные
  (`google/jsonschema-go`, `yosida95/uritemplate/v3`) добавлены в `vendor/` через `go mod vendor` на
  ХОСТЕ (proxy в Docker недоступен, ADR-002) с коммитом `vendor/`+`go.mod`+`go.sum`; `go mod verify`
  проходит; в импорт-графе пакета `mcp` нет CGO (все элементы pure Go, `CGO_ENABLED=0` сохраняется,
  amd64+arm64); лицензии permissive (SDK Apache-2.0/MIT/CC-BY-4.0; jsonschema-go без внешних
  зависимостей; uritemplate/v3 BSD-3-Clause). Если фактический `go mod vendor` втянет не-permissive или
  CGO-пакет в импорт-граф — эскалация (ОР-М4). Проверка: `go mod verify` ОК; инспекция `vendor/.../
  LICENSE` — все permissive; сборка с `CGO_ENABLED=0 -mod=vendor` зелёная; grep по импорт-графу — нет
  `import "C"`. (baseline §6; spec AC14; plan Trade-offs/вендоринг; threat-model R-M6/ОР-М4; research
  §D/§E)

- [ ] **SR-39. Все проверки MCP-слоя прогоняются в Docker, офлайн из `vendor/`.** Тесты этой задачи
  (initialize→capabilities; tools/list набор; ping→pong; server_info без секретов; неаутентифицированный
  →401; невалидный JSON-RPC→ошибка без паники; аудит с fingerprint+tool; ключ не встречается подстрокой
  в ответах и логе; Origin present&invalid→403) зелёные в Docker; сборка/тесты `-mod=vendor` без `go mod
  download`; `-race`-прогон параллельных `tools/call` без data race (конкурентность SDK-handler, R-M7);
  на хосте `raxd` НЕ запускается. Проверка: CI/локальный прогон в контейнере проходит из `vendor/`.
  (baseline §6; spec AC14; plan «Тестируемость в Docker»; threat-model R-M6/R-M7)

## Наследуемые требования (выполнены в `tls-transport`, MCP-слой НЕ переопределяет)

> Полный текст и проверки — `specs/tls-transport/security-requirements.md`. MCP-эндпоинт `/mcp` сидит за
> этой периметровой защитой; дублировать её как новые SR ЗАПРЕЩЕНО (см. CLAUDE.md: «не переписывать
> транспорт»). Перечислены ссылки, обязательные для понимания контекста developer/qa.

- **SR-1/SR-2 (TLS 1.3, CipherSuites не задаются)** — `buildTLSConfig`, `MinVersion: tls.VersionTLS13`.
  MCP идёт ПОВЕРХ этого TLS.
- **SR-3…SR-6 (self-signed серт, права `0600`, переиспользование, битый серт→ошибка)** — генерация/
  загрузка TLS не меняется.
- **SR-7 (bind `127.0.0.1` по умолчанию)** — тот же слушающий сокет обслуживает `/mcp` (AC11).
- **SR-8/SR-9/SR-10/SR-11/SR-13 (auth ДО маршрутизации, Bearer→`keystore.Verify`, constant-time,
  мгновенный отзыв, отказ без раскрытия причины / `ErrCorrupt`→403)** — база для SR-27/SR-28.
- **SR-14/SR-15/SR-16 (Host/Origin в middleware ДО auth; Host вне allowlist→403; Origin present&invalid
  →403)** — база для SR-32.
- **SR-17/SR-18 (rate-limit per-key/per-IP→429; потокобезопасное хранилище лимитеров с TTL-GC)** —
  ограничивает частоту MCP-вызовов; не дублируется.
- **SR-19/SR-20/SR-21 (аудит на каждое соединение; обязательный аудит отказов/rate-limit; никаких
  секретов в логах)** — база для SR-34/SR-35/SR-36; MCP-аудит расширяет тот же канал.
- **SR-24/SR-25 (graceful `Shutdown→FlushUsage`; таймауты + лимит тела/заголовков)** — защищают и
  MCP-эндпоинт от Slowloris/больших тел; не переопределяются.

## Вне scope этой задачи (фиксация, не требование к mcp-server)

- **mTLS / клиентские сертификаты** (baseline §2) — отложено решением дирижёра (наследуемая отсрочка);
  риск с компенсацией зафиксирован в `threat-model.md` ОР-М1, эскалация наследует `tls-transport` ОР-1.
- **Инструменты исполнения команд/файлов и контроли baseline §3** (exec без shell, таймаут, allowlist,
  рабочая директория, демон не от root) — задачи `command-exec`/`file-upload`; передано как обязательное
  требование (threat-model ОР-М2). Здесь только точка расширения (SR-37).
- **Полная браузер-ориентированная Origin-политика / явный allowlist Origin** — при подключении
  конкретного браузерного MCP-клиента; базовый MUST покрыт (SR-32), эскалация в threat-model ОР-М3.
- **MCP Resources и Prompts** — следующая итерация (Q4); объявляется только `tools` capability (SR-31).
- **Ротация и файловый бэкенд аудит-лога** (baseline §4 «с ротацией») — уровень сервиса/дистрибуции;
  здесь обязательны только структура и поля записи (наследуется `tls-transport` ОР-4).
- **Управление ключами** (генерация/хранение/формат/constant-time `Verify`/fingerprint) — готово в
  `key-management`; здесь только ПОТРЕБЛЕНИЕ `keystore.Verify`/`Fingerprint`.
- **Install-скрипт / `SHA256SUMS` / подпись-нотаризация** (baseline §5) — задача `distribution`.
- **Документация подключения MCP-клиента** (URL, Bearer, self-signed) — `tech-writer` (spec AC15).
