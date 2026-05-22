# research-guardian — задача `service-install` (раунд 1)

**Verdict: needs-changes.** Guardian (read-only). Дата: 2026-05-22. Сохранено дирижёром (red line 1).

Проверены `specs/service-install/research.md` + ADR-001..004 (proposed) против контракта research-analyst.

## Подтверждено (pass-пункты)
- Покрыты все Q0-Q9 (включая Q9 systemd-в-Docker по AC16): варианты A/B/C, плюсы/минусы, обоснованная
  рекомендация по каждому. Сравнение реальное, не однобокое.
- ADR-001..004 заполнены полностью, статус proposed (финал делегирован architect). Рекомендации поданы
  как рекомендации.
- **Вендоринг-конфликт STACK↔go.mod зафиксирован честно** (kardianos/service в STACK, нет в go.mod/vendor;
  альтернативы с учётом стоимости вендоринга; рекомендация — ручная генерация без новой зависимости).
- macOS-непроверяемость в Docker отмечена честно по каждому Q (вынесено в открытые вопросы, AC13).
- Язык русский, кода нет, reference не дублируется.

## Issues (needs-changes — строгость источников)

**Issue 1 — неофициальный/сомнительный источник.** research.md Q4 (~строки 295-299): факт «AmbientCapabilities
+ NoNewPrivileges совместимы» подкреплён `https://docs.arbitrary.ch/security/systemd.html` (сторонний/
подозрительный домен, не первоисточник) в связке с man7. Fix: убрать arbitrary.ch, дать цитату из
официального `man5/systemd.exec.5.html` (раздел AmbientCapabilities ↔ NoNewPrivileges) ИЛИ вынести в
открытые вопросы «требует проверки по офиц. докам».

**Issue 2 — псевдо-цитата с неточностью.** research.md Q7 (~строки 469-472): «Aforementioned four signals =
SIGHUP, SIGINT, SIGTERM, SIGPIPE» подан как дословная цитата man, но это интерпретация; SIGHUP как «clean
exit» для Restart=on-failure неточен (зависит от RestartPreventExitStatus/версии). Fix: дать дословную
цитату systemd.service(5) по «clean exit» в контексте Restart=on-failure ИЛИ переформулировать как
«SIGTERM и SIGINT считаются чистым выходом» с прямой цитатой, SIGHUP убрать/вынести.

**Issue 3 — факт без URL в аналитике.** research.md Q8 (~строки 526-529): «-race требует CGO» используется
как опора вывода без URL (в скобках признано «цитаты нет»). Факт верен, но контракт требует URL. Fix:
добавить офиц. URL (`https://go.dev/doc/articles/race_detector` или `https://pkg.go.dev/cmd/go`) ИЛИ
перенести тезис в открытые вопросы.

## Итог
needs-changes (3 дефекта строгости источников, все правятся точечно без переработки структуры).
Блокирующих (незаполненный ADR / непокрытый Q / accepted-ADR / замолчанный вендоринг) нет. После правок —
инлайн-проверка дирижёра, повторный полный гейт не требуется.
