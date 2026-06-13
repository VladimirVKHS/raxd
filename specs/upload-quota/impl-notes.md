# Impl Notes: upload-quota

## Что реализовано

- **`internal/fileupload/quota.go`** (новый) — пакетный реестр мьютексов по root и логика подсчёта объёма:
  - `ErrQuotaExceeded` — sentinel-ошибка с нейтральным сообщением «upload denied: total upload quota exceeded» (без путей/чисел/секретов, SR-91/AC4).
  - `var rootLocks sync.Map` — package-level реестр `map[string]*sync.Mutex`; `rootMutex(absRoot)` через `LoadOrStore` возвращает единственный мьютекс на путь (SR-92/AC7).
  - `currentBytes(root *os.Root) (int64, error)` — суммирует `Size()` только regular-файлов через `fs.WalkDir(root.FS(), ".", …)`; симлинки и не-regular не разыменовываются (SR-93/AC6); любая ошибка обхода → fail-closed (SR-96/AC10).

- **`internal/fileupload/config.go`** (изменён) — добавлено поле `MaxTotalBytes int64` в `Config`; `Config` остаётся value-типом без полей синхронизации (SR-92/plan.md §Contracts, AC11).

- **`internal/fileupload/upload.go`** (изменён) — функция `Write` расширена квота-веткой:
  - При `MaxTotalBytes == 0` — ветка `writeUnderRoot` без мьютекса/обхода (AC2/SR-99).
  - При `MaxTotalBytes > 0` — `mu := rootMutex(cfg.UploadRoot); mu.Lock(); defer mu.Unlock()` ДО `os.OpenRoot`; root открывается ОДИН раз и переиспользуется для обхода и записи (SR-92/plan.md §Contracts).
  - Порядок проверок: `ErrIsDir` → `ErrExists` → `currentBytes` → квота-арифметика → `doWrite` (SR-95/plan.md §Contracts).
  - `replaced = fi.Size()` только при `Overwrite==true` AND regular-файл; иначе `replaced = 0` (AC8/SR-95).
  - `current - replaced + int64(len(in.Data)) > cfg.MaxTotalBytes` → `ErrQuotaExceeded` ДО создания temp (SR-90/AC3).
  - Ошибка `currentBytes` → fail-closed; запись не выполняется (SR-96/AC10).
  - Сигнатура `Write(cfg Config, in Input) (Result, error)` не изменилась (AC11).
  - Вынесена вспомогательная функция `doWrite` (переиспользуется обеими ветками).

- **`internal/config/config.go`** (изменён):
  - Добавлено поле `MaxTotalBytes int64` в `UploadConfig`.
  - `SetDefault("upload.max_total_bytes", int64(0))` — дефолт 0 = лимит отключён (AC1/AC2/SR-99).
  - В `buildConfig`: `if maxTotalBytes < 0 → ошибка старта` (AC1/SR-98); не связываем с `max_file_bytes` (Q2/AC9/AC10).

- **`internal/cli/serve.go`** (изменён) — проброс `cfg.Upload.MaxTotalBytes → uplCfg.MaxTotalBytes` (AC1).

- **`internal/mcp/upload_tool.go`** (изменён) — добавлен case `errors.Is(writeErr, fileupload.ErrQuotaExceeded)`:
  - `auditResult = "deny"`, `reason = "total upload quota exceeded"` (AC5/SR-94).
  - Нейтральный reason без путей/чисел/секретов (SR-91/AC4).
  - Ровно одна deny-запись на отклонение (AC5/SR-94).
  - Ошибка обхода (fail-closed, не `ErrQuotaExceeded`) попадает в `default` → `Result:"fail"` (SR-96b/AC10).
  - Существующие ветки и формат записей не изменены (AC11/SR-100).

- **`internal/fileupload/quota_test.go`** (новый) — TDD-тесты для всех AC (21 тест):
  - Написаны до реализации; каждый был RED до появления кода (TDD).

## Отклонения/эскалации

Отклонений нет. Реализация выполнена строго по `plan.md`.

## Тесты

### Что покрыто

| AC / SR | Тест | Описание |
|---------|------|----------|
| AC2/SR-99 | `TestQuota_ZeroDisabled` | MaxTotalBytes=0: 5 файлов проходят |
| AC3/SR-90 | `TestQuota_ExceedDenied` | Превышение → deny, файл не появляется |
| AC3/AC10 | `TestQuota_ExactlyAtLimit` | Ровно в лимит → OK (строгое >) |
| AC3/AC10 | `TestQuota_OneBeyondLimit` | На 1 байт больше → deny |
| AC4/SR-91 | `TestQuota_ErrorMessageNeutral` | Сообщение нейтральное |
| AC5/SR-94 | `TestQuota_SentinelError` | errors.Is(err, ErrQuotaExceeded) |
| AC6/SR-93 | `TestQuota_AccountsExistingFiles` | Доисторические файлы учитываются |
| AC6 | `TestQuota_AccountsSubdirectories` | Файлы в подкаталогах учитываются |
| SR-93 | `TestQuota_SymlinkNotFollowed` | Симлинк не считается |
| AC7/SR-92 | `TestQuota_ConcurrentSafety` | 10 горутин, итог <= 1000B |
| AC8/SR-95 | `TestQuota_OverwriteSameSize_OK` | Перезапись тот же размер → OK |
| AC8/SR-95 | `TestQuota_OverwriteSmallerSize_OK` | Перезапись меньше → OK |
| AC8/SR-95 | `TestQuota_OverwriteLarger_Denied` | Перезапись больше → deny, оригинал цел |
| AC9/SR-97 | `TestQuota_BothLimitsActive` | Проходит per-file, но превышает total → deny |
| AC9/SR-97 | `TestQuota_PerFileLimitStillActive` | Нарушает per-file → ErrTooLarge (регресс нет) |
| AC10 | `TestQuota_WritePreciselyIntoRemainder` | Запись «впритык» → OK |
| SR-95 | `TestQuota_IsDirBeforeQuota` | ErrIsDir до квоты |
| SR-95 | `TestQuota_ExistsBeforeQuota` | ErrExists до квоты |
| AC10/Q2 | `TestQuota_TotalSmallerThanPerFile` | 0<total<file: корректный deny |
| AC11/SR-100 | `TestQuota_NoRegression_BasicWrite` | Регресс базовой записи |
| AC11/SR-100 | `TestQuota_NoRegression_Traversal` | Traversal-защита при любом лимите |

Существующие тесты (`upload_test.go`, `mode_test.go`) — все зелёные без изменений.

### Команды запуска (в Docker, -mod=vendor)

```bash
# Сборка образа
docker build --target test -t raxd-test .

# go vet (все пакеты)
docker run --rm raxd-test go vet -mod=vendor ./...

# go test (все пакеты)
docker run --rm raxd-test go test -mod=vendor -v -count=1 ./...

# go test -race (fileupload, включая квота-тесты)
docker run --rm raxd-test sh -c "CGO_ENABLED=1 go test -mod=vendor -race -count=1 -v ./internal/fileupload/..."
```

### Подтверждение результата

- `go vet ./...` — чист, нет замечаний по копированию мьютекса или иному.
- `go test ./...` — все 11 пакетов PASS, ни одного FAIL.
- `go test -race ./internal/fileupload/...` — PASS, race-детектор не сработал.
- Тест `TestQuota_ConcurrentSafety` (10 горутин x 200B, лимит 1000B) — суммарный объём на диске <= 1000B.
- Все 21 новый тест из `quota_test.go` зелёные; ни одного `t.Skip`.

## Безопасность

### SR-90: Deny по квоте ДО фиксации
Проверка `current - replaced + int64(len(in.Data)) > cfg.MaxTotalBytes` в `writeWithQuota` (`upload.go`) выполняется до вызова `doWrite` (и значит, до создания temp-файла). При deny temp не создаётся; существующие файлы не изменяются. Тест: `TestQuota_ExceedDenied`, `TestQuota_OneBeyondLimit`.

### SR-91: Нейтральное сообщение
`ErrQuotaExceeded = errors.New("upload denied: total upload quota exceeded")` — без абсолютных путей, числовых значений объёма, содержимого файла или секретов. `upload_tool.go` использует `reason = "total upload quota exceeded"` в audit-записи. Тест: `TestQuota_ErrorMessageNeutral`.

### SR-92: TOCTOU закрыт мьютексом
`mu.Lock()` берётся ДО `os.OpenRoot`; `defer mu.Unlock()` срабатывает после фиксации ИЛИ любой ошибки. `sync.Map.LoadOrStore` гарантирует единственный `*sync.Mutex` на `UploadRoot` без гонок инициализации. `go vet` чист (мьютекс в `sync.Map`, не в `Config`). Тест: `TestQuota_ConcurrentSafety` (-race зелёный).

### SR-93: Только regular-файлы, симлинки не разыменовываются
`currentBytes` проверяет `d.Type().IsRegular()` и пропускает всё остальное. `fs.WalkDir(root.FS(), …)` работает через `*os.Root` — os.Root-инвариант SR-69 сохранён. Тест: `TestQuota_SymlinkNotFollowed`.

### SR-96: Fail-closed при ошибке обхода
Любая ошибка `WalkDir` или `d.Info()` возвращается из `currentBytes`; `writeWithQuota` возвращает обёрнутую ошибку, не выполняет запись. В `upload_tool.go` такая ошибка (не `ErrQuotaExceeded`) попадает в `default` → `Result:"fail"`.

### SR-98: Невалидный конфиг отвергается на старте
`buildConfig` в `config.go`: `if maxTotalBytes < 0 → ошибка`.

### SR-99: Дефолт 0 = нулевая цена
При `MaxTotalBytes == 0` `Write` сразу вызывает `writeUnderRoot` — без мьютекса, без обхода. Тест: `TestQuota_ZeroDisabled`.

### SR-100: Наследуемые контроли не изменены
Auth/Origin/rate-limit в транспортном слое не затронуты. Существующие ветки `uploadHandler` (traversal/exists/isdir/too-large/bad-mode) не изменены. Добавлен только один новый case. Тест: `TestQuota_NoRegression_Traversal`, `TestQuota_PerFileLimitStillActive`.

### SR-101: Все проверки в Docker, офлайн из vendor/
Сборка и тесты выполнены в Docker (`docker build --target test` + `docker run --rm`), офлайн из `vendor/` (`-mod=vendor`), без `go mod download`. Новых зависимостей нет (только stdlib `sync`, `io/fs`, `os`, `errors`). `go.mod` не изменён.

## Отклонения, завершённые после прерывания сессии (I-2, I-3 из qa)

### I-2 (doc): ветвление от main — норма для данного репозитория

На remote опубликован только `main` (релиз v0.1.0 из main; ветки `develop` на remote нет).
Ветки feature ветвятся от `main` и вливаются в `main` — это решение дирижёра, реализующее
«master-as-trunk» из GIT-FLOW-GUIDE (не нарушение). Ветка `feature/upload-quota` от `main` корректна.

### I-3 (SR-90): MkdirAll ПОСЛЕ квота-проверки

До исправления: `writeWithQuota` выполнял `root.MkdirAll(dir, 0o700)` ДО квота-арифметики
→ при deny по квоте промежуточный подкаталог оставался на диске (нарушение SR-90 «без следов при deny»).

Исправление: `MkdirAll` перенесён ПОСЛЕ `if current-replaced+len(data) > max` в `writeWithQuota`.
Порядок теперь строго:
1. `ErrIsDir` / `ErrExists` (root.Stat)
2. `currentBytes` → ошибка обхода → fail-closed
3. квота-арифметика → `ErrQuotaExceeded` (если deny)
4. `MkdirAll` (только при approved)
5. `doWrite` (temp→chmod→write→sync→rename→fsync-dir)

`writeUnderRoot` (ветка без квоты) не затронут — там нет `ErrQuotaExceeded`, порядок без изменений.

Тест: `TestQuota_DenyBeforeMkdirAll` — deny при путь с новым подкаталогом → `subdir` НЕ создан.

### #2 (SR-96): fail-closed без молчаливого skip

`TestQuota_FailClosedOnWalkError` переписан: убран `t.Skip` при `euid==0`.
Реализован вариант (a): в `quota.go` добавлена package-level переменная `currentBytesHook`
(nil в production); `export_test.go` (package `fileupload`, компилируется только в тест-сборке)
экспортирует `SetCurrentBytesHook`. Тест детерминированно инжектирует ошибку обхода через хук
без зависимости от uid. Работает в Docker от root (канонический прогон). Без `t.Skip`.

## Ветка и коммиты

Ветка: `feature/upload-quota` (от `main`; см. I-2 выше).

Коммиты в `feature/upload-quota`:
- `26bf759` test(fileupload): TDD-тесты для общего лимита объёма upload root (upload-quota)
- `794a595` feat(fileupload/config): добавить поле MaxTotalBytes в Config (upload-quota AC1)
- `9f8b013` feat(fileupload/upload): интегрировать квота-проверку в Write (upload-quota AC3/AC7/AC8)
- `c2b48a0` feat(config): добавить upload.max_total_bytes, дефолт 0, валидация <0 → ошибка старта (AC1/SR-98)
- `61cab50` feat(cli/serve): пробросить MaxTotalBytes из UploadConfig в fileupload.Config (upload-quota AC1)
- `71974d3` feat(mcp/upload_tool): маппить ErrQuotaExceeded → deny + нейтральный reason (upload-quota AC5/SR-94)
- `588a3e8` chore(specs/upload-quota): добавить артефакты задачи
- `9453bbb` docs(specs/upload-quota): impl-notes — что реализовано, тесты, безопасность
(хэши коммитов qa/завершения сессии — добавляются после коммита)
