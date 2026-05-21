# specs/ — артефакты задач (handoff между агентами)

Каждая задача получает папку `specs/<task-id>/` (kebab-case slug, напр. `key-management`).
Внутри агенты складывают артефакты, которые читают следующие роли. Это «зона передачи» pipeline.

```
specs/<task-id>/
  spec.md                     # pm — что и зачем, acceptance criteria, scope
  research.md                 # research-analyst — внешний research
  decisions/ADR-NNN-*.md      # research-analyst/architect — зафиксированные решения
  plan.md                     # architect — выбранный подход, модули, контракты
  threat-model.md             # security — модель угроз
  security-requirements.md    # security — требования безопасности (контракт для builders)
  ux-spec.md                  # cli-ux — дизайн консольного вывода
  mcp-spec.md                 # mcp-engineer — дизайн MCP
  service-design.md           # system-dev — сервис/кросс-сборка
  test-plan.md                # qa — тест-план
  review.md                   # reviewer — ревью (персистит главный Claude)
  guardians/
    <agent>-guardian.md       # отчёты стражей (персистит главный Claude)
```

Код продукта живёт в исходниках репозитория и в ветке по `guides/GIT-FLOW-GUIDE.ru.md`.
Продуктовая документация — в `docs/` (пишет tech-writer).

Образец ожидаемого уровня детализации артефактов — в `S08-subagents-starter-kit/specs/EXAMPLE-001/`.
