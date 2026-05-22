# Threat Model: MCP Server — MCP-эндпоинт поверх готового аутентифицированного TLS-транспорта raxd

Автор: security (raxd). Дата: 2026-05-21. Язык: русский.
Автор продукта: Vladimir Kovalev, OEM TECH.
Вход: `specs/mcp-server/{spec.md (AC1–AC15),plan.md,research.md}`, ADR-001 (SDK), ADR-002 (версия
2025-11-25), ADR-003 (Origin/auth), `.claude/reference/SECURITY-BASELINE.ru.md` (§1 аутентификация,
§2 транспорт, §4 аудит/устойчивость, §6 Docker), `.claude/reference/MCP-INTEGRATION.ru.md`, готовый
код `internal/server/*` (цепочка `bodyLimit→recover→Host/Origin→auth→rate-limit→authSuccessAudit→mux`,
`AuditRecord`/`writeAudit`), `internal/keystore` (`Verify`/`Fingerprint`/`ErrCorrupt`), и УЖЕ ВЫПОЛНЕННАЯ
`specs/tls-transport/{threat-model.md,security-requirements.md}`.

> **Контекст наследования.** MCP-сервер строится ПОВЕРХ готового транспорта `tls-transport`.
> Транспортные контроли (TLS 1.3, Bearer-auth ДО обработки, Host/Origin-валидация, rate-limit
> per-key/per-IP, аудит без секретов, bind `127.0.0.1`, лимит тела, таймауты, graceful shutdown)
> уже реализованы и проверены (`specs/tls-transport/*`, риски R1–R15). Они НАСЛЕДУЮТСЯ MCP-слоем без
> изменений и ЗДЕСЬ НЕ переопределяются — на них даются ссылки. Эта модель угроз фиксирует ТОЛЬКО
> новое, что вносит MCP-слой: монтаж SDK-handler внутрь цепочки, MCP-семантику ответов (`server_info`,
> результаты инструментов, JSON-RPC), MCP-аудит вызова инструмента, валидацию JSON-RPC-ввода, новую
> цепочку зависимостей (Go MCP SDK + транзитивные) и конкурентность SDK-handler.
> Каждый риск ниже имеет смягчение; невыполнимое/отложенное — в «Остаточные риски / эскалации».

## Активы

- **API-ключи (тело `rax_live_…`)** — главный секрет. В MCP-слое ключ НЕ должен появляться нигде:
  MCP-обработка идёт ПОСЛЕ auth-middleware, которое кладёт в контекст только `Fingerprint` (тело ключа
  MCP-слою недоступно по контракту, plan «`server.FingerprintFromContext`»). Утечка ключа через ответ
  MCP, JSON-RPC-ошибку или MCP-аудит = полный доступ.
- **Приватный TLS-ключ** (`TLSDir/key.pem`) — не должен утечь через `server_info`/любой результат
  инструмента/ошибку. Источник версии для `server_info` — `internal/version`, НЕ файлы секретов
  (plan `serverInfoHandler`).
- **Хэш/соль ключа из keystore** — не должны раскрываться MCP-ответами; MCP-слой их не читает.
- **Аудит-лог** (расширяемый полем `Tool`) — журнал MCP-вызовов; ценен для расследования и не должен
  содержать секретов. Подделка/обход / отсутствие записи о вызове инструмента = repudiation.
- **server_info как поверхность раскрытия** — структурированный ответ о демоне. Любое «удобное»
  расширение полей (пути, конфиг, окружение) может непреднамеренно раскрыть инфраструктуру. Объявленный
  состав: только имя `raxd`, версия (`version.Version`), `protocolVersion` (нечувствительно).
- **Сервер-процесс и хост** — конечная цель. В этой задаче инструменты исполнения команд/файлов НЕ
  реализованы (только `ping`/`server_info` без побочных эффектов), но точка расширения (AC13) — будущий
  вектор для `command-exec`/`file-upload`.
- **`vendor/`-дерево и `go.sum`** — целостность завендоренного MCP SDK и транзитивных зависимостей;
  компрометация = подмена кода обработки протокола (supply chain).

## Поверхность атаки

- **MCP-эндпоинт `POST/GET https://127.0.0.1:<port>/mcp`** — НОВЫЙ маршрут (заменяет 501-заглушку),
  смонтированный `mux.Handle("/mcp", mcpHandler)` ВНУТРИ той же middleware-цепочки. Внутри: JSON-RPC 2.0
  тело (`initialize`, `notifications/initialized`, `tools/list`, `tools/call`), заголовки `Accept`,
  `MCP-Protocol-Version`, (наследуемые) `Authorization`/`Origin`/`Host`. GET без server→client стрима
  → 405 (v1 stateless).
- **JSON-RPC парсер/диспетчер SDK** — обрабатывает произвольный недоверенный ввод клиента: битый JSON,
  неизвестный метод/инструмент, некорректные аргументы, чрезмерно большое тело (лимит наследуется,
  `bodyLimitMiddleware`).
- **Хендлеры инструментов** `pingHandler`/`serverInfoHandler` + аудит-декоратор `withAudit`
  (`internal/mcp/audit.go`) — формируют ответ и MCP-аудит-запись.
- **Точка расширения `mcp.AddTool`** (AC13) — будущая регистрация `execute_command`/`upload_file`;
  в этой задаче пуста, но архитектурно открыта.
- **Сборочный/вендоринг-флоу** — `go mod vendor` на хосте (proxy недоступен в Docker, ADR-002),
  коммит `vendor/`+`go.sum`, `-mod=vendor`+`CGO_ENABLED=0` в Docker.
- **Кто атакует** (дополнительно к наследуемым из `tls-transport`):
  - **Аутентифицированный MCP-клиент** — шлёт некорректный JSON-RPC/неизвестный инструмент/мусорные
    аргументы, пытаясь вызвать панику, обход аудита или раскрытие секретов через ответ.
  - **Внешний клиент без ключа** — пытается достучаться до `/mcp` мимо middleware-цепочки (если бы
    handler был смонтирован отдельным mux/каналом) → анонимный доступ к MCP.
  - **Браузерный контекст / вредоносная веб-страница** — DNS-rebinding на локальный `/mcp` (MCP
    Security Warning MUST по Origin).
  - **Компрометация цепочки поставки** — подмена кода SDK/транзитивных зависимостей в `vendor/`.

## Угрозы (STRIDE-ориентир)

- **S (Spoofing) — обход аутентификации MCP-слоя.** Если SDK-handler смонтирован отдельным
  `http.ServeMux`/отдельным сетевым каналом, а не за общей цепочкой, или если SDK вводит собственный
  механизм аутентификации (OAuth-подпакеты SDK), `/mcp` станет анонимной точкой входа. Смягчение:
  handler монтируется ВНУТРИ цепочки `bodyLimit→recover→Host/Origin→auth→rate-limit→authSuccessAudit→
  mux`, ДО которой отрабатывает `keystore.Verify`; SDK-handler НЕ активирует свой канал auth, MCP про
  auth не знает (ADR-003, best-practice «MUST NOT use sessions for authentication») — R-M1.
- **T (Tampering) — подмена кода обработки протокола через зависимости.** Внесение вредоносного кода в
  завендоренный MCP SDK или транзитивные (`jsonschema-go`, `uritemplate/v3`). Смягчение: целостность
  через коммит `go.sum`, проверка `go mod verify`, вендоринг офлайн, permissive-аудируемые лицензии,
  pure Go без CGO — R-M6.
- **R (Repudiation) — MCP-вызов не зафиксирован в аудите.** Если SDK-handler обрабатывает
  `tools/call` без записи в аудит, вызов инструмента невозможно расследовать. Смягчение: декоратор
  `withAudit` пишет запись на КАЖДЫЙ `tools/call` (fingerprint из ctx + `tool=<имя>` + результат) через
  расширенный `AuditRecord.Tool`/`writeAudit` — R-M3.
- **I (Information disclosure) — утечка секретов через MCP-поверхность.** Тело ключа / приватный
  TLS-ключ / хэш / соль / raw `Authorization` просочились в `server_info`, результат инструмента,
  JSON-RPC-ошибку или MCP-аудит. Смягчение: `server_info` отдаёт только версию (`internal/version`) и
  нечувствительные сведения; MCP-аудит — только `Fingerprint` (из ctx, не ключ); тело ключа MCP-слою
  недоступно по контракту; проверка подстрокой ключа в ответе и логе — R-M2.
- **D (Denial of service) — некорректный ввод/конкурентность.** Битый JSON-RPC, неизвестный
  метод/инструмент, мусорные аргументы вызывают панику и роняют сервер; конкурентные запросы к
  stateless SDK-handler провоцируют гонки. Смягчение: SDK возвращает корректные JSON-RPC-ошибки
  (`-32700/-32600/-32601/-32602`), input-ошибки → `isError:true`, не паника; `recoverMiddleware`
  наследуется как страховка; rate-limit/таймауты/лимит тела наследуются; SDK-handler конкурентен,
  состояние сессии для auth НЕ используется (stateless) — R-M4, R-M7.
- **E (Elevation of privilege) — преждевременное/непредусмотренное исполнение.** Неизвестный
  инструмент (например, будущий `execute_command`, ещё не реализованный) трактуется как вызов вместо
  ошибки → исполнение мимо контракта. Смягчение: незарегистрированный инструмент → JSON-RPC-ошибка
  (`-32601/-32602`), НЕ исполнение; `execute_command`/`upload_file` НЕ зарегистрированы (только точка
  расширения AC13); `ping`/`server_info` без побочных эффектов на хосте — R-M5.

## Риски (риск → вероятность/влияние → смягчение)

- [ ] **R-M1. Обход аутентификации: MCP-handler смонтирован мимо middleware-цепочки → анонимный
  доступ.** Если `StreamableHTTPHandler` зарегистрирован отдельным mux/портом/каналом или раньше
  auth-middleware, либо SDK включает собственный auth-обход, `/mcp` доступен без валидного ключа.
  Вероятность **низкая** (контракт plan фиксирует монтаж за цепочкой) / влияние **критическое**
  (открытая точка входа к MCP, в перспективе — к exec). Смягчение: handler монтируется
  `mux.Handle("/mcp", mcpHandler)` ВНУТРИ единой цепочки
  `bodyLimit→recover→Host/Origin→auth→rate-limit→authSuccessAudit→mux` (наследуется из `tls-transport`,
  не дублируется); `auth` (`keystore.Verify`) отрабатывает ДО `/mcp`; отдельного процесса/порта/mux для
  MCP НЕТ (AC11); SDK-handler НЕ активирует собственный канал аутентификации (ADR-003, best-practice
  «MUST NOT use sessions for authentication»). Тест: запрос на `/mcp` без `Authorization: Bearer`,
  с неизвестным/отозванным ключом → **401**, не доходит до SDK-диспетчера (нет JSON-RPC-ответа на
  `initialize`/`tools/call`); `ErrCorrupt` keystore → **403**. (baseline §1; spec AC2/AC8/AC11;
  plan «auth ДО MCP», `server.New(...,mcpHandler)`)

- [ ] **R-M2. Утечка секретов через MCP-поверхность (`server_info`, результаты инструментов,
  JSON-RPC-ошибки, MCP-аудит).** Тело ключа / приватный TLS-ключ / хэш / соль / raw `Authorization`
  попадают в структурированный ответ `server_info`, в `content`/`structuredContent` инструмента, в
  текст JSON-RPC-ошибки или в MCP-аудит-запись. Вероятность **средняя** (легко допустить «для удобства
  диагностики» в `server_info` или в сообщении об ошибке) / влияние **критическое**. Смягчение:
  `serverInfoHandler` возвращает `ServerInfo{Name:"raxd", Version, ProtocolVersion}` — ТОЛЬКО версия из
  `internal/version.Version` и нечувствительные сведения, БЕЗ путей к секретам/конфигу/окружения
  (plan); MCP-аудит оперирует только `Fingerprint` (из ctx через `server.FingerprintFromContext`, тело
  ключа MCP-слою недоступно); JSON-RPC-ошибки не включают тело запроса/ключ. Проверяемо: предъявленный
  полный ключ как ПОДСТРОКА ОТСУТСТВУЕТ в теле любого MCP-ответа (initialize/tools.list/tools.call/
  error) И в захваченном аудит-логе (тест grep); приватный TLS-ключ не встречается в ответах.
  (baseline §4 «никаких секретов в логах», §1; spec AC6/AC10; plan `serverInfoHandler`/`withAudit`)

- [ ] **R-M3. MCP-вызов инструмента не фиксируется в аудите (repudiation).** SDK-handler обрабатывает
  `tools/call`, но без аудит-записи о вызове конкретного инструмента → нельзя расследовать, кто и что
  вызывал. Вероятность **средняя** (SDK сам по себе аудит не пишет) / влияние **среднее**. Смягчение:
  декоратор `withAudit` (`internal/mcp/audit.go`) оборачивает КАЖДЫЙ tool-хендлер и после вызова пишет
  `AuditRecord{Fingerprint: server.FingerprintFromContext(ctx), Tool: <имя инструмента>, Result:
  success|fail, RemoteAddr, Reason}` через инжектированный `AuditFn`; `AuditRecord` расширен полем
  `Tool`, `writeAudit` логирует `tool=` во всех ветках включая success (msg-label `MCP`). Имя
  инструмента — не секрет; тело ключа не извлекается. Проверяемо: после `tools/call ping` в аудит-логе
  есть запись с `fp=<fingerprint>`, `tool=ping`, `result=ok` (или эквивалент success). (baseline §4
  «аудит каждого действия: timestamp, fingerprint, адрес»; spec AC9; plan `withAudit`/`AuditRecord.Tool`)

- [ ] **R-M4. Паника/падение сервера на некорректном JSON-RPC-вводе (DoS).** Битый JSON, нарушенная
  структура JSON-RPC, неизвестный метод, неизвестный инструмент или мусорные аргументы инструмента
  приводят к панике/500/501 вместо корректной ошибки → отказ обслуживания. Вероятность **средняя** /
  влияние **среднее**. Смягчение: SDK сериализует ошибки протокола стандартными JSON-RPC-кодами —
  битый JSON → `-32700` (parse error); невалидный request → `-32600`; неизвестный метод → `-32601`;
  неизвестный инструмент/неверные параметры → `-32601/-32602`; ошибки валидации ВВОДА инструмента →
  `isError:true` (Tool Execution Error, не protocol error, спека 2025-11-25); GET `/mcp` без стрима →
  405; невалидная `MCP-Protocol-Version` → 400; `ping`/`server_info` имеют непустую `inputSchema`
  (`{"type":"object","additionalProperties":false}`) и не принимают опасный ввод. Наследуемый
  `recoverMiddleware` — страховка от паники. Сервер остаётся работоспособным после некорректного
  запроса. Проверяемо: тест — неизвестный инструмент → JSON-RPC error (не паника, не 501); битый
  JSON-RPC → `-32700`; после такого запроса валидный `ping` всё ещё → `pong`. (baseline §4
  «устойчивость»; spec AC7; plan «Обработка ошибок SDK»)

- [ ] **R-M5. Неизвестный/нереализованный инструмент трактуется как исполнение (преждевременный
  exec).** `execute_command`/`upload_file` ещё не реализованы (out of scope), но точка расширения
  открыта (AC13). Риск: вызов несуществующего инструмента приводит к исполнению/побочному эффекту
  вместо ошибки; либо инструмент случайно зарегистрирован раньше своей задачи. Вероятность **низкая** /
  влияние **высокое** (исполнение мимо контракта `command-exec`). Смягчение: `tools/list` возвращает
  РОВНО `ping`+`server_info`; вызов любого другого имени → JSON-RPC-ошибка (`-32601/-32602`), НЕ
  исполнение; `execute_command`/`upload_file` в `internal/mcp` НЕ регистрируются в этой задаче (только
  `mcp.AddTool`-точка расширения); `ping`/`server_info` без побочных эффектов на хосте. Будущие
  опасные инструменты — предмет threat-model задач `command-exec`/`file-upload` (baseline §3: exec без
  shell, таймаут, allowlist) — см. ОР-М2. Проверяемо: `tools/list` = {ping, server_info}; `tools/call`
  с `execute_command` → JSON-RPC error, никакой команды не исполнено. (baseline §3 — наследуемая точка
  расширения; spec AC4/AC5/AC13; plan «Точки расширения»)

- [ ] **R-M6. Supply chain: компрометация/непригодность завендоренного MCP SDK и транзитивных.**
  Новая зависимость `github.com/modelcontextprotocol/go-sdk/mcp` (v1.6.0) + транзитивные
  (`google/jsonschema-go`, `yosida95/uritemplate/v3`). Риски: вредоносный/подменённый код в `vendor/`;
  несовместимая/«копилефт» лицензия; CGO ломает кросс-сборку amd64/arm64; «тяжёлое» дерево не
  вендорится офлайн (proxy недоступен в Docker, ADR-002). Вероятность **низкая** / влияние **высокое**
  (код обрабатывает недоверенный сетевой ввод). Смягчение: вендоринг офлайн (`go mod vendor` на хосте →
  коммит `vendor/`+`go.sum` → `-mod=vendor` в Docker, без `go mod download`); целостность через
  коммит `go.sum` + `go mod verify`; лицензии permissive (SDK Apache-2.0/MIT/CC-BY-4.0, jsonschema-go
  без внешних зависимостей, uritemplate/v3 BSD-3-Clause — research §D/§E); все элементы pure Go,
  `CGO_ENABLED=0` сохраняется, поддержка amd64+arm64; реальное вендорённое дерево пакета `mcp` малое
  (research §E: только импортируемые пакеты, не всё go.mod-замыкание). Точный состав `vendor/`
  подтверждается фактическим `go mod vendor` (OQ-1, research). Проверяемо: сборка/тесты офлайн из
  `vendor/` зелёные в Docker; `go mod verify` ОК; в `vendor/` нет CGO-кода в импорт-графе; лицензии
  permissive (инспекция `vendor/.../LICENSE`). (baseline §6; spec AC14; plan Trade-offs/вендоринг;
  research §D/§E/OQ-1, ADR-001/ADR-002)

- [ ] **R-M7. Конкурентность stateless SDK-handler / гонки.** `StreamableHTTPHandler` обрабатывает
  запросы конкурентно; общее изменяемое состояние (кэш, счётчики) или использование MCP-сессии для
  аутентификации могли бы дать гонки/обход. Вероятность **низкая** / влияние **среднее**. Смягчение:
  v1 — stateless (без `MCP-Session-Id`, GET→405); MCP-сессия НЕ используется для аутентификации (auth
  на транспорте ДО MCP, ADR-003); хендлеры `ping`/`server_info` не имеют общего изменяемого состояния
  и без побочных эффектов; rate-limit (наследуется) ограничивает частоту; тесты гоняются с `-race`
  (qa). Проверяемо: `-race`-прогон параллельных `tools/call` без data race; отсутствие сессионного
  состояния, влияющего на auth (инспекция). (baseline §4 «устойчивость»; plan «Stateless, GET→405»,
  research B/OQ-2)

> **Наследуемые риски (закрыты в `tls-transport`, MCP-слой их НЕ переоткрывает).** Полное описание —
> `specs/tls-transport/threat-model.md`. MCP-эндпоинт сидит за той же периметровой защитой:
> - R1 (MITM/self-signed TOFU) — TLS 1.3 + bind `127.0.0.1`; см. также ОР-М1 (mTLS вне scope).
> - R2 (DNS-rebinding) — bind `127.0.0.1` + auth + Host-allowlist + Origin present&invalid→403
>   (наследуется MCP-слоем; см. R-M-Origin ниже и ОР-М3).
> - R3 (обход auth до маршрутизации) — auth-middleware ДО mux (база для R-M1).
> - R5 (timing-атака) — constant-time `keystore.Verify`.
> - R6 (утечка ключа в логах транспорта) — аудит только по `Fingerprint` (база для R-M2).
> - R7/R8 (аудит отказов, брутфорс) — rate-limit per-key/per-IP + аудит fail/rate-limited.
> - R9/R11/R13 (Slowloris, рост лимитеров, graceful) — таймауты, лимит тела, TTL-GC, `Shutdown→FlushUsage`.
> - R15 (catch-all без побочных эффектов) — заменён реальным `/mcp` за цепочкой (база для R-M5).

- [ ] **R-M-Origin. DNS-rebinding для браузерных MCP-клиентов (наследуется).** MCP Security Warning
  (MUST): «Servers MUST validate the `Origin` header … If the `Origin` header is present and invalid,
  servers MUST respond with HTTP 403» (research §B). Вероятность **низкая** (raxd-агенты не браузерные)
  / влияние **высокое**. Смягчение: НАСЛЕДУЕТСЯ от `tls-transport` `hostOriginMiddleware` (ADR-003):
  `Origin` present И вне allowlist → **403**; `Origin` отсутствует → не отклонять; + bind `127.0.0.1` +
  auth + Host-allowlist. Базовый MUST покрыт транспортом; явный allowlist Origin для конкретных
  браузерных MCP-клиентов — конфигурируем (`cfg.OriginAllow`). Проверяемо: запрос на `/mcp` с
  present&invalid `Origin` → 403, не доходит до MCP-обработки. (baseline §2; spec AC12/Q5; plan ADR-003;
  наследует `tls-transport` SR-14/SR-16, R2/ОР-3)

## Остаточные риски / эскалации

- [ ] **ОР-М1. mTLS / клиентские сертификаты (baseline §2 «mTLS опционально») НЕ реализуется
  (наследуемая отсрочка).** Почему: решение зафиксировано в `tls-transport` (ОР-1) — для модели
  «один продукт-клиент ↔ сервер» аутентификации по API-ключу поверх TLS 1.3 + bind `127.0.0.1` +
  rate-limit достаточно; mTLS добавляет вес дистрибуции клиентских сертификатов без выигрыша в текущей
  модели. MCP-слой эту отсрочку НЕ меняет. Остаточный риск: при выносе демона на не-loopback интерфейс
  доверие к self-signed серту (TOFU, R1) повышает риск MITM. Компенсирующий контроль: TLS 1.3, bind
  `127.0.0.1`, auth по ключу как основной гейт, аудит. **Эскалация:** наследует эскалацию
  `tls-transport` ОР-1 — перед релизом любой не-loopback конфигурации вернуть вопрос mTLS пользователю.
  (baseline §2)

- [ ] **ОР-М2. Инструменты исполнения команд/файлов (`execute_command`/`upload_file`) и контроли
  baseline §3 (exec без shell, таймаут на команду, allowlist, рабочая директория, демон не от root) НЕ
  реализуются этой задачей.** Почему: spec Out of Scope — это задачи `command-exec`/`file-upload`;
  здесь только точка расширения `mcp.AddTool` (AC13) и инструменты `ping`/`server_info` без побочных
  эффектов. Остаточный риск: будущая регистрация опасных инструментов без контролей §3 = исполнение
  произвольных команд по сети (главный риск продукта). Компенсирующий контроль: в этой задаче опасные
  инструменты НЕ зарегистрированы, вызов неизвестного → JSON-RPC-ошибка (R-M5); каждый будущий
  инструмент ОБЯЗАН быть обёрнут `withAudit` (R-M3) и регистрироваться через ту же точку. **Эскалация:**
  все контроли baseline §3 (exec через `exec.Command` без shell, таймаут через `context`, опциональный
  allowlist строгим сопоставлением, ограниченная рабочая директория/окружение, демон не от root) —
  передаются как ОБЯЗАТЕЛЬНОЕ требование в threat-model задач `command-exec`/`file-upload`. (baseline §3)

- [ ] **ОР-М3. Полная браузер-ориентированная Origin-политика (MCP MUST) — лёгкий middleware из
  `tls-transport`, не строгая «Origin обязателен».** Почему: ADR-003 / ADR-002 `tls-transport` —
  middleware отклоняет present&invalid Origin (403), но НЕ требует наличия Origin (raxd-агенты curl/SDK
  Origin не шлют; жёсткое «Origin обязателен» сломало бы легитимных клиентов). Базовый MUST спеки («if
  present and invalid → 403») выполнен. Остаточный риск: для специфичного браузерного MCP-клиента может
  потребоваться явный allowlist доверенных Origin. Компенсирующий контроль: bind `127.0.0.1` + auth +
  Host-allowlist закрывают rebinding для текущей модели; `cfg.OriginAllow` позволяет задать allowlist
  при появлении браузерного клиента. **Эскалация:** при подключении конкретного браузерного MCP-клиента
  — согласовать с пользователем явный allowlist Origin (значения origin доверенных клиентов) до
  включения. (baseline §2; наследует `tls-transport` ОР-3)

- [ ] **ОР-М4. Точный состав `vendor/` после `go mod vendor` с SDK подтверждается только прогоном
  (OQ-1).** Почему: research §E установил, что в `vendor/` попадут лишь импортируемые пакетом `mcp`
  зависимости (`jsonschema-go` + `uritemplate/v3` + internal SDK), а не всё go.mod-замыкание SDK; но
  фактический список фиксируется только реальным `go mod vendor` на хосте (developer/devops). Остаточный
  риск: в граф мог бы попасть нежданный CGO/копилефт-пакет. Компенсирующий контроль: после прогона
  developer/devops проверяет состав `vendor/` (нет CGO в импорт-графе, лицензии permissive), `go mod
  verify` ОК, сборка офлайн зелёная (R-M6). **Эскалация:** если фактический `go mod vendor` втянет
  пакет с не-permissive лицензией или CGO в импорт-графе — эскалировать выбор SDK vs stdlib (research
  вариант B) пользователю до релиза. (baseline §6; research §E/OQ-1, ADR-002)
