# Страж `devops-guardian`

## Назначение
Read-only гейт качества для работы роли `devops` (`install.sh`, `.goreleaser.yaml`, CI +
`release-plan.md`). Проверяет безопасность установщика, полноту build-матрицы, регистрацию сервиса,
обработку macOS quarantine, отсутствие секретов и git-flow.

## Когда вызывается
Автоматически как гейт после `devops`, до перехода к reviewer/tech-writer. Явно: `@devops-guardian`.

## Вход → Выход
- Вход: `install.sh`, `.goreleaser.yaml`, `specs/<task-id>/release-plan.md`,
  `.claude/reference/SECURITY-BASELINE.ru.md`, контракт `.claude/agents/devops/devops.md`,
  `guides/GIT-FLOW-GUIDE.ru.md`.
- Выход: отчёт (verdict `pass|needs-changes|blocked`), который оркестратор сохраняет в
  `specs/<task-id>/guardians/devops-guardian.md`.

## Tools (scope) и почему
`Read, Grep, Glob` — тир **Verifier**. Без `Write/Edit/Bash/Skill`: независимость проверки
гарантирована на уровне доступа, а не обещания.

## Красные линии
Ничего не правит; `pass` только при закрытых обязательных пунктах (проверка SHA256, нет секретов,
полная матрица); без вкусовщины.
