# Impl Notes: command-exec

## Что реализовано

- **`internal/cmdexec`** (config.go, exec.go, cappedwriter.go, sysproc_unix.go) — безопасный раннер
  субпроцессов. Контракт: `Run(ctx context.Context, cfg Config, in Input) (Result, error)`.
  - exec.CommandContext без shell (SR-43/AC2); allowlist до LookPath (SR-48/AC7);
  - явный Cmd.Env из env-whitelist (SR-49/AC10); валидация cwd (SR-50/AC10);
  - Setpgid + killGroup + WaitDelay (SR-47/AC6/ADR-001); CappedWriter stdout/stderr (SR-53/AC11);
  - ErrNotAllowed и ErrBadCwd — sentinel-ошибки для маппинга в handler;
  - ненулевой exit и таймаут — не ошибки, возвращаются в Result.

- **`internal/cmdexec/cappedwriter.go`** — CappedWriter (io.Writer с лимитом). Дренирует
  избыток без возврата ошибки — критично для предотвращения deadlock пайпа (SR-53).

- **`internal/cmdexec/sysproc_unix.go`** (`//go:build unix`) — applyProcessGroup (Setpgid:true),
  killGroup (syscall.Kill(-pgid, SIGKILL)), waitDelay=5s. Credential НЕ задаётся (SR-54).

- **`internal/mcp/exec_tool.go`** — ExecInput/ExecOutput типы (mcp-spec §5.1/§5.2, 7 полей
  вывода), execTool() дескриптор, execHandler(cfg, audit). ADR-004: execHandler пишет свой
  audit во всех ветках (deny/fail/success) — withAudit-обёртка не используется.
  Root-WARN всегда при euid==0; DenyRoot=true → отказ (SR-56/AC3). Лимиты max_args/max_arg_len
  проверяются в handler (SR-52). SR-62: доступ только к fingerprint и remote addr, не к телу ключа.

- **`internal/server/audit.go`** — AuditRecord расширен полями Command, Args []string,
  ExitCode *int, Duration, TimedOut. writeAudit рендерит exec-поля только для
  Tool=="execute_command" (SR-59); non-exec записи не изменены (AUTH/FAIL/DENY/RATE/MCP).

- **`internal/config/config.go`** — ExecConfig struct и Config.Exec; viper-дефолты для всего
  exec.* раздела (SR-66): timeout 30s/300s, cwd=/tmp, whitelist PATH/HOME/LANG/TERM,
  max_args=256, max_arg_len=128KiB, max_output_bytes=1MiB, deny_root=false.

- **`internal/mcp/server.go`** — NewHandler расширен до
  `NewHandler(ver string, audit server.AuditFn, execCfg cmdexec.Config) (http.Handler, error)`.
  execute_command регистрируется через sdkmcp.AddTool без withAudit-обёртки (ADR-004).

- **`internal/cli/serve.go`** — переключён на LogfmtFormatter (F-1/SR-60); сборка execCfg
  из cfg.Exec; передача в internalmcp.NewHandler.

- **`security_static_test.go`** — TestStaticNoExecCommand обновлён: исключает internal/cmdexec
  из запрещённого скана (единственный авторизованный пакет для os/exec; spec AC2).

## Отклонения/эскалации

Нет. Реализация строго по plan.md, spec.md, mcp-spec.md, security-requirements.md.

Единственное нетривиальное уточнение: TestStaticNoExecCommand в security_static_test.go
требовал обновления, т.к. был написан до появления cmdexec. Изменение сохраняет смысл
теста (никакой несанкционированный пакет не использует os/exec) и соответствует spec AC2.

## Тесты

Покрытые acceptance criteria:
- AC1 (execute_command в tools/list): TestExecToolInToolsList, TestMCPToolsList
- AC2/SR-43 (без shell-инъекции): TestNoShellInjection, TestNoShellPipeInjection, TestExecNoShellInjectionViaMCP
- AC3/SR-56 (deny_root): TestExecDenyRootConfigField, ADR-003 в конфиге
- AC4/SR-65 (7 полей вывода): TestExecOutputHas7Fields
- AC5/SR-46 (таймаут): TestTimeoutKillsProcess, TestExecTimeoutKills, TestExecTimeoutExceedsMaxIsError
- AC6/SR-47 (kill группы): TestContextCancelKillsProcessGroup
- AC7/SR-48 (allowlist): TestAllowlistDenyNotInList, TestAllowlistPermitInList, TestExecAllowlistDeny, TestExecAllowlistPermit
- AC8/SR-44/SR-45 (LookPath/ErrDot): TestNonExistentBinaryReturnsError, TestRelativePathBinaryRejected, TestExecNonExistentBinary
- AC9/SR-54 (uid наследование): TestInheritedUID
- AC10/SR-49 (env-whitelist): TestEnvWhitelistBlocksDangerousVars, TestEnvWhitelistOnlyContainsAllowedVars, TestExecEnvWhitelist
- AC10/SR-50 (cwd): TestInvalidCwdReturnsError, TestCwdIsFile, TestExecInvalidCwdIsError
- AC11/SR-53 (лимит вывода): TestOutputTruncatedAtLimit, TestOutputNotTruncatedWhenUnderLimit, TestExecOutputTruncatedViaMCP
- AC13/SR-57 (exec аудит): TestExecAuditContainsRequiredFields, TestExecAuditDenyContainsCommandArgs
- AC14/SR-59 (не-exec формат): TestExecAuditDoesNotBreakNonExecFormat
- AC15/SR-62 (нет ключа в аудите): TestExecNoKeyInAuditOrResponse
- AC16/SR-41 (auth наследована): TestExecRateLimitInherited
- SR-52 (max_args/max_arg_len): TestExecTooManyArgsIsError, TestExecArgTooLongIsError
- CappedWriter: TestCappedWriterUnderLimit, TestCappedWriterExactLimit, TestCappedWriterOverLimit,
  TestCappedWriterMultipleWrites, TestCappedWriterWriteAfterFull, TestCappedWriterZeroLimit

Команда запуска (только в Docker — SECURITY-BASELINE §6):

    docker build --target test -t raxd-test . && docker run --rm raxd-test

Подтверждение: все тесты зелёные, без skip/t.Skip. Вывод:

    ok  github.com/vladimirvkhs/raxd                        0.006s
    ok  github.com/vladimirvkhs/raxd/internal/banner        0.001s
    ok  github.com/vladimirvkhs/raxd/internal/cli           0.071s
    ok  github.com/vladimirvkhs/raxd/internal/cmdexec       0.626s
    ok  github.com/vladimirvkhs/raxd/internal/config        0.004s
    ok  github.com/vladimirvkhs/raxd/internal/keystore      0.155s
    ok  github.com/vladimirvkhs/raxd/internal/mcp           3.668s
    ok  github.com/vladimirvkhs/raxd/internal/server        2.202s
    ok  github.com/vladimirvkhs/raxd/internal/version       0.002s
    (+ -race прогон: keystore, server, mcp — зелёные)

## Безопасность

- **Выполнение команд** — `exec.CommandContext(ctx, bin, args...)` без shell; shell-метасимволы
  в args трактуются буквально. Файл: `internal/cmdexec/exec.go`, функция `Run`.

- **Таймаут через context** — `context.WithTimeout` устанавливается в execHandler; передаётся в
  `Run`. `cmd.Cancel` → killGroup(-pgid, SIGKILL) + `cmd.WaitDelay = 5s`.
  Файл: `internal/mcp/exec_tool.go` + `internal/cmdexec/sysproc_unix.go`.

- **Allowlist** — строгая проверка равенства `entry == in.Command` ДО LookPath.
  ErrDot (relative path) отвергается. Файл: `internal/cmdexec/exec.go`.

- **Env-whitelist** — `cmd.Env` собирается явно из `os.Getenv` по списку; LD_PRELOAD/DYLD_*/IFS
  не в whitelist по умолчанию. Файл: `internal/cmdexec/exec.go`, `buildEnv`.

- **Аудит** — каждый вызов execute_command (deny/fail/success) пишет запись с timestamp,
  fingerprint, remote addr, command, args, exit_code (success), duration (success), timed_out.
  Ключевое тело НИКОГДА не логируется (SR-62). Файл: `internal/mcp/exec_tool.go`, `internal/server/audit.go`.

- **Аутентификация ключей** — выполняется transport-слоем (internal/server) ДО достижения
  MCP handler; cmdexec не импортирует internal/keystore (SR-27/SR-28).

- **Сравнение секретов** — не применимо в cmdexec: сравнение ключей — в internal/server/auth.go
  через `crypto/subtle.ConstantTimeCompare` (реализовано в предыдущей задаче mcp-server).

- **Права файлов** — не применимо в cmdexec: секреты хранятся в keystore (0600, предыдущая задача).

- **LogfmtFormatter** (F-1/SR-60) — `logger.SetFormatter(clog.LogfmtFormatter)` в serve.go.
  Файл: `internal/cli/serve.go`.

- **Credential не задаётся** (SR-54) — SysProcAttr содержит только Setpgid:true; дочерний процесс
  наследует uid/gid демона без эскалации привилегий.
  Файл: `internal/cmdexec/sysproc_unix.go`.

- **DenyRoot** (SR-56/ADR-003) — при euid==0 всегда пишется WARN-аудит; если DenyRoot=true —
  execHandler возвращает isError. Файл: `internal/mcp/exec_tool.go`.
