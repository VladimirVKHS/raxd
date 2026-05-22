# research-guardian — задача `distribution` (раунд 1)

**Verdict: needs-changes.** Guardian (read-only). Дата: 2026-05-22. Сохранено дирижёром (red line 1).

Проверены `specs/distribution/research.md` + ADR-001..005 (proposed) против контракта research-analyst.

## Подтверждено
- Все Q1-Q7 + Q8 hardening покрыты; сравнения трёхпозиционные, рекомендации обоснованы.
- ADR заполнены, статус proposed. Офлайн-констрейнт проработан глубоко; STACK↔реальность mismatch
  (goreleaser в STACK vs работающий Makefile) зафиксирован, не замолчан.
- Q3 (критичный) — конкретный воспроизводимый рецепт (python3 -m http.server + RAXD_BASE_URL override).
- Q7 — нетривиальная находка: curl не ставит com.apple.quarantine (Apple DTS Quinn) → идемпотентный xattr -d;
  непроверяемость в Docker честно отмечена. Русский, кода нет.

## Issues (needs-changes — строгость источников + 1 red-line)
**Issue 1 — сомнительный факт «goreleaser требует Go 1.26».** research.md Q1 (~57-61). Go 1.26 НЕ существует на
май 2026 (актуален 1.25). Цитата приписывает требование невышедшей версии. Fix: WebFetch актуальной
goreleaser install/oss, зафиксировать реальную требуемую версию Go дословно; если не подтверждается — в
открытые вопросы.
**Issue 2 — несуществующий путь URL.** research.md Q2 (~138): `goreleaser.com/customization/package/checksum/`
— нет такого раздела (правильно `/customization/checksum/`). Паттерн как `docs.arbitrary.ch` ранее. Fix:
корректный URL или переформулировать «формат не найден в доке на [дата]» → открытый вопрос.
**Issue 3 (red line) — ADR-001 «Решение» финализирует за architect.** decisions/ADR-001 (~11-15): «Для v1
использовать ручной скрипт» — декларатив, не рекомендация. Fix: переформулировать «Research рекомендует B…;
финал за architect», статус proposed сохранить (research.md формулирует корректно «склоняется к B» —
синхронизировать ADR).
**Issue 4 — shasum macOS цитирует Linux man7.** research.md Q8 (~393-395): факт «shasum -a 256 на macOS» с
URL man7 GNU coreutils (Linux). Fix: источник для macOS (man shasum macOS / Apple) или открытый вопрос.
**Issue 5 — Q6 brew неверный URL.** research.md Q6 (~313): факт «goreleaser генерирует brew/deb/rpm» со
ссылкой на install/oss (про установку самого goreleaser). Fix: релевантные разделы
`/customization/homebrew/`, `/customization/nfpm/` или проверить актуальные пути v2.

## Итог
needs-changes (Issue 1/2 — достоверность URL/фактов; Issue 3 — red line ADR; Issue 4/5 — нерелевантные
источники). Блокеров нет. После правок — инлайн-проверка дирижёра.
