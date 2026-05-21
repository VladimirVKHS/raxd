# Команда субагентов raxd — как пользоваться

Набор из **24 субагентов** для разработки продукта `raxd`: 12 функциональных ролей +
12 «стражей» (guardians, по одному на роль). Дирижёр — главный Claude Code в корне репозитория,
правила для него — в `CLAUDE.md`.

## Состав и порядок (pipeline)

```
pm → research-analyst → architect → security → (cli-ux ‖ mcp-engineer ‖ system-dev)
   → developer → (devops ‖ qa) → reviewer → tech-writer
```

После каждого шага его **guardian** проверяет результат (read-only) и выдаёт verdict
`pass | needs-changes | blocked`. `needs-changes`/`blocked` → возврат к роли.

| Роль | Что делает | Артефакт |
|---|---|---|
| `pm` | требования, acceptance criteria, scope | `specs/<id>/spec.md` |
| `research-analyst` | внешний research (Go/MCP/безопасность) | `specs/<id>/research.md`, ADR |
| `architect` | один выбранный подход, модули, контракты | `specs/<id>/plan.md` |
| `security` | модель угроз, требования безопасности | `threat-model.md`, `security-requirements.md` |
| `cli-ux` | дизайн консольного вывода, баннер, таблицы | `specs/<id>/ux-spec.md` |
| `mcp-engineer` | дизайн MCP (tools/resources/транспорт) | `specs/<id>/mcp-spec.md` |
| `system-dev` | сервис (systemd/launchd), кросс-сборка | сервис-файлы, `service-design.md` |
| `developer` | код по plan/spec | ветка по git-flow |
| `devops` | install.sh, goreleaser, CI | `install.sh`, `.goreleaser.yaml` |
| `qa` | тест-план и тесты | `test-plan.md` + тесты |
| `reviewer` | ревью кода против spec+plan (read-only) | `specs/<id>/review.md` |
| `tech-writer` | документация продукта | `docs/**` |

## Как вызывать

- **Автоматически**: пишите задачу обычным языком — главный Claude по полю `description`
  выберет роль. Пример: «опиши требования к экспорту ключей» → `pm`.
- **Явно**: `@architect продумай архитектуру` — гарантированный вызов конкретной роли.

## Тиры доступа (tools scoping)

- **Authors** (pm, research-analyst, architect, security, cli-ux, mcp-engineer, tech-writer):
  `Read, Grep, Glob, Write, WebFetch, Skill` — пишут только md-артефакты, НЕ трогают код
  (нет `Edit`/`Bash`).
- **Builders** (system-dev, developer, devops, qa): `+ Edit, Bash` — пишут код и запускают команды.
- **Verifiers** (reviewer + все guardians): `Read, Grep, Glob` — только чтение. Свой отчёт
  возвращают текстом; **главный Claude сохраняет** его в файл-артефакт. Это архитектурная
  гарантия независимости: без `Write/Edit/Bash` они физически не могут менять код.

## Reference (общие знания)

`.claude/reference/` — единый источник истины, который агенты читают перед работой:
`STACK.*` (стек), `SECURITY-BASELINE.*` (обязательный чеклист безопасности),
`MCP-INTEGRATION.*` (транспорт/SDK). Меняете стек — меняйте здесь, а не в каждом агенте.

## Тюнинг

- **Модель**: правьте `model:` во frontmatter (`opus` для рассуждения, `sonnet` для рутины,
  `haiku` для дешёвых guardian-проверок).
- **Скилы**: список — в разделе «Скилы» каждого агента (вызов через инструмент `Skill`).
- **Имя продукта** `raxd`: меняется правкой `.claude/reference/STACK.*` и `CLAUDE.md`.

## Проверка установки

Запустите `claude` в корне `test_project/` и наберите `/agents` — должны быть видны все 24 имени.

**Если guardians не видны** (некоторые сборки Claude Code не сканируют вложенность на 2 уровня):
переместите файлы стражей из `.claude/agents/guardians/<name>/<name>.md` в плоский
`.claude/agents/<name>.md`, оставив их доки/шаблоны в подпапке. Идентичность агента определяется
полем `name`, а не путём, поэтому ничего больше менять не нужно.

Автор: **Vladimir Kovalev, OEM TECH**.
