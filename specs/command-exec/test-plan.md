# Test Plan: command-exec — MCP-инструмент `execute_command`

Автор: qa (raxd). Дата: 2026-05-22. Язык: русский.
Автор продукта: Vladimir Kovalev, OEM TECH.

Входные документы: spec.md (AC1–AC18), security-requirements.md (SR-40..SR-67),
plan.md, threat-model.md, ADR-001..ADR-004, impl-notes.md,
существующие тесты в `internal/cmdexec/*_test.go`, `internal/mcp/exec_tool_test.go`,
`internal/mcp/mcp_*_test.go`, `internal/server/*_test.go`.

## Стратегия

- **Unit** — `internal/cmdexec`: чистый раннер без MCP/HTTP. Тестирует shell-безопасность,
  allowlist, env-whitelist, cwd-валидацию, лимиты вывода, kill-группы. Без зависимостей от сети.
- **Integration** — `internal/mcp/exec_tool_test.go`: MCP-стек через httptest (без TLS).
  Проверяет полный поток handler → cmdexec → ExecOutput, аудит exec-записи, error-mapping.
- **Integration/Transport** — `internal/server/`: TLS-стек, auth, rate-limit — наследуемые
  требования (SR-41, SR-42), убеждаемся что execute_command сидит за этим периметром.
- **Install-flow** — вне scope этой задачи (command-exec не вводит install.sh).
- **Docker-only** — все тесты прогоняются ТОЛЬКО в контейнере (`baseline §6/AC18/SR-67`).

Команды запуска (только в Docker):
```
# Полный прогон + race:
docker build --target test -t raxd-test . && docker run --rm raxd-test

# Только cmdexec (unit):
docker run --rm raxd-test sh -c "go test -v -count=1 ./internal/cmdexec/..."

# Только mcp (integration):
docker run --rm raxd-test sh -c "go test -v -count=1 ./internal/mcp/..."

# С race-детектором:
docker run --rm raxd-test sh -c \
  "CGO_ENABLED=1 go test -race -count=1 ./internal/cmdexec/... ./internal/mcp/..."
```

## Матрица AC → тест

| AC | Описание (кратко) | Уровень | Тест(ы) (файл::функция) | Статус |
|---|---|---|---|---|
| AC1 | execute_command в tools/list рядом с ping/server_info | integration | `exec_tool_test.go::TestExecToolInToolsList`; `mcp_security_test.go::TestToolsListSchemas` | green |
| AC2 | без shell-инъекции; метасимволы — литеральные аргументы | unit+integration | `exec_test.go::TestNoShellInjection`, `::TestNoShellPipeInjection`; `exec_tool_test.go::TestExecNoShellInjectionViaMCP` (с os.Stat-проверкой) | green |
| AC3 | command/args/timeout_ms/cwd; лишнее поле → isError; env нет | integration | `exec_tool_test.go::TestExecExtraFieldRejected`, `::TestExecUnknownFieldRejected` | green |
| AC4 | 7 полей вывода; ненулевой exit — не isError | unit+integration | `exec_test.go::TestResultFields`, `::TestNonZeroExitCodeNotError`; `exec_tool_test.go::TestExecOutputHas7Fields`, `::TestExecNonZeroExitNotError` | green |
| AC5 | таймаут: timed_out:true; timeout>max → isError | unit+integration | `exec_test.go::TestTimeoutKillsProcess`; `exec_tool_test.go::TestExecTimeoutKills`, `::TestExecTimeoutExceedsMaxIsError` | green |
| AC6 | отмена ctx → kill группы, нет осиротевших процессов | unit+**QA-добавлен** | `exec_test.go::TestContextCancelKillsProcessGroup`; **`exec_qa_test.go::TestContextCancelKillsChildren`** (проверяет реальную гибель дочернего PID) | green (базовый) / **QA добавил строгую проверку** |
| AC7 | allowlist: строгое сопоставление; вне списка → isError/deny | unit+integration | `exec_test.go::TestAllowlistDenyNotInList`, `::TestAllowlistPermitInList`, `::TestAllowlistDisabledAllowsAll`, `::TestAllowlistStrictMatch`; `exec_tool_test.go::TestExecAllowlistDeny`, `::TestExecAllowlistPermit` | green |
| AC8 | несуществующий бинарь → isError, сервер жив; ErrDot | unit+integration | `exec_test.go::TestNonExistentBinaryReturnsError`, `::TestRelativePathBinaryRejected`; `exec_tool_test.go::TestExecNonExistentBinary` | green |
| AC9 | нет повышения привилегий; root euid==0 → WARN-аудит | unit+**QA-добавлен** | `exec_test.go::TestInheritedUID`; **`exec_qa_test.go::TestRootWarnAuditRecord`** (unit-тест логики writeAudit с Result:"warn") | green (UID) / **QA добавил WARN-аудит** |
| AC10 | env-whitelist; cwd-валидация; DefaultCwd при пустом | unit+integration | `exec_test.go::TestEnvWhitelistBlocksDangerousVars`, `::TestEnvWhitelistOnlyContainsAllowedVars`, `::TestInvalidCwdReturnsError`, `::TestCwdIsFile`, `::TestDefaultCwdUsedWhenEmpty`; `exec_tool_test.go::TestExecEnvWhitelist`, `::TestExecInvalidCwdIsError` | green |
| AC11 | лимиты вывода → truncated:true; лимиты входа max_args/max_arg_len | unit+integration | `exec_test.go::TestOutputTruncatedAtLimit`, `::TestOutputNotTruncatedWhenUnderLimit`, `::TestCappedWriterDoesNotOOM`; `exec_tool_test.go::TestExecOutputTruncatedViaMCP`, `::TestExecTooManyArgsIsError`, `::TestExecArgTooLongIsError` | green |
| AC12 | auth наследуется: без Bearer → 401/403 до инструмента; ErrCorrupt → 403 ДО инструмента | integration+**QA-добавлен** | `exec_tool_test.go::TestExecRateLimitInherited` (401 без ключа); **`exec_qa_test.go::TestExecKeystoreCorruptReturns403`** (403 при corrupt keys.db) | green |
| AC13 | аудит каждого вызова: timestamp/fp/command/args/exit_code/duration/remote/result | integration+**QA-добавлен** | `exec_tool_test.go::TestExecAuditContainsRequiredFields`, `::TestExecAuditDenyContainsCommandArgs`; **`exec_qa_test.go::TestExecAuditExactlyOneRecord`** (ровно одна запись/вызов) | green (поля) / **QA добавил счёт записей** |
| AC14 | машиночитаемый logfmt-формат; не-exec записи не ломаются | integration+**QA-добавлен** | `exec_tool_test.go::TestExecAuditDoesNotBreakNonExecFormat`; **`exec_qa_test.go::TestExecAuditLogfmtParseable`** (парсинг logfmt-записи) | green (регрессия) / **QA добавил logfmt-тест** |
| AC15 | ключ raxd не подстрока аудита и ответа | integration | `exec_tool_test.go::TestExecNoKeyInAuditOrResponse`; `mcp_security_test.go::TestNoSecretsInMCPResponsesAndAuditLog` | green |
| AC16 | rate-limit 429 ДО исполнения | integration+**QA-добавлен** | `exec_tool_test.go::TestExecRateLimitInherited` (только 401); **`exec_qa_test.go::TestExecRateLimit429BeforeCommand`** (РЕАЛЬНЫЙ 429 через полный TLS-стек, RateLimit=1 RateBurst=1; RATE в аудите без tool=execute_command result=ok) | green (401) / **QA добавил реальный 429** |
| AC17 | некорректный JSON-RPC → корректная ошибка, без паники/501; неверные параметры | integration | `mcp_security_test.go::TestInvalidJSONReturnsParseError`, `::TestUnknownToolNotExecuted` | green |
| AC18 | зелёные в Docker, -mod=vendor, без go mod download | Docker CI | все тесты в Docker | green |

**Итог по AC: покрыты все 18 AC. Пробелы (AC6, AC9, AC12, AC13, AC14, AC16) закрыты QA-тестами (после qa-guardian раунда 1: AC16 — реальный 429, AC12 — добавлен TestExecKeystoreCorruptReturns403, F-3 — проверка отсутствия exec-записи при отклонении).**

## Матрица ключевых SR-40..SR-67 → тест

| SR | Суть | Тест(ы) | Статус |
|---|---|---|---|
| SR-40 | execute_command только через MCP, нет отдельного эндпоинта | `TestExecToolInToolsList`, `TestToolsListSchemas` | green |
| SR-41 | аутентификация ДО инструмента (401 без Bearer) | `TestExecRateLimitInherited` | green |
| SR-42 | rate-limit 429 ДО исполнения | **`TestExecRateLimit429BeforeCommand`** (полный TLS-стек, startMCPServerWithRateLimit, burst=1) | **QA добавил реальный 429** |
| SR-43 | нет sh -c; метасимволы буквально; grep нет sh в коде | `TestNoShellInjection`, `TestNoShellPipeInjection`, `TestExecNoShellInjectionViaMCP` | green |
| SR-44 | ErrDot отвергается; PATH не от клиента | `TestRelativePathBinaryRejected` | green |
| SR-45 | несуществующий бинарь → нейтральная ошибка, без паники | `TestNonExistentBinaryReturnsError`, `TestExecNonExistentBinary` | green |
| SR-46 | таймаут через context; max_timeout → isError; kill+timed_out | `TestTimeoutKillsProcess`, `TestExecTimeoutKills`, `TestExecTimeoutExceedsMaxIsError` | green |
| SR-47 | process-group kill: потомки убиты при таймауте/отмене | `TestContextCancelKillsProcessGroup`; **`TestContextCancelKillsChildren`** | **QA усилил** |
| SR-48 | allowlist строгое точное; регистр/пробел не совпадает | `TestAllowlistDenyNotInList`, `TestAllowlistPermitInList`, `TestAllowlistStrictMatch`, `TestExecAllowlistDeny` | green |
| SR-49 | env-whitelist: LD_PRELOAD/DYLD_*/IFS не в дочернем | `TestEnvWhitelistBlocksDangerousVars`, `TestEnvWhitelistOnlyContainsAllowedVars`; **`TestEnvWhitelistBlocksLdLibraryPath`** | **QA добавил LD_LIBRARY_PATH** |
| SR-50 | cwd валидируется os.Stat+IsDir; невалид → isError | `TestInvalidCwdReturnsError`, `TestCwdIsFile`, `TestExecInvalidCwdIsError` | green |
| SR-51 | additionalProperties:false; поле env → isError | `TestExecExtraFieldRejected`, `TestExecUnknownFieldRejected` | green |
| SR-52 | max_args/max_arg_len ДО запуска → isError | `TestExecTooManyArgsIsError`, `TestExecArgTooLongIsError` | green |
| SR-53 | capped-writer: лимит вывода + truncated + дренаж | `TestOutputTruncatedAtLimit`, `TestCappedWriterDoesNotOOM`, `TestExecOutputTruncatedViaMCP` + cappedwriter_test.go | green |
| SR-54 | Credential не задаётся; uid наследуется | `TestInheritedUID` | green |
| SR-55 | euid==0 → WARN-аудит при КАЖДОМ вызове | **`TestRootWarnAuditRecord`** (writeAudit напрямую = unit-уровень; при euid==0 в Docker также проверяет реальный execHandler-путь) | **QA добавил** |
| SR-56 | deny_root=true + euid==0 → isError | `TestExecDenyRootConfigField` (негативный путь: non-root); **`TestDenyRootUnitLogic`** | **QA добавил unit** |
| SR-57 | ровно одна exec-запись/вызов; deny тоже пишет | **`TestExecAuditExactlyOneRecord`** | **QA добавил** |
| SR-58 | поля success: fp+command+args+exit_code+duration+remote+result | `TestExecAuditContainsRequiredFields` | green |
| SR-59 | exec-поля ТОЛЬКО для execute_command; не-exec записи неизменны | `TestExecAuditDoesNotBreakNonExecFormat` | green |
| SR-60 | logfmt строго парсимый | **`TestExecAuditLogfmtParseable`** | **QA добавил** |
| SR-62 | ключ raxd не в аудите и не в ответе | `TestExecNoKeyInAuditOrResponse`, `TestNoSecretsInMCPResponsesAndAuditLog` | green |
| SR-63 | args в аудите дословно (success-ветка) | `TestExecAuditDenyContainsCommandArgs` (deny); **`TestExecAuditArgsVerbatimInSuccess`** | **QA добавил success** |
| SR-64 | невалидный ввод → isError/JSON-RPC error, не паника/501 | `TestExecNonExistentBinary`, `TestInvalidJSONReturnsParseError` | green |
| SR-65 | 7 полей ExecOutput | `TestExecOutputHas7Fields` | green |
| SR-66 | конфиг-дефолты применяются | `TestDefaultCwdUsedWhenEmpty` (косвенно); **`TestExecConfigDefaults`** | **QA добавил** |
| SR-67 | прогон в Docker офлайн vendor | docker build/run (AC18) | green |

## Edge cases

| Вектор | Тест | Статус |
|---|---|---|
| Shell-метасимволы: `;`, `\|`, `$()`, `&&`, `>`, `` ` `` | `TestNoShellInjection`, `TestNoShellPipeInjection` | green |
| ErrDot — относительный путь `./binary` | `TestRelativePathBinaryRejected` | green |
| cwd = файл (не директория) | `TestCwdIsFile` | green |
| Несуществующий cwd | `TestInvalidCwdReturnsError` | green |
| Нулевой лимит cappedWriter | `TestCappedWriterZeroLimit` | green |
| Запись после заполнения cappedWriter | `TestCappedWriterWriteAfterFull` | green |
| LD_PRELOAD в окружении демона → дочерний не получает | `TestEnvWhitelistBlocksDangerousVars` | green |
| LD_LIBRARY_PATH в окружении → не передаётся | **`TestEnvWhitelistBlocksLdLibraryPath`** | **QA** |
| Регистр allowlist: "Echo" ≠ "echo" | `TestAllowlistStrictMatch` | green |
| Лишний пробел в allowlist: " echo" ≠ "echo" | `TestAllowlistStrictMatch` | green |
| Ненулевой exit — не isError | `TestNonZeroExitCodeNotError`, `TestExecNonZeroExitNotError` | green |
| Таймаут → timed_out:true, не isError | `TestTimeoutKillsProcess`, `TestExecTimeoutKills` | green |
| timeout_ms > max_timeout_ms → isError | `TestExecTimeoutExceedsMaxIsError` | green |
| Дочерние процессы убиты после cancel | `TestContextCancelKillsProcessGroup`; **`TestContextCancelKillsChildren`** | **QA усилил** |
| Большой вывод > 1 MiB → truncated, нет OOM | `TestOutputTruncatedAtLimit`, `TestCappedWriterDoesNotOOM` | green |

## Security-тесты

| Вектор безопасности | Тест | SR | Статус |
|---|---|---|---|
| Shell-инъекция через MCP — маркер-файл не создан | `TestExecNoShellInjectionViaMCP` | SR-43 | green (после исправления фальш-зелёного) |
| Shell-инъекция через юнит | `TestNoShellInjection`, `TestNoShellPipeInjection` | SR-43 | green |
| Без Bearer → 401, команда не запускается | `TestExecRateLimitInherited` | SR-41 | green |
| Rate-limit 429 ДО execute_command | **`TestExecRateLimit429BeforeCommand`** | SR-42 | **QA добавил** |
| LD_PRELOAD не в дочернем | `TestEnvWhitelistBlocksDangerousVars` | SR-49 | green |
| LD_LIBRARY_PATH не в дочернем | **`TestEnvWhitelistBlocksLdLibraryPath`** | SR-49 | **QA добавил** |
| IFS не в дочернем | `TestEnvWhitelistBlocksDangerousVars` | SR-49 | green |
| DYLD_INSERT_LIBRARIES не в дочернем | `TestEnvWhitelistBlocksDangerousVars` | SR-49 | green |
| cwd = несуществующий → isError ДО запуска | `TestInvalidCwdReturnsError` | SR-50 | green |
| Лишнее поле env в запросе → isError | `TestExecExtraFieldRejected` | SR-51 | green |
| max_args превышен → isError+DENY, не запущено | `TestExecTooManyArgsIsError` | SR-52 | green |
| max_arg_len превышен → isError+DENY | `TestExecArgTooLongIsError` | SR-52 | green |
| root WARN-аудит при euid==0 (unit writeAudit) | **`TestRootWarnAuditRecord`** | SR-55 | **QA добавил** |
| deny_root=true + euid==0 → isError | **`TestDenyRootUnitLogic`** | SR-56 | **QA добавил** |
| Ровно одна exec-запись/вызов | **`TestExecAuditExactlyOneRecord`** | SR-57 | **QA добавил** |
| Ключ raxd не в аудите и не в MCP-ответе | `TestExecNoKeyInAuditOrResponse` | SR-62 | green |
| Corrupt keys.db → 403 ДО execute_command | **`TestExecKeystoreCorruptReturns403`** | SR-27/AC12 | **QA добавил** |
| Args в аудите дословно (success-ветка) | **`TestExecAuditArgsVerbatimInSuccess`** | SR-63 | **QA добавил** |
| process-group kill: дочерние процессы убиты | **`TestContextCancelKillsChildren`** | SR-47 | **QA добавил** |
| logfmt exec-запись парсируется как key=value | **`TestExecAuditLogfmtParseable`** | SR-60 | **QA добавил** |

## Добавленные QA-тесты

Файл: `internal/cmdexec/exec_qa_test.go` — unit-тесты раннера:
- `TestContextCancelKillsChildren` — AC6/SR-47: проверяет что дочерний PID действительно мёртв после cancel
- `TestEnvWhitelistBlocksLdLibraryPath` — SR-49: LD_LIBRARY_PATH не в дочернем процессе

Файл: `internal/mcp/exec_qa_test.go` — integration-тесты MCP-стека:
- `TestExecAuditExactlyOneRecord` — AC13/SR-57: ровно одна exec-запись за вызов
- `TestExecAuditLogfmtParseable` — AC14/SR-60: exec-запись парсится как logfmt
- `TestExecAuditArgsVerbatimInSuccess` — SR-63: args в success-аудите дословно
- `TestExecRateLimit429BeforeCommand` — AC16/SR-42: РЕАЛЬНЫЙ 429 через полный TLS-стек (startMCPServerWithRateLimit, burst=1, без фальш-зелёного)
- `TestRootWarnAuditRecord` — AC9/SR-55: unit-тест логики WARN-аудита при euid==0 (writeAudit напрямую); при euid==0 (Docker) также проверяет реальный execHandler-путь
- `TestDenyRootUnitLogic` — SR-56: deny_root=true + euid==0 через writeAudit
- `TestExecConfigDefaults` — SR-66: конфиг-дефолты применяются
- `TestExecKeystoreCorruptReturns403` — AC12/SR-27: corrupt keys.db → 403 ДО execute_command

Файл: `internal/mcp/exec_tool_test.go` — усиления существующих тестов (F-3):
- `TestExecExtraFieldRejected` — добавлена проверка что нет exec-записи result=ok в аудите
- `TestExecUnknownFieldRejected` — добавлена проверка что нет exec-записи result=ok в аудите

Dockerfile (F-6): добавлен `./internal/cmdexec/...` в race-цель.

## Реальный результат Docker-прогона (AC18)

Прогон: `docker build --target test -t raxd-test . && docker run --rm raxd-test`
Дата: 2026-05-22. Все пакеты зелёные.

```
go vet ./...   — чист (0 ошибок)

go test -v -count=1 ./...
ok  github.com/vladimirvkhs/raxd                    0.005s
ok  github.com/vladimirvkhs/raxd/internal/banner    0.001s
ok  github.com/vladimirvkhs/raxd/internal/cli       0.069s
ok  github.com/vladimirvkhs/raxd/internal/cmdexec   1.179s
ok  github.com/vladimirvkhs/raxd/internal/config    0.003s
ok  github.com/vladimirvkhs/raxd/internal/keystore  0.143s
ok  github.com/vladimirvkhs/raxd/internal/mcp       1.473s
ok  github.com/vladimirvkhs/raxd/internal/server    2.187s
ok  github.com/vladimirvkhs/raxd/internal/version   0.001s

CGO_ENABLED=1 go test -race -count=1 ./internal/cmdexec/... ./internal/keystore/... ./internal/server/... ./internal/mcp/...
ok  github.com/vladimirvkhs/raxd/internal/cmdexec   2.174s
ok  github.com/vladimirvkhs/raxd/internal/keystore  1.246s
ok  github.com/vladimirvkhs/raxd/internal/server    3.922s
ok  github.com/vladimirvkhs/raxd/internal/mcp       5.275s
```

Ключевые QA-тесты (результат в Docker от root/euid==0):
```
=== RUN   TestExecRateLimit429BeforeCommand
    exec_qa_test.go:463: AC16/SR-42: получен 429 на попытке 1
    exec_qa_test.go:495: AC16/SR-42: OK — реальный 429 получен; execute_command не вызван после сброса буфера
    exec_qa_test.go:502: AC16/SR-42: OK — RATE-запись присутствует в аудите
--- PASS: TestExecRateLimit429BeforeCommand (0.03s)
=== RUN   TestExecKeystoreCorruptReturns403
    exec_qa_test.go:866: AC12/SR-27: OK — corrupt keys.db → HTTP 403
    exec_qa_test.go:878: AC12/SR-27: OK — tool=execute_command не найден в аудите (MCP не достигнут)
--- PASS: TestExecKeystoreCorruptReturns403 (0.03s)
```

**AC18: ВЕРИФИЦИРОВАН.** Все тесты зелёные в Docker, `-mod=vendor`, race чист. Баги не найдены.

## Найденные пробелы до добавления тестов

1. **AC6/SR-47 (process-group kill)** — `TestContextCancelKillsProcessGroup` возвращает управление после cancel, но не проверяет что дочерний PID физически мёртв. Добавлен `TestContextCancelKillsChildren` с проверкой через `os.FindProcess` + `Signal(0)`.

2. **AC9/SR-55 (root WARN)** — единственный тест `TestExecDenyRootConfigField` проверяет только non-root путь. Нет теста самой WARN-логики `writeAudit` с `Result:"warn"`. Добавлен `TestRootWarnAuditRecord` на уровне `writeAudit`.

3. **AC13/SR-57 (ровно одна запись/вызов)** — тесты проверяют наличие полей, но не считают число exec-записей. Добавлен `TestExecAuditExactlyOneRecord` с `strings.Count(log, "tool=execute_command")`.

4. **AC14/SR-60 (logfmt парсимость)** — нет теста что exec-запись является структурной logfmt (ключи извлекаются). Добавлен `TestExecAuditLogfmtParseable`.

5. **AC16/SR-42 (rate-limit 429)** — `TestExecRateLimitInherited` проверяет 401 (отсутствие auth), но не 429 при превышении лимита. Изначально добавлен `TestExecRateLimit429BeforeCommand` через httptest без rateLimitMiddleware — **фальш-зелёный (qa-guardian F-2)**. Переписан на полный TLS-стек через `startMCPServerWithRateLimit(t, 1, 1)` — РЕАЛЬНЫЙ 429 подтверждён.

6. **SR-49 (LD_LIBRARY_PATH)** — `TestEnvWhitelistBlocksDangerousVars` проверяет `LD_PRELOAD`, `IFS`, `DYLD_INSERT_LIBRARIES`, но не `LD_LIBRARY_PATH`. Добавлен `TestEnvWhitelistBlocksLdLibraryPath`.

7. **SR-63 (args дословно в success-аудите)** — проверялось только в deny-ветке. Добавлен `TestExecAuditArgsVerbatimInSuccess` для success-пути.

8. **AC12 (ErrCorrupt → 403 ДО execute_command)** — `TestMCPKeystoreCorruptReturns403` из mcp_security_test.go проверяет только инструмент `ping`. Не было аналогичного теста для `execute_command`. Добавлен `TestExecKeystoreCorruptReturns403` (qa-guardian F-1).

9. **AC3/F-3 (лишние поля → команда не запущена)** — `TestExecExtraFieldRejected`/`TestExecUnknownFieldRejected` не проверяли что execute_command не был вызван. Добавлены проверки аудита (нет exec-записи result=ok).

## Найденные баги (если есть)

Баги не обнаружены. Код реализован строго по плану. Developer-guardian (раунд 2) уже устранил:
- Фальш-зелёный `TestExecNoShellInjectionViaMCP` (теперь с `os.Stat`-проверкой).
- Root-WARN разделён семантически на `Result:"warn"` (детекция) и `Result:"deny"` (при deny_root=true).
- `remote=` поле добавлено в тест аудита.
- Dead code `pathVal` удалён.

## Как запускать (только в Docker)

```bash
# Сборка тест-образа:
docker build --target test -t raxd-test .

# Полный прогон (vet + все тесты + race на ключевых пакетах):
docker run --rm raxd-test

# Только новые QA-тесты (unit cmdexec):
docker run --rm raxd-test sh -c \
  "go test -v -count=1 -run 'TestContextCancelKillsChildren|TestEnvWhitelistBlocksLdLibraryPath' ./internal/cmdexec/..."

# Только новые QA-тесты (integration mcp):
docker run --rm raxd-test sh -c \
  "go test -v -count=1 -run 'TestExecAuditExactlyOneRecord|TestExecAuditLogfmtParseable|TestExecAuditArgsVerbatimInSuccess|TestExecRateLimit429BeforeCommand|TestRootWarnAuditRecord|TestDenyRootUnitLogic|TestExecConfigDefaults' ./internal/mcp/..."

# Race на cmdexec + mcp:
docker run --rm raxd-test sh -c \
  "CGO_ENABLED=1 go test -race -count=1 ./internal/cmdexec/... ./internal/mcp/..."
```

Примечание: `raxd serve` и прогон команд выполняются ТОЛЬКО в контейнере (baseline §6/AC18/SR-67).
На хосте тесты не запускаются.
