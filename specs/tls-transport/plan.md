# Plan: TLS Transport — защищённый аутентифицированный сетевой транспорт raxd

Автор плана: architect (raxd). Вход: spec.md (AC1–AC14), research.md, ADR-001 (HTTP/TLS), ADR-002 (Origin/Host).
Автор продукта: Vladimir Kovalev, OEM TECH.

## Chosen Approach
Строим транспорт как **HTTP/1.1+ поверх `crypto/tls` (`net/http`)**, ADR-001 переводится в `accepted`.
Сервер слушает `127.0.0.1:Port` (TLS 1.3, self-signed ECDSA P-256), пропускает каждый запрос через
фиксированную цепочку middleware **аудит → recover → Host/Origin → auth (`keystore.Verify`) → rate-limit**
ДО маршрутизации; за ней — `http.ServeMux` с health-обработчиком (`GET /healthz` → `pong`) и
catch-all диспетчером «not implemented». Выбор обоснован совместимостью с будущим MCP Streamable HTTP
(официальный SDK — `http.Handler` на том же mux), нативными заголовками для ключа/Origin/Host (AC4/Q2),
зрелым stdlib (`Server.Shutdown`, `httptest`) под AC1/AC12/AC14 без ручного фрейминга. Сырой TCP отклонён
(см. Trade-offs). `raxd serve` оживляется как foreground-запуск этого сервера (Docker-only, AC11).

## Modules
- `internal/server/server.go` — тип `Server`: владеет `http.Server`+`tls.Config`, lifecycle (`Run`/graceful
  shutdown), сборка mux и middleware-цепочки, вызов `Store.FlushUsage` после `Shutdown`.
- `internal/server/tls.go` — управление сертификатом: загрузка пары из `TLSDir` или генерация self-signed
  ECDSA P-256 (SAN `127.0.0.1`+`localhost`), права ключ `0600`/серт `0644`, переиспользование (AC2/AC3/AC13).
- `internal/server/middleware.go` — middleware-обёртки (тип `func(http.Handler) http.Handler`): auth,
  Host/Origin, rate-limit, audit, recover; константы кодов отказа (AC4/AC5/AC6/AC8/AC13).
- `internal/server/auth.go` — извлечение ключа из `Authorization: Bearer`, вызов `keystore.Verify`,
  проброс `Fingerprint` и результата в контекст запроса для аудита (AC4/AC5/AC8/AC9).
- `internal/server/ratelimit.go` — тип `Limiters`: per-key и per-IP token-bucket поверх
  `golang.org/x/time/rate`, потокобезопасное хранилище + TTL-очистка (AC6, OQ-2).
- `internal/server/audit.go` — структурированная JSON-запись через `charmbracelet/log` (уже в стеке):
  поля timestamp/fingerprint/remote/result/reason; гарантия отсутствия секретов (AC8/AC9).
- `internal/server/handlers.go` — `healthHandler` (ping→pong) и `dispatchHandler` (catch-all
  «not implemented», 501) как явная точка расширения под mcp-server/command-exec (AC10).
- `internal/config/config.go` — расширить `Config`: `BindAddr string` (дефолт `127.0.0.1`), `RateLimit`/
  `RateBurst`, `OriginAllow`/`HostAllow []string`, таймауты; источник порта прежний (`Config.Port`).
- `internal/cli/serve.go` — заменить honest-stub на запуск `server.Server` в foreground (AC11).

## Contracts
- `server.New(cfg *config.Config, paths config.PathSet, store *keystore.Store, log *log.Logger) (*Server, error)`
  - параметры: загруженный конфиг (адрес/порт/лимиты/таймауты), пути (`TLSDir`), открытый keystore, логгер аудита;
  - возврат: готовый `*Server` (TLS-конфиг собран, mux+middleware подключены);
  - ошибки: оборачивает `ErrTLSCert` (повреждённый/нечитаемый серт, AC13) и ошибки генерации; не паникует.
- `(*Server) Run(ctx context.Context) error`
  - параметры: контекст, чья отмена (или SIGINT/SIGTERM, перехватываемые в `serve.go`) инициирует shutdown;
  - возврат: `nil` при штатном завершении; биндит `cfg.BindAddr:cfg.Port`, по отмене вызывает
    `http.Server.Shutdown(ctxDeadline)`, затем `store.FlushUsage()`, ждёт `ErrServerClosed` (AC12);
  - ошибки: `ErrPortInUse` при занятом порте (AC13); `ErrServerClosed` поглощается как успех.
- `loadOrCreateCert(tlsDir string) (tls.Certificate, error)` (внутр., `tls.go`)
  - параметры: каталог `TLSDir`; возврат: загруженная (AC3) или сгенерированная (AC2) пара;
  - ошибки: `ErrTLSCert` (sentinel) при пустом/битом файле; права ключ `0600`, серт `0644` выставляются явно.
- `authMiddleware(store *keystore.Store, audit AuditFn) func(http.Handler) http.Handler` (`auth.go`)
  - извлекает `Bearer`-токен (НЕ argv/env, AC4); `store.Verify` → при `(false,nil)`/пусто/`Bearer` нет → **401**;
    при `Verify`-`error`/`ErrCorrupt` → **403** + аудит (AC5/AC13); успех → кладёт `Fingerprint`+id в контекст.
- `(*Limiters) Allow(key, ip string) (ok bool)` (`ratelimit.go`)
  - lazy-создание `*rate.Limiter` per-key и per-IP под `sync.Mutex`; `false` → **429** + аудит (AC6);
  - очистка: фоновая горутина по TTL (последнее обращение) удаляет простаивающие лимитеры (OQ-2),
    останавливается по контексту `Run`; всё хранилище под мьютексом (потокобезопасность).
- `hostOriginMiddleware(hostAllow, originAllow []string) func(http.Handler) http.Handler` (ADR-002)
  - `Host` вне allowlist → **403**; `Origin` present И вне allowlist → **403**; `Origin` отсутствует → пропуск.
- `auditMiddleware(audit AuditFn) func(http.Handler) http.Handler`; `AuditFn func(rec AuditRecord)`
  - `AuditRecord{TS time.Time; Fingerprint, RemoteAddr, Result, Reason string}` — JSON, БЕЗ тела ключа/хэша/salt (AC8/AC9).

## Поток запроса (end-to-end)
TCP → TLS 1.3 handshake (handshake-обрыв → закрытие без паники, AC13) → `auditMiddleware` (deferred-запись) →
`recover` → `hostOriginMiddleware` → `authMiddleware` (Bearer → `Verify` ДО любой обработки) → `Limiters.Allow`
(per-`Fingerprint` + per-IP) → mux: `GET /healthz`→`pong` (AC10) | catch-all→501. Аудит на КАЖДОЕ соединение:
`Fingerprint` (не тело), `RemoteAddr`, success/fail+reason. `FlushUsage` — один раз при graceful shutdown в `Run`.

## Привязка к AC
| AC | Модуль/контракт |
|---|---|
| AC1 (TLS1.3) | `tls.go`: `MinVersion: tls.VersionTLS13`, CipherSuites НЕ задаём |
| AC2/AC3/AC13(серт) | `tls.go: loadOrCreateCert` (генерация/переиспользование/`ErrTLSCert`) |
| AC4/AC5 | `auth.go: authMiddleware` (Bearer→`Verify` до обработки; 401/403) |
| AC6 | `ratelimit.go: Limiters.Allow` (per-key/per-IP, 429) |
| AC7 | `config.go: BindAddr=127.0.0.1`, `Server.Run` бинд `Config.Port` |
| AC8/AC9 | `audit.go: AuditRecord` (Fingerprint, без секретов) |
| AC10 | `handlers.go: healthHandler` + `dispatchHandler` (501) |
| AC11 | `cli/serve.go` foreground запуск `Server.Run` |
| AC12 | `server.go: Run` → `Shutdown(ctx)` → `FlushUsage` |
| AC13(порт/keys.db/handshake) | `ErrPortInUse`; `Verify (_,false,nil)`/`ErrCorrupt`→403; `recover` |
| AC14 | тестируется в Docker из `vendor/` (см. ниже) |

## Trade-offs
- Выбрали **HTTP/TLS** вместо **сырого TCP-фрейминга**: цена — HTTP-оверхед на кадр (для health/команд
  пренебрежим) и аккуратная связка SSE↔`Shutdown` в будущем MCP (вне scope); взамен — нулевой ручной
  фрейминг, нативные Bearer/Origin/Host, `httptest`, прямая совместимость с MCP `StreamableHTTPHandler`.
- Очистка rate-лимитеров: выбрали **`map`+`sync.Mutex`+фоновый TTL-GC** вместо `sync.Map`/LRU (OQ-2):
  цена — одна фоновая горутина и блокировка на запись; взамен — предсказуемое удаление простоя без вытеснения
  активных ключей. Недоказанную формулу «P-256≈RSA-3072» (OQ-1) в plan/docs НЕ используем.
- Origin/Host (ADR-002): в v1 «лёгкий» middleware (403 при present&invalid Host/Origin, пропуск при
  отсутствии Origin) — цена: полная браузерная Origin-политика отложена в `mcp-server`; взамен не ломаем
  не-браузерных агентов, защиту держим на bind+ключ+rate-limit.
- Новая зависимость **`golang.org/x/time/rate`** (есть в STACK.ru.md, отсутствует в `go.mod`): обоснование —
  baseline §4 предписывает token-bucket; developer ОБЯЗАН `go mod vendor` + коммит `vendor/`/`go.sum`
  (offline, ADR-002 key-management). Остальное — stdlib (`net/http`, `crypto/{tls,x509,ecdsa}`).

## Тестируемость в Docker (для qa, baseline §6/AC14)
Сборка/тесты `-mod=vendor` в контейнере, без `go mod download`. Стратегия: AC1 — клиент `tls.Config{MaxVersion:
TLS12}` против listener → handshake-ошибка; AC2/AC3 — проверка прав `0600`/`0644` и идентичности серта между
двумя `New`; AC4/AC5/AC6 — `httptest.NewTLSServer` + запросы без/с битым/валидным Bearer и всплеск >лимита;
AC8/AC9 — захват вывода `charmbracelet/log`, проверка что предъявленный ключ НЕ встречается подстрокой; AC12 —
`Run` с отменяемым контекстом, замер дедлайна; AC13 — битый серт/занятый порт/`ErrCorrupt` keys.db → ненулевой код.

## Точки расширения
`dispatchHandler` и `http.ServeMux` спроектированы так, чтобы mcp-server подключил
`StreamableHTTPHandler` (это `http.Handler`) как маршрут `/mcp` за той же middleware-цепочкой, а command-exec —
свой обработчик; диспетчер переписывать не придётся, меняется только регистрация маршрутов в `server.New`.
