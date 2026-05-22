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
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
)

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
