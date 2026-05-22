# ADR-004: Аудит execute_command — собственная запись в execHandler (без generic withAudit)

## Контекст
Каждый вызов `execute_command` должен писать аудит-запись с полями command/args/exit_code/duration/
timed_out/fingerprint/remote/result (AC13), машиночитаемо и не ломая формат остальных записей (AC14).
Существующий generic-декоратор `internal/mcp/audit.go:withAudit[In,Out any]` пишет `AuditRecord`
ПОСЛЕ handler и оперирует только tool name + fingerprint + remote + success/fail — он НЕ имеет доступа
к типизированным полям `ExecOutput` (exit code, duration, truncated). Кроме того, deny по allowlist и
превышение входных лимитов возникают ВНУТРИ `cmdexec.Run`/`execHandler` (часто без валидного `Out`),
и generic-декоратор записал бы их лишь как «fail» без exec-полей. Если оставить generic `withAudit`
на execute_command и при этом писать exec-запись внутри handler — получится ДВОЙНОЙ аудит одного вызова.

## Решение
**execute_command НЕ оборачивается generic `withAudit`.** В `internal/mcp/server.go` он регистрируется
как `sdkmcp.AddTool(s, execTool(), execHandler(execCfg, audit))` — handler принимает `server.AuditFn`
и сам пишет РОВНО одну exec-аудит-запись за вызов (плюс отдельный root-WARN при euid==0). Контракт
переноса полей (fingerprint/remote — из ctx через `server.FingerprintFromContext`/`RemoteAddrFromContext`):
- **success/таймаут:** `Result:"success"` + Command, Args, ExitCode, Duration, TimedOut (таймаут — тот же
  Result с `TimedOut:true`);
- **deny** (allowlist / превышение `max_args`/`max_arg_len`/`max_timeout_ms`): `Result:"deny"` + Command,
  Args, Reason;
- **fail** (несуществующий бинарь / невалидный cwd): `Result:"fail"` + Command, Args, Reason.
`ping`/`server_info` по-прежнему используют generic `withAudit` (без изменений).

## Альтернативы
- **Вариант B: обобщить `withAudit` через опциональный интерфейс `AuditFields()` на `Out`.** Декоратор
  при наличии интерфейса дописывал бы exec-поля из `ExecOutput`. Отвергнут: при deny/ошибке `Out` пуст
  или невалиден → нужен доп. протокол передачи команды/reason в декоратор; сложнее тестировать; усложняет
  generic-путь ради одного инструмента.
- **Оставить generic `withAudit` + писать exec-запись в handler.** Отвергнут: ДВОЙНАЯ аудит-запись на
  один вызов (одна неполная от декоратора, одна полная от handler) — мусор в логе, риск рассинхрона.

## Последствия
- Плюсы: одна полная аудит-запись на вызов; deny/fail внутри cmdexec надёжно покрыты; типизированные
  поля ExecOutput доступны напрямую; формат не-exec записей не меняется (AC14).
- Минусы/цена: execute_command — единственный инструмент с собственным аудит-путём (асимметрия с
  ping/server_info, которые остаются на generic `withAudit`); дублируется маленький объём кода
  извлечения fingerprint/remote из ctx (тот же, что в `withAudit`).
- Влияние на стек: без новых зависимостей; согласуется с ADR-002 (расширение `AuditRecord`/`writeAudit`).

## Зависимость от security
Состав полей и факт «один раз за вызов» — контракт AC13/AC14; формат сериализации (logfmt vs JSON)
подтверждает security в threat-model.md (ADR-002). Этот ADR фиксирует ТОЛЬКО механизм (где и как пишется
запись), не формат.

## Статус (proposed|accepted)
accepted — ратифицирован гейтами security-guardian + architect-guardian (command-exec), 2026-05-22.
