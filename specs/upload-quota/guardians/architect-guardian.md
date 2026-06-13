# architect-guardian — upload-quota

**Verdict: pass** (повторная проверка после доработки)

Артефакт: `specs/upload-quota/plan.md`. Подход не менялся (пересчёт объёма обходом `os.Root.FS()`+`fs.WalkDir` + один `sync.Mutex` на абсолютный UploadRoot). Три прошлых issue устранены:

1. **Владение мьютексом** — зафиксирована ровно одна схема: package-level `var rootLocks sync.Map` + `rootMutex(absRoot) *sync.Mutex` через `LoadOrStore`, `Write` сам резолвит. Развилка и делегирование developer убраны. `Config` остаётся value-типом.
2. **Границы критической секции** — `mu.Lock(); defer mu.Unlock()` до `os.OpenRoot`; root открывается один раз и переиспользуется для обхода и записи; `currentBytes` вызывается под удержанным мьютексом; вся запись temp→Rename→fsync-dir под мьютексом.
3. **Overwrite-дельта** — каталог→ErrIsDir, `!overwrite&&exists`→ErrExists ДО квота-арифметики; `replaced=Size()` только при Overwrite==true И существующем regular-файле.

Инварианты целы: os.Root traversal (SR-69), атомарность, AC9/AC11; новых зависимостей нет (stdlib sync/io/fs/errors). Готов к следующему гейту (security).
