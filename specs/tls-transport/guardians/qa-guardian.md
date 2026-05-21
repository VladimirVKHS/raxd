# Guardian Report: qa-guardian — tls-transport

**Дата:** 2026-05-21
**Артефакты:** `specs/tls-transport/test-plan.md`, `internal/server/server_test.go`,
`internal/server/server_qa_test.go`, `internal/cli/*_test.go`, `security_static_test.go`

## Раунд 1 — needs-changes

- Issue 1 (CRITICAL): `t.Skip` в `TestRateLimitRefillAfterPause` — нарушение red line qa.
- Issue 2 (CRITICAL): `TestRateLimitPerKeyBudgetsAreIndependent` без `t.Error`/`t.Fatal` — всегда
  зелёный, изоляция per-key (AC6/SR-17) реально не покрыта.
- Issue 3 (HIGH): SR-14 (Host/Origin до auth) только «инспекция», без реального теста порядка.
- Issue 4 (MEDIUM): RATE-путь в `TestAuditFieldsOnAllPaths` — заглушка `t.Logf` («RATE-or-AUTH»),
  принимает любое состояние.
- Issue 5 (LOW): install-flow вне scope без ссылки на spec/distribution.
- Issue 6 (LOW): `TestSANLocalhostConnection` — мягкий `t.Logf`+return.

Матрица AC1-14 → тест плотная; security-слой (anti-enumeration на 4 кейсах, секрет-подстрока на всех
путях, raw TLS1.2 downgrade через tls.Dial) — реальный, не имитация.

## Раунд 2 — pass

Все 6 закрыты фактически (коммит 58a69b4), проверено по коду:
- Issue 1: `t.Skip` удалён → `t.Fatal` с диагностикой; refill реально проверяется (429 до паузы → 200 после).
- Issue 2: переписан через `server.NewLimiters` с разными IP (1.2.3.4/5.6.7.8), 4 строгих ассерта;
  изоляция ключа B при исчерпании A доказана; per-IP не маскирует.
- Issue 3: добавлен `TestHostDeniedBeforeAuth` (невалидный Host без Authorization → 403, не 401 —
  доказывает порядок middleware); матрица SR-14 обновлена.
- Issue 4: RATE-ветка строгая (`t.Fatalf` на отсутствие 429 и отсутствие `RATE` в логе).
- Issue 5: test-plan ссылается на Out of Scope spec.md + задачу distribution.
- Issue 6: `t.Fatalf` при ошибке TLS-хендшейка через localhost.
- `grep t.Skip` → 0; фальш-зелёных тестов нет; продуктовый код не тронут (только тесты+test-plan).
  Docker: vet чисто, test PASS, -race PASS. Багов продукта не выявлено.

## Verdict (раунд 2)
pass
