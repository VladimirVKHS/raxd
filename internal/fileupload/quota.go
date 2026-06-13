package fileupload

// quota.go — общий лимит объёма upload root (upload-quota).
//
// БЕЗОПАСНОСТЬ:
//   - SR-90: deny ДО фиксации — проверка перед созданием temp-файла.
//   - SR-91: ErrQuotaExceeded — нейтральное сообщение без путей/чисел/секретов.
//   - SR-92: один sync.Mutex на абсолютный UploadRoot через package-level sync.Map;
//            мьютекс НЕ хранится в Config (value-тип, go vet чист).
//   - SR-93: currentBytes суммирует только regular-файлы; симлинки не разыменовываются.
//   - SR-96: любая ошибка обхода → fail-closed (запись не выполняется).
//   - SR-99: при MaxTotalBytes==0 мьютекс/обход не задействуются (нулевая цена).
//
// Без MCP/SDK/логирования. Юнит-тестируем офлайн.

import (
	"errors"
	"io/fs"
	"os"
	"sync"
)

// ErrQuotaExceeded возвращается Write при превышении суммарного лимита upload root.
// Нейтральное сообщение — без абсолютных путей, числовых значений объёма и секретов (SR-91/AC4/Q6).
// Handler маппит в Result:"deny" (AC5/SR-94).
var ErrQuotaExceeded = errors.New("upload denied: total upload quota exceeded")

// rootLocks — package-level реестр мьютексов, ключ = абсолютный cfg.UploadRoot.
// Единственный мьютекс на root-путь независимо от вызывающего (SR-92/plan.md §Contracts).
// SECURITY: sync.Map безопасна для конкурентного доступа; LoadOrStore гарантирует
// единственный экземпляр *sync.Mutex на путь (без race при инициализации).
var rootLocks sync.Map // map[string]*sync.Mutex

// rootMutex возвращает единственный *sync.Mutex для данного абсолютного пути root.
// Используется только Write; вызывается при MaxTotalBytes > 0 (SR-99).
// SECURITY: LoadOrStore — атомарная операция; гонки инициализации нет (SR-92).
func rootMutex(absRoot string) *sync.Mutex {
	mu := &sync.Mutex{}
	actual, _ := rootLocks.LoadOrStore(absRoot, mu)
	return actual.(*sync.Mutex)
}

// currentBytes рекурсивно суммирует размер всех regular-файлов в root.FS().
// Вызывается УЖЕ под удержанным мьютексом root (не лочит сам).
//
// SECURITY:
//   - SR-93: только regular-файлы (d.Type().IsRegular()); симлинки и не-regular
//     (каталоги, устройства, сокеты) НЕ разыменовываются и НЕ учитываются.
//   - SR-96 (fail-closed): ЛЮБАЯ ошибка WalkDir/Info → возврат ошибки наверх;
//     вызывающий Write НЕ выполняет запись при ошибке обхода.
//   - os.Root-инвариант SR-69 сохранён: обход через root.FS() не выходит за корень.
func currentBytes(root *os.Root) (int64, error) {
	var total int64
	err := fs.WalkDir(root.FS(), ".", func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Любая ошибка при обходе → fail-closed (SR-96).
			return walkErr
		}
		// Считаем ТОЛЬКО regular-файлы (SR-93).
		// d.Type().IsRegular() исключает каталоги, симлинки, устройства и т.д.
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			// Ошибка Info() → fail-closed (SR-96).
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}
