# Review — задача `file-upload`

**Verdict: accept.** Reviewer (read-only). Дата: 2026-05-22. Ветка feature/file-upload поверх develop.
Сохранено дирижёром (reviewer не пишет сам — red line 1).

Вход: spec.md (AC1-20), plan.md, security-requirements.md (SR-68..82), mcp-spec.md, ADR-001/002/003,
threat-model.md, SECURITY-BASELINE. Сборку/тесты не запускал (read-only) — доверие Docker-выводу qa,
критичное перепроверено чтением.

## Соответствие AC — все 20 закрыты кодом
| AC | Доказательство |
|---|---|
| AC1 поверхность только MCP | server.go:58 AddTool; нет CLI; tools/list тест |
| AC2 строгая схема входа | upload_tool.go:40-54 (4 поля, нет abs/owner); лишнее поле→isError, файл не создан |
| AC3 формат выхода | UploadOutput upload_tool.go:59-68 (без абс.пути); тест +проверка отсутствия абс.пути |
| AC4 path traversal | upload.go:96 IsLocal + :102 OpenRoot + Root.*; тесты ../абс/симлинк (os.Stat вне корня) |
| AC5a безопасный дефолт корня | serve.go:105-108 пусто→<StateDir>/uploads; тест |
| AC5b подкаталоги | upload.go:108-114 root.MkdirAll 0700; тесты |
| AC6 base64 | upload_tool.go:154 DecodeString→deny; невалидный/бинарь точные байты |
| AC7 лимит размера | ранний DecodedLen :139 + точная :170 + страховка Write; тесты (+нет temp) |
| AC8 overwrite дефолт запрет | upload.go:119-128 Root.Stat+ErrExists/Rename; тесты |
| AC9 права (chmod по fd) | upload.go:161 ДО записи, 0600, без chown; тесты 0600/0700 |
| AC10 атомарность | temp(crypto/rand,O_EXCL)→Sync→Rename→fsync-dir+defer cleanup; тесты no-temp (3 уровня) |
| AC11 root WARN/deny_root | upload_tool.go:109-134; тесты euid-условные |
| AC12/19 аудит без содержимого, одна запись | handler одна запись все ветки; тесты fp/remote/exactly-one |
| AC13 без секретов | fp из ctx, content не логируется; тест (ключ/контент/абс.путь отсутствуют) |
| AC14 устойчивость+edge | deny (dir/mode/base64) + I/O→fail; не паникует; fail-ветка ENOTDIR +unit |
| AC15 конфиг+валидация | config.go:169-233; тесты дефолты/0/neg/>ceiling/setuid/world-writable |
| AC16 лимит vs тело | ceiling-валидация на старте; 4xx тест (файл не создан) — см. 400 vs 413 |
| AC17 auth наследуется | server.go:58 единая цепочка; 401 тест (файл не создан) |
| AC18 rate-limit наследуется | 429 ДО upload тест (нет result=ok, RATE в аудите) |
| AC20 Docker/vendor/без deps | go.mod без новых deps; fileupload офлайн-юниты |

## Безопасность (SR-68..82, baseline) — соблюдены
SR-69 os.Root для ВСЕХ операций (нет обхода по сырому пути; IsLocal+OpenRoot; chmod по fd не
Root.Chmod — race закрыт); SR-73+ADR-003 mode-политика (setuid/setgid/sticky/world-writable отвергнуты;
см. F-1 — частичное расхождение строгости, НЕ дыра); SR-74 атомарность (O_EXCL+committed+defer cleanup;
too-large→temp не создаётся); SR-77 отдельный upload.deny_root + WARN; SR-78/79 generic withAudit НЕ
применён, одна запись, isUpload в каждом case writeAudit, не-upload записи не тронуты; SR-79
logfmt-инъекция через путь закрыта (квотирование, тест реальный); SR-80 без секретов/абс.пути/
содержимого (upload-слой не имеет доступа к телу ключа). Дыр не найдено (двойной аудит/утечка/обход
os.Root/осиротевший temp/паника/переполнение int — все проверены, отсутствуют).

## Отклонения — корректны
П-U1 logfmt (путь как logfmt-значение с авто-квотированием), П-U2 root WARN+опц.deny_root — реализованы
как принято security; ADR-003 accepted, threat-model.

## 400 vs 413 — техдолг доки, не дефект кода
Security-контракт соблюдён: тело>max_body_bytes → транспорт 4xx ДО handler, файл НЕ создан (AC16/SR-76,
тест os.Stat). go-sdk на ошибку MaxBytesReader отдаёт 400 «failed to read body» (в самом SDK), не 413.
Правка потребовала бы переписать ответ SDK — вне scope (red line «не переписывать транспорт/MCP»). Код
ответа не security-контракт. → tech-writer документирует фактический 4xx/400 (не 413).

## Findings (не блокируют accept)
- **F-1 (minor).** mode.go:46-61 ParseMode отвергает только 0o7000 и 0o002, но ADR-003 п.2 требует «любой
  бит вне 0777 запрещён». mode="010000" пройдёт валидацию. НЕ дыра (Go syscallMode отбрасывает биты вне
  ModePerm|Setuid|Setgid|Sticky → на диск 0000, setuid недостижим), но строгость ADR нарушена,
  UploadOutput.Mode покажет искажённый "10000". Fix: `if mode &^ 0o777 != 0 { return ErrBadMode }` +
  тест-вектор "010000"/"017777". Желательно до релиза, не гейт.
- **F-2 (info).** Дублирование mode-политики config.go:272-290 (parseModeStr) vs mode.go (осознанно —
  избежать цикл. зависимости). Тот же F-1 в обеих копиях — при фиксе поправить обе.
- **F-3 (info).** mcp_test.go:53 комментарий «UploadRoot будет пустым» противоречит :57 (t.TempDir()). Косметика.
- **F-4 (info).** upload.go:206 `_ = dir` мёртвый параметр в randomTmpName. Косметика.

## Качество кода
Идиоматичный Go (чистое разделение fileupload без MCP/логов, типизированные sentinel-ошибки + errors.Is);
обработка ошибок полная (deny/fail разделены); мёртвый код только F-4; фальш-зелёных тестов нет
(перепроверено: traversal/симлинк os.Stat вне корня; logfmt квотирование падает при поломке; mode реально
отвергает+точный режим на диске; атомарность; fail-ветка ENOTDIR).

## Итог
accept. Все 20 AC + SR-68..82 + baseline соблюдены, отклонения как утверждено security, дыр нет, scope
не превышен, без новых зависимостей. F-1 minor (не вектор эскалации) + F-2..F-4 info — не гейт.
Рекомендация: F-1/F-2 закрыть тривиальной правкой mode.go/config.go (1 строка маски + тест) до релиза
для полного соответствия букве ADR-003. Хендофф tech-writer: документация upload_file + оговорка
4xx/400 при превышении тела (AC16) + предупреждение mount points/bind-mount внутри upload root (SR-70).
