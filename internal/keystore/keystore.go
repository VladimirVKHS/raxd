package keystore

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const dbVersion = 1

// Store manages the API key database at a given path.
// It is safe to use Store methods concurrently from multiple goroutines;
// each method acquires an advisory flock for the duration of the operation.
//
// SR-25: Store does NOT retain PlainKey values in any field.
type Store struct {
	path string

	// usageBuf buffers LastUsed timestamps from Verify calls.
	// Written to disk only by FlushUsage (SR-17).
	usageBuf map[string]time.Time
}

// Open creates a Store bound to path (the KeysDB path from config.PathSet).
// If the file does not exist or is empty, the Store treats it as empty (not an error) — SR-22.
// If the file exists, is non-empty, and is malformed, Open returns ErrCorrupt without modifying the file.
func Open(path string) (*Store, error) {
	// Probe for corruption only if the file exists and is non-empty.
	info, err := os.Stat(path)
	if err == nil && info.Size() > 0 {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrCorrupt, err.Error())
		}
		var db Database
		if err := json.NewDecoder(f).Decode(&db); err != nil {
			_ = f.Close()
			return nil, ErrCorrupt
		}
		_ = f.Close()
	}
	return &Store{
		path:     path,
		usageBuf: make(map[string]time.Time),
	}, nil
}

// Create generates a new API key, stores the record, and returns the PlainKey for
// one-time display plus the Record metadata.
//
// Preconditions: label ≤ 64 characters (ErrLabelTooLong otherwise).
// Atomicity: write is temp→chmod 0600→sync→rename→fsync-dir (SR-20).
// Lock: exclusive flock for the duration of read-modify-write (SR-23).
// SR-25: PlainKey is returned to the caller; Store never retains it.
func (s *Store) Create(label string) (PlainKey, Record, error) {
	if len(label) > 64 {
		return "", Record{}, ErrLabelTooLong
	}

	lf, err := acquireLock(s.path, lockExclusive)
	if err != nil {
		return "", Record{}, err
	}
	defer releaseLock(lf)

	db, err := s.readDB()
	if err != nil {
		return "", Record{}, err
	}

	// Build collision set for ID generation (SR-5).
	existing := make(map[string]struct{}, len(db.Keys))
	for _, k := range db.Keys {
		existing[k.ID] = struct{}{}
	}

	// Generate credentials (SR-1, SR-4, SR-5).
	body := generateBody()
	salt := generateSalt()
	hash := hashKey(body, salt)
	id := generateID(existing)

	rec := Record{
		ID:      id,
		Label:   label,
		Created: time.Now().UTC(),
		hash:    hash,
		salt:    salt,
	}

	db.Keys = append(db.Keys, toDBRecord(rec))
	if err := s.writeDB(db); err != nil {
		return "", Record{}, err
	}

	// SR-25: PlainKey is returned directly to caller; not stored in Store.
	return PlainKey(body), rec, nil
}

// List returns all active (non-revoked) records without any secret material.
// Missing file → empty slice, nil error (SR-22).
// Lock: shared flock (SR-23).
// SR-12: returned Records contain only id/label/created/last-used.
func (s *Store) List() ([]Record, error) {
	lf, err := acquireLock(s.path, lockShared)
	if err != nil {
		return nil, err
	}
	if lf == nil {
		// File does not exist → empty store.
		return nil, nil
	}
	defer releaseLock(lf)

	db, err := s.readDB()
	if err != nil {
		return nil, err
	}

	var out []Record
	for _, d := range db.Keys {
		if d.Revoked {
			continue
		}
		r := fromDBRecord(d)
		// SR-12: strip hash/salt from returned value (they stay in internal fields,
		// which are unexported — callers cannot access them).
		out = append(out, r)
	}
	return out, nil
}

// Revoke marks a key as revoked (soft-delete; record is retained for audit).
// Returns ErrNotFound or ErrAlreadyRevoked on misuse (both → exit 1 in CLI).
// Lock: exclusive flock (SR-23).
// SR-16: Revoke is immediately reflected in subsequent Verify calls.
func (s *Store) Revoke(id string) error {
	lf, err := acquireLock(s.path, lockExclusive)
	if err != nil {
		return err
	}
	defer releaseLock(lf)

	db, err := s.readDB()
	if err != nil {
		return err
	}

	found := false
	for i, k := range db.Keys {
		if k.ID == id {
			found = true
			if k.Revoked {
				return ErrAlreadyRevoked
			}
			db.Keys[i].Revoked = true
			db.Keys[i].RevokedAt = time.Now().UTC()
			break
		}
	}
	if !found {
		return ErrNotFound
	}

	return s.writeDB(db)
}

// Verify checks whether a presented full key (rax_live_…) matches any active record.
// Uses constant-time comparison for every candidate (SR-9, SR-10).
// Purely read-only: file is never written during Verify (SR-17).
// LastUsed is buffered in memory; flush to disk via FlushUsage.
// Lock: shared flock (SR-23).
// Returns (record, true, nil) on match; (_, false, nil) on no match; (_, _, err) on I/O error.
func (s *Store) Verify(presented string) (Record, bool, error) {
	lf, err := acquireLock(s.path, lockShared)
	if err != nil {
		return Record{}, false, err
	}
	if lf == nil {
		return Record{}, false, nil
	}
	defer releaseLock(lf)

	db, err := s.readDB()
	if err != nil {
		return Record{}, false, err
	}

	// Compute candidate hash once; re-use per record with its salt.
	// SR-9: constant-time comparison on every record, not short-circuit after first mismatch.
	var matched Record
	var found bool

	for _, d := range db.Keys {
		if d.Revoked {
			// SR-16: revoked keys are excluded from verification.
			continue
		}
		// Compute sha256(presented‖salt) and compare constant-time.
		candidate := hashKey(presented, d.Salt)
		// SR-9, SR-10: ONLY subtle.ConstantTimeCompare; no ==, bytes.Equal, etc.
		if subtle.ConstantTimeCompare(candidate, d.Hash) == 1 {
			matched = fromDBRecord(d)
			found = true
			// Do not break early — continue comparing to prevent timing leaks
			// (all records are visited in constant-time fashion regardless of match position).
		}
	}

	if found {
		// Buffer LastUsed update; do NOT write file here (SR-17).
		s.usageBuf[matched.ID] = time.Now().UTC()
		matched.LastUsed = s.usageBuf[matched.ID]
	}

	return matched, found, nil
}

// FlushUsage persists buffered LastUsed timestamps to disk.
// Uses exclusive flock; re-reads the file to avoid overwriting concurrent Revoke (SR-17).
// Revoked records are not updated (FlushUsage never resurrects a revoked key).
// No-op when the buffer is empty.
func (s *Store) FlushUsage() error {
	if len(s.usageBuf) == 0 {
		return nil
	}

	lf, err := acquireLock(s.path, lockExclusive)
	if err != nil {
		return err
	}
	defer releaseLock(lf)

	db, err := s.readDB()
	if err != nil {
		return err
	}

	for i, k := range db.Keys {
		t, ok := s.usageBuf[k.ID]
		if !ok {
			continue
		}
		if k.Revoked {
			// SR-17: do not touch revoked records.
			continue
		}
		db.Keys[i].LastUsed = t
	}

	if err := s.writeDB(db); err != nil {
		return err
	}

	// Clear buffer after successful flush.
	s.usageBuf = make(map[string]time.Time)
	return nil
}

// readDB reads and parses the JSON database from disk.
// Called under an appropriate flock held by the caller.
// Missing or empty file returns an empty Database (not an error).
func (s *Store) readDB() (Database, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return Database{Version: dbVersion}, nil
	}
	if err != nil {
		return Database{}, fmt.Errorf("%w: %s", ErrCorrupt, err.Error())
	}

	// Empty file is treated as empty store (created by acquireLock with O_CREATE).
	if len(data) == 0 {
		return Database{Version: dbVersion}, nil
	}

	var db Database
	if err := json.Unmarshal(data, &db); err != nil {
		return Database{}, ErrCorrupt
	}
	return db, nil
}

// writeDB atomically writes the database to disk.
// Protocol: temp file (same dir) → chmod 0600 → sync → close → rename → fsync dir.
// SR-20: no window with permissions wider than 0600.
// SR-21: temp file is cleaned up on any error before rename.
func (s *Store) writeDB(db Database) error {
	data, err := json.Marshal(db)
	if err != nil {
		return fmt.Errorf("cannot serialise key store: %w", err)
	}

	dir := filepath.Dir(s.path)

	// Create temp file in the same directory (required for atomic rename on same FS).
	tmp, err := os.CreateTemp(dir, ".keys-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot create temp file for key store: %w", err)
	}
	tmpName := tmp.Name()

	// SR-20: set 0600 BEFORE writing any content.
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName) // SR-21: clean up temp on error.
		return fmt.Errorf("cannot set temp file permissions: %w", err)
	}

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName) // SR-21
		return fmt.Errorf("cannot write key store: %w", err)
	}

	// Sync data to disk before rename for durability.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName) // SR-21
		return fmt.Errorf("cannot sync key store: %w", err)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName) // SR-21
		return fmt.Errorf("cannot close temp key store: %w", err)
	}

	// Atomic rename: target sees either old content or fully new content, never partial.
	if err := os.Rename(tmpName, s.path); err != nil {
		_ = os.Remove(tmpName) // SR-21: best-effort cleanup.
		return fmt.Errorf("cannot commit key store: %w", err)
	}

	// fsync the directory to make the rename durable across power loss (Trade-offs: Durability).
	dirF, err := os.Open(dir)
	if err == nil {
		_ = dirF.Sync()
		_ = dirF.Close()
	}
	// Directory fsync failure is non-fatal (best-effort durability).

	return nil
}
