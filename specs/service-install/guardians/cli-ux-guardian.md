# cli-ux-guardian — задача `service-install` (раунд 1)

**Verdict: needs-changes.** Guardian (read-only). Дата: 2026-05-22. Сохранено дирижёром (red line 1).

Проверен `specs/service-install/ux-spec.md` против контракта cli-ux, STACK-конвенций и SR-95.

## Подтверждено
- Покрыты 5 команд + ASCII-макеты; баннер автора Vladimir Kovalev OEM TECH; NO_COLOR/узкий терминал;
  стек lipgloss/log/tablewriter; SR-95 (секретов в выводе нет, П-7 явно); не-root в status (`user raxd [not root]`);
  ошибки `error:`/`hint:` строчными; русский артефакт; Go-кода нет.

## Issues (needs-changes)
**Issue 1 — пропущены 2 состояния `stop`.** В «Состояниях вывода» для stop только успех/«уже остановлен»; нет
ASCII-блоков «не установлен» (ErrNotInstalled) и «сбой остановки» (есть только в таблице ошибок). Для start
аналоги оформлены. Fix: добавить два блока для stop по образцу start (exit 1).

**Issue 2 — конвенция stdout/stderr для `service status` расходится с кодом.** ux-spec назначает человекочит.
status→stderr (по STACK), но существующий `internal/cli/status.go:29-43` пишет человекочит. в stdout. Риск
тихого расхождения spec↔код. Fix: явно разграничить — `raxd service status` (новая команда) и `raxd status`
(существующая). Рекомендация дирижёра: для query-команды status первичный отчёт (человекочит. И `--json`) →
stdout, согласованно с `raxd status`; баннер/инцидентные сообщения → stderr. Зафиксировать однозначно.

**Issue 3 — exit-код идемпотентного install не зафиксирован (противоречит plan).** plan §Contracts: Install →
ErrAlreadyInstalled (ошибка). ux-spec декларирует exit 0, но оговаривает «выбор за developer» — недопустимо
в контракте. Fix: зафиксировать однозначно: CLI-слой мапит sentinel ErrAlreadyInstalled (от менеджера) в
**exit 0** + дружелюбный текст без `error:`-префикса (AC9). Аналогично uninstall/ErrNotInstalled → exit 0.

**Issue 4 — ErrNotInstalled разный exit без пояснения.** uninstall→exit 0 (идемпотентность AC10),
start/stop→exit 1 (операция неприменима). Семантически верно, но не пояснено. Fix: одна фраза в таблице/
примечании.

## Итог
needs-changes (Issue 1/3 блокируют однозначную реализацию; Issue 2 риск расхождения; Issue 4 малый).
Возврат к cli-ux. Блокировок нет (Go-кода нет, SR-95 соблюдён).
