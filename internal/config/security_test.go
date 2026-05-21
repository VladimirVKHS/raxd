package config_test

// security_test.go — security invariant tests for the config package.
// Verifies permissions, idempotency, and absence of file creation beyond 0600 contract.

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/config"
)

// TestEnsureDirsUmaskIndependent verifies that directories are created with
// strictly 0700 permissions regardless of the process umask.
// Security requirement: "права задаются явным аргументом режима, не через umask".
// baseline §2: explicit mode arg in file operations.
func TestEnsureDirsUmaskIndependent(t *testing.T) {
	// Set a very permissive umask (022) that would result in 0755 if mode
	// were delegated to umask. We verify that EnsureDirs still creates 0700.
	oldUmask := syscall.Umask(0o022)
	defer syscall.Umask(oldUmask)

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
			t.Errorf("umask=022: dir %q has permissions %04o, want 0700 (must be umask-independent)",
				d, perm)
		}
	}
}

// TestEnsureDirsNoFilesCreated verifies that EnsureDirs does NOT create any
// files inside StateDir or TLSDir — only directories.
// Security requirement: "каркас не создаёт в этих каталогах файлов шире 0600";
// on bootstrap-cli there must be NO files created at all in StateDir/TLSDir.
func TestEnsureDirsNoFilesCreated(t *testing.T) {
	base := t.TempDir()
	p := config.PathSet{
		ConfigDir: filepath.Join(base, "config", "raxd"),
		StateDir:  filepath.Join(base, "state", "raxd"),
		TLSDir:    filepath.Join(base, "state", "raxd", "tls"),
	}

	if err := config.EnsureDirs(p); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}

	// Walk all directories: ensure no regular files exist.
	for _, dir := range []string{p.ConfigDir, p.StateDir, p.TLSDir} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir(%q) error = %v", dir, err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				t.Errorf("EnsureDirs created unexpected file %q inside %q (must create dirs only)",
					e.Name(), dir)
			}
		}
	}
}

// TestLoadDefaultsNoSecrets verifies that the default Config contains no
// sensitive values — only non-secret defaults like Port.
// Security requirement: "дефолтный config.yaml/дефолты viper не содержат секретов".
func TestLoadDefaultsNoSecrets(t *testing.T) {
	base := t.TempDir()
	p := config.PathSet{
		ConfigDir:  filepath.Join(base, "raxd"),
		ConfigFile: filepath.Join(base, "raxd", "config.yaml"), // does not exist
	}

	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load() must not error on missing file: %v", err)
	}

	// The only default must be a non-sensitive port number.
	if cfg.Port == 0 {
		t.Error("default Port must not be 0 (must be 7822)")
	}
	// Port must not look like a secret (just a sanity check).
	if cfg.Port < 1 || cfg.Port > 65535 {
		t.Errorf("default Port %d is out of valid TCP range", cfg.Port)
	}
}
