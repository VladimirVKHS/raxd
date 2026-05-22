# Docs Outline: command-exec — MCP-инструмент `execute_command`

Автор продукта: **Vladimir Kovalev, OEM TECH**. Язык документации: английский (как существующие
`docs/`); внутренние пометки — русский.

Источник истины — РЕАЛЬНЫЙ КОД (сверено построчно):
`internal/cmdexec/{exec.go,cappedwriter.go,sysproc_unix.go}`, `internal/mcp/{exec_tool.go,server.go}`,
`internal/server/audit.go`, `internal/config/config.go` (`ExecConfig`), тесты
`internal/mcp/{exec_tool_test.go,exec_qa_test.go}`. Спеки (`spec.md`, `mcp-spec.md`,
`security-requirements.md`, `threat-model.md`, `review.md`, `impl-notes.md`) — контекст; при
расхождении со спекой документируется КОД (см. «Расхождения спека↔код»).

## Структура docs/ (что трогаем в этой задаче)

- `docs/mcp.md` — **обновлён**: `execute_command` встроен как третий tool рядом с `ping`/`server_info`
  (назначение, вход `ExecInput`, выход `ExecOutput` из 7 полей, error-mapping, audit, curl-примеры).
  «Scope and limitations» переписан: command-exec больше НЕ «not implemented».
- `docs/configuration.md` — **обновлён**: новая секция `exec.*` (реальные ключи/дефолты из
  `ExecConfig`) + предупреждения безопасности (allowlist-семантика, deny_root, секреты в argv).
- `docs/troubleshooting.md` — **обновлён**: типовые `isError`-случаи `execute_command`, чтение
  exec-аудита (`msg=MCP result=ok` / `msg=DENY reason=` / `msg=FAIL reason=` / `msg=WARN
  reason=running-as-root`), факт «`timed_out:true` — не ошибка». Старый «unknown tool execute_command»
  скорректирован.
- `docs/commands.md` — **обновлён**: упоминание `execute_command` в составе `serve`/MCP, exec-аудит в
  «Audit stream», `serve` scope больше не говорит «command execution not implemented».
- `README.md` — **обновлён**: `execute_command` в «What works today», убран из «Coming next»,
  добавлен короткий пример + ссылка на `docs/mcp.md`.
- `docs/execute-command-security.md` — **новый**: концентрированные предупреждения безопасности
  (П-3/SR-63 секреты в argv, allowlist-семантика F-2, deny_root/root, изоляция/остаточные риски),
  чтобы не раздувать `mcp.md`/`configuration.md` и иметь одну ссылку для оператора.

(Не трогаем: `install.md` — установщика нет; man-страниц нет; `development.md` — без exec-специфики.)

## На каждый документ

### docs/mcp.md (раздел `execute_command`)
- **Цель**: дать интегратору ИИ-агента точный контракт нового инструмента (что делает, вход/выход,
  ошибки, audit, примеры).
- **Аудитория**: интегратор MCP-клиента / ИИ-агент-разработчик; вторично — оператор.
- **Ключевые секции**: что делает (бинарь+argv без shell); вход (`command`/`args`/`timeout_ms`/`cwd`,
  нет `env`, `additionalProperties:false`); выход (7 полей в `structuredContent` + text-резюме);
  поведение/error-mapping (ненулевой exit и таймаут — не ошибка; deny/fail → `isError:true`;
  неизвестный инструмент → −32602; транспорт 401/403/405/429); curl-примеры (успех/ненулевой
  exit/deny/таймаут/лишнее поле); audit (success/deny/fail/root-WARN); ссылка на security-doc.

### docs/configuration.md (секция `exec`)
- **Цель**: документировать реальные ключи `exec.*` и безопасные дефолты, как их менять.
- **Аудитория**: оператор/админ raxd.
- **Ключевые секции**: YAML-пример с дефолтами; таблица ключ→тип→дефолт→смысл; заметки безопасности
  (allowlist-семантика, env-whitelist, deny_root, секреты в argv) со ссылкой на security-doc.

### docs/execute-command-security.md (новый)
- **Цель**: собрать обязательные предупреждения безопасности в одном месте.
- **Аудитория**: оператор/безопасник перед прод-инсталляцией.
- **Ключевые секции**: «опасный примитив» (RCE уровня SSH); НЕ передавать секреты в argv (П-3/SR-63);
  allowlist строгий и точный (F-2/ОР-5); deny_root/root (П-2/ОР-1); не-root + контейнер; остаточные
  риски (форк-бомба/ресурсы вне scope v1 — ОР-3); ротация аудита (ОР-2).

### docs/troubleshooting.md (раздел execute_command)
- **Цель**: помочь диагностировать `isError` и читать exec-аудит.
- **Аудитория**: оператор/интегратор.
- **Ключевые секции**: команда вернула `isError` (not found / deny / bad cwd / лимиты / таймаут-как-
  не-ошибка); как читать exec-аудит; root-WARN.

### docs/commands.md / README.md
- **Цель**: консистентно отразить, что `serve`/MCP теперь предоставляет `execute_command`.
- **Аудитория**: все.
- **Ключевые секции**: список MCP-инструментов (три), exec-аудит в «Audit stream», статус-таблицы.

## Примеры команд (проверяемые, из реального CLI/кода)

- `raxd key create --name agent` — выпуск ключа для MCP-клиента (показывается один раз).
- `raxd serve` — запуск TLS-сервера с MCP-эндпоинтом `/mcp` (в Docker).
- `curl -k https://127.0.0.1:7822/mcp -H "Authorization: Bearer $KEY" -H "Content-Type:
  application/json" -H "Accept: application/json, text/event-stream" -d '{"jsonrpc":"2.0","id":10,
  "method":"tools/call","params":{"name":"execute_command","arguments":{"command":"ls","args":["-la"],
  "timeout_ms":5000}}}'` — вызов инструмента (структура ответа сверена с go-sdk и примерами `ping`).
- `config.yaml`: `exec.allowlist: ["ls","cat"]`, `exec.deny_root: true` — включение allowlist и
  hard-fail от root (ключи и дефолты сверены с `config.go`).

## Об авторе (OEM TECH)

**Vladimir Kovalev, OEM TECH** — присутствует в README (раздел Author + баннер) и в footer
`docs/mcp.md`; новый `docs/execute-command-security.md` несёт тот же footer для консистентности.

## Расхождения спека↔код (документируем КОД)

1. **`Result:"warn"` — реальное значение аудита (раунд 2 developer-guardian, impl-notes #2).**
   `mcp-spec §2.3` перечисляет только `success/deny/fail`, но в коде root-детекция пишет ОТДЕЛЬНУЮ
   запись `Result:"warn"`, рендеримую `writeAudit` как `level=WARN msg=WARN ... reason=running-as-root
   ...` (`internal/server/audit.go:117-136`, `internal/mcp/exec_tool.go:94-105`). Документируется как
   `msg=WARN`, НЕ как «root-WARN внутри success».
2. **deny_root=true && euid==0 → ДВЕ записи**: сначала `warn` (running-as-root), затем `deny`
   (`exec_tool.go:94-120`). Документируется обе.
3. **Текст ошибки клиенту для input-лимитов — НЕ нейтральный**: `too many arguments: N > M`,
   `argument too long: N > M`, `timeout_ms N exceeds max M`, `execution as root is forbidden by policy`
   возвращаются дословно (`exec_tool.go`); тогда как allowlist-deny → `command not allowed`, fail →
   `command not found` (нейтральные). Документируется как есть.
4. **Абсолютный путь бинаря**: проверяется только `os.Stat` (F-1) — существование, без проверки
   x-бита; несуществующий абсолютный путь → fail (`command not found`). Документируется поведение.
5. **allowlist + абсолютный путь**: запись должна ТОЧНО совпадать с присланной строкой `command`
   (`entry == in.Command`, `exec.go:85`); `ls` ≠ `/bin/ls` (F-2/ОР-5). Документируется обязательно.

## Открытые вопросы

- None. Каждое поле/флаг/поведение сверено с кодом. Расхождения спека↔код разрешены в пользу кода и
  перечислены выше; они НЕ блокируют документацию (поведение однозначно подтверждено кодом и тестами).
</content>
</invoke>
