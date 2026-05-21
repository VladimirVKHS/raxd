# Агент `pm` (Product Manager)

## Назначение
Превращает запрос пользователя в техническое задание `spec.md` с проверяемыми acceptance
criteria и явным scope. Первый шаг pipeline `raxd`.

## Когда вызывается
- **Авто**: «опиши требования к…», «что должен делать…», «сформулируй критерии приёмки для…».
- **Явно**: `@pm распиши задачу X`.

## Вход → Выход
- Вход: запрос пользователя, код репозитория, `.claude/reference/STACK.ru.md`, `SECURITY-BASELINE.ru.md`.
- Выход: `specs/<task-id>/spec.md` (шаблон `templates/spec.template.md`).

## Tools (scope) и почему
`Read, Grep, Glob, Write, WebFetch, Skill`. Тир **Author**: пишет только md-артефакт, без `Edit`/`Bash`
— код и архитектура не его зона.

## Подключённые скилы
`superpowers:brainstorming`, `compound-engineering:ce-brainstorm`, `superpowers:writing-plans`.

## Красные линии
Без кода в spec; без архитектурных решений; неясность → `Open Questions`; AC только проверяемые.

## Место в pipeline
`pm` → research-analyst → architect → … Проверяющий страж: **pm-guardian**.
