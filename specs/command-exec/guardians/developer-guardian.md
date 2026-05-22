# developer-guardian — задача `command-exec`

**Итоговый verdict: pass** (раунд 2). Раунд 1 — needs-changes (#1 HIGH фальш-зелёный тест +
#2 MEDIUM + #3/#4/#5), все закрыты. Самая опасная фича — планка максимальная.
Сохранено дирижёром (verifier не пишет сам). Статический анализ (read-only, без запуска).

## Раунд 1 — needs-changes

Ветка feature/command-exec (10 коммитов). Сверено с plan, security-requirements (SR-40..67),
mcp-spec, spec (18 AC), ADR-001..004.

### Покрытие SR/AC кодом — почти полное (реализовано)
no-shell exec.CommandContext (exec.go:113, SR-43), ErrDot (exec.go:222/229, SR-44), таймаут+max
(exec_tool.go:146-158, SR-46), Setpgid+killGroup+WaitDelay (sysproc_unix.go:23-33, exec.go:133-138,
SR-47), allowlist точное равенство ДО LookPath (exec.go:82-93, SR-48), env-whitelist (exec.go:116/246,
SR-49), cwd os.Stat+IsDir (exec.go:178-187, SR-50), additionalProperties (exec_tool.go:35-44, SR-51),
лимиты входа (exec_tool.go:110-138, SR-52), capped writer (cappedwriter.go:30-56, SR-53), Credential
не задан (sysproc_unix.go:23-26, SR-54), root WARN+deny_root (exec_tool.go:91-106, SR-55/56), нет
двойного аудита (server.go:53, SR-57), поля аудита (exec_tool.go:232-243, audit.go:81-98, SR-58),
не-exec записи не сломаны (audit.go:74, SR-59), LogfmtFormatter (serve.go:85, SR-60), fingerprint из
ctx без ключа (SR-62), безопасные дефолты (config.go:130-138, SR-66). Раннер безопасен (no sh -c).

### Findings
- **#1 (HIGH, MUST-FIX).** `TestExecNoShellInjectionViaMCP` (internal/mcp/exec_tool_test.go:161-187) —
  ФАЛЬШ-ЗЕЛЁНЫЙ: создаёт имя маркер-файла, но НЕ вызывает os.Stat(marker) после команды; проверяет
  лишь isError!=true. Пройдёт даже при реальной shell-инъекции через MCP-стек. Правильный образец —
  TestNoShellInjection (exec_test.go:71-74) с os.Stat. SR-43 требует реальной проверки отсутствия
  файла. Fix: добавить `if _, statErr := os.Stat(marker); statErr == nil { t.Errorf(...); os.Remove(marker) }`.
- **#2 (MEDIUM, SHOULD-FIX).** root-WARN пишется с `Result:"deny"` (exec_tool.go:93-102). При
  deny_root=false команда ВЫПОЛНЯЕТСЯ, но в аудите уже `result=deny` → вводит в заблуждение (deny
  должен означать, что команда не выполнена). SR-55 — это отдельная WARN-запись-предупреждение, не
  отказ. Fix: сделать root-WARN семантически отличимой (отдельная ветка/метка WARN, например reason
  «running as root», НЕ результат deny), не ломая не-exec записи; задокументировать в impl-notes.
- **#3 (LOW, SHOULD-FIX).** TestExecAuditContainsRequiredFields (exec_tool_test.go:652-699) не
  проверяет `remote=`, хотя SR-58 требует. Поле заполняется (exec_tool.go:86), но тест не верифицирует.
  Fix: добавить assert на `remote=`.
- **#4 (INFO).** Dead code `pathVal` в lookupBinary (exec.go:194-236): вычисляется и отбрасывается
  `_ = pathVal`. Убрать либо реализовать задуманную верификацию PATH.
- **#5 (INFO).** TestNonZeroExitCodeNotError/TestCappedWriterDoesNotOOM используют `sh -c` как
  инструмент (не инъекция — корректно), но при включённом allowlist без sh сломаются. Осознать.

### Что хорошо
Чистый MCP-независимый раннер (нет импортов mcp/keystore); sysproc_unix минималистичен и корректен;
CappedWriter без дедлока пайпа (always len(p),nil + дренаж); ADR-004 строго выполнен (нет двойного
аудита, одна запись во всех ветках); безопасные дефолты; LogfmtFormatter установлен; статический
TestStaticNoExecCommand корректно исключает cmdexec; isError через fmt.Errorf корректен по SDK; новых
зависимостей нет; allowlist строгий (точное равенство).

### Резюме для developer
MUST: #1 (фальш-зелёный тест shell-инъекции через MCP). SHOULD: #2 (семантика root-WARN), #3 (assert
remote=). INFO: #4 dead code, #5 осознать. Перепрогнать тесты в Docker. После — повторный гейт.

## Раунд 2 — pass
Коммиты eab7ee1/3ae38ee/d83dcf4/a111e36/3d1065c.
- #1 закрыт: TestExecNoShellInjectionViaMCP (exec_tool_test.go:191-196) реально проверяет os.Stat(marker),
  падает при создании файла; вектор `args:["a; touch "+marker]`; pre-cleanup; зелёный по правильной причине.
- #2 закрыт: case "warn" в writeAudit (audit.go:117-136) WARN-уровень, отличим от DENY; два пути root
  (euid==0+deny_root=false→warn, команда идёт; +deny_root=true→warn+deny, команда отклонена). Не-exec
  ветки не сломаны; комментарий Result обновлён. Соответствует ADR-004/SR-55/57.
- #3 закрыт: assert remote= (exec_tool_test.go:709-713).
- #4 закрыт: pathVal удалён (exec.go), LookPath по PATH демона сохранён.
Новых findings/регрессий/дыр нет, go.mod не изменён. Передаётся qa.
