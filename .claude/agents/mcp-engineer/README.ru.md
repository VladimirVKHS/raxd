# Агент `mcp-engineer` (MCP Engineer)

## Назначение
Проектирует MCP-сервер `raxd` (tools/resources/prompts, схемы вход/выход, транспорт, поток
вызова) и фиксирует это в `mcp-spec.md`. Контракт для developer, который пишет код.

## Когда вызывается
- **Авто**: «спроектируй MCP», «опиши tools для агента», «как ИИ-агент будет вызывать команды».
- **Явно**: `@mcp-engineer спроектируй MCP-сервер`.

## Вход → Выход
- Вход: `specs/<task-id>/spec.md`, `plan.md`, `.claude/reference/MCP-INTEGRATION.ru.md`,
  `SECURITY-BASELINE.ru.md`.
- Выход: `specs/<task-id>/mcp-spec.md` (шаблон `templates/mcp-spec.template.md`).

## Tools (scope) и почему
`Read, Grep, Glob, Write, WebFetch, Skill`. Тир **Author**: пишет только md-артефакт (дизайн и
JSON-схемы), без `Edit`/`Bash` — Go-реализация это зона developer.

## Подключённые скилы
`compound-engineering:agent-native-architecture`.

## Красные линии
Транспорт Streamable HTTP/TLS (не stdio); каждый tool проходит auth→rate-limit→audit→exec;
без Go-кода; у каждого tool входная/выходная схема и ошибки; версии spec (2025-11-25) и SDK
(modelcontextprotocol/go-sdk).

## Место в pipeline
… security → (cli-ux ‖ `mcp-engineer` ‖ system-dev) → developer → … Проверяющий страж:
**mcp-guardian**.
