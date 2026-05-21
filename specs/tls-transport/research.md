# Research: TLS Transport — протокол поверх TLS, Origin-валидация и технические факты Go

Автор research: research-analyst (raxd). Дата: 2026-05-21. Язык: русский.
Вход: `specs/tls-transport/spec.md`, `.claude/reference/{SECURITY-BASELINE,STACK,MCP-INTEGRATION}.ru.md`,
контракт `internal/keystore` (`Verify`/`Fingerprint`).

> Это рекомендации для **architect** (он выбирает финальную архитектуру) и факты для developer.
> Каждый нетривиальный факт сопровождён URL. Код здесь не пишется.

## Вопросы

- **Q1 (главный):** протокол поверх TLS — сырой TCP-протокол приложения vs HTTP/TLS. Критерии:
  совместимость с будущим MCP Streamable HTTP, Origin/Host-валидация (HTTP-заголовки), простота
  аутентификации (ключ НЕ через argv/env), зрелость stdlib, тестируемость, расширяемость под
  command-exec/file-upload. (Спека: Open Question Q1, дефолт PM — HTTP/TLS.)
- **Q2:** Origin-валидация — тайминг и реализация (middleware, allowlist, поведение при отсутствии
  заголовка для не-браузерных клиентов); включать сразу или закладывать под `mcp-server`. (Спека Q2.)
- **Q3 (mTLS):** РЕШЕНО дирижёром — отложено. Здесь только подтверждаем обоснование отсрочки.
- **Технические факты** для AC2/AC1/AC6/AC12/AC4/AC8 (self-signed x509, TLS 1.3, rate limiting,
  graceful shutdown, аутентификация-middleware, вендоринг новых зависимостей).

---

## Найдено (факт → источник URL)

### MCP-транспорт и его требования к HTTP-слою (для Q1/Q2)

- **MCP Streamable HTTP — это HTTP, единый эндпоинт с POST и GET.** Спецификация (актуальная версия
  **2025-11-25**): «The server MUST provide a single HTTP endpoint path … that supports both POST and
  GET methods. For example … `https://example.com/mcp`». → https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
- **Security Warning спеки (важно для Q2 и baseline §2):** «Servers MUST validate the `Origin` header
  on all incoming connections to prevent DNS rebinding attacks. If the `Origin` header is present and
  invalid, servers MUST respond with HTTP 403 Forbidden». Далее: «When running locally, servers SHOULD
  bind only to localhost (127.0.0.1)…» и «Servers SHOULD implement proper authentication for all
  connections». → https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
  - Замечание: формулировка «if the Origin header is present and invalid» означает, что **403 —
    обязателен только при наличии заголовка и его несовпадении**; поведение при ОТСУТСТВИИ Origin спека
    напрямую не предписывает (не-браузерные клиенты, такие как curl/SDK, Origin обычно не шлют).
- **MCP security best practices (2025-11-25):** «MCP servers that implement authorization MUST verify
  all inbound requests. MCP Servers MUST NOT use sessions for authentication.» И для локального
  HTTP-транспорта: «Restrict access if using an HTTP transport, such as: Require an authorization
  token…». → https://modelcontextprotocol.io/specification/2025-11-25/basic/security_best_practices
- **DNS-rebinding на практике (CVE rmcp, defense-in-depth):** в Rust SDK rmcp (< 1.4.0) транспорт
  Streamable HTTP не валидировал заголовок и был уязвим к DNS-rebinding. Фикс ввёл проверку **`Host`**
  по allowlist с дефолтом `["localhost","127.0.0.1","::1"]` (вне списка → 403); валидация **Origin**
  трекается отдельно как defense-in-depth. → https://github.com/modelcontextprotocol/rust-sdk/security/advisories/GHSA-89vp-x53w-74fx
  - Вывод для нас: для НЕ-браузерных клиентов (raxd-агенты) Origin часто отсутствует; защиту от
    DNS-rebinding надёжнее держать на связке **bind 127.0.0.1 + Host-allowlist + аутентификация по
    ключу**, а Origin-валидацию — как обязательную для браузерных клиентов (MCP MUST) добавлять при
    включении mcp-server.

### Официальный Go SDK MCP (для расширяемости Q1)

- **Версия и статус:** `github.com/modelcontextprotocol/go-sdk` — **v1.6.0, опубликован 30.04.2026**
  (стабильный, активно сопровождается). → https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp
  - (STACK.ru.md указывает «официальный, v1.x» — факт уточнён до конкретной версии.)
- **Streamable HTTP в SDK — это `http.Handler`:** SDK экспортирует
  `func NewStreamableHTTPHandler(getServer func(*http.Request) *Server, opts *StreamableHTTPOptions) *StreamableHTTPHandler`
  с методом `ServeHTTP(w http.ResponseWriter, req *http.Request)`. То есть MCP-эндпоинт встраивается в
  обычный `http.ServeMux`/middleware-цепочку. → https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp
  - Прямое следствие для Q1: при HTTP/TLS будущий `mcp-server` подключается как ещё один маршрут к
    существующему `http.Server`; при сыром TCP пришлось бы поднимать отдельный HTTP-листенер или мост.

### TLS 1.3 в Go stdlib (для AC1)

- **`Config.MinVersion` + `tls.VersionTLS13`:** «MinVersion contains the minimum TLS version that is
  acceptable. By default, TLS 1.2 is currently used as the minimum.» Константа `VersionTLS13 = 0x0304`.
  → https://pkg.go.dev/crypto/tls
- **Cipher suites под TLS 1.3 НЕ настраиваются (важно — НЕ задавать `CipherSuites`):** документация
  `Config.CipherSuites`: «CipherSuites is a list of enabled TLS 1.0–1.2 cipher suites. … Note that TLS
  1.3 ciphersuites are not configurable.» → https://pkg.go.dev/crypto/tls
  - Подтверждение в исходниках/issue: при `MinVersion = TLS1.3` поле `CipherSuites` игнорируется,
    набор шифров фиксирован реализацией. → https://github.com/golang/go/issues/29349
- **Серверный сертификат:** «Server configurations must set one of Certificates, GetCertificate or
  GetConfigForClient.» Загрузка PEM-пары: `tls.LoadX509KeyPair(certFile, keyFile)` → кладётся в
  `Config.Certificates`. → https://pkg.go.dev/crypto/tls

### Self-signed x509 (для AC2/AC3)

- **`x509.CreateCertificate`:** сигнатура
  `func CreateCertificate(rand io.Reader, template, parent *Certificate, pub, priv any) ([]byte, error)`,
  возвращает DER. «If parent is equal to template then the certificate is self-signed» — для
  self-signed передаём один и тот же template дважды. → https://pkg.go.dev/crypto/x509
- **Поля шаблона для SAN/срока:** `Certificate` содержит `IPAddresses []net.IP`, `DNSNames []string`,
  `NotBefore/NotAfter time.Time`, `SerialNumber *big.Int`, `KeyUsage`, `ExtKeyUsage`,
  `BasicConstraintsValid bool` — то есть `127.0.0.1`/`localhost` кладутся в SAN (`IPAddresses`/
  `DNSNames`), срок — в `NotBefore/NotAfter`. → https://pkg.go.dev/crypto/x509
- **Ключ — ECDSA на кривой P-256 (secp256r1):** P-256 — допустимая кривая для TLS 1.3: RFC 8446
  перечисляет `secp256r1(0x0017)` среди named groups (ECDHE) и `ecdsa_secp256r1_sha256(0x0403)` среди
  signature algorithms. → https://www.rfc-editor.org/rfc/rfc8446 (§4.2.7 Supported Groups, §4.2.3
  Signature Algorithms). P-256 (она же secp256r1/prime256v1) входит в набор эллиптических кривых,
  рекомендованных NIST SP 800-186 (февраль 2023, «specifies the set of elliptic curves recommended for
  U.S. Government use»). → https://csrc.nist.gov/pubs/sp/800/186/final ,
  PDF: https://nvlpubs.nist.gov/nistpubs/SpecialPublications/NIST.SP.800-186.pdf
  - Для self-signed «свой клиент ↔ свой сервер» legacy-совместимость не требуется → ECDSA P-256 уместен
    (поддерживается stdlib без дополнительных зависимостей).
    Генерация: `ecdsa.GenerateKey(c elliptic.Curve, r io.Reader)` с `elliptic.P256()` — `crypto/ecdsa`
    явно перечисляет P-256 среди поддерживаемых кривых (constant-time). → https://pkg.go.dev/crypto/ecdsa
    Ключ сериализуется в PKCS#8 PEM: `x509.MarshalPKCS8PrivateKey` поддерживает `*ecdsa.PrivateKey`.
    → https://pkg.go.dev/crypto/x509
  - ⚠️ Количественная формулировка «P-256 ≈ RSA-3072 по стойкости и даёт более быстрый handshake/меньший
    серт» в нормативных источниках (RFC 8446 / NIST SP 800-186 / Go docs) прямо НЕ подтверждена —
    перенесена в «Открытые вопросы» (OQ-1). Подтверждённый факт: P-256 — валидная для TLS 1.3 кривая,
    рекомендованная NIST, поддерживаемая stdlib; ECDSA-ключи P-256 короче RSA при сопоставимом уровне
    стойкости (точная эквивалентность бит — предмет проверки по NIST SP 800-57 Part 1, см. OQ-1).

### Rate limiting (для AC6)

- **`golang.org/x/time/rate` — token bucket, актуальная версия v0.15.0 (11.02.2026), активно
  поддерживается, импортируется ~14k пакетами.** API:
  `func NewLimiter(r Limit, b int) *Limiter` (r — событий/сек, b — burst), `Allow() bool`,
  `func Every(interval time.Duration) Limit`, `Wait(ctx)`, `Reserve()`.
  → https://pkg.go.dev/golang.org/x/time/rate
  - Паттерн per-key/per-IP: один `*rate.Limiter` на ключ/IP, хранимый в `map[ключ]*rate.Limiter` под
    мьютексом (lazy-создание лимитера при первом обращении). Официальная дока `x/time/rate`
    показывает только одиночный `Limiter` и тип `Sometimes`, готового примера map-per-IP в ней нет
    → конкретная реализация хранилища лимитеров и стратегия очистки устаревших записей — деталь для
    architect/developer (см. «Технические тезисы» п.5 и «Открытые вопросы»/детали реализации).
    → https://pkg.go.dev/golang.org/x/time/rate

### Graceful shutdown (для AC12)

- **`http.Server.Shutdown(ctx context.Context) error`:** «Shutdown gracefully shuts down the server
  without interrupting any active connections. Shutdown works by first closing all open listeners,
  then … waiting … for connections to … become idle, and then shut down.» После Shutdown
  `ListenAndServe`/`Serve` возвращают `ErrServerClosed`. `Close()` — немедленное закрытие всех
  соединений. → https://pkg.go.dev/net/http#Server.Shutdown , https://pkg.go.dev/net/http#Server.Close
  - Связка с AC12: по SIGINT/SIGTERM вызвать `Shutdown(ctxWithDeadline)`; ПОСЛЕ него (или в defer)
    выполнить `Store.FlushUsage()`. Долгоживущие SSE-потоки (будущий MCP) `Shutdown` сам не прерывает —
    их завершение надо инициировать через отмену контекста; для текущей задачи (только ping→pong)
    активных стримов нет, риск не материализуется.

### Зависимости и вендоринг (для AC14, сверка с ADR-002)

- **`golang.org/x/time` ОТСУТСТВУЕТ в текущем `go.mod`** (есть `x/sys`, `x/text`, `x/exp`, но не
  `x/time`) → это **новая прямая зависимость**. По ADR-002 (вендоринг) обязателен `go mod vendor` +
  коммит обновлённого `vendor/` и `go.sum`. → файл `go.mod` репозитория;
  политика — `specs/key-management/decisions/ADR-002-vendoring-offline-builds.md`
- Если architect выберет встраивание MCP уже сейчас (НЕ требуется этой задачей), добавится
  `github.com/modelcontextprotocol/go-sdk` — тоже под вендоринг. Для tls-transport он НЕ нужен
  (out of scope), но HTTP/TLS оставляет дверь открытой без новых зависимостей.

### Контракт keystore (подтверждено по коду, для AC4/AC8)

- `func (s *Store) Verify(presented string) (Record, bool, error)` — read-only, constant-time,
  отозванные ключи исключены; буферизует LastUsed (сброс через `FlushUsage`).
  → `internal/keystore/keystore.go`
- `func Fingerprint(presented string) string` — 12 hex-символов sha256(ключа), необратим; для аудита.
  → `internal/keystore/crypto.go`
- Сентинелы: `ErrCorrupt` (повреждён keys.db). При отсутствии/пустом файле Verify → `(_, false, nil)`
  (= нет валидных ключей → отказ), что покрывает edge-case AC13. → `internal/keystore/{keystore,errors}.go`

---

## Варианты по Q1 (A/B: плюсы/минусы)

> Метод: сгенерированы оба варианта, затем каждый подвергнут острой критике; в детали выносится только
> survivor (HTTP/TLS), сырой TCP отклонён как primary с указанием причины.

- **A: Сырой TCP + кастомный фрейминг приложения поверх `crypto/tls` (`tls.Listen`).**
  - Плюсы: полный контроль над протоколом; минимальные накладные расходы на кадр; нет HTTP-семантики,
    которая «не нужна».
  - Минусы: **переизобретение** фрейминга, таймаутов чтения/записи, half-open, keep-alive —
    `net/http` это уже решает; **нет нативных Origin/Host-заголовков** → DNS-rebinding-защиту и передачу
    ключа надо делать кастомным handshake; **прямой конфликт с целью спеки** «совместимость с будущим
    MCP Streamable HTTP» — MCP только HTTP, понадобится отдельный листенер/мост (см. SDK —
    `StreamableHTTPHandler` это `http.Handler`); сложнее тестировать (нет `httptest`); graceful
    shutdown пишется руками. Источник по MCP-HTTP:
    https://modelcontextprotocol.io/specification/2025-11-25/basic/transports ;
    SDK как http.Handler: https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp
  - **Вывод критики:** оправдан только при жёстком требовании минимального бинарного протокола, которого
    спека НЕ ставит. **Отклонён как основной.**

- **B: HTTP/1.1+ поверх TLS (`net/http` `http.Server` над `tls`-конфигом / `ListenAndServeTLS`).**
  - Плюсы: зрелый stdlib; **Origin/Host — готовые заголовки** (Q2, baseline §2); **ключ через
    `Authorization: Bearer rax_live_…`** — заголовок, НЕ argv/env (AC4), совпадает с контрактом
    MCP-INTEGRATION; **аутентификация-middleware** до маршрутизации к health-обработчику (AC4/AC5);
    `httptest` для тестов (AC14); `http.Server.Shutdown(ctx)` под AC12; `StreamableHTTPHandler`
    официального SDK подключается как маршрут того же mux — расширяемость под mcp-server/command-exec/
    file-upload без смены транспорта. Источники: crypto/tls https://pkg.go.dev/crypto/tls ;
    Shutdown https://pkg.go.dev/net/http#Server.Shutdown ; MCP-HTTP
    https://modelcontextprotocol.io/specification/2025-11-25/basic/transports ;
    SDK https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp
  - Минусы: HTTP-оверхед на кадр (для команд/health пренебрежимо); SSE-стримы будущего MCP требуют
    аккуратной связки с `Shutdown` (отмена контекста), но это вне scope текущей задачи (только ping→pong).
  - **Вывод критики:** survivor.

---

## Рекомендация (для architect, не финальный выбор)

- **Q1 → вариант B (HTTP/TLS).** Это совпадает с дефолтом PM и обосновано фактами: (1) MCP Streamable
  HTTP — строго HTTP, а официальный Go SDK даёт `StreamableHTTPHandler` как `http.Handler`, поэтому
  HTTP/TLS обеспечивает прямую совместимость без новых транспортов; (2) Origin/Host — нативные
  HTTP-заголовки (baseline §2, DNS-rebinding); (3) ключ передаётся заголовком `Authorization: Bearer`
  (AC4 — не argv/env) и это тот же механизм, что в MCP-INTEGRATION; (4) `net/http`+`crypto/tls`,
  `httptest`, `Server.Shutdown` зрелы и закрывают AC1/AC12/AC14 без ручного фрейминга. Конкретная форма
  health-обработчика (ping→pong как `GET /healthz`/`POST /` JSON-RPC-подобно) — за architect.
- **Q2 → Origin/Host-валидация: закладывать middleware сразу, активную защиту строить на связке.**
  Рекомендация:
  1. Дефолт-bind `127.0.0.1` (baseline §2, MCP SHOULD) — первичная защита от DNS-rebinding (AC7).
  2. **Аутентификация по API-ключу — основной гейт** (AC4): отказ без валидного ключа отсекает
     rebinding-эксплойт независимо от заголовков.
  3. **Origin-валидация как middleware:** если заголовок `Origin` ПРИСУТСТВУЕТ и не в allowlist → 403
     (это MCP MUST для браузерных клиентов); если ОТСУТСТВУЕТ (типичные не-браузерные raxd-агенты:
     curl/SDK) → НЕ отклонять только из-за отсутствия Origin (иначе сломаются легитимные клиенты), а
     полагаться на bind+ключ. Дополнительно — Host-allowlist (`localhost`/`127.0.0.1`/`::1`) как
     defense-in-depth (паттерн из фикса rmcp). Сам middleware-каркас закладывается уже в tls-transport
     (точка расширения), полная браузер-ориентированная Origin-политика обязательна с включением
     `mcp-server`. Источники: MCP MUST по Origin
     https://modelcontextprotocol.io/specification/2025-11-25/basic/transports ; Host-allowlist-паттерн
     https://github.com/modelcontextprotocol/rust-sdk/security/advisories/GHSA-89vp-x53w-74fx ; OWASP
     по теме — см. ссылки SSRF/DNS в security_best_practices
     https://modelcontextprotocol.io/specification/2025-11-25/basic/security_best_practices
- **Q3 (mTLS) → отсрочка подтверждена.** Обоснование (зафиксировано в baseline §2 как «опционально»):
  для v1 аутентификация по API-ключу поверх TLS 1.3 покрывает требование «аутентифицировать каждое
  подключение»; mTLS добавляет управление клиентскими сертификатами и их дистрибуцию (это вес сервиса/
  distribution-задач), не давая в нашей модели угроз (один продукт-клиент↔сервер) дополнительной защиты
  сверх «ключ + TLS + bind + rate-limit». `tls.RequireAndVerifyClientCert` остаётся будущей опцией.
  → baseline §2: `.claude/reference/SECURITY-BASELINE.ru.md` ; spec Out of Scope / Q3.

### Технические тезисы для architect/developer (кратко, с фактами)

1. **TLS:** `tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}`; **НЕ**
   задавать `CipherSuites` (под TLS1.3 игнорируется). https://pkg.go.dev/crypto/tls
2. **Self-signed:** `x509.CreateCertificate(rand, tmpl, tmpl, pub, priv)` (template==parent →
   self-signed); SAN с `127.0.0.1` (`IPAddresses`) и `localhost` (`DNSNames`); ключ — ECDSA P-256
   (`crypto/ecdsa` + `elliptic.P256()`, https://pkg.go.dev/crypto/ecdsa); приватный ключ `0600`,
   серт `0644` (AC2). https://pkg.go.dev/crypto/x509
3. **Переиспользование серта (AC3):** при наличии пары в `PathSet.TLSDir` грузить через
   `tls.LoadX509KeyPair`, не перегенерировать. https://pkg.go.dev/crypto/tls
4. **Аутентификация (AC4/AC5/AC8):** middleware ДО маршрутизации; извлечь ключ из
   `Authorization: Bearer`; `keystore.Verify` → при false/err закрыть 401/403; в аудит — только
   `keystore.Fingerprint`, не тело ключа (AC9). `internal/keystore/*`
5. **Rate limit (AC6):** `golang.org/x/time/rate`; per-key/per-IP — `*rate.Limiter` на ключ/IP в
   `map[key]*rate.Limiter` + `map[ip]*rate.Limiter` под мьютексом, lazy-создание; превышение → 429 +
   аудит. **Деталь реализации для architect/developer:** официальная дока `x/time/rate` готового
   примера хранилища map-per-IP и стратегии очистки устаревших лимитеров не содержит — выбор структуры
   хранения и механизма очистки (TTL/LRU/sync.Map) оставлен на этап plan/implementation (см. OQ-2).
   https://pkg.go.dev/golang.org/x/time/rate
6. **Graceful shutdown (AC12):** SIGINT/SIGTERM → `Server.Shutdown(ctxDeadline)` → `FlushUsage()`;
   ждать `ErrServerClosed`. https://pkg.go.dev/net/http#Server.Shutdown
7. **Вендоринг (AC14):** `golang.org/x/time` — НОВАЯ зависимость → `go mod vendor` + коммит `vendor/`
   (ADR-002). `go.mod`; `specs/key-management/decisions/ADR-002-vendoring-offline-builds.md`

---

## Открытые вопросы

- **OQ-1 (факт, требует проверки по нормативу) — количественная стойкость P-256.** Утверждение
  «P-256 эквивалентна по стойкости RSA-3072 и даёт более быстрый handshake / меньший сертификат» НЕ
  подтверждено нормативными источниками, использованными в research (RFC 8446, NIST SP 800-186, Go
  docs). Подтверждено лишь: P-256 — валидная для TLS 1.3 кривая (RFC 8446), рекомендованная NIST
  (SP 800-186), поддерживаемая stdlib (`crypto/ecdsa`). Эквивалентность «P-256 ↔ ~128-бит / RSA-3072»
  принято проверить по NIST SP 800-57 Part 1 Rev. 5 (таблицы сравнения уровней стойкости) до выдачи
  как факта. На выбор кривой для self-signed «свой клиент↔сервер» это не влияет (P-256 уместна
  независимо от точной эквивалентности), но количественную формулировку в plan/docs использовать
  нельзя до проверки.
- **OQ-2 (деталь реализации, не блокирует) — стратегия хранения и очистки rate-лимитеров.** Конкретная
  структура хранилища (`map`+mutex / `sync.Map`) и механизм удаления устаревших per-IP/per-key
  лимитеров (TTL, LRU, периодический GC) официальной докой `x/time/rate` не предписаны. Решается
  architect (plan) / developer (implementation); на выбор транспорта Q1 не влияет.

- Замечания, оставленные НА РЕШЕНИЕ architect (не открытые факты, а проектные развилки в его зоне):
  - конкретная форма health-эндпоинта (REST `GET /healthz` vs JSON-RPC-стиль `POST /`) — AC10;
  - точные дефолты лимитов rate-limit и burst (спека требует «разумные дефолты, конфигурируемы») и их
    сериализация в `config.Config` — спека явно отдаёт формат конфигурации architect (Out of Scope);
  - срок жизни self-signed серта (например 1–10 лет для long-running демона) — выбрать в plan.
