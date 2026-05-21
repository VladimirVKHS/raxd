# Guardian Report: tech-writer-guardian — tls-transport

**Дата:** 2026-05-21
**Артефакты:** `docs/commands.md`, `docs/configuration.md`, `docs/development.md`,
`docs/troubleshooting.md`, `README.md`, `specs/tls-transport/docs-outline.md` (сверка с
`internal/cli/serve.go`, `internal/server/*.go`, `internal/config/config.go`)

## Раунд 1 — needs-changes

- ISSUE-1 (MEDIUM): доки не отражали, что ошибки `config.Load` (невалидный bind_addr И невалидный
  YAML) идут одним блоком с одним общим hint про `bind_addr` → для YAML-ошибки hint вводит в
  заблуждение; доки показывали мнимый точный pair-matching.
- ISSUE-2 (LOW): `development.md` «prints the startup block» вместо «registers OnListen hook».
- ISSUE-3 (LOW): 413 от body-limit не аудируется — в доках не отмечено.

Подтверждено корректным: serve как working (TLS1.3/Bearer/cert 0600/health/501/маппинг кодов/каналы/
exit), все 12 config-полей точны, D-1 fix, D-2/D-3/D-4 честно задокументированы, автор на месте,
нет выдуманного (command-exec/MCP/installer/mTLS/system-service не как working).

## Раунд 2 — pass

После правок tech-writer:
- ISSUE-1: `commands.md` (секция «Configuration load failure») и `troubleshooting.md` честно описывают
  единый config-load hint; YAML-callout «hint mentions bind_addr, but real problem is YAML syntax».
- ISSUE-2: `development.md` — «registers an OnListen hook that prints the startup block only after a
  successful bind (via srv.SetOnListen, fired from inside srv.Run)».
- ISSUE-3: заметки про неаудируемый 413 в `commands.md` (пайплайн + под таблицей кодов + Audit stream)
  и `troubleshooting.md`.
- docs-outline синхронизирован (D-5 единый config-load hint, D-6 413 не аудируется, G1 закрыт).

Регрессий нет; факты соответствуют коду; автор на месте; ссылки живые; нет выдуманного.

## Verdict (раунд 2)
pass
