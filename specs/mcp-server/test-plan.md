# Test Plan: mcp-server

QA: qa-role (raxd). Задача: `mcp-server`. Ветка: `feature/mcp-server`.
Все тесты прогоняются ТОЛЬКО в Docker (`SECURITY-BASELINE.ru.md §6`), офлайн из `vendor/`.

---

## Стратегия

- **Unit (httptest, без TLS)** — изолированная проверка `internal/mcp.NewHandler` через
  `httptest.NewServer`: без auth-цепочки, напрямую против MCP-handler. Цель: протокол MCP
  (initialize, ping, server_info, GET→405, аудит-декоратор withAudit, импорт-граф SR-28).

- **Integration (полный сервер, TLS)** — `server.New(..., mcpHandler)` + реальный TLS 1.3 +
  middleware-цепочка (auth, Origin, rate-limit) + MCP. `httptest` TLS не используется — запускаем
  настоящий listener на свободном порту (port 0) внутри контейнера.
  Цель: AC2/AC8/AC12 (auth/Origin до MCP), AC9/AC10 (аудит+no-secrets), AC11 (один порт/TLS),
  MEDIUM-1 (реальный key.pem), MEDIUM-2 (Origin-поведение).

- **Race (-race + CGO_ENABLED=1)** — конкурентные `tools/call` под детектором гонок (SR-39/R-M7).

- **Статический (grep по источникам)** — SR-28: `internal/mcp` не импортирует `internal/keystore`;
  нет жёстко прошитых секретов в коде.

- **Install-flow** — в scope задачи `distribution`, не `mcp-server`; здесь out of scope (AC15 —
  docs, а не install-скрипт).

---

## Команды запуска (только в Docker)

```bash
# 1. Сборка образа + базовый прогон (vet + test + race):
docker build --target test -t raxd-test . && docker run --rm raxd-test

# 2. go vet (отдельно):
docker run --rm raxd-test sh -c "go vet -mod=vendor ./... && echo 'vet: ok'"

# 3. go test все пакеты:
docker run --rm raxd-test sh -c "go test -mod=vendor -count=1 ./..."

# 4. Race-прогон (CGO=1, internal/server + internal/mcp):
docker run --rm raxd-test sh -c \
  "CGO_ENABLED=1 go test -mod=vendor -race -count=1 ./internal/server/... ./internal/mcp/..."
```

---

## Матрица AC → тест

| AC | Описание | Уровень | Файл::Тест | Статус |
|---|---|---|---|---|
| AC1 | POST /mcp → корректный JSON-RPC (не 501); GET /mcp → 405 | integration | `internal/mcp/mcp_test.go::TestMCPGetReturns405` | green |
| AC1 | GET /mcp → 405 (unit, httptest) | unit | `mcp_test.go::TestNewHandlerGetReturns405` | green |
| AC2 | Без auth Bearer → 401 до MCP | integration | `mcp_test.go::TestMCPNoAuthReturns401` | green |
| AC2 | Неизвестный ключ → 401 до MCP | integration | `mcp_test.go::TestMCPUnknownKeyReturns401` | green |
| AC3 | initialize → capabilities+serverInfo(name=raxd,version)+protocolVersion | integration | `mcp_test.go::TestMCPInitializeCapabilities` | green |
| AC3 | initialize → только tools в capabilities (нет resources/prompts) | integration | `mcp_security_test.go::TestInitializeCapabilitiesOnlyTools` | green |
| AC3 | initialize (unit, httptest) | unit | `mcp_test.go::TestNewHandlerInitializeViaHTTPTest` | green |
| AC4 | tools/list → ровно ping + server_info, не execute_command | integration | `mcp_test.go::TestMCPToolsList` | green |
| AC4 | tools/list → inputSchema.type=object у обоих; не execute_command | integration | `mcp_security_test.go::TestToolsListSchemas` | green |
| AC5 | tools/call ping → pong, isError=false | integration | `mcp_test.go::TestMCPCallPingReturnsPong` | green |
| AC5 | tools/call ping (unit, httptest) | unit | `mcp_test.go::TestNewHandlerPingViaHTTPTest` | green |
| AC6 | tools/call server_info → {name,version,protocolVersion}, isError=false | integration | `mcp_test.go::TestMCPCallServerInfo` | green |
| AC6 | server_info → structuredContent ровно 3 поля, нет forbidden | integration | `mcp_security_test.go::TestServerInfoExactFields` | green |
| AC6 | server_info (unit, httptest) | unit | `mcp_test.go::TestNewHandlerServerInfoViaHTTPTest` | green |
| AC7 | Неизвестный инструмент → JSON-RPC error, сервер жив | integration | `mcp_test.go::TestMCPUnknownToolReturnsError` | green |
| AC7 | execute_command → -32602/-32601, сервер жив (SR-37) | integration | `mcp_security_test.go::TestUnknownToolNotExecuted` | green |
| AC7 | Невалидный JSON → error/-32700 или HTTP 400, сервер жив | integration | `mcp_security_test.go::TestInvalidJSONReturnsParseError` | green |
| AC8 | Без auth → 401, неизвестный ключ → 401 (до MCP) | integration | `mcp_test.go::TestMCPNoAuthReturns401`, `TestMCPUnknownKeyReturns401` | green |
| AC9 | tools/call ping → аудит содержит tool=ping + fp= (не тело ключа) | integration | `mcp_test.go::TestMCPAuditHasFingerprintAndTool` | green |
| AC9 | Ровно 1 MCP-запись + 1 AUTH на один tools/call | integration | `mcp_security_test.go::TestMCPAuditExactRecordsPerToolsCall` | green |
| AC9 | Аудит withAudit: tool= и fp= (unit, httptest) | unit | `mcp_test.go::TestNewHandlerAuditContainsToolAndFP` | green |
| AC10 | key.pem (реальное содержимое) не встречается в ответах и логе | integration | `mcp_security_test.go::TestNoSecretsInMCPResponsesAndAuditLog` | green |
| AC10 | API-ключ не встречается в ответе server_info и логе | integration | `mcp_test.go::TestMCPServerInfoNoSecrets` | green |
| AC10 | Не-MCP AUTH-запись не содержит tool= | integration | `mcp_test.go::TestMCPAuditNonMCPNoToolField` | green |
| AC11 | MCP доступен на том же порту/TLS после server.New | integration | все mcp_test.go (startMCPServer использует server.New с mcpH) | green |
| AC12 | Invalid Origin → 403 до MCP (нет tool= в логе) | integration | `mcp_test.go::TestMCPInvalidOriginReturns403` | green |
| AC12 | Invalid Origin → 403, no tool= (MEDIUM-2) | integration | `mcp_security_test.go::TestOriginInvalidReturnsForbiddenBeforeMCP` | green |
| AC12 | Absent Origin → 401 (не 403) | integration | `mcp_security_test.go::TestOriginAbsentPassesOriginCheck` | green |
| AC12 | Valid Origin (в allowlist) → 200 | integration | `mcp_security_test.go::TestOriginValidAllowsRequest` | green |
| AC13 | tools/list не содержит execute_command/upload_file | integration | `mcp_test.go::TestMCPToolsList` (проверяет !names["execute_command"]) | green |
| AC14 | Все тесты зелёные в Docker -mod=vendor | Docker | все команды выше | green |
| AC15 | Документация подключения (URL, Bearer, self-signed) | docs | tech-writer (out of scope для qa) | — |

---

## Матрица SR → тест (ключевые SR-27..SR-39)

| SR | Описание | Файл::Тест | Статус |
|---|---|---|---|
| SR-27 | Auth до MCP: нет/неизвестный/отозванный → 401; ErrCorrupt → 403 | `mcp_test.go::TestMCPNoAuthReturns401`, `TestMCPUnknownKeyReturns401`; `mcp_security_test.go::TestMCPKeystoreCorruptReturns403` | green |
| SR-28 | internal/mcp не импортирует keystore (статич.) | `mcp_test.go::TestMCPPackageDoesNotImportKeystore` | green |
| SR-29 | MCP за тем же server.New (тест с полным сервером) | все integration-тесты `startMCPServer` | green |
| SR-30 | Невалидный JSON → error; неизвестный инструмент → -32601/-32602; GET → 405 | `mcp_security_test.go::TestInvalidJSONReturnsParseError`, `TestUnknownToolNotExecuted`, `mcp_test.go::TestMCPGetReturns405` | green |
| SR-31 | initialize/tools.list/tools.call по контракту (capabilities, schemas, pong) | `TestMCPInitializeCapabilities`, `TestMCPToolsList`, `TestMCPCallPingReturnsPong`, `TestInitializeCapabilitiesOnlyTools`, `TestToolsListSchemas` | green |
| SR-32 | Origin present&invalid → 403 до MCP; absent → проходит; valid → проходит | `mcp_security_test.go::TestOriginInvalidReturnsForbiddenBeforeMCP`, `TestOriginAbsentPassesOriginCheck`, `TestOriginValidAllowsRequest` | green |
| SR-33 | server_info возвращает только {name,version,protocolVersion} без секретов | `mcp_security_test.go::TestServerInfoExactFields` | green |
| SR-34 | key.pem (реальное содержимое) + API-ключ не в ответах и логе | `mcp_security_test.go::TestNoSecretsInMCPResponsesAndAuditLog` | green |
| SR-35 | withAudit: реальный (не fp=-) fingerprint + tool= в каждой MCP-записи | `mcp_test.go::TestMCPAuditHasFingerprintAndTool` (усилен: fp≠- + hex), `TestNewHandlerAuditContainsToolAndFP`; `mcp_security_test.go::TestMCPAuditExactRecordsPerToolsCall` (усилен: fp≠- + hex) | green |
| SR-36 | AuditRecord.Tool; writeAudit: MCP-успех → label MCP + tool=; не-MCP → AUTH без tool= | `audit_mcp_test.go::TestAuditRecordToolField`, `TestWriteAuditMCPSuccessLabel`, `TestWriteAuditNonMCPUnchanged` | green |
| SR-37 | execute_command не зарегистрирован; tools/call → error не исполнение | `mcp_security_test.go::TestUnknownToolNotExecuted`, `mcp_test.go::TestMCPToolsList` | green |
| SR-38 | vendor/ офлайн, -mod=vendor, CGO_ENABLED=0 | Docker build (все тесты компилируются без сети) | green |
| SR-39 | Race-прогон параллельных tools/call без data race | `mcp_test.go::TestMCPConcurrentPing` (под -race) | green |

---

## Edge cases

### Протокол MCP
- Невалидный JSON-тело → JSON-RPC -32700 или HTTP 400; сервер жив (AC7/SR-30).
- Неизвестный инструмент (execute_command) → -32602/-32601, не исполнение (SR-37).
- GET /mcp → 405 (stateless v1, нет SSE).
- notifications/initialized (нотификация, не request) → 202 Accepted без тела.

### Безопасность
- Origin: present&invalid → 403 до MCP (нет tool= в логе); absent → auth 401; valid → OK.
- TLS private key body (реальный key.pem) не встречается в ответах и логе.
- API-ключ (rax_live_...) не встречается в ответах MCP и логе.
- Fingerprint (12 hex) есть в MCP-аудит-записи; тело ключа — нет.
- internal/mcp не импортирует keystore (статическая проверка исходников).

### Аудит
- Ровно 1 MCP-запись на tools/call (withAudit).
- Ровно 1 AUTH-запись от transport (authSuccessAuditMiddleware) на тот же запрос.
- Не-MCP AUTH-запись не содержит tool=.
- tools/call ping пишет result=ok, MCP label.

### Конкурентность
- 10 параллельных tools/call ping под -race → нет data race (SR-39/R-M7).

---

## Security-тесты (SECURITY-BASELINE.ru.md)

| Пункт baseline | Тест |
|---|---|
| §1 Аутентификация до MCP | SR-27: TestMCPNoAuthReturns401, TestMCPUnknownKeyReturns401 |
| §1 Constant-time Verify | выполняется в keystore (наследуется tls-transport SR-10); MCP не сравнивает |
| §2 Origin/Host-защита | SR-32: TestOriginInvalidReturnsForbiddenBeforeMCP, TestOriginAbsentPassesOriginCheck |
| §2 Bind loopback | наследуется tls-transport (server_test.go::TestServerBindsLoopback) |
| §3 exec без shell | не применимо (ping/server_info не выполняют команды) |
| §4 Аудит каждого действия | SR-35/SR-36: TestMCPAuditHasFingerprintAndTool, TestMCPAuditExactRecordsPerToolsCall |
| §4 Никаких секретов в логах | SR-34: TestNoSecretsInMCPResponsesAndAuditLog |
| §6 Только Docker, -mod=vendor | все команды запуска — docker-команды выше |

---

## Состояние t.Skip (LOW-debt)

### Исправлено

`TestServePortInUseNoStartupBlock` и `TestServePortInUseNoShutdownBlock` в
`internal/cli/cli_gaps_test.go` ранее вызывали `t.Skip("port 7822 unavailable for test setup")`
когда 7822 был занят другим процессом.

**Исправление:** введена функция `occupyFreePort(t)` которая:
1. находит свободный порт через `net.Listen("tcp", "127.0.0.1:0")` (OS-assigned, не 7822);
2. создаёт `config.yaml` с этим портом в `$XDG_CONFIG_HOME/raxd/` (temp dir);
3. повторно занимает порт так что `executeCmd("serve")` видит его занятым и завершается ошибкой.

Оба теста теперь детерминированы. Единственный оставшийся `t.Skipf` в `occupyFreePort` —
тривиальная гонка ОС между двумя `net.Listen` (крайне маловероятна в CI), с диагностическим
сообщением. Это не отключение логики теста, а корректная обработка внешнего ресурса.

**Статус t.Skipf в occupyFreePort: LOW-risk, принятый necessary-defensive.**
Ситуация: между первым `net.Listen("tcp","127.0.0.1:0")` (probe, немедленно закрывается) и
вторым `net.Listen(port)` (occupy) ОС теоретически может выдать тот же порт другому процессу.
Вероятность в CI — крайне низкая (порт выбирается ОС из ephemeral range, гонка длиной ~1мс).
Решение принято: `t.Skipf` в этой ситуации корректен — он пропускает тест целиком из-за
проблемы с внешним ресурсом, а не маскирует логику. Логика теста (port-in-use error handling)
остаётся нетронутой.
**Пересмотреть, если тест станет flaky в CI** — перейти к O_EXCL-биндингу или использовать
`SO_REUSEPORT` с проверкой. До появления flakiness изменение не требуется.

Прогон в Docker подтверждён:
- `TestServePortInUseNoStartupBlock` — PASS
- `TestServePortInUseNoShutdownBlock` — PASS

---

## Найденные баги

Продуктовых багов не обнаружено. Все усиленные тесты прошли зелёными:
- MEDIUM-1: ни TLS private key, ни API-ключ не утекают в ответы или лог.
- MEDIUM-2: Origin-поведение соответствует SR-32/ADR-003.

---

## Результаты прогона в Docker

### go vet -mod=vendor ./...
```
vet: ok
```

### go test -mod=vendor -count=1 ./...
```
ok  github.com/vladimirvkhs/raxd                        0.011s
ok  github.com/vladimirvkhs/raxd/internal/banner        0.001s
ok  github.com/vladimirvkhs/raxd/internal/cli           0.072s
ok  github.com/vladimirvkhs/raxd/internal/config        0.003s
ok  github.com/vladimirvkhs/raxd/internal/keystore      0.178s
ok  github.com/vladimirvkhs/raxd/internal/mcp           2.866s
ok  github.com/vladimirvkhs/raxd/internal/server        2.267s
ok  github.com/vladimirvkhs/raxd/internal/version       0.001s
```

### CGO_ENABLED=1 go test -mod=vendor -race -count=1 ./internal/server/... ./internal/mcp/...
```
ok  github.com/vladimirvkhs/raxd/internal/server        3.925s
ok  github.com/vladimirvkhs/raxd/internal/mcp           4.164s
```
