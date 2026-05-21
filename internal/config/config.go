package config

import (
	"errors"
	"fmt"
	"net"
	"os"
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
	v.SetDefault("rate_limit", 10.0)   // 10 req/s per key and per IP
	v.SetDefault("rate_burst", 20)     // burst of 20
	v.SetDefault("origin_allow", []string{"localhost", "127.0.0.1", "::1"})
	v.SetDefault("host_allow", []string{"localhost", "127.0.0.1", "::1"})
	v.SetDefault("read_timeout", "30s")
	v.SetDefault("read_header_timeout", "10s")
	v.SetDefault("write_timeout", "30s")
	v.SetDefault("idle_timeout", "120s")
	v.SetDefault("max_header_bytes", 1<<20) // 1 MiB

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
	}, nil
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
