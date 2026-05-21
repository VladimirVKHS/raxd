package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/vladimirvkhs/raxd/internal/keystore"
)

// ctxKey is an unexported type for context keys in this package.
type ctxKey int

const (
	ctxKeyFingerprint ctxKey = iota
	ctxKeyKeyID
)

// fingerprintFromCtx returns the fingerprint stored in ctx by authMiddleware.
// Returns "-" if not set.
func fingerprintFromCtx(ctx context.Context) string {
	if fp, ok := ctx.Value(ctxKeyFingerprint).(string); ok && fp != "" {
		return fp
	}
	return "-"
}

// authMiddleware extracts the Bearer token from the Authorization header,
// verifies it via store.Verify, and stores the fingerprint in the request
// context for downstream use (audit, rate-limit).
//
// Mapping (SR-9, SR-12, SR-13, security-requirements.md table):
//   - No header / not Bearer / empty token → 401
//   - store.Verify → (_, false, nil)         → 401
//   - store.Verify → error / ErrCorrupt       → 403 + audit DENY
//   - success                                  → fp+id into context, call next
//
// SECURITY (SR-10): NO comparison of key/token/hash using ==, strings.EqualFold,
// bytes.Equal etc. All verification goes through keystore.Verify (constant-time).
// SECURITY (SR-21): raw Authorization header is NEVER logged.
func authMiddleware(store *keystore.Store, auditFn AuditFn) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			remote := remoteIP(r)

			// Extract Bearer token. SR-9: key ONLY from Authorization header.
			token, ok := bearerToken(r)
			if !ok {
				// No header or not Bearer format → 401 (SR-9, mapping table).
				auditFn(AuditRecord{
					Fingerprint: "-",
					RemoteAddr:  remote,
					Result:      "fail",
					Reason:      "no authorization header",
				})
				http.Error(w, "", http.StatusUnauthorized)
				return
			}

			// SR-10: verification only through keystore.Verify (constant-time inside).
			rec, found, err := store.Verify(token)
			if err != nil {
				// Verify error (including ErrCorrupt) → 403 (SR-13, mapping table).
				// Compute fingerprint for audit without logging the key body.
				fp := keystore.Fingerprint(token)
				reason := "key store unavailable"
				if errors.Is(err, keystore.ErrCorrupt) {
					fp = "-"
				}
				auditFn(AuditRecord{
					Fingerprint: fp,
					RemoteAddr:  remote,
					Result:      "deny",
					Reason:      reason,
				})
				http.Error(w, "", http.StatusForbidden)
				return
			}

			if !found {
				// Unknown / revoked key → 401 (SR-9, mapping table).
				fp := keystore.Fingerprint(token)
				auditFn(AuditRecord{
					Fingerprint: fp,
					RemoteAddr:  remote,
					Result:      "fail",
					Reason:      "authentication failed",
				})
				http.Error(w, "", http.StatusUnauthorized)
				return
			}

			// Success: store fingerprint and key ID in context (AC8, SR-9).
			// SECURITY: only fingerprint (non-reversible) goes into context, NOT the token.
			fp := rec.Fingerprint
			if fp == "" {
				fp = keystore.Fingerprint(token)
			}
			ctx := context.WithValue(r.Context(), ctxKeyFingerprint, fp)
			ctx = context.WithValue(ctx, ctxKeyKeyID, rec.ID)

			// SR-19/SR-20 (ISSUE-3): AUTH audit record is NOT written here.
			// It is written by authSuccessAuditMiddleware AFTER rate-limit passes,
			// ensuring exactly ONE audit record per request that reaches a handler.
			// Writing AUTH here + RATE in rateLimitMiddleware would produce two records
			// for valid-key rate-limited requests, violating the one-record invariant.

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// authSuccessAuditMiddleware writes a single AUTH audit record for every request
// that passes BOTH authentication AND rate-limiting (i.e., reaches a real handler).
//
// ISSUE-3 / SR-19 invariant: exactly ONE audit record per request.
//   - Rejected by Host/Origin → DENY record (hostOriginMiddleware), stop.
//   - Rejected by auth → FAIL record (authMiddleware), stop.
//   - Rejected by rate-limit → RATE record (rateLimitMiddleware), stop.
//   - Passed everything → AUTH record here (authSuccessAuditMiddleware), once.
//
// This middleware must be placed AFTER rateLimitMiddleware in the chain so it
// runs only when rate-limit allows the request through.
// Requires authMiddleware to have already stored the fingerprint in context.
func authSuccessAuditMiddleware(auditFn AuditFn) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fp := fingerprintFromCtx(r.Context())
			remote := remoteIP(r)
			// Write AUTH success record — request has cleared all gates.
			auditFn(AuditRecord{
				Fingerprint: fp,
				RemoteAddr:  remote,
				Result:      "success",
			})
			next.ServeHTTP(w, r)
		})
	}
}

// bearerToken extracts the token from "Authorization: Bearer <token>".
// Returns ("", false) when header is absent, not Bearer scheme, or token is empty.
// SECURITY (SR-21): this function returns the raw token only for passing to
// keystore.Verify; it must never be logged.
func bearerToken(r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return "", false
	}
	if !strings.HasPrefix(auth, "Bearer ") {
		return "", false
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	return token, true
}
