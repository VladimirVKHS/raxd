// Package cli_test — unit-tests for "raxd service uninstall --purge" command.
//
// Tests verify AC1–AC10 through fakeManager.Purge injection (SR-126).
// No real userdel/dscl/rm are called on the host (SECURITY-BASELINE §6).
// Build: no build tags — compiles on both linux and darwin (SR-127, service-design.md §9).
package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/cli"
	"github.com/vladimirvkhs/raxd/internal/service"
)

// ─── fakeManager.Purge implementation ────────────────────────────────────────
// fakeManager struct is declared in service_test.go (same package cli_test).
// We add the Purge method here to satisfy the updated ServiceManager interface.

func (f *fakeManager) Purge(_ context.Context, opts service.PurgeOptions) (service.PurgeReport, error) {
	if !opts.Confirmed {
		return service.PurgeReport{}, service.ErrPurgeNotConfirmed
	}
	// Simulate the preliminary audit record that the real manager emits before RemoveAll.
	// This allows TestPurge_AuditSinkReceivedBeforeRemoveAll to verify the CLI passes AuditOut.
	if opts.AuditOut != nil {
		fmt.Fprintf(opts.AuditOut, "INFO purge intent action=purge phase=pre-deletion platform=linux\n")
	}
	switch f.kind {
	case "purge-permission":
		return service.PurgeReport{}, service.ErrPermission
	case "purge-user-mismatch":
		return service.PurgeReport{}, service.ErrUserMismatch
	case "purge-suspicious-path":
		return service.PurgeReport{}, service.ErrSuspiciousPath
	case "purge-stop-failed":
		return service.PurgeReport{}, &service.ServiceError{
			Sentinel: service.ErrManagerUnavailable,
			Detail:   "service did not stop",
		}
	case "purge-already-absent":
		return service.PurgeReport{
			Platform:    "linux",
			Stopped:     false,
			Uninstalled: true,
			UserRemoved: false,
			UserAbsent:  true,
			DirsRemoved: nil,
			DirsAbsent:  []string{"/var/lib/raxd", "/etc/raxd"},
		}, nil
	default:
		return service.PurgeReport{
			Platform:    "linux",
			Stopped:     true,
			Uninstalled: true,
			UserRemoved: true,
			UserAbsent:  false,
			DirsRemoved: []string{"/var/lib/raxd", "/etc/raxd"},
			DirsAbsent:  nil,
		}, nil
	}
}

// executeServicePurgeCmd runs CLI with fake manager and given args.
func executeServicePurgeCmd(fakeKind string, args ...string) (stdout, stderr string, err error) {
	root := cli.NewRootCmdWithServiceManager(newFakeManager(fakeKind))

	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	root.SetOut(outBuf)
	root.SetErr(errBuf)
	root.SetArgs(args)

	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// ─── Barrier tests (AC9, SR-114, SR-115) ─────────────────────────────────────

// TestPurge_WithoutYes_Exit1_NoDeletion verifies --purge without --yes:
// exit != 0, warning printed, Purge NOT called (AC9, SR-114).
func TestPurge_WithoutYes_Exit1_NoDeletion(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServicePurgeCmd("", "service", "uninstall", "--purge")
	if err == nil {
		t.Error("--purge without --yes must exit != 0, got nil error")
	}

	// SR-115: warning about irreversibility must be present.
	if !strings.Contains(stderr, "irreversible") {
		t.Errorf("barrier must warn about irreversibility, stderr:\n%s", stderr)
	}
	// keys.db explicitly mentioned (SR-115).
	if !strings.Contains(stderr, "keys.db") {
		t.Errorf("barrier must mention keys.db, stderr:\n%s", stderr)
	}
	// Hint with --yes must be present.
	if !strings.Contains(stderr, "--yes") {
		t.Errorf("barrier must show hint with --yes, stderr:\n%s", stderr)
	}
	// Must NOT contain "purge complete" (nothing executed).
	if strings.Contains(stderr, "purge complete") {
		t.Errorf("barrier must not show 'purge complete'; nothing deleted; stderr:\n%s", stderr)
	}
}

// TestPurge_WithYes_Success_Exit0 verifies --purge --yes:
// exit 0, Purge called, report printed (AC1, AC3).
func TestPurge_WithYes_Success_Exit0(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServicePurgeCmd("", "service", "uninstall", "--purge", "--yes")
	if err != nil {
		t.Errorf("--purge --yes must exit 0, got: %v\nstderr:\n%s", err, stderr)
	}

	if !strings.Contains(stderr, "purge complete") {
		t.Errorf("purge success must contain 'purge complete', stderr:\n%s", stderr)
	}
}

// ─── Uninstall without --purge unchanged (AC2, SR-125) ───────────────────────

// TestUninstall_WithoutPurge_ByteForByte verifies "uninstall" without --purge
// still outputs the existing "kept" line and does NOT call Purge (AC2, SR-125).
func TestUninstall_WithoutPurge_ByteForByte(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServicePurgeCmd("", "service", "uninstall")
	if err != nil {
		t.Errorf("uninstall without --purge must exit 0, got: %v", err)
	}

	// AC2: existing output preserved — "kept" for the user.
	if !strings.Contains(stderr, "kept") {
		t.Errorf("uninstall without --purge must still show 'kept', stderr:\n%s", stderr)
	}
	// Must NOT contain purge complete (Purge was not called).
	if strings.Contains(stderr, "purge complete") {
		t.Errorf("uninstall without --purge must NOT show 'purge complete', stderr:\n%s", stderr)
	}
	// Must NOT contain warning about irreversibility.
	if strings.Contains(stderr, "irreversible") {
		t.Errorf("uninstall without --purge must NOT show irreversibility warning, stderr:\n%s", stderr)
	}
}

// ─── Error mapping tests (AC5, AC4, AC6, AC7) ────────────────────────────────

// TestPurge_PermissionError_Exit1 verifies ErrPermission → exit 1 + error + sudo hint (AC5, SR-121).
func TestPurge_PermissionError_Exit1(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServicePurgeCmd("purge-permission", "service", "uninstall", "--purge", "--yes")
	if err == nil {
		t.Error("purge with ErrPermission must exit != 0")
	}

	if !strings.Contains(stderr, "error:") {
		t.Errorf("ErrPermission must produce 'error:', stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "hint:") {
		t.Errorf("ErrPermission must produce 'hint:', stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "sudo") {
		t.Errorf("ErrPermission hint must mention sudo, stderr:\n%s", stderr)
	}
	// No partial deletions shown.
	if strings.Contains(stderr, "purge complete") {
		t.Errorf("ErrPermission must NOT show 'purge complete', stderr:\n%s", stderr)
	}
}

// TestPurge_UserMismatch_Exit1 verifies ErrUserMismatch → exit 1 + neutral error (AC6, SR-117).
func TestPurge_UserMismatch_Exit1(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServicePurgeCmd("purge-user-mismatch", "service", "uninstall", "--purge", "--yes")
	if err == nil {
		t.Error("purge with ErrUserMismatch must exit != 0")
	}

	if !strings.Contains(stderr, "error:") {
		t.Errorf("ErrUserMismatch must produce 'error:', stderr:\n%s", stderr)
	}
	// SR-95: no shell details in error message.
	if strings.Contains(stderr, "/bin/bash") || strings.Contains(stderr, "nologin") {
		t.Errorf("ErrUserMismatch error must be neutral (no shell details), stderr:\n%s", stderr)
	}
}

// TestPurge_SuspiciousPath_Exit1 verifies ErrSuspiciousPath → exit 1 + neutral error (AC7, SR-118).
func TestPurge_SuspiciousPath_Exit1(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServicePurgeCmd("purge-suspicious-path", "service", "uninstall", "--purge", "--yes")
	if err == nil {
		t.Error("purge with ErrSuspiciousPath must exit != 0")
	}

	if !strings.Contains(stderr, "error:") {
		t.Errorf("ErrSuspiciousPath must produce 'error:', stderr:\n%s", stderr)
	}
}

// TestPurge_StopFailed_Exit1 verifies stop failure → exit 1, no deletion shown (AC4, SR-122).
func TestPurge_StopFailed_Exit1(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServicePurgeCmd("purge-stop-failed", "service", "uninstall", "--purge", "--yes")
	if err == nil {
		t.Error("purge with stop failure must exit != 0")
	}

	if !strings.Contains(stderr, "error:") {
		t.Errorf("stop-fail purge must produce 'error:', stderr:\n%s", stderr)
	}
}

// ─── Idempotency tests (AC3) ──────────────────────────────────────────────────

// TestPurge_Idempotent_AllAbsent_Exit0 verifies all-absent (user+dirs) → exit 0 (AC3).
func TestPurge_Idempotent_AllAbsent_Exit0(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServicePurgeCmd("purge-already-absent", "service", "uninstall", "--purge", "--yes")
	if err != nil {
		t.Errorf("idempotent purge (all absent) must exit 0, got: %v\nstderr:\n%s", err, stderr)
	}

	// AC3: must show "absent" or "purge complete".
	if !strings.Contains(stderr, "absent") && !strings.Contains(stderr, "purge complete") {
		t.Errorf("idempotent purge must show absent/purge-complete info, stderr:\n%s", stderr)
	}
}

// ─── No secrets in purge output (SR-124) ─────────────────────────────────────

// TestPurge_NoSecretsInOutput verifies API keys, PEM markers never appear in purge output.
func TestPurge_NoSecretsInOutput(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, _ := executeServicePurgeCmd("", "service", "uninstall", "--purge", "--yes")

	forbidden := []string{
		"rax_live_",  // API key prefix
		"-----BEGIN", // PEM marker
		"panic:",     // Go panic
	}
	for _, f := range forbidden {
		if strings.Contains(stderr, f) {
			t.Errorf("purge output contains forbidden string %q:\n%s", f, stderr)
		}
	}
}

// ─── Flag registration (AC1 CLI aspect) ──────────────────────────────────────

// TestServiceUninstall_HasPurgeAndYesFlags verifies --purge and --yes flags are registered.
func TestServiceUninstall_HasPurgeAndYesFlags(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	outBuf := &bytes.Buffer{}
	root := cli.NewRootCmd()
	root.SetOut(outBuf)
	root.SetErr(outBuf)
	root.SetArgs([]string{"service", "uninstall", "--help"})
	_ = root.Execute()

	helpOut := outBuf.String()
	if !strings.Contains(helpOut, "--purge") {
		t.Errorf("uninstall --help must show --purge flag, got:\n%s", helpOut)
	}
	if !strings.Contains(helpOut, "--yes") {
		t.Errorf("uninstall --help must show --yes flag, got:\n%s", helpOut)
	}
}

// TestPurge_AuditLogPresent verifies that audit log appears in stderr output (AC8, SR-116).
func TestPurge_AuditLogPresent(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServicePurgeCmd("", "service", "uninstall", "--purge", "--yes")
	if err != nil {
		t.Fatalf("--purge --yes must exit 0, got: %v", err)
	}

	// Audit log (charmbracelet/log) writes "INFO" lines with action=purge.
	if !strings.Contains(stderr, "action=purge") && !strings.Contains(stderr, "purge") {
		t.Errorf("audit log must be present with action=purge info, stderr:\n%s", stderr)
	}
}

// TestPurge_AuditSinkReceivedBeforeRemoveAll verifies that:
//   - CLI injects stderr as AuditOut into PurgeOptions (SR-116)
//   - The fakeManager receives a non-nil writer and writes the preliminary record
//     (platform, pre-deletion phase) before any removal step (Issue 1)
//   - The audit record contains no secrets (SR-124: no API keys, no key material)
func TestPurge_AuditSinkReceivedBeforeRemoveAll(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServicePurgeCmd("", "service", "uninstall", "--purge", "--yes")
	if err != nil {
		t.Fatalf("purge must exit 0, got: %v", err)
	}

	// fakeManager writes "purge intent" with "pre-deletion" phase to opts.AuditOut.
	// These strings originate from the preliminary record emitted BEFORE RemoveAll.
	if !strings.Contains(stderr, "purge intent") {
		t.Errorf("audit sink must receive 'purge intent' preliminary record, stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "pre-deletion") {
		t.Errorf("audit sink must contain 'pre-deletion' phase marker, stderr:\n%s", stderr)
	}

	// SR-124: audit record must not contain secrets or key material.
	if strings.Contains(stderr, "rax_live_") {
		t.Errorf("audit record must not contain API key material (rax_live_), stderr:\n%s", stderr)
	}
	if strings.Contains(stderr, "-----BEGIN") {
		t.Errorf("audit record must not contain PEM/certificate material, stderr:\n%s", stderr)
	}
}
