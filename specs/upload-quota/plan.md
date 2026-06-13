# Plan: upload-quota — общий лимит объёма upload root

Автор продукта: Vladimir Kovalev, OEM TECH

## Chosen Approach
Подсчёт суммарного объёма — **пересчётом обходом upload root на каждую загрузку** (`fs.WalkDir`
через `os.Root.FS()`), а НЕ поддерживаемым in-memory счётчиком. Источник истины — сама ФС: это
единственный способ авторитетно учесть файлы, записанные до включения лимита, в подкаталогах и вне
жизни процесса (AC6), без durable-персистенции счётчика и логики его реконсиляции. Конкурентность
(AC7) закрывается **одним `sync.Mutex` на абсолютный `cfg.UploadRoot`**, который `Write` сам резолвит
из package-level реестра (`sync.Map`); под этим мьютексом атомарно выполняется «открытие root → обход
→ проверка дельты → вся существующая запись temp→fsync→rename». Так как проверка идёт ДО создания
temp-файла, отказ по квоте не оставляет ни temp, ни целевого файла (AC3/AC10). Лимит — независимая
страховка поверх `max_file_bytes` (оба действуют, AC9); FS/ОС-quota не используется (риск закрывается
на уровне приложения, см. Out of Scope spec).

## Modules
- `internal/fileupload/quota.go` — НОВЫЙ: package-level реестр мьютексов по root, типизированная
  ошибка `ErrQuotaExceeded`, обход root (`currentBytes`), вычисление overwrite-дельты, fail-closed при
  ошибке обхода. Инкапсулирует всю логику квоты; без MCP/SDK/логирования.
- `internal/fileupload/config.go` — ИЗМЕНИТЬ: добавить поле `MaxTotalBytes int64` в `Config`.
- `internal/fileupload/upload.go` — ИЗМЕНИТЬ: `Write` под мьютексом root выполняет проверку лимита ДО
  создания temp-файла, затем существующую атомарную запись (контракт `Write` не меняется внешне).
- `internal/config/config.go` — ИЗМЕНИТЬ: поле `MaxTotalBytes` в `UploadConfig`, дефолт
  `upload.max_total_bytes=0`, валидация (≥0) в upload-блоке `buildConfig`.
- `internal/cli/serve.go` — ИЗМЕНИТЬ: проброс `MaxTotalBytes` в `fileupload.Config{...}`.
- `internal/mcp/upload_tool.go` — ИЗМЕНИТЬ: маппинг `ErrQuotaExceeded` → `deny` + нейтральный reason.

## Contracts
- `Config.MaxTotalBytes int64` (`config.go`) — общий лимит upload root в байтах. `0` = лимит отключён
  (AC2). Заполняется из `upload.max_total_bytes`. `Config` остаётся value-типом без полей синхронизации.
- `var ErrQuotaExceeded = errors.New(...)` (`quota.go`) — типизированная sentinel-ошибка отказа по
  общему лимиту. Сообщение нейтральное: «upload denied: total upload quota exceeded» — без абсолютных
  путей/чисел/секретов (AC4/Q6/SR-80). Handler маппит её в `Result:"deny"`.
- **Владение мьютексом (РОВНО ОДНА схема, AC7)** (`quota.go`): package-level `var rootLocks sync.Map`
  (`map[string]*sync.Mutex`), плюс `rootMutex(absRoot string) *sync.Mutex`, который через
  `LoadOrStore` возвращает единственный мьютекс для данного абсолютного пути root. `Write` сам зовёт
  `rootMutex(cfg.UploadRoot)` — один и тот же мьютекс на root независимо от вызывающего. НЕ через
  `Config` (value-тип; `sync.Mutex` в нём ломает копирование и ловится `go vet`) и НЕ параметром
  `Write` (сигнатура неизменна). Реестр-выбор зафиксирован планом, не делегируется developer.
- `Write(cfg Config, in Input) (Result, error)` (`upload.go`) — сигнатура НЕ меняется (AC11).
  При `cfg.MaxTotalBytes == 0` — мьютекс/обход не задействуются, поведение прежнее (AC2).
  При `cfg.MaxTotalBytes > 0` критическая секция строго ограничена так:
  1. `mu := rootMutex(cfg.UploadRoot); mu.Lock(); defer mu.Unlock()` — Lock берётся ДО `os.OpenRoot`;
     Unlock через `defer` срабатывает после фиксации ИЛИ любой ошибки (AC7);
  2. `os.OpenRoot(cfg.UploadRoot)` открывается ОДИН раз и переиспользуется и для обхода, и для всей
     записи (один `*os.Root` на критическую секцию; SR-69/traversal-инвариант сохранён);
  3. ранний лексический `filepath.IsLocal` и определение существующей цели/`overwrite`-веток
     (`ErrExists`/`ErrIsDir`) — см. порядок ниже;
  4. `current := currentBytes(root)` (под удержанным мьютексом), вычисление `replaced` (см. ниже);
     если `current - replaced + int64(len(in.Data)) > cfg.MaxTotalBytes` → вернуть `ErrQuotaExceeded`
     ДО создания temp;
  5. иначе — существующая атомарная запись temp→Chmod→Write→Sync→Rename→fsync-dir, вся под тем же
     удержанным мьютексом.
  Ошибки: `ErrQuotaExceeded` (превышение), обёрнутая ошибка обхода (fail-closed, ниже), прежние
  `ErrTraversal/ErrExists/ErrIsDir/ErrTooLarge`/I-O.
- **Порядок проверок и overwrite-дельта** (`upload.go`): после `root.Stat(in.RelPath)`:
  - цель существует и `fi.IsDir()` → `ErrIsDir` (ДО квота-арифметики; дельта не считается);
  - цель существует, `!in.Overwrite` → `ErrExists` (ДО квота-арифметики);
  - `replaced = fi.Size()` ТОЛЬКО когда `in.Overwrite == true` И `root.Stat` дал существующий
    **regular**-файл (не каталог); во ВСЕХ остальных случаях (новый файл / Stat-ошибка `not exist`)
    `replaced = 0`. Так перезапись учитывается дельтой (AC8), а новый файл — полным размером.
- `currentBytes(root *os.Root) (int64, error)` (`quota.go`) — вызывается УЖЕ под удержанным
  мьютексом root (не лочит сам). Суммирует `Size()` всех regular-файлов рекурсивно (включая
  подкаталоги) через `fs.WalkDir(root.FS(), ".", …)`. Симлинки/не-regular не учитываются и не
  разыменовываются (os.Root-инвариант). Любая ошибка обхода/`Info()`/`Stat` → возврат наверх как
  **fail-closed**: `Write` НЕ выполняет запись и возвращает ошибку (Q4) — молчаливого обхода нет.
- `uploadHandler` ветка `fileupload.Write`-ошибки (`upload_tool.go`) — добавить case
  `errors.Is(writeErr, fileupload.ErrQuotaExceeded)` → `auditResult="deny"`,
  `reason="total upload quota exceeded"`. Пишется РОВНО одна основная deny-запись
  `AuditRecord{Result:"deny", Tool:"upload_file", Path:input.Path, Reason, fp, remote, TS:UTC}` (AC5);
  `isError:true` через возврат `error` (AC3). Существующие ветки и формат не меняются (AC11). Ошибка
  обхода (fail-closed, не `ErrQuotaExceeded`) попадает в `default` → `Result:"fail"` (I/O-семантика),
  сервер жив (AC10).
- `buildConfig` upload-валидация (`config.go`) — `v.SetDefault("upload.max_total_bytes", int64(0))`;
  `mt := v.GetInt64("upload.max_total_bytes")`; если `mt < 0` → ошибка старта (как `max_file_bytes`,
  AC1). НЕ связывать с `max_file_bytes` (AC9): значение `0 < max_total_bytes < max_file_bytes`
  допускается (Q2) — крупный файл тогда корректно отклонится по общему лимиту (deny), сервер жив (AC10).

## Закрытие Open Questions
- **Q1**: имя/единица — `upload.max_total_bytes`, целое, **байты** (как `max_file_bytes`).
- **Q2**: `0 < max_total_bytes < max_file_bytes` **допускается** на старте (не связываем лимиты, AC9);
  такой файл штатно отклоняется deny по общему лимиту во время загрузки (AC10).
- **Q3**: граница — **строгое `>`** для отказа: загрузка проходит, пока `итог <= max_total_bytes`, и
  отклоняется при `итог > max_total_bytes` (значение «ровно лимит» разрешено; AC3/AC7/AC10-«ровно
  достигнут»).
- **Q4**: ошибка обхода/подсчёта — **fail-closed**: запись не выполняется, возвращается ошибка
  (→ `fail`-аудит). Молчаливый обход лимита исключён.
- **Q5**: **пересчёт обходом на каждую загрузку** (не счётчик) + **один `sync.Mutex` на abs `UploadRoot`**
  из package-level `sync.Map`, под которым атомарно «открытие root → обход → проверка → запись».
- **Q6**: сообщение и аудит сообщают **только факт исчерпания квоты**, без абсолютных путей и без
  точных чисел текущего/предельного объёма (AC4/SR-80); относительный `Path` в аудите допустим (AC5).

## Trade-offs
- Выбрали **пересчёт обходом под мьютексом** вместо **in-memory счётчика с инициализацией обходом при
  старте**. Цена: O(N) обход root на каждую загрузку и сериализация конкурентных `upload_file` (мьютекс
  глушит параллелизм записи в один root). Платим производительностью загрузок ради авторитетности
  (всегда учитывает внешние/доисторические файлы — AC6) и простоты корректности (нет дрейфа счётчика,
  нет durable-состояния, нет реконсиляции после краша). Для одиночного клиента с мелкими файлами
  объём root скромен, а upload — редкая операция, так что цена приемлема.
- Выбрали **реестр мьютексов по абс. `UploadRoot` (`sync.Map`)** вместо хранения `*Quota`/`sync.Mutex`
  в `Config`. Цена: package-level состояние и ключ-строка вместо явного объекта; зато `Config` остаётся
  копируемым value-типом (чисто для `go vet`) и сигнатура `Write` неизменна (AC11), а сериализация
  гарантированно привязана к root, а не к вызывающему.
- Отвергли **FS/ОС-quota (filesystem quota / cgroup / `--storage-opt`)**: это эксплуатационная мера ОС,
  не артефакт задачи (Out of Scope spec); решаем на уровне приложения — переносимо (macOS+Linux) и
  тестируемо в Docker офлайн.
- **Новых зависимостей нет** (AC12): только stdlib `sync`, `io/fs`, `errors` — сверено со STACK.ru.md.
