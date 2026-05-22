# Guardian Report: architect-guardian — mcp-server

**Дата:** 2026-05-21
**Артефакт:** `specs/mcp-server/plan.md` (сверка со spec.md, research.md, ADR-001/002/003,
`internal/server/*`, `internal/keystore`, `internal/version`, SECURITY-BASELINE §1/§2/§4/§6, MCP-INTEGRATION)

## Раунд 1 — needs-changes

- M1 (MAJOR): запланированный MCP-аудит через `AuditRecord{Reason:"tool="+name}` несовместим с реальной
  `writeAudit` (при success выводит только fp+remote, игнорирует Reason; `AuditRecord` без поля под имя
  инструмента) → AC9 (имя инструмента+результат в логе) провалился бы.
- m1 (minor): формулировка fingerprint (AC9 говорит `keystore.Fingerprint`, план берёт из ctx — эквивалентно, но не оговорено).
- m2 (minor): длина 108 > 100.

## Раунд 2 — pass

- M1 закрыт: зафиксирован один путь — поле `AuditRecord.Tool string` + `writeAudit` логирует `tool=<rec.Tool>`
  во ВСЕХ ветках (вкл. success при Tool!=""), новый msg-label `MCP` (`INFO MCP fp=... remote=... tool=ping
  result=ok`); connection-записи (Tool=="") не меняются; помечено ЛОМАЮЩИМ изменением (как server.New) с
  перечнем затронутых файлов/тестов. Совместимость с существующими ассертами (Contains-подстроки) подтверждена.
- m1 закрыт: `FingerprintFromContext` === значение `keystore.Fingerprint` из ctx; тело ключа MCP-слою
  недоступно (AC10); `"-"` без ключа. Соответствует `auth.go:22-27`.
- m2 закрыт: ~100 контентных строк, контракты не урезаны (неделимая задача).

Регрессий нет: один подход (SDK), монтаж `/mcp` внутри middleware-цепочки (auth/Origin/rate-limit/audit),
ping/server_info, JSON-RPC коды -32700/-32600/-32601/-32602, поток end-to-end, полная таблица AC1-15, scope
без execute_command/upload_file, нет тел функций, baseline §1/§2/§4/§6.

## Verdict (раунд 2)
pass
