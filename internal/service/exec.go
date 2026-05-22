// Package service — exec.go: runManager wraps os/exec calls to systemctl/launchctl.
//
// SR-91: exec.Command(name, args...) — NO shell interpolation, NO sh -c, NO string concat.
// SR-95: raw stderr from manager is captured and NOT propagated verbatim to user output.
// plan.md §Contracts: runManager(ctx, name, args...) (string, error)
package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// runCommandRaw creates an exec.Cmd with context for internal use (systemd.go, launchd.go).
// Returns nil if the binary is not found (caller checks).
// SR-91: always exec.CommandContext(ctx, name, args...) — no shell interpolation.
func runCommandRaw(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...) //nolint:gosec // SR-91: name is a fixed system binary path
}

// isExitCode returns true if err is an *exec.ExitError with the given exit code.
func isExitCode(err error, code int) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode() == code
	}
	return false
}

// RunManager executes an OS service manager command (systemctl or launchctl).
//
// The binary is invoked via exec.Command(name, args...) — NO shell interpolation.
// SR-91: args are separate parameters, never concatenated into a single string for /bin/sh.
//
// Return value:
//   - (combined stdout+stderr output, nil) on exit 0
//   - ("", ErrManagerUnavailable) when the binary is not found (exec.ErrNotFound)
//   - ("", typed error) on non-zero exit; raw stderr is NOT propagated (SR-95)
//
// Exported for use in exec_test.go.
func RunManager(ctx context.Context, name string, args ...string) (string, error) {
	// SR-91: exec.Command(name, args...) — binary + separate args, no shell.
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // SR-91: name is a fixed system binary path
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return stdout.String(), nil
	}

	// Binary not found → ErrManagerUnavailable (SR-91, plan.md §Contracts).
	if errors.Is(err, exec.ErrNotFound) {
		return "", wrapErr(ErrManagerUnavailable, fmt.Sprintf("binary %q not found", name))
	}

	// Check for path error (binary missing on some OSes returns *os.PathError, not exec.ErrNotFound).
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		// Non-ExitError: likely path error or context cancellation.
		// Check context first.
		if ctx.Err() != nil {
			return "", fmt.Errorf("manager command cancelled: %w", ctx.Err())
		}
		// Likely binary not found via path error.
		return "", wrapErr(ErrManagerUnavailable, fmt.Sprintf("cannot execute %q: %s", name, neutralizeStderr(err.Error())))
	}

	// Non-zero exit: capture stderr but do NOT propagate raw (SR-95).
	// Return a neutral error. The caller maps this to a typed sentinel.
	rawStderr := stderr.String()
	_ = rawStderr // intentionally dropped; only used internally for mapping below

	// Map specific exit codes from systemctl to typed errors.
	code := exitErr.ExitCode()
	return "", mapExitCode(name, code, neutralizeStderr(rawStderr))
}

// mapExitCode maps a service manager exit code to a typed ServiceManager error.
// Systemctl exit codes (man systemctl): 1=error, 3=not running (for is-active),
// 4=no such unit. We map conservatively.
func mapExitCode(name string, code int, detail string) error {
	switch {
	case code == 1 && detail == "manager command failed":
		// Generic failure — could be permission, unit not found, etc.
		// Return as a raw service error; the caller (systemdManager) will interpret context.
		return &ServiceError{
			Sentinel: ErrManagerUnavailable,
			Detail:   fmt.Sprintf("%s exited with code %d: %s", name, code, detail),
		}
	default:
		// Unrecognized exit code — neutral error.
		return &ServiceError{
			Sentinel: ErrManagerUnavailable,
			Detail:   fmt.Sprintf("%s exited with code %d", name, code),
		}
	}
}
