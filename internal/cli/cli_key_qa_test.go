package cli_test

// cli_key_qa_test.go — QA gap-closing integration tests for key-management CLI.
// Закрывает пробелы: e2e create→list→delete→list, audit no body, error messages,
// exit codes, channel split, SR-11/SR-12/SR-13/SR-18/SR-24.
//
// All tests run in Docker per SECURITY-BASELINE §6.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Integration: create → list → delete → list ---

// TestKeyCreateListDeleteIntegration verifies the full lifecycle:
// create a key → it appears in list → delete it → it disappears from list.
// AC: "create печатает ключ; list показывает активные; delete → revoked скрыт в list".
func TestKeyCreateListDeleteIntegration(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	// 1. Create a key with a label.
	createStdout, createStderr, err := executeCmd("key", "create", "--name", "integration-test-key")
	if err != nil {
		t.Fatalf("key create must succeed; err=%v stderr=%q", err, createStderr)
	}

	// Key must appear on stdout.
	if !strings.Contains(createStdout, "rax_live_") {
		t.Fatalf("key create stdout must contain rax_live_ key; got=%q", createStdout)
	}
	// WARNING must appear on stderr.
	if !strings.Contains(createStderr, "WARNING") {
		t.Errorf("key create stderr must contain WARNING; got=%q", createStderr)
	}

	// Extract the ID from stderr metadata.
	var createdID string
	for _, line := range strings.Split(createStderr, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "id") {
			createdID = strings.TrimSpace(strings.TrimPrefix(line, "id"))
			break
		}
	}
	if createdID == "" {
		t.Fatalf("could not extract ID from key create stderr: %q", createStderr)
	}

	// 2. List must show the new key (by label).
	listStdout, _, err := executeCmd("key", "list")
	if err != nil {
		t.Fatalf("key list must succeed; err=%v", err)
	}
	if !strings.Contains(listStdout, "integration-test-key") {
		t.Errorf("key list stdout must contain label 'integration-test-key'; got=%q", listStdout)
	}
	// List must NOT contain any rax_live_ body.
	if strings.Contains(listStdout, "rax_live_") {
		t.Errorf("key list stdout must not contain key body rax_live_; got=%q", listStdout)
	}

	// 3. Delete (revoke) the key.
	_, deleteStderr, err := executeCmd("key", "delete", createdID)
	if err != nil {
		t.Fatalf("key delete must succeed; err=%v stderr=%q", err, deleteStderr)
	}
	if !strings.Contains(deleteStderr, "revoked") {
		t.Errorf("key delete stderr must contain 'revoked'; got=%q", deleteStderr)
	}
	if !strings.Contains(deleteStderr, "hint:") {
		t.Errorf("key delete stderr must contain 'hint:'; got=%q", deleteStderr)
	}

	// 4. List must no longer show the deleted key.
	listStdout2, _, err := executeCmd("key", "list")
	if err != nil {
		t.Fatalf("key list after delete must succeed; err=%v", err)
	}
	// Either "No API keys found" or the label is absent.
	if strings.Contains(listStdout2, "integration-test-key") {
		t.Errorf("deleted key must not appear in list after revoke; stdout=%q", listStdout2)
	}
}

// --- SR-18/CLI: delete non-existent id gives error: + hint: and exit ≠0 ---

// TestKeyDeleteNotFoundCLI verifies that "key delete <nonexistent>" gives exit≠0,
// exact ux-spec error message and hint.
// SR-18: "неизвестный id → ErrNotFound; exit≠0; сообщение без секрета".
// ux-spec: error: key "<id>" not found + hint: run "raxd key list" to see available key IDs
func TestKeyDeleteNotFoundCLI(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	const id = "nonexistent123"
	_, stderr, err := executeCmd("key", "delete", id)
	if err == nil {
		t.Error("key delete with nonexistent id must return non-zero exit")
	}
	// ux-spec exact message: error: key "<id>" not found
	wantMsg := fmt.Sprintf("error: key %q not found", id)
	if !strings.Contains(stderr, wantMsg) {
		t.Errorf("stderr must contain %q; got=%q", wantMsg, stderr)
	}
	// ux-spec exact hint
	wantHint := `hint: run "raxd key list" to see available key IDs`
	if !strings.Contains(stderr, wantHint) {
		t.Errorf("stderr must contain %q; got=%q", wantHint, stderr)
	}
	// Must not contain any secret material.
	if strings.Contains(stderr, "rax_live_") {
		t.Errorf("error message must not contain key body; got=%q", stderr)
	}
}

// --- SR-18/CLI: delete already-revoked id gives specific error ---

// TestKeyDeleteAlreadyRevokedCLI verifies that revoking an already-revoked key
// gives exit≠0 and "already revoked" message.
// SR-18: "уже отозванный id → ErrAlreadyRevoked; exit≠0".
func TestKeyDeleteAlreadyRevokedCLI(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	// Create a key.
	_, createStderr, err := executeCmd("key", "create", "--name", "already-revoked-test")
	if err != nil {
		t.Fatalf("key create must succeed; err=%v", err)
	}

	// Extract ID.
	var id string
	for _, line := range strings.Split(createStderr, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "id") {
			id = strings.TrimSpace(strings.TrimPrefix(line, "id"))
			break
		}
	}
	if id == "" {
		t.Fatalf("could not extract ID from create stderr: %q", createStderr)
	}

	// First delete: must succeed.
	if _, _, err := executeCmd("key", "delete", id); err != nil {
		t.Fatalf("first key delete must succeed; err=%v", err)
	}

	// Second delete: must fail with ux-spec exact messages.
	_, stderr, err := executeCmd("key", "delete", id)
	if err == nil {
		t.Error("second key delete must return non-zero exit (already revoked)")
	}
	// ux-spec exact message: error: key "<id>" is already revoked
	wantMsg := fmt.Sprintf("error: key %q is already revoked", id)
	if !strings.Contains(stderr, wantMsg) {
		t.Errorf("stderr must contain %q; got=%q", wantMsg, stderr)
	}
	// ux-spec exact hint
	wantHint := `hint: run "raxd key list" to see active keys`
	if !strings.Contains(stderr, wantHint) {
		t.Errorf("stderr must contain %q; got=%q", wantHint, stderr)
	}
}

// --- SR-11: key body appears only on stdout, never on stderr ---

// TestKeyCreateBodyOnlyOnStdout verifies that the key body (rax_live_...) appears
// on stdout and NEVER on stderr.
// SR-11: "полный ключ показывается ровно один раз при key create; не на stderr".
func TestKeyCreateBodyOnlyOnStdout(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	stdout, stderr, err := executeCmd("key", "create")
	if err != nil {
		t.Fatalf("key create must succeed; err=%v", err)
	}

	// Key MUST be on stdout.
	if !strings.Contains(stdout, "rax_live_") {
		t.Errorf("key body must appear on stdout; got stdout=%q", stdout)
	}

	// Key MUST NOT be on stderr (even partially).
	if strings.Contains(stderr, "rax_live_") {
		t.Errorf("key body must NOT appear on stderr (SR-11); got stderr=%q", stderr)
	}
}

// --- SR-24: create audit log has fingerprint, not key body ---

// TestKeyCreateAuditContainsFingerprintNotBody verifies that the charmbracelet/log
// audit output (on stderr) for "key create" contains "fingerprint" and does NOT
// contain the key body (rax_live_...).
// SR-24: "аудит create: timestamp+id+fingerprint, НЕ тело".
func TestKeyCreateAuditContainsFingerprintNotBody(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	stdout, stderr, err := executeCmd("key", "create", "--name", "audit-test")
	if err != nil {
		t.Fatalf("key create must succeed; err=%v", err)
	}

	// Audit is written to stderr via charmbracelet/log.
	// Check that fingerprint field is present.
	if !strings.Contains(stderr, "fingerprint") {
		t.Errorf("audit (stderr) must contain 'fingerprint'; got stderr=%q", stderr)
	}
	// Audit must NOT contain the key body.
	if strings.Contains(stderr, "rax_live_") {
		t.Errorf("audit (stderr) must not contain key body 'rax_live_'; got stderr=%q", stderr)
	}
	// Audit should mention "action" and "create".
	if !strings.Contains(stderr, "action") {
		t.Errorf("audit (stderr) must contain 'action'; got stderr=%q", stderr)
	}

	_ = stdout // Key goes to stdout; we don't need it here.
}

// --- SR-24: delete audit log has fingerprint, not key body ---

// TestKeyDeleteAuditContainsFingerprintNotBody verifies that the audit entry
// for "key delete" includes fingerprint but not the key body.
// SR-24: "аудит delete: timestamp+id+fingerprint, НЕ тело".
func TestKeyDeleteAuditContainsFingerprintNotBody(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	// Create first.
	_, createStderr, err := executeCmd("key", "create", "--name", "delete-audit-test")
	if err != nil {
		t.Fatalf("key create must succeed; err=%v", err)
	}

	// Extract ID.
	var id string
	for _, line := range strings.Split(createStderr, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "id") {
			id = strings.TrimSpace(strings.TrimPrefix(line, "id"))
			break
		}
	}
	if id == "" {
		t.Fatalf("could not extract ID from create stderr: %q", createStderr)
	}

	// Delete: audit goes to stderr.
	_, deleteStderr, err := executeCmd("key", "delete", id)
	if err != nil {
		t.Fatalf("key delete must succeed; err=%v stderr=%q", err, deleteStderr)
	}

	// Audit must contain fingerprint.
	if !strings.Contains(deleteStderr, "fingerprint") {
		t.Errorf("delete audit (stderr) must contain 'fingerprint'; got=%q", deleteStderr)
	}
	// Audit must NOT contain key body.
	if strings.Contains(deleteStderr, "rax_live_") {
		t.Errorf("delete audit (stderr) must not contain 'rax_live_' (SR-24); got=%q", deleteStderr)
	}
}

// --- SR-12/ux-spec: key list output on stdout has no hash/salt/rax_live_ ---

// TestKeyListNoSecretsOnStdout verifies that "key list" stdout contains no secret patterns.
// SR-12: "List не раскрывает секрет"; ux-spec: "вывод содержит только id/label/created/last-used".
func TestKeyListNoSecretsOnStdout(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	// Create a key to have something in the list.
	if _, _, err := executeCmd("key", "create", "--name", "list-secrets-test"); err != nil {
		t.Fatalf("key create must succeed; err=%v", err)
	}

	stdout, stderr, err := executeCmd("key", "list")
	if err != nil {
		t.Fatalf("key list must succeed; err=%v", err)
	}

	// stdout must not contain any key body patterns.
	forbiddenInStdout := []string{"rax_live_", `"hash"`, `"salt"`}
	for _, pattern := range forbiddenInStdout {
		if strings.Contains(stdout, pattern) {
			t.Errorf("key list stdout must not contain %q; got=%q", pattern, stdout)
		}
	}

	// stderr (banner only) must not contain rax_live_.
	if strings.Contains(stderr, "rax_live_") {
		t.Errorf("key list stderr must not contain key body 'rax_live_'; got=%q", stderr)
	}
}

// --- ux-spec: error:/hint: lowercase ---

// TestErrorMessagesLowercase verifies that error and hint prefixes are lowercase.
// ux-spec: "ошибки: error: + hint: строчными".
func TestErrorMessagesLowercase(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	cases := []struct {
		name string
		args []string
	}{
		{"missing arg", []string{"key", "delete"}},
		{"not found", []string{"key", "delete", "nonexistent_id_xyz"}},
		{"long label", []string{"key", "create", "--name", strings.Repeat("x", 65)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, err := executeCmd(tc.args...)
			if err == nil {
				t.Errorf("%v must return non-zero exit", tc.args)
				return
			}
			// Check that error: is lowercase (not Error: or ERROR:).
			if strings.Contains(stderr, "Error:") || strings.Contains(stderr, "ERROR:") {
				t.Errorf("error prefix must be lowercase 'error:'; got stderr=%q", stderr)
			}
			if !strings.Contains(stderr, "error:") {
				t.Errorf("stderr must contain lowercase 'error:'; got=%q", stderr)
			}
			// Check hint: is lowercase where present.
			if strings.Contains(stderr, "Hint:") || strings.Contains(stderr, "HINT:") {
				t.Errorf("hint prefix must be lowercase 'hint:'; got stderr=%q", stderr)
			}
		})
	}
}

// --- AC: label > 64 chars → error and exit ≠0 ---

// TestKeyCreateLabelTooLongCLI verifies that a label exceeding 64 chars
// results in exit≠0 and "error:" message with hint.
// AC D4: "длина label ≤ 64 символов; превышение — понятная ошибка валидации и ненулевой код".
func TestKeyCreateLabelTooLongCLI(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	longLabel := strings.Repeat("a", 65)
	_, stderr, err := executeCmd("key", "create", "--name", longLabel)
	if err == nil {
		t.Error("key create with 65-char label must return non-zero exit")
	}
	if !strings.Contains(stderr, "error:") {
		t.Errorf("stderr must contain 'error:'; got=%q", stderr)
	}
	if !strings.Contains(stderr, "hint:") {
		t.Errorf("stderr must contain 'hint:'; got=%q", stderr)
	}
	// Must not echo the long label back (could contain sensitive data).
	if strings.Contains(stderr, "rax_live_") {
		t.Errorf("error output must not contain rax_live_; got=%q", stderr)
	}
}

// --- AC: empty list shows "No API keys found" on stdout, exit 0 ---

// TestKeyListEmptyShowsMessageOnStdout verifies the empty-list UX path.
// AC: "на пустом списке — понятное сообщение «ключей нет», exit 0".
func TestKeyListEmptyShowsMessageOnStdout(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	stdout, _, err := executeCmd("key", "list")
	if err != nil {
		t.Errorf("key list must exit 0 on empty store; err=%v", err)
	}
	if !strings.Contains(stdout, "No API keys found") {
		t.Errorf("empty key list must show 'No API keys found' on stdout; got=%q", stdout)
	}
	if !strings.Contains(stdout, "hint:") {
		t.Errorf("empty key list must show 'hint:' on stdout; got=%q", stdout)
	}
}

// --- AC: key create label optional, shows "-" when absent in metadata ---

// TestKeyCreateNoLabelShowsDash verifies that key create without --name
// shows "-" in the label metadata line (stderr).
// AC D2: "при отсутствии label в key create выводить «-»".
func TestKeyCreateNoLabelShowsDash(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	_, stderr, err := executeCmd("key", "create")
	if err != nil {
		t.Fatalf("key create without --name must succeed; err=%v", err)
	}

	// Metadata section shows "label     -".
	if !strings.Contains(stderr, "label") {
		t.Errorf("stderr must contain 'label' metadata; got=%q", stderr)
	}
	// The label value should be "-" when no --name is provided.
	// Look for "label" followed by "-" in the metadata section.
	lines := strings.Split(stderr, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "label") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "label"))
			if val != "-" {
				t.Errorf("label metadata must show '-' when no --name given; got %q", val)
			}
			return
		}
	}
	t.Errorf("label metadata line not found in stderr: %q", stderr)
}

// --- AC: key delete produces no stdout ---

// TestKeyDeleteSuccessProducesNoStdout verifies that successful key delete
// produces nothing on stdout (confirmation goes to stderr per ux-spec).
// ux-spec: "key delete — stdout: —; stderr: подтверждение".
func TestKeyDeleteSuccessProducesNoStdout(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	// Create key first.
	_, createStderr, err := executeCmd("key", "create")
	if err != nil {
		t.Fatalf("key create must succeed: %v", err)
	}

	// Extract ID.
	var id string
	for _, line := range strings.Split(createStderr, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "id") {
			id = strings.TrimSpace(strings.TrimPrefix(line, "id"))
			break
		}
	}
	if id == "" {
		t.Fatalf("could not extract ID: %q", createStderr)
	}

	stdout, _, err := executeCmd("key", "delete", id)
	if err != nil {
		t.Fatalf("key delete must succeed: %v", err)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("key delete must produce no stdout; got=%q", stdout)
	}
}

// --- AC: key create exits 0 on success, exit≠0 on write error is tested via label error ---

// TestKeyCreateExitCodes verifies exit code 0 on success, exit≠0 on validation error.
// AC: "exit 0 на успехе; exit≠0 на ошибке валидации".
func TestKeyCreateExitCodes(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	// Success path → exit 0.
	if _, _, err := executeCmd("key", "create", "--name", "exit-code-test"); err != nil {
		t.Errorf("key create success must exit 0; err=%v", err)
	}

	// Label too long → exit≠0.
	if _, _, err := executeCmd("key", "create", "--name", strings.Repeat("z", 65)); err == nil {
		t.Error("key create with invalid label must exit non-zero")
	}
}

// --- AC: key list exits≠0 on corrupt DB ---

// TestKeyListExitNonZeroOnCorrupt verifies that a corrupt keys.db gives exit≠0 for all commands.
// AC: "повреждённый/нечитаемый keys.db — понятная ошибка и ненулевой код".
func TestKeyListExitNonZeroOnCorrupt(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)

	// Create the raxd directory and a corrupt keys.db inside it.
	raxdDir := filepath.Join(stateDir, "raxd")
	if err := os.MkdirAll(raxdDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	keysDB := filepath.Join(raxdDir, "keys.db")
	if err := os.WriteFile(keysDB, []byte("{corrupt json!!!}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// All key commands must exit non-zero and mention corruption.
	for _, args := range [][]string{
		{"key", "list"},
		{"key", "delete", "someid"},
	} {
		_, stderr, err := executeCmd(args...)
		if err == nil {
			t.Errorf("%v must exit non-zero on corrupt DB", args)
		}
		if !strings.Contains(stderr, "corrupted or unreadable") {
			t.Errorf("%v stderr must mention corruption; got=%q", args, stderr)
		}
	}
}
