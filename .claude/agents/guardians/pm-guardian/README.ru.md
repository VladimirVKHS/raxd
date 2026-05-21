# Страж `pm-guardian`

## Назначение
Read-only гейт качества для артефакта роли `pm` (`spec.md`). Проверяет полноту секций,
проверяемость AC, отсутствие кода/архитектуры, учёт безопасности.

## Когда вызывается
Автоматически как гейт после `pm`, до перехода к research-analyst/architect. Явно: `@pm-guardian`.

## Вход → Выход
- Вход: `specs/<task-id>/spec.md`, контракт `.claude/agents/pm/pm.md`, `SECURITY-BASELINE.ru.md`.
- Выход: отчёт (verdict `pass|needs-changes|blocked`), который оркестратор сохраняет в
  `specs/<task-id>/guardians/pm-guardian.md`.

## Tools (scope) и почему
`Read, Grep, Glob` — тир **Verifier**. Без `Write/Edit/Bash/Skill`: независимость проверки
гарантирована на уровне доступа, а не обещания.

## Красные линии
Ничего не правит; `pass` только при закрытых обязательных пунктах; без вкусовщины.
