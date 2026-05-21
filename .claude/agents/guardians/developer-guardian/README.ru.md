# Страж `developer-guardian`

## Назначение
Read-only гейт качества для работы роли `developer` (код в feature-ветке + `impl-notes.md`).
Проверяет соответствие `plan.md`, отсутствие функционала вне scope, наличие зелёных тестов,
безопасность кода и git-flow.

## Когда вызывается
Автоматически как гейт после `developer`, до перехода к reviewer/qa. Явно: `@developer-guardian`.

## Вход → Выход
- Вход: изменённый код, `specs/<task-id>/impl-notes.md`, `plan.md`, `security-requirements.md`,
  `.claude/reference/SECURITY-BASELINE.ru.md`, контракт `.claude/agents/developer/developer.md`,
  `guides/GIT-FLOW-GUIDE.ru.md`.
- Выход: отчёт (verdict `pass|needs-changes|blocked`), который оркестратор сохраняет в
  `specs/<task-id>/guardians/developer-guardian.md`.

## Tools (scope) и почему
`Read, Grep, Glob` — тир **Verifier**. Без `Write/Edit/Bash/Skill`: независимость проверки
гарантирована на уровне доступа, а не обещания.

## Красные линии
Ничего не правит; `pass` только при закрытых обязательных пунктах (зелёные тесты, безопасность,
соответствие плану); без вкусовщины.
