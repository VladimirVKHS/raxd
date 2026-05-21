# Impl Notes: tls-transport

## Что реализовано

### `internal/server/tls.go`
Функция `loadOrCreateCert(tlsDir string) (certResult, error)`:
- Генерирует ECDSA P-256 self-signed сертификат с SAN 127.0.0.1 + localhost при первом запуске (AC2, SR-3).
- Переиспользует существующую пару через `tls.LoadX509KeyPair` без перегенерации (AC3, SR-5).
- Права: ключ `0600`, сертификат `0644`, атомарная запись через temp→chmod→rename (SR-4).
- Частичное/пустое/повреждённое состояние файлов → `ErrTLSCert` без перезаписи, без паники (AC13, SR-6).

### `internal/server/audit.go`
Тип `AuditRecord` + функция `writeAudit(logger, rec)`:
- Структурированный вывод через `charmbracelet/log` в формате `key=value` (AC8, SR-19).
- Поля: `fp` (fingerprint 12 hex), `remote` (IP:port), `reason` (при отказе).
- Уровни/метки: AUTH/INFO, FAIL/WARN, DENY/WARN, RATE/WARN (ux-spec §3).
- Никогда не логирует тело ключа, raw Authorization, соль, хэш или приватный TLS-ключ (AC9, SR-21).

### `internal/server/ratelimit.go`
Тип `Limiters` с методами `Allow(fp, ip string) bool` и `StartGC(ctx, interval)`:
- Per-fingerprint и per-IP token bucket через `golang.org/x/time/rate` (AC6, SR-17).
- Хранилище — `map[string]*limiterEntry` под `sync.Mutex`; ленивое создание лимитеров (SR-18).
- Фоновый TTL-GC удаляет простаивающие записи; горутина останавливается по контексту Run (SR-18).

### `internal/server/auth.go`
Функция `authMiddleware(store, auditFn)`:
- Извлекает Bearer-токен из заголовка `Authorization: Bearer` (AC4, SR-9, SR-12).
- Нет заголовка / не Bearer / пустой токен → 401 + FAIL-аудит.
- `store.Verify(token)` → `(_, false, nil)` → 401 + FAIL-аудит (SR-9).
- `store.Verify(token)` → `error`/`ErrCorrupt` → 403 + DENY-аудит (SR-13).
- Успех → `fp` и `rec.ID` в контекст запроса + AUTH-аудит (SR-19/SR-20).
- Никакого самостоятельного сравнения ключей/хэшей (SR-10); только через `keystore.Verify`.

### `internal/server/middleware.go`
- `hostOriginMiddleware(hostAllow, originAllow, auditFn)`: Host вне allowlist → 403 + DENY-аудит (SR-19/SR-20); Origin present & вне allowlist → 403 + DENY-аудит; отсутствие Origin → пропуск (SR-14, SR-15, SR-16). Причина отказа не передаётся в тело HTTP-ответа (anti-enumeration, ux-spec §3.5/§3.6).
- `recoverMiddleware`: перехват паник → 500, без краша сервера (SR-25, AC13).
- `rateLimitMiddleware(limiters, auditFn)`: 429 + RATE-аудит при превышении лимита (AC6, SR-17).

### `internal/server/handlers.go`
- `healthHandler`: `GET /healthz` → `200 pong` (AC10, SR-22).
- `dispatchHandler`: catch-all → `501 not implemented` без побочных эффектов (AC10, SR-23).

### `internal/server/server.go`
Тип `Server` с методами `New(cfg, paths, store, logger)` и `Run(ctx) error`:
- TLS-конфиг: `MinVersion: tls.VersionTLS13`, `CipherSuites` не задаётся (AC1, SR-1, SR-2).
- Цепочка middleware: recover → hostOrigin → auth → ratelimit → mux (порядок по plan.md).
- Bind `cfg.BindAddr:cfg.Port` (по умолчанию `127.0.0.1:7822`, AC7, SR-7).
- HTTP-таймауты: Read/ReadHeader/Write/Idle, MaxHeaderBytes (SR-25).
- Graceful shutdown: ctx → `http.Server.Shutdown(30s)` → `store.FlushUsage()` (AC12, SR-24).
- `ErrPortInUse` при занятом порте (AC13).
- Возвращает `ErrTLSCert` при повреждённом сертификате.
- `SetAfterShutdownHook(fn func())` — test seam для проверки порядка SR-24: вызывается ПОСЛЕ Shutdown(), ДО FlushUsage().

### `internal/config/config.go`
Расширение `Config`:
- Поля: `BindAddr` (дефолт `127.0.0.1`), `RateLimit` (10 req/s), `RateBurst` (20), `OriginAllow`/`HostAllow` (localhost/127.0.0.1/::1), `ReadTimeout`/`ReadHeaderTimeout`/`WriteTimeout`/`IdleTimeout`/`MaxHeaderBytes` (SR-7, SR-15, SR-16, SR-17, SR-25).
- Валидация `BindAddr` как IP через `net.ParseIP` при загрузке (ux-spec §5.6).

### `internal/cli/serve.go`
Замена честной заглушки на рабочий foreground-запуск (AC11):
- `signal.NotifyContext(SIGINT, SIGTERM)` → `srv.Run(ctx)` → вывод shutdown-блока (AC12, SR-24).
- Startup-блок по ux-spec: cert generated/loaded, key, tls, listening, warning на пустой keystore.
- Ошибки old error:/hint: по ux-spec §5.
- Ключ не передаётся через argv/env (SR-12).

## Отклонения/эскалации

### Отклонение 1: `security_static_test.go` — scope проверки `net.Listen`
Существующий тест `TestStaticNoNetListen` запрещал `net.Listen` во всём `internal/`. Это правило было написано для заглушки `serve` из bootstrap-cli. Реализация tls-transport легитимно использует `net.Listen` в `internal/server/server.go` — это и есть транспортный слой. Тест обновлён: `net.Listen` разрешён в `internal/server`, запрещён в остальных пакетах (cli, config, keystore, cmd). Architect уведомлён через комментарий в коде.

### Отклонение 2: CLI-тесты, ожидавшие честной заглушки
Тесты `TestStubServe`, `TestServeDoesNotBlock`, `TestRemainingStubErrorMessageContainsCommandName`, `TestNonKeyStubsProduceNoStdout`, `TestRemainingStubsErrorPrefix` ожидали `"not implemented yet"` для `serve`. Обновлены для нового поведения — serve запускает реальный сервер и завершается быстро только при занятом порту.

### Отклонение 3: `buildAuditMiddleware` — упрощённый паттерн
В plan описан "deferred-запись" audit через outermost middleware. Реализовано альтернативно: каждый middleware пишет аудит-запись при принятии решения (authMiddleware, hostOriginMiddleware, rateLimitMiddleware). Это функционально эквивалентно: SR-19/SR-20 выполнены — каждый отказ и успех логируется.

### Отклонение 4: сигнатура hostOriginMiddleware расширена (guardian needs-changes)
Plan.md задаёт сигнатуру без auditFn: `hostOriginMiddleware(hostAllow, originAllow []string)`. Для выполнения SR-19/SR-20 (аудит каждого отказа) сигнатура расширена до `hostOriginMiddleware(hostAllow, originAllow []string, auditFn AuditFn)`. Изменение обосновано нарушением требований безопасности в исходной реализации — устранение замечания ISSUE-1 developer-guardian.

### Решение OQ-1 (маршрут в аудит): поле `path` не добавлено
По ux-spec OQ-1 решено не добавлять — аудит уровня security, не application-лога.

## Тесты

### Покрытие AC

| AC | Тест(ы) | Статус |
|---|---|---|
| AC1 TLS 1.3 | `TestTLS13Enforced` | PASS |
| AC2 cert/key перм | `TestCertGeneratedWithCorrectPerms` | PASS |
| AC2 ECDSA P-256 self-signed SAN | `TestCertIsECDSAP256SelfSignedWithSAN` | PASS |
| AC3 переиспользование | `TestCertReusedOnSecondNew` | PASS |
| AC4 auth до обработки | `TestNoAuthReturns401`, `TestValidKeyReachesHealth` | PASS |
| AC5 отозванный ключ | `TestRevokedKeyReturns401`, `TestUnknownKeyReturns401` | PASS |
| AC6 rate-limit 429 | `TestRateLimitPerKeyReturns429`, `TestRateLimitPerIPReturns429` | PASS |
| AC7 loopback bind | `TestServerBindsLoopback` | PASS |
| AC8 audit fields | `TestAuditHasFingerprintField`, `TestAuditFailRecorded`, `TestAuditRateRecorded` | PASS |
| AC9 нет секретов в логе | `TestAuditHasNoKeyBody`, `TestAuditFailHasDash` | PASS |
| AC10 health/dispatch | `TestHealthReturnsPoong`, `TestDispatchReturns501` | PASS |
| AC11 foreground serve | `TestServeIsNoLongerStub`, `TestServeStartsRealServer` | PASS |
| AC12 graceful shutdown | `TestGracefulShutdown`, `TestGracefulShutdownOrder` | PASS |
| AC13 edge-cases | `TestCorruptCertReturnsError`, `TestPortInUse`, `TestErrCorruptReturns403` | PASS |
| AC14 Docker vendor | все тесты в Docker, -mod=vendor | PASS |

### Покрытие SR

SR-1, SR-2: TLS 1.3, нет CipherSuites — `TestTLS13Enforced` + grep инспекция.
SR-3: ECDSA P-256, SAN — `TestCertIsECDSAP256SelfSignedWithSAN`.
SR-4: key 0600, cert 0644 — `TestCertGeneratedWithCorrectPerms`.
SR-5: reuse — `TestCertReusedOnSecondNew`.
SR-6: ErrTLSCert, no overwrite — `TestCorruptCertReturnsError`.
SR-7: loopback — `TestServerBindsLoopback`.
SR-8: auth before routing — `TestNoAuthReturns401`.
SR-9: Bearer extraction, Verify — `TestUnknownKeyReturns401`.
SR-10: нет прямого сравнения — code inspection (`auth.go` — только через `keystore.Verify`).
SR-11: revoke immediate — `TestRevokedKeyReturns401`.
SR-12: нет ключа в argv/env — code inspection (`serve.go`).
SR-13: ErrCorrupt → 403 — `TestErrCorruptReturns403` (corrupts keys.db after server.New, sends Bearer token, expects 403 + DENY audit). Уточнение: `TestCorruptCertReturnsError` покрывает SR-6 (corrupt TLS cert), а не SR-13.
SR-14: Host/Origin перед auth — middleware chain order.
SR-15: invalid Host → 403 + DENY audit — `TestInvalidHostReturns403` (проверяет статус 403 и наличие "DENY"/"invalid host header" в логе).
SR-16: invalid Origin → 403 + DENY audit; absent → pass — `TestInvalidOriginReturns403` (проверяет статус 403 и наличие "DENY"/"invalid origin header" в логе), `TestAbsentOriginNotRejected`.
SR-17: 429 per-key/per-IP — `TestRateLimitPerKeyReturns429`, `TestRateLimitPerIPReturns429`.
SR-18: mutex + no race — `TestRateLimiterConcurrency` (под `-race`).
SR-19: audit fields — `TestAuditHasFingerprintField`.
SR-20: FAIL/RATE audit — `TestAuditFailRecorded`, `TestAuditRateRecorded`.
SR-21: нет секретов в логе — `TestAuditHasNoKeyBody`.
SR-22: health после auth — `TestValidKeyReachesHealth`, `TestHealthReturnsPoong`.
SR-23: 501 dispatch — `TestDispatchReturns501`.
SR-24: Shutdown → FlushUsage, deadline — `TestGracefulShutdown` (дедлайн) + `TestGracefulShutdownOrder` (порядок через SetAfterShutdownHook seam).
SR-25: таймауты — code inspection (server.go httpSrv fields).
SR-26: Docker vendor — CI прогон подтверждён.

### Команды запуска в Docker

```bash
# Полный прогон (vet + все тесты + race):
docker build --target test -t raxd-test . && docker run --rm raxd-test

# Только server-тесты с race:
docker run --rm raxd-test sh -c "CGO_ENABLED=1 go test -race -v -count=1 ./internal/server/..."

# Только vet:
docker run --rm raxd-test sh -c "go vet ./..."
```

### Результаты Docker (хвост вывода)

```
=== RUN   TestTLS13Enforced
--- PASS: TestTLS13Enforced (0.03s)
=== RUN   TestCertGeneratedWithCorrectPerms
--- PASS: TestCertGeneratedWithCorrectPerms (0.00s)
...
=== RUN   TestAuditRateRecorded
--- PASS: TestAuditRateRecorded (0.04s)
PASS
ok  github.com/vladimirvkhs/raxd/internal/server  0.647s (go test -v)
ok  github.com/vladimirvkhs/raxd/internal/server  1.899s (CGO_ENABLED=1 -race)
```

Все 28 тестов server (добавлены TestErrCorruptReturns403, TestGracefulShutdownOrder + расширены TestInvalidHostReturns403/TestInvalidOriginReturns403) + 50+ cli/config/keystore/banner/version тестов PASS.
Нет `t.Skip`, нет закомментированных проверок.

## Безопасность

| Требование | Где реализовано |
|---|---|
| Генерация: `crypto/rand` | `tls.go`: `ecdsa.GenerateKey(elliptic.P256(), rand.Reader)` |
| Хэш ключей: `sha256(key+salt)` | В `internal/keystore` (готово, потребляется через `Verify`) |
| Constant-time: только через `keystore.Verify` | `auth.go`: нет `==`/`bytes.Equal` по секретам |
| Ключ не в argv/env | `serve.go`: ключ только из `Authorization: Bearer` |
| Права TLS-ключа 0600 | `tls.go`: `writePEM(keyPath, ..., 0o600)`, атомарная запись |
| Права TLS-серта 0644 | `tls.go`: `writePEM(certPath, ..., 0o644)` |
| Аудит timestamp+fingerprint+remote+result | `audit.go`: `AuditRecord`, `writeAudit` |
| Нет секретов в логах | `auth.go`: только `keystore.Fingerprint(token)` в log, не `token` |
| Rate limiting per-key/per-IP | `ratelimit.go`: `Limiters.Allow(fp, ip)` |
| Graceful shutdown | `server.go`: `Run` → ctx cancel → `Shutdown` → `FlushUsage` |
| Таймауты (Slowloris) | `server.go`: `ReadTimeout`, `ReadHeaderTimeout`, `WriteTimeout`, `IdleTimeout` |
| TLS 1.3 minimum | `server.go`: `buildTLSConfig` → `MinVersion: tls.VersionTLS13` |
| Нет CipherSuites в TLS 1.3 | `server.go`: `buildTLSConfig` не задаёт `CipherSuites` |
| Сборка/тесты только в Docker | Все тесты прогнаны в Docker, raxd на хосте не запускался |
