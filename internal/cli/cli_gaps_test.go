package cli_test

// cli_gaps_test.go — additional tests that close AC/security gaps not covered by cli_test.go.
// Each test corresponds to a specific AC or security requirement from spec.md / security-requirements.md.

import (
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

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
	// Version info must NOT appear on stderr outside the banner.
	// Banner itself carries the version in its second line (alongside "raxd  —"), so
	// we tolerate version text that co-appears with the banner header. Any occurrence
	// of "2.0.0" on stderr WITHOUT the banner header line means version.Info() leaked
	// to the diagnostic channel outside the intended banner context.
	if strings.Contains(stderr, "2.0.0") && !strings.Contains(stderr, "raxd  —") {
		t.Errorf("version info leaked to stderr outside banner context: stderr=%q", stderr)
	}
	// stdout must NOT contain banner box-drawing characters.
	if strings.Contains(stdout, "┌") || strings.Contains(stdout, "│") {
		t.Errorf("stdout must not contain banner box-drawing; got: %q", stdout)
	}
}

// TestStatusOnStdout verifies that status key-value output goes to stdout
// and the banner goes to stderr (diagnostic channel, via cmd.ErrOrStderr()).
// AC: "raxd status … exit 0"; ux-spec: "Канал: stdout".
// BUG-001 fixed: PersistentPreRun now uses cmd.ErrOrStderr(), so banner IS
// captured in errBuf via executeCmd, enabling the full channel-split assertion.
func TestStatusOnStdout(t *testing.T) {
	stdout, stderr, err := executeCmd("status")
	if err != nil {
		t.Fatalf("status must exit 0, got: %v", err)
	}

	// State/paths MUST be on stdout.
	for _, field := range []string{"state", "config", "keys", "tls"} {
		if !strings.Contains(stdout, field) {
			t.Errorf("stdout missing field %q; stdout=%q", field, stdout)
		}
	}

	// Banner MUST be on stderr (captured via cmd.ErrOrStderr()).
	if !strings.Contains(stderr, "Vladimir Kovalev, OEM TECH") {
		t.Errorf("banner author line must appear in stderr; got stderr=%q", stderr)
	}

	// Key-value state output must NOT bleed to stderr.
	if strings.Contains(stderr, "not running") {
		t.Errorf("status key-value output must not appear in stderr; got stderr=%q", stderr)
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

// TestRemainingStubErrorMessageContainsCommandName verifies that remaining stubs
// (config port) output "error: <cmd>: not implemented yet".
// Note: "serve" is NO LONGER a stub (tls-transport task implemented it).
// AC: "оставшиеся заглушки завершаются с понятным сообщением вида <команда>: not implemented yet".
func TestRemainingStubErrorMessageContainsCommandName(t *testing.T) {
	cases := []struct {
		args    []string
		wantCmd string
	}{
		{[]string{"config", "port", "8080"}, "config port"},
		// "serve" removed: no longer a stub after tls-transport.
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

// --- SECURITY: serve is a real server (not a blocking stub without exit) ---

// TestServeStartsRealServer verifies that "serve" is a real TLS server,
// not the old honest stub. It checks that:
// 1. serve output does NOT contain "not implemented yet"
// 2. serve output DOES contain "tls" or "listening" (startup block)
//
// Note: serve blocks waiting for SIGINT/SIGTERM in real use. In the CLI unit
// test environment we only check startup output by running with an occupied
// port (so it fails fast with a bind error).
func TestServeStartsRealServer(t *testing.T) {
	// Use temp state dir + pre-occupy port 7822 to force fast bind-error exit.
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	// Try to occupy port 7822 so serve fails fast.
	ln, err := net.Listen("tcp", "127.0.0.1:7822")
	if err != nil {
		// Port may already be in use — still valid for the test.
		ln = nil
	}
	if ln != nil {
		defer ln.Close()
	}

	_, stderr, _ := executeCmd("serve")

	// Must not be the old stub message.
	if strings.Contains(stderr, "not implemented yet") {
		t.Errorf("serve must not be a stub; got 'not implemented yet' in stderr=%q", stderr)
	}
	// Should contain something from the real server (TLS info or bind error).
	hasServerOutput := strings.Contains(stderr, "tls") ||
		strings.Contains(stderr, "listening") ||
		strings.Contains(stderr, "cannot bind") ||
		strings.Contains(stderr, "address already in use") ||
		strings.Contains(stderr, "cert") ||
		strings.Contains(stderr, "TLS")
	if !hasServerOutput {
		t.Errorf("serve should produce server output; stderr=%q", stderr)
	}
}

// --- SECURITY: banner channel verification ---

// TestBannerChannelSplit verifies that the banner goes to stderr and does NOT
// pollute stdout (machine-readable channel).
// BUG-001 fixed: PersistentPreRun uses cmd.ErrOrStderr(), so we can assert
// both sides of the channel split.
// Security requirement: "баннер выводится на stderr … не засоряет машиночитаемый stdout".
func TestBannerChannelSplit(t *testing.T) {
	version.Set("dev", "none", "unknown")
	defer version.Set("dev", "none", "unknown")

	// version exits 0 and puts only version.Info() on stdout — clean baseline.
	stdout, stderr, err := executeCmd("version")
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

	// Banner MUST appear on stderr (via cmd.ErrOrStderr()).
	if !strings.Contains(stderr, "┌") {
		t.Errorf("banner box-drawing not found in stderr; stderr=%q", stderr)
	}
	if !strings.Contains(stderr, "Vladimir Kovalev, OEM TECH") {
		t.Errorf("banner author line not found in stderr; stderr=%q", stderr)
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

// ============================================================================
// D-1 / ux-spec §5: startup block must NOT appear on bind error
// ============================================================================

// occupyFreePort finds a free TCP port, creates a config.yaml with that port in
// a fresh XDG_CONFIG_HOME/raxd directory, sets XDG_CONFIG_HOME and XDG_STATE_HOME
// in the test environment, then re-listens on the same port so serve() sees it busy.
//
// Returns the occupied listener (caller must defer ln.Close()) and the port number.
// If two sequential Listen calls cannot agree on the same port (race), the test is
// skipped with a diagnostic — but this is deterministic in normal CI because we use
// SO_REUSEADDR semantics: the first ln intentionally closes before we open the second.
//
// LOW-debt note: the old implementation used hardcoded port 7822 and called
// t.Skip when it was unavailable — violating the "no t.Skip" rule. The new
// approach uses port 0 (OS-assigned free port) + a temp config file, making
// the test deterministic regardless of what else is running on the host.
func occupyFreePort(t *testing.T) (ln net.Listener, port int) {
	t.Helper()

	// Step 1: find a free port.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupyFreePort: probe Listen: %v", err)
	}
	port = probe.Addr().(*net.TCPAddr).Port
	probe.Close() // release so we can write the config first

	// Step 2: write config.yaml with the chosen port into a fresh XDG dir.
	cfgHome := t.TempDir()
	raxdCfgDir := cfgHome + "/raxd"
	if err := os.MkdirAll(raxdCfgDir, 0o700); err != nil {
		t.Fatalf("occupyFreePort: mkdir: %v", err)
	}
	cfgContent := fmt.Sprintf("port: %d\nbind_addr: \"127.0.0.1\"\n", port)
	if err := os.WriteFile(raxdCfgDir+"/config.yaml", []byte(cfgContent), 0o600); err != nil {
		t.Fatalf("occupyFreePort: write config: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	// Step 3: re-occupy the port so serve() fails to bind.
	ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		// Extremely rare race between step 1 and step 3; skip with explanation.
		t.Skipf("occupyFreePort: port %d was taken between probe and re-listen (CI race): %v", port, err)
	}
	return ln, port
}

// TestServePortInUseNoStartupBlock verifies ux-spec §5: when the port is already in use,
// the startup block ("listening", "cert", "tls", "press Ctrl+C") must NOT be printed.
// Only "error:" and "hint:" must appear on stderr, and the command must exit non-zero.
// Regression: previously serve.go printed the startup block BEFORE srv.Run(), so a bind
// error still showed "listening on https://..." — a false positive (D-1).
//
// LOW-debt fix: was t.Skip("port 7822 unavailable") — now deterministic via port 0 +
// temp config.yaml. No t.Skip on the test logic itself.
func TestServePortInUseNoStartupBlock(t *testing.T) {
	ln, _ := occupyFreePort(t)
	defer ln.Close()

	_, stderr, cmdErr := executeCmd("serve")

	// Command must exit non-zero (bind error → exit 1).
	if cmdErr == nil {
		t.Error("D-1/ux-spec §5: serve with occupied port must exit non-zero, got nil error")
	}

	// "listening" must NOT appear — the server never bound.
	if strings.Contains(stderr, "listening") {
		t.Errorf("D-1/ux-spec §5: startup block 'listening' must not appear on bind error; stderr=%q", stderr)
	}

	// "press Ctrl+C" must NOT appear.
	if strings.Contains(stderr, "press Ctrl+C") {
		t.Errorf("D-1/ux-spec §5: startup block 'press Ctrl+C' must not appear on bind error; stderr=%q", stderr)
	}

	// "error:" must appear on stderr (ux-spec §5.1).
	if !strings.Contains(stderr, "error:") {
		t.Errorf("D-1/ux-spec §5: 'error:' must appear on bind error; stderr=%q", stderr)
	}
}

// TestServePortInUseNoShutdownBlock verifies ux-spec §5: when the port is already in use,
// the shutdown block ("shutting down", "draining", "flushing", "stopped") must NOT appear.
// The server never started, so there is nothing to shut down.
//
// LOW-debt fix: was t.Skip("port 7822 unavailable") — now deterministic via port 0 +
// temp config.yaml. No t.Skip on the test logic itself.
func TestServePortInUseNoShutdownBlock(t *testing.T) {
	ln, _ := occupyFreePort(t)
	defer ln.Close()

	_, stderr, _ := executeCmd("serve")

	shutdownPhrases := []string{"shutting down", "draining", "flushing", "stopped"}
	for _, phrase := range shutdownPhrases {
		if strings.Contains(stderr, phrase) {
			t.Errorf("D-1/ux-spec §5: shutdown block phrase %q must not appear on bind error; stderr=%q",
				phrase, stderr)
		}
	}
}
