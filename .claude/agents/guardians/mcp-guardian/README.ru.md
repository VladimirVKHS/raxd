# Страж `mcp-guardian`

## Назначение
Read-only гейт качества для артефакта роли `mcp-engineer` (`mcp-spec.md`). Проверяет транспорт
(Streamable HTTP/TLS), наличие схем и ошибок у каждого tool, поток вызова, версии spec/SDK,
отсутствие Go-кода, учёт безопасности.

## Когда вызывается
Автоматически как гейт после `mcp-engineer`, до перехода к developer. Явно: `@mcp-guardian`.

## Вход → Выход
- Вход: `specs/<task-id>/mcp-spec.md`, контракт `.claude/agents/mcp-engineer/mcp-engineer.md`,
  `MCP-INTEGRATION.ru.md`, `SECURITY-BASELINE.ru.md`.
- Выход: отчёт (verdict `pass|needs-changes|blocked`), который оркестратор сохраняет в
  `specs/<task-id>/guardians/mcp-guardian.md`.

## Tools (scope) и почему
`Read, Grep, Glob` — тир **Verifier**. Без `Write/Edit/Bash/Skill`: независимость проверки
гарантирована на уровне доступа, а не обещания.

## Красные линии
Ничего не правит; `pass` только при закрытых обязательных пунктах; без вкусовщины.
