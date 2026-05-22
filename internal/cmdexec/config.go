package cmdexec

// config.go — конфигурация раннера команд.
//
// Config маппируется из config.ExecConfig вызывающей стороной (internal/cli/serve.go).
// SR-66: все параметры безопасности — конфиг-поля с безопасными дефолтами.

// Config определяет параметры безопасного запуска команд.
// Нулевое значение небезопасно — всегда инициализировать через конфиг.
type Config struct {
	// Allowlist — список разрешённых команд. Пустой (nil/[]) = allowlist выключен,
	// разрешена любая команда. Включён → точное сопоставление по присланному command
	// ДО LookPath (SR-48/AC7).
	Allowlist []string

	// DefaultTimeoutMs — таймаут по умолчанию в мс (дефолт 30000 = 30s; AC5).
	DefaultTimeoutMs int

	// MaxTimeoutMs — жёсткий максимум таймаута (дефолт 300000 = 5 мин; AC5/ADR-003).
	MaxTimeoutMs int

	// DefaultCwd — рабочая директория по умолчанию при пустом cwd (дефолт /tmp; AC10/SR-50).
	DefaultCwd string

	// EnvWhitelist — разрешённые переменные окружения (дефолт PATH/HOME/LANG/TERM; AC10/SR-49).
	// Значения берутся из окружения демона. LD_PRELOAD/IFS/DYLD_* — НЕ в списке.
	EnvWhitelist []string

	// MaxArgs — максимальное число аргументов (дефолт 256; SR-52/ADR-003).
	MaxArgs int

	// MaxArgLen — максимальная длина одного аргумента в байтах (дефолт 128 KiB; SR-52/ADR-003).
	MaxArgLen int

	// MaxOutputBytes — лимит вывода на каждый поток stdout/stderr в байтах (дефолт 1 MiB; AC11/SR-53).
	MaxOutputBytes int

	// DenyRoot — если true и euid==0, команда отклоняется (SR-56/AC9).
	// Дефолт false = только WARN (не отказ).
	DenyRoot bool
}
