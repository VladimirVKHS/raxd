# review.md — TLS Transport (`feature/tls-transport`)

**Reviewer:** reviewer (raxd). **Дата:** 2026-05-21. **Язык:** русский.
**Вход:** spec.md (AC1–AC14), plan.md, security-requirements.md (SR-1–SR-26), threat-model.md,
ux-spec.md, impl-notes.md, test-plan.md, SECURITY-BASELINE §1/§2/§4/§6.
**Объём:** `internal/server/{server,tls,auth,middleware,ratelimit,audit,handlers}.go`,
`internal/cli/serve.go`, `internal/config/config.go`, тесты `server_test.go`+`server_qa_test.go`+
`security_static_test.go`, `Dockerfile`, `go.mod`/`go.sum`, `vendor/`.

## Verdict (раунд 1): needs-changes

AC1–AC14 в основном выполнены и подтверждены кодом. Блокируют accept два незакрытых пункта
security-requirements (реальные дефекты безопасности): SR-16 (Origin) и SR-25 (лимит тела).

## Сводка AC (подтверждено кодом)

AC1 TLS1.3 (MinVersion, без CipherSuites) · AC2 ECDSA P-256/SAN/ключ 0600 атомарно · AC3 reuse без
перегенерации · AC4 auth ДО mux, Bearer-only · AC5 401/403 + revoked исключены · AC6 rate-limit
per-key/per-IP 429 · AC7 bind 127.0.0.1 · AC8 аудит-поля · AC9 нет секретов (только Fingerprint) ·
AC10 health pong / 501 · AC11 serve foreground (signal.NotifyContext) · AC12 graceful shutdown
Shutdown→FlushUsage · AC13 ErrTLSCert/ErrPortInUse/ErrCorrupt→403/recover · AC14 Docker/vendor.
Маппинг 401/403/429/501 непротиворечив; анти-enumeration (пустое тело, причина только в аудит).

## Issues

### ISSUE-1 (medium, needs-changes) — Origin обходится подстановкой поддомена (нарушение SR-16)
`internal/server/middleware.go` `originAllowed` использует `strings.HasPrefix(o, "https://"+a)`. Для
`a="localhost"` запрос `Origin: https://localhost.evil.com` → HasPrefix true → пропуск вместо 403
(аналогично `https://127.0.0.1.attacker.com`). Нарушает SR-16 «Origin present И вне allowlist → 403»
и ослабляет защиту от DNS-rebinding (R2). Severity medium (bind 127.0.0.1 + ключ — основной гейт),
но дефект реальный и наследуется mcp-server.
**Фикс:** парсить Origin (`url.Parse`), брать `u.Hostname()`, сверять точным совпадением с allowlist
(как уже сделано для Host). Добавить тест на обход `https://localhost.evil.com`/`https://127.0.0.1.evil.com` → 403.

### ISSUE-2 (medium, needs-changes) — лимит тела запроса не реализован (SR-25 частично)
В `internal/server` нет `http.MaxBytesReader` (grep 0). Задан только `MaxHeaderBytes`; ограничение
ТЕЛА отсутствует. SR-25 дословно требует «`MaxBytesReader`/`MaxHeaderBytes`»; R9 (DoS большим телом)
закрывается этим. test-plan помечает SR-25 PASS при частичной реализации.
**Фикс:** обернуть `r.Body` в `http.MaxBytesReader(w, r.Body, limit)` (middleware), добавить
`Config.MaxBodyBytes` с дефолтом + тест на отклонение тела сверх лимита. Либо явно зафиксировать
отложение SR-25 в impl-notes/threat-model как принятое отклонение с эскалацией — но не оставлять
«PASS» при частичной реализации.

### ISSUE-3 (low, рекомендуется) — двойная аудит-запись при rate-limit валидного ключа
`authMiddleware` пишет `AUTH success` ДО `next`, а `rateLimitMiddleware` за ним → при 429 валидного
ключа в лог попадают AUTH и RATE. ux-spec §3.7 показывает rate-limited как одну строку RATE; «успех»
для отвергнутого 429-запроса искажает аудит. SR-19/20 формально выполнены (не блокер).
**Фикс (если делать):** писать AUTH success после прохождения rate-limit (fp через контекст) либо
помечать success при фактическом достижении обработчика; иначе — зафиксировать как осознанное в impl-notes.

### ISSUE-4 (info) — plan↔код отклонение по аудит-паттерну
Реализация пишет аудит из каждого решающего middleware (а не outermost deferred из plan) и расширяет
сигнатуру `hostOriginMiddleware` параметром auditFn. Функционально эквивалентно SR-19/20, честно
описано в impl-notes (Отклонения 3,4) как устранение developer-guardian ISSUE-1. Замечаний нет.

## Что проверено и претензий НЕТ
TLS (SR-1..3), права 0600/0644 атомарно (SR-4), reuse/битый серт→ErrTLSCert без перезаписи (SR-5/6),
auth Bearer-only через keystore.Verify constant-time, revoked исключены, ключ не из argv/env (SR-8..13),
Host тайминг ДО auth + точное сравнение (SR-14, TestHostDeniedBeforeAuth), rate-limit per-key/per-IP +
TTL-GC, `-race` чист (SR-17/18), аудит-поля на всех путях, ключ-подстрока отсутствует в логе (SR-19..21),
порядок Shutdown→FlushUsage + recover (SR-24), scope (только health+501, нет exec/mcp/upload), vendor
синхронен, Dockerfile offline -mod=vendor, нет t.Skip, impl-notes честен, UX serve по ux-spec.

## Что нужно для accept
Закрыть ISSUE-1 (строгое сравнение Origin по host + тест на обход) и ISSUE-2 (MaxBytesReader ИЛИ
явное отложение SR-25 с эскалацией). ISSUE-3 — фикс или явная фиксация. После правок — повторный
прогон в Docker (`-race`), обновление матрицы SR в test-plan под новые тесты, re-review.

---

## Verdict (раунд 2): accept

Все три ISSUE закрыты по существу (коммиты f7cd7cc, a7d657d, f9483f3, 9a229a2):
- **ISSUE-1 (SR-16):** `originAllowed` → `url.Parse`+`u.Hostname()`+точное case-insensitive сравнение;
  subdomain-bypass невозможен (`https://localhost.evil.com` → 403); непарсящийся → 403; отсутствие
  Origin пропускается (ADR-002). Тесты `TestOriginBypassAttemptRejected`, `TestInvalidOriginUnparseable`,
  `TestAbsentOriginNotRejected` — реальные (бьют по TLS-серверу).
- **ISSUE-2 (SR-25):** `Config.MaxBodyBytes` (1 MiB дефолт) + `bodyLimitMiddleware`
  (`http.MaxBytesReader`, outermost Layer 1); `TestMaxBodyBytesRejected` (413 через `errors.As`
  `*http.MaxBytesError`). SR-25 полон (тело+заголовки+таймауты+recover).
- **ISSUE-3 (SR-19):** AUTH убран из authMiddleware, добавлен `authSuccessAuditMiddleware` (innermost,
  за rate-limit) → ровно одна запись на запрос; на 429 валидного ключа AUTH не пишется.
  `TestSingleAuditRecordOnSuccess`, `TestSingleAuditRecordOnRateLimit`.

Регрессий нет: цепочка `bodyLimit → recover → Host/Origin → auth → rate-limit → authSuccessAudit → mux`
сохраняет auth-before-handling (SR-8/SR-14) и аудит отказов (SR-19/20); body-limit как outermost не
мешает auth; success-аудит недостижим на путях отказа. Docker: vet OK, test PASS, -race PASS.

Минор (не блокер): `TestMaxBodyBytesDefault` проверяет `newTestConfig`, а не `config.Load()` дефолт
(сам дефолт задан в config.go); ADR-002 формально `proposed`. На verdict не влияют.

Хендофф: **tech-writer**.
