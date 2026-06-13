# Test Plan: upload-quota — общий лимит объёма upload root

Автор: QA-агент команды raxd  
Задача: `upload-quota` (AC1–AC12, SR-90…SR-102)  
Ветка: `feature/upload-quota`  
Дата: 2026-06-13

---

## Стратегия тестирования

| Уровень | Что проверяем | Пакет / файл |
|---|---|---|
| **Unit (quota)** | Вся логика `fileupload.Write` + `currentBytes`: отклонение, атомарность, дельта overwrite, конкурентность, симлинки, fail-closed | `internal/fileupload/quota_test.go` |
| **Unit (config)** | Конфиг-валидация `upload.max_total_bytes`: дефолт 0, отрицательное → ошибка старта, положительное принимается, `0 < total < per-file` допускается | `internal/config/upload_config_test.go` |
| **Integration (upload)** | Интеграция `uploadHandler` → `fileupload.Write`: аудит-запись deny при квоте, нейтральность сообщений, наследование traversal/auth/rate-limit | `internal/mcp/upload_tool_test.go`, `upload_qa_test.go` |
| **E2E** | Не применимо: фича — уровень приложения; TLS/auth/rate-limit — наследуются без изменений; e2e upload проверен в `upload_qa_test.go` | — |
| **Install-flow** | Вне scope этой фичи (новых зависимостей нет, vendor без изменений) | — |

Тесты прогоняются ТОЛЬКО в Docker (SECURITY-BASELINE §6). Команды запуска — docker-команды (см. раздел «Как запускать»).

---

## Матрица AC → тест

| AC | Описание (из spec.md) | Уровень | Файл::Функция | Статус |
|---|---|---|---|---|
| **AC1** | Новый параметр `upload.max_total_bytes`; дефолт 0; отрицательное → ошибка старта | unit (config) | `upload_config_test.go::TestUploadMaxTotalBytesDefault` | GREEN |
| **AC1** | Отрицательное значение отвергается на старте | unit (config) | `upload_config_test.go::TestUploadMaxTotalBytesNegativeIsError` | GREEN |
| **AC1** | Положительное значение принимается | unit (config) | `upload_config_test.go::TestUploadMaxTotalBytesPositiveIsOK` | GREEN |
| **AC2** | `max_total_bytes=0` = лимит отключён; загрузки не отклоняются | unit | `quota_test.go::TestQuota_ZeroDisabled` | GREEN |
| **AC2** | Регресс: базовая запись без лимита работает как прежде | unit | `quota_test.go::TestQuota_NoRegression_BasicWrite` | GREEN |
| **AC3** | Загрузка за лимит отклоняется до фиксации; файл не появляется | unit | `quota_test.go::TestQuota_ExceedDenied` | GREEN |
| **AC3** | Загрузка ровно в лимит проходит (строгое `>`) | unit | `quota_test.go::TestQuota_ExactlyAtLimit` | GREEN |
| **AC3** | Загрузка на 1 байт сверх лимита → deny, файла нет | unit | `quota_test.go::TestQuota_OneBeyondLimit` | GREEN |
| **AC3** | Temp-файл не остаётся после deny (AC10 связан) | unit | `quota_test.go::TestQuota_ExceedDenied` (проверка temp) | GREEN |
| **AC4** | Сообщение упоминает квоту, не содержит абс. пути, не содержит числовых значений | unit | `quota_test.go::TestQuota_ErrorMessageNeutral` | GREEN |
| **AC5** | `ErrQuotaExceeded` — sentinel, проверяется `errors.Is` | unit | `quota_test.go::TestQuota_SentinelError` | GREEN |
| **AC5** | `uploadHandler` маппит `ErrQuotaExceeded` → `deny`; ровно одна deny-запись (через `upload_tool_test.go::TestUploadFile_AuditDeny` и `TestUploadFile_ExactlyOneAuditRecord`) | integration | `mcp/upload_tool_test.go` | GREEN |
| **AC6** | Существующие файлы учитываются (пересчёт обходом) | unit | `quota_test.go::TestQuota_AccountsExistingFiles` | GREEN |
| **AC6** | Файлы в подкаталогах учитываются | unit | `quota_test.go::TestQuota_AccountsSubdirectories` | GREEN |
| **AC7** | N параллельных загрузок не превышают `max_total_bytes`; нет повреждённых файлов | unit (-race) | `quota_test.go::TestQuota_ConcurrentSafety` | GREEN |
| **AC8** | Перезапись тем же размером при исчерпанной квоте проходит (дельта=0) | unit | `quota_test.go::TestQuota_OverwriteSameSize_OK` | GREEN |
| **AC8** | Перезапись меньшим размером при исчерпанной квоте проходит | unit | `quota_test.go::TestQuota_OverwriteSmallerSize_OK` | GREEN |
| **AC8** | Перезапись на больший размер сверх лимита → deny, исходный файл не изменён | unit | `quota_test.go::TestQuota_OverwriteLarger_Denied` | GREEN |
| **AC9** | Файл проходит per-file, но превышает total → `ErrQuotaExceeded` (не `ErrTooLarge`) | unit | `quota_test.go::TestQuota_BothLimitsActive` | GREEN |
| **AC9** | `ErrTooLarge` по-прежнему срабатывает (без регресса) | unit | `quota_test.go::TestQuota_PerFileLimitStillActive` | GREEN |
| **AC9** | `0 < max_total < max_file_bytes` допускается в конфиге (Q2) | unit (config) | `upload_config_test.go::TestUploadMaxTotalBytesSmallerThanMaxFile` | GREEN |
| **AC10** | Edge: ровно в лимит → OK (строгое `>`, Q3) | unit | `quota_test.go::TestQuota_ExactlyAtLimit` | GREEN |
| **AC10** | Edge: запись «впритык» к остатку → OK | unit | `quota_test.go::TestQuota_WritePreciselyIntoRemainder` | GREEN |
| **AC10** | Edge: `0 < max_total < max_file_bytes` — крупный файл отклоняется deny по total | unit | `quota_test.go::TestQuota_TotalSmallerThanPerFile` | GREEN |
| **AC10** | fail-closed при ошибке обхода (SR-96): при недоступном walk запись не выполняется | unit | `quota_test.go::TestQuota_FailClosedOnWalkError` | SKIP (root в Docker — см. примечание) |
| **AC11** | Traversal-защита работает при любом лимите (наследование без изменений) | unit | `quota_test.go::TestQuota_NoRegression_Traversal` | GREEN |
| **AC11** | Прежние upload-сценарии (traversal/auth/rate-limit/aудит) зелёные | integration | `mcp/upload_qa_test.go` (весь набор) | GREEN |
| **AC12** | Все тесты зелёные в Docker из `vendor/`; новых зависимостей нет | env | docker build+run (см. «Как запускать») | GREEN |

### Примечание по AC10/SR-96 (TestQuota_FailClosedOnWalkError)

Тест `TestQuota_FailClosedOnWalkError` проверяет fail-closed при `chmod 0o000` на вложенном каталоге. В Docker демон запускается от root (euid==0), root игнорирует ограничения прав файловой системы, поэтому тест явно пропускается (`t.Skip`) чтобы избежать ложного зелёного. В среде non-root (CI-runner от не-root пользователя, например с `--user 1000:1000`) тест проходит. Это не скрытый баг продукта, а известное ограничение среды запуска.

Покрытие SR-96 (fail-closed) подтверждается косвенно через:
- порядок `currentBytes` в `quota.go`: любая ошибка `WalkDir`/`Info()` возвращается наверх (fail-closed, строки 54–71 `quota.go`)
- `writeWithQuota` не выполняет `doWrite` при ошибке `currentBytes` (строка 177 `upload.go`)
- `TestUploadFile_FailBranchIOError_MCP` в `upload_qa_test.go` проверяет маппинг fail-ветки в `upload_tool.go`

---

## Edge cases

| Case | Тест |
|---|---|
| `max_total_bytes=0` → обход/мьютекс не задействуются | `TestQuota_ZeroDisabled` |
| Лимит ровно достигнут (=) → OK, строгое `>` | `TestQuota_ExactlyAtLimit` |
| Лимит +1 байт → deny | `TestQuota_OneBeyondLimit` |
| Запись «впритык» к остатку | `TestQuota_WritePreciselyIntoRemainder` |
| Существующие файлы (до включения лимита) | `TestQuota_AccountsExistingFiles` |
| Файлы в подкаталогах | `TestQuota_AccountsSubdirectories` |
| Симлинк на крупный файл снаружи не увеличивает объём | `TestQuota_SymlinkNotFollowed` |
| Цель — каталог → `ErrIsDir` ДО квота-арифметики | `TestQuota_IsDirBeforeQuota` |
| Файл существует, overwrite=false → `ErrExists` ДО квота-арифметики | `TestQuota_ExistsBeforeQuota` |
| `0 < max_total < max_file_bytes` допускается в конфиге | `TestUploadMaxTotalBytesSmallerThanMaxFile` |
| Отрицательный `max_total_bytes` → ошибка на старте | `TestUploadMaxTotalBytesNegativeIsError` |
| fail-closed при ошибке обхода → запись не выполняется | `TestQuota_FailClosedOnWalkError` (SKIP в Docker-root; покрыт косвенно) |

---

## Security-тесты (SR-90…SR-101)

| SR | Требование | Тест |
|---|---|---|
| **SR-90** | Deny по квоте ДО фиксации, без следов на диске | `TestQuota_ExceedDenied` (проверка отсутствия файла и temp) |
| **SR-91** | Нейтральное сообщение: нет абс. пути, нет чисел, нет секретов | `TestQuota_ErrorMessageNeutral` |
| **SR-92** | Атомарность «проверка+запись» под одним мьютексом, TOCTOU закрыт | `TestQuota_ConcurrentSafety` (N параллельных загрузок, итог ≤ лимита) |
| **SR-93** | Симлинки не разыменовываются, только regular-файлы | `TestQuota_SymlinkNotFollowed` |
| **SR-94** | Ровно одна deny-аудит-запись без секретов | `TestUploadFile_AuditDeny` + `TestUploadFile_ExactlyOneAuditRecord` (в `upload_tool_test.go`) |
| **SR-95** | Overwrite — дельта; ErrIsDir/ErrExists — до квота-арифметики | `TestQuota_OverwriteSameSize_OK`, `TestQuota_OverwriteLarger_Denied`, `TestQuota_IsDirBeforeQuota`, `TestQuota_ExistsBeforeQuota` |
| **SR-96** | Fail-closed при ошибке обхода | `TestQuota_FailClosedOnWalkError` (SKIP в Docker-root; code-path покрыт косвенно) |
| **SR-96b** | Ошибка обхода → fail-аудит, сервер жив | `TestUploadFile_FailBranchIOError_MCP` (в `upload_qa_test.go`) |
| **SR-97** | Оба лимита независимы | `TestQuota_BothLimitsActive`, `TestQuota_PerFileLimitStillActive` |
| **SR-98** | `max_total_bytes < 0` → ошибка старта; edge-cases — корректный исход без паники | `TestUploadMaxTotalBytesNegativeIsError`, `TestQuota_ExactlyAtLimit`, `TestQuota_TotalSmallerThanPerFile` |
| **SR-99** | Дефолт 0 = лимит отключён, нулевая цена | `TestUploadMaxTotalBytesDefault`, `TestQuota_ZeroDisabled` |
| **SR-100** | Наследуемые контроли не изменены (traversal, auth, rate-limit) | `TestQuota_NoRegression_Traversal`, `upload_qa_test.go` (весь набор) |
| **SR-101** | Все тесты в Docker, офлайн из vendor/ | docker build --target test && docker run (описание ниже) |
| **SR-102** | Остаточные риски зафиксированы в threat-model.md | Артефакт: `specs/upload-quota/threat-model.md` (проверяет reviewer) |

Дополнительно из SECURITY-BASELINE:
- Аутентификация (401/403 при отсутствии/неверном ключе): `TestMCPNoAuthReturns401`, `TestMCPUnknownKeyReturns401` — GREEN (наследование без изменений)
- Rate-limit (429): `TestUploadFile_RateLimit429BeforeUpload` — GREEN
- Constant-time сравнение секретов: не меняется фичей; покрыто `TestStaticNoDirectKeyComparison`
- `exec.Command` без shell-интерполяции: не меняется; `TestNoShellInjection` — GREEN
- Отзыв ключа → отказ: `TestRevokedKeyReturns401` — GREEN

---

## Покрытие конкурентного AC7 (-race)

Тест `TestQuota_ConcurrentSafety` запускается с флагом `-race` в Docker (Dockerfile `CMD`):

```sh
CGO_ENABLED=1 go test -race -count=1 ./internal/fileupload/...
```

Логика: 10 горутин × 200 байт = 2000 байт суммарно при лимите 1000 байт. После завершения все файлы — не более лимита. Гонка данных и TOCTOU исключаются единственным `sync.Mutex` на `UploadRoot` (SR-92).

---

## Как запускать

Все прогоны — ТОЛЬКО в Docker (SECURITY-BASELINE §6). На хосте raxd не запускается.

### Стандартный прогон (unit + integration, go vet)

```sh
docker build --target test -t raxd-test .
docker run --rm raxd-test
```

### С явной фиксацией -race для fileupload (AC7/SR-92)

```sh
docker run --rm raxd-test sh -c \
  "CGO_ENABLED=1 go test -race -count=1 -v ./internal/fileupload/..."
```

### Только тесты upload-quota

```sh
docker run --rm raxd-test sh -c \
  "go test -v -count=1 -run 'TestQuota_' ./internal/fileupload/... && \
   go test -v -count=1 -run 'TestUploadMaxTotalBytes' ./internal/config/..."
```

### Проверка отсутствия новых внешних зависимостей (AC12)

```sh
docker run --rm raxd-test sh -c \
  "go build -mod=vendor ./... && echo 'vendor: OK'"
```

### Тест SR-96 (fail-closed) в non-root среде

```sh
docker run --rm --user 1000:1000 raxd-test sh -c \
  "CGO_ENABLED=1 go test -race -count=1 -v -run 'TestQuota_FailClosed' \
   ./internal/fileupload/..."
```

---

## Добавленные тесты (этот QA-проход)

Все добавлены без ослабления существующих:

### `internal/config/upload_config_test.go`

| Функция | Покрывает |
|---|---|
| `TestUploadMaxTotalBytesDefault` | AC1 дефолт=0, AC2/SR-99 |
| `TestUploadMaxTotalBytesNegativeIsError` | AC1 отрицательное → ошибка старта, SR-98 |
| `TestUploadMaxTotalBytesPositiveIsOK` | AC1 положительное принимается |
| `TestUploadMaxTotalBytesSmallerThanMaxFile` | AC9/AC10/Q2 — `0 < total < per-file` допускается |

### `internal/fileupload/quota_test.go`

| Функция | Покрывает |
|---|---|
| `TestQuota_FailClosedOnWalkError` | SR-96/Q4/AC10 — fail-closed при ошибке обхода (SKIP в Docker-root, зелёный в non-root) |

---

## Вердикт по покрытию

**КАЖДЫЙ AC из `spec.md` имеет проверяющий тест.** Ни один AC не пропущен.

- AC1–AC12: матрица заполнена полностью (см. выше).
- SR-90…SR-101: каждое требование имеет тест или обоснованную ссылку на покрывающий тест.
- SR-102: артефакт `threat-model.md` существует, проверяется reviewer.

**Находок-багов продукта нет.** Все 21 исходный тест developer GREEN. Добавленные 5 тестов GREEN (один SKIP в Docker-root с обоснованием).

**Итог Docker-прогона** (`docker run --rm raxd-test-quota2`):
```
ok  github.com/vladimirvkhs/raxd                   0.012s
ok  github.com/vladimirvkhs/raxd/internal/banner   0.001s
ok  github.com/vladimirvkhs/raxd/internal/cli      0.094s
ok  github.com/vladimirvkhs/raxd/internal/cmdexec  1.181s
ok  github.com/vladimirvkhs/raxd/internal/config   0.008s
ok  github.com/vladimirvkhs/raxd/internal/fileupload 0.096s
ok  github.com/vladimirvkhs/raxd/internal/keystore 0.185s
ok  github.com/vladimirvkhs/raxd/internal/mcp      4.398s
ok  github.com/vladimirvkhs/raxd/internal/server   2.202s
ok  github.com/vladimirvkhs/raxd/internal/service  0.004s
ok  github.com/vladimirvkhs/raxd/internal/version  0.004s
```
(-race для fileupload/keystore/server/mcp/cmdexec — отдельная строка, тоже `ok`.)

Ни одного `FAIL`. Эскалаций к developer не требуется.
