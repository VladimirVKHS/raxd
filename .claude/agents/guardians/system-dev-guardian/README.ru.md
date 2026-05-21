# Страж `system-dev-guardian`

## Назначение
Read-only гейт качества для артефакта роли `system-dev` (`service-design.md` + сервис-файлы).
Проверяет покрытие обеих ОС (systemd+launchd), non-root/capabilities, build-матрицу (4 цели),
lifecycle с авто-рестартом, ветку по git-flow, соответствие plan и стек.

## Когда вызывается
Автоматически как гейт после `system-dev`, до перехода к developer/devops. Явно:
`@system-dev-guardian`.

## Вход → Выход
- Вход: `specs/<task-id>/service-design.md` + сервис-файлы, `plan.md`, контракт
  `.claude/agents/system-dev/system-dev.md`, `STACK.ru.md`, `SECURITY-BASELINE.ru.md`.
- Выход: отчёт (verdict `pass|needs-changes|blocked`), который оркестратор сохраняет в
  `specs/<task-id>/guardians/system-dev-guardian.md`.

## Tools (scope) и почему
`Read, Grep, Glob` — тир **Verifier**. Без `Write/Edit/Bash/Skill`: независимость проверки
гарантирована на уровне доступа, а не обещания.

## Красные линии
Ничего не правит; `pass` только при закрытых обязательных пунктах; без вкусовщины.
