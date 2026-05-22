# mcp-guardian — задача `command-exec`

**Итоговый verdict: pass** (раунд 3). Раунд 1 — needs-changes (2 MEDIUM + 3 LOW), раунд 2 —
needs-changes (1 LOW: result=ok vs Result:success), все закрыты. Сохранено дирижёром.

## Раунд 1 — needs-changes

Артефакт: `mcp-spec.md` (инструмент execute_command). Сверено со spec (18 AC), plan, ADR-001..004,
security-requirements (SR-40..67), MCP-INTEGRATION.ru.md, реальным кодом и vendored go-sdk.

### Схемы вход/выход — корректны
ExecInput (command required; args/timeout_ms/cwd optional+omitempty; env нет; additionalProperties:false
подтверждён vendor `jsonschema-go/infer.go:248`). ExecOutput 7 полей с верными JSON-тегами (AC4/SR-65).
outputSchema объявлена, SDK валидирует structuredContent.

### Error-mapping — в целом верен
Граница protocol-error vs isError точно по строкам SDK (`server.go:315-354`, ToolHandlerFor): нарушение
inputSchema/обычный error→isError; *jsonrpc.Error→wire error; ненулевой exit code и timed_out=true →
НЕ ошибка (isError:false) — выдержано последовательно.

### Поток и аудит — точен
Цепочка bodyLimit→recover→Host/Origin→auth→rate-limit→authSuccessAudit→mux→/mcp→SDK→execHandler→
cmdexec.Run→exec-аудит совпадает с кодом. ADR-004 (собственный аудит без generic withAudit) обоснован,
3 ветки покрыты. authSuccessAudit и exec-аудит — разные записи (учтено).

### Findings
- **ISSUE-1 (MEDIUM).** В §2.1/§4/§6.5 коды «−32601 / −32602» смешаны в одной строке для разных
  ситуаций. Реальность SDK: неизвестный JSON-RPC метод → −32601 (ErrMethodNotFound); неизвестный
  инструмент в tools/call → −32602 (CodeInvalidParams, server.go:749). Разнести на две строки с
  указанием кода и места в SDK. qa/developer ориентируются на эту таблицу.
- **ISSUE-2 (MEDIUM).** Формулировка про `additionalProperties:false` двусмысленна («дефолт настраивается»).
  Уточнить: гарантируется инференцией SDK из struct (infer.go:248), developer НЕ обязан выставлять явно
  в дескрипторе Tool, qa ОБЯЗАН закрепить тестом как регрессию.
- **ISSUE-3 (LOW).** §4 строка #16 «`success`+`timed_out`» → переписать как «`Result:"success"`,
  `TimedOut:true`» (риск, что developer сделает Result:"timed_out").
- **ISSUE-4 (LOW).** ADR-002/003/004 в статусе `proposed`, а спека ссылается как на принятые. Зафиксировать,
  что ADR ратифицированы прошедшими гейтами security-guardian/architect-guardian (или добавить Q-EXEC-4).
- **ISSUE-5 (LOW).** AC6 (обрыв HTTP-соединения → ctx.Done() → kill процесса и дерева, SR-47) не отражён
  в потоке/error-mapping. Добавить примечание, что developer обязан обработать отмену ctx, не только таймаут.

### Что хорошо
Диаграмма потока точна по именам функций+SR; граница protocol vs tool error разобрана по строкам SDK;
правило «exit≠0/timeout не ошибка» последовательно; ADR-004 убедителен (нет двойного аудита, 3 ветки);
примеры JSON-RPC реалистичны; additionalProperties:false проверен по vendor; Out of Scope/Q-EXEC честные.

### Резюме для mcp-engineer
Исправить ISSUE-1/2 (MEDIUM, обязательно) + ISSUE-3/5 (LOW). ISSUE-4 — закрывается ратификацией ADR
(architect переводит ADR-001..004 в accepted). После — повторный гейт.

## Раунд 2 — needs-changes (1 новый LOW)

Все 5 issue раунда 1 закрыты содержательно (ISSUE-1 коды разнесены −32601/−32602; ISSUE-2
additionalProperties:false однозначно; ISSUE-3 Result success/deny/fail; ISSUE-4 ADR accepted;
ISSUE-5 отмена ctx/AC6). Перенумерация §4 #1-#20 без битых ссылок.

### Новый finding
- **FINDING-NEW-1 (LOW).** Рассогласование `result=ok` (примеры аудита §6.1/§6.4) vs нормативные
  §2.3/§8 (`Result:"success"`).
  **Уточнение дирижёра по реальному коду (audit.go:53-95):** оба значения верны, но описывают РАЗНОЕ:
  поле `AuditRecord.Result` (Go-уровень) = "success"/"fail"/"deny"; а `writeAudit` РЕНДЕРИТ
  MCP-успех в лог как `result=ok` (строка 62), для deny → метка `DENY`+`reason` (без ключа result=),
  для fail → `FAIL`+`reason`. Спека не разграничивает «значение поля» и «рендер в логе» → путаница
  для developer/qa.
  **Fix mcp-engineer:** явно разграничить в §2.3/§6/§8: поле AuditRecord.Result принимает
  success/deny/fail; рендер логфмт-строки для exec-успеха = `msg=MCP ... result=ok`, для deny =
  `msg=DENY ... reason=...`, для fail = `msg=FAIL ... reason=...` (+ новые поля command/args/exit_code/
  duration/timed_out во всех ветках). Примеры §6 привести в соответствие с фактической логикой
  writeAudit. qa тесты пишет по РЕНДЕРУ.

### Резюме
Закрыть FINDING-NEW-1 (точное разграничение поле vs рендер по audit.go). После — финальный гейт.

## Раунд 3 — pass
FINDING-NEW-1 закрыт полностью: §2.3.1 разграничивает поле AuditRecord.Result (success/deny/fail)
и рендер writeAudit (success→msg=MCP+result=ok; deny→DENY+reason; fail→FAIL+reason; rate→RATE) —
точно сверено с audit.go:53-95. Примеры §6 приведены к фактическому коду (убраны несуществующие
ключи result=deny/result=fail). §8 — контракт расширения writeAudit exec-полями без слома не-exec
записей. Новых обязательных findings нет (R3-1 — лишь наблюдение: новые поля AuditRecord developer
добавит по спеке). Схемы/error-mapping/поток корректны. Спека готова к передаче developer.
