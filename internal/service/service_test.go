// Package service_test — unit-tests for service.go interface, errors, New() dispatch.
//
// Tests for New() dispatch by GOOS: since these tests run on Linux (Docker),
// we test the linux path directly. The darwin path is tested via a conditional
// compile guard in the function itself.
package service_test

import (
	"errors"
	"runtime"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/service"
)

// ─── Typed error sentinel tests ───────────────────────────────────────────────

// TestErrorSentinels verifies that all required typed errors exist and are distinct.
// plan.md §Contracts: ErrAlreadyInstalled/ErrNotInstalled/ErrManagerUnavailable/ErrPermission/ErrUnsupported
func TestErrorSentinels(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrAlreadyInstalled", service.ErrAlreadyInstalled},
		{"ErrNotInstalled", service.ErrNotInstalled},
		{"ErrManagerUnavailable", service.ErrManagerUnavailable},
		{"ErrPermission", service.ErrPermission},
		{"ErrUnsupported", service.ErrUnsupported},
	}

	for _, s := range sentinels {
		if s.err == nil {
			t.Errorf("%s is nil", s.name)
		}
	}

	// All sentinels must be distinct.
	for i := 0; i < len(sentinels); i++ {
		for j := i + 1; j < len(sentinels); j++ {
			if errors.Is(sentinels[i].err, sentinels[j].err) {
				t.Errorf("%s and %s are the same sentinel", sentinels[i].name, sentinels[j].name)
			}
		}
	}
}

// TestErrorIs verifies that wrapped errors are matched with errors.Is.
// plan.md §Contracts: методы возвращают типизированные ошибки.
func TestErrorIs(t *testing.T) {
	wrapped := &service.ServiceError{
		Sentinel: service.ErrNotInstalled,
		Detail:   "unit file not found",
	}

	if !errors.Is(wrapped, service.ErrNotInstalled) {
		t.Error("wrapped ErrNotInstalled not matched by errors.Is")
	}
	if errors.Is(wrapped, service.ErrAlreadyInstalled) {
		t.Error("wrapped ErrNotInstalled incorrectly matched ErrAlreadyInstalled")
	}
}

// ─── Config defaults ──────────────────────────────────────────────────────────

// TestDefaultConfig verifies that DefaultConfig returns sane defaults for the current platform.
// Platform-specific paths are verified by TestDefaultConfigForGOOS_Paths in templates_test.go.
func TestDefaultConfig(t *testing.T) {
	cfg := service.DefaultConfig()

	if cfg.User != "raxd" {
		t.Errorf("DefaultConfig.User = %q, want raxd", cfg.User)
	}
	if cfg.Group != "raxd" {
		t.Errorf("DefaultConfig.Group = %q, want raxd", cfg.Group)
	}
	if cfg.Label != "tech.oem.raxd" {
		t.Errorf("DefaultConfig.Label = %q, want tech.oem.raxd", cfg.Label)
	}
	if cfg.Port != 7822 {
		t.Errorf("DefaultConfig.Port = %d, want 7822", cfg.Port)
	}
	// ConfigDir must be the full raxd-specific path, not a bare XDG parent (BUG-1 fix).
	if cfg.ConfigDir == "/etc" {
		t.Errorf("DefaultConfig.ConfigDir = %q: must be full path /etc/raxd, not bare XDG parent", cfg.ConfigDir)
	}
}

// ─── New() dispatch tests ─────────────────────────────────────────────────────

// TestNew_CurrentPlatform verifies New() returns a manager (not error) on the current platform.
// On Linux this returns systemdManager; on darwin — launchdManager.
// On other platforms it returns ErrUnsupported.
func TestNew_CurrentPlatform(t *testing.T) {
	cfg := service.DefaultConfig()
	cfg.ExecPath = "/usr/local/bin/raxd"

	mgr, err := service.New(cfg)

	switch runtime.GOOS {
	case "linux", "darwin":
		if err != nil {
			t.Errorf("New() on %s returned error: %v", runtime.GOOS, err)
		}
		if mgr == nil {
			t.Error("New() returned nil manager on supported platform")
		}
	default:
		if !errors.Is(err, service.ErrUnsupported) {
			t.Errorf("New() on %s expected ErrUnsupported, got: %v", runtime.GOOS, err)
		}
	}
}

// TestNew_EmptyExecPath verifies that New() uses os.Executable() when ExecPath is empty.
// It should succeed (not crash) since os.Executable() always works in tests.
func TestNew_EmptyExecPath(t *testing.T) {
	cfg := service.DefaultConfig()
	// ExecPath is empty — New should resolve it via os.Executable()

	mgr, err := service.New(cfg)

	switch runtime.GOOS {
	case "linux", "darwin":
		if err != nil {
			t.Errorf("New() with empty ExecPath on %s returned error: %v", runtime.GOOS, err)
		}
		if mgr == nil {
			t.Error("New() with empty ExecPath returned nil manager")
		}
	}
}

// ─── Status struct ────────────────────────────────────────────────────────────

// TestStatusZeroValue verifies the zero value of Status is a sane "not installed" state.
func TestStatusZeroValue(t *testing.T) {
	var s service.Status
	if s.Installed {
		t.Error("zero Status.Installed should be false")
	}
	if s.Active {
		t.Error("zero Status.Active should be false")
	}
	if s.PID != 0 {
		t.Error("zero Status.PID should be 0")
	}
}
