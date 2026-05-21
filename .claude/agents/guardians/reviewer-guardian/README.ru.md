# Страж `reviewer-guardian`

## Назначение
Read-only гейт качества для артефакта роли `reviewer` (`review.md`). Проверяет, что ревью прошлось по
всем AC и контрактам, verdict честен, issues в формате Где/Почему/Что делать, нет блокировки на стиле.

## Когда вызывается
Автоматически как гейт после `reviewer`, до перехода к tech-writer / возврата к developer.
Явно: `@reviewer-guardian`.

## Вход → Выход
- Вход: `specs/<task-id>/review.md`, `spec.md`, `plan.md`, контракт `.claude/agents/reviewer/reviewer.md`.
- Выход: отчёт (verdict `pass|needs-changes|blocked`), который оркестратор сохраняет в
  `specs/<task-id>/guardians/reviewer-guardian.md`.

## Tools (scope) и почему
`Read, Grep, Glob` — тир **Verifier**. Без `Write/Edit/Bash/Skill`: независимость проверки
гарантирована на уровне доступа, а не обещания.

## Красные линии
Ничего не правит и не переписывает ревью; `pass` только при полном обходе AC/контрактов и честном
verdict; без вкусовщины (в т.ч. не придирается к стилю самого отчёта).
