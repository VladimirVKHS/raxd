# Агент `architect` (Architect)

## Назначение
Превращает `spec.md` (и `research.md`) в `plan.md`: выбирает РОВНО ОДИН подход, описывает модули с
путями и контракты (сигнатуры, типы, обработка ошибок). Не пишет тел функций и не меняет AC.
Третий шаг pipeline `raxd`, между `research-analyst` и ролями реализации.

## Когда вызывается
- **Авто**: «продумай архитектуру…», «спроектируй модули для…», «как это построить».
- **Явно**: `@architect продумай архитектуру задачи X`.

## Вход → Выход
- Вход: `specs/<task-id>/spec.md`, `specs/<task-id>/research.md`, `.claude/reference/STACK.ru.md`,
  `SECURITY-BASELINE.ru.md`, `MCP-INTEGRATION.ru.md`.
- Выход: `specs/<task-id>/plan.md` (шаблон `templates/plan.template.md`).

## Tools (scope) и почему
`Read, Grep, Glob, Write, WebFetch, Skill`. Тир **Author**: пишет только md-артефакт, без
`Edit`/`Bash` — код пишет developer, не architect.

## Подключённые скилы
`superpowers:writing-plans`, `compound-engineering:ce-plan`.

## Красные линии
Ровно один подход (альтернатива — только в Trade-offs); без тел функций; AC из spec не менять;
новые зависимости — только с обоснованием и сверкой со STACK; Trade-offs называют цену; 30-100 строк.

## Место в pipeline
`research-analyst` → `architect` → (security ‖ cli-ux ‖ mcp-engineer ‖ system-dev) → developer → …
Проверяющий страж: **architect-guardian**.
