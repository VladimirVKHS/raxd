# Guardian Report: tech-writer-guardian — key-management

## Verdict: needs-changes

## Summary
Документация качественная: разделение stdout/stderr, тексты ошибок error:/hint: дословно по коду, exit-коды, одноразовый показ ключа и запрет показа в list/ошибках — всё корректно; «Coming next» честен; docs-outline фиксирует расхождения Q1-Q4; автор и Docker-only соблюдены. Но примеры содержат неточности форматов.

## Issues
- [ ] ISSUE-2 (БЛОКЕР, точность): в `README.md` примеры `key list`/`key delete` показывают id 9-10 символов, тогда как код усекает id до 12 (docs/commands.md тоже пишет 12). Привести примеры README к 12-символьным id, согласованным с таблицей.
- [ ] ISSUE-3 (БЛОКЕР, требует сверки): пример `key list` в `docs/commands.md` и `README.md` без строки-разделителя `──────────` под заголовком, тогда как ux-spec её предписывает, а tablewriter v1.x по умолчанию её рендерит. Проверить фактический вывод в Docker и привести примеры в соответствие (добавить разделитель, либо зафиксировать отклонение от ux-spec в docs-outline).
- [ ] ISSUE-1 (рекомендация, не жёсткий блокер): `docs/development.md` пишет, что lipgloss «not used», но он присутствует в go.mod как транзитивная зависимость (через charmbracelet/log). Уточнить: lipgloss — transitive, напрямую не импортируется; прямое использование — точка расширения. (После `go mod tidy` log/tablewriter станут direct, lipgloss останется indirect.)

## Looks good
- Разделение stdout/stderr точно; тексты ошибок дословно по коду (printError/printStoreError).
- Exit-коды согласованы (list пусто → 0; ошибки → 1).
- Одноразовый показ ключа; запрет в list/ошибках подчёркнут многократно.
- «Coming next» согласован с отсутствием кода (TLS, MCP, serve, curl|sh, config port).
- docs-outline честно фиксирует Q1 (хэш по полному ключу), Q2 (id full/12), Q3 (fingerprint в db), Q4 (FlushUsage не из CLI).
- TLS-каталог корректно оговорён («path reserved, files not created yet»).

## Verdict
needs-changes (БЛОКЕРЫ: ISSUE-2, ISSUE-3; рекомендация: ISSUE-1)

---

## Раунд 2 — после Q5-фикса (полный 16-hex id) + аудит-строки, 2026-05-21

Доки выровнены под реальный вывод (полный 16-hex id в `key list`, полная Unicode-рамка).

**Раунд 2a — needs-changes:**
- ISSUE-1 (средняя): `docs-outline.md` содержал устаревшее «ID усечён до 12», 12-hex пример-таблицу
  (`6f3ad36e9e0b`/`8a113aadbe33`), Q5 как pre-fix проблему.
- ISSUE-2 (низкая): `docs/commands.md` не упоминал реальную audit-строку charmbracelet/log на stderr
  в выводе create/delete.

**Раунд 2b — pass (после правок tech-writer):**
- `docs-outline.md`: правило колонок list → «полный 16-hex id без усечения»; пример-таблица на
  полный 16-hex (`d7bc3a34da19d94e`/`e4b550b565a232b6`); Q2 помечен решённым; Q5 переписан как
  «фикс применён, workaround не нужен». Остаточные «12» — только Fingerprint (12-hex sha256(body))
  и прошедшее время в описании фикса.
- `docs/commands.md`: в `key create`/`key delete` добавлены честные заметки про audit-строку
  (`INFO key created/revoked action=… id=… fingerprint=…`, только id+fingerprint, не тело ключа,
  формат нестабилен) — сверено с `internal/cli/key.go:158-163, 296-301`.
- Регрессий нет: `key list` в commands.md/README — полный 16-hex; нет «truncated to 12 / first 12»
  про id (единственное `truncat` — LABEL до 20); согласованность id-примеров внутри файлов;
  автор продукта на месте.

## Verdict (раунд 2)
pass
