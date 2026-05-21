# Test Plan: tls-transport — защищённый аутентифицированный сетевой транспорт raxd

Автор: qa (raxd). Вход: spec.md (AC1–AC14), security-requirements.md (SR-1–SR-26),
plan.md, impl-notes.md. Автор продукта: Vladimir Kovalev, OEM TECH.

---

## Стратегия

- **Unit (модульные)** — изолированная логика: `loadOrCreateCert`, `Limiters.Allow/GC`,
  `bearerToken`, `writeAudit`, `buildTLSConfig`. Без сети, без реального HTTP. Файл
  `internal/server/server_test.go` и `internal/server/server_qa_test.go`.
- **Integration (интеграционные)** — поднимается реальный TLS-listener через `server.New` +
  `srv.Run` в горутине; тесты делают HTTP/TLS-запросы. Используют `httptest`-паттерн с
  реальным TLS (не `httptest.NewTLSServer` — он не поддерживает MinVersion:TLS13). Это
  основной слой: покрывает middleware-цепочку, keystore.Verify, rate-limiter, аудит.
- **Static (статические grep-тесты)** — `security_static_test.go` и статические проверки
  в `server_qa_test.go`: проверяют отсутствие запрещённых паттернов (exec.Command,
  net.Listen вне server, CipherSuites в tls.Config, ключей в argv/env).
- **Race-тесты** — `CGO_ENABLED=1 go test -race ./internal/server/...`: покрывают SR-18
  (конкурентность rate-limiter), а также все остальные тесты в режиме детектора гонок.
- **Install-flow** — **вне scope задачи `tls-transport`**. Spec.md явно выносит install.sh
  и goreleaser в отдельную задачу `distribution` (раздел «Out of Scope»: install script,
  release pipeline). Решение дирижёра зафиксировано: install-flow тестируется в задаче
  `distribution`. В рамках `tls-transport` AC14 требует только Docker + vendor — проверяется
  командами `docker run --rm raxd-test`.

Все тесты прогоняются **только в Docker**, офлайн из `vendor/`. На хосте тесты не
запускаются (SECURITY-BASELINE §6).

---

## Команды запуска в Docker

```bash
# Сборка образа:
docker build --target test -t raxd-test .

# Полный прогон (vet + все тесты + race для server и keystore):
docker run --rm raxd-test

# Только vet:
docker run --rm raxd-test sh -c "go vet -mod=vendor ./..."

# Только server-тесты (все):
docker run --rm raxd-test sh -c "go test -mod=vendor -v -count=1 ./internal/server/..."

# Server-тесты с race-детектором:
docker run --rm raxd-test sh -c "CGO_ENABLED=1 go test -mod=vendor -race -v -count=1 ./internal/server/..."

# Все пакеты:
docker run --rm raxd-test sh -c "go test -mod=vendor -count=1 ./..."
```

---

## Матрица AC → тест

Каждый acceptance criterion из spec.md имеет минимум один тест-кейс.

| AC | Требование (кратко) | Уровень | Тест (файл :: функция) | Статус |
|---|---|---|---|---|
| AC1 | TLS 1.3 min, handshake TLS 1.2 отвергается | integration | `server_test.go::TestTLS13Enforced` | PASS |
| AC1 | TLS downgrade через raw tls.Dial | integration | `server_qa_test.go::TestTLS13EnforcedRawDial` | PASS |
| AC2 | Генерация self-signed ECDSA P-256 + SAN | integration | `server_test.go::TestCertIsECDSAP256SelfSignedWithSAN` | PASS |
| AC2 | Права cert.pem=0644, key.pem=0600 | unit | `server_test.go::TestCertGeneratedWithCorrectPerms` | PASS |
| AC2 | Создание в пустом TLSDir | unit | `server_qa_test.go::TestEmptyTLSDirCreatesNewCert` | PASS |
| AC3 | Переиспользование cert.pem (mtime + bytes) | unit | `server_test.go::TestCertReusedOnSecondNew` | PASS |
| AC3 | Переиспользование key.pem (mtime + bytes) | unit | `server_qa_test.go::TestKeyPemNotRewrittenOnSecondNew` | PASS |
| AC4 | Auth до обработки, нет ключа → 401 | integration | `server_test.go::TestNoAuthReturns401` | PASS |
| AC4 | Auth до routing: /healthz без ключа → 401 не 404 | integration | `server_qa_test.go::TestUnauthHealthReturns401NotFound` | PASS |
| AC4 | Malformed Authorization (8 случаев) → 401 | integration | `server_qa_test.go::TestMalformedAuthorizationFormats` | PASS |
| AC4 | Валидный ключ достигает health | integration | `server_test.go::TestValidKeyReachesHealth` | PASS |
| AC5 | Неизвестный ключ → 401 | integration | `server_test.go::TestUnknownKeyReturns401` | PASS |
| AC5 | Отозванный ключ → 401 немедленно | integration | `server_test.go::TestRevokedKeyReturns401` | PASS |
| AC5 | Отозванный ключ (ещё раз через anti-enum) | integration | `server_qa_test.go::TestAuthFailBodyNoEnumeration` | PASS |
| AC6 | Rate-limit per-key → 429 | integration | `server_test.go::TestRateLimitPerKeyReturns429` | PASS |
| AC6 | Rate-limit per-IP → 429 | integration | `server_test.go::TestRateLimitPerIPReturns429` | PASS |
| AC6 | Rate-limit refill после паузы | integration | `server_qa_test.go::TestRateLimitRefillAfterPause` | PASS |
| AC6 | Per-key бюджеты независимы | integration | `server_qa_test.go::TestRateLimitPerKeyBudgetsAreIndependent` | PASS |
| AC6 | Per-key и per-IP оба срабатывают | integration | `server_qa_test.go::TestRateLimitPerIPAndKeyBothApply` | PASS |
| AC7 | Bind по умолчанию 127.0.0.1 | integration | `server_test.go::TestServerBindsLoopback` | PASS |
| AC8 | Аудит-поля fp/remote присутствуют (success) | integration | `server_test.go::TestAuditHasFingerprintField` | PASS |
| AC8 | Аудит FAIL логируется | integration | `server_test.go::TestAuditFailRecorded` | PASS |
| AC8 | Аудит RATE логируется | integration | `server_test.go::TestAuditRateRecorded` | PASS |
| AC8 | Аудит-поля на всех путях (AUTH/FAIL/DENY/RATE) | integration | `server_qa_test.go::TestAuditFieldsOnAllPaths` | PASS |
| AC8 | fp на success-пути не "-" | integration | `server_qa_test.go::TestAuthSuccessAuditFpNotDash` | PASS |
| AC8 | fp="-" при отсутствии ключа | integration | `server_test.go::TestAuditFailHasDash` | PASS |
| AC9 | Ключ не в логе (success-путь) | integration | `server_test.go::TestAuditHasNoKeyBody` | PASS |
| AC9 | Ключ не в логе на ВСЕХ путях (fail/rate) | integration | `server_qa_test.go::TestNoKeyBodyInLogOnFailPaths` | PASS |
| AC10 | /healthz → pong, 200 | integration | `server_test.go::TestHealthReturnsPoong` | PASS |
| AC10 | /healthz Content-Type: text/plain | integration | `server_qa_test.go::TestHealthContentType` | PASS |
| AC10 | Неизвестный маршрут → 501 | integration | `server_test.go::TestDispatchReturns501` | PASS |
| AC10 | Dispatch body "not implemented" | integration | `server_qa_test.go::TestDispatchBodyNotImplemented` | PASS |
| AC11 | serve — не заглушка (запускает реальный сервер) | e2e | `cli_test.go::TestServeIsNoLongerStub` | PASS |
| AC11 | serve выдаёт серверный вывод | e2e | `cli_gaps_test.go::TestServeStartsRealServer` | PASS |
| AC12 | Graceful shutdown в пределах 10s | integration | `server_test.go::TestGracefulShutdown` | PASS |
| AC12 | Shutdown → FlushUsage (порядок через seam) | integration | `server_test.go::TestGracefulShutdownOrder` | PASS |
| AC12 | Graceful shutdown в пределах 5s (deadline) | integration | `server_qa_test.go::TestGracefulShutdownWithinDeadline` | PASS |
| AC13 | Повреждённый cert → ErrTLSCert, no overwrite | unit | `server_test.go::TestCorruptCertReturnsError` | PASS |
| AC13 | Частичное состояние cert (только cert / только key) | unit | `server_qa_test.go::TestPartialCertStateReturnsError` | PASS |
| AC13 | Занятый порт → ErrPortInUse | integration | `server_test.go::TestPortInUse` | PASS |
| AC13 | ErrCorrupt из keystore → 403 + DENY-аудит | integration | `server_test.go::TestErrCorruptReturns403` | PASS |
| AC14 | Все тесты зелёные в Docker, vendor, vet | CI | `docker run --rm raxd-test` | PASS |

---

## Матрица SR → тест

Ключевые security requirements с непосредственными тест-кейсами.

| SR | Требование | Тест | Статус |
|---|---|---|---|
| SR-1 | MinVersion TLS 1.3 | `TestTLS13Enforced`, `TestTLS13EnforcedRawDial` | PASS |
| SR-2 | CipherSuites не задаются | `TestStaticNoCipherSuites` (grep server.go) | PASS |
| SR-3 | ECDSA P-256, SAN 127.0.0.1+localhost | `TestCertIsECDSAP256SelfSignedWithSAN`, `TestSANLocalhostConnection` | PASS |
| SR-4 | key.pem=0600, cert.pem=0644 | `TestCertGeneratedWithCorrectPerms` | PASS |
| SR-5 | Переиспользование, нет перегенерации | `TestCertReusedOnSecondNew`, `TestKeyPemNotRewrittenOnSecondNew` | PASS |
| SR-6 | Corrupt cert → ErrTLSCert, без перезаписи | `TestCorruptCertReturnsError`, `TestPartialCertStateReturnsError` | PASS |
| SR-7 | Bind 127.0.0.1 по умолчанию | `TestServerBindsLoopback` | PASS |
| SR-8 | Auth ДО маршрутизации | `TestNoAuthReturns401`, `TestUnauthHealthReturns401NotFound` | PASS |
| SR-9 | Bearer-only, Verify через keystore | `TestUnknownKeyReturns401`, `TestMalformedAuthorizationFormats` | PASS |
| SR-10 | Нет прямого сравнения секретов | `TestStaticNoDirectKeyComparison` (grep auth.go) | PASS |
| SR-11 | Отзыв мгновенный | `TestRevokedKeyReturns401` | PASS |
| SR-12 | Ключ не в argv/env | `TestStaticServeNoKeyFromArgvOrEnv` (grep serve.go) | PASS |
| SR-13 | Anti-enumeration: тело 401/403 без причины | `TestAuthFailBodyNoEnumeration` | PASS |
| SR-14 | Host/Origin до auth в цепочке | `server_qa_test.go::TestHostDeniedBeforeAuth` | PASS |
| SR-14 | Host/Origin до auth: невалидный Host без auth → 403 не 401 | `server_qa_test.go::TestHostDeniedBeforeAuth` | PASS |
| SR-15 | Недопустимый Host → 403 + DENY-аудит | `TestInvalidHostReturns403` | PASS |
| SR-16 | Недопустимый Origin → 403; нет Origin → пропуск | `TestInvalidOriginReturns403`, `TestAbsentOriginNotRejected` | PASS |
| SR-17 | Rate-limit per-key и per-IP, 429 | `TestRateLimitPerKeyReturns429`, `TestRateLimitPerIPReturns429`, `TestRateLimitRefillAfterPause` | PASS |
| SR-18 | Mutex, нет гонок, TTL-GC | `TestRateLimiterConcurrency` (-race), `TestRateLimiterGCRemovesIdleEntries` | PASS |
| SR-19 | Аудит на каждом пути: fp/remote/result | `TestAuditHasFingerprintField`, `TestAuditFieldsOnAllPaths` | PASS |
| SR-20 | FAIL/RATE обязательно логируются | `TestAuditFailRecorded`, `TestAuditRateRecorded` | PASS |
| SR-21 | Нет секретов в логах (ВСЕ пути) | `TestAuditHasNoKeyBody`, `TestNoKeyBodyInLogOnFailPaths` | PASS |
| SR-22 | /healthz только после auth → pong | `TestValidKeyReachesHealth`, `TestHealthReturnsPoong` | PASS |
| SR-23 | Dispatch 501 без побочных эффектов | `TestDispatchReturns501`, `TestDispatchBodyNotImplemented` | PASS |
| SR-24 | Shutdown → FlushUsage, дедлайн | `TestGracefulShutdown`, `TestGracefulShutdownOrder`, `TestGracefulShutdownWithinDeadline` | PASS |
| SR-25 | Таймауты заданы (Slowloris) | инспекция `server.go::New` (ReadTimeout и др. заданы) | PASS |
| SR-26 | Docker, vendor, offline | `docker run --rm raxd-test` весь прогон | PASS |

---

## Edge cases безопасности (покрыты QA-тестами)

### TLS

- **Downgrade через HTTP-клиент** (`TestTLS13Enforced`): клиент с `MaxVersion: TLS12` не подключается.
- **Downgrade через raw tls.Dial** (`TestTLS13EnforcedRawDial`): прямой TCP-handshake TLS 1.2 → ошибка.
- **SAN localhost** (`TestSANLocalhostConnection`): cert содержит `localhost` в DNSNames; сертификат прошёл парсинг.
- **CipherSuites не задаются** (`TestStaticNoCipherSuites`): grep server.go.

### Сертификат

- **Частичное состояние**: только cert.pem или только key.pem → ErrTLSCert, нет паники.
- **Пустой TLSDir**: первый запуск генерирует обе файлы с правильными правами.
- **Повторный New**: cert.pem и key.pem — mtime и содержимое неизменны.
- **Corrupt cert**: файл не перезаписывается, возвращается ErrTLSCert.

### Аутентификация

- **8 форматов malformed Authorization** (lowercase bearer, нет пробела, пустой после пробела,
  только пробелы, Token scheme, Basic scheme, только слово Bearer, лишние пробелы перед Bearer) → 401.
- **Anti-enumeration**: тела ответов 401/403/429 не содержат "unknown key", "revoked", "corrupt",
  "not found", "ErrCorrupt", "authentication failed", "key store unavailable", тела ключа, ID ключа.
- **Constant-time**: grep auth.go — нет `bytes.Equal(token`, `strings.EqualFold(token/key`.
- **Ключ не в argv/env**: grep serve.go — нет чтения API-ключа из env/флагов.

### Аудит / секреты

- **Ключ не в логе на success-пути**: `TestAuditHasNoKeyBody`.
- **Ключ не в логе на fail-пути** (неизвестный, отозванный), **rate-пути**: `TestNoKeyBodyInLogOnFailPaths`.
- **fp не "-" на success**: `TestAuthSuccessAuditFpNotDash`.
- **fp="-" при нет ключа**: `TestAuditFailHasDash`.
- **Все аудит-поля присутствуют** (fp, remote) на AUTH/FAIL/DENY/RATE: `TestAuditFieldsOnAllPaths`.

### Rate-limit

- **Refill после паузы**: после исчерпания burst ждём 700ms (rate=2 req/s) → снова 200.
- **Изоляция per-key**: бюджеты ключей независимы, не делятся.
- **Per-key и per-IP оба срабатывают**: `TestRateLimitPerIPAndKeyBothApply`.
- **TTL-GC удаляет idle**: `TestRateLimiterGCRemovesIdleEntries` — после TTL+2xGC Allow снова
  создаёт запись без паники.
- **Concurrency/race**: `TestRateLimiterConcurrency` с 20 горутинами под `-race`.

### Устойчивость

- **Graceful shutdown** завершается в пределах 5s дедлайна.
- **Порядок Shutdown → FlushUsage** через test seam `SetAfterShutdownHook`.
- **Занятый порт** → ErrPortInUse, понятное сообщение.

---

## Найденные пробелы (закрыты QA-тестами)

| Пробел | Тест (добавлен) |
|---|---|
| Malformed Authorization (8 форматов) не тестировались | `TestMalformedAuthorizationFormats` |
| Anti-enumeration тел ответов 401/403 не проверялась | `TestAuthFailBodyNoEnumeration` |
| Ключ не проверялся в логе на fail/rate-путях | `TestNoKeyBodyInLogOnFailPaths` |
| TLS downgrade только через HTTP-клиент, не raw TLS | `TestTLS13EnforcedRawDial` |
| SAN localhost только в парсинге серта, не в TLS-dial | `TestSANLocalhostConnection` |
| key.pem mtime/bytes не проверялся при повторном New | `TestKeyPemNotRewrittenOnSecondNew` |
| Частичное cert-состояние (cert-only / key-only) | `TestPartialCertStateReturnsError` |
| Rate-limit refill не проверялся | `TestRateLimitRefillAfterPause` |
| Изоляция per-key бюджетов не проверялась | `TestRateLimitPerKeyBudgetsAreIndependent` |
| TTL-GC не проверялся | `TestRateLimiterGCRemovesIdleEntries` |
| Content-Type /healthz не проверялся | `TestHealthContentType` |
| Auth-before-routing: /healthz без ключа → 401 не 404 | `TestUnauthHealthReturns401NotFound` |
| fp не "-" на success не проверялось | `TestAuthSuccessAuditFpNotDash` |
| Аудит-поля на всех путях не проверялись одновременно | `TestAuditFieldsOnAllPaths` |
| Dispatch body "not implemented" не проверялся | `TestDispatchBodyNotImplemented` |
| Deadline shutdown не проверялся явно | `TestGracefulShutdownWithinDeadline` |
| Empty TLSDir не проверялся явно | `TestEmptyTLSDirCreatesNewCert` |
| Static: CipherSuites в server.go не проверялись grep | `TestStaticNoCipherSuites` |
| Static: прямое сравнение секретов в auth.go | `TestStaticNoDirectKeyComparison` |
| Static: ключ в argv/env в serve.go | `TestStaticServeNoKeyFromArgvOrEnv` |

---

## Найденные баги в продуктовом коде

Баги в продуктовом коде не обнаружены. Все тесты зелёные в Docker с `-mod=vendor`,
`go vet` чист, `-race` чист.

> **Примечание по `TestRateLimitIsolationBetweenKeys`** (первоначальная версия QA-теста):
> Тест предполагал, что ключ B на том же IP не получит 429 после исчерпания бюджета ключа A.
> Это неверная семантика: per-IP лимитер срабатывает независимо от ключа. Поведение корректно
> по spec (SR-17: per-key И per-IP). Тест исправлен — переписан как `TestRateLimitPerKeyBudgetsAreIndependent`.

> **Правки по qa-guardian (needs-changes, раунд 2):**
> Issue 1: `t.Skip` в `TestRateLimitRefillAfterPause` удалён → заменён на `t.Fatal` с диагностикой.
> Issue 2: `TestRateLimitPerKeyBudgetsAreIndependent` переписан как unit-тест через `server.NewLimiters`
> с разными IP для A/B; добавлены строгие `t.Fatal` при нарушении инварианта изоляции.
> Issue 3: добавлен `TestHostDeniedBeforeAuth` (SR-14): невалидный Host/Origin без auth → 403, не 401;
> матрица SR-14 обновлена с «инспекция порядка» на реальный тест.
> Issue 4: RATE-ветка в `TestAuditFieldsOnAllPaths` усилена: строгий `t.Fatalf` при отсутствии 429
> и отсутствии RATE в логе.
> Issue 5: добавлена явная ссылка на Out of Scope spec.md и решение дирижёра об install-flow.
> Issue 6: мягкий `t.Logf`+`return` в `TestSANLocalhostConnection` заменён на `t.Fatalf`.

---

## Результаты Docker (хвост прогона)

```
go vet -mod=vendor ./...
# (нет вывода — нет ошибок)

go test -mod=vendor -count=1 ./...
ok  github.com/vladimirvkhs/raxd                     0.006s
ok  github.com/vladimirvkhs/raxd/internal/banner     0.001s
ok  github.com/vladimirvkhs/raxd/internal/cli        0.067s
ok  github.com/vladimirvkhs/raxd/internal/config     0.003s
ok  github.com/vladimirvkhs/raxd/internal/keystore   0.155s
ok  github.com/vladimirvkhs/raxd/internal/server     2.172s
ok  github.com/vladimirvkhs/raxd/internal/version    0.001s

CGO_ENABLED=1 go test -mod=vendor -race -count=1 ./internal/server/...
ok  github.com/vladimirvkhs/raxd/internal/server     3.991s
```

Все 57 тестов server-пакета прошли, включая race-детектор.
