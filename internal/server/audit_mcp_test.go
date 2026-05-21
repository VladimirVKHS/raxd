package server_test

// audit_mcp_test.go — tests for SR-35/SR-36: AuditRecord.Tool field and
// writeAudit MCP-success label.
//
// RED phase: these tests fail until AuditRecord gains a Tool field and
// writeAudit emits "MCP ... tool=<name>" for MCP-success records.

import (
	"bytes"
	"context"
	"strings"
	"testing"

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
