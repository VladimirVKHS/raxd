# Страж `qa-guardian`

## Назначение
Read-only гейт качества для артефактов роли `qa` (`test-plan.md` + тесты). Проверяет полноту
матрицы `AC → тест`, отсутствие skip/отключений, покрытие security-кейсов и install-flow.

## Когда вызывается
Автоматически как гейт после `qa`, до перехода к reviewer. Явно: `@qa-guardian`.

## Вход → Выход
- Вход: `specs/<task-id>/test-plan.md` + тесты, `spec.md`, контракт `.claude/agents/qa/qa.md`,
  `SECURITY-BASELINE.ru.md`.
- Выход: отчёт (verdict `pass|needs-changes|blocked`), который оркестратор сохраняет в
  `specs/<task-id>/guardians/qa-guardian.md`.

## Tools (scope) и почему
`Read, Grep, Glob` — тир **Verifier**. Без `Write/Edit/Bash/Skill`: независимость проверки
гарантирована на уровне доступа, а не обещания.

## Красные линии
Ничего не правит и не дописывает тесты; `pass` только при полной матрице AC, покрытых security/install
кейсах и отсутствии skip; без вкусовщины.
