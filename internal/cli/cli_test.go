package cli_test

import (
	"bytes"
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

func TestStubKeyCreate(t *testing.T) {
	_, stderr, err := executeCmd("key", "create")
	if err == nil {
		t.Error("key create must return non-nil error (non-zero exit)")
	}
	if !strings.Contains(stderr, "not implemented yet") {
		t.Errorf("stderr %q must contain 'not implemented yet'", stderr)
	}
	if !strings.Contains(stderr, "error:") {
		t.Errorf("stderr %q must contain 'error:' prefix", stderr)
	}
}

func TestStubKeyList(t *testing.T) {
	_, stderr, err := executeCmd("key", "list")
	if err == nil {
		t.Error("key list must return non-nil error")
	}
	if !strings.Contains(stderr, "not implemented yet") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestStubKeyDelete(t *testing.T) {
	_, stderr, err := executeCmd("key", "delete", "someid")
	if err == nil {
		t.Error("key delete must return non-nil error")
	}
	if !strings.Contains(stderr, "not implemented yet") {
		t.Errorf("stderr = %q", stderr)
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

func TestStubServe(t *testing.T) {
	_, stderr, err := executeCmd("serve")
	if err == nil {
		t.Error("serve must return non-nil error (honest stub)")
	}
	if !strings.Contains(stderr, "not implemented yet") {
		t.Errorf("stderr = %q", stderr)
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

// --- SECURITY: Stubs must not produce stdout (machine-readable channel) ---

func TestStubsProduceNoStdout(t *testing.T) {
	cases := [][]string{
		{"key", "create"},
		{"key", "list"},
		{"key", "delete", "id"},
		{"config", "port", "8080"},
		{"serve"},
	}
	for _, args := range cases {
		stdout, _, _ := executeCmd(args...)
		if strings.TrimSpace(stdout) != "" {
			t.Errorf("stub %v must not write to stdout, got: %q", args, stdout)
		}
	}
}
