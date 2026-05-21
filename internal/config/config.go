package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/viper"
)

// Config holds raxd configuration values.
// Extension points (KeysDB, TLSDir) are carried as path fields resolved
// from Paths — no logic here, full implementation in key-management/tls-transport.
type Config struct {
	// Port is the TCP port raxd listens on (future: tls-transport task).
	Port int
}

// Load reads config.yaml from p.ConfigFile using viper.
// Absence of the file is NOT an error — defaults are applied instead.
// A malformed YAML file returns an explicit error.
func Load(p Paths) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(p.ConfigFile)
	v.SetConfigType("yaml")

	// Defaults (non-sensitive values only — SECURITY §4).
	v.SetDefault("port", 7822)

	if err := v.ReadInConfig(); err != nil {
		// File not found → use defaults, no error.
		// When SetConfigFile is used, viper returns a path error (fs.ErrNotExist)
		// rather than ConfigFileNotFoundError, so we check both.
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) || errors.Is(err, os.ErrNotExist) {
			return &Config{Port: v.GetInt("port")}, nil
		}
		// Any other read error (e.g. bad YAML) → propagate.
		return nil, fmt.Errorf("config file is not valid YAML: %w", err)
	}

	return &Config{
		Port: v.GetInt("port"),
	}, nil
}
