# Страж `security-guardian`

## Назначение
Read-only гейт качества для артефактов роли `security` (`threat-model.md`,
`security-requirements.md`). Проверяет покрытие всех разделов SECURITY-BASELINE, наличие смягчения
у каждого риска, проверяемость требований, отсутствие кода и «упрощений» безопасности.

## Когда вызывается
Автоматически как гейт после `security`, до перехода к developer/system-dev/devops/mcp-engineer.
Явно: `@security-guardian`.

## Вход → Выход
- Вход: `specs/<task-id>/threat-model.md` + `security-requirements.md`, контракт
  `.claude/agents/security/security.md`, `SECURITY-BASELINE.ru.md`.
- Выход: отчёт (verdict `pass|needs-changes|blocked`), который оркестратор сохраняет в
  `specs/<task-id>/guardians/security-guardian.md`.

## Tools (scope) и почему
`Read, Grep, Glob` — тир **Verifier**. Без `Write/Edit/Bash/Skill`: независимость проверки
гарантирована на уровне доступа, а не обещания.

## Красные линии
Ничего не правит; `pass` только при покрытых разделах baseline и смягчении у каждого риска; без
вкусовщины.
