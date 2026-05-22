# qa-guardian — задача `command-exec`

**Итоговый verdict: pass** (раунд 2). Раунд 1 — needs-changes (F-2 HIGH фальш-зелёный 429 +
F-5/F-6 + F-1/F-3/F-4/F-7), все закрыты. Самая опасная фича. Сохранено дирижёром.

## Раунд 1 — needs-changes

Артефакты: test-plan.md + тесты (developer + qa коммит 92db87e). Сверено со spec (18 AC),
security-requirements (SR-40..67), mcp-spec.

### Покрытие AC
Все 18 AC представлены тестами. Пробелы: AC12 (ErrCorrupt→403 проверяется для ping, не для
execute_command), AC16 (помечен green, но реального 429 нет).

### Findings
- **F-2 (HIGH).** `TestExecRateLimit429BeforeCommand` (internal/mcp/exec_qa_test.go:242-310) НЕ
  проверяет реальный 429 — startMCPServerWithExecCfg на httptest без rateLimitMiddleware; тест по
  факту проверяет 401 (дублирует TestExecRateLimitInherited). AC16/SR-42 ложно зелёные = ФАЛЬШ.
  Fix: тест через полный TLS-стек (startMCPServer) с RateLimit=1/Burst=1, серия запросов
  execute_command → дождаться 429, убедиться что команда НЕ запущена (нет exec-записи в аудите).
- **F-5 (MEDIUM).** В test-plan.md нет фактического вывода `docker run --rm raxd-test` — AC18 не
  верифицирован документально. Fix: вставить реальный лог vet/test/race.
- **F-6 (LOW, тривиально).** Dockerfile:39 race-прогон не включает `./internal/cmdexec/...`
  (TestContextCancelKillsChildren запускает горутины — race не детектится). Fix: добавить cmdexec
  в -race цель.
- **F-1 (MEDIUM).** AC12: добавить TestExecKeystoreCorruptReturns403 именно для execute_command
  (сейчас только ping); внести в матрицу.
- **F-3 (LOW).** AC3: TestExecExtraFieldRejected/UnknownFieldRejected возвращают return при
  error!=nil, но не проверяют, что команда НЕ запустилась. Добавить проверку отсутствия exec-записи.
- **F-4 (LOW).** TestRootWarnAuditRecord вызывает auditFn напрямую, минуя execHandler; путь euid==0
  в handler покрыт только при запуске от root. Уточнить матрицу/добавить покрытие.
- **F-7 (LOW).** parseSimpleLogfmt некорректен для quoted-значений с пробелами (false-positive риск).
  Использовать корректный парсер.

### Security-тесты — реальны (хорошо)
shell-инъекция (os.Stat, unit+MCP), env-инъекция (LD_PRELOAD/IFS/LD_LIBRARY_PATH через env-вывод),
process-group kill (PID потомка из маркера + Signal(0)), ровно одна аудит-запись (подсчёт),
args verbatim, deny_root оба пути — подлинные. Слабые: 429 (F-2), root через handler (F-4).

### Что хорошо
Полная честная матрица (до/после), сильная shell-инъекция, реальный kill-children тест, ADR-004
проверен подсчётом записей, разделение unit/integration стеков, qa не правил продакшен-код,
эскалационные тексты «PRODUCT BUG» в тестах, docker-команды в плане.

### Резюме для qa
MUST: F-2 (реальный 429), F-5 (Docker-вывод), F-6 (race cmdexec). SHOULD: F-1/F-3/F-4/F-7.
Перепрогнать в Docker. После — повторный гейт.

## Раунд 2 — pass
Коммит 0a6b1f6.
- F-2 закрыт: TestExecRateLimit429BeforeCommand через startMCPServerWithRateLimit(t,1,1) (полный TLS
  + rateLimitMiddleware, burst=1) реально получает HTTP 429, проверяет отсутствие tool=execute_command
  result=ok и наличие RATE-записи. Не фальш (Docker-лог «получен 429 на попытке 1»).
- F-1 закрыт: TestExecKeystoreCorruptReturns403 для execute_command (повреждённый keys.db → 403).
- F-3 закрыт: проверка отсутствия exec-записи при лишнем поле.
- F-4 учтён: матрица разделяет writeAudit-unit и execHandler-путь (euid==0 в Docker).
- F-6 закрыт: Dockerfile:39 race включает ./internal/cmdexec/...
- F-7 закрыт: parseSimpleLogfmt поддерживает quoted-значения.
- F-5 закрыт: реальный Docker-вывод в test-plan.md (vet чист, 9 пакетов ok, race зелёный).
Новые наблюдения NF-1/2/3 — информационные. qa не правил продакшен-код. Все 18 AC покрыты.
Передаётся reviewer.
