# tech-writer-guardian — задача `mcp-server`

**Итоговый verdict: pass** (раунд 2). Раунд 1 — needs-changes (3 findings, ниже), все исправлены.

---

## Раунд 1

**Verdict: needs-changes**

Read-only финальный гейт качества продуктовой документации raxd. Отчёт сохранён дирижёром
(verifier-роли ничего не пишут сами — CLAUDE.md red line №1).

## Проверенные файлы

- `docs/mcp.md`
- `README.md`
- `docs/commands.md`
- `docs/troubleshooting.md`
- `docs/development.md`
- `specs/mcp-server/docs-outline.md`

Сверялось с: `internal/mcp/{server,tools,audit,mcp_test,mcp_security_test}.go`,
`internal/server/{server,audit,auth,middleware,tls}.go`, `internal/config/config.go`,
`docs/configuration.md`, `go.mod`, `specs/mcp-server/{spec,mcp-spec,review}.md`,
`.claude/reference/{MCP-INTEGRATION,SECURITY-BASELINE}.ru.md`.

## Соответствие коду (подтверждено)

| Утверждение | Статус |
|---|---|
| Маршрут `/mcp`, Streamable HTTP, protocolVersion `2025-11-25` | подтверждено |
| SDK `github.com/modelcontextprotocol/go-sdk` v1.6.0 (go.mod) | подтверждено |
| Ровно два инструмента `ping` и `server_info` | подтверждено |
| `ping` → text `"pong"`, без side effects, не isError | подтверждено |
| `server_info` → ровно `{name, version, protocolVersion}` + text | подтверждено |
| Auth Bearer ДО MCP; `internal/mcp` не импортирует keystore | подтверждено |
| 401/403/429 не доходят до MCP | подтверждено |
| GET `/mcp` → 405 (Stateless=true) | подтверждено |
| Аудит `result=ok`, fp/remote из контекста (не тело ключа) | подтверждено |
| Origin default `localhost,127.0.0.1,::1` | подтверждено |
| TLS 1.3, ECDSA P-256, SAN 127.0.0.1/localhost; порт 7822, bind 127.0.0.1 | подтверждено |
| Неизвестный инструмент → JSON-RPC error, не исполнение | подтверждено тестами |

## Findings

### MAJOR — docs/mcp.md:177 и docs/troubleshooting.md:267 — код unknown tool
Документация утверждает unknown tool / bad params → JSON-RPC `-32602` (Invalid params).
Тесты (`mcp_security_test.go` ~527-528, `TestMCPUnknownToolReturnsError`) принимают
`-32602 || -32601`; комментарий теста прямо допускает `-32601` (Method not found).
SDK v1.6.0 для незарегистрированного tool в `tools/call` фактически возвращает `-32601`.
Документ описывает только `-32602` — потенциально неверный единственный код.
**Контракт:** чеклист п.1 (точность), red line tech-writer «документируй только реально
существующее, сверяй с кодом».
**Fix:** в `docs/mcp.md` (таблица «Behaviour and error handling», строка Unknown tool) и
`docs/troubleshooting.md:267` заменить на «JSON-RPC error `-32601` (Method not found) или
`-32602` (Invalid params), в зависимости от версии SDK».

### MINOR — docs/mcp.md (~95, JSON-примеры ~236, ~249) — `isError: false`
Документ показывает `isError: false` как гарантированно присутствующее поле. В коде
`IsError` не задаётся явно (нулевое значение), SDK при `omitempty` может опускать поле.
**Fix:** уточнить «`isError` отсутствует либо равно `false`» или убрать `"isError": false`
из JSON-примеров со сноской, что поле присутствует только при `true`.

### MINOR — docs/mcp.md:158 — расположение ссылки на configuration.md
Ссылка `configuration.md#networking-and-serve-fields` корректна (якорь существует), но стоит
в середине предложения об audit stream — читаемость. Не ошибка фактуры.
**Fix (опц.):** перенести ссылку в конец абзаца об отказах.

## Что хорошо

1. Точность против кода: почти все технические факты подтверждены, выдумок нет.
2. Безопасность: реальных секретов нет, ключи — плейсхолдеры; `curl -k` и
   `NODE_TLS_REJECT_UNAUTHORIZED=0` явно помечены «dev only» с предупреждением.
3. Полнота против docs-outline.md: все разделы написаны, пустых нет, явные `None` с причиной.
4. Консистентность README/commands.md/mcp.md: факты совпадают, противоречий нет.
5. Автор (Vladimir Kovalev, OEM TECH) присутствует в README и docs/mcp.md.
6. Язык/аудитория: CLI и продуктовая документация — английский; план docs-outline — русский.

## Итог раунда 1
Два фактических расхождения docs↔код/тесты (1 major, 1 minor) + 1 minor по читаемости.
Возврат к tech-writer на правки; после — повторный гейт.

---

## Раунд 2

**Verdict: pass**

Tech-writer внёс правки, проверены повторно против кода/тестов/SDK.

### Статус findings раунда 1
- **Finding 1 (MAJOR, код unknown tool) — ИСПРАВЛЕН.** `docs/mcp.md:182` (таблица) и буллет
  `:188-189`, `docs/troubleshooting.md:267-269`: теперь «`-32601` (Method not found) or
  `-32602` (Invalid params), depending on the SDK version». Согласовано с
  `mcp_security_test.go:527-528` (`code != -32602 && code != -32601`).
- **Finding 2 (MINOR, isError:false) — ИСПРАВЛЕН.** `docs/mcp.md:95-96` и `:106`:
  «`isError` is absent or `false`» + сноска `[^iserror]` (`:132-134`) про `omitempty`.
  Из JSON-примеров `ping`/`server_info` строка `"isError": false` удалена. Подтверждено
  тегом SDK `json:"isError,omitempty"` (vendor protocol.go:108, content.go:267).
- **Finding 3 (MINOR, ссылка) — ИСПРАВЛЕН.** `docs/mcp.md:161-163`: ссылка
  `configuration.md#networking-and-serve-fields` перенесена в конец предложения; якорь
  существует (`docs/configuration.md:154`).

### Новые findings
Нет. Регрессий по точности/безопасности/консистентности не обнаружено. JSON-примеры валидны,
сноска определена единожды и вызывается дважды, таблица error-handling внутренне согласована,
формулировки mcp.md↔troubleshooting.md идентичны по смыслу, автор присутствует.

## Финальный итог
Документация mcp-server соответствует реальному коду, тестам и SDK. Гейт пройден.
