package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/spf13/viper"
)

// Config holds raxd configuration values.
// Extension points (KeysDB, TLSDir) are carried as path fields resolved
// from Paths — no logic here, full implementation in key-management/tls-transport.
type Config struct {
	// Port is the TCP port raxd listens on (tls-transport task).
	Port int

	// BindAddr is the local address raxd binds to (default: 127.0.0.1).
	// SR-7: default bind to loopback only.
	BindAddr string

	// RateLimit is the sustained request rate per key/IP (events per second).
	// SR-17: token-bucket rate limiting.
	RateLimit float64

	// RateBurst is the maximum burst size for rate limiting.
	RateBurst int

	// OriginAllow is the list of allowed Origin header values.
	// SR-16: Origin present and not in this list → 403.
	OriginAllow []string

	// HostAllow is the list of allowed Host header values (host only, no port).
	// SR-15: Host not in this list → 403.
	HostAllow []string

	// ReadTimeout is the maximum time to read the full request including body.
	// SR-25: Slowloris protection.
	ReadTimeout time.Duration

	// ReadHeaderTimeout is the maximum time to read request headers.
	// SR-25: Slowloris protection.
	ReadHeaderTimeout time.Duration

	// WriteTimeout is the maximum time to write the response.
	// SR-25: protection against slow clients.
	WriteTimeout time.Duration

	// IdleTimeout is the maximum time a keep-alive connection may be idle.
	// SR-25: connection lifecycle management.
	IdleTimeout time.Duration

	// MaxHeaderBytes is the maximum size of request headers.
	// SR-25: protection against header-flooding.
	MaxHeaderBytes int

	// MaxBodyBytes is the maximum size of the request body.
	// SR-25: protection against large-body flooding; enforced via http.MaxBytesReader.
	MaxBodyBytes int64

	// Exec содержит параметры безопасного выполнения команд (command-exec task).
	// SR-66: все параметры с безопасными дефолтами; без env-оверрайдов.
	Exec ExecConfig

	// Upload содержит параметры загрузки файлов (file-upload task).
	// SR-81: безопасные дефолты; невалидные значения отвергаются на старте.
	Upload UploadConfig
}

// ExecConfig — параметры секции exec (command-exec task).
// SR-66: безопасные дефолты; без env-оверрайдов.
type ExecConfig struct {
	// Allowlist — список разрешённых команд. Пустой = выключен (AC7/SR-48).
	Allowlist []string

	// DefaultTimeoutMs — таймаут по умолчанию в мс (дефолт 30000; AC5/SR-46).
	DefaultTimeoutMs int

	// MaxTimeoutMs — жёсткий максимум таймаута (дефолт 300000; AC5/ADR-003).
	MaxTimeoutMs int

	// DefaultCwd — рабочая директория по умолчанию (дефолт /tmp; AC10/SR-50).
	DefaultCwd string

	// EnvWhitelist — разрешённые переменные окружения (дефолт PATH/HOME/LANG/TERM; AC10/SR-49).
	EnvWhitelist []string

	// MaxArgs — максимальное число аргументов (дефолт 256; SR-52/ADR-003).
	MaxArgs int

	// MaxArgLen — максимальная длина аргумента в байтах (дефолт 131072; SR-52/ADR-003).
	MaxArgLen int

	// MaxOutputBytes — лимит вывода на поток в байтах (дефолт 1048576; AC11/SR-53).
	MaxOutputBytes int

	// DenyRoot — hard-fail при euid==0 (дефолт false = только WARN; SR-56/AC9).
	DenyRoot bool
}

// UploadConfig — параметры секции upload (file-upload task).
// SR-81: безопасные дефолты; без env-оверрайдов.
type UploadConfig struct {
	// Root — корень записи файлов.
	// Пусто → serve.go резолвит к <StateDir>/uploads (0700; AC5a/SR-71).
	Root string

	// MaxFileBytes — максимальный декодированный размер файла.
	// Дефолт 716800 (700 KiB; SR-76/AC7/AC16).
	MaxFileBytes int64

	// DefaultMode — режим файла по умолчанию в восьмеричной строке.
	// Дефолт "0600" (AC9/SR-73/ADR-003).
	DefaultMode string

	// OverwriteDefault — политика перезаписи по умолчанию.
	// Дефолт false (AC8/AC15).
	OverwriteDefault bool

	// DenyRoot — жёсткий отказ при euid==0 (дефолт false = только WARN; SR-77/AC11).
	DenyRoot bool
}

// LimiterTTL returns the idle TTL for rate-limiter GC entries.
// Not configurable in v1 — fixed at 10 minutes.
func (c *Config) LimiterTTL() time.Duration {
	return 10 * time.Minute
}

// Load reads config.yaml from p.ConfigFile using viper.
// Absence of the file is NOT an error — defaults are applied instead.
// A malformed YAML file returns an explicit error.
// A present but invalid bind_addr returns an explicit error.
func Load(p PathSet) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(p.ConfigFile)
	v.SetConfigType("yaml")

	// Defaults (non-sensitive values only — SECURITY §4).
	v.SetDefault("port", 7822)
	v.SetDefault("bind_addr", "127.0.0.1")
	v.SetDefault("rate_limit", 10.0)        // 10 req/s per key and per IP
	v.SetDefault("rate_burst", 20)          // burst of 20
	v.SetDefault("origin_allow", []string{"localhost", "127.0.0.1", "::1"})
	v.SetDefault("host_allow", []string{"localhost", "127.0.0.1", "::1"})
	v.SetDefault("read_timeout", "30s")
	v.SetDefault("read_header_timeout", "10s")
	v.SetDefault("write_timeout", "30s")
	v.SetDefault("idle_timeout", "120s")
	v.SetDefault("max_header_bytes", 1<<20)      // 1 MiB
	v.SetDefault("max_body_bytes", int64(1<<20)) // 1 MiB

	// Exec-дефолты (SR-66/ADR-003): безопасные значения для command-exec.
	// Без env-оверрайдов — конфиг только через config.yaml.
	v.SetDefault("exec.allowlist", []string{})           // пустой = allowlist выключен (AC7)
	v.SetDefault("exec.default_timeout_ms", 30000)       // 30s (AC5)
	v.SetDefault("exec.max_timeout_ms", 300000)          // 5 мин (AC5/ADR-003)
	v.SetDefault("exec.default_cwd", "/tmp")             // безопасная директория (AC10)
	v.SetDefault("exec.env_whitelist", []string{"PATH", "HOME", "LANG", "TERM"}) // (AC10/SR-49)
	v.SetDefault("exec.max_args", 256)                   // (SR-52/ADR-003)
	v.SetDefault("exec.max_arg_len", 131072)             // 128 KiB (SR-52/ADR-003)
	v.SetDefault("exec.max_output_bytes", 1048576)       // 1 MiB на поток (AC11/SR-53)
	v.SetDefault("exec.deny_root", false)                // WARN-дефолт (SR-56/ADR-003)

	// Upload-дефолты (SR-81/AC15/plan §Config): безопасные значения для file-upload.
	// Без env-оверрайдов — конфиг только через config.yaml.
	v.SetDefault("upload.root", "")                          // пусто → serve.go резолвит к <StateDir>/uploads (AC5a)
	v.SetDefault("upload.max_file_bytes", int64(716800))     // 700 KiB (SR-76/AC7/AC16)
	v.SetDefault("upload.default_mode", "0600")              // (AC9/SR-73/ADR-003)
	v.SetDefault("upload.overwrite_default", false)          // deny overwrite по умолчанию (AC8/AC15)
	v.SetDefault("upload.deny_root", false)                  // WARN-дефолт (SR-77/AC11)

	if err := v.ReadInConfig(); err != nil {
		// File not found → use defaults, no error.
		// When SetConfigFile is used, viper returns a path error (fs.ErrNotExist)
		// rather than ConfigFileNotFoundError, so we check both.
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) || errors.Is(err, os.ErrNotExist) {
			return buildConfig(v)
		}
		// Any other read error (e.g. bad YAML) → propagate.
		return nil, fmt.Errorf("config file is not valid YAML: %w", err)
	}

	return buildConfig(v)
}

// buildConfig constructs a Config from viper values, validating fields.
func buildConfig(v *viper.Viper) (*Config, error) {
	bindAddr := v.GetString("bind_addr")
	if net.ParseIP(bindAddr) == nil {
		return nil, fmt.Errorf("invalid bind address %q: not a valid IP address", bindAddr)
	}

	readTimeout, err := parseDuration(v, "read_timeout")
	if err != nil {
		return nil, err
	}
	readHeaderTimeout, err := parseDuration(v, "read_header_timeout")
	if err != nil {
		return nil, err
	}
	writeTimeout, err := parseDuration(v, "write_timeout")
	if err != nil {
		return nil, err
	}
	idleTimeout, err := parseDuration(v, "idle_timeout")
	if err != nil {
		return nil, err
	}

	// --- Upload-валидация (SR-81/AC15) ---
	maxBodyBytes := v.GetInt64("max_body_bytes")
	maxFileBytes := v.GetInt64("upload.max_file_bytes")

	// SR-76: max_file_bytes должен быть > 0 и ≤ потолка (max_body_bytes−reserve)×3/4.
	// reserve = 1024 байт (base64-паддинг + JSON-RPC overhead).
	const reserve = int64(1024)
	ceiling := (maxBodyBytes - reserve) * 3 / 4
	if maxFileBytes <= 0 || maxFileBytes > ceiling {
		return nil, fmt.Errorf("upload.max_file_bytes=%d is invalid: must be > 0 and ≤ %d (derived from max_body_bytes=%d; SR-76)",
			maxFileBytes, ceiling, maxBodyBytes)
	}

	// SR-81/ADR-003: default_mode парсится и проходит mode-политику.
	uploadDefaultModeStr := v.GetString("upload.default_mode")
	if _, err := parseModeStr(uploadDefaultModeStr); err != nil {
		return nil, fmt.Errorf("upload.default_mode=%q is invalid: %w", uploadDefaultModeStr, err)
	}

	return &Config{
		Port:              v.GetInt("port"),
		BindAddr:          bindAddr,
		RateLimit:         v.GetFloat64("rate_limit"),
		RateBurst:         v.GetInt("rate_burst"),
		OriginAllow:       v.GetStringSlice("origin_allow"),
		HostAllow:         v.GetStringSlice("host_allow"),
		ReadTimeout:       readTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    v.GetInt("max_header_bytes"),
		MaxBodyBytes:      v.GetInt64("max_body_bytes"),
		Exec: ExecConfig{
			Allowlist:        v.GetStringSlice("exec.allowlist"),
			DefaultTimeoutMs: v.GetInt("exec.default_timeout_ms"),
			MaxTimeoutMs:     v.GetInt("exec.max_timeout_ms"),
			DefaultCwd:       v.GetString("exec.default_cwd"),
			EnvWhitelist:     v.GetStringSlice("exec.env_whitelist"),
			MaxArgs:          v.GetInt("exec.max_args"),
			MaxArgLen:        v.GetInt("exec.max_arg_len"),
			MaxOutputBytes:   v.GetInt("exec.max_output_bytes"),
			DenyRoot:         v.GetBool("exec.deny_root"),
		},
		Upload: UploadConfig{
			Root:             v.GetString("upload.root"),
			MaxFileBytes:     maxFileBytes,
			DefaultMode:      uploadDefaultModeStr,
			OverwriteDefault: v.GetBool("upload.overwrite_default"),
			DenyRoot:         v.GetBool("upload.deny_root"),
		},
	}, nil
}

// parseModeStr парсит восьмеричную строку и применяет mode-политику ADR-003.
// Дублирует логику fileupload.ParseMode, чтобы избежать циклической зависимости.
// SECURITY (SR-73/ADR-003): запрет setuid/setgid/sticky/world-writable.
func parseModeStr(s string) (fs.FileMode, error) {
	if s == "" {
		return 0, fmt.Errorf("empty mode string")
	}
	val, err := strconv.ParseInt(s, 8, 32)
	if err != nil || val < 0 {
		return 0, fmt.Errorf("cannot parse %q as octal mode", s)
	}
	mode := fs.FileMode(val)
	const specialBits = fs.FileMode(0o7000)
	if mode&specialBits != 0 {
		return 0, fmt.Errorf("mode %s contains forbidden special bits (setuid/setgid/sticky)", s)
	}
	const worldWritable = fs.FileMode(0o002)
	if mode&worldWritable != 0 {
		return 0, fmt.Errorf("mode %s is world-writable (bit 0002 forbidden)", s)
	}
	return mode, nil
}

// parseDuration reads a viper key as either a time.Duration or a string
// that can be parsed as a duration.
func parseDuration(v *viper.Viper, key string) (time.Duration, error) {
	val := v.Get(key)
	switch t := val.(type) {
	case time.Duration:
		return t, nil
	case string:
		d, err := time.ParseDuration(t)
		if err != nil {
			return 0, fmt.Errorf("invalid duration for %q: %w", key, err)
		}
		return d, nil
	case int, int64, float64:
		// Numeric values treated as nanoseconds (viper may return this for YAML numbers).
		return time.Duration(v.GetInt64(key)), nil
	default:
		return 0, fmt.Errorf("invalid type for %q: %T", key, val)
	}
}
