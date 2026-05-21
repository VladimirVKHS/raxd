// Package config handles XDG-based path resolution and configuration loading.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// PathSet holds the filesystem locations used by raxd.
// All directories follow XDG conventions with D3 override: ConfigDir is always
// $XDG_CONFIG_HOME/raxd when XDG_CONFIG_HOME is set, otherwise $HOME/.config/raxd.
// This matches both Linux and macOS (D3 decision from spec).
//
// Previously named Paths (struct). Renamed to PathSet so that the constructor
// function can be named Paths() per plan.md contract.
type PathSet struct {
	ConfigDir  string // $XDG_CONFIG_HOME/raxd  or  $HOME/.config/raxd
	ConfigFile string // ConfigDir/config.yaml
	StateDir   string // $XDG_STATE_HOME/raxd   or  $HOME/.local/state/raxd
	KeysDB     string // StateDir/keys.db
	TLSDir     string // StateDir/tls/
}

// Paths resolves all raxd filesystem locations following XDG Base Directory
// Specification and D3 (canonical $HOME/.config/raxd on both Linux and macOS).
//
// Returns an error only when $HOME is unavailable.
func Paths() (PathSet, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return PathSet{}, fmt.Errorf("cannot determine config directory: $HOME is not set")
	}

	// ConfigDir: respect XDG_CONFIG_HOME if set; otherwise $HOME/.config (D3).
	configBase := os.Getenv("XDG_CONFIG_HOME")
	if configBase == "" {
		configBase = filepath.Join(home, ".config")
	}
	configDir := filepath.Join(configBase, "raxd")

	// StateDir: respect XDG_STATE_HOME if set; otherwise $HOME/.local/state.
	stateBase := os.Getenv("XDG_STATE_HOME")
	if stateBase == "" {
		stateBase = filepath.Join(home, ".local", "state")
	}
	stateDir := filepath.Join(stateBase, "raxd")

	return PathSet{
		ConfigDir:  configDir,
		ConfigFile: filepath.Join(configDir, "config.yaml"),
		StateDir:   stateDir,
		KeysDB:     filepath.Join(stateDir, "keys.db"),
		TLSDir:     filepath.Join(stateDir, "tls"),
	}, nil
}

// EnsureDirs creates ConfigDir, StateDir, and TLSDir with permissions 0700.
// The function is idempotent: calling it on already-existing directories is safe
// and does not widen permissions.
//
// SECURITY: permissions are set explicitly (not delegated to umask).
func EnsureDirs(p PathSet) error {
	dirs := []string{p.ConfigDir, p.StateDir, p.TLSDir}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("cannot create config directory: %w", err)
		}
	}
	return nil
}
