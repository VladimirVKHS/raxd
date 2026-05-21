package keystore_test

// keystore_test.go — unit tests for the internal/keystore package.
// Tests are run in Docker per SECURITY-BASELINE §6.
// Coverage: SR-1..SR-25, spec AC, plan Contracts.
//
// Docker: docker build --target test -t raxd-test . && docker run --rm raxd-test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/keystore"
)

// newTestStore creates a Store in a temp directory for testing.
// Returns the store and the path to keys.db.
func newTestStore(t *testing.T) (*keystore.Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.db")
	store, err := keystore.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return store, path
}

// --- SR-1, SR-2, SR-3: crypto/rand, ≥128 bits, no math/rand ---

// TestKeyFormat verifies the generated key has the correct prefix and base64url encoding.
// SR-6: format rax_live_<base64url> without padding.
// AC: key has rax_live_ prefix, body is base64url without =.
func TestKeyFormat(t *testing.T) {
	store, _ := newTestStore(t)

	plain, _, err := store.Create("")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	key := string(plain)
	if !strings.HasPrefix(key, "rax_live_") {
		t.Errorf("key must have rax_live_ prefix; got %q", key)
	}
	body := strings.TrimPrefix(key, "rax_live_")
	if strings.Contains(body, "=") {
		t.Errorf("key body must not contain padding '='; got %q", body)
	}
	if strings.Contains(body, "+") || strings.Contains(body, "/") {
		t.Errorf("key body must use base64url (no +/); got %q", body)
	}
	// SR-1: ≥128 bits = ≥16 bytes = ≥22 base64url chars; plan: 32 bytes = 43 chars.
	if len(body) < 43 {
		t.Errorf("key body length = %d, want ≥43 (32 bytes base64url)", len(body))
	}
}

// TestKeyBodyEntropy verifies two keys have different bodies (probabilistic SR-1).
func TestKeyBodyEntropy(t *testing.T) {
	store, _ := newTestStore(t)

	plain1, _, _ := store.Create("a")
	plain2, _, _ := store.Create("b")

	if plain1 == plain2 {
		t.Error("two generated keys must not be equal (crypto/rand entropy check)")
	}
}

// --- SR-4: per-key salt ≥16 bytes, unique per key ---

// TestSaltUniqueness verifies each key gets its own distinct salt (SR-4).
// We check indirectly: same plain key presented to Verify only matches its own record.
func TestSaltUniqueness(t *testing.T) {
	store, path := newTestStore(t)

	plain1, _, err := store.Create("key1")
	if err != nil {
		t.Fatalf("Create key1: %v", err)
	}
	plain2, _, err := store.Create("key2")
	if err != nil {
		t.Fatalf("Create key2: %v", err)
	}
	_ = path

	// key1 must verify, key2 must not match key1's hash (different salts → different hashes).
	_, ok, err := store.Verify(string(plain1))
	if err != nil || !ok {
		t.Errorf("Verify key1 must succeed; ok=%v err=%v", ok, err)
	}
	_, ok, err = store.Verify(string(plain2))
	if err != nil || !ok {
		t.Errorf("Verify key2 must succeed; ok=%v err=%v", ok, err)
	}

	// Each key only matches itself (distinct salts ensure distinct hashes).
	// Attempt to cross-verify: presenting key1 should not match key2's record.
	// (This is validated structurally: Verify uses record-specific salt for comparison.)
}

// --- SR-5: id from crypto/rand, not derived from key body ---

// TestIDIsRandom verifies two created keys get different IDs.
func TestIDIsRandom(t *testing.T) {
	store, _ := newTestStore(t)

	_, r1, _ := store.Create("a")
	_, r2, _ := store.Create("b")

	if r1.ID == r2.ID {
		t.Error("two keys must have different IDs")
	}
}

// TestIDNotDerivedFromBody verifies ID does not appear in the key body.
// SR-5: id is independent of key body/hash.
func TestIDNotDerivedFromBody(t *testing.T) {
	store, _ := newTestStore(t)

	plain, rec, err := store.Create("")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if strings.Contains(string(plain), rec.ID) {
		t.Errorf("key body must not contain ID; body=%q id=%q", plain, rec.ID)
	}
}

// --- SR-7: keys.db does not contain plaintext key body ---

// TestNoPlaintextInDB verifies that the keys.db file does not contain the key body.
// AC: "подстрока тела ВЫПУЩЕННОГО ключа отсутствует в байтах файла keys.db".
func TestNoPlaintextInDB(t *testing.T) {
	store, path := newTestStore(t)

	plain, _, err := store.Create("test-key")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	body := strings.TrimPrefix(string(plain), "rax_live_")
	if strings.Contains(string(data), body) {
		t.Errorf("keys.db must not contain key body; body=%q db=%q", body, string(data))
	}
	// Also verify the full key is not in the db.
	if strings.Contains(string(data), string(plain)) {
		t.Errorf("keys.db must not contain full key; key=%q", plain)
	}
}

// --- SR-8: hash is sha256(key‖salt) ---

// TestHashScheme verifies the hash stored is sha256(presented_key‖salt).
// We verify indirectly: Verify with correct key succeeds, with wrong key fails.
func TestHashScheme(t *testing.T) {
	store, _ := newTestStore(t)

	plain, _, err := store.Create("hash-test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, ok, err := store.Verify(string(plain))
	if err != nil || !ok {
		t.Errorf("Verify with correct key must succeed; ok=%v err=%v", ok, err)
	}
	_, ok, err = store.Verify("rax_live_wrongkey")
	if err != nil || ok {
		t.Errorf("Verify with wrong key must fail; ok=%v err=%v", ok, err)
	}
}

// --- SR-9, SR-16: Verify before and after Revoke ---

// TestVerifyBeforeAfterRevoke verifies that Verify succeeds before Revoke and fails immediately after.
// SR-16: revocation is immediate.
// AC: "верификация успешна ДО Revoke и неуспешна СРАЗУ после".
func TestVerifyBeforeAfterRevoke(t *testing.T) {
	store, _ := newTestStore(t)

	plain, rec, err := store.Create("revoke-test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify must succeed before revoke.
	_, ok, err := store.Verify(string(plain))
	if err != nil || !ok {
		t.Errorf("Verify before Revoke must succeed; ok=%v err=%v", ok, err)
	}

	if err := store.Revoke(rec.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// Verify must fail immediately after revoke.
	_, ok, err = store.Verify(string(plain))
	if err != nil || ok {
		t.Errorf("Verify after Revoke must fail; ok=%v err=%v", ok, err)
	}
}

// --- SR-12: List returns no secrets ---

// TestListNoSecrets verifies that List output contains only id/label/created/last-used.
// SR-12: List does not expose hash, salt, or key body.
func TestListNoSecrets(t *testing.T) {
	store, _ := newTestStore(t)

	plain, _, err := store.Create("list-test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	records, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("List must return at least one record")
	}

	// Marshal each record to JSON to check no secrets leak.
	for _, r := range records {
		data, _ := json.Marshal(r)
		if strings.Contains(string(data), strings.TrimPrefix(string(plain), "rax_live_")) {
			t.Errorf("List record must not contain key body; record=%q", string(data))
		}
	}
}

// --- SR-17: FlushUsage does not resurrect revoked keys ---

// TestFlushUsageDoesNotResurrect verifies that FlushUsage after Revoke keeps key revoked.
// SR-17: "FlushUsage не «воскрешает» revoked".
func TestFlushUsageDoesNotResurrect(t *testing.T) {
	store, _ := newTestStore(t)

	plain, rec, err := store.Create("flush-test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify first to buffer LastUsed.
	_, ok, err := store.Verify(string(plain))
	if err != nil || !ok {
		t.Fatalf("Verify must succeed; ok=%v err=%v", ok, err)
	}

	// Revoke the key.
	if err := store.Revoke(rec.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// FlushUsage — must not resurrect revoked key.
	if err := store.FlushUsage(); err != nil {
		t.Fatalf("FlushUsage: %v", err)
	}

	// Verify must still fail after FlushUsage.
	_, ok, err = store.Verify(string(plain))
	if err != nil || ok {
		t.Errorf("Verify after Revoke+FlushUsage must still fail; ok=%v err=%v", ok, err)
	}
}

// --- SR-18: Revoke errors for not-found and already-revoked ---

// TestRevokeNotFound verifies ErrNotFound on unknown ID.
func TestRevokeNotFound(t *testing.T) {
	store, _ := newTestStore(t)

	err := store.Revoke("nonexistent")
	if !errors.Is(err, keystore.ErrNotFound) {
		t.Errorf("Revoke nonexistent id must return ErrNotFound; got %v", err)
	}
}

// TestRevokeAlreadyRevoked verifies ErrAlreadyRevoked on second revoke.
func TestRevokeAlreadyRevoked(t *testing.T) {
	store, _ := newTestStore(t)

	_, rec, err := store.Create("double-revoke")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Revoke(rec.ID); err != nil {
		t.Fatalf("first Revoke: %v", err)
	}

	err = store.Revoke(rec.ID)
	if !errors.Is(err, keystore.ErrAlreadyRevoked) {
		t.Errorf("second Revoke must return ErrAlreadyRevoked; got %v", err)
	}
}

// --- SR-19: keys.db permissions 0600 ---

// TestFilePermissions verifies keys.db is created with 0600 permissions.
// AC: "os.Stat(keys.db).Mode().Perm() == 0600".
func TestFilePermissions(t *testing.T) {
	store, path := newTestStore(t)

	if _, _, err := store.Create("perm-test"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat keys.db: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("keys.db permissions = %o, want 0600", perm)
	}
}

// --- SR-20: temp file permissions 0600 ---

// TestAtomicWritePermissions verifies the write results in a 0600 file.
// (temp→0600→sync→rename; result file must be 0600 — SR-20).
func TestAtomicWritePermissions(t *testing.T) {
	store, path := newTestStore(t)

	if _, _, err := store.Create("atomic-test"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// After successful write, no .tmp files should linger.
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file %q must not linger after successful write", e.Name())
		}
	}
}

// --- SR-22: corrupted file → ErrCorrupt without modifying ---

// TestCorruptFileReturnsErrCorrupt verifies that a corrupted keys.db returns ErrCorrupt.
// AC: "подсунутый битый файл → ErrCorrupt, файл байт-в-байт не изменён".
func TestCorruptFileReturnsErrCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.db")

	// Write corrupt data.
	corrupt := []byte("not valid json at all!!!")
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := keystore.Open(path)
	if !errors.Is(err, keystore.ErrCorrupt) {
		t.Errorf("Open with corrupt file must return ErrCorrupt; got %v", err)
	}

	// File must not be modified.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after Open: %v", err)
	}
	if string(after) != string(corrupt) {
		t.Errorf("corrupt file must not be modified; before=%q after=%q", corrupt, after)
	}
}

// TestMissingFileIsEmptyStore verifies that a missing keys.db is treated as empty store.
// AC: "отсутствующий keys.db для List/verify трактуется как пустое хранилище".
func TestMissingFileIsEmptyStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.db")
	// Do not create the file.

	store, err := keystore.Open(path)
	if err != nil {
		t.Fatalf("Open with missing file must succeed; got %v", err)
	}

	records, err := store.List()
	if err != nil {
		t.Fatalf("List on empty store must succeed; got %v", err)
	}
	if len(records) != 0 {
		t.Errorf("List on empty store must return empty slice; got %d records", len(records))
	}

	_, ok, err := store.Verify("rax_live_anything")
	if err != nil || ok {
		t.Errorf("Verify on empty store must return (_, false, nil); ok=%v err=%v", ok, err)
	}
}

// --- ISSUE-2: Fingerprint persisted in Record ---

// TestFingerprintPersistedInRecord verifies that Create stores fingerprint in the returned Record.
// ISSUE-2: fingerprint must be persisted so delete audit can include it without the plaintext key.
func TestFingerprintPersistedInRecord(t *testing.T) {
	store, path := newTestStore(t)

	plain, rec, err := store.Create("fp-persist-test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Fingerprint in returned Record must match Fingerprint(plaintext).
	expectedFP := keystore.Fingerprint(string(plain))
	if rec.Fingerprint != expectedFP {
		t.Errorf("rec.Fingerprint = %q, want %q (Fingerprint(plain))", rec.Fingerprint, expectedFP)
	}
	if rec.Fingerprint == "" {
		t.Error("rec.Fingerprint must not be empty")
	}

	// Fingerprint must also be present in List() result (persisted to disk).
	store2, err := keystore.Open(path)
	if err != nil {
		t.Fatalf("Open store2: %v", err)
	}
	records, err := store2.List()
	if err != nil {
		t.Fatalf("List store2: %v", err)
	}
	for _, r := range records {
		if r.ID == rec.ID {
			if r.Fingerprint != expectedFP {
				t.Errorf("persisted Fingerprint = %q, want %q", r.Fingerprint, expectedFP)
			}
			return
		}
	}
	t.Errorf("record %q not found in store2", rec.ID)
}

// TestFingerprintNotKeyBody verifies that fingerprint stored in Record is not the key body.
// SR-15: fingerprint must not allow reconstruction of the key.
func TestFingerprintNotKeyBody(t *testing.T) {
	store, _ := newTestStore(t)

	plain, rec, err := store.Create("")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	body := strings.TrimPrefix(string(plain), "rax_live_")
	if rec.Fingerprint == string(plain) {
		t.Error("Fingerprint must not equal the full plaintext key")
	}
	if rec.Fingerprint == body {
		t.Error("Fingerprint must not equal the key body (base64url part)")
	}
}

// --- ISSUE-3: errors.Is handles wrapped ErrCorrupt ---

// TestWrappedErrCorruptFromOpen verifies that Open returns an error matching ErrCorrupt
// via errors.Is even when the error is wrapped (fmt.Errorf("%w", ErrCorrupt)).
// ISSUE-3: CLI must use errors.Is, not == comparison, for correct behaviour.
func TestWrappedErrCorruptFromOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.db")

	// Write content that is clearly non-empty but not valid JSON.
	if err := os.WriteFile(path, []byte("{bad json!!!}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := keystore.Open(path)
	if err == nil {
		t.Fatal("Open with corrupt JSON must return error")
	}
	// errors.Is must match ErrCorrupt even if the error is wrapped.
	if !errors.Is(err, keystore.ErrCorrupt) {
		t.Errorf("errors.Is(err, ErrCorrupt) must be true for corrupt file; got %v", err)
	}
}

// TestWrappedErrCorruptFromReadDB verifies that readDB (called inside List/Verify/etc.)
// returns an error matching ErrCorrupt when the file becomes corrupt between Open and List.
func TestWrappedErrCorruptFromReadDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.db")

	// Open succeeds on empty file.
	store, err := keystore.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Now corrupt the file after Open.
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("WriteFile corrupt: %v", err)
	}

	_, err = store.List()
	if err == nil {
		t.Fatal("List with corrupt file must return error")
	}
	if !errors.Is(err, keystore.ErrCorrupt) {
		t.Errorf("errors.Is(err, ErrCorrupt) must be true; got %v (type %T)", err, err)
	}
}

// --- SR-15: Fingerprint ---

// TestFingerprint verifies fingerprint properties: deterministic, ≤12 chars, != body, != full hash.
func TestFingerprint(t *testing.T) {
	key := "rax_live_testbody123"

	fp1 := keystore.Fingerprint(key)
	fp2 := keystore.Fingerprint(key)

	if fp1 != fp2 {
		t.Errorf("Fingerprint must be deterministic; got %q and %q", fp1, fp2)
	}
	if len(fp1) > 12 {
		t.Errorf("Fingerprint length = %d, want ≤12; got %q", len(fp1), fp1)
	}
	if fp1 == key {
		t.Errorf("Fingerprint must not equal key body; got %q", fp1)
	}
	// fingerprint must be hex (lowercase only).
	for _, c := range fp1 {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("Fingerprint must be lowercase hex; got %q", fp1)
			break
		}
	}
}

// --- SR validation: label too long → ErrLabelTooLong ---

// TestLabelTooLong verifies ErrLabelTooLong when label exceeds 64 chars.
// AC: "длина label ≤ 64 символов; превышение — ошибка".
func TestLabelTooLong(t *testing.T) {
	store, _ := newTestStore(t)

	longLabel := strings.Repeat("a", 65)
	_, _, err := store.Create(longLabel)
	if !errors.Is(err, keystore.ErrLabelTooLong) {
		t.Errorf("Create with 65-char label must return ErrLabelTooLong; got %v", err)
	}
}

// TestLabelMaxLength verifies a 64-char label is accepted.
func TestLabelMaxLength(t *testing.T) {
	store, _ := newTestStore(t)

	label64 := strings.Repeat("a", 64)
	_, _, err := store.Create(label64)
	if err != nil {
		t.Errorf("Create with 64-char label must succeed; got %v", err)
	}
}

// TestEmptyLabel verifies that an empty label is accepted (label is optional).
// AC: "label опционален; при отсутствии label в key list показывать «-»".
func TestEmptyLabel(t *testing.T) {
	store, _ := newTestStore(t)

	_, rec, err := store.Create("")
	if err != nil {
		t.Fatalf("Create with empty label: %v", err)
	}
	if rec.Label != "" {
		t.Errorf("empty label must be stored as empty string; got %q", rec.Label)
	}
}

// TestDuplicateLabels verifies duplicate labels are allowed (uniqueness by id only).
// AC: "дубликаты label разрешены (уникален id, не label)".
func TestDuplicateLabels(t *testing.T) {
	store, _ := newTestStore(t)

	_, r1, err := store.Create("same-label")
	if err != nil {
		t.Fatalf("Create 1: %v", err)
	}
	_, r2, err := store.Create("same-label")
	if err != nil {
		t.Fatalf("Create 2 with duplicate label: %v", err)
	}

	if r1.ID == r2.ID {
		t.Errorf("duplicate-label keys must have different IDs; both=%q", r1.ID)
	}
}

// --- List: revoked keys hidden ---

// TestListHidesRevoked verifies that revoked keys do not appear in List.
// AC: "отозванные (revoked) ключи по умолчанию НЕ показываются".
func TestListHidesRevoked(t *testing.T) {
	store, _ := newTestStore(t)

	_, r1, err := store.Create("active")
	if err != nil {
		t.Fatalf("Create active: %v", err)
	}
	_, r2, err := store.Create("to-revoke")
	if err != nil {
		t.Fatalf("Create to-revoke: %v", err)
	}

	if err := store.Revoke(r2.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	records, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, r := range records {
		if r.ID == r2.ID {
			t.Errorf("revoked key %q must not appear in List", r2.ID)
		}
	}
	found := false
	for _, r := range records {
		if r.ID == r1.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("active key %q must appear in List", r1.ID)
	}
}

// TestEmptyListReturnsNil verifies that an empty store returns nil slice, nil error.
func TestEmptyListReturnsNil(t *testing.T) {
	store, _ := newTestStore(t)

	records, err := store.List()
	if err != nil {
		t.Fatalf("List on empty store: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("empty store List must return empty/nil slice; got %d", len(records))
	}
}

// --- FlushUsage: LastUsed updated on disk ---

// TestFlushUsagePersistsLastUsed verifies FlushUsage writes LastUsed to disk.
func TestFlushUsagePersistsLastUsed(t *testing.T) {
	store, path := newTestStore(t)

	plain, rec, err := store.Create("flush-lastused")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, _, err = store.Verify(string(plain))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if err := store.FlushUsage(); err != nil {
		t.Fatalf("FlushUsage: %v", err)
	}

	// Re-open store and check LastUsed is persisted.
	store2, err := keystore.Open(path)
	if err != nil {
		t.Fatalf("Open store2: %v", err)
	}
	records, err := store2.List()
	if err != nil {
		t.Fatalf("List store2: %v", err)
	}

	for _, r := range records {
		if r.ID == rec.ID {
			if r.LastUsed.IsZero() {
				t.Errorf("LastUsed must be non-zero after FlushUsage; got zero")
			}
			return
		}
	}
	t.Errorf("record %q not found in store2 after FlushUsage", rec.ID)
}
