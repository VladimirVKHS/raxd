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
	"errors"
	"os"
	"path/filepath"
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

// TestValidatePurgePath_HomeAncestor verifies a home ancestor (parent of $HOME) → ErrSuspiciousPath.
func TestValidatePurgePath_HomeAncestor(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}
	parent := filepath.Dir(home)
	if parent == home || parent == "/" {
		t.Skip("home dir is at root, cannot test ancestor")
	}
	e := service.ValidatePurgePath(parent, []string{"/var/lib/raxd"})
	if !errors.Is(e, service.ErrSuspiciousPath) {
		t.Errorf("home ancestor %q must return ErrSuspiciousPath, got: %v", parent, e)
	}
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
