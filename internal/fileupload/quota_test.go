package fileupload_test

// quota_test.go — TDD-тесты для общего лимита объёма upload root (upload-quota).
//
// Покрывают AC1–AC12 и SR-90…SR-101:
//   - AC1/SR-98:  конфиг MaxTotalBytes, дефолт 0, отрицательное — ошибка.
//   - AC2/SR-99:  дефолт 0 = лимит отключён; мьютекс/обход не задействуются.
//   - AC3/SR-90:  отклонение до фиксации; файл на диске не появляется.
//   - AC4/SR-91:  нейтральное сообщение без путей/чисел/секретов.
//   - AC5/SR-94:  ErrQuotaExceeded — sentinel, можно проверить через errors.Is.
//   - AC6/SR-93:  учёт существующих файлов (пересчёт обходом).
//   - AC7/SR-92:  конкурентная безопасность: N параллельных загрузок, итог ≤ лимита.
//   - AC8/SR-95:  overwrite — дельта; перезапись в рамках — ОК; с превышением — deny.
//   - AC9/SR-97:  оба лимита независимы.
//   - AC10/SR-98: edge-cases: ровно в лимит, «впритык».
//   - SR-93:      симлинк в root не разыменовывается при обходе.
//   - SR-96:      fail-closed при ошибке обхода.
//
// Все тесты офлайн (только tmpdir); запускаются в Docker -mod=vendor.
// БЕЗОПАСНОСТЬ: тесты НЕ запускают raxd на хосте (baseline §6).

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/fileupload"
)

// quotaCfg возвращает Config с заданным MaxTotalBytes для тестов квоты.
func quotaCfg(t *testing.T, maxTotal int64) fileupload.Config {
	t.Helper()
	root := t.TempDir()
	return fileupload.Config{
		UploadRoot:    root,
		MaxFileBytes:  716800, // 700 KiB
		DefaultMode:   0o600,
		DenyRoot:      false,
		MaxTotalBytes: maxTotal,
	}
}

// ─── AC2/SR-99: дефолт 0 = лимит отключён ───────────────────────────────────

// TestQuota_ZeroDisabled: при MaxTotalBytes=0 любое количество файлов проходит (AC2/SR-99).
func TestQuota_ZeroDisabled(t *testing.T) {
	cfg := quotaCfg(t, 0) // 0 = лимит выключен

	for i := 0; i < 5; i++ {
		fname := strings.Repeat("a", i+1) + ".txt"
		_, err := fileupload.Write(cfg, fileupload.Input{
			RelPath:   fname,
			Data:      make([]byte, 1000),
			Overwrite: false,
			Mode:      0o600,
		})
		if err != nil {
			t.Fatalf("quota=0: unexpected error on upload #%d: %v", i+1, err)
		}
	}
}

// ─── AC3/SR-90: отклонение до фиксации ──────────────────────────────────────

// TestQuota_ExceedDenied: загрузка, превышающая лимит, возвращает ErrQuotaExceeded
// и файл на диске не появляется (AC3/SR-90).
func TestQuota_ExceedDenied(t *testing.T) {
	cfg := quotaCfg(t, 100) // 100 байт лимит

	// Первая загрузка в пределах лимита — должна пройти.
	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "first.bin",
		Data:      make([]byte, 50),
		Overwrite: false,
		Mode:      0o600,
	})
	if err != nil {
		t.Fatalf("first upload within quota: unexpected error: %v", err)
	}

	// Вторая загрузка: 50 (уже) + 60 (новый) = 110 > 100 → deny.
	_, err = fileupload.Write(cfg, fileupload.Input{
		RelPath:   "second.bin",
		Data:      make([]byte, 60),
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("second upload over quota: want error, got nil")
	}
	if !errors.Is(err, fileupload.ErrQuotaExceeded) {
		t.Errorf("second upload over quota: want ErrQuotaExceeded, got %v (%T)", err, err)
	}

	// Файл second.bin не должен появиться на диске (AC3/SR-90).
	if _, statErr := os.Stat(filepath.Join(cfg.UploadRoot, "second.bin")); !os.IsNotExist(statErr) {
		t.Errorf("second.bin should not exist, statErr=%v", statErr)
	}

	// Temp-файл не должен оставаться.
	entries, _ := os.ReadDir(cfg.UploadRoot)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".raxd-upload-") {
			t.Errorf("temp file left in upload root after quota deny: %s", e.Name())
		}
	}
}

// TestQuota_ExactlyAtLimit: загрузка ровно до лимита — проходит (AC3/AC10, строгое >, Q3).
func TestQuota_ExactlyAtLimit(t *testing.T) {
	cfg := quotaCfg(t, 100)

	// 100 байт = ровно лимит → должна пройти (current=100 не > max=100).
	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "exact.bin",
		Data:      make([]byte, 100),
		Overwrite: false,
		Mode:      0o600,
	})
	if err != nil {
		t.Fatalf("exact limit upload: unexpected error: %v", err)
	}
}

// TestQuota_OneBeyondLimit: ровно на 1 байт больше лимита → deny (AC3/AC10).
func TestQuota_OneBeyondLimit(t *testing.T) {
	cfg := quotaCfg(t, 100)

	// 101 байт > 100 → deny.
	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "over.bin",
		Data:      make([]byte, 101),
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("one beyond limit: want error, got nil")
	}
	if !errors.Is(err, fileupload.ErrQuotaExceeded) {
		t.Errorf("one beyond limit: want ErrQuotaExceeded, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(cfg.UploadRoot, "over.bin")); !os.IsNotExist(statErr) {
		t.Errorf("over.bin should not exist")
	}
}

// ─── AC4/SR-91: нейтральное сообщение без путей/чисел/секретов ──────────────

// TestQuota_ErrorMessageNeutral: сообщение ErrQuotaExceeded не содержит
// абсолютных путей, числовых значений объёма или секретов (AC4/SR-91).
func TestQuota_ErrorMessageNeutral(t *testing.T) {
	cfg := quotaCfg(t, 50)

	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "toobig.bin",
		Data:      make([]byte, 100),
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("want error, got nil")
	}

	msg := err.Error()

	// Нейтральное сообщение содержит факт исчерпания квоты.
	lmsg := strings.ToLower(msg)
	if !strings.Contains(lmsg, "quota") && !strings.Contains(lmsg, "limit") {
		t.Errorf("error message should mention quota/limit, got: %q", msg)
	}

	// НЕ должен содержать абсолютный путь upload root.
	if strings.Contains(msg, cfg.UploadRoot) {
		t.Errorf("error message must NOT contain absolute upload root path, got: %q", msg)
	}

	// НЕ должен содержать числовые значения лимита.
	if strings.Contains(msg, "50") || strings.Contains(msg, "100") {
		t.Errorf("error message must NOT contain numeric quota values, got: %q", msg)
	}
}

// ─── AC5/SR-94: ErrQuotaExceeded — sentinel ──────────────────────────────────

// TestQuota_SentinelError: ErrQuotaExceeded — sentinel, проверяется через errors.Is (AC5/SR-94).
func TestQuota_SentinelError(t *testing.T) {
	cfg := quotaCfg(t, 10)

	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "big.bin",
		Data:      make([]byte, 100),
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !errors.Is(err, fileupload.ErrQuotaExceeded) {
		t.Errorf("want errors.Is(err, ErrQuotaExceeded), got %v (%T)", err, err)
	}
}

// ─── AC6/SR-93: учёт существующих файлов ────────────────────────────────────

// TestQuota_AccountsExistingFiles: существующие файлы в root учитываются при расчёте (AC6/SR-93).
func TestQuota_AccountsExistingFiles(t *testing.T) {
	root := t.TempDir()

	// Создаём существующий файл напрямую (имитация «доисторического» файла).
	existingPath := filepath.Join(root, "old.bin")
	if err := os.WriteFile(existingPath, make([]byte, 80), 0o600); err != nil {
		t.Fatalf("setup existing file: %v", err)
	}

	cfg := fileupload.Config{
		UploadRoot:    root,
		MaxFileBytes:  716800,
		DefaultMode:   0o600,
		DenyRoot:      false,
		MaxTotalBytes: 100, // лимит 100, уже занято 80
	}

	// Попытка загрузить ещё 30 байт: 80+30=110 > 100 → deny.
	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "new.bin",
		Data:      make([]byte, 30),
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("should deny: existing 80B + new 30B > 100B limit")
	}
	if !errors.Is(err, fileupload.ErrQuotaExceeded) {
		t.Errorf("want ErrQuotaExceeded, got %v", err)
	}

	// Существующий файл не изменён.
	info, _ := os.Stat(existingPath)
	if info == nil || info.Size() != 80 {
		t.Errorf("existing file modified! size=%v", info)
	}
}

// TestQuota_AccountsSubdirectories: файлы в подкаталогах тоже учитываются (AC6).
func TestQuota_AccountsSubdirectories(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "sub")
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "inner.bin"), make([]byte, 70), 0o600); err != nil {
		t.Fatalf("write inner: %v", err)
	}

	cfg := fileupload.Config{
		UploadRoot:    root,
		MaxFileBytes:  716800,
		DefaultMode:   0o600,
		DenyRoot:      false,
		MaxTotalBytes: 100, // 70 занято в подкаталоге
	}

	// 70 + 40 = 110 > 100 → deny.
	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "topfile.bin",
		Data:      make([]byte, 40),
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("should deny: subdir 70B + new 40B > 100B limit")
	}
	if !errors.Is(err, fileupload.ErrQuotaExceeded) {
		t.Errorf("want ErrQuotaExceeded, got %v", err)
	}
}

// ─── SR-93: симлинки не разыменовываются при обходе ──────────────────────────

// TestQuota_SymlinkNotFollowed: симлинк внутри root, указывающий на крупный файл снаружи,
// НЕ увеличивает учтённый объём (SR-93, AC6).
func TestQuota_SymlinkNotFollowed(t *testing.T) {
	// Создаём крупный файл вне root.
	outerDir := t.TempDir()
	outerFile := filepath.Join(outerDir, "large.bin")
	if err := os.WriteFile(outerFile, make([]byte, 900), 0o600); err != nil {
		t.Fatalf("write outer large: %v", err)
	}

	root := t.TempDir()
	symlinkPath := filepath.Join(root, "link")
	if err := os.Symlink(outerFile, symlinkPath); err != nil {
		t.Fatalf("symlink creation failed (infrastructure error, not a product bug): %v", err)
	}

	cfg := fileupload.Config{
		UploadRoot:    root,
		MaxFileBytes:  716800,
		DefaultMode:   0o600,
		DenyRoot:      false,
		MaxTotalBytes: 500, // симлинк на 900B файл НЕ должен считаться
	}

	// Загружаем 200 байт. Если симлинк не считается → 200 ≤ 500 → OK.
	// Если симлинк ошибочно считается → 900+200=1100 > 500 → deny (тест провалится).
	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "real.bin",
		Data:      make([]byte, 200),
		Overwrite: false,
		Mode:      0o600,
	})
	if err != nil {
		t.Fatalf("symlink should not count toward quota, but got error: %v", err)
	}
}

// ─── AC7/SR-92: конкурентная безопасность ───────────────────────────────────

// TestQuota_ConcurrentSafety: N параллельных загрузок, суммарно превышающих остаток квоты,
// оставляют upload root в пределах max_total_bytes (AC7/SR-92).
func TestQuota_ConcurrentSafety(t *testing.T) {
	const limit = int64(1000)
	const fileSize = 200
	const numGoroutines = 10 // 10 × 200 = 2000 > limit

	cfg := quotaCfg(t, limit)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			name := strings.Repeat("g", i+1) + ".bin"
			_, _ = fileupload.Write(cfg, fileupload.Input{
				RelPath:   name,
				Data:      make([]byte, fileSize),
				Overwrite: false,
				Mode:      0o600,
			})
		}()
	}
	wg.Wait()

	// Подсчитываем фактический объём на диске.
	var total int64
	err := filepath.WalkDir(cfg.UploadRoot, func(_ string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}

	if total > limit {
		t.Errorf("concurrent uploads exceeded quota: total=%d, limit=%d", total, limit)
	}
}

// ─── AC8/SR-95: overwrite — дельта ──────────────────────────────────────────

// TestQuota_OverwriteSameSize_OK: при исчерпанной квоте перезапись на тот же размер проходит (AC8/SR-95).
func TestQuota_OverwriteSameSize_OK(t *testing.T) {
	cfg := quotaCfg(t, 100)

	// Заполняем до лимита ровно.
	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "file.bin",
		Data:      make([]byte, 100),
		Overwrite: false,
		Mode:      0o600,
	})
	if err != nil {
		t.Fatalf("initial write: %v", err)
	}

	// Перезапись тем же размером (дельта=0) → должна пройти.
	newData := make([]byte, 100)
	newData[0] = 0xFF // отличаемся от нулей
	_, err = fileupload.Write(cfg, fileupload.Input{
		RelPath:   "file.bin",
		Data:      newData,
		Overwrite: true,
		Mode:      0o600,
	})
	if err != nil {
		t.Fatalf("overwrite same size at limit: unexpected error: %v", err)
	}
}

// TestQuota_OverwriteSmallerSize_OK: перезапись на меньший размер при исчерпанной квоте → ОК (AC8/SR-95).
func TestQuota_OverwriteSmallerSize_OK(t *testing.T) {
	cfg := quotaCfg(t, 100)

	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "file.bin",
		Data:      make([]byte, 100),
		Overwrite: false,
		Mode:      0o600,
	})
	if err != nil {
		t.Fatalf("initial write: %v", err)
	}

	// Перезапись меньшим (дельта отрицательная) → должна пройти.
	_, err = fileupload.Write(cfg, fileupload.Input{
		RelPath:   "file.bin",
		Data:      make([]byte, 50),
		Overwrite: true,
		Mode:      0o600,
	})
	if err != nil {
		t.Fatalf("overwrite smaller at limit: unexpected error: %v", err)
	}

	// Проверяем, что файл перезаписан.
	info, _ := os.Stat(filepath.Join(cfg.UploadRoot, "file.bin"))
	if info == nil || info.Size() != 50 {
		t.Errorf("file should be 50B, got %v", info)
	}
}

// TestQuota_OverwriteLarger_Denied: перезапись на больший размер, выводящий за лимит → deny (AC8/SR-95).
func TestQuota_OverwriteLarger_Denied(t *testing.T) {
	cfg := quotaCfg(t, 100)

	originalData := make([]byte, 100)
	originalData[0] = 0xAA
	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "file.bin",
		Data:      originalData,
		Overwrite: false,
		Mode:      0o600,
	})
	if err != nil {
		t.Fatalf("initial write: %v", err)
	}

	// Попытка перезаписи на 150B: current=100, replaced=100, 100-100+150=150 > 100 → deny.
	_, err = fileupload.Write(cfg, fileupload.Input{
		RelPath:   "file.bin",
		Data:      make([]byte, 150),
		Overwrite: true,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("overwrite larger over quota: want error, got nil")
	}
	if !errors.Is(err, fileupload.ErrQuotaExceeded) {
		t.Errorf("want ErrQuotaExceeded, got %v", err)
	}

	// Исходный файл не изменён (AC8/SR-95).
	data, _ := os.ReadFile(filepath.Join(cfg.UploadRoot, "file.bin"))
	if len(data) != 100 || data[0] != 0xAA {
		t.Errorf("original file was modified! len=%d data[0]=%02x", len(data), data[0])
	}
}

// ─── AC9/SR-97: оба лимита независимы ──────────────────────────────────────

// TestQuota_BothLimitsActive: файл проходит per-file лимит, но выводит суммарно за max_total → deny (AC9/SR-97).
func TestQuota_BothLimitsActive(t *testing.T) {
	cfg := fileupload.Config{
		UploadRoot:    t.TempDir(),
		MaxFileBytes:  500,  // per-file лимит 500B
		DefaultMode:   0o600,
		DenyRoot:      false,
		MaxTotalBytes: 100, // общий лимит 100B
	}

	// 200B: проходит per-file (200 ≤ 500), но превышает total (200 > 100) → deny.
	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "file.bin",
		Data:      make([]byte, 200),
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("within per-file but over total: want error, got nil")
	}
	if !errors.Is(err, fileupload.ErrQuotaExceeded) {
		t.Errorf("want ErrQuotaExceeded, got %v", err)
	}
}

// TestQuota_PerFileLimitStillActive: файл нарушающий per-file лимит → ErrTooLarge (без регресса) (AC9/SR-97).
func TestQuota_PerFileLimitStillActive(t *testing.T) {
	cfg := fileupload.Config{
		UploadRoot:    t.TempDir(),
		MaxFileBytes:  50,    // per-file лимит 50B
		DefaultMode:   0o600,
		DenyRoot:      false,
		MaxTotalBytes: 10000, // общий лимит большой
	}

	// 100B: нарушает per-file (100 > 50) → ErrTooLarge (не ErrQuotaExceeded).
	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "file.bin",
		Data:      make([]byte, 100),
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("over per-file limit: want error, got nil")
	}
	if !errors.Is(err, fileupload.ErrTooLarge) {
		t.Errorf("want ErrTooLarge (per-file), got %v", err)
	}
}

// ─── AC10/SR-98: edge-cases ──────────────────────────────────────────────────

// TestQuota_WritePreciselyIntoRemainder: запись «впритык» к остатку → OK (AC10/SR-98).
func TestQuota_WritePreciselyIntoRemainder(t *testing.T) {
	cfg := quotaCfg(t, 200)

	// Первый файл 100B.
	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "first.bin",
		Data:      make([]byte, 100),
		Overwrite: false,
		Mode:      0o600,
	})
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Второй файл ровно 100B (остаток): 100+100=200 = лимит → OK (строгое >).
	_, err = fileupload.Write(cfg, fileupload.Input{
		RelPath:   "second.bin",
		Data:      make([]byte, 100),
		Overwrite: false,
		Mode:      0o600,
	})
	if err != nil {
		t.Fatalf("second write precisely into remainder: unexpected error: %v", err)
	}
}

// TestQuota_IsDirBeforeQuota: цель — каталог → ErrIsDir ДО квота-арифметики (SR-95/plan.md порядок).
func TestQuota_IsDirBeforeQuota(t *testing.T) {
	cfg := quotaCfg(t, 1) // очень маленький лимит

	// Создаём каталог с именем target.
	if err := os.MkdirAll(filepath.Join(cfg.UploadRoot, "target"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Должен вернуть ErrIsDir, а не ErrQuotaExceeded (проверка каталога РАНЬШЕ квоты).
	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "target",
		Data:      make([]byte, 100),
		Overwrite: true,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !errors.Is(err, fileupload.ErrIsDir) {
		t.Errorf("want ErrIsDir (before quota check), got %v", err)
	}
}

// TestQuota_ExistsBeforeQuota: файл существует и overwrite=false → ErrExists ДО квота-арифметики (SR-95).
func TestQuota_ExistsBeforeQuota(t *testing.T) {
	cfg := quotaCfg(t, 1) // очень маленький лимит

	// Создаём файл напрямую.
	if err := os.WriteFile(filepath.Join(cfg.UploadRoot, "existing.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write existing: %v", err)
	}

	// overwrite=false + существующий → ErrExists (не ErrQuotaExceeded).
	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "existing.txt",
		Data:      make([]byte, 100),
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !errors.Is(err, fileupload.ErrExists) {
		t.Errorf("want ErrExists (before quota check), got %v", err)
	}
}

// TestQuota_TotalSmallerThanPerFile: 0 < max_total < max_file_bytes — допускается (Q2/AC10/SR-98).
func TestQuota_TotalSmallerThanPerFile(t *testing.T) {
	cfg := fileupload.Config{
		UploadRoot:    t.TempDir(),
		MaxFileBytes:  1000, // per-file большой
		DefaultMode:   0o600,
		DenyRoot:      false,
		MaxTotalBytes: 50, // общий маленький
	}

	// 60B: проходит per-file (60 ≤ 1000), но превышает total (60 > 50) → ErrQuotaExceeded.
	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "file.bin",
		Data:      make([]byte, 60),
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("want quota exceeded, got nil")
	}
	if !errors.Is(err, fileupload.ErrQuotaExceeded) {
		t.Errorf("want ErrQuotaExceeded, got %v", err)
	}
}

// ─── SR-96: fail-closed при ошибке обхода ────────────────────────────────────

// TestQuota_FailClosedOnWalkError: если при обходе root возникает ошибка → запись НЕ
// выполняется, возвращается ошибка (SR-96/Q4/AC10). Fail-closed: молчаливого обхода нет.
//
// Реализация: детерминированная инъекция ошибки через SetCurrentBytesHook (export_test.go).
// Хук подменяет реальный WalkDir заглушкой, возвращающей ошибку — без зависимости от uid
// процесса. Работает при запуске от root в Docker (канонический прогон).
func TestQuota_FailClosedOnWalkError(t *testing.T) {
	root := t.TempDir()

	cfg := fileupload.Config{
		UploadRoot:    root,
		MaxFileBytes:  716800,
		DefaultMode:   0o600,
		DenyRoot:      false,
		MaxTotalBytes: 1000, // лимит включён → currentBytes вызывается
	}

	// Инжектируем детерминированную ошибку обхода через тестовый хук.
	// Хук сбрасывается в defer — другие тесты не затронуты.
	injectedErr := errors.New("injected walk error: permission denied")
	fileupload.SetCurrentBytesHook(func(*os.Root) (int64, error) {
		return 0, injectedErr
	})
	defer fileupload.SetCurrentBytesHook(nil)

	// Write должен вернуть ошибку (fail-closed): обход упал → запись не выполнена.
	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "test.bin",
		Data:      make([]byte, 10),
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("SR-96 fail-closed: ожидалась ошибка при ошибке обхода, Write вернул nil — молчаливый обход недопустим")
	}
	// Ошибка НЕ должна быть ErrQuotaExceeded (это fail-closed, а не quota-deny).
	if errors.Is(err, fileupload.ErrQuotaExceeded) {
		t.Errorf("SR-96: ошибка обхода должна давать fail (не ErrQuotaExceeded), got ErrQuotaExceeded")
	}
	// Ошибка должна содержать причину (обёртка над injectedErr).
	if !errors.Is(err, injectedErr) {
		t.Errorf("SR-96: ожидалась обёртка над injectedErr; got %v (%T)", err, err)
	}

	// Файл test.bin должен отсутствовать (fail-closed: запись не выполнена).
	if _, statErr := os.Stat(filepath.Join(root, "test.bin")); !os.IsNotExist(statErr) {
		t.Errorf("SR-96: файл не должен быть создан при ошибке обхода; statErr=%v", statErr)
	}
}

// ─── I-3 (SR-90): без следов при deny — MkdirAll ПОСЛЕ квота-проверки ───────

// TestQuota_DenyBeforeMkdirAll: при deny по квоте промежуточный подкаталог НЕ создаётся.
// SR-90: deny ДО фиксации БЕЗ следов на диске; MkdirAll — часть «фиксации» (I-3/plan.md).
func TestQuota_DenyBeforeMkdirAll(t *testing.T) {
	root := t.TempDir()
	cfg := fileupload.Config{
		UploadRoot:    root,
		MaxFileBytes:  716800,
		DefaultMode:   0o600,
		DenyRoot:      false,
		MaxTotalBytes: 50, // лимит 50 байт
	}

	// Попытка записи в новый подкаталог с файлом, превышающим лимит.
	// Подкаталог "subdir" ещё не существует в root.
	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "subdir/file.bin",
		Data:      make([]byte, 100), // 100 > 50 → deny по квоте
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("ожидалась ошибка quota exceeded, получен nil")
	}
	if !errors.Is(err, fileupload.ErrQuotaExceeded) {
		t.Errorf("want ErrQuotaExceeded, got %v", err)
	}

	// Подкаталог "subdir" НЕ должен существовать: MkdirAll не должен выполняться до deny.
	subdirPath := filepath.Join(root, "subdir")
	if _, statErr := os.Stat(subdirPath); !os.IsNotExist(statErr) {
		t.Errorf("SR-90/I-3: подкаталог subdir не должен быть создан при deny по квоте; statErr=%v", statErr)
	}
}

// ─── Регресс: существующие сценарии без лимита ───────────────────────────────

// TestQuota_NoRegression_BasicWrite: запись без лимита работает как прежде (AC11/SR-100).
func TestQuota_NoRegression_BasicWrite(t *testing.T) {
	cfg := quotaCfg(t, 0) // лимит выключен

	result, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "hello.txt",
		Data:      []byte("hello world\n"),
		Overwrite: false,
		Mode:      0o600,
	})
	if err != nil {
		t.Fatalf("basic write regression: %v", err)
	}
	if result.RelPath != "hello.txt" {
		t.Errorf("result.RelPath = %q, want %q", result.RelPath, "hello.txt")
	}
}

// TestQuota_NoRegression_Traversal: traversal-защита работает при любом лимите (AC11/SR-100).
func TestQuota_NoRegression_Traversal(t *testing.T) {
	cfg := quotaCfg(t, 10000) // большой лимит

	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "../escape.txt",
		Data:      []byte("x"),
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("traversal with quota: want error, got nil")
	}
	if !errors.Is(err, fileupload.ErrTraversal) {
		t.Errorf("traversal with quota: want ErrTraversal, got %v", err)
	}
}
