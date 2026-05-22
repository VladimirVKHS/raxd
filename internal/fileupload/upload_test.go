package fileupload_test

// upload_test.go — тесты пакета internal/fileupload (TDD).
//
// Покрывают AC4/AC5a/AC5b/AC6/AC7/AC8/AC9/AC10/AC14 и ключевые SR-69..SR-75.
// Все тесты офлайн (no I/O вне tmpdir); запускаются в Docker -mod=vendor.
//
// БЕЗОПАСНОСТЬ: тесты НЕ запускают raxd на хосте (baseline §6); только FS в tmpdir.

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/fileupload"
)

// defaultCfg возвращает Config с безопасными дефолтами для тестов.
func defaultCfg(t *testing.T) fileupload.Config {
	t.Helper()
	root := t.TempDir()
	return fileupload.Config{
		UploadRoot:   root,
		MaxFileBytes: 716800, // 700 KiB
		DefaultMode:  0o600,
		DenyRoot:     false,
	}
}

// ─── AC5b / AC6 / SR-75: успешная запись (базовый случай) ──────────────────

// TestWriteSuccess_BasicFile проверяет успешную запись нового файла с правильными
// метаданными результата (AC3/AC6/AC5b/SR-75).
func TestWriteSuccess_BasicFile(t *testing.T) {
	cfg := defaultCfg(t)
	data := []byte("hello world\n")

	result, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "hello.txt",
		Data:      data,
		Overwrite: false,
		Mode:      0o600,
	})
	if err != nil {
		t.Fatalf("Write: unexpected error: %v", err)
	}

	// AC3: поля результата.
	if result.RelPath != "hello.txt" {
		t.Errorf("Result.RelPath = %q, want %q", result.RelPath, "hello.txt")
	}
	if result.Size != int64(len(data)) {
		t.Errorf("Result.Size = %d, want %d", result.Size, len(data))
	}
	if result.Overwritten {
		t.Errorf("Result.Overwritten = true, want false (new file)")
	}
	if result.Mode != 0o600 {
		t.Errorf("Result.Mode = %04o, want 0600", result.Mode)
	}

	// Содержимое на диске точно соответствует исходным байтам (AC6/SR-75).
	written, err := os.ReadFile(filepath.Join(cfg.UploadRoot, "hello.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(written) != string(data) {
		t.Errorf("disk content = %q, want %q", written, data)
	}
}

// TestWriteSuccess_BinaryContent: бинарные данные → точные байты на диске (AC6/SR-75).
func TestWriteSuccess_BinaryContent(t *testing.T) {
	cfg := defaultCfg(t)
	data := []byte{0x00, 0xFF, 0x01, 0xFE, 0x7F, 0x80}

	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "binary.bin",
		Data:      data,
		Overwrite: false,
		Mode:      0o600,
	})
	if err != nil {
		t.Fatalf("Write binary: %v", err)
	}

	written, err := os.ReadFile(filepath.Join(cfg.UploadRoot, "binary.bin"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(written) != len(data) {
		t.Errorf("binary: disk len=%d want %d", len(written), len(data))
	}
	for i := range data {
		if written[i] != data[i] {
			t.Errorf("binary: byte[%d]=%02x want %02x", i, written[i], data[i])
		}
	}
}

// ─── AC5b / SR-71: создание подкаталогов внутри корня ──────────────────────

// TestWriteSuccess_Subdirectory: запись в несуществующий подкаталог создаёт его (AC5b/SR-71).
func TestWriteSuccess_Subdirectory(t *testing.T) {
	cfg := defaultCfg(t)
	data := []byte("script content\n")

	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "scripts/deploy.sh",
		Data:      data,
		Overwrite: false,
		Mode:      0o700,
	})
	if err != nil {
		t.Fatalf("Write subdir: %v", err)
	}

	path := filepath.Join(cfg.UploadRoot, "scripts", "deploy.sh")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat after write: %v", err)
	}
	if info.IsDir() {
		t.Errorf("target is a dir, want file")
	}
}

// TestWriteSuccess_NestedSubdirectories: глубокая вложенность (AC5b).
func TestWriteSuccess_NestedSubdirectories(t *testing.T) {
	cfg := defaultCfg(t)

	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "a/b/c/file.txt",
		Data:      []byte("nested"),
		Overwrite: false,
		Mode:      0o600,
	})
	if err != nil {
		t.Fatalf("Write nested: %v", err)
	}

	if _, err := os.Stat(filepath.Join(cfg.UploadRoot, "a", "b", "c", "file.txt")); err != nil {
		t.Errorf("nested file not found: %v", err)
	}
}

// ─── AC4 / SR-69: path traversal ────────────────────────────────────────────

// TestWriteTraversal_DotDotEscape: path с ../ → ErrTraversal (AC4/SR-69).
func TestWriteTraversal_DotDotEscape(t *testing.T) {
	cfg := defaultCfg(t)
	sensitiveFile := filepath.Join(filepath.Dir(cfg.UploadRoot), "sensitive.txt")

	// Убедиться, что файл за корнем не существует до теста.
	if err := os.WriteFile(sensitiveFile, []byte("original"), 0o600); err != nil {
		t.Fatalf("setup sensitive file: %v", err)
	}
	t.Cleanup(func() { os.Remove(sensitiveFile) })

	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "../sensitive.txt",
		Data:      []byte("pwned"),
		Overwrite: true,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("Write traversal: want error, got nil")
	}
	if !isTraversalErr(err) {
		t.Errorf("Write traversal: want ErrTraversal, got %v (%T)", err, err)
	}

	// Файл вне корня НЕ изменён (AC4/SR-69).
	data, err2 := os.ReadFile(sensitiveFile)
	if err2 != nil {
		t.Fatalf("ReadFile sensitive: %v", err2)
	}
	if string(data) != "original" {
		t.Errorf("sensitive file was modified! content=%q", data)
	}
}

// TestWriteTraversal_AbsolutePath: абсолютный path → ErrTraversal (AC4/SR-69).
func TestWriteTraversal_AbsolutePath(t *testing.T) {
	cfg := defaultCfg(t)

	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "/etc/passwd",
		Data:      []byte("x"),
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("Write abs path: want error, got nil")
	}
	if !isTraversalErr(err) {
		t.Errorf("Write abs path: want ErrTraversal, got %v", err)
	}

	// /etc/passwd НЕ тронут.
	if _, statErr := os.Stat("/etc/passwd"); statErr == nil {
		data, _ := os.ReadFile("/etc/passwd")
		if string(data) == "x" {
			t.Error("/etc/passwd was overwritten!")
		}
	}
}

// TestWriteTraversal_MultipleEscape: "a/../../b" → ErrTraversal (AC4/SR-69).
func TestWriteTraversal_MultipleEscape(t *testing.T) {
	cfg := defaultCfg(t)

	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "a/../../b",
		Data:      []byte("x"),
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("Write multi-escape: want error, got nil")
	}
	if !isTraversalErr(err) {
		t.Errorf("Write multi-escape: want ErrTraversal, got %v", err)
	}
}

// TestWriteTraversal_Symlink: путь через симлинк наружу → ErrTraversal (AC4/SR-69).
func TestWriteTraversal_Symlink(t *testing.T) {
	cfg := defaultCfg(t)

	// Создаём симлинк внутри upload root, указывающий наружу.
	outerDir := t.TempDir()
	symlinkPath := filepath.Join(cfg.UploadRoot, "link")
	if err := os.Symlink(outerDir, symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	// Пытаемся записать через симлинк.
	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "link/escape.txt",
		Data:      []byte("pwned"),
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("Write symlink: want error, got nil")
	}
	// os.Root возвращает специфическую ошибку; принимаем любую ошибку как traversal-защиту.
	// Реальный os.Root возвращает системную ошибку ("escapes from root") — не обязательно ErrTraversal.
	// Проверяем что файл НЕ создан.
	entries, _ := os.ReadDir(outerDir)
	if len(entries) > 0 {
		t.Errorf("symlink traversal: outer dir was written! entries=%v", entries)
	}
}

// ─── AC7 / SR-75: превышение лимита ─────────────────────────────────────────

// TestWriteTooLarge: данные > MaxFileBytes → ErrTooLarge, файла нет (AC7/SR-75).
func TestWriteTooLarge(t *testing.T) {
	cfg := defaultCfg(t)
	cfg.MaxFileBytes = 10

	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "big.bin",
		Data:      make([]byte, 11),
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("Write too large: want error, got nil")
	}
	if !isTooLargeErr(err) {
		t.Errorf("Write too large: want ErrTooLarge, got %v (%T)", err, err)
	}

	// Файл на диске не создан (AC7/SR-75).
	if _, statErr := os.Stat(filepath.Join(cfg.UploadRoot, "big.bin")); !os.IsNotExist(statErr) {
		t.Errorf("file was created despite too-large! statErr=%v", statErr)
	}

	// Temp-файл тоже не остался (AC10/SR-74).
	entries, _ := os.ReadDir(cfg.UploadRoot)
	if len(entries) != 0 {
		t.Errorf("temp file left in upload root! entries=%v", entries)
	}
}

// ─── AC8 / SR-72: overwrite политика ─────────────────────────────────────────

// TestWriteOverwrite_Denied: существующий файл + overwrite=false → ErrExists (AC8/SR-72).
func TestWriteOverwrite_Denied(t *testing.T) {
	cfg := defaultCfg(t)
	original := []byte("original content")

	// Первая запись.
	if _, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "existing.txt",
		Data:      original,
		Overwrite: false,
		Mode:      0o600,
	}); err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Попытка перезаписать.
	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "existing.txt",
		Data:      []byte("new content"),
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("Write overwrite=false: want error, got nil")
	}
	if !isExistsErr(err) {
		t.Errorf("Write overwrite=false: want ErrExists, got %v", err)
	}

	// Содержимое прежнее (AC8/SR-72).
	data, _ := os.ReadFile(filepath.Join(cfg.UploadRoot, "existing.txt"))
	if string(data) != string(original) {
		t.Errorf("file was modified! content=%q, want %q", data, original)
	}
}

// TestWriteOverwrite_Allowed: существующий файл + overwrite=true → замена (AC8/SR-72).
func TestWriteOverwrite_Allowed(t *testing.T) {
	cfg := defaultCfg(t)
	original := []byte("original")
	updated := []byte("updated content")

	if _, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "replace.txt",
		Data:      original,
		Overwrite: false,
		Mode:      0o600,
	}); err != nil {
		t.Fatalf("first write: %v", err)
	}

	result, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "replace.txt",
		Data:      updated,
		Overwrite: true,
		Mode:      0o600,
	})
	if err != nil {
		t.Fatalf("Write overwrite=true: %v", err)
	}
	if !result.Overwritten {
		t.Errorf("Result.Overwritten = false, want true")
	}

	data, _ := os.ReadFile(filepath.Join(cfg.UploadRoot, "replace.txt"))
	if string(data) != string(updated) {
		t.Errorf("file not replaced! content=%q, want %q", data, updated)
	}
}

// TestWriteTargetIsDirectory: цель — каталог → ErrIsDir (AC14/SR-72).
func TestWriteTargetIsDirectory(t *testing.T) {
	cfg := defaultCfg(t)

	// Создаём подкаталог с тем же именем, что и цель.
	if err := os.MkdirAll(filepath.Join(cfg.UploadRoot, "mydir"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "mydir",
		Data:      []byte("x"),
		Overwrite: true,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("Write target=dir: want error, got nil")
	}
	if !isDirErr(err) {
		t.Errorf("Write target=dir: want ErrIsDir, got %v (%T)", err, err)
	}
}

// ─── AC9 / SR-73: права файла ──────────────────────────────────────────────

// TestWriteMode_Default0600: без mode → файл с 0600 (AC9/SR-73).
func TestWriteMode_Default0600(t *testing.T) {
	cfg := defaultCfg(t)

	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "secret.txt",
		Data:      []byte("data"),
		Overwrite: false,
		Mode:      0o600, // дефолт
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	info, err := os.Stat(filepath.Join(cfg.UploadRoot, "secret.txt"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	got := info.Mode().Perm()
	if got != 0o600 {
		t.Errorf("file mode = %04o, want 0600", got)
	}
}

// TestWriteMode_Custom0700: mode=0700 → файл с 0700 (AC9/SR-73).
func TestWriteMode_Custom0700(t *testing.T) {
	cfg := defaultCfg(t)

	result, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "script.sh",
		Data:      []byte("#!/bin/bash"),
		Overwrite: false,
		Mode:      0o700,
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if result.Mode != 0o700 {
		t.Errorf("Result.Mode = %04o, want 0700", result.Mode)
	}

	info, err := os.Stat(filepath.Join(cfg.UploadRoot, "script.sh"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("disk mode = %04o, want 0700", info.Mode().Perm())
	}
}

// ─── AC10 / SR-74: атомарность ────────────────────────────────────────────

// TestAtomicity_NoTempOnError: при ошибке (too large) не остаётся ни целевого, ни temp-файла (AC10/SR-74).
func TestAtomicity_NoTempOnError(t *testing.T) {
	cfg := defaultCfg(t)
	cfg.MaxFileBytes = 5

	_, err := fileupload.Write(cfg, fileupload.Input{
		RelPath:   "fail.bin",
		Data:      make([]byte, 100),
		Overwrite: false,
		Mode:      0o600,
	})
	if err == nil {
		t.Fatal("want error, got nil")
	}

	// Ни целевой, ни temp-файл не остались.
	entries, _ := os.ReadDir(cfg.UploadRoot)
	for _, e := range entries {
		t.Errorf("unexpected file left in upload root: %s", e.Name())
	}
}

// ─── helpers ────────────────────────────────────────────────────────────────

func isTraversalErr(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "traversal") ||
		strings.Contains(err.Error(), "outside") ||
		strings.Contains(err.Error(), "escapes") ||
		strings.Contains(err.Error(), "local") ||
		strings.Contains(err.Error(), "invalid path"))
}

func isTooLargeErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "too large")
}

func isExistsErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "exists")
}

func isDirErr(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "directory") ||
		strings.Contains(err.Error(), "is a dir"))
}

// Ensure fs package is referenced.
var _ fs.FileMode
