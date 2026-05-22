# architect-guardian — задача `command-exec`

**Итоговый verdict: pass** (раунд 2). Раунд 1 — needs-changes (F1 significant + F2/F3/F4),
все закрыты. Сохранено дирижёром (verifier не пишет сам).

## Раунд 1 — needs-changes

Артефакты: `plan.md` (~141 стр.) + ADR-003 (input-limits + root-policy). Сверено с реальным кодом
(internal/mcp/{server,tools,audit}.go, internal/server/{audit,auth}.go, internal/config/config.go,
internal/cli/serve.go).

### Покрытие AC
Все 18 AC промаппированы. AC13/AC14 и AC9 покрыты, но с архитектурной недосказанностью (F1, F3).

### Контракты против кода (реальны)
`NewHandler` (server.go:37) — расширение `+execCfg` ок; вызовов **11** (serve.go:87 + 10 в тестах) —
план верно требует обновить все. `AuditRecord`/`writeAudit` (audit.go) — расширение опц. полями
совместимо. `FingerprintFromContext`/`RemoteAddrFromContext` (auth.go) есть. config.go viper-паттерн
совпадает. Имя `internal/cmdexec` уникально, не конфликтует с `os/exec`. `sdkmcp.AddTool` паттерн ок.

### Решения по развилкам — все приняты конкретно
Модули (cmdexec + MCP-адаптер), контракт ExecInput/ExecOutput, таймаут+kill (ADR-001), cappedWriter,
env-whitelist `[PATH,HOME,LANG,TERM]` + deny LD_*/DYLD_*/IFS, cwd `/tmp`+валидация, allowlist ДО
LookPath точное равенство, root WARN (ADR-003), max_args=256/max_arg_len=128KiB/max_output=1MiB,
max_timeout=5мин, 8 конфиг-полей с дефолтами. Аудит-поля приняты, но механизм доставки недосказан.

### Findings
- **F1 (significant).** Не определён механизм, как расширенные поля аудита команды
  (command/args/exit_code/duration/timed_out) попадают в `AuditRecord`. Реальный `withAudit[In,Out]`
  (audit.go:33) пишет запись ПОСЛЕ handler и НЕ видит типизированный `ExecOutput`; deny по allowlist
  возникает ВНУТРИ cmdexec.Run/execHandler. План декларирует расширение writeAudit, но не описывает
  контракт переноса полей и не исключает двойной аудит (generic withAudit уже пишет MCP-запись для
  того же вызова). Зафиксировать ОДИН механизм: либо обобщить withAudit (контракт извлечения полей
  из Out/req), либо execHandler пишет полную exec-запись и НЕ оборачивается generic-аудитом. Назвать,
  кто пишет deny-запись (result=deny, AC13) и как исключается двойная запись.
- **F2 (minor).** plan ~141 стр. при норме 30-100. Задача крупная/неделима (отмечено в spec) —
  добавить строку-пометку или ужать описательные части.
- **F3 (minor).** AC9 часть (1) — наследование UID/GID (не задавать SysProcAttr.Credential/setuid) —
  не выражена явным контрактом. Добавить фразу в контракт sysproc_unix.go/cmdexec.Run.
- **F4 (nit).** Заголовок «Формат аудита logfmt vs JSON» читается как незакрытая развилка; на деле
  корректно вынесено на подтверждение security. Косметика.

### ADR-оценка
ADR-003 валиден (контекст→решение→альтернативы→последствия→зависимость security→статус proposed).
Числа разумны и привязаны к research Q4/Q10. Red line 4 соблюдена образцово (отклонения вынесены
security в threat-model).

### Безопасность
Архитектурных дыр нет: shell-инъекция исключена (no sh -c, ErrDot), неубиваемые процессы закрыты
(Setpgid+Kill группы+WaitDelay), env-whitelist, allowlist строгий, DoS-лимиты, fingerprint вместо
ключа. Единственная связанная недосказанность — F1 (надёжность deny-записи в аудит → AC13).

### Резюме для architect
Закрыть F1 (обязательно) + F3 (желательно), пометить F2, F4 — опц. После — повторный гейт.

## Раунд 2 — pass
- F1 — закрыт: вариант A (ADR-004) — execute_command НЕ оборачивается generic withAudit; execHandler
  сам пишет ровно одну запись через server.AuditFn. Три ветки (success/timeout, deny+reason,
  fail+reason), fp/remote из FingerprintFromContext/RemoteAddrFromContext (auth.go:36/56, реальны),
  AuditFn=func(AuditRecord) совместим (audit.go:33). ping/server_info остаются на withAudit. AC13/14
  закрыты, deny-запись надёжно доходит до аудита.
- F3 — закрыт: SysProcAttr.Credential НЕ задаётся (нет setuid), наследование uid/gid (AC9-тест №1).
- F2 — закрыт: пометка о размере. F4 — закрыт: заголовок переименован.
ADR-004 валиден, не конфликтует с ADR-001/002/003 (механизм vs формат разделены чисто). Новых
findings нет, AC не тронуты, развилки решены конкретно, тел функций нет. Передаётся security.
