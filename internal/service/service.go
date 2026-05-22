// Package service implements OS-level service management for raxd.
//
// plan.md §Modules: internal/service/service.go — ServiceManager interface,
// typed errors, Status, Config, New() dispatch by runtime.GOOS.
//
// SR-83: service runs under unprivileged user raxd:raxd (euid != 0).
// SR-84: ErrPermission on insufficient privileges — no silent root fallback.
// SR-96: stdlib only (os/exec, text/template, runtime, context).
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
)

// ─── Typed error sentinels (plan.md §Contracts) ───────────────────────────────

// ErrAlreadyInstalled is returned by Install when the service is already registered.
// CLI maps this to exit 0 with an informational block (AC9, ux-spec).
var ErrAlreadyInstalled = errors.New("service is already installed")

// ErrNotInstalled is returned when an operation requires the service to be present
// but it is not found. CLI maps this to exit 0 for Uninstall, exit 1 for Start/Stop (AC10).
var ErrNotInstalled = errors.New("service is not installed")

// ErrManagerUnavailable is returned when the OS service manager (systemd/launchd)
// cannot be found or reached. Corresponds to exec.ErrNotFound from runManager (SR-91).
var ErrManagerUnavailable = errors.New("service manager is not available")

// ErrPermission is returned when the current process lacks privileges to perform
// the operation (e.g., writing to /etc/systemd/system/). SR-84.
var ErrPermission = errors.New("insufficient privileges")

// ErrUnsupported is returned by New() on platforms other than linux and darwin.
var ErrUnsupported = errors.New("platform not supported for service management")

// ServiceError is a wrapped error that carries a sentinel and a human-readable detail.
// Use errors.Is(err, ErrXxx) to check the sentinel type.
type ServiceError struct {
	Sentinel error
	Detail   string
}

func (e *ServiceError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s: %s", e.Sentinel.Error(), e.Detail)
	}
	return e.Sentinel.Error()
}

// Is implements errors.Is compatibility — matches against the Sentinel.
func (e *ServiceError) Is(target error) bool {
	return errors.Is(e.Sentinel, target)
}

// wrapErr creates a ServiceError wrapping the given sentinel with a detail message.
func wrapErr(sentinel error, detail string) error {
	return &ServiceError{Sentinel: sentinel, Detail: detail}
}

// ─── Status (plan.md §Contracts) ─────────────────────────────────────────────

// Status describes the current state of the raxd service as reported by the OS manager.
// plan.md: Status{Installed, Active bool; PID, EUID int; State string}
type Status struct {
	// Installed indicates whether the service is registered with the OS manager.
	Installed bool `json:"installed"`

	// Active indicates whether the service is currently running.
	Active bool `json:"active"`

	// PID is the main process ID of the running daemon (0 when not active).
	PID int `json:"pid"`

	// EUID is the effective user ID of the running daemon process (/proc/<pid>/status on Linux).
	// AC6: must be != 0 for a correctly configured service. 0 when not active.
	EUID int `json:"euid"`

	// State is the human-readable state string from the OS manager
	// (e.g. "active (running)", "inactive (dead)", "not installed").
	State string `json:"state"`
}

// ─── Config (plan.md §Contracts) ─────────────────────────────────────────────

// Config carries parameters for constructing a ServiceManager.
// plan.md: Config{ExecPath string; Port int; User, Group, Label string}
type Config struct {
	// ExecPath is the absolute path to the raxd binary.
	// If empty, New() resolves it via os.Executable().
	ExecPath string

	// Port is the TCP port raxd will listen on.
	// Used to determine NeedNetBindCap = Port < 1024 (ADR-003, SR-85).
	Port int

	// User is the OS user name to run the daemon under (default: "raxd").
	User string

	// Group is the OS group name for the daemon (default: "raxd").
	Group string

	// Label is the launchd job label (macOS, default: "tech.oem.raxd").
	Label string

	// StateDir is the directory for runtime state (/var/lib/raxd on Linux, same on macOS).
	StateDir string

	// ConfigDir is the directory for configuration files (/etc/raxd).
	ConfigDir string

	// LogPath is the directory for log files (macOS only; Linux uses journald).
	LogPath string
}

// DefaultConfig returns a Config with sensible production defaults for the current platform.
// plan.md §Contracts: defaults from service-design.md (updated for macOS BUG-1 fix).
//
// Linux:  ConfigDir=/etc/raxd,            StateDir=/var/lib/raxd,    LogPath=/var/log/raxd
// macOS:  ConfigDir=/usr/local/etc/raxd,  StateDir=/usr/local/var/raxd, LogPath=/usr/local/var/log/raxd
//
// ConfigDir is the FULL raxd-specific directory (not the XDG parent).
// XDG_CONFIG_HOME for plist is derived as filepath.Dir(ConfigDir) at render time.
func DefaultConfig() Config {
	return DefaultConfigForGOOS(runtime.GOOS)
}

// DefaultConfigForGOOS returns platform-specific defaults by GOOS string.
// Called by DefaultConfig() and exported for tests that build darwin configs
// on Linux (AC13 — tests run in Docker, runtime.GOOS is always linux there).
func DefaultConfigForGOOS(goos string) Config {
	base := Config{
		Port:  7822,
		User:  "raxd",
		Group: "raxd",
		Label: "tech.oem.raxd",
	}
	switch goos {
	case "darwin":
		base.ConfigDir = "/usr/local/etc/raxd"
		base.StateDir = "/usr/local/var/raxd"
		base.LogPath = "/usr/local/var/log/raxd"
	default: // linux and all others
		base.ConfigDir = "/etc/raxd"
		base.StateDir = "/var/lib/raxd"
		base.LogPath = "/var/log/raxd"
	}
	return base
}

// ─── ServiceManager interface (plan.md §Contracts) ────────────────────────────

// ServiceManager manages the OS-level lifecycle of the raxd service.
// Methods accept a context.Context for timeout/cancellation of manager calls.
// All methods return typed errors from the ErrXxx sentinels above.
type ServiceManager interface {
	// Install generates and registers the service, enables autostart (AC1, AC3).
	// Idempotency: already installed → ErrAlreadyInstalled (AC9).
	// Rollback: any failure during install removes partially created artifacts (AC11).
	Install(ctx context.Context) error

	// Uninstall disables autostart and removes all registration artifacts (AC10).
	// Not installed → ErrNotInstalled.
	Uninstall(ctx context.Context) error

	// Start starts the service (must be installed first).
	// Not installed → ErrNotInstalled.
	Start(ctx context.Context) error

	// Stop stops the service via SIGTERM (graceful shutdown, AC5).
	// Not installed → ErrNotInstalled.
	Stop(ctx context.Context) error

	// Status returns the current state of the service.
	// Not installed → Status{Installed: false} without error (AC10).
	Status(ctx context.Context) (Status, error)
}

// ─── New() — platform dispatch (plan.md §Contracts) ──────────────────────────

// New constructs a ServiceManager appropriate for the current OS.
// linux → systemdManager; darwin → launchdManager; other → ErrUnsupported.
//
// If cfg.ExecPath is empty, it is resolved via os.Executable().
func New(cfg Config) (ServiceManager, error) {
	if cfg.ExecPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("cannot determine executable path: %w", err)
		}
		cfg.ExecPath = exe
	}

	// Apply defaults for missing fields using platform-specific values (BUG-1 macOS fix).
	platformDefaults := DefaultConfigForGOOS(runtime.GOOS)
	if cfg.User == "" {
		cfg.User = platformDefaults.User
	}
	if cfg.Group == "" {
		cfg.Group = platformDefaults.Group
	}
	if cfg.Label == "" {
		cfg.Label = platformDefaults.Label
	}
	if cfg.StateDir == "" {
		cfg.StateDir = platformDefaults.StateDir
	}
	if cfg.ConfigDir == "" {
		cfg.ConfigDir = platformDefaults.ConfigDir
	}
	if cfg.LogPath == "" {
		cfg.LogPath = platformDefaults.LogPath
	}
	if cfg.Port == 0 {
		cfg.Port = platformDefaults.Port
	}

	switch runtime.GOOS {
	case "linux":
		return newSystemdManager(cfg), nil
	case "darwin":
		return newLaunchdManager(cfg), nil
	default:
		return nil, wrapErr(ErrUnsupported, fmt.Sprintf("GOOS=%s", runtime.GOOS))
	}
}
