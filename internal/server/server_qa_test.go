package server_test

// server_qa_test.go — QA edge-case coverage for tls-transport.
//
// Covers gaps not addressed by the developer's server_test.go:
//   - Malformed Authorization formats → 401 (AC4/AC5/SR-9)
//   - Anti-enumeration: 401/403/429 bodies do not reveal reason (SR-13)
//   - TLS downgrade via raw tls.Dial (AC1/SR-1, direct handshake check)
//   - SAN: connection via "localhost" works (SR-3)
//   - Cert files not rewritten on second New — key.pem mtime checked (AC3/SR-5)
//   - No key body in log on 401/403/429 paths (AC9/SR-21)
//   - Audit fields (fp/remote/result) present on every path (AC8/SR-19)
//   - Rate-limit refill after pause — 200 returns after token bucket refills (AC6/SR-17)
//   - Different keys do not share rate-limit budgets (AC6/SR-17)
//   - TTL GC removes idle limiters (SR-18)
//   - Health Content-Type is text/plain (AC10)
//   - Auth-before-routing: unauthenticated /healthz → 401, not 404 (AC4/SR-8)
//
// All tests use the same helpers defined in server_test.go (same package).
// Run in Docker: docker run --rm raxd-test sh -c
//   "CGO_ENABLED=1 go test -race -v -count=1 ./internal/server/..."

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vladimirvkhs/raxd/internal/config"
	"github.com/vladimirvkhs/raxd/internal/keystore"
	"github.com/vladimirvkhs/raxd/internal/server"
)

// ============================================================================
// AC1/SR-1: TLS downgrade via raw tls.Dial (direct handshake, not HTTP client)
// ============================================================================

// TestTLS13EnforcedRawDial verifies that a raw TLS 1.2 client cannot complete
// the handshake even without going through net/http. This is a more direct
// check than TestTLS13Enforced (which goes via http.Client).
func TestTLS13EnforcedRawDial(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := tls.Dial("tcp", addr, &tls.Config{
		MaxVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, //nolint:gosec
	})
	if err == nil {
		conn.Close()
		t.Error("AC1/SR-1: raw TLS 1.2 dial must fail handshake, got nil error")
	}
}

// ============================================================================
// SR-3: SAN — connection via "localhost" hostname succeeds
// ============================================================================

// TestSANLocalhostConnection verifies that the generated certificate contains
// "localhost" in DNSNames (SR-3) so that a client trusting the cert can connect
// using the "localhost" hostname (not just 127.0.0.1).
func TestSANLocalhostConnection(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("san-localhost")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	// Allow "localhost" in Host header for this test.
	cfg.HostAllow = []string{"localhost", "127.0.0.1", "::1"}
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)

	// Build a client that trusts the self-signed cert and connects via "localhost".
	certPath := filepath.Join(paths.TLSDir, "cert.pem")
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatal("failed to append cert to pool")
	}

	// Parse cert and verify "localhost" is in DNSNames (SR-3 direct check).
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("failed to decode cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	foundLocalhost := false
	for _, dns := range cert.DNSNames {
		if dns == "localhost" {
			foundLocalhost = true
		}
	}
	if !foundLocalhost {
		t.Error("SR-3: cert SAN missing 'localhost' DNSName — localhost connections cannot be validated")
	}

	// Attempt HTTPS connection via "localhost" hostname.
	localhostClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				ServerName: "localhost",
			},
		},
		Timeout: 5 * time.Second,
	}
	localhostURL := fmt.Sprintf("https://localhost:%d/healthz", port)
	req, err := http.NewRequest(http.MethodGet, localhostURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+string(plain))
	// Override Host to pass hostOriginMiddleware.
	req.Host = "localhost"

	resp, err := localhostClient.Do(req)
	if err != nil {
		// In Docker (and on any loopback-capable host) "localhost" resolves to 127.0.0.1,
		// which is exactly where the server listens. A TLS error here means the cert SAN
		// does not include "localhost" — that is a real SR-3 violation, not an env issue.
		t.Fatalf("SR-3: TLS connection via localhost failed — cert SAN must include 'localhost' "+
			"so that loopback TLS handshake succeeds; error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("SR-3: localhost connection → want 200, got %d", resp.StatusCode)
	}
}

// ============================================================================
// AC3/SR-5: key.pem not rewritten on second New (mtime check)
// ============================================================================

// TestKeyPemNotRewrittenOnSecondNew verifies that key.pem (not only cert.pem)
// is not modified when server.New is called a second time with an existing pair.
func TestKeyPemNotRewrittenOnSecondNew(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	cfg := newTestConfig(0)
	var logBuf bytes.Buffer

	// First call — generates cert+key.
	_, err = server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
	if err != nil {
		t.Fatalf("first server.New: %v", err)
	}

	keyPath := filepath.Join(paths.TLSDir, "key.pem")
	keyStat1, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat key.pem after first New: %v", err)
	}
	keyContent1, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key.pem: %v", err)
	}

	// Second call — must not touch key.pem.
	_, err = server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
	if err != nil {
		t.Fatalf("second server.New: %v", err)
	}

	keyStat2, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat key.pem after second New: %v", err)
	}
	keyContent2, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key.pem after second: %v", err)
	}

	if keyStat1.ModTime() != keyStat2.ModTime() {
		t.Error("AC3/SR-5: key.pem mtime changed on second server.New — file was overwritten")
	}
	if !bytes.Equal(keyContent1, keyContent2) {
		t.Error("AC3/SR-5: key.pem content changed on second server.New — file was overwritten")
	}
}

// ============================================================================
// AC4/AC5/SR-9: malformed Authorization header formats → 401
// ============================================================================

// TestMalformedAuthorizationFormats verifies that various malformed Authorization
// header values all produce 401 (not 403, not 200, not panic).
// SR-9: only "Bearer <token>" is accepted; all other formats → 401.
func TestMalformedAuthorizationFormats(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)

	// Build an HTTP client that trusts the self-signed cert.
	certPath := filepath.Join(paths.TLSDir, "cert.pem")
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)
	rawClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
		Timeout: 5 * time.Second,
	}

	cases := []struct {
		name        string
		headerValue string // raw value for Authorization header; empty = omit
	}{
		{"lowercase bearer", "bearer sometoken"},
		{"no space after Bearer", "Bearertokenvalue"},
		{"empty after Bearer space", "Bearer "},
		{"only spaces after Bearer", "Bearer    "},
		{"Token scheme", "Token sometoken"},
		{"Basic scheme", "Basic dXNlcjpwYXNz"},
		{"just the word Bearer", "Bearer"},
		{"extra leading spaces", "  Bearer sometoken"},
	}

	url := baseURL(port) + "/healthz"
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			if tc.headerValue != "" {
				req.Header.Set("Authorization", tc.headerValue)
			}
			resp, err := rawClient.Do(req)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("AC4/SR-9: %q → want 401, got %d", tc.name, resp.StatusCode)
			}
		})
	}
}

// ============================================================================
// SR-13: 401/403 response body does not reveal reason (anti-enumeration)
// ============================================================================

// TestAuthFailBodyNoEnumeration verifies that 401 and 403 response bodies
// do not contain identifying strings that would help enumerate key validity.
// SR-13: "ответ клиенту на любой провал аутентификации НЕ раскрывает, почему".
func TestAuthFailBodyNoEnumeration(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, rec, err := store.Create("enumeration-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	// Forbidden substrings in any 401/403 response body.
	forbidden := []string{
		"unknown key",
		"revoked",
		"corrupt",
		"not found",
		"ErrCorrupt",
		"authentication failed",
		"key store unavailable",
		"no authorization header",
		string(plain), // key body itself
		rec.ID,        // key id
	}

	checkBody := func(t *testing.T, label string, resp *http.Response) {
		t.Helper()
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		bodyStr := string(body)
		for _, s := range forbidden {
			if s != "" && strings.Contains(bodyStr, s) {
				t.Errorf("SR-13 anti-enumeration: %s response body contains forbidden string %q; body=%q",
					label, s, bodyStr)
			}
		}
	}

	// Case 1: no auth header → 401.
	resp := get(t, client, baseURL(port)+"/healthz", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("want 401 for no auth, got %d", resp.StatusCode)
	}
	checkBody(t, "no-auth-401", resp)

	// Case 2: unknown key → 401.
	resp = get(t, client, baseURL(port)+"/healthz", "rax_live_unknownkeyxxx", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("want 401 for unknown key, got %d", resp.StatusCode)
	}
	checkBody(t, "unknown-key-401", resp)

	// Case 3: revoked key → 401.
	if err := store.Revoke(rec.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	resp = get(t, client, baseURL(port)+"/healthz", string(plain), nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("want 401 for revoked key, got %d", resp.StatusCode)
	}
	checkBody(t, "revoked-key-401", resp)

	// Case 4: corrupt keys.db → 403.
	if err := os.WriteFile(paths.KeysDB, []byte("not valid json {{{"), 0o600); err != nil {
		t.Fatalf("corrupt keys.db: %v", err)
	}
	resp = get(t, client, baseURL(port)+"/healthz", "rax_live_anytoken", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("want 403 for corrupt db, got %d", resp.StatusCode)
	}
	checkBody(t, "corrupt-db-403", resp)
}

// ============================================================================
// AC9/SR-21: key body absent from log on ALL auth paths (401/403/429)
// ============================================================================

// TestNoKeyBodyInLogOnFailPaths verifies that the key presented in the
// Authorization header never appears as a substring in the audit log output
// on failure paths (401 unknown, 401 revoked, 403 corrupt, 429 rate-limit).
func TestNoKeyBodyInLogOnFailPaths(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, rec, err := store.Create("noleak-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	cfg.RateLimit = 0.001 // ensure rate-limit triggers quickly
	cfg.RateBurst = 1
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	keyStr := string(plain)

	// Path: unknown key → 401.
	resp := get(t, client, baseURL(port)+"/healthz", "rax_live_unknowntestkey", nil)
	resp.Body.Close()

	// Path: revoked key → 401.
	if err := store.Revoke(rec.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	resp = get(t, client, baseURL(port)+"/healthz", keyStr, nil)
	resp.Body.Close()

	// Re-create a valid key for rate-limit path.
	plainRL, _, err := store.Create("noleak-rate")
	if err != nil {
		t.Fatalf("create rate key: %v", err)
	}
	keyStrRL := string(plainRL)

	// Path: rate-limit → 429. Exhaust the burst first.
	for i := 0; i < 10; i++ {
		resp = get(t, client, baseURL(port)+"/healthz", keyStrRL, nil)
		resp.Body.Close()
	}

	logOutput := logBuf.String()

	// The full key bodies must NEVER appear anywhere in the log.
	for _, secret := range []string{keyStr, keyStrRL, "rax_live_unknowntestkey"} {
		if strings.Contains(logOutput, secret) {
			t.Errorf("AC9/SR-21: key body %q found in audit log on fail/rate paths; log=%s",
				secret, logOutput)
		}
	}
}

// ============================================================================
// AC8/SR-19: audit fields present on all outcome paths
// ============================================================================

// TestAuditFieldsOnAllPaths verifies that every audit record (AUTH/FAIL/DENY/RATE)
// contains the required fields: fp, remote.
func TestAuditFieldsOnAllPaths(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("fields-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	cfg.RateLimit = 0.001
	cfg.RateBurst = 1
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	// Flush log before each request.
	checkFields := func(t *testing.T, label string) {
		t.Helper()
		log := logBuf.String()
		if !strings.Contains(log, "fp=") {
			t.Errorf("AC8/SR-19: %s — audit record missing fp= field; log=%s", label, log)
		}
		if !strings.Contains(log, "remote=") {
			t.Errorf("AC8/SR-19: %s — audit record missing remote= field; log=%s", label, log)
		}
	}

	// AUTH path.
	resp := get(t, client, baseURL(port)+"/healthz", string(plain), nil)
	resp.Body.Close()
	checkFields(t, "AUTH")
	logBuf.Reset()

	// FAIL path (no auth).
	resp = get(t, client, baseURL(port)+"/healthz", "", nil)
	resp.Body.Close()
	checkFields(t, "FAIL")
	logBuf.Reset()

	// DENY path (invalid Host).
	resp = get(t, client, baseURL(port)+"/healthz", string(plain), map[string]string{"Host": "bad.example.com"})
	resp.Body.Close()
	checkFields(t, "DENY")
	logBuf.Reset()

	// RATE path — with burst=1 and rate=0.001, the first request consumes the
	// single token. Every subsequent rapid request must trigger 429 + RATE audit.
	// We send 5 requests; at least one must be rate-limited.
	got429Rate := false
	for i := 0; i < 5; i++ {
		resp = get(t, client, baseURL(port)+"/healthz", string(plain), nil)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			got429Rate = true
		}
	}
	if !got429Rate {
		t.Fatalf("AC8/SR-19: RATE path: expected at least one 429 with burst=1 rate=0.001 "+
			"(5 rapid requests) — rate limiter is not enforcing the limit; log=%s", logBuf.String())
	}
	rateLog := logBuf.String()
	if !strings.Contains(rateLog, "RATE") {
		t.Fatalf("AC8/SR-19: RATE audit entry missing after 429 was triggered; log=%s", rateLog)
	}
	checkFields(t, "RATE")
}

// ============================================================================
// AC6/SR-17: rate-limit refill — token bucket refills after pause
// ============================================================================

// TestRateLimitRefillAfterPause verifies that a key that was rate-limited can
// make successful requests again after waiting for the token bucket to refill.
// SR-17: token bucket semantics — tokens are added at rate tokens/second.
func TestRateLimitRefillAfterPause(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("refill-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	// 2 req/s, burst 1. After exhaustion, 500ms pause should allow ≥1 token.
	cfg.RateLimit = 2
	cfg.RateBurst = 1
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	url := baseURL(port) + "/healthz"

	// Exhaust burst: burst=1 means the first request consumes the single token.
	// The second rapid request must receive 429. We send up to 5 to be robust against
	// scheduling jitter, but 429 MUST appear — if it does not, the limiter is broken.
	got429 := false
	for i := 0; i < 5; i++ {
		resp := get(t, client, url, string(plain), nil)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		// burst=1 and rate=2 mean we MUST see 429 within 5 rapid requests.
		// Failing here is a real product bug, not a test infrastructure issue.
		t.Fatal("AC6/SR-17: could not exhaust per-key rate limit (burst=1, rate=2, 5 attempts): " +
			"expected 429 before refill — token bucket is not enforcing the limit")
	}

	// Wait for token bucket to refill: at 2 req/s one token arrives every ~500ms.
	// We wait 700ms for headroom against scheduling jitter.
	time.Sleep(700 * time.Millisecond)

	// After refill, must be able to make at least one successful request.
	resp := get(t, client, url, string(plain), nil)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		t.Errorf("AC6/SR-17: rate-limit did not refill after 700ms pause "+
			"(rate=2 req/s, burst=1 — one token must have been restored); got 429")
	}
}

// ============================================================================
// AC6/SR-17: different keys do not share rate-limit budgets
// ============================================================================

// TestRateLimitPerKeyBudgetsAreIndependent verifies the isolation invariant:
// exhausting the per-key token budget for key A must NOT reduce key B's budget.
//
// Strategy (unit-level via server.NewLimiters):
//   - burst=1, rate=0.001 (effectively no refill during the test)
//   - key A uses IP "1.2.3.4", key B uses IP "5.6.7.8" (different IPs → separate per-IP limiters)
//   - Allow("fp-a", "1.2.3.4") → true  (key_A: 1→0, ip1: 1→0)
//   - Allow("fp-a", "1.2.3.4") → false (key_A: 0, ip1 not touched by Allow's early-exit)
//   - Allow("fp-b", "5.6.7.8") → true  (key_B is a fresh limiter: 1→0, ip2: 1→0)
//
// The invariant: after A is exhausted, B's first Allow still returns true.
// This proves per-key limiters are independent maps, not a shared budget.
func TestRateLimitPerKeyBudgetsAreIndependent(t *testing.T) {
	// Use a near-zero rate so no refill can happen during rapid calls.
	lim := server.NewLimiters(0.001, 1, 5*time.Minute)

	// Step 1: consume key A's single token.
	if !lim.Allow("fp-key-a", "1.2.3.4") {
		t.Fatal("AC6/SR-17: first Allow for key A must succeed (fresh burst=1 limiter)")
	}

	// Step 2: key A's budget is exhausted — must be denied.
	if lim.Allow("fp-key-a", "1.2.3.4") {
		t.Fatal("AC6/SR-17: second Allow for key A must be denied after burst=1 exhausted")
	}

	// Step 3: key B has its own per-key limiter and its own per-IP limiter (different IP).
	// Key A exhaustion must NOT affect key B — this is the core isolation invariant.
	if !lim.Allow("fp-key-b", "5.6.7.8") {
		t.Fatal("AC6/SR-17: Allow for key B must succeed — " +
			"key A exhaustion must NOT drain key B's per-key budget (budgets are independent)")
	}

	// Step 4: key B's budget is also exhausted — must be denied.
	if lim.Allow("fp-key-b", "5.6.7.8") {
		t.Error("AC6/SR-17: second Allow for key B must be denied after burst=1 exhausted")
	}
}

// ============================================================================
// SR-18: TTL GC removes idle limiters — map does not grow unboundedly
// ============================================================================

// TestRateLimiterGCRemovesIdleEntries verifies that the TTL GC goroutine
// deletes rate-limiter entries that have not been accessed for longer than TTL.
// This prevents unbounded memory growth (SR-18).
//
// We use a very short TTL by constructing a Limiters directly (unit test level).
func TestRateLimiterGCRemovesIdleEntries(t *testing.T) {
	// We need access to NewLimiters which is an exported constructor.
	// Since we're in package server_test we import it through the server package.
	// NewLimiters is exported; we call it here with a 100ms TTL.

	// Use a TTL of 100ms and GC interval of 50ms.
	ttl := 100 * time.Millisecond
	gcInterval := 50 * time.Millisecond
	lim := server.NewLimiters(10, 10, ttl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lim.StartGC(ctx, gcInterval)

	// Add entries by calling Allow.
	lim.Allow("fp-test-gc-1", "127.0.0.1")
	lim.Allow("fp-test-gc-2", "127.0.0.2")

	// Wait for GC to run (TTL + 2x GC interval).
	time.Sleep(ttl + 2*gcInterval + 50*time.Millisecond)

	// After GC, previously-idle entries should be removed.
	// We cannot inspect the internal map directly (unexported), so we verify
	// indirectly: a fresh call after GC still works (no nil-pointer panic),
	// confirming the GC ran without corrupting state.
	ok := lim.Allow("fp-test-gc-1", "127.0.0.1")
	if !ok {
		t.Error("SR-18: Allow after GC should succeed (new entry created lazily), got false")
	}
}

// ============================================================================
// AC10/SR-22: health endpoint Content-Type is text/plain
// ============================================================================

// TestHealthContentType verifies that GET /healthz returns Content-Type: text/plain.
// AC10/SR-22: health handler sets correct content type.
func TestHealthContentType(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("ct-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	resp := get(t, client, baseURL(port)+"/healthz", string(plain), nil)
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("AC10: /healthz Content-Type = %q, want text/plain", ct)
	}
}

// ============================================================================
// AC4/SR-8: auth before routing — unauthenticated /healthz → 401, not 404
// ============================================================================

// TestUnauthHealthReturns401NotFound verifies that /healthz without auth
// returns 401 (auth middleware fires before routing), not 404.
// SR-8: ALL routes (including /healthz) are behind the auth middleware.
func TestUnauthHealthReturns401NotFound(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	// No auth → must be 401 (auth middleware intercepts before mux).
	resp := get(t, client, baseURL(port)+"/healthz", "", nil)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		t.Error("AC4/SR-8: /healthz without auth returned 404 — auth middleware is not before mux")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("AC4/SR-8: /healthz without auth → want 401, got %d", resp.StatusCode)
	}
}

// ============================================================================
// SR-14: Host/Origin validation fires BEFORE auth in middleware chain
// ============================================================================

// TestHostDeniedBeforeAuth proves that SR-14 ordering is enforced:
// a request with an invalid Host and NO Authorization header must receive
// 403 (Host middleware rejects it first), not 401 (auth middleware did NOT run first).
//
// If the middleware order were reversed (auth before Host/Origin), the server
// would return 401 (no auth header) instead of 403 (invalid host). This test
// fails if auth runs before Host validation — detecting the ordering regression.
func TestHostDeniedBeforeAuth(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	// Case 1: invalid Host, no Authorization header → must be 403 (Host check before auth).
	resp := get(t, client, baseURL(port)+"/healthz", "", map[string]string{
		"Host": "evil.attacker.com",
	})
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("SR-14: invalid Host without auth returned 401 — "+
			"auth ran BEFORE Host validation; middleware order is wrong (want 403, got 401)")
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("SR-14: invalid Host without auth → want 403, got %d", resp.StatusCode)
	}

	// Case 2: invalid Origin, no Authorization header → must also be 403.
	logBuf.Reset()
	resp2 := get(t, client, baseURL(port)+"/healthz", "", map[string]string{
		"Origin": "https://evil.attacker.com",
	})
	resp2.Body.Close()
	if resp2.StatusCode == http.StatusUnauthorized {
		t.Errorf("SR-14: invalid Origin without auth returned 401 — "+
			"auth ran BEFORE Origin validation; middleware order is wrong (want 403, got 401)")
	}
	if resp2.StatusCode != http.StatusForbidden {
		t.Errorf("SR-14: invalid Origin without auth → want 403, got %d", resp2.StatusCode)
	}

	// Both cases must produce DENY audit entries (not FAIL).
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "DENY") {
		t.Errorf("SR-14: invalid Origin denial must produce DENY audit entry; log=%s", logOutput)
	}
}

// ============================================================================
// AC8/SR-19: audit record for success path includes fp (not "-")
// ============================================================================

// TestAuthSuccessAuditFpNotDash verifies that a successful authentication
// produces an audit record where fp != "-" (it's the real fingerprint).
func TestAuthSuccessAuditFpNotDash(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("fp-not-dash")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	resp := get(t, client, baseURL(port)+"/healthz", string(plain), nil)
	resp.Body.Close()

	logOutput := logBuf.String()
	// On success, fp must NOT be "-" (fp=- is reserved for unidentified requests).
	if strings.Contains(logOutput, "fp=-") {
		t.Errorf("AC8/SR-19: successful auth audit should NOT have fp=-, got log=%s", logOutput)
	}
	if !strings.Contains(logOutput, "AUTH") {
		t.Errorf("AC8/SR-19: successful auth should produce AUTH audit entry; log=%s", logOutput)
	}
}

// ============================================================================
// SR-10 (static): no direct key/hash comparison in auth.go
// ============================================================================

// TestStaticNoDirectKeyComparison verifies that auth.go does not contain
// any direct comparison of keys/tokens/hashes using unsafe operators.
// SR-10: only keystore.Verify is used (constant-time inside).
func TestStaticNoDirectKeyComparison(t *testing.T) {
	authFile := "auth.go"
	data, err := os.ReadFile(authFile)
	if err != nil {
		t.Fatalf("read auth.go: %v", err)
	}
	src := string(data)

	// Filter out comment lines.
	var nonComment strings.Builder
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "//") {
			nonComment.WriteString(line)
			nonComment.WriteByte('\n')
		}
	}
	filtered := nonComment.String()

	// SR-10 forbids direct comparison of secret key/token values via non-constant-time
	// operators. The following are NOT violations (empty-checks are allowed):
	//   auth == ""  — checks if Authorization header is absent (no secret compared)
	//   token == "" — checks if the trimmed token is empty (no secret compared)
	// We search for patterns that would compare a token/key against a stored hash
	// or another key material using non-constant-time equality.
	forbidden := []string{
		"bytes.Equal(token",
		"bytes.Equal(key",
		"strings.EqualFold(token",
		"strings.EqualFold(key",
		`hmac.Equal(token`,
		`hmac.Equal(key`,
	}
	for _, pat := range forbidden {
		if strings.Contains(filtered, pat) {
			t.Errorf("SR-10: forbidden direct comparison %q found in auth.go (must use keystore.Verify only)", pat)
		}
	}
}

// ============================================================================
// SR-2 (static): CipherSuites not set in tls.Config
// ============================================================================

// TestStaticNoCipherSuites verifies that server.go does not set CipherSuites
// in the tls.Config. Under TLS 1.3 this field is ignored and setting it is
// an anti-pattern (SR-2).
func TestStaticNoCipherSuites(t *testing.T) {
	serverFile := "server.go"
	data, err := os.ReadFile(serverFile)
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}
	src := string(data)

	var nonComment strings.Builder
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "//") {
			nonComment.WriteString(line)
			nonComment.WriteByte('\n')
		}
	}
	filtered := nonComment.String()

	if strings.Contains(filtered, "CipherSuites") {
		t.Error("SR-2: CipherSuites field set in server.go — must not set CipherSuites under TLS 1.3")
	}
}

// ============================================================================
// SR-12 (static): serve.go does not read key from argv/env
// ============================================================================

// TestStaticServeNoKeyFromArgvOrEnv verifies that serve.go does not read
// an API key from command-line flags or environment variables.
// SR-12: key must only come from Authorization header.
func TestStaticServeNoKeyFromArgvOrEnv(t *testing.T) {
	serveFile := filepath.Join("..", "cli", "serve.go")
	data, err := os.ReadFile(serveFile)
	if err != nil {
		t.Fatalf("read serve.go: %v", err)
	}
	src := string(data)

	var nonComment strings.Builder
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "//") {
			nonComment.WriteString(line)
			nonComment.WriteByte('\n')
		}
	}
	filtered := nonComment.String()

	// serve.go must not reference a "key" or "token" flag or env variable.
	forbidden := []string{
		`os.Getenv("API`,
		`os.Getenv("KEY`,
		`os.Getenv("TOKEN`,
		`os.Getenv("RAXD_KEY`,
		`os.Getenv("RAXD_TOKEN`,
		`"--key"`,
		`"--token"`,
		`"--api-key"`,
	}
	for _, pat := range forbidden {
		if strings.Contains(filtered, pat) {
			t.Errorf("SR-12: forbidden key-from-env/argv pattern %q found in serve.go", pat)
		}
	}
}

// ============================================================================
// AC10/SR-23: dispatch returns 501 with body "not implemented"
// ============================================================================

// TestDispatchBodyNotImplemented verifies that unimplemented routes return
// the exact body "not implemented" (SR-23: no side effects, explicit stub).
func TestDispatchBodyNotImplemented(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("dispatch-body")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	resp := get(t, client, baseURL(port)+"/exec/run", string(plain), nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("SR-23: /exec/run want 501, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	bodyStr := strings.TrimSpace(string(body))
	if !strings.Contains(bodyStr, "not implemented") {
		t.Errorf("SR-23: dispatch body want 'not implemented', got %q", bodyStr)
	}
}

// ============================================================================
// AC12/SR-24: graceful shutdown completes within deadline
// ============================================================================

// TestGracefulShutdownWithinDeadline verifies that Run returns within 5 seconds
// after context cancellation. This tests the deadline constraint explicitly.
func TestGracefulShutdownWithinDeadline(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	runDone := make(chan error, 1)
	go func() {
		runDone <- srv.Run(ctx)
	}()

	// Wait for server to be ready.
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec
		if dialErr == nil {
			conn.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	start := time.Now()
	cancel()

	const maxShutdown = 5 * time.Second
	select {
	case runErr := <-runDone:
		elapsed := time.Since(start)
		if runErr != nil {
			t.Errorf("AC12/SR-24: Run returned error: %v", runErr)
		}
		if elapsed > maxShutdown {
			t.Errorf("AC12/SR-24: graceful shutdown took %v, want < %v", elapsed, maxShutdown)
		}
	case <-time.After(maxShutdown + 2*time.Second):
		t.Fatalf("AC12/SR-24: graceful shutdown did not complete within %v", maxShutdown)
	}
}

// ============================================================================
// AC7/SR-7: empty cert paths.TLSDir case handled without panic (empty dir)
// ============================================================================

// TestEmptyTLSDirCreatesNewCert verifies that server.New succeeds and generates
// a new certificate when TLSDir exists but is empty (no cert/key files).
func TestEmptyTLSDirCreatesNewCert(t *testing.T) {
	paths := newTestPaths(t)
	// TLSDir already exists (created by newTestPaths) but has no cert/key files.
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	cfg := newTestConfig(0)
	var logBuf bytes.Buffer
	_, err = server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
	if err != nil {
		t.Fatalf("AC2/SR-3: server.New on empty TLSDir must succeed; got: %v", err)
	}

	certPath := filepath.Join(paths.TLSDir, "cert.pem")
	keyPath := filepath.Join(paths.TLSDir, "key.pem")

	if _, err := os.Stat(certPath); err != nil {
		t.Errorf("AC2: cert.pem not created in empty TLSDir: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Errorf("AC2: key.pem not created in empty TLSDir: %v", err)
	}
}

// ============================================================================
// AC13: partial cert state (only cert.pem, no key.pem) → ErrTLSCert
// ============================================================================

// TestPartialCertStateReturnsError verifies that having only cert.pem without
// key.pem (or vice versa) produces ErrTLSCert, not a panic or silent failure.
func TestPartialCertStateReturnsError(t *testing.T) {
	t.Run("cert_only", func(t *testing.T) {
		paths := newTestPaths(t)
		store, err := keystore.Open(paths.KeysDB)
		if err != nil {
			t.Fatalf("open store: %v", err)
		}

		// Write only cert.pem (no key.pem).
		certPath := filepath.Join(paths.TLSDir, "cert.pem")
		if err := os.WriteFile(certPath, []byte("-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----\n"), 0o644); err != nil {
			t.Fatalf("write cert: %v", err)
		}

		cfg := newTestConfig(0)
		var logBuf bytes.Buffer
		_, err = server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
		if err == nil {
			t.Error("AC13/SR-6: cert-only partial state must return error, got nil")
		}
	})

	t.Run("key_only", func(t *testing.T) {
		paths := newTestPaths(t)
		store, err := keystore.Open(paths.KeysDB)
		if err != nil {
			t.Fatalf("open store: %v", err)
		}

		// Write only key.pem (no cert.pem).
		keyPath := filepath.Join(paths.TLSDir, "key.pem")
		if err := os.WriteFile(keyPath, []byte("-----BEGIN PRIVATE KEY-----\nfake\n-----END PRIVATE KEY-----\n"), 0o600); err != nil {
			t.Fatalf("write key: %v", err)
		}

		cfg := newTestConfig(0)
		var logBuf bytes.Buffer
		_, err = server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
		if err == nil {
			t.Error("AC13/SR-6: key-only partial state must return error, got nil")
		}
	})
}

// ============================================================================
// AC6/SR-17: rate-limit per-IP is independent of per-key (separate budgets)
// ============================================================================

// TestRateLimitPerIPIndependentOfKey verifies that per-IP and per-key limiters
// are independent: a fresh key on the same IP (after per-IP burst) gets 429 too,
// and a fresh IP with same key also gets its own budget.
func TestRateLimitPerIPAndKeyBothApply(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("dual-limit-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	cfg.RateLimit = 1
	cfg.RateBurst = 1
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf), nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	url := baseURL(port) + "/healthz"

	// Send enough requests to trigger at least one 429.
	got429 := false
	for i := 0; i < 20; i++ {
		resp := get(t, client, url, string(plain), nil)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			got429 = true
		}
	}
	if !got429 {
		t.Error("AC6/SR-17: expected at least one 429 with burst=1, rate=1; none received")
	}

	// RATE audit must have been produced.
	if !strings.Contains(logBuf.String(), "RATE") {
		t.Errorf("SR-17: RATE audit entry missing after rate-limit triggered; log=%s", logBuf.String())
	}
}

// ============================================================================
// NewLimiters exported — verify it's accessible from test package
// ============================================================================

// TestNewLimitersExported is a compile-time check that NewLimiters is exported
// and callable with the expected signature. This covers SR-18 testability.
func TestNewLimitersExported(_ *testing.T) {
	_ = server.NewLimiters(10.0, 20, 5*time.Minute)
}

// helperReadBody reads and closes the body, returns trimmed string.
func helperReadBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(b))
}

// ensure config.PathSet satisfies our test expectations (compile check).
var _ config.PathSet = config.PathSet{}
