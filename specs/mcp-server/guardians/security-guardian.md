# Guardian Report: security-guardian — mcp-server

**Дата:** 2026-05-21
**Артефакты:** `specs/mcp-server/threat-model.md`, `specs/mcp-server/security-requirements.md`
(сверка со spec.md, plan.md, research/ADR, ОБЯЗАТЕЛЬНЫМ SECURITY-BASELINE, наследуемыми SR tls-transport)

## Итог

Новый MCP-слой покрыт по всем 7 векторам (R-M1..R-M7 + R-M-Origin), 13 SR (SR-27..SR-39). Транспортные
контроли (TLS1.3/серт/rate-limit/bind/Origin/graceful, SR-1..26) корректно НАСЛЕДУЮТСЯ ссылкой, не
переоткрываются. baseline §1/§2/§4/§6 отражён; §3 (исполнение команд) корректно вне scope с эскалацией
(ОР-М2 → command-exec/file-upload).

**Ослаблений baseline нет** (наоборот, усилены инварианты):
- SR-27/28/29 (R-M1): `/mcp` смонтирован ВНУТРИ цепочки, auth ДО MCP; SDK-handler не вводит второй
  auth-канал; MCP-сессия НЕ для аутентификации.
- SR-33/34 (R-M2): server_info — только версия из `internal/version`; полный ключ/приватный TLS-ключ
  отсутствуют как подстрока в любом MCP-ответе И в аудите (проверка подстрокой).
- SR-35/36 (R-M3): `withAudit` пишет fingerprint (из ctx, не ключ) + `tool=<имя>` + результат через
  `AuditRecord.Tool`; не ломает формат не-MCP записей и наследуемые тесты.
- SR-30/31 (R-M4): JSON-RPC коды -32700/-32600/-32601/-32602, input→isError, без паники.
- SR-37 (R-M5): execute_command/upload_file НЕ регистрируются; неизвестный инструмент → ошибка, не исполнение.
- SR-38 (R-M6): вендоринг SDK офлайн, permissive-лицензии, без CGO, go.sum/go mod verify, CGO_ENABLED=0.
- SR-39 (R-M7): конкурентность SDK-handler под `-race`.
Эскалации (ОР-М1 mTLS, ОР-М2 §3 exec, ОР-М3 полная Origin-политика, ОР-М4 точный vendor) оформлены
риск+причина+смягчение.

## Находки (не блокер)

| # | Severity | Описание |
|---|----------|----------|
| 1 | low/косметика | Словесный порядок middleware в наследуемой ссылке (`bodyLimit→recover→Host/Origin→auth→rate-limit→authSuccessAudit→mux`) отличается от формулировки SR-14 tls-transport (`audit→recover→...`). Инварианты (auth и Host/Origin ДО /mcp) согласованы; рассинхрон только словесный. Опц. синхронизировать. |

## Verdict
pass
