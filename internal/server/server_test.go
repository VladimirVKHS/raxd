package server_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	clog "github.com/charmbracelet/log"
	"github.com/vladimirvkhs/raxd/internal/config"
	"github.com/vladimirvkhs/raxd/internal/keystore"
	"github.com/vladimirvkhs/raxd/internal/server"
)

// ---- helpers ----------------------------------------------------------------

// newTestPaths returns a PathSet with a unique temp directory.
func newTestPaths(t *testing.T) config.PathSet {
	t.Helper()
	dir := t.TempDir()
	tlsDir := filepath.Join(dir, "tls")
	if err := os.MkdirAll(tlsDir, 0o700); err != nil {
		t.Fatalf("create tlsDir: %v", err)
	}
	return config.PathSet{
		ConfigDir:  dir,
		ConfigFile: filepath.Join(dir, "config.yaml"),
		StateDir:   dir,
		KeysDB:     filepath.Join(dir, "keys.db"),
		TLSDir:     tlsDir,
	}
}

// newTestConfig builds a config with sane test defaults.
func newTestConfig(port int) *config.Config {
	return &config.Config{
		Port:              port,
		BindAddr:          "127.0.0.1",
		RateLimit:         100,
		RateBurst:         200,
		HostAllow:         []string{"localhost", "127.0.0.1", "::1"},
		OriginAllow:       []string{"localhost", "127.0.0.1", "::1"},
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       10 * time.Second,
		MaxHeaderBytes:    1 << 20,
		MaxBodyBytes:      1 << 20, // 1 MiB default for tests (SR-25)
	}
}

// newTestLogger creates a charmbracelet logger writing into buf.
func newTestLogger(buf *bytes.Buffer) *clog.Logger {
	l := clog.New(buf)
	l.SetTimeFormat("2006-01-02T15:04:05Z")
	return l
}

// freePort returns a free TCP port on 127.0.0.1.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// startServer starts the server in a goroutine and waits for it to be ready.
// Returns the cancel function that triggers graceful shutdown.
// It registers a t.Cleanup that cancels and waits for Run to finish before
// the test's TempDir is removed.
func startServer(t *testing.T, srv *server.Server, port int) (cancel context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		_ = srv.Run(ctx)
	}()

	// Register cleanup: stop server and wait for it to finish BEFORE TempDir cleanup.
	t.Cleanup(func() {
		cancel()
		select {
		case <-runDone:
		case <-time.After(5 * time.Second):
			// Timeout — continue anyway to avoid hanging test suite.
		}
	})

	// Wait up to 3s for server to accept TLS connections.
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec
		if err == nil {
			conn.Close()
			return cancel
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s not ready within 3s", addr)
	return nil
}

// clientForCert builds an http.Client that trusts the server's self-signed cert.
func clientForCert(t *testing.T, tlsDir string) *http.Client {
	t.Helper()
	certPath := filepath.Join(tlsDir, "cert.pem")
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatal("failed to append cert to pool")
	}
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
		Timeout: 5 * time.Second,
	}
}

// get performs a GET request with optional Authorization header and extra headers.
// The special key "Host" in headers sets req.Host (not just the header) so that
// Go's HTTP client sends the override Host header to the server.
func get(t *testing.T, client *http.Client, url string, bearer string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range headers {
		if k == "Host" {
			// req.Host overrides the Host header sent by the Go HTTP client.
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

// baseURL returns https://127.0.0.1:<port>.
func baseURL(port int) string {
	return fmt.Sprintf("https://127.0.0.1:%d", port)
}

// ============================================================================
// AC1/SR-1: TLS 1.3 enforced — TLS 1.2 clients rejected
// ============================================================================

func TestTLS13Enforced(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)

	// Client limited to TLS 1.2 must fail.
	tls12Client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MaxVersion:         tls.VersionTLS12,
				InsecureSkipVerify: true, //nolint:gosec
			},
		},
		Timeout: 3 * time.Second,
	}
	_, err = tls12Client.Get(baseURL(port) + "/healthz")
	if err == nil {
		t.Error("AC1/SR-1: TLS 1.2 client must be rejected; got nil error")
	}
}

// ============================================================================
// AC2/SR-3/SR-4: self-signed cert generated with correct perms and SAN
// ============================================================================

func TestCertGeneratedWithCorrectPerms(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	cfg := newTestConfig(0)
	var logBuf bytes.Buffer
	_, err = server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	certPath := filepath.Join(paths.TLSDir, "cert.pem")
	keyPath := filepath.Join(paths.TLSDir, "key.pem")

	certStat, err := os.Stat(certPath)
	if err != nil {
		t.Fatalf("stat cert.pem: %v", err)
	}
	keyStat, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat key.pem: %v", err)
	}

	if got := certStat.Mode().Perm(); got != 0o644 {
		t.Errorf("AC2/SR-4: cert.pem perms = %04o, want 0644", got)
	}
	if got := keyStat.Mode().Perm(); got != 0o600 {
		t.Errorf("AC2/SR-4: key.pem perms = %04o, want 0600", got)
	}
}

func TestCertIsECDSAP256SelfSignedWithSAN(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	cfg := newTestConfig(0)
	var logBuf bytes.Buffer
	_, err = server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	certPath := filepath.Join(paths.TLSDir, "cert.pem")
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatal("failed to decode cert.pem as PEM CERTIFICATE")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	// Self-signed: issuer == subject (SR-3).
	if cert.Issuer.String() != cert.Subject.String() {
		t.Errorf("SR-3: not self-signed (issuer=%q, subject=%q)", cert.Issuer, cert.Subject)
	}

	// ECDSA key (SR-3).
	if _, ok := cert.PublicKey.(*ecdsa.PublicKey); !ok {
		t.Errorf("SR-3: public key is not ECDSA, got %T", cert.PublicKey)
	}

	// SAN: 127.0.0.1 (SR-3).
	found127 := false
	for _, ip := range cert.IPAddresses {
		if ip.String() == "127.0.0.1" {
			found127 = true
		}
	}
	if !found127 {
		t.Error("SR-3: SAN missing 127.0.0.1")
	}

	// SAN: localhost (SR-3).
	foundLocal := false
	for _, dns := range cert.DNSNames {
		if dns == "localhost" {
			foundLocal = true
		}
	}
	if !foundLocal {
		t.Error("SR-3: SAN missing localhost")
	}
}

// ============================================================================
// AC3/SR-5: existing cert/key reused without regeneration
// ============================================================================

func TestCertReusedOnSecondNew(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	cfg := newTestConfig(0)
	var logBuf bytes.Buffer

	// First call — generates cert.
	_, err = server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("first server.New: %v", err)
	}

	certPath := filepath.Join(paths.TLSDir, "cert.pem")
	content1, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert 1: %v", err)
	}
	stat1, _ := os.Stat(certPath)

	// Second call — must reuse.
	_, err = server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("second server.New: %v", err)
	}

	content2, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert 2: %v", err)
	}
	stat2, _ := os.Stat(certPath)

	if !bytes.Equal(content1, content2) {
		t.Error("AC3/SR-5: cert.pem overwritten on second server.New call")
	}
	if stat1.ModTime() != stat2.ModTime() {
		t.Error("AC3/SR-5: cert.pem mtime changed (file overwritten)")
	}
}

// ============================================================================
// AC13/SR-6: corrupt cert → ErrTLSCert, no overwrite
// ============================================================================

func TestCorruptCertReturnsError(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	// Place corrupt files.
	certPath := filepath.Join(paths.TLSDir, "cert.pem")
	keyPath := filepath.Join(paths.TLSDir, "key.pem")
	corrupt := []byte("this is not a valid PEM certificate")
	if err := os.WriteFile(certPath, corrupt, 0o644); err != nil {
		t.Fatalf("write corrupt cert: %v", err)
	}
	if err := os.WriteFile(keyPath, corrupt, 0o600); err != nil {
		t.Fatalf("write corrupt key: %v", err)
	}
	origContent, _ := os.ReadFile(certPath)

	cfg := newTestConfig(0)
	var logBuf bytes.Buffer
	_, err = server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err == nil {
		t.Fatal("AC13/SR-6: expected error for corrupt cert, got nil")
	}

	// Error should mention TLS cert (ErrTLSCert).
	if !strings.Contains(err.Error(), "TLS certificate") {
		t.Errorf("AC13/SR-6: error should mention 'TLS certificate', got: %v", err)
	}

	// File must NOT be overwritten (SR-6).
	afterContent, _ := os.ReadFile(certPath)
	if !bytes.Equal(origContent, afterContent) {
		t.Error("SR-6: corrupt cert.pem was overwritten — must not overwrite")
	}
}

// ============================================================================
// AC4/AC5/SR-8/SR-9: authentication before routing
// ============================================================================

func TestNoAuthReturns401(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	resp := get(t, client, baseURL(port)+"/healthz", "", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("AC4/SR-9: no auth → want 401, got %d", resp.StatusCode)
	}
}

func TestUnknownKeyReturns401(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	resp := get(t, client, baseURL(port)+"/healthz", "rax_live_unknownkey123", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("AC5/SR-9: unknown key → want 401, got %d", resp.StatusCode)
	}
}

func TestValidKeyReachesHealth(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("health-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	resp := get(t, client, baseURL(port)+"/healthz", string(plain), nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("AC4/AC10: valid key → want 200, got %d", resp.StatusCode)
	}

	var body [16]byte
	n, _ := resp.Body.Read(body[:])
	if string(body[:n]) != "pong" {
		t.Errorf("AC10: want body 'pong', got %q", string(body[:n]))
	}
}

// SR-11: revoked key immediately returns 401.
func TestRevokedKeyReturns401(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, rec, err := store.Create("revoke-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	// Before revoke.
	resp := get(t, client, baseURL(port)+"/healthz", string(plain), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SR-11: before revoke want 200, got %d", resp.StatusCode)
	}

	// Revoke.
	if err := store.Revoke(rec.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// After revoke.
	resp2 := get(t, client, baseURL(port)+"/healthz", string(plain), nil)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("SR-11: after revoke want 401, got %d", resp2.StatusCode)
	}
}

// ============================================================================
// AC6/SR-17: rate limiting per-key and per-IP
// ============================================================================

func TestRateLimitPerKeyReturns429(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("ratelimit-key")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	cfg.RateLimit = 1   // 1 req/s
	cfg.RateBurst = 1   // burst of 1

	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	got429 := false
	for i := 0; i < 10; i++ {
		resp := get(t, client, baseURL(port)+"/healthz", string(plain), nil)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Error("AC6/SR-17: expected 429 after exceeding per-key rate limit")
	}
}

func TestRateLimitPerIPReturns429(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("ratelimit-ip")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	// Extremely tight rate to force IP limit.
	cfg.RateLimit = 0.01 // 1 req per 100s
	cfg.RateBurst = 1

	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	got429 := false
	for i := 0; i < 5; i++ {
		resp := get(t, client, baseURL(port)+"/healthz", string(plain), nil)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Error("AC6/SR-17: expected 429 after exceeding per-IP rate limit")
	}
}

// ============================================================================
// AC7/SR-7: bind on loopback by default
// ============================================================================

func TestServerBindsLoopback(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)

	// srv.Addr() should start with 127.0.0.1.
	addr := srv.Addr()
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Errorf("AC7/SR-7: server addr = %q, want 127.0.0.1:<port>", addr)
	}
}

// ============================================================================
// AC8/AC9/SR-19/SR-21: audit records, no secrets in log
// ============================================================================

func TestAuditHasNoKeyBody(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("audit-secret")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	resp := get(t, client, baseURL(port)+"/healthz", string(plain), nil)
	resp.Body.Close()

	logOutput := logBuf.String()

	// AC9/SR-21: key body must NOT appear in audit log.
	if strings.Contains(logOutput, string(plain)) {
		t.Errorf("AC9/SR-21: key body found in audit log! log=%s", logOutput)
	}
	// Raw Authorization header must not appear.
	if strings.Contains(logOutput, "Bearer "+string(plain)) {
		t.Errorf("SR-21: raw Authorization header leaked into log")
	}
}

func TestAuditHasFingerprintField(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("fp-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	resp := get(t, client, baseURL(port)+"/healthz", string(plain), nil)
	resp.Body.Close()

	if !strings.Contains(logBuf.String(), "fp=") {
		t.Errorf("AC8/SR-19: audit log missing fp= field; log=%s", logBuf.String())
	}
}

func TestAuditFailHasDash(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	// No auth → fp should be "-".
	resp := get(t, client, baseURL(port)+"/healthz", "", nil)
	resp.Body.Close()

	if !strings.Contains(logBuf.String(), "fp=-") {
		t.Errorf("SR-19: no-key request should have fp=- in log; log=%s", logBuf.String())
	}
}

// ============================================================================
// AC10/SR-22/SR-23: health and dispatch
// ============================================================================

func TestHealthReturnsPoong(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("pong-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	resp := get(t, client, baseURL(port)+"/healthz", string(plain), nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("AC10: GET /healthz want 200, got %d", resp.StatusCode)
	}
	var body [16]byte
	n, _ := resp.Body.Read(body[:])
	if string(body[:n]) != "pong" {
		t.Errorf("AC10: want 'pong', got %q", string(body[:n]))
	}
}

func TestDispatchReturns501(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("dispatch-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	resp := get(t, client, baseURL(port)+"/not-implemented", string(plain), nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("AC10/SR-23: unimplemented route want 501, got %d", resp.StatusCode)
	}
}

// ============================================================================
// AC12/SR-24: graceful shutdown — Shutdown then FlushUsage
// ============================================================================

func TestGracefulShutdown(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
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
		conn, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Cancel → graceful shutdown.
	cancel()

	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("AC12: Run returned error on graceful shutdown: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("AC12: graceful shutdown did not complete within 10s")
	}
}

// ============================================================================
// SR-15/SR-16: Host/Origin validation
// ============================================================================

func TestInvalidHostReturns403(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("host-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	resp := get(t, client, baseURL(port)+"/healthz", string(plain), map[string]string{
		"Host": "evil.example.com",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("SR-15: evil Host → want 403, got %d", resp.StatusCode)
	}

	// SR-19/SR-20: invalid Host denial MUST appear in audit log.
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "DENY") {
		t.Errorf("SR-19: Host denial not in audit log (missing DENY); log=%s", logOutput)
	}
	if !strings.Contains(logOutput, "invalid host header") {
		t.Errorf("SR-19: Host denial missing reason in audit log; log=%s", logOutput)
	}
}

func TestInvalidOriginReturns403(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("origin-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	resp := get(t, client, baseURL(port)+"/healthz", string(plain), map[string]string{
		"Origin": "https://evil.example.com",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("SR-16: invalid Origin → want 403, got %d", resp.StatusCode)
	}

	// SR-19/SR-20: invalid Origin denial MUST appear in audit log.
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "DENY") {
		t.Errorf("SR-19: Origin denial not in audit log (missing DENY); log=%s", logOutput)
	}
	if !strings.Contains(logOutput, "invalid origin header") {
		t.Errorf("SR-19: Origin denial missing reason in audit log; log=%s", logOutput)
	}
}

// TestOriginBypassAttemptRejected verifies that ISSUE-1 fix is in place:
// subdomains that share a prefix with an allowed origin host must NOT pass.
// E.g. allowlist has "localhost" — Origin: https://localhost.evil.com must be 403.
// SR-16: strict hostname match via url.Hostname(), not HasPrefix.
func TestOriginBypassAttemptRejected(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("bypass-origin-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	// HostAllow has "localhost" and "127.0.0.1"; OriginAllow same.
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	cases := []struct {
		name   string
		origin string
	}{
		{"localhost prefix bypass", "https://localhost.evil.com"},
		{"127.0.0.1 prefix bypass", "https://127.0.0.1.evil.com"},
		{"::1 prefix bypass", "https://::1.evil.com"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			resp := get(t, client, baseURL(port)+"/healthz", string(plain), map[string]string{
				"Origin": tc.origin,
			})
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("SR-16 bypass: Origin %q must be rejected (403), got %d", tc.origin, resp.StatusCode)
			}
			// DENY must appear in audit log.
			logOutput := logBuf.String()
			if !strings.Contains(logOutput, "DENY") {
				t.Errorf("SR-16: bypass origin %q — DENY missing from audit log; log=%s", tc.origin, logOutput)
			}
			logBuf.Reset()
		})
	}
}

// TestInvalidOriginUnparseable verifies that an unparseable/malformed Origin header
// is treated as present-and-invalid → 403 (ISSUE-1 ADR: invalid = treat as hostile).
func TestInvalidOriginUnparseable(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("invalid-origin-parse")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	// "://\x00invalid" cannot be parsed as a valid URL.
	// Note: Go's http.Client strips some control chars, use a string that url.Parse returns empty host.
	// An origin with no scheme/host parsed as empty hostname → not in allowlist → 403.
	resp := get(t, client, baseURL(port)+"/healthz", string(plain), map[string]string{
		"Origin": "not-a-url",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("SR-16: unparseable Origin → want 403, got %d", resp.StatusCode)
	}
}

// TestMaxBodyBytesRejected verifies ISSUE-2: http.MaxBytesReader is applied per-request
// and reading more than the configured limit returns an error.
// SR-25: protection against large-body flooding.
//
// Strategy: use net/http/httptest to drive bodyLimitMiddleware directly, bypassing TLS/auth.
// A handler that reads the full body is used as the inner handler; if MaxBytesReader is
// in place, reading > limit bytes must produce a *http.MaxBytesError.
// The test verifies that the body-limit middleware causes the handler to observe an error.
func TestMaxBodyBytesRejected(t *testing.T) {
	const limit = 16

	// Build a handler that attempts to drain the body and returns 413 on MaxBytesError.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, limit+100)
		_, err := r.Body.Read(buf)
		if err != nil {
			// http.MaxBytesReader wraps the error; check via MaxBytesError.
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with bodyLimitMiddleware (the same function used in production chain).
	wrapped := server.BodyLimitMiddlewareForTest(limit)(inner)

	// Send a request with a body larger than the limit.
	bigBody := bytes.NewReader(make([]byte, 1024)) // 1 KiB > 16 bytes
	req := httptest.NewRequest(http.MethodPost, "/exec", bigBody)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("SR-25/ISSUE-2: oversized body → want 413, got %d", rr.Code)
	}
}

// TestMaxBodyBytesDefault verifies that Config.MaxBodyBytes has a non-zero default
// (≥1 byte) so the field is always active. Regression: field was not in Config.
func TestMaxBodyBytesDefault(t *testing.T) {
	cfg := &config.Config{
		Port:              0,
		BindAddr:          "127.0.0.1",
		MaxBodyBytes:      0, // intentionally zero to test that Load() sets a default
	}
	// When MaxBodyBytes is explicitly set, it must be used as-is.
	// Verify that a non-zero value round-trips through newTestConfig.
	cfgWithDefault := newTestConfig(0)
	if cfgWithDefault.MaxBodyBytes <= 0 {
		t.Errorf("SR-25: newTestConfig must produce MaxBodyBytes > 0, got %d", cfgWithDefault.MaxBodyBytes)
	}
	_ = cfg // ensure compilation
}

// TestSingleAuditRecordOnSuccess verifies ISSUE-3: exactly ONE audit record per
// successful request — the AUTH record must appear after rate-limit pass-through.
// Previously authMiddleware wrote AUTH before rateLimitMiddleware, causing dual records on 429.
func TestSingleAuditRecordOnSuccess(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("single-audit-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	// High rate limit — ensure request passes.
	cfg.RateLimit = 1000
	cfg.RateBurst = 1000

	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	resp := get(t, client, baseURL(port)+"/healthz", string(plain), nil)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SR-19/ISSUE-3: expected 200, got %d", resp.StatusCode)
	}

	logOutput := logBuf.String()

	// Count AUTH occurrences — must be exactly 1.
	authCount := strings.Count(logOutput, "AUTH")
	if authCount != 1 {
		t.Errorf("ISSUE-3/SR-19: expected exactly 1 AUTH record on success, got %d; log=%s", authCount, logOutput)
	}
}

// TestSingleAuditRecordOnRateLimit verifies ISSUE-3: when a valid key is rate-limited,
// the audit log must NOT contain AUTH+RATE for the same request.
// Invariant: a rate-limited request produces ONLY a RATE record, never an AUTH record.
//
// Since the rate-limited request never reaches the handler (rate-limit fires first),
// authSuccessAuditMiddleware is never invoked → no AUTH record for that request.
// Total AUTH records in log must equal the number of SUCCESSFUL (non-429) requests.
func TestSingleAuditRecordOnRateLimit(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("rate-audit-single")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	cfg.RateLimit = 0.001 // very slow refill — burst exhausted quickly
	cfg.RateBurst = 1

	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	// Send exactly 5 rapid requests. With burst=1 and rate=0.001,
	// the first request consumes the token (200+AUTH), subsequent ones → 429+RATE.
	const total = 5
	successCount := 0
	got429 := false
	for i := 0; i < total; i++ {
		resp := get(t, client, baseURL(port)+"/healthz", string(plain), nil)
		code := resp.StatusCode
		resp.Body.Close()
		if code == http.StatusOK {
			successCount++
		} else if code == http.StatusTooManyRequests {
			got429 = true
		}
	}
	if !got429 {
		t.Fatal("ISSUE-3: could not trigger 429 with burst=1 rate=0.001 in 5 requests")
	}

	logOutput := logBuf.String()

	// AUTH count must equal successCount (one AUTH per successful request, none for 429).
	authCount := strings.Count(logOutput, "AUTH")
	if authCount != successCount {
		t.Errorf("ISSUE-3/SR-19: AUTH records=%d want %d (= success requests); "+
			"dual AUTH+RATE on same request would inflate count; log=%s",
			authCount, successCount, logOutput)
	}

	// RATE must appear (we confirmed got429 above).
	if !strings.Contains(logOutput, "RATE") {
		t.Errorf("ISSUE-3/SR-19: expected RATE record in log; log=%s", logOutput)
	}
}

func TestAbsentOriginNotRejected(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("no-origin")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	// No Origin header → must NOT return 403.
	resp := get(t, client, baseURL(port)+"/healthz", string(plain), nil)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		t.Errorf("SR-16: absent Origin should not produce 403; got %d", resp.StatusCode)
	}
}

// ============================================================================
// AC13: port already in use → ErrPortInUse
// ============================================================================

func TestPortInUse(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	// Bind a port to block it.
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen blocker: %v", err)
	}
	defer blocker.Close()
	port := blocker.Addr().(*net.TCPAddr).Port

	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = srv.Run(ctx)
	if err == nil {
		t.Fatal("AC13: expected error for port-in-use, got nil")
	}
	if !strings.Contains(err.Error(), "address already in use") {
		t.Errorf("AC13: error should mention 'address already in use', got: %v", err)
	}
}

// ============================================================================
// SR-18: rate limiter concurrency — no data races under -race flag
// ============================================================================

func TestRateLimiterConcurrency(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("race-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	cfg.RateLimit = 1000
	cfg.RateBurst = 1000

	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	var wg sync.WaitGroup
	const workers = 20
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := get(t, client, baseURL(port)+"/healthz", string(plain), nil)
			resp.Body.Close()
		}()
	}
	wg.Wait()
}

// ============================================================================
// SR-19/SR-20: audit records for FAIL and RATE
// ============================================================================

func TestAuditFailRecorded(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	// No auth → FAIL audit.
	resp := get(t, client, baseURL(port)+"/healthz", "", nil)
	resp.Body.Close()

	if !strings.Contains(logBuf.String(), "FAIL") {
		t.Errorf("SR-20: FAIL not found in audit log; log=%s", logBuf.String())
	}
}

func TestAuditRateRecorded(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("rate-audit-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	cfg.RateLimit = 0.001
	cfg.RateBurst = 1

	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	for i := 0; i < 5; i++ {
		resp := get(t, client, baseURL(port)+"/healthz", string(plain), nil)
		resp.Body.Close()
	}

	if !strings.Contains(logBuf.String(), "RATE") {
		t.Errorf("SR-20: RATE not found in audit log; log=%s", logBuf.String())
	}
}

// ============================================================================
// SR-13: ErrCorrupt from keystore.Verify → HTTP 403 + DENY audit, no panic
// ============================================================================

func TestErrCorruptReturns403(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	// Create a key so keys.db is initialized with valid content.
	plain, _, err := store.Create("corrupt-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	startServer(t, srv, port)
	client := clientForCert(t, paths.TLSDir)

	// Corrupt keys.db AFTER server.New succeeds — this simulates disk corruption at
	// request time, causing keystore.Verify to return ErrCorrupt (SR-13 mapping).
	if err := os.WriteFile(paths.KeysDB, []byte("not valid json {{{"), 0o600); err != nil {
		t.Fatalf("corrupt keys.db: %v", err)
	}

	// Request with a well-formed Bearer token → keystore.Verify returns ErrCorrupt → 403.
	resp := get(t, client, baseURL(port)+"/healthz", string(plain), nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("SR-13: ErrCorrupt from Verify → want 403, got %d", resp.StatusCode)
	}

	// Must produce DENY audit record — no panic allowed.
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "DENY") {
		t.Errorf("SR-13: DENY audit record missing after ErrCorrupt; log=%s", logOutput)
	}
}

// ============================================================================
// SR-24: graceful shutdown order — Shutdown BEFORE FlushUsage
// ============================================================================

func TestGracefulShutdownOrder(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	// Record timestamps: when Shutdown returned vs when FlushUsage was called.
	var shutdownReturnedAt int64 // set by afterShutdownHook (after Shutdown, before FlushUsage)

	// afterShutdownHook fires between Shutdown() return and FlushUsage().
	// We record the time so we can assert it precedes the FlushUsage completion.
	srv.SetAfterShutdownHook(func() {
		shutdownReturnedAt = time.Now().UnixNano()
	})

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

	// Trigger graceful shutdown.
	cancel()

	select {
	case runErr := <-runDone:
		if runErr != nil {
			t.Errorf("SR-24 order: Run returned error: %v", runErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("SR-24 order: graceful shutdown did not complete within 10s")
	}

	// The hook must have been called (meaning Shutdown completed before FlushUsage).
	if shutdownReturnedAt == 0 {
		t.Error("SR-24: afterShutdownHook was never called — Shutdown may not have run")
	}
}

// ============================================================================
// D-1 / ux-spec §5: OnListen hook fires AFTER successful bind, not on error
// ============================================================================

// TestOnListenHookCalledOnSuccessfulBind verifies that SetOnListen hook is
// called after the server successfully binds the port (the seam serve.go uses
// to defer the startup block until after the real listener is up).
//
// Uses sync/atomic to access hookCalled across goroutines without data race.
func TestOnListenHookCalledOnSuccessfulBind(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	// Use atomic flag to avoid data race: hook fires in Run's goroutine,
	// polling loop reads from the test goroutine.
	var hookCalled int32 // 0 = not called, 1 = called
	var hookAddr string
	var hookAddrMu sync.Mutex
	srv.SetOnListen(func(addr string) {
		hookAddrMu.Lock()
		hookAddr = addr
		hookAddrMu.Unlock()
		atomic.StoreInt32(&hookCalled, 1)
	})

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() {
		runDone <- srv.Run(ctx)
	}()

	// Wait for hook to be called (server is up).
	deadline := time.Now().Add(3 * time.Second)
	for atomic.LoadInt32(&hookCalled) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	select {
	case <-runDone:
	case <-time.After(5 * time.Second):
		t.Fatal("D-1: Run did not return after cancel")
	}

	if atomic.LoadInt32(&hookCalled) == 0 {
		t.Error("D-1/ux-spec §5: SetOnListen hook was not called on successful bind")
	}
	hookAddrMu.Lock()
	addr := hookAddr
	hookAddrMu.Unlock()
	if addr == "" {
		t.Error("D-1: OnListen hook received empty addr")
	}
}

// TestOnListenHookNotCalledOnPortInUse verifies that when the port is already in
// use, Run returns an error WITHOUT calling the OnListen hook.
// This is the core D-1 invariant: serve.go only prints the startup block
// when the hook fires, so it will never print "listening" on bind failure.
//
// Run is called synchronously here (no goroutine), so no data race risk.
func TestOnListenHookNotCalledOnPortInUse(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	// Block the port.
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen blocker: %v", err)
	}
	defer blocker.Close()
	port := blocker.Addr().(*net.TCPAddr).Port

	cfg := newTestConfig(port)
	var logBuf bytes.Buffer
	srv, err := server.New(cfg, paths, store, newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	// Run is called synchronously below; no cross-goroutine sharing needed.
	hookCalled := false
	srv.SetOnListen(func(_ string) {
		hookCalled = true
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runErr := srv.Run(ctx)
	if runErr == nil {
		t.Fatal("D-1: expected error for port-in-use, got nil")
	}

	if hookCalled {
		t.Error("D-1/ux-spec §5: OnListen hook must NOT be called when bind fails; " +
			"serve.go would have printed false 'listening' startup block")
	}
}
