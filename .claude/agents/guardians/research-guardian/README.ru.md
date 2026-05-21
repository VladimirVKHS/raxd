# Страж `research-guardian`

## Назначение
Read-only гейт качества для артефактов роли `research-analyst` (`research.md` и ADR). Проверяет,
что у каждого факта есть URL, есть сравнение вариантов и рекомендация, ADR заполнены, архитектура
не выбрана за architect, учтена актуальность источников.

## Когда вызывается
Автоматически как гейт после `research-analyst`, до перехода к architect. Явно: `@research-guardian`.

## Вход → Выход
- Вход: `specs/<task-id>/research.md`, `specs/<task-id>/decisions/*`, контракт
  `.claude/agents/research-analyst/research-analyst.md`, `STACK.ru.md`, `SECURITY-BASELINE.ru.md`.
- Выход: отчёт (verdict `pass|needs-changes|blocked`), который оркестратор сохраняет в
  `specs/<task-id>/guardians/research-guardian.md`.

## Tools (scope) и почему
`Read, Grep, Glob` — тир **Verifier**. Без `Write/Edit/Bash/Skill`: независимость проверки
гарантирована на уровне доступа, а не обещания.

## Красные линии
Ничего не правит; `pass` только при фактах с URL и заполненных ADR; без вкусовщины.
