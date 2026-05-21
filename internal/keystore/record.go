package keystore

import "time"

// Record holds the metadata and credential material for a single API key.
// hash and salt are unexported; they are serialised to JSON only via explicit tags.
// PlainKey (the rax_live_… string) is NEVER stored here — it lives only on the
// caller's stack during Create and is returned for one-time display.
//
// SR-7: only hash+salt stored, not the plaintext key.
type Record struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Created   time.Time `json:"created"`
	LastUsed  time.Time `json:"last_used,omitempty"`
	RevokedAt time.Time `json:"revoked_at,omitempty"`
	Revoked   bool      `json:"revoked"`

	// hash = sha256(body‖salt). Unexported to prevent accidental logging.
	// SR-7, SR-13: never exposed through List/Verify return values.
	hash []byte `json:"-"` //nolint:unused // accessed via json tags in database

	// salt is the per-key random salt (≥16 bytes, SR-4).
	salt []byte `json:"-"` //nolint:unused
}

// dbRecord is the on-disk representation of a Record.
// It mirrors Record but uses exported fields for JSON marshalling,
// with hash and salt stored as hex strings (safe, not plaintext key).
// This separation keeps Record clean (no json tags on sensitive fields)
// while allowing controlled serialisation.
type dbRecord struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Created   time.Time `json:"created"`
	LastUsed  time.Time `json:"last_used,omitempty"`
	RevokedAt time.Time `json:"revoked_at,omitempty"`
	Revoked   bool      `json:"revoked"`
	Hash      []byte    `json:"hash"`
	Salt      []byte    `json:"salt"`
}

// toDBRecord converts a Record to its on-disk form.
func toDBRecord(r Record) dbRecord {
	return dbRecord{
		ID:        r.ID,
		Label:     r.Label,
		Created:   r.Created,
		LastUsed:  r.LastUsed,
		RevokedAt: r.RevokedAt,
		Revoked:   r.Revoked,
		Hash:      r.hash,
		Salt:      r.salt,
	}
}

// fromDBRecord converts a dbRecord (from disk) to an in-memory Record.
func fromDBRecord(d dbRecord) Record {
	return Record{
		ID:        d.ID,
		Label:     d.Label,
		Created:   d.Created,
		LastUsed:  d.LastUsed,
		RevokedAt: d.RevokedAt,
		Revoked:   d.Revoked,
		hash:      d.Hash,
		salt:      d.Salt,
	}
}

// Database is the top-level on-disk structure for keys.db.
type Database struct {
	Version int        `json:"version"`
	Keys    []dbRecord `json:"keys"`
}

// PlainKey is the full rax_live_… key string returned by Create for one-time display.
// It is a named type to make it harder to accidentally log or store.
// SR-25: PlainKey lives only on the caller stack; Store never retains it.
type PlainKey string
