# Guardian Report: research-guardian — mcp-server

**Дата:** 2026-05-21
**Артефакты:** `specs/mcp-server/research.md`, `decisions/ADR-001-mcp-sdk-vs-stdlib.md`,
`ADR-002-protocol-version-2025-11-25.md`, `ADR-003-origin-auth-on-mcp-endpoint.md`

## Раунд 1 — needs-changes

- Issue 1 (warning): нестабильный URL go.mod SDK на ветку `main` (невоспроизводимо).
- Issue 2 (warning): в ADR-001 «Решение» состав vendor подан как факт без оговорки (в OQ-1 она была).
- Issue 3 (minor): Go 1.25 представлен как «потенциальный блокер» (SDK требует ≥1.25, STACK упоминает «Go 1.22+»).

## Раунд 2 — pass

- URL go.mod/LICENSE переведены на тег `v1.6.0`; при сверке исправлена фактическая ошибка — лишняя
  `golang.org/x/time v0.15.0` (была с `main`) убрана; итог «7 прямых + 2 indirect» соответствует v1.6.0.
- В ADR-001 «Решение» добавлена оговорка: установленный факт (внешние импорты пакета `mcp` —
  jsonschema-go + uritemplate/v3) vs ожидаемое следствие (состав vendor/ подтверждается прогоном
  `go mod vendor` на хосте до commit, OQ-1).
- Go 1.25 — НЕ блокер: сверено `go.mod` (`go 1.25.0`) + `Dockerfile` (`golang:1.25`) удовлетворяют
  требованию SDK v1.6.0; рекомендация обновить «Go 1.22+» в `STACK.ru.md` → «Go 1.25» оформлена как
  рекомендация дирижёру (не блокер).

Рекомендации без изменений: Q1→официальный Go MCP SDK (офлайн-вендоринг реализуем), Q2→`/mcp`,
Q3→протокол `2025-11-25`, Q4→Resources/Prompts отложены, Q5→Origin/Bearer через готовый транспорт.
ADR в статусе proposed; контракт research соблюдён.

Issues: нет.

## Verdict (раунд 2)
pass
