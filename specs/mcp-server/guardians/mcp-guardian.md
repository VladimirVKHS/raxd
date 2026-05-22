# Guardian Report: mcp-guardian — mcp-server

**Дата:** 2026-05-21
**Артефакт:** `specs/mcp-server/mcp-spec.md` (сверка со spec.md, plan.md, research/ADR-001/002/003,
security-requirements.md SR-27..39, MCP-INTEGRATION)

## Итог

- Транспорт: Streamable HTTP (офиц. Go SDK v1.6.0, `StreamableHTTPHandler`), `/mcp`, протокол
  `2025-11-25`; GET→405 (stateless v1, обосновано); auth/Origin наследуются от транспорта ДО handler;
  MCP-сессия не для auth (SR-28).
- initialize: объявляется только `tools` (Resources/Prompts/Sampling — нет, Q4); serverInfo
  `{name:"raxd", version: internal/version}`; пример обмена корректен.
- Инструменты ping/server_info: входные/выходные JSON-схемы заданы; `server_info` — белый список
  `{name, version, protocolVersion}` + явный чёрный список запрещённого (ключи/TLS-ключ/пути/порт/bind/
  allowlist/env/uptime/число ключей) — SR-33/34/AC10.
- Поток tools/call: наследуемая цепочка → SDK-handler → `withAudit` (`AuditRecord{Tool,Fingerprint,
  Result,...}`, fingerprint из ctx, не ключ) — SR-35/36; неизвестный инструмент → -32602, НЕ исполнение
  (SR-37); коды -32700/-32600/-32601/-32602; tool-ошибки isError.
- Параметры подключения (URL /mcp, Bearer, self-signed TLS, протокол) + curl smoke-test + набросок
  MCP Inspector/Claude Desktop (streamable-http).
- Точки расширения execute_command/upload_file — описаны без реализации (scope).
- Go-кода нет (только схемы/контракты-сигнатуры); открытые вопросы Q-MCP-1/2/3 с дефолтами.

## Находки (info, не блокеры)

| # | Severity | Описание |
|---|----------|----------|
| 1 | info | Неизвестный инструмент в tools/call → developer должен вернуть именно `-32602` (не `-32601`); §7.1 это разделяет, qa проверит код. |
| 2 | info | SDK уточнён до `v1.6.0` в spec, в STACK — `v1.x`; опц. обновить STACK (как Go 1.25). |
| 3 | info | `listChanged:false` в примере initialize — qa проверять факт наличия `tools` в capabilities, не точное значение. |

## Verdict
pass
