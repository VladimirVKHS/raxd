# tech-writer-guardian — задача `command-exec`

**Итоговый verdict: pass** (раунд 2). Раунд 1 — needs-changes (CRITICAL README isError:false + MINOR),
все закрыты. Финальный гейт docs. Сохранено дирижёром.

## Раунд 1 — needs-changes

Проверены: docs/mcp.md (раздел execute_command), docs/execute-command-security.md (новый),
docs/configuration.md (секция exec), docs/troubleshooting.md, docs/commands.md, README.md,
specs/command-exec/docs-outline.md. Сверено с кодом (exec.go, exec_tool.go, config.go, audit.go,
vendor protocol.go).

### Соответствие коду — подтверждено
ExecInput 4 поля (нет env, additionalProperties:false), ExecOutput 7 полей, ExecConfig все 9 ключей+
дефолты (allowlist[], 30000/300000, /tmp, [PATH,HOME,LANG,TERM], 256/131072/1048576, deny_root false),
аудит-рендер (success→MCP+result=ok, deny→DENY+reason, fail→FAIL+reason, root→WARN+reason=running-as-root),
error-mapping (exit≠0 не ошибка; deny/not-found/timeout>max/лимиты/bad-cwd→isError; unknown→−32602),
двойная запись warn+deny при deny_root=true&euid==0, text-резюме. Всё точно.

### Обязательные предупреждения (П-3/SR-63, F-2) — все на месте
Секреты в argv (логируются дословно) — в 5 местах; строгий allowlist (ls≠/bin/ls, пустой=всё) — mcp.md/
security/configuration; deny_root WARN vs hard-fail; не-root+контейнер. Покрыто.

### Findings
- **CRITICAL.** README.md:240 — выдуманный JSON: пример ping содержит `"isError":false`. SDK сериализует
  IsError с `json:"isError,omitempty"` (vendor protocol.go:108) → при false поле ОПУСКАЕТСЯ, его в ответе
  нет. mcp.md это объясняет верно, README противоречит. Нарушение red line tech-writer (только реальное).
  Fix: убрать `"isError":false` из примера (как в mcp.md smoke-test) + сноска что isError опускается на успехе.
- **MINOR.** troubleshooting.md:316 — клиенту при невалидном cwd возвращается тот же текст `command not found`
  (exec_tool.go:221), что и при отсутствии бинаря; различать по аудиту (reason=bad-cwd vs not-found). Добавить
  явное упоминание в абзац под таблицей.

### Дрейф mcp-spec↔код (для дирижёра, НЕ блокер docs)
Код использует AuditRecord.Result="warn" → рендер msg=WARN reason=running-as-root (audit.go:117,
exec_tool.go:94-105) — появился при фиксе developer-guardian #2 ПОСЛЕ гейта mcp-spec. mcp-spec §2.3.1
таблица не включает строку "warn". Docs описали КОД — это ПРАВИЛЬНО. Рекомендация: синхронизировать
mcp-spec §2.3.1 (добавить строку Result="warn" → WARN/msg=WARN/reason=running-as-root).

### Безопасность доки
Реальных секретов нет (ключи синтетические rax_live_dGhpc..., $KEY); `-k` помечен dev-only; deny-пример
(rm -rf /) демонстрирует отказ, не учит. ОК.

### Что хорошо
Полное покрытие (9 ключей/4 входа/7 выходов/4 ветки аудита/error-mapping), детальный аудит-раздел,
качественный security-гайд (8 разделов), configuration совпадает с config.go, рабочие перекрёстные
ссылки/якоря, автор Vladimir Kovalev OEM TECH сохранён, таймаут-не-ошибка явно в 3 документах.

### Резюме
CRITICAL README:240 + MINOR troubleshooting:316. + дирижёру: sync mcp-spec §2.3.1. После — повторный гейт.

## Раунд 2 — pass
- CRITICAL закрыт: README пример ping без "isError":false (+сноска omitempty со ссылкой на mcp.md);
  tech-writer ДОПОЛНИТЕЛЬНО нашёл и исправил то же в docs/commands.md:769. Grep "isError":false по
  docs/** и README — 0 совпадений (specs/** не в счёт).
- MINOR закрыт: troubleshooting.md разграничивает not-found vs bad-cwd (один клиентский текст, разный
  reason= в аудите) в таблице и подразделе аудита — соответствует exec_tool.go.
Новых findings нет, якоря/ссылки рабочие, консистентность README/commands.md/mcp.md сохранена.
Документация command-exec готова. + дирижёр: mcp-spec §2.3.1 синхронизирован mcp-engineer (Result:"warn").

