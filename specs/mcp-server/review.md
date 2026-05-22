# review.md — MCP Server (`feature/mcp-server`)

**Reviewer:** reviewer (raxd). **Дата:** 2026-05-21.
**Вход:** spec.md (AC1-15), plan.md, mcp-spec.md, security-requirements.md (SR-27..39), threat-model.md,
impl-notes.md, test-plan.md, SECURITY-BASELINE/MCP-INTEGRATION.
**Код:** `internal/mcp/{server,tools,audit}.go` + тесты, `internal/server/{server,audit,auth,handlers}.go`
+ `audit_mcp_test.go`, `internal/cli/serve.go`, `go.mod`/`go.sum`/`vendor/`, `Dockerfile`; сверка с SDK vendor.

## Verdict (раунд 1): accept

Все 15 AC закрыты кодом (не только тестами); блокеров и нарушений baseline §1/§2/§4/§6 нет.

## Подтверждено
- `/mcp` смонтирован ВНУТРИ цепочки (bodyLimit→recover→Host/Origin→auth→rate-limit→authSuccessAudit→mux),
  auth Bearer→store.Verify ДО MCP; 401 без ключа, ErrCorrupt→403 (TestMCPKeystoreCorruptReturns403), MCP не достигается.
- SR-28: `internal/mcp` не импортирует keystore; SDK stateless, сессия-как-auth путь мёртв (проверено в vendor).
- SR-33/34: server_info — только {name,version,protocolVersion}; no-secrets тест читает реальный key.pem + API-ключ.
- SR-37: ровно ping+server_info; execute_command→-32602, не исполнение.
- SR-32: Origin/Host от транспорта (SDK CrossOriginProtection nil); present&invalid→403 до MCP.
- SR-30: битый JSON/неизвестный инструмент→JSON-RPC ошибка, не паника; сервер жив.
- SR-35/36: withAudit пишет fingerprint(из ctx, не ключ)+tool+результат; fingerprint реально пробрасывается
  (SDK передаёт req.Context() в tool-handler; assertRealFingerprint: fp!=- , hex). Двойная AUTH+MCP запись осознанна.
- Конкурентность -race зелёная; nil-handler→501 (обратная совместимость); server.New-сигнатура обновлена везде.
- Supply chain: SDK v1.6.0 + транзитивные завендорены, permissive, без CGO в импорт-графе handler.

## Находки (не блокирующие)
- **LOW-1:** MCP-аудит-запись всегда `remote=-` — `remoteAddrFromCtx` (`internal/mcp/audit.go:71-87`) читает
  ключ `httpRequestCtxKey{}`, который никто не кладёт; SDK v1.6.0 не отдаёт RemoteAddr; комментарий про
  `mcp.ClientAddressFromContext` вводит в заблуждение (API нет). AC9/SR-35 упоминают «удалённый адрес».
  Митигировано: парная AUTH-запись содержит реальный remote (коррелируется по fp+время); ключевые поля
  (fingerprint/tool/результат) на месте. **Дирижёр: исправляется (remote в ctx по схеме fingerprint).**
- **INFO-1:** состав vendor шире задокументированного (добавились go-sdk/auth, oauthex, golang.org/x/oauth2 —
  все permissive, pure Go; ОР-М4 материализовался, не нарушение). Опц.: обновить impl-notes/threat-model.
- **INFO-2:** `ping` получает сгенерированный outputSchema (SDK создаёт при Out!=any); валидно по протоколу,
  расхождение с mcp-spec §4 косметическое. Не трогать.

## LOW-1 — устранён (коммит 10a5e2b)

`authMiddleware` теперь кладёт `r.RemoteAddr` в ctx (`ctxKeyRemoteAddr`, симметрично fingerprint);
экспортирована `server.RemoteAddrFromContext`; `internal/mcp/audit.go` использует её вместо заглушки
(вводящий в заблуждение комментарий про `mcp.ClientAddressFromContext` удалён). MCP-аудит-запись теперь
содержит реальный `remote=IP:port`. Тест `TestMCPAuditHasRealRemoteAddr` (remote!=- + корреляция с AUTH-
записью того же запроса). impl-notes обновлён (LOW-1 + INFO-1 vendor-состав). developer-guardian
фокус-перепроверка: **pass** (в ctx только несекретные fingerprint/keyID/remote; регрессий нет). Docker
зелёный вкл. -race.

## Verdict
accept (LOW-1 устранён; INFO-2 ping outputSchema — оставлено как валидное по протоколу)
