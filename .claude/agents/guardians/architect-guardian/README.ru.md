# Страж `architect-guardian`

## Назначение
Read-only гейт качества для артефакта роли `architect` (`plan.md`). Проверяет, что выбран ровно
один подход, модули с конкретными путями, контракты с типами и обработкой ошибок, нет тел функций,
AC из spec не изменены, новые зависимости обоснованы и сверены со STACK.

## Когда вызывается
Автоматически как гейт после `architect`, до перехода к security/cli-ux/mcp-engineer/system-dev/developer.
Явно: `@architect-guardian`.

## Вход → Выход
- Вход: `specs/<task-id>/plan.md`, `specs/<task-id>/spec.md`, контракт
  `.claude/agents/architect/architect.md`, `STACK.ru.md`.
- Выход: отчёт (verdict `pass|needs-changes|blocked`), который оркестратор сохраняет в
  `specs/<task-id>/guardians/architect-guardian.md`.

## Tools (scope) и почему
`Read, Grep, Glob` — тир **Verifier**. Без `Write/Edit/Bash/Skill`: независимость проверки
гарантирована на уровне доступа, а не обещания.

## Красные линии
Ничего не правит; `pass` только при одном подходе, без тел функций и при неизменных AC; без вкусовщины.
