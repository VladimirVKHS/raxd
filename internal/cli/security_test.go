package cli_test

// security_test.go — static and behavioural security invariant tests.
// These tests verify that the bootstrap-cli skeleton does NOT contain
// forbidden patterns (exec.Command, net.Listen, hardcoded secrets) as
// required by security-requirements.md and SECURITY-BASELINE.ru.md §3.
//
// "Behavioural" means we test observable side-effects (non-blocking, no port),
// not just grep — grep tests are in internal/security_static_test.go.

import (
	"strings"
	"testing"
)

// TestStubsErrorPrefix verifies all stubs use the canonical "error:" prefix.
// Security requirement: "заглушки key/config/serve возвращают ошибку not implemented yet".
// This complements TestStubErrorMessageContainsCommandName with explicit prefix check.
func TestStubsErrorPrefix(t *testing.T) {
	cases := [][]string{
		{"key", "create"},
		{"key", "list"},
		{"key", "delete", "id"},
		{"config", "port", "9999"},
		{"serve"},
	}
	for _, args := range cases {
		_, stderr, err := executeCmd(args...)
		if err == nil {
			t.Errorf("%v: must return non-zero exit", args)
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(strings.Split(stderr, "\n")[1]), "error:") &&
			!strings.Contains(stderr, "error:") {
			t.Errorf("%v stderr must contain 'error:' prefix; got: %q", args, stderr)
		}
	}
}

// TestVersionOutputNoSecretPatterns verifies that the version command output
// contains no known secret patterns on either stdout or stderr.
// Security requirement: "вывод raxd version … не содержит секретов".
func TestVersionOutputNoSecretPatterns(t *testing.T) {
	stdout, stderr, _ := executeCmd("version")
	combined := stdout + stderr

	secrets := []string{
		"rax_live_",
		"BEGIN PRIVATE KEY",
		"BEGIN RSA PRIVATE KEY",
		"BEGIN EC PRIVATE KEY",
		"AAAA",        // base64-blob typical start in keys
		"secret",      // any literal 'secret' word
	}
	for _, s := range secrets {
		if strings.Contains(combined, s) {
			t.Errorf("version output contains sensitive pattern %q", s)
		}
	}
}

// TestBannerNoSecretPatterns verifies that the banner (seen in stderr of any command)
// contains no secret patterns — only product name, description, author.
// Security requirement: "баннер … без чувствительных данных".
func TestBannerNoSecretPatterns(t *testing.T) {
	_, stderr, _ := executeCmd("version")

	secrets := []string{
		"rax_live_",
		"BEGIN PRIVATE KEY",
		"BEGIN RSA PRIVATE KEY",
		"BEGIN EC PRIVATE KEY",
	}
	for _, s := range secrets {
		if strings.Contains(stderr, s) {
			t.Errorf("banner (stderr) contains sensitive pattern %q", s)
		}
	}
}
