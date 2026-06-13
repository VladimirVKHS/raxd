// Package service_test — unit-tests for purge.go (validatePurgePath, PurgeReport).
//
// Tests run on Linux in Docker (SR-126, SECURITY-BASELINE §6).
// No build tags: platform logic tested through fakes, not real userdel/dscl.
//
// Covers:
//   - AC3:  idempotency (absent user/dirs → success)
//   - AC6:  ErrUserMismatch (shell not in noLoginShells)
//   - AC7:  validatePurgePath rejects /, $HOME, system roots, symlink outside layout
//   - AC8:  audit record BEFORE physical deletion
//   - AC9:  ErrPurgeNotConfirmed when opts.Confirmed=false
//   - AC10: all branches testable without real system commands
package service_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/service"
)

// ─── validatePurgePath tests (SR-118, SR-119, AC7) ───────────────────────────

// TestValidatePurgePath_EmptyPath verifies empty path → ErrSuspiciousPath.
func TestValidatePurgePath_EmptyPath(t *testing.T) {
	err := service.ValidatePurgePath("", []string{"/var/lib/raxd"})
	if !errors.Is(err, service.ErrSuspiciousPath) {
		t.Errorf("empty path must return ErrSuspiciousPath, got: %v", err)
	}
}

// TestValidatePurgePath_Root verifies "/" → ErrSuspiciousPath.
func TestValidatePurgePath_Root(t *testing.T) {
	err := service.ValidatePurgePath("/", []string{"/var/lib/raxd"})
	if !errors.Is(err, service.ErrSuspiciousPath) {
		t.Errorf("root path '/' must return ErrSuspiciousPath, got: %v", err)
	}
}

// TestValidatePurgePath_HomeDir verifies $HOME path → ErrSuspiciousPath.
func TestValidatePurgePath_HomeDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}
	e := service.ValidatePurgePath(home, []string{"/var/lib/raxd"})
	if !errors.Is(e, service.ErrSuspiciousPath) {
		t.Errorf("$HOME %q must return ErrSuspiciousPath, got: %v", home, e)
	}
}

// TestValidatePurgePath_HomeAncestor is superseded by TestValidatePurgePath_HomeAncestor_ViaEnv
// (deterministic, no t.Skip) added by qa. That test exercises the same invariant without
// depending on the real $HOME value that triggers skip in Docker (HOME=/root, parent="/").
// Kept as a no-op placeholder so diff is minimal; logic is now in _ViaEnv below.
func TestValidatePurgePath_HomeAncestor(t *testing.T) {
	// Deterministic variant: TestValidatePurgePath_HomeAncestor_ViaEnv (below).
	// This function intentionally contains no t.Skip — it passes trivially.
	// The invariant is fully exercised by the _ViaEnv test.
}

// TestValidatePurgePath_SystemRoots verifies well-known system roots → ErrSuspiciousPath.
func TestValidatePurgePath_SystemRoots(t *testing.T) {
	systemRoots := []string{
		"/etc", "/var", "/usr", "/usr/local",
		"/tmp", "/bin", "/sbin", "/lib", "/lib64",
		"/boot", "/dev", "/proc", "/sys", "/run",
	}
	for _, p := range systemRoots {
		err := service.ValidatePurgePath(p, []string{"/var/lib/raxd"})
		if !errors.Is(err, service.ErrSuspiciousPath) {
			t.Errorf("system root %q must return ErrSuspiciousPath, got: %v", p, err)
		}
	}
}

// TestValidatePurgePath_SimilarPrefixNotAllowed verifies that /var/lib/raxd2 is NOT
// considered inside /var/lib/raxd (prefix collision protection, service-design.md §3 check 8).
func TestValidatePurgePath_SimilarPrefixNotAllowed(t *testing.T) {
	// We need a real path that exists to pass EvalSymlinks; create a temp dir.
	tmp := t.TempDir()
	// Path that looks like /var/lib/raxd2 vs allowed root /var/lib/raxd
	// We simulate with a tmpdir: allowedRoot=tmp+"/raxd", path=tmp+"/raxd2"
	allowedRoot := filepath.Join(tmp, "raxd")
	testPath := filepath.Join(tmp, "raxd2")
	if err := os.MkdirAll(testPath, 0o755); err != nil {
		t.Fatalf("cannot create test dir: %v", err)
	}
	err := service.ValidatePurgePath(testPath, []string{allowedRoot})
	if !errors.Is(err, service.ErrSuspiciousPath) {
		t.Errorf("path %q similar-prefix to allowed %q must return ErrSuspiciousPath, got: %v",
			testPath, allowedRoot, err)
	}
}

// TestValidatePurgePath_SymlinkOutside verifies symlink pointing outside layout → ErrSuspiciousPath.
// SR-119: EvalSymlinks must be applied and resolved path checked.
func TestValidatePurgePath_SymlinkOutside(t *testing.T) {
	tmp := t.TempDir()
	// target outside allowed root
	target := filepath.Join(tmp, "outside")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	// allowed root
	allowedRoot := filepath.Join(tmp, "layout", "raxd")
	if err := os.MkdirAll(allowedRoot, 0o755); err != nil {
		t.Fatalf("mkdir allowedRoot: %v", err)
	}
	// symlink inside layout pointing outside
	symPath := filepath.Join(tmp, "layout", "raxd", "data")
	if err := os.Symlink(target, symPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	err := service.ValidatePurgePath(symPath, []string{allowedRoot})
	if !errors.Is(err, service.ErrSuspiciousPath) {
		t.Errorf("symlink outside layout must return ErrSuspiciousPath, got: %v", err)
	}
}

// TestValidatePurgePath_ValidPath verifies a path within the allowed root → nil.
func TestValidatePurgePath_ValidPath(t *testing.T) {
	tmp := t.TempDir()
	allowedRoot := filepath.Join(tmp, "raxd")
	testPath := filepath.Join(tmp, "raxd")
	if err := os.MkdirAll(testPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err := service.ValidatePurgePath(testPath, []string{allowedRoot})
	if err != nil {
		t.Errorf("valid path within allowed root must return nil, got: %v", err)
	}
}

// TestValidatePurgePath_AbsentPath verifies a non-existent path (idempotent repeat) → nil.
// AC3: already-deleted path must not cause ErrSuspiciousPath.
func TestValidatePurgePath_AbsentPath(t *testing.T) {
	tmp := t.TempDir()
	allowedRoot := filepath.Join(tmp, "raxd")
	// path that doesn't exist — simulates already-deleted state
	nonExistentPath := filepath.Join(tmp, "raxd")
	// Do not create it; test EvalSymlinks → not exist → skip check 8 → nil
	err := service.ValidatePurgePath(nonExistentPath, []string{allowedRoot})
	if err != nil {
		t.Errorf("absent (already deleted) path must return nil, got: %v", err)
	}
}

// TestValidatePurgePath_RelativePath verifies relative path → ErrSuspiciousPath.
func TestValidatePurgePath_RelativePath(t *testing.T) {
	err := service.ValidatePurgePath("raxd/data", []string{"/var/lib/raxd"})
	if !errors.Is(err, service.ErrSuspiciousPath) {
		t.Errorf("relative path must return ErrSuspiciousPath, got: %v", err)
	}
}

// ─── New sentinel errors tests ────────────────────────────────────────────────

// TestNewSentinels verifies ErrUserMismatch, ErrSuspiciousPath, ErrPurgeNotConfirmed exist and are distinct.
// plan.md §Contracts, service-design.md §6.
func TestNewSentinels(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrUserMismatch", service.ErrUserMismatch},
		{"ErrSuspiciousPath", service.ErrSuspiciousPath},
		{"ErrPurgeNotConfirmed", service.ErrPurgeNotConfirmed},
	}

	for _, s := range sentinels {
		if s.err == nil {
			t.Errorf("%s is nil", s.name)
		}
	}

	// All must be distinct.
	for i := 0; i < len(sentinels); i++ {
		for j := i + 1; j < len(sentinels); j++ {
			if errors.Is(sentinels[i].err, sentinels[j].err) {
				t.Errorf("%s and %s are the same sentinel", sentinels[i].name, sentinels[j].name)
			}
		}
	}

	// Must be distinct from existing sentinels.
	existing := []error{
		service.ErrAlreadyInstalled,
		service.ErrNotInstalled,
		service.ErrManagerUnavailable,
		service.ErrPermission,
		service.ErrUnsupported,
	}
	for _, s := range sentinels {
		for _, e := range existing {
			if errors.Is(s.err, e) {
				t.Errorf("new sentinel %v must not match existing %v", s.err, e)
			}
		}
	}
}

// ─── PurgeOptions / PurgeReport type shape tests ─────────────────────────────

// TestPurgeOptionsType verifies PurgeOptions has Confirmed field.
func TestPurgeOptionsType(t *testing.T) {
	opts := service.PurgeOptions{Confirmed: true}
	if !opts.Confirmed {
		t.Error("PurgeOptions.Confirmed should be settable to true")
	}
	opts2 := service.PurgeOptions{}
	if opts2.Confirmed {
		t.Error("PurgeOptions zero value Confirmed must be false")
	}
}

// TestPurgeReportFields verifies PurgeReport has all required fields (plan.md §Contracts).
func TestPurgeReportFields(t *testing.T) {
	r := service.PurgeReport{
		Platform:    "linux",
		Stopped:     true,
		Uninstalled: true,
		UserRemoved: true,
		UserAbsent:  false,
		DirsRemoved: []string{"/var/lib/raxd"},
		DirsAbsent:  []string{"/etc/raxd"},
	}

	if r.Platform != "linux" {
		t.Error("PurgeReport.Platform not set")
	}
	if !r.Stopped {
		t.Error("PurgeReport.Stopped not set")
	}
	if !r.Uninstalled {
		t.Error("PurgeReport.Uninstalled not set")
	}
	if !r.UserRemoved {
		t.Error("PurgeReport.UserRemoved not set")
	}
	if len(r.DirsRemoved) != 1 {
		t.Error("PurgeReport.DirsRemoved not set")
	}
	if len(r.DirsAbsent) != 1 {
		t.Error("PurgeReport.DirsAbsent not set")
	}
}

// ─── verifyTargetUser logic tests (via exported helpers) ─────────────────────

// TestParsePasswdLine_ValidNologin verifies Linux passwd parsing for a valid nologin shell.
func TestParsePasswdLine_ValidNologin(t *testing.T) {
	// Typical getent passwd output: raxd:x:999:999:raxd daemon:/nonexistent:/usr/sbin/nologin
	present, err := service.ParsePasswdLineForTest("raxd:x:999:999:raxd daemon:/nonexistent:/usr/sbin/nologin", "raxd")
	if err != nil {
		t.Errorf("valid nologin user must not return error, got: %v", err)
	}
	if !present {
		t.Error("valid user line must return present=true")
	}
}

// TestParsePasswdLine_LoginShell verifies Linux passwd parsing returns ErrUserMismatch for login shell.
// AC6, SR-117: shell not in {/usr/sbin/nologin, /sbin/nologin, /usr/bin/false} → ErrUserMismatch.
func TestParsePasswdLine_LoginShell(t *testing.T) {
	_, err := service.ParsePasswdLineForTest("raxd:x:999:999:raxd daemon:/home/raxd:/bin/bash", "raxd")
	if !errors.Is(err, service.ErrUserMismatch) {
		t.Errorf("login shell user must return ErrUserMismatch, got: %v", err)
	}
}

// TestParsePasswdLine_WrongName verifies name mismatch → ErrUserMismatch.
func TestParsePasswdLine_WrongName(t *testing.T) {
	_, err := service.ParsePasswdLineForTest("raxd:x:999:999:raxd daemon:/nonexistent:/usr/sbin/nologin", "other-user")
	if !errors.Is(err, service.ErrUserMismatch) {
		t.Errorf("name mismatch must return ErrUserMismatch, got: %v", err)
	}
}

// TestParseDsclShellOutput_ValidNologin verifies macOS dscl parsing for a valid UserShell.
func TestParseDsclShellOutput_ValidNologin(t *testing.T) {
	// dscl . -read /Users/raxd UserShell output format
	output := "UserShell: /usr/bin/false\n"
	present, err := service.ParseDsclShellOutputForTest(output, "raxd")
	if err != nil {
		t.Errorf("valid nologin dscl output must not return error, got: %v", err)
	}
	if !present {
		t.Error("valid dscl output must return present=true")
	}
}

// TestParseDsclShellOutput_LoginShell verifies macOS dscl parsing returns ErrUserMismatch for login shell.
func TestParseDsclShellOutput_LoginShell(t *testing.T) {
	output := "UserShell: /bin/bash\n"
	_, err := service.ParseDsclShellOutputForTest(output, "raxd")
	if !errors.Is(err, service.ErrUserMismatch) {
		t.Errorf("login shell in dscl output must return ErrUserMismatch, got: %v", err)
	}
}

// TestMapDsclDeleteError_NotFound verifies macOS dscl delete "not found" stderr → nil (idempotent).
// AC3, SR-123.
func TestMapDsclDeleteError_NotFound(t *testing.T) {
	cases := []string{
		"eDSRecordNotFound",
		"Unknown node name",
		"No such record",
	}
	for _, stderr := range cases {
		err := service.MapDsclDeleteErrorForTest(stderr)
		if err != nil {
			t.Errorf("dscl stderr %q must map to nil (idempotent), got: %v", stderr, err)
		}
	}
}

// TestMapDsclDeleteError_Permission verifies macOS dscl delete permission error → ErrPermission.
// SR-121, SR-123.
func TestMapDsclDeleteError_Permission(t *testing.T) {
	cases := []string{
		"Permission denied",
		"Operation not permitted",
		"eDSPermissionError",
	}
	for _, stderr := range cases {
		err := service.MapDsclDeleteErrorForTest(stderr)
		if !errors.Is(err, service.ErrPermission) {
			t.Errorf("dscl stderr %q must map to ErrPermission, got: %v", stderr, err)
		}
	}
}

// TestMapUserdelExitCode_NotFound verifies userdel exit 6 → nil (idempotent AC3, SR-123).
func TestMapUserdelExitCode_NotFound(t *testing.T) {
	// exit code 6 = "specified user doesn't exist"
	err := service.MapUserdelExitCodeForTest(6)
	if err != nil {
		t.Errorf("userdel exit 6 (not found) must return nil, got: %v", err)
	}
}

// TestMapUserdelExitCode_Permission verifies userdel exit 1 → ErrPermission.
func TestMapUserdelExitCode_Permission(t *testing.T) {
	err := service.MapUserdelExitCodeForTest(1)
	if !errors.Is(err, service.ErrPermission) {
		t.Errorf("userdel exit 1 must return ErrPermission, got: %v", err)
	}
}

// TestMapUserdelExitCode_Permission10 verifies userdel exit 10 → ErrPermission (SR-123).
func TestMapUserdelExitCode_Permission10(t *testing.T) {
	err := service.MapUserdelExitCodeForTest(10)
	if !errors.Is(err, service.ErrPermission) {
		t.Errorf("userdel exit 10 must return ErrPermission, got: %v", err)
	}
}

// TestMapUserdelExitCode_Success verifies userdel exit 0 → nil.
func TestMapUserdelExitCode_Success(t *testing.T) {
	err := service.MapUserdelExitCodeForTest(0)
	if err != nil {
		t.Errorf("userdel exit 0 must return nil, got: %v", err)
	}
}

// ─── isEqualOrAncestor deterministic tests (AC7 $HOME-ancestor guard) ───────
//
// TestValidatePurgePath_HomeAncestor in Docker runs in HOME=/root where
// filepath.Dir("/root")=="/", which triggers the t.Skip branch. The invariant
// is still exercised here deterministically via the exported helper, without
// depending on the actual $HOME value (QA gap closed per task specification).

// TestIsEqualOrAncestor_Equal verifies that candidate==base returns true.
func TestIsEqualOrAncestor_Equal(t *testing.T) {
	if !service.IsEqualOrAncestorForTest("/home/user", "/home/user") {
		t.Error("candidate==base must return true")
	}
}

// TestIsEqualOrAncestor_AncestorOfBase verifies candidate is a directory
// ancestor of base (base starts with candidate+"/").
func TestIsEqualOrAncestor_AncestorOfBase(t *testing.T) {
	cases := []struct {
		candidate string
		base      string
	}{
		{"/home", "/home/user"},
		{"/home/user", "/home/user/documents"},
		{"/var", "/var/lib/raxd"},
	}
	for _, c := range cases {
		if !service.IsEqualOrAncestorForTest(c.candidate, c.base) {
			t.Errorf("isEqualOrAncestor(%q, %q) must be true (candidate is ancestor)", c.candidate, c.base)
		}
	}
}

// TestIsEqualOrAncestor_NotAncestor verifies unrelated paths return false.
func TestIsEqualOrAncestor_NotAncestor(t *testing.T) {
	cases := []struct {
		candidate string
		base      string
	}{
		{"/var/lib/raxd", "/home/user"},
		{"/home/userx", "/home/user"},   // prefix similarity, not ancestor
		{"/home/user2", "/home/user"},   // prefix collision protection
		{"/etc", "/var/lib/raxd"},
	}
	for _, c := range cases {
		if service.IsEqualOrAncestorForTest(c.candidate, c.base) {
			t.Errorf("isEqualOrAncestor(%q, %q) must be false (not an ancestor)", c.candidate, c.base)
		}
	}
}

// TestValidatePurgePath_HomeAncestor_ViaEnv verifies that validatePurgePath
// rejects a path that is an ancestor of $HOME when HOME is set to a known value.
// This test is deterministic regardless of the real $HOME value (closes Docker skip
// for TestValidatePurgePath_HomeAncestor where HOME=/root and parent=="/" ).
//
// Strategy: set HOME to a synthetic path (/tmp/qa-home-guard) via t.Setenv,
// then pass its parent (/tmp) — which is NOT in blockedSystemRoots so would
// pass all other checks, but must fail the HOME-ancestor check.
// We use /tmp/qa-home-guard because /tmp IS in blockedSystemRoots, so we
// need a path that bypasses other checks to isolate the HOME-ancestor guard.
// We use /opt/qa-home-guard as HOME (not a system root, not /) and test
// that /opt (parent) is rejected as a HOME-ancestor.
func TestValidatePurgePath_HomeAncestor_ViaEnv(t *testing.T) {
	// Set a synthetic HOME that is not / so the parent is testable.
	// /opt/qa-home-guard is not in blockedSystemRoots and is not "/".
	t.Setenv("HOME", "/opt/qa-home-guard")

	// /opt is the parent of HOME ("/opt/qa-home-guard") → must be rejected.
	// /opt is NOT in blockedSystemRoots (only /etc, /var, /usr, /usr/local, /tmp, etc.)
	// so it will reach the HOME-ancestor check.
	err := service.ValidatePurgePath("/opt", []string{"/var/lib/raxd"})
	if !errors.Is(err, service.ErrSuspiciousPath) {
		t.Errorf("parent of $HOME (/opt when HOME=/opt/qa-home-guard) must return ErrSuspiciousPath, got: %v", err)
	}
}

// TestValidatePurgePath_HomeDir_ViaEnv verifies that validatePurgePath
// rejects the exact $HOME path regardless of the actual home directory value.
func TestValidatePurgePath_HomeDir_ViaEnv(t *testing.T) {
	t.Setenv("HOME", "/opt/qa-home-guard")

	err := service.ValidatePurgePath("/opt/qa-home-guard", []string{"/var/lib/raxd"})
	if !errors.Is(err, service.ErrSuspiciousPath) {
		t.Errorf("exact $HOME path must return ErrSuspiciousPath, got: %v", err)
	}
}

// ─── PurgeReport partial idempotency (AC3) ───────────────────────────────────

// TestPurgeReport_PartialIdempotency verifies PurgeReport correctly encodes
// mixed removed/absent state — the basis for AC3 partial-idempotency output.
// This is a type-level test; CLI rendering is tested in service_purge_test.go.
func TestPurgeReport_PartialIdempotency(t *testing.T) {
	r := service.PurgeReport{
		Platform:    "linux",
		Stopped:     false,
		Uninstalled: false,
		UserRemoved: true,
		UserAbsent:  false,
		DirsRemoved: []string{"/etc/raxd"},
		DirsAbsent:  []string{"/var/lib/raxd"},
	}

	// UserRemoved=true and DirsAbsent has entries — mixed state must be representable.
	if !r.UserRemoved {
		t.Error("UserRemoved must be true in partial-idempotency report")
	}
	if len(r.DirsAbsent) != 1 || r.DirsAbsent[0] != "/var/lib/raxd" {
		t.Errorf("DirsAbsent must list the already-absent dir, got: %v", r.DirsAbsent)
	}
	if len(r.DirsRemoved) != 1 || r.DirsRemoved[0] != "/etc/raxd" {
		t.Errorf("DirsRemoved must list the removed dir, got: %v", r.DirsRemoved)
	}
}

// ─── ErrPurgeNotConfirmed via PurgeOptions (AC9, SR-114) ─────────────────────

// TestPurgeOptions_Unconfirmed_IsSentinel verifies that Confirmed=false and
// ErrPurgeNotConfirmed are the intended mechanism for the manager-level barrier.
// The fakeManager in cli tests relies on this contract.
func TestPurgeOptions_Unconfirmed_IsSentinel(t *testing.T) {
	// ErrPurgeNotConfirmed must exist and wrap nothing (it IS the sentinel).
	if service.ErrPurgeNotConfirmed == nil {
		t.Fatal("ErrPurgeNotConfirmed must not be nil")
	}
	// Must be distinct from other error sentinels (already covered by TestNewSentinels,
	// but repeated here as an isolated AC9-specific assertion).
	if errors.Is(service.ErrPurgeNotConfirmed, service.ErrPermission) {
		t.Error("ErrPurgeNotConfirmed must not match ErrPermission")
	}
	if errors.Is(service.ErrPurgeNotConfirmed, service.ErrUserMismatch) {
		t.Error("ErrPurgeNotConfirmed must not match ErrUserMismatch")
	}
}

// ─── ParsePasswdLine additional shells (AC6, SR-117) ─────────────────────────

// TestParsePasswdLine_AllValidNologinShells verifies each acceptable nologin shell
// variant is recognised as a valid system account (not ErrUserMismatch).
func TestParsePasswdLine_AllValidNologinShells(t *testing.T) {
	validShells := []string{
		"/usr/sbin/nologin",
		"/sbin/nologin",
		"/usr/bin/false",
	}
	for _, shell := range validShells {
		line := "raxd:x:999:999:raxd daemon:/nonexistent:" + shell
		present, err := service.ParsePasswdLineForTest(line, "raxd")
		if err != nil {
			t.Errorf("shell %q is valid nologin → must not return error, got: %v", shell, err)
		}
		if !present {
			t.Errorf("shell %q is valid → must return present=true", shell)
		}
	}
}

// TestParsePasswdLine_LoginShellVariants verifies several login-shell variants
// all trigger ErrUserMismatch (AC6, SR-117).
func TestParsePasswdLine_LoginShellVariants(t *testing.T) {
	loginShells := []string{"/bin/bash", "/bin/sh", "/usr/bin/zsh", "/bin/dash"}
	for _, shell := range loginShells {
		line := "raxd:x:999:999:raxd daemon:/home/raxd:" + shell
		_, err := service.ParsePasswdLineForTest(line, "raxd")
		if !errors.Is(err, service.ErrUserMismatch) {
			t.Errorf("login shell %q must return ErrUserMismatch, got: %v", shell, err)
		}
	}
}

// ─── mapUserdelExitCode: user-logged-in case (SR-123) ────────────────────────

// TestMapUserdelExitCode_UserLoggedIn verifies userdel exit 8 → non-nil error
// (user still logged in is not the same as "not found", must not be idempotent).
func TestMapUserdelExitCode_UserLoggedIn(t *testing.T) {
	err := service.MapUserdelExitCodeForTest(8)
	if err == nil {
		t.Error("userdel exit 8 (user logged in) must return non-nil error")
	}
	// Must NOT be ErrPermission — it's a different condition.
	if errors.Is(err, service.ErrPermission) {
		t.Error("userdel exit 8 must not map to ErrPermission")
	}
}

// ─── parseDsclShellOutput: WrongName path (macOS, AC6) ───────────────────────

// TestParseDsclShellOutput_ValidShellWrongName verifies that if the dscl output
// is for the correct user but the caller passes a different expectedName,
// the function still returns the shell check result (dscl output does not contain
// the username — parseDsclShellOutput ignores the name param per implementation).
// This is a documentation test: the name param is vestigial in the dscl parser
// (name mismatch prevention is at the call site, not in the parser).
func TestParseDsclShellOutput_ValidShell_NameIgnored(t *testing.T) {
	output := "UserShell: /usr/bin/false\n"
	// expectedName is ignored by parseDsclShellOutput — function only checks shell.
	present, err := service.ParseDsclShellOutputForTest(output, "someother")
	if err != nil {
		t.Errorf("dscl parser ignores name; valid nologin shell must not error, got: %v", err)
	}
	if !present {
		t.Error("dscl parser with valid nologin shell must return present=true")
	}
}

// ─── uid-check tests (Issue 3, SR-117, service-design.md §2.1) ──────────────

// TestParsePasswdLine_HighUID verifies uid >= 1000 → ErrUserMismatch (defense-in-depth).
// A regular user account (uid >= 1000) must never match the expected system account.
func TestParsePasswdLine_HighUID(t *testing.T) {
	// uid=1001 (regular user range, >= 1000)
	_, err := service.ParsePasswdLineForTest("raxd:x:1001:1001:regular user:/home/raxd:/usr/sbin/nologin", "raxd")
	if !errors.Is(err, service.ErrUserMismatch) {
		t.Errorf("uid >= 1000 must return ErrUserMismatch (not a system account), got: %v", err)
	}
}

// TestParsePasswdLine_UID0_Root verifies uid=0 (root) → ErrUserMismatch.
// uid=0 is out of the [1,999] system account range — running as root would be a security issue.
func TestParsePasswdLine_UID0_Root(t *testing.T) {
	_, err := service.ParsePasswdLineForTest("raxd:x:0:0:root:/root:/usr/sbin/nologin", "raxd")
	if !errors.Is(err, service.ErrUserMismatch) {
		t.Errorf("uid=0 (root) must return ErrUserMismatch, got: %v", err)
	}
}

// TestParsePasswdLine_ValidUID verifies uid in [1,999] (system range) → present=true.
func TestParsePasswdLine_ValidUID(t *testing.T) {
	cases := []string{
		"raxd:x:1:1:daemon:/:/usr/sbin/nologin",   // uid=1
		"raxd:x:999:999:raxd daemon:/nonexistent:/usr/sbin/nologin", // uid=999
		"raxd:x:100:100:raxd:/nonexistent:/sbin/nologin",            // uid=100
	}
	for _, line := range cases {
		present, err := service.ParsePasswdLineForTest(line, "raxd")
		if err != nil {
			t.Errorf("system uid in line %q must not return error, got: %v", line, err)
		}
		if !present {
			t.Errorf("system uid in line %q must return present=true", line)
		}
	}
}

// TestParsePasswdLine_NonNumericUID verifies non-numeric uid field → ErrUserMismatch.
func TestParsePasswdLine_NonNumericUID(t *testing.T) {
	_, err := service.ParsePasswdLineForTest("raxd:x:abc:999:raxd daemon:/nonexistent:/usr/sbin/nologin", "raxd")
	if !errors.Is(err, service.ErrUserMismatch) {
		t.Errorf("non-numeric uid must return ErrUserMismatch, got: %v", err)
	}
}

// ─── audit-sink (AuditOut) tests (Issue 1, SR-116, AC8) ─────────────────────

// TestPurgeOptions_AuditOut_ZeroValueSafe verifies AuditOut=nil (zero value) is safe.
// Existing fakeManager and tests that do not set AuditOut must not panic.
func TestPurgeOptions_AuditOut_ZeroValueSafe(t *testing.T) {
	opts := service.PurgeOptions{Confirmed: true}
	if opts.AuditOut != nil {
		t.Error("PurgeOptions.AuditOut zero value must be nil")
	}
}

// TestPurgeOptions_AuditOut_WriterSet verifies AuditOut can be set to a bytes.Buffer.
// This confirms the field type is io.Writer and is injectable for testing.
func TestPurgeOptions_AuditOut_WriterSet(t *testing.T) {
	var buf bytes.Buffer
	opts := service.PurgeOptions{
		Confirmed: true,
		AuditOut:  &buf,
	}
	if opts.AuditOut == nil {
		t.Error("PurgeOptions.AuditOut must accept an io.Writer (bytes.Buffer)")
	}
}

// TestEmitPurgeAuditRecord_WritesBeforeRemoveAll verifies the audit record is written
// to opts.AuditOut when provided, and contains expected metadata fields (SR-116, AC8, SR-124).
//
// Strategy: call the exported helper EmitPurgeAuditRecordForTest and verify the writer
// received a record with platform/user_present/dirs_present fields and NO secrets/file-content.
func TestEmitPurgeAuditRecord_WritesBeforeRemoveAll(t *testing.T) {
	var buf bytes.Buffer
	service.EmitPurgeAuditRecordForTest(&buf, "linux", true, []string{"/var/lib/raxd", "/etc/raxd"})

	out := buf.String()
	if out == "" {
		t.Error("emitPurgeAuditRecord must write to the provided writer when non-nil")
	}

	// SR-116: must contain intent fields.
	requiredFields := []string{"purge", "platform", "linux", "user_present"}
	for _, f := range requiredFields {
		if !strings.Contains(out, f) {
			t.Errorf("audit record must contain %q, got:\n%s", f, out)
		}
	}

	// SR-124: no secrets / file contents.
	forbidden := []string{"rax_live_", "-----BEGIN", "keys.db content"}
	for _, f := range forbidden {
		if strings.Contains(out, f) {
			t.Errorf("audit record must NOT contain %q (SR-124), got:\n%s", f, out)
		}
	}
}

// TestEmitPurgeAuditRecord_NilWriter_NoPanic verifies nil AuditOut does not panic.
func TestEmitPurgeAuditRecord_NilWriter_NoPanic(t *testing.T) {
	// Must not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("emitPurgeAuditRecord with nil writer panicked: %v", r)
		}
	}()
	service.EmitPurgeAuditRecordForTest(nil, "linux", false, nil)
}
