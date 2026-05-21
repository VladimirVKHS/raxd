package server_test

// audit_mcp_test.go — tests for SR-35/SR-36: AuditRecord.Tool field and
// writeAudit MCP-success label.
//
// RED phase: these tests fail until AuditRecord gains a Tool field and
// writeAudit emits "MCP ... tool=<name>" for MCP-success records.

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/config"
	"github.com/vladimirvkhs/raxd/internal/keystore"
	"github.com/vladimirvkhs/raxd/internal/server"
)

// TestAuditRecordToolField verifies that AuditRecord has a Tool field
// (SR-36: "добавить поле Tool string").
// This is a compile-time check via struct literal.
func TestAuditRecordToolField(t *testing.T) {
	rec := server.AuditRecord{
		Fingerprint: "abc123def456",
		RemoteAddr:  "127.0.0.1:12345",
		Result:      "success",
		Tool:        "ping",
	}
	if rec.Tool != "ping" {
		t.Errorf("SR-36: AuditRecord.Tool = %q, want %q", rec.Tool, "ping")
	}
}

// TestWriteAuditMCPSuccessLabel verifies that writeAudit (via server.WriteAuditForTest)
// emits "MCP" msg-label and "tool=ping" for a success record with Tool set.
// SR-36: msg-label for MCP-success is "MCP", tool= logged when Tool != "".
func TestWriteAuditMCPSuccessLabel(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	auditFn := server.NewAuditFnForTest(logger)
	auditFn(server.AuditRecord{
		Fingerprint: "abc123def456",
		RemoteAddr:  "127.0.0.1:12345",
		Result:      "success",
		Tool:        "ping",
	})

	log := buf.String()
	if !strings.Contains(log, "MCP") {
		t.Errorf("SR-36: MCP-success record must have msg-label MCP; log=%s", log)
	}
	if !strings.Contains(log, "tool=ping") {
		t.Errorf("SR-36: MCP-success record must have tool=ping; log=%s", log)
	}
	if !strings.Contains(log, "fp=abc123def456") {
		t.Errorf("SR-36: MCP-success record must have fp= field; log=%s", log)
	}
}

// TestWriteAuditNonMCPUnchanged verifies that non-MCP (Tool=="") success records
// still emit "AUTH" label and no "tool=" field — backward compatibility.
// SR-36: "для существующих connection-записей tool= не добавляется".
func TestWriteAuditNonMCPUnchanged(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	auditFn := server.NewAuditFnForTest(logger)
	auditFn(server.AuditRecord{
		Fingerprint: "abc123def456",
		RemoteAddr:  "127.0.0.1:12345",
		Result:      "success",
		Tool:        "", // not MCP
	})

	log := buf.String()
	if !strings.Contains(log, "AUTH") {
		t.Errorf("SR-36: non-MCP success must still emit AUTH; log=%s", log)
	}
	if strings.Contains(log, "tool=") {
		t.Errorf("SR-36: non-MCP success must NOT emit tool= field; log=%s", log)
	}
}

// TestFingerprintFromContext verifies that FingerprintFromContext is exported
// and returns "-" when no fingerprint is in context.
// This is required by mcp.withAudit to retrieve the fingerprint from ctx (SR-35).
func TestFingerprintFromContext(t *testing.T) {
	ctx := context.Background()
	fp := server.FingerprintFromContext(ctx)
	if fp != "-" {
		t.Errorf("FingerprintFromContext on empty ctx: want \"-\", got %q", fp)
	}
}

// TestRemoteAddrFromContextEmpty verifies that RemoteAddrFromContext returns "-"
// when no remote address has been stored in context.
// Symmetric to FingerprintFromContext (AC9/SR-35: remote must be in MCP audit records).
func TestRemoteAddrFromContextEmpty(t *testing.T) {
	ctx := context.Background()
	remote := server.RemoteAddrFromContext(ctx)
	if remote != "-" {
		t.Errorf("RemoteAddrFromContext on empty ctx: want \"-\", got %q", remote)
	}
}

// TestRemoteAddrFromContextSet verifies that RemoteAddrFromContext returns the real
// remote address when authMiddleware has stored it in context.
// This is the symmetric mechanism to FingerprintFromContext (AC9/SR-35).
//
// Strategy: drive authMiddleware directly via httptest with a real key,
// then verify RemoteAddrFromContext returns the client address from the request,
// not "-". The address must contain a colon (IP:port format).
func TestRemoteAddrFromContextSet(t *testing.T) {
	dir := t.TempDir()
	tlsDir := t.TempDir()
	paths := config.PathSet{
		ConfigDir:  dir,
		ConfigFile: dir + "/config.yaml",
		StateDir:   dir,
		KeysDB:     dir + "/keys.db",
		TLSDir:     tlsDir,
	}
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("remote-ctx-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	// Build an inner handler that captures RemoteAddrFromContext.
	var capturedRemote string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRemote = server.RemoteAddrFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with authMiddleware.
	var auditBuf bytes.Buffer
	logger := newTestLogger(&auditBuf)
	auditFn := server.NewAuditFnForTest(logger)
	handler := server.AuthMiddlewareForTest(store, auditFn)(inner)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Authorization", "Bearer "+string(plain))
	// httptest.NewRequest sets RemoteAddr to "192.0.2.1:1234" by default.

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("TestRemoteAddrFromContextSet: want 200, got %d", rr.Code)
	}
	if capturedRemote == "-" || capturedRemote == "" {
		t.Errorf(
			"AC9/SR-35: RemoteAddrFromContext returned %q after authMiddleware — "+
				"remote address was not stored in context. "+
				"MCP audit records would show remote=- for all authenticated requests.",
			capturedRemote,
		)
	}
	// Must contain a colon (IP:port format, same as r.RemoteAddr).
	if !strings.Contains(capturedRemote, ":") {
		t.Errorf(
			"AC9/SR-35: RemoteAddrFromContext returned %q — expected IP:port format (contains ':')",
			capturedRemote,
		)
	}
}
