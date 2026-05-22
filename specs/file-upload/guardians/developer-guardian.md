# developer-guardian — задача `file-upload`

**Итоговый verdict: pass** (раунд 2). Раунд 1 — needs-changes (F-1 CRITICAL фальш-зелёный +
F-2 HIGH + F-3 MEDIUM + F-4/F-5 LOW), все закрыты. Опасная фича — планка максимальная.
Статический анализ (read-only). Сохранено дирижёром.

## Раунд 1 — needs-changes

Ветка feature/file-upload (9 коммитов). Сверено с plan, security-requirements (SR-68..82), mcp-spec,
spec (20 AC), ADR-001/002/003.

## Покрытие SR/AC кодом — production-код корректен
SR-69 os.Root везде (upload.go:96,102; все ФС через Root.*, нет raw os.OpenFile в обход), SR-73 chmod
по fd (upload.go:161, не Root.Chmod) + mode-политика (mode.go:50-58 запрет 0o7000/0o002), SR-74
атомарность (O_EXCL+Sync+Rename+defer-cleanup, crypto/rand имя), SR-75 размер до записи
(upload_tool.go:139,170), SR-76 валидация лимита (config.go:220-226), SR-77 root WARN+deny_root код
(upload_tool.go:109-133), SR-78 один аудит без withAudit (server.go:58), SR-79 isUpload в КАЖДОМ case
(audit.go:118,157,194,231), SR-80 без content/abs-path/ключа, SR-82 go.mod не изменён.

## Аудит (F-2 из mcp-guardian) — ВЫПОЛНЕН
writeAudit: все 4 case (success/warn/fail/deny) имеют отдельный `else if isUpload` блок с tool=upload_file;
upload НЕ в общем else. Не-upload записи не затронуты. Один аудит/вызов, без withAudit.

## Безопасность — production дыр нет
os.Root для всех операций, chmod по fd umask-независимо, атомарность строгая, mode-политика верные
константы, root-детекция, секреты не логируются. Замечание: int(cfg.MaxFileBytes) на 32-бит — но
целевые amd64/arm64 (int=64) не затронуты (F-5).

## Findings
- **F-1 (CRITICAL, ФАЛЬШ-ЗЕЛЁНЫЙ).** TestUploadFile_PathLogfmtInjection (upload_tool_test.go:754-762):
  `if strings.Contains(line,"tool=upload_file") { if !strings.Contains(line,"tool=upload_file") {t.Errorf}}`
  — внутреннее условие инверсно внешнему → t.Errorf НЕДОСТИЖИМ, тест всегда зелёный. SR-79 квотирование
  пути НЕ проверяется. Fix: реальные проверки — path="..." со спецсимволами в кавычках; строка лога не
  разбита (нет сырого \n); вектор с \n не увеличивает число строк tool=upload_file; убрать тавтологию.
- **F-2 (HIGH).** Нет теста SR-77 для upload (root WARN при euid==0; deny_root=true+euid==0→isError,
  файл не создан). Код есть (upload_tool.go:109-133), тест отсутствует. Добавить по образцу
  TestRootWarnAuditRecord/TestDenyRootUnitLogic (exec_qa_test.go).
- **F-3 (MEDIUM).** Нет тестов валидации upload-конфига (SR-81/76): max_file_bytes=0→ошибка,
  >потолка→ошибка, default_mode setuid/world-writable→ошибка на старте, дефолты без файла. Добавить
  в internal/config/.
- **F-4 (LOW).** impl-notes.md:9 сигнатура Write(ctx,cfg,in) не совпадает с кодом Write(cfg,in).
- **F-5 (LOW).** upload_tool.go:139 int(cfg.MaxFileBytes) сужение int64→int. Заменить на
  int64(DecodedLen(...)) > cfg.MaxFileBytes.

## Что хорошо
os.Root везде без исключений; chmod по fd образцово; атомарная схема полная (committed-guard);
ADR-004-стиль строг; isUpload в каждом case; mode-политика корректные константы; go.mod без изменений;
traversal-тесты реально проверяют отсутствие/неизменность файла вне корня (TestWriteTraversal_DotDotEscape,
TestUploadFile_TraversalDotDot os.Stat); mode-тесты типизированы (errors.Is ErrBadMode); нет t.Skip.

## Резюме для developer
F-1 (CRITICAL фальш-зелёный) + F-2 (HIGH) + F-3 (MEDIUM) обязательно; F-4/F-5 (LOW). Перепрогнать в
Docker. После — повторный гейт.

## Раунд 2 — pass
- F-1 закрыт: TestUploadFile_PathLogfmtInjection (upload_tool_test.go:747-879) переписан, тавтологии
  нет, 3 реальных вектора (space_and_equals path=" квотирование падает при поломке; quote_in_path;
  newline_in_path — │ U+2502 не whitespace, TrimSpace не убирает, t.Errorf достижим при сырой инъекции).
- F-2 закрыт: TestUploadRootWarnAuditRecord (4 подтеста: unit writeAudit warn всегда; no_warn не root;
  warn_when_root euid==0; deny_root_upload deny_root=true+euid==0→isError+файл не создан+DENY). Не пустые.
- F-3 закрыт: internal/config/upload_config_test.go (10 тестов — дефолты, max_file_bytes 0/-1/превышение/
  граница, default_mode setuid/setgid/world-writable отказ; реальный отказ, не запуск).
- F-4 закрыт: impl-notes сигнатура Write(cfg,in). F-5 закрыт: int64-сравнение без сужения.
- go.mod не изменён, production-код не сломан, Docker зелёный (race чист).

### Новый finding
- **N-1 (LOW).** impl-notes:71 утверждает «нет t.Skip», но 3 euid-условных t.Skip есть
  (upload_tool_test.go:924/951/983). Инженерно корректны (взаимоисключающие по euid, unit-подтест без
  skip), но текст impl-notes неточен. Не блокирует. (Зафиксировано дирижёром; правка опциональна.)
Передаётся qa.

---

## Раунд 2 — delta-гейт F-1/F-3/F-4 (после reviewer accept)

**Дата:** 2026-05-22 · **Guardian:** developer-guardian (read-only) · сохранено дирижёром.
**Проверяемые коммиты:** `6a903f8` (F-1 маска), `b735e36` (static-тест + fileupload в -race),
`c513dbb` (F-3/F-4), `3127503` (impl-notes).

**Verdict: pass.**

Проверено по существу (delta-scope):
1. `mode.go:51` / `config.go:283` — `mode&^fs.FileMode(0o777) != 0` отвергает ЛЮБОЙ бит вне 0o777
   (010000=4096 → 4096&^511=4096≠0; setuid 04000 → 1536≠0). World-writable `0o002` СОХРАНЁН отдельной
   проверкой в обеих копиях (0o777=511 проходит маску, ловится `511&2≠0`). ADR-003 §2+§3 выполнены.
2. Новые тесты (`mode_test.go`, `upload_config_test.go`) содержательны, не тавтологичны: `errors.Is(ErrBadMode)`
   и ошибка `Load()` с упоминанием `default_mode`; до фикса 010000 проходил → тест был бы красным.
   Легитимные 0600/0644/0700/0755/0400/0660 проходят.
3. `security_static_test.go` — матчер `isWideModeFsCall` (широкий режим + перм-синк на строке) НЕ ослаблен:
   реальное `os.WriteFile(...,0o644)` ловится; самопроверки `matcher_violation_detected` /
   `matcher_validation_mask_excluded` осмысленны (не no-op); маска валидации исключена корректно.
4. F-3 (`mcp_test.go:53` комментарий ↔ `t.TempDir()`) и F-4 (`randomTmpName()` без мёртвого `dir`,
   вызов согласован) устранены.
5. `Dockerfile:39` — `./internal/fileupload/...` добавлен в -race CMD, синтаксис валиден.
6. Scope не превышен: поведение записи/аудита/MCP-схемы не тронуто; новых зависимостей нет.

Передаётся reviewer/tech-writer (file-upload готов к закрытию по коду).
