// Package keystore manages API key generation, storage, verification, and revocation.
// Keys are stored as sha256(body+per-key-salt)+salt in a JSON flat-file (keys.db).
// All secrets are handled according to SECURITY-BASELINE §1.
package keystore

import "errors"

// Sentinel errors returned by Store methods.
// CLI exit-code mapping: ErrNotFound/ErrAlreadyRevoked/ErrLabelTooLong/ErrCorrupt → exit 1.
var (
	// ErrNotFound is returned by Revoke when the given id does not exist in the store.
	ErrNotFound = errors.New("key not found")

	// ErrAlreadyRevoked is returned by Revoke when the key is already revoked.
	ErrAlreadyRevoked = errors.New("key is already revoked")

	// ErrCorrupt is returned by Open when keys.db exists but cannot be parsed.
	// The file is NOT modified on this error (SR-22).
	ErrCorrupt = errors.New("key store is corrupted or unreadable")

	// ErrLabelTooLong is returned by Create when len(label) > 64 (spec D4, SR).
	ErrLabelTooLong = errors.New("label is too long (max 64 characters)")
)
