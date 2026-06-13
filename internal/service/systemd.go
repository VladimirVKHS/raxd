// Package service — systemd.go: systemdManager implements ServiceManager for Linux.
//
// SR-83: creates user raxd:raxd (system, no-login-shell) at install time.
// SR-88: unit/drop-in written as root:root 0644.
// SR-89: StateDirectory=raxd + StateDirectoryMode=0700 in unit (explicit, not default).
// SR-92: idempotent install + rollback on failure (AC11).
// SR-93: uninstall removes unit + drop-in; user raxd INTENTIONALLY kept (П-2).
// SR-94: journald drop-in installed/removed with service registration.
// SR-96: stdlib only (os, os/exec, context).
//
// Build: compiles on all platforms (no build tag here).
// Linux-specific OS calls (useradd, systemctl) are reached only at runtime.
package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/log"
)

const (
	// unitPath is the canonical systemd unit location (SR-88: root:root 0644).
	unitPath = "/etc/systemd/system/raxd.service"

	// dropInDir is the journald drop-in directory (SR-94, AC8).
	dropInDir = "/etc/systemd/journald.conf.d"

	// dropInPath is the full path to the journald size-limit drop-in.
	dropInPath = "/etc/systemd/journald.conf.d/raxd.conf"

	// systemctlBin is the fixed path to systemctl (SR-91: no shell interpolation).
	systemctlBin = "/bin/systemctl"

	// useradd is the fixed path used for idempotent user creation (SR-83).
	useraddBin = "/usr/sbin/useradd"

	// Useradd exit code 9 means the user already exists → treat as success.
	useraddExitAlreadyExists = 9

	// userdelBin is the fixed path to userdel for purge (SR-120: absolute path, no shell).
	userdelBin = "/usr/sbin/userdel"

	// getentBin is the fixed path to getent for user verification (SR-120).
	getentBin = "/usr/bin/getent"
)

// noLoginShells is the set of acceptable shells for a system (no-login) raxd account.
// Used by verifyTargetUser on both platforms (service-design.md §9.2).
var noLoginShells = map[string]bool{
	"/usr/sbin/nologin": true,
	"/sbin/nologin":     true,
	"/usr/bin/false":    true,
}

// systemdManager implements ServiceManager for Linux + systemd.
type systemdManager struct {
	cfg Config
}

func newSystemdManager(cfg Config) *systemdManager {
	return &systemdManager{cfg: cfg}
}

// Install implements ServiceManager.Install.
// Steps (service-design.md §5.1):
//  1. Check for root/sufficient privileges → ErrPermission
//  2. Check already installed → ErrAlreadyInstalled (AC9)
//  3. Create system user raxd (idempotent, SR-83)
//  4. Render unit from template (SR-90)
//  5. Write unit to unitPath (root:root 0644) — rollback point A
//  6. Write journald drop-in — rollback point B
//  7. systemctl daemon-reload
//  8. systemctl enable raxd (AC3)
//
// Rollback on failure: removes unit/drop-in created in this run (AC11).
// User raxd is NOT rolled back (ADR-002, П-2).
func (m *systemdManager) Install(ctx context.Context) error {
	// Step 1: privilege check — writing to /etc/systemd/system/ requires root.
	if os.Geteuid() != 0 {
		return wrapErr(ErrPermission, "must be run as root or with sudo")
	}

	// Step 2: idempotency — already installed?
	if _, err := os.Stat(unitPath); err == nil {
		return ErrAlreadyInstalled
	}

	// Step 3: create system user raxd (idempotent).
	// exit code 0 = created; exit code 9 = already exists (OK, ADR-002).
	if err := m.createUser(ctx); err != nil {
		return fmt.Errorf("service install: create user: %w", err)
	}

	// Step 4: render unit.
	td := TemplateDataFromConfig(m.cfg)
	unitContent, err := RenderUnit(td)
	if err != nil {
		return fmt.Errorf("service install: render unit: %w", err)
	}

	// Rollback tracking.
	var createdUnit, createdDropIn bool

	// Step 5: write unit (root:root 0644, SR-88).
	if err := writeFile(unitPath, []byte(unitContent), 0o644); err != nil {
		return fmt.Errorf("service install: write unit: %w", err)
	}
	createdUnit = true

	// Step 6: write journald drop-in (SR-94, AC8).
	if err := os.MkdirAll(dropInDir, 0o755); err != nil {
		m.rollback(createdUnit, createdDropIn)
		return fmt.Errorf("service install: create drop-in dir: %w", err)
	}
	if err := writeFile(dropInPath, []byte(JournaldDropIn()), 0o644); err != nil {
		m.rollback(createdUnit, createdDropIn)
		return fmt.Errorf("service install: write drop-in: %w", err)
	}
	createdDropIn = true

	// Step 7: systemctl daemon-reload.
	if _, err := RunManager(ctx, systemctlBin, "daemon-reload"); err != nil {
		m.rollback(createdUnit, createdDropIn)
		return wrapErr(ErrManagerUnavailable, "systemctl daemon-reload failed")
	}

	// Step 8: systemctl enable raxd (AC3 — autostart at boot).
	if _, err := RunManager(ctx, systemctlBin, "enable", "raxd"); err != nil {
		m.rollback(createdUnit, createdDropIn)
		return wrapErr(ErrManagerUnavailable, "systemctl enable failed")
	}

	return nil
}

// rollback removes unit and drop-in created during a failed Install (AC11, SR-92).
// User raxd is NOT removed (ADR-002, П-2).
func (m *systemdManager) rollback(unit, dropIn bool) {
	if unit {
		_ = os.Remove(unitPath)
	}
	if dropIn {
		_ = os.Remove(dropInPath)
	}
}

// createUser creates the system user raxd idempotently (SR-83).
// useradd exit 0 → created; exit 9 → already exists → OK (ADR-002).
func (m *systemdManager) createUser(ctx context.Context) error {
	// SR-91: exec.Command with separate args — no shell interpolation.
	cmd := runCommandRaw(ctx, useraddBin,
		"--system",
		"--no-create-home",
		"--shell", "/usr/sbin/nologin",
		"--comment", "raxd daemon",
		m.cfg.User,
	)
	if cmd == nil {
		return wrapErr(ErrManagerUnavailable, "useradd binary not found")
	}
	err := cmd.Run()
	if err == nil {
		return nil // user created
	}
	// Check for exit code 9 (user already exists).
	if isExitCode(err, useraddExitAlreadyExists) {
		return nil // already exists — idempotent
	}
	// Other error — report but neutralize stderr (SR-95).
	return wrapErr(ErrPermission, "useradd failed")
}

// Uninstall implements ServiceManager.Uninstall.
// Steps (service-design.md §5.2):
//  1. Privilege check
//  2. Not installed → ErrNotInstalled (AC10)
//  3. systemctl stop raxd
//  4. systemctl disable raxd
//  5. systemctl daemon-reload
//  6. Remove unit file
//  7. Remove journald drop-in (SR-93)
//  8. systemctl daemon-reload (after removal)
//
// INTENTIONALLY KEPT: user raxd:raxd (ADR-002, П-2, SR-93).
func (m *systemdManager) Uninstall(ctx context.Context) error {
	if os.Geteuid() != 0 {
		return wrapErr(ErrPermission, "must be run as root or with sudo")
	}

	// Check if installed.
	if _, err := os.Stat(unitPath); errors.Is(err, os.ErrNotExist) {
		return ErrNotInstalled
	}

	// Stop (ignore "not running").
	_, _ = RunManager(ctx, systemctlBin, "stop", "raxd")

	// Disable (remove enable symlink).
	_, _ = RunManager(ctx, systemctlBin, "disable", "raxd")

	// Reload.
	_, _ = RunManager(ctx, systemctlBin, "daemon-reload")

	// Remove unit file.
	if err := os.Remove(unitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return wrapErr(ErrManagerUnavailable, fmt.Sprintf("cannot remove unit: %s", err.Error()))
	}

	// Remove journald drop-in (SR-93, AC10).
	if err := os.Remove(dropInPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return wrapErr(ErrManagerUnavailable, fmt.Sprintf("cannot remove drop-in: %s", err.Error()))
	}

	// Reload after removal.
	_, _ = RunManager(ctx, systemctlBin, "daemon-reload")

	return nil
}

// Start implements ServiceManager.Start.
func (m *systemdManager) Start(ctx context.Context) error {
	if os.Geteuid() != 0 {
		return wrapErr(ErrPermission, "must be run as root or with sudo")
	}

	if _, err := os.Stat(unitPath); errors.Is(err, os.ErrNotExist) {
		return ErrNotInstalled
	}

	if _, err := RunManager(ctx, systemctlBin, "start", "raxd"); err != nil {
		return wrapErr(ErrManagerUnavailable, "systemctl start failed")
	}
	return nil
}

// Stop implements ServiceManager.Stop.
// SR-24 (inherited): stop sends SIGTERM → graceful shutdown → exit 0 → no restart (AC5).
func (m *systemdManager) Stop(ctx context.Context) error {
	if os.Geteuid() != 0 {
		return wrapErr(ErrPermission, "must be run as root or with sudo")
	}

	if _, err := os.Stat(unitPath); errors.Is(err, os.ErrNotExist) {
		return ErrNotInstalled
	}

	if _, err := RunManager(ctx, systemctlBin, "stop", "raxd"); err != nil {
		return wrapErr(ErrManagerUnavailable, "systemctl stop failed")
	}
	return nil
}

// Status implements ServiceManager.Status.
// Returns Status{Installed: false} when not installed (no error, AC10).
// EUID is read from /proc/<pid>/status for AC6 verification.
func (m *systemdManager) Status(ctx context.Context) (Status, error) {
	// Check if unit file exists.
	if _, err := os.Stat(unitPath); errors.Is(err, os.ErrNotExist) {
		return Status{Installed: false, State: "not installed"}, nil
	}

	// Query systemctl for MainPID and ActiveState.
	out, err := RunManager(ctx, systemctlBin, "show",
		"-p", "MainPID,ActiveState,SubState,UnitFileState",
		"raxd",
	)
	if err != nil {
		// Unit is registered (file exists) but systemctl show failed.
		return Status{Installed: true, State: "unknown"}, nil
	}

	props := parseSystemctlProps(out)
	pid := 0
	if pidStr, ok := props["MainPID"]; ok {
		pid, _ = strconv.Atoi(pidStr)
	}

	activeState := props["ActiveState"]
	subState := props["SubState"]
	stateStr := activeState
	if subState != "" {
		stateStr = activeState + " (" + subState + ")"
	}

	active := activeState == "active"

	// Read EUID from /proc/<pid>/status (AC6: verify euid != 0).
	euid := 0
	if pid > 0 && active {
		euid = readProcEUID(pid)
	}

	return Status{
		Installed: true,
		Active:    active,
		PID:       pid,
		EUID:      euid,
		State:     stateStr,
	}, nil
}

// ─── Purge (service-design.md §2.1, §4) ──────────────────────────────────────

// Purge implements ServiceManager.Purge for Linux.
// Orchestration order (service-design.md §4, SR-122):
//  1. Privilege check (SR-121)
//  2. Confirmed check (SR-114)
//  3. Status — check if running
//  4. Stop (if running or unknown), STОP on failure (AC4)
//  5. Uninstall (ignore ErrNotInstalled, AC3)
//  6–7. validatePurgePath for StateDir and ConfigDir (SR-118, SR-119)
//  8. verifyTargetUserLinux (SR-117)
//  10. Audit record BEFORE physical deletion (SR-116, AC8)
//  11. Delete user via userdel (SR-120, SR-123)
//  12–13. os.RemoveAll for StateDir and ConfigDir (AC3)
//  15. Return PurgeReport, nil
func (m *systemdManager) Purge(ctx context.Context, opts PurgeOptions) (PurgeReport, error) {
	report := PurgeReport{Platform: "linux"}

	// Step 1: privilege check — userdel and directory removal require root.
	if os.Geteuid() != 0 {
		return PurgeReport{}, wrapErr(ErrPermission, "must be run as root or with sudo")
	}

	// Step 2: duplicate confirmed guard (primary barrier is CLI --yes, AC9, SR-114).
	if !opts.Confirmed {
		return PurgeReport{}, ErrPurgeNotConfirmed
	}

	// Step 3: check if the service is active.
	st, _ := m.Status(ctx)

	// Step 4: stop if running or status unknown.
	if st.Active || (!st.Installed && !st.Active) {
		// Stop only if the service appears to be running.
		// If not installed at all (not even installed), skip Stop.
		if st.Active {
			if err := m.Stop(ctx); err != nil {
				// AC4, SR-122: Stop failed → do not proceed to user/dir deletion.
				return PurgeReport{}, wrapErr(ErrManagerUnavailable, "service did not stop")
			}
			report.Stopped = true
		}
	}

	// Step 5: Uninstall (idempotent — ignore ErrNotInstalled, AC3).
	if err := m.Uninstall(ctx); err != nil {
		if !errors.Is(err, ErrNotInstalled) {
			// SR-122: uninstall failure stops the sequence.
			return PurgeReport{}, err
		}
	} else {
		report.Uninstalled = true
	}

	// Steps 6–7: validate paths before any destructive action (SR-118, SR-119).
	allowedRoots := []string{m.cfg.StateDir, m.cfg.ConfigDir}
	if err := validatePurgePath(m.cfg.StateDir, allowedRoots); err != nil {
		return PurgeReport{}, err
	}
	if err := validatePurgePath(m.cfg.ConfigDir, allowedRoots); err != nil {
		return PurgeReport{}, err
	}

	// Step 9: verify the target user (SR-117, AC6).
	userPresent, err := verifyTargetUserLinux(ctx, m.cfg.User)
	if err != nil {
		return PurgeReport{}, err
	}
	if !userPresent {
		report.UserAbsent = true
	}

	// Determine which directories exist for the pre-deletion audit record.
	var dirsPresent []string
	for _, d := range []string{m.cfg.StateDir, m.cfg.ConfigDir} {
		if _, statErr := os.Stat(d); statErr == nil {
			dirsPresent = append(dirsPresent, d)
		}
	}

	// Step 10: emit PRELIMINARY audit record BEFORE physical deletion (SR-116, AC8).
	// The record captures intent: what IS present and WILL be removed.
	// The final PurgeReport (UserRemoved/DirsRemoved) is built AFTER destructive steps.
	// (advisory от system-dev-guardian: разграничить предварительную запись и итоговый отчёт)
	emitPurgeAuditRecord(opts.AuditOut, report.Platform, userPresent, dirsPresent)

	// Step 11: delete OS user (SR-120: runCommandRaw, no shell).
	if userPresent {
		if err := deleteUserLinux(ctx, m.cfg.User); err != nil {
			return PurgeReport{}, err
		}
		report.UserRemoved = true
	}

	// Steps 12–13: remove directories (AC3: not-exist → DirsAbsent, not error).
	for _, d := range []string{m.cfg.StateDir, m.cfg.ConfigDir} {
		if err := os.RemoveAll(d); err != nil {
			// RemoveAll returns nil for non-existent paths, so a real error means FS issue.
			return report, wrapErr(ErrManagerUnavailable, fmt.Sprintf("cannot remove %s", d))
		}
		// Determine if the dir was present before (we tracked it in dirsPresent).
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

	// Step 15: return completed report.
	return report, nil
}

// verifyTargetUserLinux checks the OS user via getent passwd.
// Returns (present=false, nil) if the user does not exist (idempotent, AC3).
// Returns ErrUserMismatch if the user exists but has a login shell (SR-117, AC6).
// SR-120: runCommandRaw — no shell interpolation.
func verifyTargetUserLinux(ctx context.Context, name string) (present bool, err error) {
	cmd := runCommandRaw(ctx, getentBin, "passwd", name)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr != nil {
		// getent exit 2 = "key not found in database" → user absent → idempotent (SR-123).
		if isExitCode(runErr, 2) {
			return false, nil
		}
		// getent not found (binary missing, etc.)
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			// binary not found or other non-exit error
			return false, nil
		}
		// Other non-zero exit → unknown error.
		return false, wrapErr(ErrManagerUnavailable, "getent passwd failed")
	}

	line := strings.TrimSpace(stdout.String())
	if line == "" {
		// Empty output with exit 0 → user absent (AC3).
		return false, nil
	}

	return parsePasswdLine(line, name)
}

// parsePasswdLine parses a single getent passwd line and validates the user.
// Format: name:password:uid:gid:gecos:home:shell (7 colon-separated fields).
//
// Checks (service-design.md §2.1, SR-117, defense-in-depth):
//  1. Field 0 (name) must equal expectedName.
//  2. Field 2 (uid) must be < 1000 (systemd-default range for system accounts, [1,999]).
//  3. Field 6 (shell) must be in noLoginShells — primary protection against login shells.
//
// All three checks must pass; any failure returns ErrUserMismatch.
func parsePasswdLine(line, expectedName string) (present bool, err error) {
	fields := strings.Split(line, ":")
	if len(fields) < 7 {
		return false, wrapErr(ErrManagerUnavailable, "getent passwd: unexpected output format")
	}

	// Check 1: username must match (field 0).
	if fields[0] != expectedName {
		return false, wrapErr(ErrUserMismatch, "username does not match expected raxd account")
	}

	// Check 2: uid must be < 1000 (system account range, defense-in-depth, service-design.md §2.1).
	uid, uidErr := strconv.Atoi(fields[2])
	if uidErr != nil || uid <= 0 || uid >= 1000 {
		return false, wrapErr(ErrUserMismatch, "uid is not in system account range [1,999]")
	}

	// Check 3: shell must be a no-login shell — primary protection (SR-117).
	shell := fields[6]
	if !noLoginShells[shell] {
		return false, wrapErr(ErrUserMismatch, "user has a login shell — not a raxd system account")
	}

	return true, nil
}

// deleteUserLinux deletes the OS user via userdel (SR-120: fixed binary, separate args).
// Maps exit codes per service-design.md §2.1.
func deleteUserLinux(ctx context.Context, name string) error {
	cmd := runCommandRaw(ctx, userdelBin, name)
	err := cmd.Run()
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return mapUserdelExitCode(exitErr.ExitCode())
	}
	return wrapErr(ErrManagerUnavailable, "userdel: unexpected error")
}

// mapUserdelExitCode maps userdel exit codes to typed errors (service-design.md §2.1, SR-123).
//
//	0  → nil (user deleted)
//	6  → nil (user not found — idempotent, AC3, SR-123)
//	8  → error (user logged in)
//	10 → ErrPermission (cannot update group file, SR-121)
//	1  → ErrPermission (no permission / generic error, SR-121)
//	other → wrapped error
func mapUserdelExitCode(code int) error {
	switch code {
	case 0:
		return nil
	case 6:
		// "specified user doesn't exist" — idempotent success (AC3, SR-123).
		return nil
	case 1:
		return wrapErr(ErrPermission, "userdel: insufficient privileges")
	case 10:
		return wrapErr(ErrPermission, "userdel: cannot update group file")
	case 8:
		return wrapErr(ErrManagerUnavailable, "userdel: user is currently logged in")
	default:
		return wrapErr(ErrManagerUnavailable, fmt.Sprintf("userdel: unexpected exit code %d", code))
	}
}

// emitPurgeAuditRecord writes the PRELIMINARY audit record BEFORE physical deletion.
//
// SR-116: this function is called on step 10, before userdel/RemoveAll (steps 11–14).
// SR-124: only metadata is logged — no file contents, no keys, no secrets.
//
// Parameters:
//   - w: target writer (opts.AuditOut from PurgeOptions); nil → no-op (safe for tests).
//   - platform: "linux" or "darwin".
//   - userPresent: whether the raxd OS user exists at the time of the audit record.
//   - dirsPresent: list of state/config directories that exist at audit time.
//
// The record is "preliminary": it captures intent (what IS present and WILL be removed).
// The final PurgeReport with actual removal results is returned by Purge() after step 15.
func emitPurgeAuditRecord(w io.Writer, platform string, userPresent bool, dirsPresent []string) {
	if w == nil {
		// No audit sink provided — safe zero-value behaviour (existing tests/fakeManager).
		return
	}
	logger := log.New(w)
	logger.Info("purge intent",
		"action", "purge",
		"phase", "pre-deletion",
		"platform", platform,
		"user_present", userPresent,
		"dirs_present", dirsPresent,
	)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// writeFile writes content to path with the given mode.
// SR-88: caller must ensure mode is 0644 for unit/drop-in files.
func writeFile(path string, content []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create dir %s: %w", dir, err)
	}
	return os.WriteFile(path, content, mode)
}

// parseSystemctlProps parses "KEY=VALUE\n..." output from systemctl show.
func parseSystemctlProps(out string) map[string]string {
	props := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			props[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return props
}

// readProcEUID reads the effective UID of process pid from /proc/<pid>/status.
// Returns 0 if the file cannot be read (process may have exited).
// AC6: EUID is used to verify the daemon does not run as root.
func readProcEUID(pid int) int {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Uid:") {
			// Format: "Uid:\treal\teffective\tsaved\tfs"
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				euid, _ := strconv.Atoi(fields[2])
				return euid
			}
		}
	}
	return 0
}
