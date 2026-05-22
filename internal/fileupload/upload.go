package fileupload

// upload.go — чистый писатель файла через os.Root (ADR-001/ADR-002).
//
// БЕЗОПАСНОСТЬ:
//   - SR-69 (ADR-001): traversal-safety через os.Root; filepath.IsLocal ранний отказ.
//   - SR-73 (ADR-002): права chmod по fd umask-независимо.
//   - SR-74 (ADR-002): атомарность temp(crypto/rand)→Sync→Rename→fsync-dir; defer cleanup.
//   - SR-72: overwrite политика; цель-каталог → ErrIsDir.
//   - SR-75: лимит MaxFileBytes (страховка в Write; основная проверка — в uploadHandler).
//
// Без MCP/SDK/логирования. Юнит-тестируем офлайн.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrTraversal — попытка записи вне upload root (traversal/symlink наружу/абсолютный путь).
// SR-69/ADR-001. Handler → isError:true, Result:"deny".
var ErrTraversal = errors.New("path is outside the upload root")

// ErrExists — целевой файл существует и overwrite=false.
// SR-72. Handler → isError:true, Result:"deny".
var ErrExists = errors.New("file already exists (set overwrite to replace)")

// ErrIsDir — целевой путь указывает на существующий каталог.
// SR-72/AC14. Handler → isError:true, Result:"deny".
var ErrIsDir = errors.New("target path is a directory")

// ErrTooLarge — декодированный размер превышает MaxFileBytes.
// SR-75/AC7. Handler → isError:true, Result:"deny".
var ErrTooLarge = errors.New("file too large: exceeds max_file_bytes")

// Input — параметры записи файла (данные уже декодированы handler'ом).
// SR-69: RelPath — относительный путь внутри upload root.
// SR-73: Mode — уже распарсен/провалидирован или дефолт.
type Input struct {
	// RelPath — относительный путь назначения внутри upload root.
	// Не должен быть абсолютным или содержать ../-escape (ADR-001/SR-69).
	RelPath string
	// Data — декодированные байты содержимого файла (AC6/SR-75).
	Data []byte
	// Overwrite — разрешить замену существующего файла (AC8/SR-72).
	Overwrite bool
	// Mode — POSIX-режим файла (уже провалидированный; ADR-003/SR-73).
	Mode fs.FileMode
}

// Result — результат успешной записи.
// Возвращается handler'ом → UploadOutput (AC3).
type Result struct {
	// RelPath — фактический относительный путь записанного файла.
	RelPath string
	// Size — число записанных байт.
	Size int64
	// Overwritten — true если файл был перезаписан.
	Overwritten bool
	// Mode — фактический POSIX-режим созданного файла.
	Mode fs.FileMode
}

// Write записывает файл in.Data в путь in.RelPath внутри cfg.UploadRoot.
//
// Контракт (plan.md §Contracts):
//
//	Write(cfg Config, in Input) (Result, error)
//
// Возвращаемые ошибки:
//   - ErrTraversal — абс. путь / ..escape / симлинк наружу (SR-69/ADR-001).
//   - ErrExists — файл существует и Overwrite=false (SR-72).
//   - ErrIsDir — цель — каталог (SR-72/AC14).
//   - ErrTooLarge — len(Data) > MaxFileBytes (SR-75/страховка).
//   - прочие I/O (диск полон и т.п.) — fail.
//
// SECURITY:
//   - Все ФС-операции только через os.Root (SR-69/ADR-001); no raw os.OpenFile/MkdirAll на abs-пути.
//   - Права — chmod по fd (ADR-002/SR-73), не Root.Chmod-по-имени.
//   - Атомарность: temp → Sync → Root.Rename → fsync-dir; defer cleanup (ADR-002/SR-74).
//   - НЕ делает chown/setuid/sudo (SR-73/AC9).
func Write(cfg Config, in Input) (Result, error) {
	// --- Страховочная проверка размера (основная в handler) (SR-75/AC7) ---
	if int64(len(in.Data)) > cfg.MaxFileBytes {
		return Result{}, ErrTooLarge
	}

	// --- Ранний лексический отказ (SR-69/ADR-001) ---
	// filepath.IsLocal: дешёвая лексическая проверка перед os.OpenRoot.
	// Отвергает явно абсолютные пути и ../-escape на уровне синтаксиса.
	// НЕ единственная защита от traversal — os.Root закрывает остальное.
	if !filepath.IsLocal(in.RelPath) {
		return Result{}, fmt.Errorf("%w: %q", ErrTraversal, in.RelPath)
	}

	// --- Открыть upload root через os.Root (ADR-001/SR-69) ---
	// Все последующие операции — только через методы Root по относительным путям.
	root, err := os.OpenRoot(cfg.UploadRoot)
	if err != nil {
		return Result{}, fmt.Errorf("open upload root: %w", err)
	}
	defer root.Close()

	// --- Создать промежуточные подкаталоги внутри корня (AC5b/SR-71) ---
	dir := filepath.Dir(in.RelPath)
	if dir != "." && dir != "" {
		if err := root.MkdirAll(dir, 0o700); err != nil {
			return Result{}, fmt.Errorf("create directories: %w", err)
		}
	}

	// --- Проверка существующей цели (AC8/AC14/SR-72) ---
	// Root.Stat гарантирует, что проверяем путь внутри корня.
	overwritten := false
	if fi, statErr := root.Stat(in.RelPath); statErr == nil {
		// Цель существует.
		if fi.IsDir() {
			return Result{}, ErrIsDir
		}
		if !in.Overwrite {
			return Result{}, ErrExists
		}
		overwritten = true
	}

	// --- Создать temp-файл в том же каталоге что и цель (ADR-002/SR-74) ---
	// Root.CreateTemp ОТСУТСТВУЕТ в os.Root (ADR-001/ADR-002 верифицировано).
	// Генерируем уникальное имя через crypto/rand + O_EXCL.
	tmpName, err := randomTmpName()
	if err != nil {
		return Result{}, fmt.Errorf("generate temp name: %w", err)
	}

	tmpRel := tmpName
	if dir != "." && dir != "" {
		tmpRel = dir + "/" + tmpName
	}

	// Открываем temp с O_CREATE|O_EXCL (нет коллизии; ADR-002).
	tmpFile, err := root.OpenFile(tmpRel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Result{}, fmt.Errorf("create temp file: %w", err)
	}

	// defer: cleanup temp на ЛЮБОЙ ошибке после этой точки (SR-74/AC10).
	committed := false
	defer func() {
		if !committed {
			tmpFile.Close()
			// Best-effort: игнорируем ошибку удаления.
			_ = root.Remove(tmpRel)
		}
	}()

	// --- Chmod по fd ДО записи содержимого (ADR-002/SR-73) ---
	// Umask-независимо (chmod по fd не применяет umask).
	if err := tmpFile.Chmod(in.Mode); err != nil {
		return Result{}, fmt.Errorf("chmod: %w", err)
	}

	// --- Запись данных → Sync → Close ---
	if _, err := tmpFile.Write(in.Data); err != nil {
		return Result{}, fmt.Errorf("write: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return Result{}, fmt.Errorf("sync: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return Result{}, fmt.Errorf("close: %w", err)
	}

	// --- Атомарная фиксация: Root.Rename(tmp → target) (ADR-002/SR-74) ---
	if err := root.Rename(tmpRel, in.RelPath); err != nil {
		return Result{}, fmt.Errorf("rename: %w", err)
	}
	committed = true

	// --- fsync-dir (durability, best-effort как в keystore) ---
	if dirFile, err := root.Open(dir); err == nil {
		_ = dirFile.Sync()
		dirFile.Close()
	}

	return Result{
		RelPath:     in.RelPath,
		Size:        int64(len(in.Data)),
		Overwritten: overwritten,
		Mode:        in.Mode,
	}, nil
}

// randomTmpName генерирует уникальное имя temp-файла.
// Формат: ".raxd-upload-<16-hex>".
// O_EXCL гарантирует атомарное создание без коллизий (ADR-002).
// SECURITY (SR-74): crypto/rand для непредсказуемого имени.
func randomTmpName() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}
	return ".raxd-upload-" + hex.EncodeToString(b), nil
}
