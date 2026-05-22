// Package cli_test — unit-tests for "raxd service" command group.
// Note: whitebox tests that call exported helpers from export_test.go are in
// package cli (same package) and are placed in service_whitebox_test.go.
//
// Tests verify:
// - service subcommand is registered in root (AC1)
// - exit codes per ux-spec (ErrAlreadyInstalled→0, ErrNotInstalled@uninstall→0, ErrNotInstalled@start/stop→1)
// - error/hint text format (lowercase "error:", "hint:")
// - status output goes to stdout (AC13, ux-spec P-5)
// - --json flag outputs JSON to stdout
// - NO secrets in output (SR-95)
//
// Testing strategy: the service commands accept a cli.ServiceManagerFactory
// injectable via cli.SetServiceManagerFactory for tests. This avoids starting
// real systemd/launchd in unit tests.
package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/cli"
	"github.com/vladimirvkhs/raxd/internal/service"
)

// executeServiceCmd runs the CLI with a fake service manager injected.
func executeServiceCmd(fakeKind string, args ...string) (stdout, stderr string, err error) {
	root := cli.NewRootCmdWithServiceManager(newFakeManager(fakeKind))

	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	root.SetOut(outBuf)
	root.SetErr(errBuf)
	root.SetArgs(args)

	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// ─── fake ServiceManager ──────────────────────────────────────────────────────

type fakeManager struct {
	kind string
}

func newFakeManager(kind string) service.ServiceManager {
	return &fakeManager{kind: kind}
}

func (f *fakeManager) Install(_ context.Context) error {
	switch f.kind {
	case "already-installed":
		return service.ErrAlreadyInstalled
	case "permission-denied":
		return service.ErrPermission
	case "unavailable":
		return service.ErrManagerUnavailable
	case "unsupported":
		return service.ErrUnsupported
	default:
		return nil
	}
}

func (f *fakeManager) Uninstall(_ context.Context) error {
	if f.kind == "not-installed" {
		return service.ErrNotInstalled
	}
	return nil
}

func (f *fakeManager) Start(_ context.Context) error {
	if f.kind == "not-installed" {
		return service.ErrNotInstalled
	}
	return nil
}

func (f *fakeManager) Stop(_ context.Context) error {
	if f.kind == "not-installed" {
		return service.ErrNotInstalled
	}
	return nil
}

func (f *fakeManager) Status(_ context.Context) (service.Status, error) {
	switch f.kind {
	case "not-installed":
		return service.Status{Installed: false}, nil
	default:
		return service.Status{
			Installed: true,
			Active:    true,
			PID:       1234,
			EUID:      1001,
			State:     "active (running)",
		}, nil
	}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestServiceCommandRegistered verifies "service" and its 5 subcommands are in root.
// AC1: управление жизненным циклом сервиса доступно через CLI.
func TestServiceCommandRegistered(t *testing.T) {
	root := cli.NewRootCmd()

	var found bool
	var children []string
	for _, cmd := range root.Commands() {
		if cmd.Name() == "service" {
			found = true
			for _, child := range cmd.Commands() {
				children = append(children, child.Name())
			}
		}
	}

	if !found {
		t.Fatal("'service' command not registered in root")
	}

	wantSubs := []string{"install", "uninstall", "start", "stop", "status"}
	for _, want := range wantSubs {
		var ok bool
		for _, got := range children {
			if got == want {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("service subcommand %q not registered; got: %v", want, children)
		}
	}
}

// TestServiceInstall_AlreadyInstalled_Exit0 verifies that ErrAlreadyInstalled maps to exit 0.
// ux-spec: «ErrAlreadyInstalled → RunE возвращает nil (exit 0) + информ-блок без error:»
// AC9: идемпотентность установки.
func TestServiceInstall_AlreadyInstalled_Exit0(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServiceCmd("already-installed", "service", "install")
	if err != nil {
		t.Errorf("install with ErrAlreadyInstalled must exit 0, got error: %v", err)
	}

	// Output must contain "already installed" info block, not "error:".
	if !strings.Contains(stderr, "already installed") {
		t.Errorf("expected 'already installed' in stderr, got:\n%s", stderr)
	}
	if strings.Contains(stderr, "error:") {
		t.Errorf("ErrAlreadyInstalled must NOT produce 'error:' prefix, got:\n%s", stderr)
	}
}

// TestServiceUninstall_NotInstalled_Exit0 verifies ErrNotInstalled at uninstall → exit 0.
// ux-spec: «ErrNotInstalled@uninstall → RunE возвращает nil (exit 0) + информ-блок»
// AC10: идемпотентность удаления.
func TestServiceUninstall_NotInstalled_Exit0(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServiceCmd("not-installed", "service", "uninstall")
	if err != nil {
		t.Errorf("uninstall with ErrNotInstalled must exit 0, got error: %v", err)
	}

	if !strings.Contains(stderr, "not installed") {
		t.Errorf("expected 'not installed' in stderr, got:\n%s", stderr)
	}
	if strings.Contains(stderr, "error:") {
		t.Errorf("ErrNotInstalled@uninstall must NOT produce 'error:' prefix, got:\n%s", stderr)
	}
}

// TestServiceStart_NotInstalled_Exit1 verifies ErrNotInstalled at start → exit 1.
// ux-spec: «ErrNotInstalled@start → exit 1 error:»
func TestServiceStart_NotInstalled_Exit1(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServiceCmd("not-installed", "service", "start")
	if err == nil {
		t.Errorf("start with ErrNotInstalled must exit 1, got nil error")
	}

	if !strings.Contains(stderr, "error:") {
		t.Errorf("ErrNotInstalled@start must produce 'error:' prefix, got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "hint:") {
		t.Errorf("ErrNotInstalled@start must produce 'hint:' line, got:\n%s", stderr)
	}
}

// TestServiceStop_NotInstalled_Exit1 verifies ErrNotInstalled at stop → exit 1.
func TestServiceStop_NotInstalled_Exit1(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServiceCmd("not-installed", "service", "stop")
	if err == nil {
		t.Errorf("stop with ErrNotInstalled must exit 1, got nil error")
	}

	if !strings.Contains(stderr, "error:") {
		t.Errorf("ErrNotInstalled@stop must produce 'error:' prefix, got:\n%s", stderr)
	}
}

// TestServiceStatus_OutputOnStdout verifies that status output goes to stdout.
// ux-spec P-5: «raxd service status — человекочитаемый блок → stdout»
func TestServiceStatus_OutputOnStdout(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	stdout, stderr, err := executeServiceCmd("not-installed", "service", "status")
	if err != nil {
		t.Errorf("status must exit 0 even when not installed, got: %v", err)
	}

	// "installed" key-value must be on stdout, not stderr.
	if !strings.Contains(stdout, "installed") {
		t.Errorf("status output must contain 'installed' on stdout, got stdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

// TestServiceStatus_JSON_OnStdout verifies --json output goes to stdout.
// ux-spec: «raxd service status --json → stdout»
func TestServiceStatus_JSON_OnStdout(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	stdout, _, err := executeServiceCmd("not-installed", "service", "status", "--json")
	if err != nil {
		t.Errorf("status --json must exit 0, got: %v", err)
	}

	// Output must be valid JSON.
	var m map[string]interface{}
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &m); jsonErr != nil {
		t.Errorf("status --json must produce valid JSON on stdout, got:\n%s\nerr: %v", stdout, jsonErr)
	}

	// Must contain "installed" key.
	if _, ok := m["installed"]; !ok {
		t.Errorf("JSON must contain 'installed' field, got: %v", m)
	}
}

// TestServiceError_LowercaseFormat verifies error/hint format is lowercase (ux-spec).
// SR-95: «ошибки нейтральные, строчными error:/hint:»
func TestServiceError_LowercaseFormat(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServiceCmd("permission-denied", "service", "install")
	if err == nil {
		t.Error("expected error for permission denied, got nil")
	}

	// Must start with lowercase "error:" (not "Error:", "ERROR:", etc.)
	if !strings.Contains(stderr, "error:") {
		t.Errorf("error output must contain lowercase 'error:', got:\n%s", stderr)
	}
	if strings.Contains(stderr, "Error:") || strings.Contains(stderr, "ERROR:") {
		t.Errorf("error must be lowercase, got:\n%s", stderr)
	}
}

// TestServiceOutput_NoSecrets verifies no API keys or TLS markers in output.
// SR-95: «никаких секретов в выводе CLI».
func TestServiceOutput_NoSecrets(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	cases := []struct {
		kind string
		sub  string
	}{
		{"not-installed", "install"},
		{"not-installed", "uninstall"},
		{"not-installed", "start"},
		{"not-installed", "stop"},
	}

	for _, c := range cases {
		stdout, stderr, _ := executeServiceCmd(c.kind, "service", c.sub)
		combined := stdout + stderr

		forbidden := []string{
			"rax_live_", // API key prefix
			"-----BEGIN", // PEM marker
			"panic:",     // Go panic
		}
		for _, f := range forbidden {
			if strings.Contains(combined, f) {
				t.Errorf("service %s output contains forbidden string %q:\n%s", c.sub, f, combined)
			}
		}
	}
}

// TestServiceManagerUnavailable_Error verifies ErrManagerUnavailable → exit 1 + error:
// ux-spec: «Менеджер сервисов недоступен → error: service manager is not available»
func TestServiceManagerUnavailable_Error(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServiceCmd("unavailable", "service", "install")
	if err == nil {
		t.Error("expected error for unavailable manager, got nil")
	}

	if !strings.Contains(stderr, "error:") {
		t.Errorf("ErrManagerUnavailable must produce 'error:', got:\n%s", stderr)
	}
}

// TestServiceUnsupported_Error verifies ErrUnsupported → exit 1 + error:
func TestServiceUnsupported_Error(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServiceCmd("unsupported", "service", "install")
	if err == nil {
		t.Error("expected error for unsupported platform, got nil")
	}

	if !strings.Contains(stderr, "error:") {
		t.Errorf("ErrUnsupported must produce 'error:', got:\n%s", stderr)
	}
}

// TestServiceInstall_Success_HintPresent verifies successful install shows hint about start.
// ux-spec: «hint: start the service now with "raxd service start"»
func TestServiceInstall_Success_HintPresent(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServiceCmd("", "service", "install")
	if err != nil {
		t.Errorf("install must succeed, got: %v", err)
	}

	if !strings.Contains(stderr, "installed") {
		t.Errorf("install success must show 'installed', got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "hint:") {
		t.Errorf("install success must show 'hint:', got:\n%s", stderr)
	}
}

// TestServiceUninstall_Success_HintContainsPlatformStateDir verifies that the uninstall
// success hint shows the platform-correct state directory path (OQ-1 fix).
//
// On linux (Docker): StateDir = /var/lib/raxd.
// The test is not false-green: it asserts the concrete path that DefaultConfigForGOOS
// returns for the current GOOS, so it would fail if the path were wrong or still
// hardcoded as /var/lib/raxd on darwin.
func TestServiceUninstall_Success_HintContainsPlatformStateDir(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, stderr, err := executeServiceCmd("", "service", "uninstall")
	if err != nil {
		t.Errorf("uninstall success must exit 0, got: %v", err)
	}

	// The hint must contain the platform-specific StateDir, not a hardcoded Linux path.
	wantStateDir := service.DefaultConfigForGOOS(runtime.GOOS).StateDir
	if !strings.Contains(stderr, wantStateDir) {
		t.Errorf("uninstall hint must contain platform StateDir %q (OQ-1):\n%s", wantStateDir, stderr)
	}

	// Confirm the full hint line is present and well-formed.
	wantHint := "data in " + wantStateDir + " is preserved"
	if !strings.Contains(stderr, wantHint) {
		t.Errorf("uninstall hint line missing or malformed, want substring %q:\n%s", wantHint, stderr)
	}
}

// TestServiceStatus_Active_ContainsExpectedFields verifies status output when running.
// ux-spec: installed, running, pid, euid, user, port, autostart, unit, manager, state fields.
func TestServiceStatus_Active_ContainsExpectedFields(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	stdout, _, err := executeServiceCmd("", "service", "status")
	if err != nil {
		t.Errorf("status must exit 0, got: %v", err)
	}

	fields := []string{"installed", "running", "state"}
	for _, f := range fields {
		if !strings.Contains(stdout, f) {
			t.Errorf("status stdout missing field %q:\n%s", f, stdout)
		}
	}
}
