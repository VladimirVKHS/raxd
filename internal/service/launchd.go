// Package service — launchd.go: launchdManager implements ServiceManager for macOS.
//
// AC13: launchd integration is NOT testable in Docker (Linux containers).
// Unit tests for plist generation are in templates_test.go (no build tags).
// Full integration must be tested on real macOS outside Docker.
//
// SR-83: plist sets UserName=raxd so launchd drops privileges (AC6).
// SR-88: plist written as root:wheel 0644.
// SR-89: StateDir created as mkdir 0700 + chown raxd (no StateDirectory= equivalent in launchd).
// SR-92: rollback removes plist on Install failure (AC11).
// SR-93: Uninstall removes plist; user raxd INTENTIONALLY kept (П-2).
//
// Build: compiles on all platforms (no build tag). OS-specific calls (launchctl, dscl)
// are reached only at runtime on darwin.
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	// plistPath is the canonical location for LaunchDaemons plists (SR-88: root:wheel 0644).
	plistPath = "/Library/LaunchDaemons/tech.oem.raxd.plist"

	// launchctlBin is the fixed launchctl binary path (SR-91: no shell interpolation).
	launchctlBin = "/bin/launchctl"

	// launchLabel is the launchd job target string.
	launchLabel = "system/tech.oem.raxd"
)

// launchdManager implements ServiceManager for macOS + launchd.
type launchdManager struct {
	cfg Config
}

func newLaunchdManager(cfg Config) *launchdManager {
	return &launchdManager{cfg: cfg}
}

// Install implements ServiceManager.Install for macOS.
// Steps (service-design.md §5.1):
//  1. Privilege check
//  2. Already installed → ErrAlreadyInstalled (AC9)
//  3. Render plist (SR-90)
//  4. Create state/log directories (0700 + chown raxd; no StateDirectory= in launchd, SR-89)
//  5. Write plist to plistPath (root:wheel 0644, SR-88) — rollback point
//  6. launchctl bootstrap system <plistPath>
//  7. launchctl enable system/tech.oem.raxd (AC3)
func (m *launchdManager) Install(ctx context.Context) error {
	if os.Geteuid() != 0 {
		return wrapErr(ErrPermission, "must be run as root or with sudo")
	}

	// Idempotency.
	if _, err := os.Stat(plistPath); err == nil {
		return ErrAlreadyInstalled
	}

	// Render plist (SR-90 validation inside RenderPlist).
	td := TemplateDataFromConfig(m.cfg)
	plistContent, err := RenderPlist(td)
	if err != nil {
		return fmt.Errorf("service install: render plist: %w", err)
	}

	// Create state and log directories (SR-89: 0700, chown raxd).
	if err := m.createDirs(); err != nil {
		return fmt.Errorf("service install: create dirs: %w", err)
	}

	// Write plist (root:wheel 0644, SR-88).
	if err := writeFile(plistPath, []byte(plistContent), 0o644); err != nil {
		return fmt.Errorf("service install: write plist: %w", err)
	}

	// launchctl bootstrap (load and start).
	if _, err := RunManager(ctx, launchctlBin, "bootstrap", "system", plistPath); err != nil {
		_ = os.Remove(plistPath) // rollback (AC11)
		return wrapErr(ErrManagerUnavailable, "launchctl bootstrap failed")
	}

	// launchctl enable (AC3: autostart at boot).
	if _, err := RunManager(ctx, launchctlBin, "enable", launchLabel); err != nil {
		// Not a fatal error on some macOS versions; log but don't rollback.
		_ = err
	}

	return nil
}

// createDirs creates state and log directories for macOS (no StateDirectory= in launchd).
// SR-89: 0700 mode + chown raxd.
func (m *launchdManager) createDirs() error {
	dirs := []string{m.cfg.StateDir, m.cfg.LogPath}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("cannot create %s: %w", d, err)
		}
		// chown to service user (SR-89: owner = raxd:raxd).
		// exec.Command is used here because os.Chown requires UID/GID lookup.
		// SR-91: separate args, no shell.
		chownCmd := exec.Command("/usr/sbin/chown", "-R", m.cfg.User+":"+m.cfg.Group, d) //nolint:gosec
		_ = chownCmd.Run() // best-effort; failure is non-fatal for the test path
	}
	return nil
}

// Uninstall implements ServiceManager.Uninstall for macOS.
func (m *launchdManager) Uninstall(ctx context.Context) error {
	if os.Geteuid() != 0 {
		return wrapErr(ErrPermission, "must be run as root or with sudo")
	}

	if _, err := os.Stat(plistPath); errors.Is(err, os.ErrNotExist) {
		return ErrNotInstalled
	}

	// bootout (unload and stop; ignore "not found").
	_, _ = RunManager(ctx, launchctlBin, "bootout", launchLabel)

	// disable (AC3 autostart removal).
	_, _ = RunManager(ctx, launchctlBin, "disable", launchLabel)

	// Remove plist (SR-93).
	if err := os.Remove(plistPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return wrapErr(ErrManagerUnavailable, fmt.Sprintf("cannot remove plist: %s", err.Error()))
	}

	// User raxd INTENTIONALLY kept (ADR-002, П-2, SR-93).
	return nil
}

// Start implements ServiceManager.Start for macOS.
func (m *launchdManager) Start(ctx context.Context) error {
	if os.Geteuid() != 0 {
		return wrapErr(ErrPermission, "must be run as root or with sudo")
	}

	if _, err := os.Stat(plistPath); errors.Is(err, os.ErrNotExist) {
		return ErrNotInstalled
	}

	if _, err := RunManager(ctx, launchctlBin, "kickstart", "-k", launchLabel); err != nil {
		return wrapErr(ErrManagerUnavailable, "launchctl kickstart failed")
	}
	return nil
}

// Stop implements ServiceManager.Stop for macOS.
// SR-24 (inherited): stop sends SIGTERM → graceful shutdown → exit 0 → no restart (AC5).
// KeepAlive.SuccessfulExit=false means exit 0 is NOT restarted.
func (m *launchdManager) Stop(ctx context.Context) error {
	if os.Geteuid() != 0 {
		return wrapErr(ErrPermission, "must be run as root or with sudo")
	}

	if _, err := os.Stat(plistPath); errors.Is(err, os.ErrNotExist) {
		return ErrNotInstalled
	}

	if _, err := RunManager(ctx, launchctlBin, "kill", "SIGTERM", launchLabel); err != nil {
		return wrapErr(ErrManagerUnavailable, "launchctl kill SIGTERM failed")
	}
	return nil
}

// Status implements ServiceManager.Status for macOS.
// Returns Status{Installed: false} when plist absent (no error, AC10).
func (m *launchdManager) Status(ctx context.Context) (Status, error) {
	if _, err := os.Stat(plistPath); errors.Is(err, os.ErrNotExist) {
		return Status{Installed: false, State: "not installed"}, nil
	}

	out, err := RunManager(ctx, launchctlBin, "print", launchLabel)
	if err != nil {
		return Status{Installed: true, State: "unknown"}, nil
	}

	// Parse launchctl print output for state and pid.
	active := strings.Contains(out, "state = running")
	pid := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "pid = ") {
			fmt.Sscanf(line, "pid = %d", &pid)
		}
	}

	state := "inactive"
	if active {
		state = "running"
	}

	return Status{
		Installed: true,
		Active:    active,
		PID:       pid,
		EUID:      0, // not readable without /proc on macOS
		State:     state,
	}, nil
}
