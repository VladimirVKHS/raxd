# Guardian Report: cli-ux-guardian — bootstrap-cli

## Summary

Артефакт `specs/bootstrap-cli/ux-spec.md` (469 строк) качественно выполнен: покрывает все обязательные состояния вывода с ASCII-макетами, содержит автора в баннере, обосновывает английский язык интерфейса и соответствует решениям plan.md. Найдены: одно противоречие с plan.md по exit-коду заглушки `key list`, неточность по псевдокоду в разделе доступности и несинхронизированный путь lipgloss. Блокеров нет, но до developer нужно устранить.

## Checklist
- [x] Состояния вывода (баннер/version/status/key list/error) — каждое с ASCII-макетом.
- [x] Баннер содержит «Vladimir Kovalev, OEM TECH».
- [x] Учтены NO_COLOR/--no-color и узкий терминал.
- [x] Опора на стек charmbracelet/*; посторонних библиотек нет.
- [x] Тексты ошибок понятны (что случилось + что делать).
- [x] Артефакт на русском; тексты CLI на английском с обоснованием.
- [ ] Заглушка `key list`: канал/exit противоречат между разделами — Issue 1.
- [ ] Псевдокод в «Доступность» читается как контракт реализации — Issue 2.
- [ ] Путь `charm.land/lipgloss/v2` не синхронизирован со STACK — Issue 3.

## Issues
- [ ] Issue 1: Противоречие exit-кода заглушки `key list`
  - Где: ux-spec.md строка ~195 («exit 0») vs строка ~413 («stderr, exit 1») и сводная таблица ~655.
  - Почему: plan.md (newStub → всегда ненулевой код); developer прочтёт конфликт.
  - Что делать: в строке ~195 исправить на exit 1 либо явно разграничить «bootstrap-заглушка (exit 1, error: key list: not implemented yet)» и «полный макет ниже — контракт для key-management».
- [ ] Issue 2: Псевдокод в разделе «Доступность» (NO_COLOR), строки ~609-615
  - Почему: `os.Getenv("NO_COLOR")`, `lipgloss.NewStyle()` в императиве читаются как обязательный контракт реализации (red line: только тексты/макеты).
  - Что делать: переписать declarative-описанием поведения (без Go-API): «при NO_COLOR/--no-color — никаких ANSI-кодов; Unicode box-drawing сохраняется; lipgloss-стили не применяются».
- [ ] Issue 3: Путь `charm.land/lipgloss/v2` vs STACK, строки ~590-593
  - Почему: STACK указывает `github.com/charmbracelet/lipgloss`; developer не поймёт, какому источнику верить.
  - Что делать: добавить сноску, что расхождение зафиксировано в plan.md Trade-offs как открытый вопрос STACK-owner; на bootstrap lipgloss не подключается (точка расширения).

## Looks good
- Баннер: три варианта (wide/narrow/без рамки) с порогами 52/42 кол., дефолты dev-сборки, канал stderr с обоснованием.
- Сводная таблица контрактов вывода (stdout/stderr/exit) — сильный верифицируемый контракт.
- Раздел «Что НЕ показывает status» совпадает с security-requirements — снимает двусмысленность.

## Verdict (раунд 1)
needs-changes

---

## Повторная проверка (раунд 2)

Все три пункта устранены корректно:
- Issue 1: `key list` заглушка → Канал stderr, Код возврата 1, `error: key list: not implemented yet`; табличный макет помечен как контракт будущей задачи key-management.
- Issue 2: blockquote NO_COLOR переписан declarative; Go-идиом (`os.Getenv`, `lipgloss.NewStyle`) в файле нет (подтверждено grep).
- Issue 3: путь lipgloss снабжён сноской на plan.md (Trade-offs) и STACK-owner; на bootstrap не подключается.

Новых проблем нет.

## Verdict (финал)
pass
