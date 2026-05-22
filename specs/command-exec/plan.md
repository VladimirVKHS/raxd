# Plan: command-exec — MCP-инструмент `execute_command` для безопасного запуска команд на хосте

Автор плана: architect (raxd). Вход: spec.md (AC1–AC18, pm-guardian pass), research.md, ADR-001
(process-group kill), ADR-002 (формат аудита logfmt + системная ротация), ADR-003 (входные лимиты,
политика root), ADR-004 (аудит exec в handler). Опора на готовый код: `internal/mcp/{server,tools,audit}.go`,
`internal/server/{audit,middleware,server}.go`, `internal/config/config.go`.
Автор продукта: Vladimir Kovalev, OEM TECH.
Размер плана обусловлен 18 плотными security-AC и неделимостью задачи (spec §«Примечание о размере»):
запуск-без-shell, таймаут+kill, allowlist, лимиты, окружение и аудит без секретов — единый контракт.

## Chosen Approach
Логику запуска выносим в новый чистый пакет **`internal/cmdexec`** (имя не конфликтует со stdlib
`os/exec`), а MCP-обёртку (типы `ExecInput`/`ExecOutput` + handler + дескриптор tool) держим в
`internal/mcp`. `execute_command` регистрируется ТОЙ ЖЕ точкой расширения `sdkmcp.AddTool` в
`internal/mcp/server.go`, что `ping`/`server_info`. **Аудит exec особый (ADR-004):**
execute_command НЕ оборачивается generic `withAudit`; `execHandler` сам пишет ПОЛНУЮ exec-аудит-запись
во всех ветках (success/deny/isError) через `server.AuditFn` — это устраняет двойную запись и надёжно
покрывает deny внутри `cmdexec.Run`. auth/Origin/rate-limit/аудит транспорта потребляются как есть
(AC1/AC12/AC16). Запуск — на stdlib: `exec.CommandContext` без shell (AC2), `Setpgid:true` + custom
`Cmd.Cancel`→kill группы + `WaitDelay` (ADR-001, AC5/AC6), capped-writer для лимита вывода (AC11),
явный whitelist `Cmd.Env` (AC10), валидируемый `Cmd.Dir` (AC10). Новых внешних зависимостей нет.
Граница ошибок: несуществующий бинарь / deny allowlist / превышение лимитов входа → `isError:true`
(НЕ транспортная error), сервер жив (AC5/AC7/AC8/AC17). Альтернативы — в Trade-offs.

## Modules
- `internal/cmdexec/exec.go` — чистый раннер `Run(ctx, cfg, in) (Result, error)`: allowlist-проверка,
  валидация cwd, сборка `exec.Cmd`, whitelist env, capped-writers, измерение длительности, разбор
  exit code. Без MCP-типов и без логирования — тестируется офлайн юнит-тестами (AC18).
- `internal/cmdexec/sysproc_unix.go` (`//go:build unix`) — `applyProcessGroup(cmd *exec.Cmd)` и
  `killGroup(pid int) error`: `SysProcAttr{Setpgid:true}` + `syscall.Kill(-pgid, SIGKILL)` (ADR-001).
  `SysProcAttr.Credential` НЕ задаётся (никакого setuid) → дочерний процесс наследует uid/gid демона
  как есть (AC9-тест №1). Здесь и только здесь — платформенный syscall-код (Linux+darwin; Windows вне scope).
- `internal/cmdexec/cappedwriter.go` — тип `cappedWriter` (`io.Writer` с потолком N байт + флаг
  `Truncated`): пишет до лимита, остаток дренирует/отбрасывает без ошибки Write (AC11, research Q3).
- `internal/cmdexec/config.go` — структура `Config` пакета cmdexec (поля раздела `exec`) +
  валидация числовых значений; маппится из `config.ExecConfig` вызывающей стороной.
- `internal/mcp/exec_tool.go` — типы `ExecInput`/`ExecOutput` (json/jsonschema-теги), `execTool()`
  (дескриптор `*sdkmcp.Tool`), `execHandler(cfg cmdexec.Config, audit server.AuditFn)` (адаптер
  MCP↔cmdexec: валидация входных лимитов → `cmdexec.Run` → маппинг `Result`→`ExecOutput`+`CallToolResult`,
  root-WARN, **и сам пишет exec-аудит-запись** во всех ветках; ADR-004).
- `internal/config/config.go` — **расширить** `Config` секцией `Exec` (см. Contracts §Config) с
  viper-дефолтами; без env-оверрайдов (как везде в проекте).
- `internal/mcp/server.go` — **точка интеграции**: в `NewHandler` добавить регистрацию
  `sdkmcp.AddTool(s, execTool(), execHandler(execCfg, audit))` — БЕЗ обёртки `withAudit` (ADR-004:
  generic withAudit к execute_command НЕ применяется). Сигнатура `NewHandler` расширяется `execCfg`.
- `internal/server/audit.go` — **расширить** `AuditRecord` (поля команды/exit/duration) и
  `writeAudit` (ветка exec с этими полями, только когда заполнены) — ADR-002. `withAudit` для
  ping/server_info НЕ трогаем.
- `internal/cli/serve.go` — **точка интеграции**: собрать `cmdexec.Config` из `cfg.Exec`, передать
  в `internalmcp.NewHandler(version.Version, auditFn, execCfg)`.

## Contracts
- `cmdexec.Run(ctx context.Context, cfg Config, in Input) (Result, error)` (`internal/cmdexec/exec.go`)
  - параметры: `ctx` — несёт таймаут+отмену (handler ставит deadline до вызова); `cfg` — лимиты/env/
    cwd/allowlist; `in Input{Command string; Args []string; Cwd string}` (cwd уже резолвлен handler'ом
    к дефолту при пустом значении).
  - возврат при успехе: `Result{Stdout, Stderr []byte; ExitCode int; Duration time.Duration; TimedOut,
    StdoutTruncated, StderrTruncated bool}`. Ненулевой exit code команды — НЕ `error`, а `ExitCode` в `Result`.
  - ошибки (возврат `error`, который handler конвертирует в `isError:true` + deny/fail-аудит): allowlist
    deny (`ErrNotAllowed`), несуществующий бинарь (`exec.ErrNotFound`/`ErrDot`, AC8), невалидный cwd
    (`ErrBadCwd`, AC10). Таймаут НЕ ошибка раннера: `TimedOut:true` + частичный вывод (AC5). Никогда не паникует.
  - привилегии: НЕ устанавливает `SysProcAttr.Credential`/setuid → процесс наследует uid/gid демона (AC9).
- `cmdexec.applyProcessGroup(cmd *exec.Cmd)` / `cmdexec.killGroup(pgid int) error` (`sysproc_unix.go`)
  - `applyProcessGroup` ставит `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid:true}` (Credential не
    задаётся) ДО старта; `Run` назначает `cmd.Cancel = func() error { return killGroup(cmd.Process.Pid) }`
    и ненулевой `cmd.WaitDelay` (страховка зависших пайпов, ADR-001). `killGroup` шлёт SIGKILL группе `-pgid`.
- `ExecInput` (`internal/mcp/exec_tool.go`): `Command string \`json:"command"\`` (обязателен),
  `Args []string \`json:"args,omitempty"\``, `TimeoutMs int \`json:"timeout_ms,omitempty"\``,
  `Cwd string \`json:"cwd,omitempty"\``. Поля `env` НЕТ (AC3). Схема выводится `AddTool` из struct →
  `additionalProperties:false` автоматически; **закрепить тестом** «лишнее поле → isError» (research Q9-impl-check).
- `ExecOutput` (`internal/mcp/exec_tool.go`): `Stdout string`/`Stderr string`/`ExitCode int`/
  `DurationMs int`/`TimedOut bool`/`StdoutTruncated bool`/`StderrTruncated bool` (json: `stdout`,
  `stderr`, `exit_code`, `duration_ms`, `timed_out`, `stdout_truncated`, `stderr_truncated`) — AC4.
- `execHandler(cfg cmdexec.Config, audit server.AuditFn) sdkmcp.ToolHandlerFor[ExecInput, ExecOutput]`
  (`internal/mcp/exec_tool.go`) — **сам владеет аудитом exec (ADR-004); generic withAudit не применяется.**
  - валидирует входные лимиты ДО запуска: `len(Args) > cfg.MaxArgs`, `len(arg) > cfg.MaxArgLen`,
    `TimeoutMs > cfg.MaxTimeoutMs` → `isError:true`, команда НЕ запускается (AC5/AC9-DoS, ADR-003).
  - резолвит cwd (пусто → `cfg.DefaultCwd`), эффективный timeout (0 → `cfg.DefaultTimeoutMs`),
    ставит `context.WithTimeout`, зовёт `cmdexec.Run`; маппит `Result`→`ExecOutput` + `Content` (текст-итог).
  - root-детекция (AC9): `os.Geteuid()==0` → отдельная WARN-аудит-запись при КАЖДОМ вызове (политика —
    только WARN, не отказ; ADR-003). Тело API-ключа не извлекается (AC15); ошибки нейтральны (AC8).
  - **аудит-запись (контракт переноса полей, AC13/AC14; ADR-004):** fingerprint = `server.FingerprintFromContext(ctx)`,
    remote = `server.RemoteAddrFromContext(ctx)`. Ветки:
    · success/таймаут → `AuditRecord{Tool:"execute_command", Result:"success", Command, Args, ExitCode,
      Duration, TimedOut, Fingerprint, RemoteAddr}` (таймаут: тот же Result + `TimedOut:true`);
    · deny (allowlist/превышение лимитов входа) → `Result:"deny"` + `Command, Args, Reason, Fingerprint, RemoteAddr`;
    · isError исполнения (несуществующий бинарь/невалидный cwd) → `Result:"fail"` + `Command, Args, Reason, Fingerprint, RemoteAddr`.
    Запись пишется РОВНО один раз за вызов (плюс отдельный root-WARN при euid==0).
- `NewHandler(ver string, audit server.AuditFn, execCfg cmdexec.Config) (http.Handler, error)`
  (`internal/mcp/server.go`, **расширение сигнатуры**) — добавлен `execCfg`; регистрирует
  `execute_command` рядом с ping/server_info. Обновить все вызовы (serve.go + mcp-тесты).
- **`AuditRecord` (расширение, ADR-002)** — добавить опц. поля: `Command string`, `Args []string`,
  `ExitCode *int`, `Duration time.Duration`, `TimedOut bool` (логируются только для execute_command).
  Инвариант SR-21/AC15: команда+args НЕ содержат секретов; вместо ключа — fingerprint.
- **`writeAudit` (расширение, ADR-002)** — при `Tool=="execute_command"` логировать
  `command=`,`args=`,`exit_code=`,`duration=`,`timed_out=` в ветках `success`/`fail`/`deny`. Не-exec
  записи (AUTH/ping MCP/FAIL/DENY/RATE) не меняются: новые поля логируются ТОЛЬКО когда заполнены (AC14).

## Config (`internal/config/config.go`, секция `exec`, viper-дефолты, без env-оверрайдов)
- `allowlist []string` (ключ `exec.allowlist`, дефолт `[]` = выкл = разрешено всё, AC7);
- `default_timeout_ms int` (дефолт `30000` = 30s, AC5);
- `max_timeout_ms int` (дефолт `300000` = 5 мин — жёсткий максимум, AC5; ADR-003);
- `default_cwd string` (дефолт `/tmp` — предсказуемый, не `/`, не cwd демона, AC10);
- `env_whitelist []string` (дефолт `["PATH","HOME","LANG","TERM"]`; PATH обязателен для LookPath; НЕ
  пускать `LD_PRELOAD`/`LD_LIBRARY_PATH`/`DYLD_*`/`IFS` — research Q4; значения берутся из окружения демона);
- `max_args int` (дефолт `256`, ADR-003); `max_arg_len int` (дефолт `131072` = 128 KiB, ADR-003);
- `max_output_bytes int` (дефолт `1048576` = 1 MiB на каждый из stdout/stderr, AC11).
Все — `v.SetDefault` в `Load`, чтение в `buildConfig` в новую структуру `Config.Exec ExecConfig`.

## AC → реализация
| AC | Где |
|---|---|
| AC1 | `server.go` AddTool(execute_command) за той же цепочкой; tools/list содержит инструмент |
| AC2 | `cmdexec.Run` → `exec.CommandContext(ctx,bin,args...)`, без `sh -c`; отвергать `ErrDot` |
| AC3 | `ExecInput` struct → `additionalProperties:false` (тест на лишнее поле); `env` отсутствует |
| AC4 | `ExecOutput` (7 полей) → `structuredContent` + text-блок |
| AC5 | handler: timeout default/max из cfg, превышение max → isError; `cmdexec` ставит deadline+kill |
| AC6 | `sysproc_unix.go`: Setpgid + Cancel→killGroup + WaitDelay (ADR-001) |
| AC7 | `cmdexec.Run` allowlist строгое точное равенство по присланному `command` ДО LookPath |
| AC8 | несуществующий бинарь → `ErrNotFound`/`ErrDot` → isError, нейтральный текст, сервер жив |
| AC9 | uid/gid наследуются (Credential не задан, тест №1); `execHandler` os.Geteuid()==0 → WARN каждый вызов (тест №2; только WARN, ADR-003) |
| AC10 | `Cmd.Env`=whitelist; `Cmd.Dir`=cfg.DefaultCwd/валидируемый cwd (os.Stat+IsDir) |
| AC11 | `cappedWriter` на cmd.Stdout/Stderr, флаги `*_truncated`, дренаж остатка |
| AC12/AC16/AC17 | существующие auth/rate-limit/SDK-ошибки ДО инструмента (не переписываются) |
| AC13 | `execHandler` сам пишет exec-AuditRecord (success/deny/fail) с command/args/exit_code/duration/fp/remote/result (ADR-004) |
| AC14 | расширенный `writeAudit` (ADR-002): exec-поля только при Tool=="execute_command"; формат не-exec записей не ломается |
| AC15 | fingerprint вместо ключа; нейтральные ошибки; тест «ключ не подстрока в логе/ответе» |
| AC18 | сборка/тесты `-mod=vendor` в Docker; `internal/cmdexec` юнит-тестируется офлайн |

## Trade-offs
- Выбрали **аудит exec прямо в execHandler (ADR-004, вариант A)** вместо generic `withAudit`: цена —
  execute_command — единственный инструмент с собственным аудит-путём (небольшая асимметрия с ping/
  server_info); взамен — НЕТ двойной записи, deny/is-error внутри cmdexec надёжно покрыты, типизированные
  поля ExecOutput доступны напрямую. Отвергнут вариант B (обобщить withAudit через интерфейс
  `AuditFields()` на Out): сложнее, deny при пустом Out требует доп. протокола передачи ошибки в декоратор.
- Выбрали **отдельный пакет `internal/cmdexec`** вместо логики прямо в `internal/mcp`: цена — +пакет
  и адаптер-маппинг MCP↔cmdexec; взамен — чистый, MCP-независимый раннер, тестируемый юнитами без HTTP/SDK.
- Выбрали **capped-writer на `cmd.Stdout/Stderr`** вместо `StdoutPipe`+`LimitReader` (research Q3-B):
  цена — мини-тип-обёртка (~30 строк); взамен — жёсткий потолок памяти и снятие класса дедлоков пайпов.
- Выбрали **allowlist ДО LookPath** (строка как прислана, research Q7-A) вместо матча абсолютного пути
  (Q7-B): цена — `ls` и `/bin/ls` суть разные записи (клиент может прислать другой алиас); взамен —
  буквальное соответствие AC7 «точное совпадение» и предсказуемость для администратора.
- Выбрали **политику root = только WARN** (не отказ, ADR-003): цена — команды от root-демона
  исполняются (риск эскалации); взамен — не ломаем легитимные сценарии; основная защита — раскладка
  не-root (baseline §3, задача service-install) + контейнер (baseline §6). **Подтверждает security.**
- Выбрали **SIGKILL группе сразу** (ADR-001 базовый) вместо SIGTERM→grace→SIGKILL: цена — нет чистого
  завершения; взамен — минимум кода/edge-case для НЕинтерактивных команд. Эволюция возможна позже.
- Новых внешних зависимостей **нет**: всё на stdlib (`os/exec`,`context`,`syscall`,`io`,`os`,`time`) +
  уже вендоренные `charmbracelet/log`/go-sdk — сверено со STACK.ru.md (stdlib предпочтительна).

## Открытые зависимости (подтверждает security в threat-model.md)
- **Рекомендация: logfmt (отклонение baseline §4 «JSON») — подтверждает security.** Architect
  рекомендует остаться на текущем key=value/logfmt-совместимом `writeAudit` ради единого формата с
  транспортом (JSON фрагментирует аудит, ломает AC14). ФИНАЛЬНОЕ принятие отклонения и фиксацию риска
  делает **security** в `threat-model.md` (red line 4, ADR-002). Если security настаивает на JSON —
  это глобальная смена форматтера логгера (вне scope command-exec по коду, затрагивает транспорт).
- **Политика при root-демоне (AC9): WARN vs отказ.** Рекомендация architect (ADR-003): только WARN.
  Подтверждает **security** в threat-model.md (риск исполнения от root + смягчение).
- **Состав env-whitelist и числовые пороги** (`PATH/HOME/LANG/TERM`; max_timeout=5мин; max_args=256;
  max_arg_len=128KiB; max_output=1MiB). Architect задал безопасные дефолты (ADR-003); **security**
  подтверждает достаточность с точки зрения threat-model (DoS/эскалация окружения).
