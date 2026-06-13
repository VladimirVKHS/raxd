package fileupload

// config.go — конфигурация пакета fileupload.
// Маппится из config.UploadConfig в serve.go.
// SR-81: безопасные дефолты; невалидные значения отвергаются на старте.

import "io/fs"

// Config — параметры загрузки файлов.
// Поля заполняются из секции upload конфига (internal/config/config.go).
// SECURITY: Config — value-тип; мьютекс в нём НЕ хранится (SR-92/plan.md §Contracts).
type Config struct {
	// UploadRoot — абсолютный путь разрешённого корня записи.
	// Дефолт: <StateDir>/uploads (резолвится в serve.go при пустом config.Upload.Root).
	// SECURITY: НЕ должен быть /, /root или домашним каталогом root (SR-71/AC5a).
	UploadRoot string

	// MaxFileBytes — максимальный декодированный размер одного файла.
	// Дефолт: 716800 (700 KiB); должен быть ≤ потолка из MaxBodyBytes (SR-76/AC16).
	MaxFileBytes int64

	// MaxTotalBytes — суммарный лимит объёма upload root в байтах.
	// 0 = лимит ОТКЛЮЧЁН (дефолт; AC2/SR-99); обход и мьютекс не задействуются.
	// > 0 = включён: превышение → ErrQuotaExceeded до фиксации (AC3/SR-90).
	// Должно быть ≥ 0; отрицательное отвергается на старте (AC1/SR-98).
	// SECURITY: мьютекс привязан к UploadRoot через пакетный реестр (SR-92/plan.md §Contracts).
	MaxTotalBytes int64

	// DefaultMode — POSIX-режим файла по умолчанию (если поле mode не задано в запросе).
	// Дефолт: 0600 (AC9/SR-73/ADR-003).
	DefaultMode fs.FileMode

	// DenyRoot — жёсткий отказ при euid==0 (дефолт false = только WARN).
	// SR-77/AC11.
	DenyRoot bool
}
