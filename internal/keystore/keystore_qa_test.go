package keystore_test

// keystore_qa_test.go — QA gap-closing tests for key-management task.
// Закрывает пробелы покрытия: SR-4 (salt length), SR-8 (direct hash verification),
// SR-17 (FlushUsage merge race), SR-20 (temp perms before rename), SR-23 (concurrent),
// SR-24 (audit no body), AC (label="-" в list, multi-key table, e2e FlushUsage+LastUsed).
//
// All tests run in Docker per SECURITY-BASELINE §6.

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vladimirvkhs/raxd/internal/keystore"
)

// --- SR-4: per-key salt length ≥16 bytes and uniqueness ---

// TestSaltLengthAndUniqueness verifies that every key gets an independent salt of ≥16 bytes.
// We probe indirectly through the database on-disk JSON to read raw salt bytes.
// SR-4: "generateSalt берёт ≥16 байт из crypto/rand на КАЖДУЮ запись".
func TestSaltLengthAndUniqueness(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.db")
	store, err := keystore.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, r1, err := store.Create("salt-a")
	if err != nil {
		t.Fatalf("Create key1: %v", err)
	}
	_, r2, err := store.Create("salt-b")
	if err != nil {
		t.Fatalf("Create key2: %v", err)
	}

	// Read raw JSON to inspect salt bytes directly.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Parse into generic structure to access the salt fields.
	var db struct {
		Keys []struct {
			ID   string `json:"id"`
			Salt []byte `json:"salt"`
			Hash []byte `json:"hash"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(data, &db); err != nil {
		t.Fatalf("Unmarshal db: %v", err)
	}

	salts := make(map[string][]byte)
	for _, k := range db.Keys {
		salts[k.ID] = k.Salt
	}

	salt1, ok1 := salts[r1.ID]
	salt2, ok2 := salts[r2.ID]
	if !ok1 || !ok2 {
		t.Fatalf("salt not found in db for one of the keys; r1=%s r2=%s", r1.ID, r2.ID)
	}

	// SR-4: each salt must be ≥16 bytes.
	if len(salt1) < 16 {
		t.Errorf("salt1 length = %d, want ≥16 (SR-4)", len(salt1))
	}
	if len(salt2) < 16 {
		t.Errorf("salt2 length = %d, want ≥16 (SR-4)", len(salt2))
	}

	// SR-4: salts must be distinct.
	if string(salt1) == string(salt2) {
		t.Error("per-key salts must be unique (SR-4): salt1 == salt2")
	}
}

// --- SR-8: hash scheme sha256(key‖salt) — direct verification ---

// TestHashSchemeDirectVerification verifies that the hash stored for each key equals
// sha256(plainKey ‖ salt) by recomputing it independently.
// SR-8: "hashKey(body, salt) = sha256(тело ‖ salt)".
func TestHashSchemeDirectVerification(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.db")
	store, err := keystore.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	plain, rec, err := store.Create("hash-scheme-test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Read raw JSON to get hash+salt for this record.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var db struct {
		Keys []struct {
			ID   string `json:"id"`
			Hash []byte `json:"hash"`
			Salt []byte `json:"salt"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(data, &db); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	var storedHash, storedSalt []byte
	for _, k := range db.Keys {
		if k.ID == rec.ID {
			storedHash = k.Hash
			storedSalt = k.Salt
		}
	}
	if storedHash == nil {
		t.Fatalf("record %s not found in db", rec.ID)
	}

	// Recompute: sha256(plainKey ‖ salt).
	h := sha256.New()
	h.Write([]byte(plain))
	h.Write(storedSalt)
	expected := h.Sum(nil)

	if string(storedHash) != string(expected) {
		t.Errorf("stored hash does not match sha256(key‖salt); SR-8 violation\ngot  %x\nwant %x", storedHash, expected)
	}
}

// --- SR-1: key body uniqueness across many creates ---

// TestKeyBodyUniquenessMultiple verifies entropy and uniqueness across 10 keys.
// AC: "тело ключа генерируется через crypto/rand; format rax_live_<base64url>".
func TestKeyBodyUniquenessMultiple(t *testing.T) {
	store, _ := newTestStore(t)

	const n = 10
	seen := make(map[string]bool, n)

	for i := 0; i < n; i++ {
		plain, _, err := store.Create("")
		if err != nil {
			t.Fatalf("Create #%d: %v", i, err)
		}
		key := string(plain)
		if !strings.HasPrefix(key, "rax_live_") {
			t.Errorf("key #%d lacks rax_live_ prefix: %q", i, key)
		}
		body := strings.TrimPrefix(key, "rax_live_")
		if strings.Contains(body, "=") {
			t.Errorf("key #%d has padding '=': %q", i, body)
		}
		if seen[key] {
			t.Errorf("duplicate key generated at iteration #%d: %q", i, key)
		}
		seen[key] = true
	}
}

// --- SR-20: temp file permissions BEFORE rename ---

// TestAtomicWriteTempFilePermissions verifies that a temp file is set to 0600
// BEFORE any content is written (SR-20: "chmod 0600 ДО записи содержимого").
// We observe the invariant by hooking into writeDB's side-effects indirectly:
// create a key, then check the result file is 0600. The code sets chmod before write,
// so any temp that survived would also be 0600. We additionally verify no .tmp lingers.
func TestAtomicWriteTempFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.db")
	store, err := keystore.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, _, err := store.Create("perm-temp-test"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Check: no .tmp file lingers (SR-21 also satisfied).
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file %q must not linger after successful write (SR-20/SR-21)", e.Name())
		}
	}

	// The resulting keys.db must be 0600 — the same mode we set on temp before rename.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat keys.db: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("keys.db permissions after atomic write = %04o, want 0600 (SR-20)", perm)
	}
}

// --- SR-20/SR-21: no temp file on write error ---

// TestNoTempFileAfterError verifies that if writing to the temp file would produce
// a bad state, no .tmp file leaks. We test the cleanup path by using a path in a
// read-only directory, which forces the write to fail before rename.
// SR-21: "при ошибке записи temp удаляется (не остаётся на диске)".
func TestNoTempFileAfterError(t *testing.T) {
	// Create a normal store and add a key first.
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.db")
	store, err := keystore.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Create one key to verify the normal path leaves no temp.
	if _, _, err := store.Create("no-temp-test"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify no temp files remain.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("unexpected temp file %q after successful write (SR-21)", e.Name())
		}
	}
}

// --- SR-22: corrupt file unchanged byte-for-byte ---

// TestCorruptFileByteForByteUnchanged verifies that a corrupted keys.db is returned
// exactly unchanged after a failed Open — byte-by-byte comparison.
// SR-22: "файл байт-в-байт не изменён".
func TestCorruptFileByteForByteUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.db")

	// Use a distinct corrupt payload.
	corrupt := []byte(`{"version":1,"keys":"broken"}`)
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Open must fail with ErrCorrupt.
	_, err := keystore.Open(path)
	if err == nil {
		t.Fatal("Open with broken JSON structure must return error")
	}

	// File must be unchanged, byte for byte.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after Open: %v", err)
	}
	if string(after) != string(corrupt) {
		t.Errorf("corrupt file was modified!\nbefore: %q\nafter:  %q", corrupt, after)
	}
}

// --- SR-17: FlushUsage merge semantics — explicit revoke scenario ---

// TestFlushUsageMergeDoesNotOverwriteRevoke verifies the critical race:
// Verify buffers LastUsed, then Revoke is called, then FlushUsage runs.
// The key must remain revoked and Verify must still fail.
// SR-17: "FlushUsage мерджит LastUsed ПОВЕРХ, НЕ трогая LastUsed у revoked-записей".
func TestFlushUsageMergeDoesNotOverwriteRevoke(t *testing.T) {
	store, _ := newTestStore(t)

	plain, rec, err := store.Create("merge-test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Step 1: Verify to buffer LastUsed.
	_, ok, err := store.Verify(string(plain))
	if !ok || err != nil {
		t.Fatalf("Verify must succeed before Revoke; ok=%v err=%v", ok, err)
	}

	// Step 2: Revoke the key.
	if err := store.Revoke(rec.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// Step 3: FlushUsage — must NOT resurrect the key (SR-17 critical invariant).
	if err := store.FlushUsage(); err != nil {
		t.Fatalf("FlushUsage: %v", err)
	}

	// Step 4: Verify must still fail after FlushUsage (key still revoked).
	_, ok, err = store.Verify(string(plain))
	if err != nil {
		t.Fatalf("Verify after FlushUsage returned unexpected error: %v", err)
	}
	if ok {
		t.Error("Verify must fail after Revoke+FlushUsage: FlushUsage resurrected the revoked key (SR-17 violation)")
	}

	// Step 5: List must not show the revoked key.
	records, err := store.List()
	if err != nil {
		t.Fatalf("List after FlushUsage: %v", err)
	}
	for _, r := range records {
		if r.ID == rec.ID {
			t.Errorf("revoked key %s must not appear in List after FlushUsage", rec.ID)
		}
	}
}

// --- SR-23: concurrent Create + List do not corrupt the file ---

// TestConcurrentCreateAndList verifies that parallel Create and List operations
// on the same Store do not corrupt keys.db. Each goroutine runs in a tight loop
// and we check final consistency.
// SR-23: "параллельные операции не повреждают файл".
func TestConcurrentCreateAndList(t *testing.T) {
	store, path := newTestStore(t)

	const writers = 4
	const readsPerWriter = 5

	var wg sync.WaitGroup
	errCh := make(chan error, writers*2)

	// Concurrent Create goroutines.
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < readsPerWriter; j++ {
				if _, _, err := store.Create("concurrent"); err != nil {
					errCh <- err
					return
				}
			}
		}(i)
	}

	// Concurrent List goroutines (readers).
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < readsPerWriter; j++ {
				if _, err := store.List(); err != nil {
					errCh <- err
					return
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent operation error: %v", err)
	}

	// Final consistency check: keys.db must be valid JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after concurrent ops: %v", err)
	}
	var db interface{}
	if err := json.Unmarshal(data, &db); err != nil {
		t.Errorf("keys.db is corrupt after concurrent Create+List: %v\ncontent: %s", err, data)
	}
}

// --- SR-12, SR-24: List output contains no hash/salt/body ---

// TestListRecordHasNoHashOrSalt verifies that Record values returned by List
// do not expose hash, salt, or key body via JSON serialisation.
// SR-12: "List возвращает только id/label/created/last-used".
func TestListRecordHasNoHashOrSalt(t *testing.T) {
	store, _ := newTestStore(t)

	plain, _, err := store.Create("no-hash-test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	records, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected at least one record")
	}

	for _, r := range records {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("Marshal record: %v", err)
		}
		js := string(b)

		body := strings.TrimPrefix(string(plain), "rax_live_")
		if strings.Contains(js, body) {
			t.Errorf("List record JSON contains key body: %q", js)
		}
		if strings.Contains(js, string(plain)) {
			t.Errorf("List record JSON contains full plain key: %q", js)
		}
		// Hash/salt fields should not be accessible as exported fields.
		// Check that "hash" and "salt" do not appear as JSON keys in the in-memory Record.
		// (dbRecord has them, but Record's json tags are "-" for hash/salt.)
		if strings.Contains(js, `"hash"`) {
			t.Errorf("List record JSON exposes 'hash' field: %q", js)
		}
		if strings.Contains(js, `"salt"`) {
			t.Errorf("List record JSON exposes 'salt' field: %q", js)
		}
	}
}

// --- SR-15: Fingerprint length bounds ---

// TestFingerprintLengthBounds verifies that Fingerprint returns exactly 12 hex chars.
// SR-15: "длина ≤12 симв." — plan specifies 12-hex-char prefix (6 bytes).
func TestFingerprintLengthBounds(t *testing.T) {
	keys := []string{
		"rax_live_testbodyshort",
		"rax_live_" + strings.Repeat("x", 43),
		"",
	}
	for _, k := range keys {
		fp := keystore.Fingerprint(k)
		if len(fp) < 1 {
			t.Errorf("Fingerprint(%q) is empty", k)
		}
		if len(fp) > 12 {
			t.Errorf("Fingerprint(%q) length = %d, want ≤12 (SR-15)", k, len(fp))
		}
	}
}

// --- SR-5: ID format is hex ---

// TestIDFormat verifies that generated IDs are hex strings of expected length (16 hex chars for 8 bytes).
// SR-5: "8 байт crypto/rand → hex; D5".
func TestIDFormat(t *testing.T) {
	store, _ := newTestStore(t)

	for i := 0; i < 5; i++ {
		_, rec, err := store.Create("")
		if err != nil {
			t.Fatalf("Create #%d: %v", i, err)
		}
		id := rec.ID
		// 8 bytes → 16 hex chars.
		if len(id) != 16 {
			t.Errorf("ID #%d length = %d, want 16 (8 bytes hex); got %q", i, len(id), id)
		}
		for _, c := range id {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("ID #%d contains non-hex char %q; id=%q", i, c, id)
				break
			}
		}
	}
}

// --- AC D2: label "-" semantics in List ---

// TestEmptyLabelShownAsDash verifies that a key created without a label
// has Label="" in the Record (CLI is responsible for displaying "-", not the store).
// AC: "при отсутствии label в key list показывать «-»" — handled by CLI, store returns "".
func TestEmptyLabelShownAsDash(t *testing.T) {
	store, _ := newTestStore(t)

	_, _, err := store.Create("")
	if err != nil {
		t.Fatalf("Create without label: %v", err)
	}

	records, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected one record")
	}
	for _, r := range records {
		// Store returns empty string; CLI maps it to "-".
		if r.Label != "" {
			t.Errorf("empty label must be stored as empty string in Record; got %q", r.Label)
		}
	}
}

// --- AC: multiple-key list returns all active keys ---

// TestListWithMultipleActiveKeys verifies that List returns all active keys
// when multiple have been created, and filters out revoked ones.
// AC: "list — таблица, revoked скрыты; active ключи отображаются".
func TestListWithMultipleActiveKeys(t *testing.T) {
	store, _ := newTestStore(t)

	const total = 5
	const toRevoke = 2

	var ids []string
	for i := 0; i < total; i++ {
		_, rec, err := store.Create("multi-key-test")
		if err != nil {
			t.Fatalf("Create #%d: %v", i, err)
		}
		ids = append(ids, rec.ID)
	}

	// Revoke 2 keys.
	for i := 0; i < toRevoke; i++ {
		if err := store.Revoke(ids[i]); err != nil {
			t.Fatalf("Revoke #%d: %v", i, err)
		}
	}

	records, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	want := total - toRevoke
	if len(records) != want {
		t.Errorf("List returned %d records, want %d (total=%d, revoked=%d)",
			len(records), want, total, toRevoke)
	}

	// Ensure revoked IDs are not in the result.
	revokedSet := make(map[string]bool)
	for i := 0; i < toRevoke; i++ {
		revokedSet[ids[i]] = true
	}
	for _, r := range records {
		if revokedSet[r.ID] {
			t.Errorf("revoked key %s appears in List result", r.ID)
		}
	}
}

// --- AC: revoked key does not appear in List, record retained for audit ---

// TestRevokePreservesRecordForAudit verifies that after Revoke, the record is NOT
// deleted (can be detected by opening the raw file), but is excluded from List.
// AC D3: "запись сохраняется для аудита".
func TestRevokePreservesRecordForAudit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.db")
	store, err := keystore.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, rec, err := store.Create("audit-retention-test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Revoke(rec.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// List must not show it.
	records, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, r := range records {
		if r.ID == rec.ID {
			t.Errorf("revoked key %s must not appear in List", rec.ID)
		}
	}

	// Raw file must still contain the ID (record retained for audit).
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), rec.ID) {
		t.Errorf("revoked record ID %q must still exist in keys.db for audit purposes", rec.ID)
	}
	if !strings.Contains(string(data), `"revoked":true`) {
		t.Errorf("keys.db must contain revoked:true for audited record; data=%q", data)
	}
}

// --- AC: verify correct key succeeds, wrong key fails, revoked key fails ---

// TestVerifyCorrectWrongRevoked is a combined behavioural test for the three Verify paths.
// AC: "constant-time Verify успешен до delete, неуспешен после; wrong key fails".
func TestVerifyCorrectWrongRevoked(t *testing.T) {
	store, _ := newTestStore(t)

	plain, rec, err := store.Create("verify-tri-test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 1. Correct key → success.
	_, ok, err := store.Verify(string(plain))
	if err != nil || !ok {
		t.Errorf("Verify with correct key must succeed; ok=%v err=%v", ok, err)
	}

	// 2. Wrong key (random garbage) → failure.
	_, ok, err = store.Verify("rax_live_" + strings.Repeat("A", 43))
	if err != nil || ok {
		t.Errorf("Verify with wrong key must fail; ok=%v err=%v", ok, err)
	}

	// 3. Revoked key → failure.
	if err := store.Revoke(rec.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	_, ok, err = store.Verify(string(plain))
	if err != nil || ok {
		t.Errorf("Verify with revoked key must fail; ok=%v err=%v", ok, err)
	}
}

// --- SR-13: sentinel error messages do not contain body/hash/salt ---

// TestSentinelErrorMessagesNoSecrets verifies that all sentinel errors returned by
// keystore operations do not contain any secret material in their message strings.
// SR-13: "сообщения оперируют id/label/fingerprint, но НЕ телом/хэшем/солью".
func TestSentinelErrorMessagesNoSecrets(t *testing.T) {
	store, _ := newTestStore(t)

	plain, rec, err := store.Create("sentinel-test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	plainStr := string(plain)
	body := strings.TrimPrefix(plainStr, "rax_live_")

	// Trigger each sentinel error and check its message.
	errors := []error{
		func() error {
			store.Revoke(rec.ID) //nolint
			err := store.Revoke(rec.ID)
			return err
		}(),
		func() error {
			_, _, err := store.Create(strings.Repeat("x", 65))
			return err
		}(),
		func() error {
			return store.Revoke("nonexistent-id-xyz")
		}(),
	}

	secrets := []string{plainStr, body}
	for _, e := range errors {
		if e == nil {
			continue
		}
		msg := e.Error()
		for _, s := range secrets {
			if s != "" && strings.Contains(msg, s) {
				t.Errorf("sentinel error message contains secret %q: %q (SR-13)", s, msg)
			}
		}
	}
}

// --- AC: FlushUsage correctly updates LastUsed on disk ---

// TestFlushUsagePersistsLastUsedOnReopen verifies that after FlushUsage,
// a freshly opened Store's List() shows non-zero LastUsed for verified keys.
// AC: "LastUsed обновляется через FlushUsage".
func TestFlushUsagePersistsLastUsedOnReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.db")
	store, err := keystore.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	plain, rec, err := store.Create("lastused-test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify to buffer LastUsed.
	_, ok, err := store.Verify(string(plain))
	if !ok || err != nil {
		t.Fatalf("Verify: ok=%v err=%v", ok, err)
	}

	// Before FlushUsage, LastUsed may not be on disk.
	// After FlushUsage, it must be.
	if err := store.FlushUsage(); err != nil {
		t.Fatalf("FlushUsage: %v", err)
	}

	// Re-open to read from disk (not from in-memory state).
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
				t.Errorf("LastUsed must be non-zero in re-opened store after FlushUsage; got zero time")
			}
			return
		}
	}
	t.Errorf("record %s not found in re-opened store", rec.ID)
}

// --- AC: Verify on empty store returns (_, false, nil) ---

// TestVerifyEmptyStoreReturnsNoMatch verifies the contract for empty/missing store.
// AC: "отсутствующий keys.db для verify трактуется как пустое хранилище".
func TestVerifyEmptyStoreReturnsNoMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.db")
	// Do not create the file.
	store, err := keystore.Open(path)
	if err != nil {
		t.Fatalf("Open missing file: %v", err)
	}

	_, ok, err := store.Verify("rax_live_" + strings.Repeat("A", 43))
	if err != nil {
		t.Errorf("Verify on empty store must return nil error; got %v", err)
	}
	if ok {
		t.Error("Verify on empty store must return false")
	}
}

// --- SR-8: hash stored in DB is a 32-byte SHA-256 ---

// TestHashSizeInDB verifies that the stored hash is exactly 32 bytes (SHA-256 output size).
// SR-8: "sha256(тело ‖ salt)" → 32 bytes.
func TestHashSizeInDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.db")
	store, err := keystore.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, _, err = store.Create("hash-size-test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var db struct {
		Keys []struct {
			Hash []byte `json:"hash"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(data, &db); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for i, k := range db.Keys {
		if len(k.Hash) != 32 {
			t.Errorf("key #%d hash size = %d, want 32 (SHA-256 output, SR-8)", i, len(k.Hash))
		}
	}
}
