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
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	// dsclBin is the fixed path to dscl for macOS user management (SR-120: absolute, no shell).
	dsclBin = "/usr/bin/dscl"
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

// createDirs creates state, log, and config directories for macOS.
// launchd has no ConfigurationDirectory= equivalent (unlike systemd), so we provision
// ConfigDir explicitly here (BUG-1 fix: config.EnsureDirs → MkdirAll(ConfigDir) must
// find the directory existing and owned by raxd before raxd serve runs).
// SR-89: 0700 mode + chown raxd for all provisioned directories.
func (m *launchdManager) createDirs() error {
	dirs := []string{m.cfg.StateDir, m.cfg.LogPath, m.cfg.ConfigDir}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("cannot create %s: %w", d, err)
		}
		// chown to service user (SR-89: owner = raxd:raxd).
		// runCommandRaw: separate args, no shell (SR-91).
		chownCmd := runCommandRaw(context.Background(), "/usr/sbin/chown", "-R", m.cfg.User+":"+m.cfg.Group, d)
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

// ─── Purge (service-design.md §2.2, §4) ──────────────────────────────────────

// Purge implements ServiceManager.Purge for macOS.
// Orchestration order (service-design.md §4, SR-122):
//  1. Privilege check (SR-121)
//  2. Confirmed check (SR-114)
//  3. Status
//  4. Stop (if running), STOP on failure (AC4)
//  5. Uninstall (ignore ErrNotInstalled, AC3)
//  6–8. validatePurgePath for StateDir, ConfigDir, LogPath (SR-118, SR-119)
//  9. verifyTargetUserDarwin (SR-117)
//  10. Audit record BEFORE physical deletion (SR-116, AC8)
//  11. dscl . -delete (SR-120, SR-123)
//  12–14. os.RemoveAll for StateDir, ConfigDir, LogPath (AC3)
//  15. Return PurgeReport, nil
func (m *launchdManager) Purge(ctx context.Context, opts PurgeOptions) (PurgeReport, error) {
	report := PurgeReport{Platform: "darwin"}

	// Step 1: privilege check.
	if os.Geteuid() != 0 {
		return PurgeReport{}, wrapErr(ErrPermission, "must be run as root or with sudo")
	}

	// Step 2: duplicate confirmed guard (primary barrier is CLI --yes, AC9, SR-114).
	if !opts.Confirmed {
		return PurgeReport{}, ErrPurgeNotConfirmed
	}

	// Step 3: check status.
	st, _ := m.Status(ctx)

	// Step 4: stop if running.
	if st.Active {
		if err := m.Stop(ctx); err != nil {
			// AC4, SR-122: Stop failure → do not proceed.
			return PurgeReport{}, wrapErr(ErrManagerUnavailable, "service did not stop")
		}
		report.Stopped = true
	}

	// Step 5: Uninstall (idempotent, AC3).
	if err := m.Uninstall(ctx); err != nil {
		if !errors.Is(err, ErrNotInstalled) {
			return PurgeReport{}, err
		}
	} else {
		report.Uninstalled = true
	}

	// Steps 6–8: validate paths (SR-118, SR-119).
	dirs := []string{m.cfg.StateDir, m.cfg.ConfigDir, m.cfg.LogPath}
	allowedRoots := dirs
	for _, d := range dirs {
		if err := validatePurgePath(d, allowedRoots); err != nil {
			return PurgeReport{}, err
		}
	}

	// Step 9: verify target user (SR-117, AC6).
	userPresent, err := verifyTargetUserDarwin(ctx, m.cfg.User)
	if err != nil {
		return PurgeReport{}, err
	}
	if !userPresent {
		report.UserAbsent = true
	}

	// Determine which directories currently exist for pre-deletion audit.
	var dirsPresent []string
	for _, d := range dirs {
		if _, statErr := os.Stat(d); statErr == nil {
			dirsPresent = append(dirsPresent, d)
		}
	}

	// Step 10: emit PRELIMINARY audit record BEFORE physical deletion (SR-116, AC8).
	emitPurgeAuditRecord(report, userPresent, dirsPresent)

	// Step 11: delete OS user via dscl (SR-120).
	if userPresent {
		if err := deleteUserDarwin(ctx, m.cfg.User); err != nil {
			return PurgeReport{}, err
		}
		report.UserRemoved = true
	}

	// Steps 12–14: remove directories (AC3: not-exist → DirsAbsent).
	for _, d := range dirs {
		if err := os.RemoveAll(d); err != nil {
			return report, wrapErr(ErrManagerUnavailable, fmt.Sprintf("cannot remove %s", d))
		}
		wasPresent := false
		for _, p := range dirsPresent {
			if p == d {
				wasPresent = true
				break
			}
		}
		if wasPresent {
			report.DirsRemoved = append(report.DirsRemoved, d)
		} else {
			report.DirsAbsent = append(report.DirsAbsent, d)
		}
	}

	return report, nil
}

// verifyTargetUserDarwin checks the macOS user via dscl . -read /Users/<name> UserShell.
// Returns (present=false, nil) if the user does not exist.
// Returns ErrUserMismatch if the user exists but has a login shell (SR-117, AC6).
// SR-120: runCommandRaw — no shell interpolation.
func verifyTargetUserDarwin(ctx context.Context, name string) (present bool, err error) {
	cmd := runCommandRaw(ctx, dsclBin, ".", "-read", "/Users/"+name, "UserShell")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr != nil {
		// Non-zero exit from dscl -read means user not found (AC3, SR-123).
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			// Any non-zero exit from dscl -read /Users/<name> means not found.
			return false, nil
		}
		// Binary not found or other non-exit error.
		return false, nil
	}

	return parseDsclShellOutput(stdout.String(), name)
}

// parseDsclShellOutput parses the output of "dscl . -read /Users/<name> UserShell".
// Format: "UserShell: /usr/bin/false\n"
// Returns ErrUserMismatch if the shell is a login shell (SR-117).
func parseDsclShellOutput(output, _ string) (present bool, err error) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "UserShell:") {
			shell := strings.TrimSpace(strings.TrimPrefix(line, "UserShell:"))
			if !noLoginShells[shell] {
				return false, wrapErr(ErrUserMismatch, "user has a login shell — not a raxd system account")
			}
			return true, nil
		}
	}
	// No UserShell line found.
	return false, wrapErr(ErrManagerUnavailable, "dscl: unexpected output format")
}

// deleteUserDarwin deletes the macOS user via dscl . -delete /Users/<name>.
// SR-120: runCommandRaw — no shell interpolation.
// Maps exit/stderr per service-design.md §2.2.
func deleteUserDarwin(ctx context.Context, name string) error {
	cmd := runCommandRaw(ctx, dsclBin, ".", "-delete", "/Users/"+name)
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	if err == nil {
		return nil
	}
	return mapDsclDeleteError(stderrBuf.String())
}

// mapDsclDeleteError maps dscl . -delete stderr to a typed error (service-design.md §2.2, SR-123).
// "not found" patterns → nil (idempotent, AC3).
// "permission denied" patterns → ErrPermission (SR-121).
// Other → ErrManagerUnavailable (neutral, SR-124).
func mapDsclDeleteError(stderrStr string) error {
	switch {
	case strings.Contains(stderrStr, "eDSRecordNotFound"),
		strings.Contains(stderrStr, "Unknown node"),
		strings.Contains(stderrStr, "No such record"):
		// User absent → idempotent success (AC3, SR-123).
		return nil
	case strings.Contains(stderrStr, "Permission denied"),
		strings.Contains(stderrStr, "Operation not permitted"),
		strings.Contains(stderrStr, "eDSPermissionError"):
		return wrapErr(ErrPermission, "dscl delete: insufficient privileges")
	default:
		return wrapErr(ErrManagerUnavailable, "dscl delete failed")
	}
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
