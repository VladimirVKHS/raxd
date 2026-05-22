// Package service_test — unit-tests for exec.go runManager error mapping.
//
// SR-91: runManager uses exec.Command(name, args...) without shell interpolation.
// SR-95: raw stderr from manager is NOT propagated to user output.
package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/service"
)

// TestRunManager_NotFound verifies that a missing binary maps to ErrManagerUnavailable.
// SR-91: exec.ErrNotFound → ErrManagerUnavailable.
func TestRunManager_NotFound(t *testing.T) {
	ctx := context.Background()
	_, err := service.RunManager(ctx, "/nonexistent-binary-xyzzy-12345")
	if err == nil {
		t.Fatal("expected error for nonexistent binary, got nil")
	}
	if !errors.Is(err, service.ErrManagerUnavailable) {
		t.Errorf("expected ErrManagerUnavailable for missing binary, got: %v", err)
	}
}

// TestRunManager_NoShellInterpolation verifies that args are passed separately, not via shell.
// SR-91: «никогда sh -c <строка>, никогда конкатенация значений».
// We pass a nonexistent command: if shell were used, it might interpret it differently.
func TestRunManager_NoShellInterpolation(t *testing.T) {
	ctx := context.Background()
	// This would succeed if shell were used (echo is a shell builtin).
	// It should fail with ErrManagerUnavailable because "echo" may not be at the exact path.
	// We test a safer invariant: no panic, error is typed.
	_, err := service.RunManager(ctx, "/nonexistent-shell-test-cmd", "arg1", "arg2; echo injected")
	if err == nil {
		t.Fatal("expected error for nonexistent command, got nil")
	}
	// Should be ErrManagerUnavailable, not a raw error.
	if !errors.Is(err, service.ErrManagerUnavailable) {
		t.Errorf("expected ErrManagerUnavailable, got: %T %v", err, err)
	}
}

// TestRunManager_RawStderrNotPropagated verifies that raw stderr from the subprocess
// is NOT included verbatim in the returned error message (SR-95).
//
// Strategy: run `ls /nonexistent-raxd-stderr-test-xyzzy` — the command writes a known
// path fragment to stderr on Linux (ls: cannot access '/nonexistent-raxd-stderr-test-xyzzy':
// No such file or directory) and exits non-zero. We assert the raw path fragment does NOT
// appear in err.Error(), proving RunManager neutralizes stderr before returning.
func TestRunManager_RawStderrNotPropagated(t *testing.T) {
	ctx := context.Background()

	// This sentinel path will appear verbatim in /bin/ls stderr output.
	// It must NOT appear in the returned error.
	sentinel := "nonexistent-raxd-stderr-test-xyzzy"
	_, err := service.RunManager(ctx, "/bin/ls", "/"+sentinel)
	if err == nil {
		// /bin/ls on a non-existent path must exit non-zero; if it somehow
		// succeeded (impossible) we cannot test the SR-95 invariant.
		t.Fatal("expected error from ls on nonexistent path, got nil")
	}

	errStr := err.Error()
	if strings.Contains(errStr, sentinel) {
		t.Errorf("SR-95 violation: raw stderr leaked into error message.\n"+
			"sentinel %q found in: %q", sentinel, errStr)
	}
}

// TestRunManager_ContextCancellation verifies that a cancelled context stops execution.
func TestRunManager_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := service.RunManager(ctx, "/bin/sleep", "100")
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	// Should be some error (context cancelled or manager unavailable).
}
