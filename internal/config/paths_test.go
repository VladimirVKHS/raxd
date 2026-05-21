package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/config"
)

// TestPathsDefault verifies that without XDG variables the canonical path
// $HOME/.config/raxd is used (D3 decision: same on Linux and macOS).
// The test is deterministic: HOME is overridden with a temp dir so the test
// always runs regardless of the host environment.
func TestPathsDefault(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")

	paths, err := config.Paths()
	if err != nil {
		t.Fatalf("Paths() error = %v", err)
	}

	wantConfigDir := filepath.Join(fakeHome, ".config", "raxd")
	if paths.ConfigDir != wantConfigDir {
		t.Errorf("ConfigDir = %q, want %q", paths.ConfigDir, wantConfigDir)
	}

	wantStateDir := filepath.Join(fakeHome, ".local", "state", "raxd")
	if paths.StateDir != wantStateDir {
		t.Errorf("StateDir = %q, want %q", paths.StateDir, wantStateDir)
	}

	if paths.ConfigFile != filepath.Join(wantConfigDir, "config.yaml") {
		t.Errorf("ConfigFile = %q", paths.ConfigFile)
	}
	if paths.KeysDB != filepath.Join(wantStateDir, "keys.db") {
		t.Errorf("KeysDB = %q", paths.KeysDB)
	}
	if paths.TLSDir != filepath.Join(wantStateDir, "tls") {
		t.Errorf("TLSDir = %q", paths.TLSDir)
	}
}

// TestPathsXDGOverride verifies that XDG_CONFIG_HOME and XDG_STATE_HOME
// take precedence over $HOME defaults.
func TestPathsXDGOverride(t *testing.T) {
	customConfig := t.TempDir()
	customState := t.TempDir()

	t.Setenv("XDG_CONFIG_HOME", customConfig)
	t.Setenv("XDG_STATE_HOME", customState)

	paths, err := config.Paths()
	if err != nil {
		t.Fatalf("Paths() error = %v", err)
	}

	want := filepath.Join(customConfig, "raxd")
	if paths.ConfigDir != want {
		t.Errorf("ConfigDir = %q, want %q", paths.ConfigDir, want)
	}

	wantState := filepath.Join(customState, "raxd")
	if paths.StateDir != wantState {
		t.Errorf("StateDir = %q, want %q", paths.StateDir, wantState)
	}
}

// TestEnsureDirsCreatesWithMode0700 verifies that EnsureDirs creates
// directories with strictly 0700 permissions regardless of umask.
func TestEnsureDirsCreatesWithMode0700(t *testing.T) {
	base := t.TempDir()

	p := config.PathSet{
		ConfigDir: filepath.Join(base, "config", "raxd"),
		StateDir:  filepath.Join(base, "state", "raxd"),
		TLSDir:    filepath.Join(base, "state", "raxd", "tls"),
	}

	if err := config.EnsureDirs(p); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}

	for _, d := range []string{p.ConfigDir, p.StateDir, p.TLSDir} {
		info, err := os.Stat(d)
		if err != nil {
			t.Fatalf("Stat(%q) error = %v", d, err)
		}
		perm := info.Mode().Perm()
		if perm != 0o700 {
			t.Errorf("dir %q has permissions %o, want 0700", d, perm)
		}
	}
}

// TestEnsureDirsIdempotent verifies that calling EnsureDirs twice does not
// fail and does not widen permissions.
func TestEnsureDirsIdempotent(t *testing.T) {
	base := t.TempDir()

	p := config.PathSet{
		ConfigDir: filepath.Join(base, "config", "raxd"),
		StateDir:  filepath.Join(base, "state", "raxd"),
		TLSDir:    filepath.Join(base, "state", "raxd", "tls"),
	}

	if err := config.EnsureDirs(p); err != nil {
		t.Fatalf("first EnsureDirs() error = %v", err)
	}
	if err := config.EnsureDirs(p); err != nil {
		t.Fatalf("second EnsureDirs() error = %v (must be idempotent)", err)
	}

	for _, d := range []string{p.ConfigDir, p.StateDir, p.TLSDir} {
		info, _ := os.Stat(d)
		if info.Mode().Perm() != 0o700 {
			t.Errorf("after second call dir %q permissions widened to %o", d, info.Mode().Perm())
		}
	}
}

// TestLoadMissingFileReturnsDefaults verifies that absence of config.yaml is
// not an error — defaults are applied (AC "отсутствие config.yaml не ошибка").
func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	base := t.TempDir()
	p := config.PathSet{
		ConfigDir:  filepath.Join(base, "raxd"),
		ConfigFile: filepath.Join(base, "raxd", "config.yaml"), // does not exist
	}

	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load() with missing file should not error, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil Config")
	}
	if cfg.Port != 7822 {
		t.Errorf("default Port = %d, want 7822", cfg.Port)
	}
}

// TestLoadBrokenYAMLReturnsError verifies that a malformed config.yaml
// produces an explicit error.
func TestLoadBrokenYAMLReturnsError(t *testing.T) {
	base := t.TempDir()
	cfgFile := filepath.Join(base, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("port: [broken yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := config.PathSet{
		ConfigDir:  base,
		ConfigFile: cfgFile,
	}

	_, err := config.Load(p)
	if err == nil {
		t.Fatal("Load() with broken YAML must return an error")
	}
	if !strings.Contains(err.Error(), "config file is not valid YAML") {
		t.Errorf("error message %q must contain 'config file is not valid YAML'", err.Error())
	}
}
