package server

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// hostOriginMiddleware validates the Host and Origin headers per ADR-002 and SR-15/SR-16.
//
// Rules:
//   - Host (host part only, port stripped) NOT in hostAllow → 403
//   - Origin PRESENT and NOT in originAllow → 403
//   - Origin ABSENT → pass through (non-browser raxd agents: curl/SDK don't send Origin)
//
// SR-14: this middleware is placed BEFORE auth in the chain.
// SR-19/SR-20: every 403 denial is written to the audit log via auditFn.
func hostOriginMiddleware(hostAllow, originAllow []string, auditFn AuditFn) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			remote := remoteIP(r)

			// Validate Host header (SR-15).
			host := r.Host
			if host == "" {
				host = r.Header.Get("Host")
			}
			hostOnly, _, err := net.SplitHostPort(host)
			if err != nil {
				// No port in host — use as-is.
				hostOnly = host
			}
			if !contains(hostAllow, hostOnly) {
				// SR-19/SR-20: audit every Host denial. Reason is NOT sent in response body
				// (anti-enumeration per ux-spec §3.5).
				auditFn(AuditRecord{
					Fingerprint: "-",
					RemoteAddr:  remote,
					Result:      "deny",
					Reason:      "invalid host header",
				})
				http.Error(w, "", http.StatusForbidden)
				return
			}

			// Validate Origin header only if it is present (SR-16).
			origin := r.Header.Get("Origin")
			if origin != "" && !originAllowed(origin, originAllow) {
				// SR-19/SR-20: audit every Origin denial. Reason is NOT sent in response body
				// (anti-enumeration per ux-spec §3.6).
				auditFn(AuditRecord{
					Fingerprint: "-",
					RemoteAddr:  remote,
					Result:      "deny",
					Reason:      "invalid origin header",
				})
				http.Error(w, "", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// bodyLimitMiddleware wraps r.Body in http.MaxBytesReader(w, r.Body, limit) so that
// reading more than limit bytes from the body returns an error and typically produces
// a 413 response via the default http.MaxBytesError handling.
//
// SR-25: protection against large-body flooding. Placed as an outer layer so that
// every handler (health, dispatch, future handlers) inherits the limit automatically.
// The limit is set per-request; http.MaxBytesReader is idempotent on Body replacement.
func bodyLimitMiddleware(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

// BodyLimitMiddlewareForTest is a test-only export of bodyLimitMiddleware.
// It allows server_test (external package) to drive the middleware directly
// without a full TLS server, verifying SR-25 body-limit enforcement in isolation.
// NOT for production use — use bodyLimitMiddleware via New() instead.
func BodyLimitMiddlewareForTest(limit int64) func(http.Handler) http.Handler {
	return bodyLimitMiddleware(limit)
}

// recoverMiddleware catches panics and returns 500 without crashing the server.
// SR-25: handles mid-handshake / handler panics.
func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				http.Error(w, "", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// rateLimitMiddleware checks per-key and per-IP rate limits after auth.
// Requires fingerprintFromCtx to be set (i.e., must run AFTER authMiddleware).
// SR-17: per-key AND per-IP; 429 on exceed; audit RATE.
func rateLimitMiddleware(limiters *Limiters, auditFn AuditFn) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			remote := remoteIP(r)
			fp := fingerprintFromCtx(r.Context())
			ip := ipOnly(remote)

			// Check per-key first, then per-IP.
			// Limiters.Allow checks both atomically (under mu).
			if !limiters.Allow(fp, ip) {
				// Determine which limit was hit for the reason string.
				// We report per-key if fp is not "-"; otherwise per-IP.
				reason := "rate limit exceeded (ip)"
				if fp != "-" {
					reason = "rate limit exceeded (key)"
				}
				auditFn(AuditRecord{
					Fingerprint: fp,
					RemoteAddr:  remote,
					Result:      "rate-limited",
					Reason:      reason,
				})
				http.Error(w, "", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// contains reports whether s is in the slice (case-insensitive).
func contains(slice []string, s string) bool {
	s = strings.ToLower(s)
	for _, v := range slice {
		if strings.ToLower(v) == s {
			return true
		}
	}
	return false
}

// originAllowed checks whether the host part of origin (a full URL per RFC 6454)
// exactly matches any entry in allow (case-insensitive).
//
// SR-16 strict-match rules:
//   - Parse origin via url.Parse; extract u.Hostname() (strips port, handles brackets).
//   - Compare extracted hostname EXACTLY (case-insensitive) to each allowlist entry.
//   - Empty hostname after parse (e.g. relative URL, unparseable) → NOT allowed.
//   - HasPrefix is intentionally NOT used — it enables subdomain-bypass attacks
//     such as Origin: https://localhost.evil.com passing allowlist entry "localhost".
func originAllowed(origin string, allow []string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		// Unparseable Origin → treat as hostile.
		return false
	}
	hostname := strings.ToLower(u.Hostname())
	if hostname == "" {
		// No host component (e.g. relative URL, bare string) → not allowed.
		return false
	}
	return contains(allow, hostname)
}

// remoteIP returns the IP:port of the request, stripping X-Forwarded-For
// (we don't trust proxies for local-only server).
func remoteIP(r *http.Request) string {
	return r.RemoteAddr
}

// ipOnly extracts the IP part from an "IP:port" string.
func ipOnly(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}
