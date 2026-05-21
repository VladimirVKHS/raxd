package cli_test

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/cli"
	"github.com/vladimirvkhs/raxd/internal/version"
)

// init sets a stable version for all CLI tests so banner output is predictable.
func init() {
	version.Set("dev", "none", "unknown")
}

// executeCmd runs the CLI with the given args and returns stdout, stderr, and
// the returned error. It never calls os.Exit.
func executeCmd(args ...string) (stdout, stderr string, err error) {
	root := cli.NewRootCmd()

	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	root.SetOut(outBuf)
	root.SetErr(errBuf)
	root.SetArgs(args)

	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// --- AC: Registration of all sub-commands ---

func TestSubcommandsRegistered(t *testing.T) {
	root := cli.NewRootCmd()

	want := map[string]bool{
		"key":     false,
		"config":  false,
		"serve":   false,
		"version": false,
		"status":  false,
	}

	for _, cmd := range root.Commands() {
		want[cmd.Name()] = true
	}

	for name, found := range want {
		if !found {
			t.Errorf("command %q is not registered in root", name)
		}
	}
}

func TestKeySubcommandsRegistered(t *testing.T) {
	root := cli.NewRootCmd()

	var keyCmd interface{ Commands() []*interface{} }
	_ = keyCmd
	// Find key command.
	var keyFound bool
	var keyChildren []string
	for _, cmd := range root.Commands() {
		if cmd.Name() == "key" {
			keyFound = true
			for _, child := range cmd.Commands() {
				keyChildren = append(keyChildren, child.Name())
			}
		}
	}

	if !keyFound {
		t.Fatal("key command not registered")
	}

	want := []string{"create", "list", "delete"}
	for _, w := range want {
		found := false
		for _, got := range keyChildren {
			if got == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("key sub-command %q not registered; registered: %v", w, keyChildren)
		}
	}
}

func TestConfigPortSubcommandRegistered(t *testing.T) {
	root := cli.NewRootCmd()

	for _, cmd := range root.Commands() {
		if cmd.Name() == "config" {
			for _, child := range cmd.Commands() {
				if child.Name() == "port" {
					return // found
				}
			}
			t.Fatal("config port sub-command not registered")
		}
	}
	t.Fatal("config command not registered")
}

// --- AC: Stub commands exit non-zero ---

// TestKeyCreateExitZero verifies that "key create" exits 0 on success.
// After key-management implementation, key create no longer returns "not implemented yet".
func TestKeyCreateExitZero(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_, _, err := executeCmd("key", "create")
	if err != nil {
		t.Errorf("key create must exit 0 on success, got: %v", err)
	}
}

// TestKeyListExitZero verifies that "key list" exits 0 (empty list is not an error).
// After key-management implementation, key list outputs table/empty message on stdout.
func TestKeyListExitZero(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_, _, err := executeCmd("key", "list")
	if err != nil {
		t.Errorf("key list must exit 0 on success, got: %v", err)
	}
}

// TestKeyDeleteMissingArg verifies that "key delete" without an id returns non-zero exit.
func TestKeyDeleteMissingArg(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_, stderr, err := executeCmd("key", "delete")
	if err == nil {
		t.Error("key delete without id must return non-nil error")
	}
	if !strings.Contains(stderr, "error:") {
		t.Errorf("stderr %q must contain 'error:' prefix", stderr)
	}
}

func TestStubConfigPort(t *testing.T) {
	_, stderr, err := executeCmd("config", "port", "8080")
	if err == nil {
		t.Error("config port must return non-nil error")
	}
	if !strings.Contains(stderr, "not implemented yet") {
		t.Errorf("stderr = %q", stderr)
	}
}

// TestServeIsNoLongerStub verifies that "serve" is no longer an honest stub.
// It occupies port 7822 so serve fails fast with a bind error (no blocking).
// After tls-transport: serve starts the real TLS server; it does NOT print
// "not implemented yet".
func TestServeIsNoLongerStub(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// Occupy port 7822 so serve returns quickly with bind error.
	ln, err := net.Listen("tcp", "127.0.0.1:7822")
	if err != nil {
		// Port may already be in use — still good for the test.
		ln = nil
	}
	if ln != nil {
		defer ln.Close()
	}
	_, stderr, _ := executeCmd("serve")
	// serve returns non-nil error (port in use) — that is expected.
	// The important invariant: it no longer prints "not implemented yet".
	if strings.Contains(stderr, "not implemented yet") {
		t.Errorf("serve must no longer be a stub; stderr = %q", stderr)
	}
}

// --- AC: version exits 0 and prints correct info ---

func TestVersionExitZero(t *testing.T) {
	stdout, _, err := executeCmd("version")
	if err != nil {
		t.Errorf("version must return nil error (exit 0), got: %v", err)
	}
	if !strings.Contains(stdout, "raxd") {
		t.Errorf("version stdout %q must contain 'raxd'", stdout)
	}
}

func TestVersionFormat(t *testing.T) {
	version.Set("1.0.0", "abc1234", "2025-06-01")
	defer version.Set("dev", "none", "unknown")

	stdout, _, err := executeCmd("version")
	if err != nil {
		t.Fatalf("version error = %v", err)
	}

	want := "raxd 1.0.0 (commit abc1234, built 2025-06-01)"
	if !strings.Contains(stdout, want) {
		t.Errorf("version stdout = %q, must contain %q", stdout, want)
	}
}

// --- AC: status exits 0 and prints required fields ---

func TestStatusExitZero(t *testing.T) {
	_, _, err := executeCmd("status")
	if err != nil {
		t.Errorf("status must return nil error (exit 0), got: %v", err)
	}
}

func TestStatusOutputFields(t *testing.T) {
	stdout, _, err := executeCmd("status")
	if err != nil {
		t.Fatalf("status error = %v", err)
	}

	fields := []string{"state", "not running", "config", "keys", "tls"}
	for _, f := range fields {
		if !strings.Contains(stdout, f) {
			t.Errorf("status stdout missing field %q:\n%s", f, stdout)
		}
	}
}

func TestStatusNoSecrets(t *testing.T) {
	stdout, stderr, _ := executeCmd("status")
	combined := stdout + stderr

	secrets := []string{"rax_live_", "BEGIN PRIVATE KEY", "BEGIN RSA PRIVATE KEY"}
	for _, s := range secrets {
		if strings.Contains(combined, s) {
			t.Errorf("status output contains sensitive pattern %q", s)
		}
	}
}

// --- SECURITY: Non-key stubs must not produce stdout (machine-readable channel) ---

// TestNonKeyStubsProduceNoStdout verifies that remaining stubs (config port)
// do not write to stdout. Key commands are now implemented and have defined stdout behaviour.
// Note: "serve" is now a real server and is tested separately in TestServeIsNoLongerStub.
func TestNonKeyStubsProduceNoStdout(t *testing.T) {
	cases := [][]string{
		{"config", "port", "8080"},
		// "serve" removed: it is now a real server that blocks; tested separately.
	}
	for _, args := range cases {
		stdout, _, _ := executeCmd(args...)
		if strings.TrimSpace(stdout) != "" {
			t.Errorf("stub %v must not write to stdout, got: %q", args, stdout)
		}
	}
}

// TestKeyDeleteProducesNoStdout verifies that "key delete" does not write to stdout
// (confirmation goes to stderr per ux-spec).
func TestKeyDeleteProducesNoStdout(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// delete without args — must error but still produce no stdout.
	stdout, _, _ := executeCmd("key", "delete")
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("key delete must not write to stdout, got: %q", stdout)
	}
}

// TestKeyCreateKeyOnStdout verifies that "key create" outputs the key to stdout.
// ux-spec: key body in box frame on stdout; warning+metadata on stderr.
func TestKeyCreateKeyOnStdout(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	stdout, stderr, err := executeCmd("key", "create")
	if err != nil {
		t.Fatalf("key create must exit 0, got: %v", err)
	}
	// Key must appear on stdout.
	if !strings.Contains(stdout, "rax_") {
		t.Errorf("stdout must contain key body; got stdout=%q", stdout)
	}
	// Warning must appear on stderr.
	if !strings.Contains(stderr, "WARNING") {
		t.Errorf("stderr must contain WARNING; got stderr=%q", stderr)
	}
	// Key must NOT appear on stderr (SR-11).
	if strings.Contains(stderr, "rax_live_") {
		t.Errorf("key body must not appear on stderr (SR-11); got stderr=%q", stderr)
	}
}

// TestKeyListOutputOnStdout verifies that "key list" output goes to stdout.
// ux-spec: table is machine-readable, stdout channel.
func TestKeyListOutputOnStdout(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	stdout, _, err := executeCmd("key", "list")
	if err != nil {
		t.Fatalf("key list must exit 0, got: %v", err)
	}
	// Empty list must contain helpful message on stdout.
	if !strings.Contains(stdout, "No API keys found") {
		t.Errorf("empty key list must contain 'No API keys found' on stdout; got=%q", stdout)
	}
}

// --- ISSUE-3: wrapped ErrCorrupt produces specific error message in CLI ---

// TestCorruptDBGivesSpecificMessage verifies that a corrupted keys.db produces the
// canonical "key store is corrupted or unreadable" error message (not a generic one).
// ISSUE-3: CLI uses errors.Is(err, keystore.ErrCorrupt) to catch wrapped errors.
func TestCorruptDBGivesSpecificMessage(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)

	// Create the raxd subdirectory and a corrupt keys.db inside it.
	raxdDir := filepath.Join(stateDir, "raxd")
	if err := os.MkdirAll(raxdDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	keysDB := filepath.Join(raxdDir, "keys.db")
	if err := os.WriteFile(keysDB, []byte("{bad json!!!}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cases := [][]string{
		{"key", "create"},
		{"key", "list"},
		{"key", "delete", "someid"},
	}
	for _, args := range cases {
		_, stderr, err := executeCmd(args...)
		if err == nil {
			t.Errorf("%v: must return non-zero exit with corrupt db", args)
			continue
		}
		// Must produce the specific corruption message, not a generic Go error.
		if !strings.Contains(stderr, "corrupted or unreadable") {
			t.Errorf("%v: stderr must contain 'corrupted or unreadable'; got: %q", args, stderr)
		}
		// Must include a hint about file permissions.
		if !strings.Contains(stderr, "hint:") {
			t.Errorf("%v: stderr must contain 'hint:' with recovery guidance; got: %q", args, stderr)
		}
	}
}
