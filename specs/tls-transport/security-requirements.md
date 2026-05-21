# Security Requirements: TLS Transport — защищённый аутентифицированный сетевой транспорт raxd

> Каждое требование ПРОВЕРЯЕМО (ответ «выполнено/нет» одним вопросом: тест / grep / инспекция) и
> ссылается на пункт `SECURITY-BASELINE.ru.md`, на контракт `plan.md` и на риск из `threat-model.md`.
> Эти требования ОБЯЗАНЫ выполнить `developer` (`internal/server/*`, `internal/cli/serve.go`,
> `internal/config/config.go`), `system-dev`/`devops` (где касается запуска), `mcp-engineer` (точка
> расширения диспетчера) и `qa` (тесты). Соответствие проверяют `reviewer` и `security-guardian`.
> Способ проверки везде: тесты гоняются в Docker из `vendor/` (baseline §6, AC14).
>
> **Терминология.** «Полный ключ» = строка `rax_live_<base64url>` целиком — то, что клиент передаёт в
> заголовке `Authorization: Bearer` и что подаётся в `keystore.Verify`. «Fingerprint» = `keystore.Fingerprint(...)`
> = 12 hex sha256 ключа, необратим, используется в аудите вместо ключа. Транспорт НЕ хэширует и НЕ
> сравнивает ключи сам — только вызывает контракт keystore (constant-time уже внутри, подтверждено по
> `internal/keystore/keystore.go`).
>
> **Маппинг кодов отказа (формализован, закрывает спорный пункт architect — обязателен к соблюдению):**
> | Условие | Код | Где |
> |---|---|---|
> | Нет заголовка `Authorization` / не `Bearer` / пустой токен | **401** | SR-9, SR-12 |
> | `Verify → (_, false, nil)` (неизвестный/отозванный ключ, отсутствующий/пустой `keys.db`) | **401** | SR-9, SR-12 |
> | `Verify → error` / `ErrCorrupt` (повреждение `keys.db` в рантайме = нет валидных ключей) | **403** | SR-13 |
> | `Host` вне allowlist | **403** | SR-15 |
> | `Origin` present И вне allowlist | **403** | SR-16 |
> | Превышение rate-limit (per-key или per-IP) | **429** | SR-17 |
> | Маршрут не реализован (catch-all exec/MCP/upload) | **501** | SR-23 |

## Транспорт / TLS (baseline §2)

- [ ] **SR-1. TLS обязателен, `MinVersion: tls.VersionTLS13`.** `tls.Config` транспорта задаёт
  `MinVersion: tls.VersionTLS13`; TCP-listener обёрнут в `crypto/tls`. Проверка: тест — клиент с
  `tls.Config{MaxVersion: tls.VersionTLS12}` против listener получает ошибку handshake (не подключается);
  grep по `internal/server` подтверждает `MinVersion: tls.VersionTLS13`. (baseline §2 «TLS обязателен,
  MinVersion ≥ TLS1.3»; spec AC1; plan tls.go; threat-model R1)

- [ ] **SR-2. `CipherSuites` под TLS 1.3 НЕ задаются.** Поле `tls.Config.CipherSuites` НЕ устанавливается
  (под TLS 1.3 оно игнорируется реализацией; ручной набор шифров — анти-паттерн). Проверка: инспекция/grep
  по `internal/server/tls.go` — `CipherSuites` отсутствует в `tls.Config`. (baseline §2; research «TLS 1.3
  ciphersuites are not configurable»; plan AC1; threat-model R1)

- [ ] **SR-3. Сертификат self-signed, ключ — ECDSA P-256, корректный SAN.** При генерации: ключ
  `ecdsa.GenerateKey(elliptic.P256(), rand.Reader)`; серт self-signed (`x509.CreateCertificate` с
  template==parent); SAN содержит `127.0.0.1` (`IPAddresses`) и `localhost` (`DNSNames`); установлены
  `NotBefore/NotAfter` (конечный срок), `KeyUsage`/`ExtKeyUsage` для server-auth. Проверка: тест —
  сгенерированный серт парсится, SAN содержит `127.0.0.1` и `localhost`, публичный ключ — ECDSA P-256,
  серт self-signed (issuer==subject). (baseline §2; spec AC2; research self-signed/P-256; threat-model R1)

- [ ] **SR-4. Приватный TLS-ключ создаётся с правами `0600`, сертификат `0644`.** Файл приватного ключа в
  `PathSet.TLSDir` — права ровно `0600`; файл сертификата — `0644`; каталог `TLSDir` не шире `0700`
  (создаётся `config.EnsureDirs`). Проверка: тест — `os.Stat(key.pem).Mode().Perm() == 0o600` и
  `os.Stat(cert.pem).Mode().Perm() == 0o644` сразу после генерации. (baseline §2 «ключ 0600»; spec AC2;
  plan tls.go; threat-model R10)

- [ ] **SR-5. Существующая валидная пара серт+ключ переиспользуется, без перегенерации.** При наличии
  валидной пары в `TLSDir` — загрузка через `tls.LoadX509KeyPair`, повторная генерация НЕ выполняется.
  Проверка: тест — два вызова `server.New`/`loadOrCreateCert` дают один и тот же серт (идентичные байты/
  серийный номер), файлы не перезаписаны. (baseline §2; spec AC3; plan AC2/AC3; threat-model R10)

- [ ] **SR-6. Пустой/повреждённый серт → явная ошибка, без перезаписи и без паники.** Пустой/битый
  `cert.pem`/`key.pem` → sentinel `ErrTLSCert` (или эквивалент), серт НЕ перезаписывается молча, процесс
  завершается с ненулевым кодом и понятным сообщением (без паники). Проверка: тест — подсунутый пустой/битый
  серт → ненулевой код выхода, файл байт-в-байт не изменён, паники нет. (baseline §2; spec AC13; plan
  ErrTLSCert; threat-model R10)

- [ ] **SR-7. По умолчанию bind на `127.0.0.1`; адрес/порт конфигурируемы.** Дефолт `config.Config.BindAddr
  = "127.0.0.1"`; порт из `config.Config.Port` (дефолт 7822); сервер биндит `BindAddr:Port`. Проверка:
  тест — при дефолтной конфигурации сокет недоступен с не-loopback адреса (попытка подключения с внешнего
  адреса не проходит). (baseline §2 «локально — bind 127.0.0.1»; spec AC7; plan config.go; threat-model R14)

## Аутентификация запросов (baseline §1)

- [ ] **SR-8. Аутентификация выполняется ДО любой обработки/маршрутизации.** `authMiddleware` стоит в
  цепочке ДО `http.ServeMux`; ВСЕ маршруты (включая `/healthz` и catch-all) — за единой middleware-цепочкой;
  запрос без валидного ключа НЕ достигает ни одного обработчика. Проверка: тест — запрос без ключа и с
  неизвестным/отозванным ключом НЕ доходит до health-обработчика (нет `pong`, нет побочных эффектов).
  (baseline §1; spec AC4/AC5; plan «Verify ДО любой обработки»; threat-model R3)

- [ ] **SR-9. Ключ извлекается из `Authorization: Bearer` и проверяется через `keystore.Verify`.**
  Транспорт читает токен из заголовка `Authorization: Bearer <ключ>` и вызывает `store.Verify(токен)`;
  нет/не-`Bearer`/пустой токен → **401**; `Verify → (_, false, nil)` → **401**; успех → `Fingerprint`+id
  кладутся в контекст запроса для аудита и rate-limit. Транспорт НЕ реализует собственного сравнения
  ключей/хэшей. Проверка: тест — валидный Bearer проходит, остальные случаи → 401 (по таблице маппинга);
  инспекция — сравнение только через `keystore.Verify`. (baseline §1; spec AC4/AC5; plan auth.go;
  threat-model R3/R5)

- [ ] **SR-10. Сравнение секретов только constant-time (через keystore).** Транспорт НЕ сравнивает ключи/
  хэши операторами `==`/`bytes.Equal`/`strings.EqualFold`; единственный путь сверки ключа — `keystore.Verify`
  (внутри `subtle.ConstantTimeCompare`, перебор всех записей без раннего выхода). Проверка: grep по
  `internal/server` — нет сравнения ключа/токена/хэша небезопасными операторами; инспекция auth.go.
  (baseline §1 «только constant-time»; threat-model R5)

- [ ] **SR-11. Отозванный ключ перестаёт проходить немедленно.** `Verify` перебирает только активные записи
  (revoked исключены — подтверждено по коду keystore); ранее валидный, затем отозванный ключ сразу не
  проходит и не достигает обработчика. Проверка: тест — запрос с ключом успешен ДО `Revoke`, и тот же ключ
  → 401 СРАЗУ после `Revoke`. (baseline §1 «отзыв мгновенный, дальнейшие запросы → отказ»; spec AC5;
  threat-model R3)

- [ ] **SR-12. Ключ принимается ТОЛЬКО из заголовка, НЕ из argv/env.** Ни `raxd serve`, ни транспорт НЕ
  принимают тело ключа как флаг/позиционный аргумент и не читают его из переменных окружения (защита от
  утечки через `ps`/`/proc`). Проверка: инспекция `internal/cli/serve.go` и `internal/server/*` — нет
  чтения ключа из argv/env; ключ берётся из `Authorization`. (baseline §1, SR-14; spec AC4; threat-model R12)

- [ ] **SR-13. Отказ без раскрытия причины; повреждение `keys.db` в рантайме → 403.** Ответ клиенту на
  любой провал аутентификации НЕ раскрывает, почему (не «нет такого id»/«отозван»/«битый файл») — единый
  необъясняющий отказ. `Verify → error`/`ErrCorrupt` (повреждённый `keys.db` в рантайме = нет валидных
  ключей) → **403** + аудит-запись. Различие причин фиксируется ТОЛЬКО в аудит-логе сервера (SR-19), не в
  теле ответа. Проверка: тест — тело ответа 401/403 не содержит идентифицирующих деталей; подсунутый битый
  `keys.db` → 403, без паники. (baseline §1; spec AC5/AC13; plan auth.go/ErrCorrupt→403; threat-model R4/R13)

## DNS-rebinding: Host/Origin (baseline §2)

- [ ] **SR-14. Host/Origin-валидация выполняется в middleware ДО auth/обработки.** `hostOriginMiddleware`
  стоит в цепочке до auth/маршрутизации. Проверка: инспекция порядка цепочки —
  `audit → recover → Host/Origin → auth → rate-limit → mux` (как в plan). (baseline §2; spec Q2/ADR-002;
  threat-model R2)

- [ ] **SR-15. `Host` вне allowlist → 403; дефолт allowlist `localhost`/`127.0.0.1`/`::1`.** Заголовок
  `Host` (хост-часть, порт игнорируется) сверяется с allowlist; вне списка → **403**. Проверка: тест —
  запрос с `Host: evil.example.com` → 403; запрос с `Host: 127.0.0.1`/`localhost` проходит дальше.
  (baseline §2; research Host-allowlist rmcp; plan hostOriginMiddleware; threat-model R2)

- [ ] **SR-16. `Origin` present И вне allowlist → 403; отсутствие `Origin` → НЕ отклонять.** Если заголовок
  `Origin` присутствует и не входит в allowlist → **403**; если `Origin` ОТСУТСТВУЕТ (типичные
  не-браузерные raxd-агенты: curl/SDK) → запрос НЕ отклоняется только из-за отсутствия Origin. Проверка:
  тест — present&invalid Origin → 403; отсутствующий Origin → проходит дальше (до auth). (baseline §2;
  research MCP «if Origin present and invalid → 403»; plan ADR-002; threat-model R2; ОР-3)

## Rate limiting (baseline §4)

- [ ] **SR-17. Rate-limit per-key И per-IP (token bucket), 429 при превышении.** На базе
  `golang.org/x/time/rate`: отдельный `*rate.Limiter` per-`Fingerprint` (ключ) и per-IP; при превышении
  любого из лимитов → **429** + аудит-запись (SR-20); лимит и burst имеют разумные дефолты и конфигурируемы.
  Проверка: тест — всплеск запросов сверх лимита получает 429 (per-key и per-IP проверяются отдельно).
  (baseline §4 «rate limiting per-key и per-IP, 429»; spec AC6; plan ratelimit.go; threat-model R8)

- [ ] **SR-18. Хранилище лимитеров потокобезопасно и не «течёт» памятью.** `map[key]*rate.Limiter` /
  `map[ip]*rate.Limiter` защищены `sync.Mutex` (или эквивалент); лениво создаваемые лимитеры удаляются
  фоновой TTL-очисткой по времени последнего обращения; GC-горутина останавливается по контексту `Run`.
  Проверка: тест/инспекция — все обращения к map под мьютексом (нет гонок при `-race`); тест — после TTL
  простаивающие лимитеры удалены (map не растёт безгранично). (baseline §4; spec AC6/OQ-2; plan
  Trade-offs map+mutex+TTL-GC; threat-model R11)

## Аудит / устойчивость (baseline §4)

- [ ] **SR-19. Аудит на КАЖДОЕ соединение: timestamp, Fingerprint, удалённый адрес, результат, причина.**
  Структурированная (JSON) запись через `charmbracelet/log`: `TS` (UTC), `Fingerprint`
  (`keystore.Fingerprint`, НЕ ключ; для неаутентифицированных — пусто/`-`, не raw-ключ), `RemoteAddr`,
  `Result` (success/fail/rate-limited), `Reason`. Проверка: тест — для success, fail и rate-limited
  присутствует JSON-запись с обязательными полями. (baseline §4 «аудит каждого действия: timestamp,
  fingerprint, адрес»; spec AC8; plan audit.go; threat-model R7)

- [ ] **SR-20. Неуспешные аутентификации и срабатывания rate-limit логируются обязательно.** Каждый отказ
  401/403 и каждое 429 порождают аудит-запись (для обнаружения всплеска отказов). Проверка: тест — запрос
  без/с битым ключом и всплеск сверх лимита дают соответствующие аудит-записи (fail / rate-limited).
  (baseline §4 «логировать неудачные аутентификации и аномалии»; spec AC8; threat-model R7/R8)

- [ ] **SR-21. Никаких секретов в логах/выводе/ошибках (raw `Authorization` не логируется).** В аудит-логе,
  выводе сервера и сообщениях об ошибках НЕТ тела ключа, его хэша, соли, raw-заголовка `Authorization` и
  приватного TLS-ключа. Идентификация только по `Fingerprint`. Проверка: тест — предъявленный полный ключ
  как ПОДСТРОКА ОТСУТСТВУЕТ в захваченном выводе аудита/сервера; grep по `internal/server` не находит
  логирования `r.Header.Get("Authorization")`/тела ключа/приватного ключа. (baseline §4 «никаких секретов
  в логах»; §1; spec AC9; threat-model R6)

- [ ] **SR-22. Health-обработчик доступен только после успешной аутентификации; единственная операция.**
  Аутентифицированный `ping` → `pong` (или эквивалент «жив») — единственная реально обрабатываемая операция.
  Проверка: тест — `GET /healthz` с валидным ключом → `pong`; без ключа → не достигает обработчика (SR-8).
  (baseline §1/§4; spec AC10; plan handlers.go; threat-model R3)

- [ ] **SR-23. Catch-all диспетчер возвращает 501 без побочных эффектов.** Маршрутизация на будущие
  exec/MCP/upload — явная заглушка «not implemented» (501), НЕ исполняющая никаких команд и не имеющая
  побочных эффектов. Проверка: тест — запрос на нереализованный маршрут с валидным ключом → 501, без
  исполнения/изменения состояния хоста. (baseline §3 — наследуемая точка расширения; spec AC10; plan
  dispatchHandler; threat-model R15)

- [ ] **SR-24. Graceful shutdown: `Shutdown → FlushUsage`, без зависания.** По SIGINT/SIGTERM или отмене
  контекста: `http.Server.Shutdown(ctxDeadline)` (перестать принимать новые соединения, корректно завершить
  активные) → ЗАТЕМ `Store.FlushUsage()`; завершение в пределах разумного дедлайна; `ErrServerClosed`
  трактуется как успех. Порядок строго `Shutdown → FlushUsage`. Проверка: тест — `Run` с отменяемым
  контекстом завершается в пределах дедлайна и `FlushUsage` вызван ПОСЛЕ `Shutdown`. (baseline §4 «graceful»;
  spec AC12; plan Run; threat-model R13)

- [ ] **SR-25. Таймауты соединения и лимит размера запроса.** На `http.Server` заданы `ReadTimeout`,
  `ReadHeaderTimeout`, `WriteTimeout`, `IdleTimeout` (защита от Slowloris) и ограничение размера тела/
  заголовков (`MaxBytesReader`/`MaxHeaderBytes`); обрыв соединения на середине handshake обрабатывается без
  паники (recover-middleware). Проверка: инспекция `internal/server/server.go` — таймауты и лимит тела
  заданы (не нулевые); тест — обрыв handshake/медленный клиент не вешает сервер и не паникует. (baseline §4
  «устойчивость»; spec AC13; plan middleware recover; threat-model R9)

## Среда (baseline §6)

- [ ] **SR-26. Все проверки прогоняются в Docker, офлайн из `vendor/`.** Все тесты этой задачи (TLS-версия,
  права файлов, переиспользование серта, аутентификация, маппинг кодов, Host/Origin, rate-limit, аудит без
  секретов, graceful shutdown, ping→pong, edge-cases) зелёные в Docker; сборка/тесты `-mod=vendor`, без
  `go mod download`; новая зависимость `golang.org/x/time/rate` завендорена (`vendor/` + `go.sum`
  закоммичены, ADR-002). Проверка: CI/локальный прогон в контейнере проходит из `vendor/`; на хосте `raxd`
  НЕ запускается. (baseline §6; spec AC11/AC14; plan вендоринг x/time/rate; threat-model ОР-2)

## Вне scope этой задачи (фиксация, не требование к tls-transport)

- **mTLS / клиентские сертификаты** (baseline §2 «mTLS опционально») — отложено решением дирижёра; принятый
  риск с компенсацией зафиксирован в `threat-model.md` ОР-1, эскалирован для не-loopback bind.
- **Запуск НЕ от root, capabilities для порта <1024, `Restart=on-failure`** (baseline §3/§4) — задача
  `service-install`/`distribution`; передано как обязательное требование (threat-model ОР-2).
- **Выполнение команд, allowlist, таймауты команд, `exec.Command` без shell** (baseline §3) — задача
  `command-exec`; здесь только заглушка-диспетчер 501 (SR-23) как точка расширения.
- **Полная браузер-ориентированная Origin-политика** (MCP MUST) — задача `mcp-server`; в v1 лёгкий
  middleware (SR-15/SR-16), полная политика эскалирована (threat-model ОР-3).
- **Ротация и файловый бэкенд аудит-лога** (baseline §4 «с ротацией») — уровень сервиса/дистрибуции; здесь
  обязательны только структура и поля записи (SR-19), threat-model ОР-4.
- **Управление ключами** (`key create|list|delete`, генерация, хранение, формат, constant-time `Verify`,
  fingerprint) — готово в `key-management`; здесь только ПОТРЕБЛЕНИЕ контракта `keystore.Verify`/`Fingerprint`.
- **Install-скрипт / `SHA256SUMS` / подпись-нотаризация** (baseline §5) — задача `distribution`.
- **Финальный визуальный дизайн вывода `serve`/статус-блоков** — задача `cli-ux`.
