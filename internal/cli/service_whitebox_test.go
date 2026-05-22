// Package cli — whitebox tests for resolveManagerWithPort (ISSUE-1 fix verification).
//
// ISSUE-1 (SR-85/ADR-003): resolveManagerWithPort must read port from config.Load(),
// NOT from service.DefaultConfig() (which hardcodes 7822).
//
// Test strategy: set XDG_CONFIG_HOME to a tmp dir, write config.yaml with port: 443,
// then call resolveManagerWithPort(nil) and assert returned port == 443.
// The manager itself may fail (ErrUnsupported on non-linux/darwin) — we only care about
// the port value that was passed to service.New().
package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveManagerWithPort_ReadsPortFromConfig verifies ISSUE-1 fix:
// resolveManagerWithPort reads port from config.yaml, not DefaultConfig().
//
// Plan.md §Contracts + SR-85/ADR-003: Port < 1024 triggers AmbientCapabilities;
// using DefaultConfig() (7822) would silently suppress them for privileged ports.
func TestResolveManagerWithPort_ReadsPortFromConfig(t *testing.T) {
	// Create a temporary XDG_CONFIG_HOME with a config.yaml containing port: 443.
	tmpDir := t.TempDir()
	raxdCfgDir := filepath.Join(tmpDir, "raxd")
	if err := os.MkdirAll(raxdCfgDir, 0o700); err != nil {
		t.Fatalf("cannot create tmp config dir: %v", err)
	}
	cfgFile := filepath.Join(raxdCfgDir, "config.yaml")
	// Write minimal valid config with a privileged port.
	cfgContent := "port: 443\n"
	if err := os.WriteFile(cfgFile, []byte(cfgContent), 0o600); err != nil {
		t.Fatalf("cannot write tmp config.yaml: %v", err)
	}

	// Also set XDG_STATE_HOME so config.Load() can resolve StateDir without error.
	tmpStateDir := t.TempDir()

	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("XDG_STATE_HOME", tmpStateDir)

	// Call the function under test with nil injected manager (production path).
	// The manager itself may be nil/error on unsupported platforms (e.g., Windows) —
	// we only verify the port value, which is resolved from config BEFORE service.New().
	_, port, _ := resolveManagerWithPort(nil)
	// resolveManagerWithPort returns svcCfg.Port even when service.New() errors.
	// This is the core of ISSUE-1: port must come from config.yaml, not DefaultConfig().
	if port != 443 {
		t.Errorf("resolveManagerWithPort must return port from config.yaml (443), got %d", port)
	}
}

// TestResolveManagerWithPort_DefaultPortWhenNoConfig verifies that when config.yaml
// is absent, resolveManagerWithPort uses the default port (7822).
func TestResolveManagerWithPort_DefaultPortWhenNoConfig(t *testing.T) {
	// Point XDG_CONFIG_HOME to an empty dir (no config.yaml).
	tmpDir := t.TempDir()
	tmpStateDir := t.TempDir()

	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("XDG_STATE_HOME", tmpStateDir)

	_, port, _ := resolveManagerWithPort(nil)

	// Default port is 7822 when config.yaml is absent (config.Load uses defaults).
	if port != 7822 {
		t.Errorf("resolveManagerWithPort must use default port 7822 when config absent, got %d", port)
	}
}
