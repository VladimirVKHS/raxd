# qa-guardian — задача `file-upload`

**Итоговый verdict: pass** (раунд 2). Раунд 1 — needs-changes (F-1 MEDIUM блокирующий + F-2/F-3/F-5),
все закрыты. Опасная фича. Сохранено дирижёром.

## Раунд 1 — needs-changes

Артефакты: test-plan.md + тесты (developer + qa коммит 73eb9b0). Сверено со spec (20 AC),
security-requirements (SR-68..82), mcp-spec.

## Покрытие AC — 19/20 полно
Все 20 в матрице. AC14 покрыт ЧАСТИЧНО: deny-векторы (цель-каталог/невалидный mode/нет required) есть,
но ветка result="fail" (диск полон/I/O) НЕ покрыта тестом. AC5a — конфиг-уровень есть, e2e резолва
serve.go нет (задокументировано, допустимо без запуска демона).

## Фальш-зелёные — НЕ обнаружено
traversal/симлинк реально проверяют os.Stat отсутствие/неизменность файла вне корня; logfmt-тест
переписан (вектор 1 space_and_equals падает при поломке квотирования); mode-политика errors.Is
ErrBadMode; атомарность os.ReadDir перебор; 401/429/лимит тела + os.Stat. euid-Skip взаимоисключающие
(unit_warn_writeAudit без skip — основной регресс).

## Security-тесты — реально проверяют векторы
Все ключевые: traversal (3+3 вектора+симлинк), logfmt-инъекция (квотирование), mode (setuid/setgid/
sticky/world-writable отказ), атомарность (нет частичного/temp), root-WARN unit, content не логируется,
401/429/тело. Сильные.

## 400 vs 413 (для tech-writer)
SDK (go-sdk streamable.go) на ошибку http.MaxBytesReader отдаёт HTTP 400 «failed to read body», НЕ 413.
spec AC16 и mcp-spec §4 строка #1 называют 413. НЕ баг безопасности (файл не создаётся, контракт
соблюдён); тест корректно принимает 4xx. ФИКСАЦИЯ ДЛЯ TECH-WRITER: документировать фактический 400
(не 413) при превышении max_body_bytes; точный 413 потребовал бы кастомного middleware (вне v1).

## Findings
- **F-1 (MEDIUM, БЛОКИРУЕТ — red line qa).** AC14 ветка result="fail" (диск полон/I/O→isError fail+
  msg=FAIL аудит) не покрыта тестом. fail≠deny; без теста может молча сломаться. Fix: тест с I/O-ошибкой
  (readonly upload root os.Chmod(root,0o444) или мок Write) → isError:true, FAIL в аудите, сервер жив,
  temp нет.
- **F-2 (LOW).** TestUploadFile_AuditHasFpAndRemote (httptest без TLS) проверяет лишь подстроку "fp="
  (значение может быть fp=-). Добавить пояснение/опереться на TLS-тест для реального fingerprint.
- **F-3 (INFO).** Нет раздела install-flow в test-plan (red line qa). Для file-upload неприменим (нет
  новых install-артефактов). Fix: явная строка «install-flow не применим — тестируется в distribution».
- **F-4 (INFO).** Docker-вывод без детальных счётчиков тестов (правдоподобен по таймингу). Не блокер.
- **F-5 (INFO, следствие F-1).** fail-ветка аудита покрывается тем же тестом, что F-1.

## qa не правил продакшен-код
upload_qa_test.go — только тесты + хелпер startMCPServerWithBodyLimit; production не тронут. N-1
исправление impl-notes корректно (формулировка про euid-Skip).

## Что хорошо
Полная матрица; сильное traversal-покрытие (unit+MCP+симлинк с os.Stat); QA-тесты закрыли реальные
пробелы (401/429/16/12 fp+remote); logfmt переписан разумно; атомарность на 3 уровнях; 400-vs-413
честно задокументировано; конфиг-тесты с граничными значениями.

## Резюме для qa
F-1 (MEDIUM, обязательно — тест fail-ветки AC14) + F-3 (INFO, строка про install-flow). F-2/F-4/F-5 —
желательно. Перепрогнать в Docker. После — повторный гейт.

## Раунд 2 — pass
Коммит 2f1ba10.
- F-1 закрыт: TestUploadFile_FailBranchIOError_MCP (notadir-файл→ENOTDIR в MkdirAll→result="fail",
  не deny; isError:true + FAIL+tool=upload_file в аудите + файл не создан + сервер жив; ENOTDIR не
  зависит от euid) + TestUploadFile_WriteAuditFailBranch_Unit (рендер fail: FAIL/tool=/path=/fp=/remote=,
  без result=ok/DENY). Оба реальны, не фальш-зелёные.
- F-2 закрыт: комментарий про fp="-" в httptest + ссылка на TLS-тесты.
- F-3 закрыт: строка install-flow неприменим (тестируется в distribution).
AC14 покрыт полностью (deny+fail), все 20 AC. Новых фальш-зелёных нет, qa не правил продакшен-код.
Для tech-writer (из раунда 1): документировать HTTP 400 (не 413) при превышении max_body_bytes.
Передаётся reviewer.
