# Guardian Report: developer-guardian — mcp-server

**Дата:** 2026-05-21
**Ветка:** `feature/mcp-server` (6 коммитов)
**Артефакты:** `internal/mcp/{server,tools,audit}.go` + `mcp_test.go`, `internal/server/{server,audit,auth,handlers}.go`
+ `audit_mcp_test.go`, `internal/cli/serve.go`, `go.mod`/`go.sum`/`vendor/`, `Dockerfile`, impl-notes.md.
Сверка с plan.md, mcp-spec.md, security-requirements.md (SR-27..39), spec.md (15 AC).

## Итог — pass

- **AuditRecord.Tool + writeAudit (M1/SR-35/36):** поле `Tool string` (`audit.go:26`); `writeAudit` эмитит
  `INFO MCP fp=... remote=... tool=... result=ok` для success при Tool!="" (`audit.go:55-63`); не-MCP
  записи не изменены (`TestWriteAuditNonMCPUnchanged`).
- **Монтаж /mcp (SR-27/28/29):** `server.New(cfg,paths,store,logger,mcpHandler)` (`server.go:61`); nil→501,
  не-nil→/mcp внутри цепочки за auth/Origin/rate-limit/audit; SDK-handler своего auth-канала не вводит;
  неаутентифицированный /mcp→401 (наследуется).
- **internal/mcp:** ping→pong; server_info→{name,version,protocolVersion} без секретов
  (`TestMCPServerInfoNoSecrets`); `internal/mcp` не импортирует keystore (`TestMCPPackageDoesNotImportKeystore`);
  withAudit пишет tool+fp (из ctx, не ключ); неизвестный инструмент→-32602; GET /mcp→405.
- **FingerprintFromContext** экспортирована (`auth.go:35`); serve.go собирает и передаёт MCP-handler (AC11).
- **Тесты:** 20 (mcp_test) + 4 (audit_mcp_test); без t.Skip; покрывают AC; -race. vendor синхронен
  (SDK v1.6.0 + jsonschema-go v0.4.3 + uritemplate/v3 v3.0.2; pure Go, без CGO). Коммиты Conventional;
  scope не расширен (нет execute_command/upload_file).

## Находки (не блокеры)

| # | Severity | Описание |
|---|----------|----------|
| 1 | LOW | `internal/cli/cli_gaps_test.go:275,310` — `t.Skip("port 7822 unavailable…")` унаследован из tls-transport (не файлы mcp). Передано qa оценить (убрать/обосновать). |
| 2 | MEDIUM (info) | `TestMCPServerInfoNoSecrets` проверяет подстроки keyStr/"keys.db"/"key.pem"(путь), но не содержимое приватного TLS-ключа. Риск 0 (handler ключ не читает), но не строгое доказательство SR-34. → qa усилить. |
| 3 | MEDIUM (info) | SDK v1.6.0 имеет встроенный DNS-rebinding-чек в ServeHTTP (после auth); `CrossOriginProtection=nil` → Origin-защиту даёт транспортный hostOriginMiddleware (SR-32 корректно). Не задокументировано в impl-notes. → задокументировать. |

## Verdict
pass
