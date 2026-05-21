# Guardian Report: developer-guardian — tls-transport

**Дата:** 2026-05-21
**Ветка:** `feature/tls-transport`
**Артефакты:** `internal/server/*`, `internal/cli/serve.go`, `internal/config/config.go`,
`security_static_test.go`, `go.mod`/`go.sum`/`vendor/`; сверка с plan.md, security-requirements.md
(26 SR), spec.md (14 AC), ux-spec.md.

## Раунд 1 — needs-changes

- ISSUE-1 (HIGH): `hostOriginMiddleware` не логировал 403 Host/Origin (no-op `auditPlaceholder`) —
  нарушение SR-19/SR-20 + ux-spec §3.5/§3.6.
- ISSUE-2 (MEDIUM): отсутствовал тест SR-13 (ErrCorrupt→403); impl-notes врал о покрытии
  (`TestCorruptCertReturnsError` — про серт, не keys.db).
- ISSUE-3 (MEDIUM): `TestGracefulShutdown` не проверял порядок Shutdown→FlushUsage (SR-24).
- ISSUE-4 (LOW): ложный `_ = handler` + комментарий (server.go:90).
- ISSUE-5 (LOW): самодельные stringContains/serveContains вместо strings.Contains.

Отклонения подтверждены: (а) сужение security_static_test до non-server — легитимно; (в) обновление
cli-тестов — легитимно; (б) аудит «каждый middleware пишет» — оказался дырой для hostOrigin (ISSUE-1).
Блокеров нет.

## Раунд 2 — pass

Все 5 закрыты фактически (коммиты 410802e, 80a80c3, a0f905a, c030618):
- ISSUE-1: `hostOriginMiddleware(hostAllow, originAllow, auditFn)`; `auditPlaceholder` удалён; на оба
  403 пишется `WARN DENY fp=- remote=... reason="invalid host/origin header"`; reason не в теле ответа;
  тесты `TestInvalidHostReturns403`/`TestInvalidOriginReturns403` ассертят `DENY`+reason в логе.
- ISSUE-2: `TestErrCorruptReturns403` (повреждение keys.db в рантайме → 403 + DENY, без паники);
  impl-notes честен (TestCorruptCertReturnsError → SR-6).
- ISSUE-3: `afterShutdownHook`/`SetAfterShutdownHook` между Shutdown и FlushUsage; `TestGracefulShutdownOrder`.
- ISSUE-4: ложная строка/комментарий удалены.
- ISSUE-5: `strings.Contains` из stdlib, дубли удалены.

Регрессий нет: маппинг 401/403/429/501, auth до обработки (recover→hostOrigin→auth→ratelimit→mux),
TLS1.3 MinVersion, серт 0600 ECDSA P-256, rate-limit per-key/per-IP + TTL-GC, нет секретов в логах
(только Fingerprint), scope не расширен (dispatch 501), static-проверки не ослаблены, vendor синхронен,
нет `t.Skip`. Docker: vet чисто, test PASS, -race PASS.

## Verdict (раунд 2)
pass

---

## Раунд 3 — фиксы reviewer (SR-16/SR-25/SR-19), 2026-05-21

Коммиты f7cd7cc, a7d657d, f9483f3, 9a229a2. Проверено по коду:
- ISSUE-1 (SR-16): `originAllowed` (middleware.go:158-169) — `url.Parse`→`u.Hostname()`→`contains`
  (точное case-insensitive); обход поддоменом/непарсящийся → 403; отсутствие Origin → пропуск.
  Тесты `TestOriginBypassAttemptRejected`/`TestInvalidOriginUnparseable`/`TestAbsentOriginNotRejected` реальны.
- ISSUE-2 (SR-25): `Config.MaxBodyBytes` (config.go, дефолт 1 MiB) + `bodyLimitMiddleware`
  (`http.MaxBytesReader`, Layer 1 outermost); `TestMaxBodyBytesRejected` (413) реально шлёт тело сверх лимита.
- ISSUE-3 (SR-19): AUTH убран из authMiddleware; `authSuccessAuditMiddleware` (Layer 6, после rate-limit)
  → ровно одна запись на запрос; на 429 валидного ключа AUTH не пишется. `TestSingleAuditRecordOnSuccess`/
  `TestSingleAuditRecordOnRateLimit` проверяют счётчики.

Регрессий нет (auth-before-handling, TLS1.3, rate-limit, нет секретов, graceful shutdown — на месте);
нет `t.Skip`/имитаций; vendor без новых внешних зависимостей (net/url stdlib); impl-notes/test-plan честны.
Инфо-замечание (не блокер): `TestMaxBodyBytesDefault` проверяет хелпер, а не `config.Load()`.

## Verdict (раунд 3)
pass
