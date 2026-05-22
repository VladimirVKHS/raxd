# Review — задача `command-exec`

**Verdict: accept.** Reviewer (read-only). Дата: 2026-05-22. Ветка feature/command-exec поверх develop.
Сохранено дирижёром (reviewer не пишет сам — red line 1).

Вход: spec.md (AC1-18), plan.md, security-requirements.md (SR-40..67), mcp-spec.md, ADR-001..004,
threat-model.md, SECURITY-BASELINE §3/§4. Сборку/тесты не запускал (read-only, baseline §6) — доверяю
задокументированному Docker-выводу qa, критичные места перепроверил чтением кода.

## Соответствие AC — все 18 закрыты кодом
| AC | Доказательство (файл:строка) |
|---|---|
| AC1 поверхность только tool | server.go:51-53 AddTool рядом с ping/server_info; нет CLI exec (root.go); TestExecToolInToolsList |
| AC2 без shell | exec.CommandContext(ctx,bin,args...) exec.go:113; статический инвариант security_static_test.go:71-98; os.Stat-тесты инъекции |
| AC3 вход +additionalProperties:false, нет env | exec_tool.go:35-44; TestExecExtraFieldRejected (+нет result=ok) |
| AC4 выход 7 полей | exec_tool.go:48-56,225-233; TestExecOutputHas7Fields |
| AC5 таймаут default/max+kill+timed_out | exec_tool.go:156-173,183; TestExecTimeoutKills/...ExceedsMaxIsError |
| AC6 обрыв/отмена→kill дерева | sysproc_unix.go:22-39 Setpgid+Cancel→killGroup+WaitDelay; TestContextCancelKillsChildren (Signal0→ESRCH) |
| AC7 allowlist строгий, выкл по умолч | exec.go:82-93 entry==command ДО LookPath; TestAllowlistStrictMatch/Disabled |
| AC8 несущ. бинарь→isError, жив | exec.go:208-221; TestExecNonExistentBinary (+живость) |
| AC9 не повышать привил.; root WARN; deny_root | sysproc_unix.go:22-27 без Credential; exec_tool.go:94-120; TestInheritedUID/RootWarn/DenyRoot |
| AC10 cwd дефолт+валидация; env-whitelist | exec_tool.go:176-179; exec.go:178-187,227-235; TestEnvWhitelistBlocks* (LD_*/IFS/DYLD_*) |
| AC11 лимиты вывода+truncated | cappedwriter.go:30-57; TestOutputTruncated/CappedWriterDoesNotOOM |
| AC12 auth ДО, 401/403 | наследуется; TestExecRateLimitInherited(401), TestExecKeystoreCorruptReturns403 |
| AC13 аудит каждого вызова+поля | exec_tool.go (все ветки); TestExecAuditContainsRequiredFields/DenyContainsCommandArgs |
| AC14 формат+ротация, не ломать не-exec | audit.go:77,84-101 (поля только Tool==execute_command); TestExecAuditLogfmtParseable/DoesNotBreakNonExec |
| AC15 без секретов | exec_tool.go:86 fingerprint из ctx; TestExecNoKeyInAuditOrResponse |
| AC16 rate-limit 429 | TestExecRateLimit429BeforeCommand (реальный 429 через TLS-стек) |
| AC17 протокол/isError/без паники | без panic, ошибки через error; TestExecNonZeroExitNotError |
| AC18 тесты в Docker vendor offline | impl-notes/test-plan; go.mod без новых зависимостей |

## Безопасность — SR-40..67 + baseline §3/§4 соблюдены
no-shell (единственный путь CommandContext + статический инвариант), ErrDot (двойная защита
exec.go:210,217), kill дерева (Setpgid+Kill(-pgid,SIGKILL)+WaitDelay, на любую отмену ctx),
env-whitelist (явный buildEnv, LD_*/IFS/DYLD_* блокируются — CWE-426/427), один аудит (execute_command
без withAudit; ping/server_info на withAudit — нет двойной записи), без секретов (cmdexec не импортирует
keystore, только fingerprint, нейтральные ошибки). Перепроверены критичные тесты (shell-инъекция через
os.Stat, env через реальный вывод env, реальный 429, ключ-не-подстрока, kill-детей через Signal0) —
все честные. Дыр (command injection, утечка ключа, двойной аудит, осиротевшие процессы, паника-роняющая-
сервер) НЕ обнаружено.

## Отклонения П-1/П-2/П-3 — реализованы как принято security
П-1 logfmt: serve.go:85 LogfmtFormatter (строгий парсимый). П-2 root: WARN каждый вызов
(exec_tool.go:94-105) + deny_root дефолт false→при true+euid0 isError+DENY; warn≠deny разделены
(audit.go:117-136). П-3 args дословно: formatArgs без маскирования (audit.go:188-195).

## Findings (LOW/INFO — не блокируют)
- **F-1 (LOW).** lookupBinary для абсолютного пути делает только os.Stat (без проверки x-бита/regular).
  Безопасно (cmd.Start даст isError), текст ошибки придёт от Start. Опц.: проверить Mode()&0111.
- **F-2 (LOW).** allowlist+абсолютный путь: запись должна точно совпадать с присланной строкой
  (`ls`≠`/bin/ls`) — осознанная цена AC7 (ОР-5). → tech-writer задокументировать.
- **F-3 (INFO).** TestContextCancelKillsChildren: ветка EPERM не фейлит (в Docker не возникает, ESRCH —
  настоящий ассерт). Опц. комментарий.
- **F-4 (INFO).** TimedOut через ctx.Err()!=nil после Wait — корректно покрывает таймаут И обрыв.
- **F-5 (INFO).** joinStrings/formatArgs самописные вместо strings.Join — вкусовщина, не блокер.

## Качество кода
Идиоматичный Go (sentinel-ошибки + errors.Is, чистое разделение слоёв cmdexec без MCP/логирования,
//go:build unix изоляция), корректная обработка ошибок (граница error→isError; exit≠0 и таймаут не
ошибки), мёртвого кода нет (pathVal устранён), фальш-зелёных тестов нет (перепроверено), scope не
превышен (нет env/PTY/CLI/эндпоинта), go.mod без новых зависимостей.

## Итог
accept. Все 18 AC реализованы, SR/baseline соблюдены, отклонения как утверждено security, дыр нет.
F-1..F-5 — LOW/INFO, не условие merge. Хендофф: tech-writer (документация execute_command +
ОБЯЗАТЕЛЬНОЕ предупреждение «не передавайте секреты в argv» по П-3/SR-63 + семантика allowlist F-2).
