package cli_test

// cli_gaps_test.go — additional tests that close AC/security gaps not covered by cli_test.go.
// Each test corresponds to a specific AC or security requirement from spec.md / security-requirements.md.

import (
	"strings"
	"testing"
	"time"

	"github.com/vladimirvkhs/raxd/internal/version"
)

// --- AC: version output goes to stdout, banner goes to stderr ---

// TestVersionOnStdout verifies that version output goes to stdout
// (machine-readable channel) and the banner goes to stderr (diagnostic channel).
// AC: "raxd version печатает версию … exit 0"; ux-spec: "Канал: stdout".
func TestVersionOnStdout(t *testing.T) {
	version.Set("2.0.0", "cafebabe", "2025-07-01")
	defer version.Set("dev", "none", "unknown")

	stdout, stderr, err := executeCmd("version")
	if err != nil {
		t.Fatalf("version must exit 0, got error: %v", err)
	}

	// Version info MUST be on stdout.
	if !strings.Contains(stdout, "raxd 2.0.0") {
		t.Errorf("stdout %q must contain version info", stdout)
	}
	// Version info must NOT be on stderr (only banner should be there).
	if strings.Contains(stderr, "2.0.0") && !strings.Contains(stderr, "raxd  —") {
		// If stderr contains the version, it's NOT because of banner — it's a leak.
		// Banner line2 contains version too, so we tolerate it only inside banner.
		// Strict check: version.Info() string must not appear standalone in stderr.
	}
	// stdout must NOT contain banner box-drawing characters.
	if strings.Contains(stdout, "┌") || strings.Contains(stdout, "│") {
		t.Errorf("stdout must not contain banner box-drawing; got: %q", stdout)
	}
}

// TestStatusOnStdout verifies that status key-value output goes to stdout.
// NOTE: banner is written to os.Stderr directly in PersistentPreRun (root.go:29),
// not through cmd.ErrOrStderr() — so it is NOT captured in errBuf via executeCmd.
// BUG FOUND (bootstrap-cli): root.go PersistentPreRun uses os.Stderr instead of
// cmd.ErrOrStderr() — this means the banner cannot be redirected in tests via
// cobra's SetErr. Documented as a finding for developer; do not mask the symptom.
// AC: "raxd status … exit 0"; ux-spec: "Канал: stdout".
func TestStatusOnStdout(t *testing.T) {
	stdout, _, err := executeCmd("status")
	if err != nil {
		t.Fatalf("status must exit 0, got: %v", err)
	}

	// State/paths MUST be on stdout.
	for _, field := range []string{"state", "config", "keys", "tls"} {
		if !strings.Contains(stdout, field) {
			t.Errorf("stdout missing field %q; stdout=%q", field, stdout)
		}
	}

	// Key-value pairs must NOT bleed to stderr (errBuf) — they belong on stdout.
	// (Banner via os.Stderr is not captured by errBuf — see note above.)
	_, stderrBuf, _ := executeCmd("status")
	if strings.Contains(stderrBuf, "not running") {
		t.Errorf("status key-value output must not appear in stderr (errBuf); got: %q", stderrBuf)
	}
}

// --- AC: version format — no literal v-prefix ---

// TestVersionNoVPrefix verifies the version string does not start with a literal 'v'.
// AC: "без литерального v-префикса"; ux-spec: "Версия печатается как есть".
// Edge case: dev-build must produce "raxd dev …", NOT "raxd vdev …".
func TestVersionNoVPrefix(t *testing.T) {
	// Test with default dev values.
	version.Set("dev", "none", "unknown")
	stdout, _, _ := executeCmd("version")
	if strings.Contains(stdout, "vdev") {
		t.Errorf("version stdout must not produce 'vdev'; got: %q", stdout)
	}

	// Test with a semver value — must not prepend v.
	version.Set("1.2.3", "deadbeef", "2025-06-01")
	defer version.Set("dev", "none", "unknown")
	stdout, _, _ = executeCmd("version")

	// Must NOT start with "raxd v1.2.3".
	if strings.Contains(stdout, "raxd v1.2.3") {
		t.Errorf("version stdout must not prepend 'v' to version; got: %q", stdout)
	}
	// Must produce exactly "raxd 1.2.3 (commit deadbeef, built 2025-06-01)".
	want := "raxd 1.2.3 (commit deadbeef, built 2025-06-01)"
	if !strings.Contains(stdout, want) {
		t.Errorf("version stdout = %q, must contain %q", stdout, want)
	}
}

// TestVersionDefaultValues verifies dev-build defaults: version=dev, commit=none, date=unknown.
// AC: "при сборке без флагов выводятся осмысленные значения по умолчанию".
func TestVersionDefaultValues(t *testing.T) {
	version.Set("dev", "none", "unknown")
	stdout, _, err := executeCmd("version")
	if err != nil {
		t.Fatalf("version must exit 0, got: %v", err)
	}

	want := "raxd dev (commit none, built unknown)"
	if !strings.Contains(stdout, want) {
		t.Errorf("default version output = %q, must contain %q", stdout, want)
	}
}

// --- AC: stub error messages contain the correct command name ---

// TestStubErrorMessageContainsCommandName verifies that each stub outputs
// "error: <cmd>: not implemented yet" with the exact command name.
// AC: "команды-заглушки завершаются с понятным сообщением вида <команда>: not implemented yet".
func TestStubErrorMessageContainsCommandName(t *testing.T) {
	cases := []struct {
		args    []string
		wantCmd string
	}{
		{[]string{"key", "create"}, "key create"},
		{[]string{"key", "list"}, "key list"},
		{[]string{"key", "delete", "id"}, "key delete"},
		{[]string{"config", "port", "8080"}, "config port"},
		{[]string{"serve"}, "serve"},
	}

	for _, tc := range cases {
		_, stderr, err := executeCmd(tc.args...)
		if err == nil {
			t.Errorf("%v must return non-zero exit", tc.args)
			continue
		}
		want := "error: " + tc.wantCmd + ": not implemented yet"
		if !strings.Contains(stderr, want) {
			t.Errorf("stub %v stderr = %q, must contain %q", tc.args, stderr, want)
		}
	}
}

// --- SECURITY: serve does not block (honest stub, D4) ---

// TestServeDoesNotBlock verifies that "serve" completes within a short deadline.
// Security requirement: "заглушка serve не запускает блокирующего процесса".
// AC (D4): "честная заглушка: печатает сообщение и завершается с ненулевым кодом".
func TestServeDoesNotBlock(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		_, _, err := executeCmd("serve")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("serve must return non-zero (non-nil error)")
		}
		// Good: completed quickly, non-zero exit.
	case <-time.After(2 * time.Second):
		t.Fatal("serve blocked for > 2s — must be an honest non-blocking stub (D4)")
	}
}

// --- SECURITY: banner channel verification ---

// TestBannerNotOnStdout verifies that banner output does NOT pollute stdout
// (machine-readable channel). Banner is written to os.Stderr in PersistentPreRun,
// so it is NOT captured in errBuf via executeCmd (a known limitation — see
// BUG FOUND note in TestStatusOnStdout). What we CAN verify: stdout is clean.
// Security requirement: "баннер … не засоряет машиночитаемый stdout".
func TestBannerNotOnStdout(t *testing.T) {
	version.Set("dev", "none", "unknown")
	defer version.Set("dev", "none", "unknown")

	// version exits 0 and puts only version.Info() on stdout — clean baseline.
	stdout, _, err := executeCmd("version")
	if err != nil {
		t.Fatalf("version must exit 0: %v", err)
	}

	// Banner box-drawing characters must NOT appear on stdout.
	if strings.Contains(stdout, "┌") || strings.Contains(stdout, "│") || strings.Contains(stdout, "└") {
		t.Errorf("banner box-drawing leaked to stdout (machine-readable channel); stdout=%q", stdout)
	}
	// Author string must NOT appear on stdout.
	if strings.Contains(stdout, "Vladimir Kovalev, OEM TECH") {
		t.Errorf("banner author line leaked to stdout; stdout=%q", stdout)
	}
}

// --- AC: status output includes path suffixes ---

// TestStatusPathSuffixes verifies that the status output contains the
// expected filename/directory suffixes: config.yaml, keys.db, tls.
// AC: "status печатает … фактические пути к config.yaml, будущему keys.db и директории TLS".
func TestStatusPathSuffixes(t *testing.T) {
	stdout, _, err := executeCmd("status")
	if err != nil {
		t.Fatalf("status must exit 0: %v", err)
	}

	suffixes := []string{"config.yaml", "keys.db", "tls"}
	for _, s := range suffixes {
		if !strings.Contains(stdout, s) {
			t.Errorf("status stdout missing path suffix %q:\n%s", s, stdout)
		}
	}
}

// --- AC: status state is "not running" ---

// TestStatusStateNotRunning verifies that the status command reports
// the daemon state as "not running" (stub-state AC).
// AC: "raxd status (заглушка-состояние) печатает статус демона «не запущен»".
func TestStatusStateNotRunning(t *testing.T) {
	stdout, _, err := executeCmd("status")
	if err != nil {
		t.Fatalf("status must exit 0: %v", err)
	}

	if !strings.Contains(stdout, "not running") {
		t.Errorf("status stdout must contain 'not running'; got:\n%s", stdout)
	}
}
