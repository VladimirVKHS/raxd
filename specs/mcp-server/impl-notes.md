# Impl Notes: mcp-server

## Что реализовано

- **`internal/mcp` (новый пакет)**
  - `NewHandler(ver string, audit server.AuditFn) (http.Handler, error)` — строит `mcp.Server`
    с двумя инструментами (`ping`, `server_info`) и возвращает `http.Handler`, смонтированный
    через `mcp.NewStreamableHTTPHandler` с `Stateless: true, JSONResponse: true` (mcp-spec §1.3).
  - `ping` → возвращает текстовый контент `"pong"` (AC5).
  - `server_info` → возвращает JSON `{name, version, protocolVersion}` и текстовую строку
    `"raxd <ver> (MCP 2025-11-25)"` (AC6). Секреты в ответе отсутствуют (SR-33).
  - `withAudit[In, Out any]` — универсальный декоратор `ToolHandlerFor`, пишет `AuditRecord`
    с `Fingerprint` из `server.FingerprintFromContext` (SR-35), именем инструмента, результатом
    `"success"/"fail"` (SR-35/SR-36). Ключевое тело никогда не доступно.
  - `remoteAddrFromCtx` — возвращает `"-"` как безопасный фоллбэк (SDK не экспонирует
    `http.Request` из контекста).
  - Пакет не импортирует `internal/keystore` — выполнен SR-28 (нет второго канала аутентификации).

- **`internal/server/audit.go`** — расширение `AuditRecord`
  - Добавлено поле `Tool string` для имени MCP-инструмента (SR-36). Пустое поле → запись
    для non-MCP соединений без изменений в формате.
  - `writeAudit`: новая ветка `success + Tool != ""` → `INFO MCP fp=… remote=… tool=… result=ok`
    per mcp-spec §2.2 и ux-spec §3. Non-MCP ветка (`AUTH`) не изменена.
  - `NewAuditFn(logger) AuditFn` — производственная функция для `serve.go` (SR-21: не логирует
    тело ключа, заголовок Authorization, хэш, соль, приватный TLS-ключ).
  - `NewAuditFnForTest` — алиас для обратной совместимости с тестами.

- **`internal/server/auth.go`** — экспорт `FingerprintFromContext`
  - `FingerprintFromContext(ctx context.Context) string` — публичная обёртка над приватным
    `fingerprintFromCtx`. Доступна для `internal/mcp`; возвращает 12-hex fingerprint или `"-"`.
    Ключевое тело никогда не передаётся (SR-35/SR-29).

- **`internal/server/server.go`** — расширение сигнатуры `New`
  - `New(cfg, paths, store, logger, mcpHandler http.Handler)` — добавлен последний параметр.
    `nil` сохраняет поведение 501 для `/mcp`. Непустой `mcpHandler` монтируется на `/mcp` до
    catch-all (`/`) в mux (AC11, SR-29).

- **`internal/cli/serve.go`** — подключение MCP-обработчика
  - `internalmcp.NewHandler(version.Version, auditFn)` собирается с той же `auditFn`, что и
    транспортный слой (SR-28: единый канал аутентификации через `authMiddleware`).
  - Результат передаётся в `server.New` как `mcpHandler` (AC11).

- **`Dockerfile`** — расширение race-цели
  - `./internal/mcp/...` добавлен в `CGO_ENABLED=1 go test -race` команду.

## Отклонения/эскалации

Нет. Реализация строго по `plan.md` и контрактам из артефактов задачи.

Примечание по `remoteAddrFromCtx`: SDK `v1.6.0` не предоставляет публичного API для извлечения
`http.Request` из контекста внутри `ToolHandlerFor`. Реализован безопасный фоллбэк `"-"`.
Fingerprint (критическое поле SR-35) передаётся корректно через `server.FingerprintFromContext`.
Это поведение задокументировано в комментариях кода; если SDK добавит публичный API — можно
улучшить без изменения контракта.

## Тесты

Покрытые acceptance criteria → тесты:

| AC | Тест |
|----|------|
| AC1 — 401 без ключа | `TestMCPNoAuthReturns401` |
| AC2 — 401 на неверный ключ | `TestMCPUnknownKeyReturns401` |
| AC3 — 403 на недопустимый origin | `TestMCPInvalidOriginReturns403` |
| AC4 — initialize + capabilities | `TestMCPInitializeCapabilities` |
| AC5 — ping → pong | `TestMCPCallPingReturnsPong`, `TestNewHandlerPingViaHTTPTest` |
| AC6 — server_info | `TestMCPCallServerInfo`, `TestMCPServerInfoNoSecrets`, `TestNewHandlerServerInfoViaHTTPTest` |
| AC7 — tools/list | `TestMCPToolsList` |
| AC8 — неизвестный инструмент → ошибка | `TestMCPUnknownToolReturnsError` |
| AC9 — аудит с fp+tool | `TestMCPAuditHasFingerprintAndTool`, `TestNewHandlerAuditContainsToolAndFP` |
| AC10 — non-MCP без tool= | `TestMCPAuditNonMCPNoToolField` |
| AC11 — GET /mcp → 405 | `TestMCPGetReturns405`, `TestNewHandlerGetReturns405` |
| AC12 — concurrent | `TestMCPConcurrentPing` |
| AC13 — FingerprintFromContext | `TestFingerprintFromContext` |
| SR-28 — нет импорта keystore | `TestMCPPackageDoesNotImportKeystore` |
| SR-36 — поле Tool в AuditRecord | `TestAuditRecordToolField`, `TestWriteAuditMCPSuccessLabel`, `TestWriteAuditNonMCPUnchanged` |

Всего: 20 тестов в `internal/mcp/mcp_test.go` + 4 теста в `internal/server/audit_mcp_test.go`.

Команда запуска (только в Docker, SECURITY-BASELINE §6):

```sh
# Сборка и базовые тесты:
docker build --target test -t raxd-test . && docker run --rm raxd-test

# Полная команда внутри контейнера:
go vet ./... && go test -v -count=1 ./... && CGO_ENABLED=1 go test -race -count=1 ./internal/keystore/... ./internal/server/... ./internal/mcp/...
```

Подтверждение последнего прогона (Docker, до коммита):

```
ok  github.com/vladimirvkhs/raxd/cmd/raxd           (tests)
ok  github.com/vladimirvkhs/raxd/internal/cli
ok  github.com/vladimirvkhs/raxd/internal/config
ok  github.com/vladimirvkhs/raxd/internal/keystore
ok  github.com/vladimirvkhs/raxd/internal/mcp        2.530s
ok  github.com/vladimirvkhs/raxd/internal/server
ok  github.com/vladimirvkhs/raxd/internal/version
--- race target ---
ok  github.com/vladimirvkhs/raxd/internal/keystore   1.196s
ok  github.com/vladimirvkhs/raxd/internal/server     4.253s
ok  github.com/vladimirvkhs/raxd/internal/mcp        2.530s
```

Все тесты зелёные, `skip`/`t.Skip` отсутствуют.

## Безопасность

- **Аутентификация ключей `crypto/rand` + `sha256(key+salt)` + salt** — выполняется в
  `internal/keystore` (реализовано в задаче `key-management`). MCP-слой к keystore не
  обращается (SR-28): аутентификация происходит в `authMiddleware` (`internal/server/auth.go`)
  до передачи запроса в MCP-handler.

- **Сравнение секретов constant-time** — `hmac.Equal` в `internal/server/auth.go`
  (`verifyKey`). MCP-слой сравнений не производит.

- **`exec.Command` без shell-интерполяции** — не применяется в данной задаче (MCP-сервер
  не выполняет системных команд; ping/server_info — только чтение данных).

- **Аудит-лог каждого действия** — `withAudit` в `internal/mcp/audit.go` пишет
  `AuditRecord` на каждый `tools/call` с timestamp, fingerprint, remote, tool, result.
  Транспортный аудит (`authMiddleware`) логирует каждый входящий запрос до MCP-слоя.
  SR-21 соблюдён: ключевое тело, Authorization-заголовок, хэш, соль, приватный TLS-ключ
  не попадают в лог ни в одном поле.

- **Права файлов секретов `0600`** — применяется к TLS-ключу (`internal/server/tls.go`)
  и `keys.db` (`internal/keystore`). MCP-слой файлов не создаёт.

- **SR-28 — нет второго канала аутентификации** — `internal/mcp` не импортирует
  `internal/keystore`. Проверяется статически тестом `TestMCPPackageDoesNotImportKeystore`.

- **SR-33 — нет секретов в ответах инструментов** — `ping` возвращает `"pong"`,
  `server_info` возвращает только публичные поля `{name, version, protocolVersion}`.
  Проверяется тестом `TestMCPServerInfoNoSecrets`.

- **SR-35/SR-36 — fingerprint в аудите, не ключевое тело** — `withAudit` использует
  исключительно `server.FingerprintFromContext(ctx)` (12-hex); ключевое тело недоступно
  на уровне MCP-пакета. Проверяется тестами `TestMCPAuditHasFingerprintAndTool` и
  `TestFingerprintFromContext`.
