# Guardian Report: qa-guardian — mcp-server

**Дата:** 2026-05-21
**Артефакты:** `specs/mcp-server/test-plan.md`, `internal/mcp/mcp_test.go`, `internal/mcp/mcp_security_test.go`,
`internal/server/audit_mcp_test.go`, `internal/cli/cli_gaps_test.go`

## Раунд 1 — needs-changes

- Issue 1 (MEDIUM): SR-27 ветка `ErrCorrupt→403` (до MCP) не покрыта тестом.
- Issue 2 (MEDIUM): SR-35 аудит-тест проверял лишь наличие `fp=` (пропускал `fp=-`/пустое).
- Issue 3 (LOW): `t.Skipf` в `occupyFreePort` без явной оценки риска в test-plan.

Подтверждено хорошим: SR-34 усилен (читает реальное содержимое key.pem); устранены 2 наследованных
`t.Skip("port 7822…")` через свободный порт (детерминированно); ассерты содержательны; продуктовый код
не тронут.

## Раунд 2 — pass

- Issue 1: `TestMCPKeystoreCorruptReturns403` (mcp_security_test.go:627-771) — реально портит keys.db в
  рантайме → POST /mcp с Bearer → 403, нет `tool=` в логе (MCP не достигнут), liveness-check (нет паники);
  матрица SR-27 обновлена.
- Issue 2: helpers `assertMCPRealFingerprint`/`assertRealFingerprint` — проверяют fp != "-"/непустой,
  ≥1 hex; применены к интеграционным тестам с полной auth-цепочкой; матрица SR-35 обновлена.
- Issue 3: test-plan раздел «Состояние t.Skip» — статус «LOW-risk, принятый necessary-defensive»,
  природа гонки, триггер пересмотра (flaky в CI); не лазейка.

Регрессий нет: в `internal/mcp` t.Skip = 0; ассерты не ослаблены; продуктовый код не менялся. Docker:
vet ok; test PASS (mcp 5.461s, server 5.429s); -race PASS. Багов продукта не выявлено (ErrCorrupt→403
работает, fingerprint реальный).

## Verdict (раунд 2)
pass
