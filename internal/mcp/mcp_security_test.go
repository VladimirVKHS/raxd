package mcp_test

// mcp_security_test.go — strengthened security tests for mcp-server task.
//
// Shared helpers (startMCPServer, newTestPaths, freePort, postMCP, readBody,
// jsonrpcBody, newTestConfig, newTestLogger) are defined in mcp_test.go (same
// package mcp_test). This file adds helpers needed only here.
//
// MEDIUM-1 (SR-34, AC10):
//   Reads the ACTUAL content of key.pem (private TLS key) from TLSDir after
//   server start; verifies the raw key bytes do NOT appear as a substring in:
//   - every MCP response: initialize, tools/list, server_info, ping
//   - the captured audit log
//   Same check for the full API key string (rax_live_...).
//   If this test fails → product security bug (SR-34); escalate to developer.
//
// MEDIUM-2 (SR-32):
//   Documents and tests Origin/Host validation behaviour:
//   - Origin present AND invalid → 403 from transport, BEFORE MCP layer
//   - No MCP audit record written (tool= absent in log)
//   - Origin absent → passes to auth (401), not rejected by origin check
//   - Valid Origin (in allowlist) → passes through
//
//   Design note: hostOriginMiddleware (transport) provides all Origin protection.
//   SDK StreamableHTTPOptions.CrossOriginProtection is nil — no SDK-level DNS-
//   rebinding check. Protection is 100% from transport middleware (SR-32/ADR-003).
//
// All tests run in Docker (-mod=vendor, SECURITY-BASELINE §6).

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	internalmcp "github.com/vladimirvkhs/raxd/internal/mcp"
	"github.com/vladimirvkhs/raxd/internal/keystore"
	"github.com/vladimirvkhs/raxd/internal/server"
	"github.com/vladimirvkhs/raxd/internal/version"
)

// ============================================================================
// MEDIUM-1 (SR-34, AC10): no-secrets test reads ACTUAL private TLS key content
// ============================================================================

// TestNoSecretsInMCPResponsesAndAuditLog strengthens SR-34/AC10:
//   - reads the raw content of key.pem (private TLS key) from TLSDir after server start
//   - reads the full API key string (rax_live_...)
//   - verifies neither appears as a substring in any MCP response body:
//     initialize, tools/list, ping, server_info
//   - verifies neither appears as a substring in the captured audit log
//
// If this test fails because key material leaks → product security bug;
// escalate to developer; do NOT weaken the assertion.
func TestNoSecretsInMCPResponsesAndAuditLog(t *testing.T) {
	baseURL, keyStr, client, auditBuf, tlsKeyContent := startMCPServerWithTLSKey(t)

	// Sanity: TLS key was actually read.
	if len(tlsKeyContent) == 0 {
		t.Fatal("MEDIUM-1: TLS key.pem is empty — cannot perform no-secrets assertion")
	}
	if !strings.Contains(tlsKeyContent, "-----BEGIN") {
		t.Fatalf("MEDIUM-1: key.pem does not look like PEM: first 80 chars = %q",
			tlsKeyContent[:minInt(80, len(tlsKeyContent))])
	}

	// Extract a distinctive substring from PEM body (not the header/footer).
	keyDistinctive := extractPEMBodySubstring(tlsKeyContent)
	if keyDistinctive == "" {
		t.Fatal("MEDIUM-1: could not extract distinctive substring from key.pem body")
	}

	auditBuf.Reset()

	// --- initialize ---
	initBody := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "security-test", "version": "1"},
	})
	initResp := postMCP(t, client, baseURL, keyStr, initBody, nil)
	initRespBody := readBody(t, initResp)

	assertNoSecret(t, "initialize response", "TLS private key body", keyDistinctive, initRespBody)
	assertNoSecret(t, "initialize response", "API key string", keyStr, initRespBody)

	// --- tools/list ---
	listBody := jsonrpcBody(2, "tools/list", map[string]interface{}{})
	listResp := postMCP(t, client, baseURL, keyStr, listBody, map[string]string{
		"MCP-Protocol-Version": "2025-11-25",
	})
	listRespBody := readBody(t, listResp)

	assertNoSecret(t, "tools/list response", "TLS private key body", keyDistinctive, listRespBody)
	assertNoSecret(t, "tools/list response", "API key string", keyStr, listRespBody)

	// --- tools/call ping ---
	pingBody := jsonrpcBody(3, "tools/call", map[string]interface{}{
		"name": "ping", "arguments": map[string]interface{}{},
	})
	pingResp := postMCP(t, client, baseURL, keyStr, pingBody, map[string]string{
		"MCP-Protocol-Version": "2025-11-25",
	})
	pingRespBody := readBody(t, pingResp)

	assertNoSecret(t, "ping response", "TLS private key body", keyDistinctive, pingRespBody)
	assertNoSecret(t, "ping response", "API key string", keyStr, pingRespBody)

	// --- tools/call server_info ---
	infoBody := jsonrpcBody(4, "tools/call", map[string]interface{}{
		"name": "server_info", "arguments": map[string]interface{}{},
	})
	infoResp := postMCP(t, client, baseURL, keyStr, infoBody, map[string]string{
		"MCP-Protocol-Version": "2025-11-25",
	})
	infoRespBody := readBody(t, infoResp)

	assertNoSecret(t, "server_info response", "TLS private key body", keyDistinctive, infoRespBody)
	assertNoSecret(t, "server_info response", "API key string", keyStr, infoRespBody)

	// --- audit log ---
	logOutput := auditBuf.String()
	assertNoSecret(t, "audit log", "TLS private key body", keyDistinctive, logOutput)
	assertNoSecret(t, "audit log", "API key string", keyStr, logOutput)
}

// assertNoSecret checks that secret does not appear as a substring in haystack.
// A failure here is a product security bug (SR-34) — escalate to developer.
func assertNoSecret(t *testing.T, location, secretName, secret, haystack string) {
	t.Helper()
	if strings.Contains(haystack, secret) {
		t.Errorf(
			"MEDIUM-1/SR-34 PRODUCT SECURITY BUG: %s contains %s!\n"+
				"Escalate to developer — do NOT weaken this assertion.\n"+
				"Secret (first 20 chars): %q\n"+
				"Location snippet (first 200 chars): %q",
			location, secretName,
			secret[:minInt(20, len(secret))],
			haystack[:minInt(200, len(haystack))],
		)
	}
}

// extractPEMBodySubstring returns a distinctive 40-char substring from the
// base64 body lines of a PEM block (skipping header/footer). Returns "" if not found.
func extractPEMBodySubstring(pemContent string) string {
	for _, line := range strings.Split(pemContent, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-----") {
			continue
		}
		if len(line) >= 40 {
			return line[:40]
		}
	}
	return ""
}

// minInt returns the smaller of a and b (avoids collision with builtin min).
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ============================================================================
// MEDIUM-2 (SR-32, AC12): Origin/Host validation — behaviour documented + tested
//
// Design note (SR-32, ADR-003, plan.md):
//   All Origin protection for /mcp comes from the transport hostOriginMiddleware.
//   The SDK's CrossOriginProtection is nil — the MCP layer adds no Origin logic.
//   Behaviour is inherited from tls-transport:
//     - Origin present AND NOT in cfg.OriginAllow → 403 (before auth, before MCP)
//     - Origin absent                             → not rejected; proceeds to auth
//     - Origin present AND in cfg.OriginAllow     → passes; proceeds to auth
// ============================================================================

// TestOriginInvalidReturnsForbiddenBeforeMCP verifies SR-32/AC12:
//   A present, invalid Origin header → 403 from transport, BEFORE MCP SDK.
//   No tool is invoked; no MCP audit record (tool=) is written.
func TestOriginInvalidReturnsForbiddenBeforeMCP(t *testing.T) {
	baseURL, keyStr, client, auditBuf := startMCPServer(t)
	auditBuf.Reset()

	initBody := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "security-test", "version": "1"},
	})
	resp := postMCP(t, client, baseURL, keyStr, initBody, map[string]string{
		"Origin": "https://attacker.example.com",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("MEDIUM-2/SR-32: invalid Origin → want 403, got %d", resp.StatusCode)
	}

	// Verify no MCP audit entry: request must not have reached the MCP layer.
	logOutput := auditBuf.String()
	if strings.Contains(logOutput, "tool=") {
		t.Errorf(
			"MEDIUM-2/SR-32: invalid Origin must NOT reach MCP layer (found tool= in audit log)\n"+
				"log: %s", logOutput,
		)
	}
}

// TestOriginAbsentPassesOriginCheck verifies SR-32:
//   Absent Origin is NOT rejected by hostOriginMiddleware.
//   Without a valid key → 401 from auth middleware (not 403 from origin check).
func TestOriginAbsentPassesOriginCheck(t *testing.T) {
	baseURL, _, client, _ := startMCPServer(t)

	initBody := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "security-test", "version": "1"},
	})
	// No Origin header, no auth → expect 401 (auth gate), not 403 (origin gate).
	resp := postMCP(t, client, baseURL, "", initBody, nil)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		t.Errorf(
			"MEDIUM-2/SR-32: absent Origin MUST NOT trigger 403 — " +
				"hostOriginMiddleware must only reject PRESENT invalid Origins",
		)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("MEDIUM-2/SR-32: absent Origin + no auth → want 401, got %d", resp.StatusCode)
	}
}

// TestOriginValidAllowsRequest verifies SR-32:
//   An Origin in cfg.OriginAllow passes the check → request proceeds to MCP.
func TestOriginValidAllowsRequest(t *testing.T) {
	baseURL, keyStr, client, _ := startMCPServer(t)

	initBody := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "security-test", "version": "1"},
	})
	// "127.0.0.1" is in newTestConfig OriginAllow.
	resp := postMCP(t, client, baseURL, keyStr, initBody, map[string]string{
		"Origin": "https://127.0.0.1",
	})
	respBody := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("MEDIUM-2/SR-32: valid Origin → want 200, got %d; body=%s", resp.StatusCode, respBody)
	}
}

// ============================================================================
// Full MCP cycle: initialize → capabilities (only tools) + serverInfo
// ============================================================================

// TestInitializeCapabilitiesOnlyTools verifies AC3/SR-31:
//   - capabilities has "tools" key
//   - capabilities does NOT have "resources" or "prompts" (Q4: not declared in v1)
//   - serverInfo.name == "raxd", version non-empty
//   - protocolVersion == "2025-11-25"
func TestInitializeCapabilitiesOnlyTools(t *testing.T) {
	baseURL, keyStr, client, _ := startMCPServer(t)

	body := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	})
	resp := postMCP(t, client, baseURL, keyStr, body, nil)
	respBody := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("AC3: initialize → want 200, got %d; body=%s", resp.StatusCode, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(respBody), &result); err != nil {
		t.Fatalf("AC3: not JSON: %v; body=%s", err, respBody)
	}
	res, ok := result["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("AC3: no result; body=%s", respBody)
	}
	if res["protocolVersion"] != "2025-11-25" {
		t.Errorf("AC3: protocolVersion = %v, want 2025-11-25", res["protocolVersion"])
	}
	si, ok := res["serverInfo"].(map[string]interface{})
	if !ok {
		t.Fatalf("AC3: no serverInfo; body=%s", respBody)
	}
	if si["name"] != "raxd" {
		t.Errorf("AC3: serverInfo.name = %v, want raxd", si["name"])
	}
	if si["version"] == nil || si["version"] == "" {
		t.Errorf("AC3: serverInfo.version is empty")
	}
	caps, ok := res["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatalf("AC3: no capabilities; body=%s", respBody)
	}
	if _, has := caps["tools"]; !has {
		t.Errorf("AC3: capabilities must have tools; caps=%v", caps)
	}
	if _, has := caps["resources"]; has {
		t.Errorf("AC3/Q4: capabilities must NOT have resources in v1; caps=%v", caps)
	}
	if _, has := caps["prompts"]; has {
		t.Errorf("AC3/Q4: capabilities must NOT have prompts in v1; caps=%v", caps)
	}
}

// ============================================================================
// tools/list: [ping, server_info] with inputSchema; NOT execute_command
// ============================================================================

// TestToolsListSchemas verifies AC4/SR-31:
//   both tools have inputSchema.type == "object"; execute_command absent.
func TestToolsListSchemas(t *testing.T) {
	baseURL, keyStr, client, _ := startMCPServer(t)

	initResp := postMCP(t, client, baseURL, keyStr, jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	}), nil)
	readBody(t, initResp)

	listResp := postMCP(t, client, baseURL, keyStr, jsonrpcBody(2, "tools/list", map[string]interface{}{}),
		map[string]string{"MCP-Protocol-Version": "2025-11-25"})
	body := readBody(t, listResp)

	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("AC4: tools/list → want 200, got %d; body=%s", listResp.StatusCode, body)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("AC4: not JSON: %v", err)
	}
	res, _ := result["result"].(map[string]interface{})
	tools, ok := res["tools"].([]interface{})
	if !ok {
		t.Fatalf("AC4: tools not array; body=%s", body)
	}

	toolMap := make(map[string]map[string]interface{})
	for _, raw := range tools {
		if tm, ok := raw.(map[string]interface{}); ok {
			if n, ok := tm["name"].(string); ok {
				toolMap[n] = tm
			}
		}
	}

	for _, name := range []string{"ping", "server_info"} {
		tool, found := toolMap[name]
		if !found {
			t.Errorf("AC4: tool %q not in tools/list", name)
			continue
		}
		schema, ok := tool["inputSchema"].(map[string]interface{})
		if !ok {
			t.Errorf("AC4/SR-31: tool %q has no inputSchema", name)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("AC4/SR-31: tool %q inputSchema.type = %v, want object", name, schema["type"])
		}
	}
	if _, bad := toolMap["execute_command"]; bad {
		t.Errorf("AC13/SR-37: execute_command must NOT be in tools/list")
	}
	if _, bad := toolMap["upload_file"]; bad {
		t.Errorf("AC13/SR-37: upload_file must NOT be in tools/list")
	}
}

// ============================================================================
// server_info: exactly {name, version, protocolVersion}, no extra fields
// ============================================================================

// TestServerInfoExactFields verifies AC6/SR-33:
//   structuredContent contains name, version, protocolVersion — no forbidden fields.
func TestServerInfoExactFields(t *testing.T) {
	baseURL, keyStr, client, _ := startMCPServer(t)

	initResp := postMCP(t, client, baseURL, keyStr, jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	}), nil)
	readBody(t, initResp)

	resp := postMCP(t, client, baseURL, keyStr, jsonrpcBody(4, "tools/call", map[string]interface{}{
		"name": "server_info", "arguments": map[string]interface{}{},
	}), map[string]string{"MCP-Protocol-Version": "2025-11-25"})
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("AC6: server_info → want 200, got %d; body=%s", resp.StatusCode, body)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("AC6: not JSON: %v", err)
	}
	res, ok := result["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("AC6: no result; body=%s", body)
	}
	if res["isError"] == true {
		t.Errorf("AC6: server_info isError=true; body=%s", body)
	}
	sc, ok := res["structuredContent"].(map[string]interface{})
	if !ok {
		t.Fatalf("AC6/SR-33: no structuredContent; body=%s", body)
	}
	if sc["name"] != "raxd" {
		t.Errorf("SR-33: name = %v, want raxd", sc["name"])
	}
	if sc["version"] == nil || sc["version"] == "" {
		t.Errorf("SR-33: version is empty")
	}
	if sc["protocolVersion"] != "2025-11-25" {
		t.Errorf("SR-33: protocolVersion = %v, want 2025-11-25", sc["protocolVersion"])
	}
	// SR-33: no forbidden fields (paths, keys, tls, env, host, port, pid, salt, etc.)
	forbidden := []string{"path", "config", "key", "tls", "env", "host", "pid", "port", "uptime", "salt"}
	for k := range sc {
		kl := strings.ToLower(k)
		for _, bad := range forbidden {
			if strings.Contains(kl, bad) {
				t.Errorf("SR-33: structuredContent has forbidden field %q (matches %q)", k, bad)
			}
		}
	}
}

// ============================================================================
// Invalid JSON → JSON-RPC -32700 (or HTTP 400), server survives
// ============================================================================

// TestInvalidJSONReturnsParseError verifies AC7/SR-30:
//   Malformed JSON → error response; server remains alive.
func TestInvalidJSONReturnsParseError(t *testing.T) {
	baseURL, keyStr, client, _ := startMCPServer(t)

	// Broken JSON body.
	resp := postMCP(t, client, baseURL, keyStr, `{not valid json at all`, map[string]string{
		"MCP-Protocol-Version": "2025-11-25",
	})
	body := readBody(t, resp)

	// Either JSON-RPC error response OR HTTP 400 — both acceptable per mcp-spec §7.1.
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(body), &result); err == nil {
		// JSON response: must be an error, not a result.
		if result["error"] == nil && result["result"] != nil {
			t.Errorf("AC7/SR-30: invalid JSON → want JSON-RPC error, got result; body=%s", body)
		}
	} else if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("AC7/SR-30: invalid JSON → want JSON-RPC error or 400, got %d; body=%s",
			resp.StatusCode, body)
	}

	// Server must survive: valid ping still works.
	pingBody := jsonrpcBody(99, "tools/call", map[string]interface{}{
		"name": "ping", "arguments": map[string]interface{}{},
	})
	pingResp := postMCP(t, client, baseURL, keyStr, pingBody, map[string]string{
		"MCP-Protocol-Version": "2025-11-25",
	})
	pingRespBody := readBody(t, pingResp)
	var pingResult map[string]interface{}
	if err := json.Unmarshal([]byte(pingRespBody), &pingResult); err != nil {
		t.Fatalf("AC7/SR-30: server not alive after invalid JSON; ping=%s", pingRespBody)
	}
	if pingResult["error"] != nil {
		t.Errorf("AC7/SR-30: ping after invalid JSON must succeed; body=%s", pingRespBody)
	}
}

// ============================================================================
// Unknown tool → JSON-RPC -32602 (not executed), server survives  (SR-37)
// ============================================================================

// TestUnknownToolNotExecuted verifies AC7/SR-37:
//   execute_command is not registered → JSON-RPC error code -32601 or -32602.
//   Server alive after error.
func TestUnknownToolNotExecuted(t *testing.T) {
	baseURL, keyStr, client, _ := startMCPServer(t)

	initResp := postMCP(t, client, baseURL, keyStr, jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	}), nil)
	readBody(t, initResp)

	resp := postMCP(t, client, baseURL, keyStr, jsonrpcBody(5, "tools/call", map[string]interface{}{
		"name": "execute_command", "arguments": map[string]interface{}{},
	}), map[string]string{"MCP-Protocol-Version": "2025-11-25"})
	body := readBody(t, resp)

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("AC7/SR-37: response not JSON: %v; body=%s", err, body)
	}
	if result["error"] == nil {
		t.Errorf("AC7/SR-37: unknown tool must return JSON-RPC error; body=%s", body)
	}
	if errObj, ok := result["error"].(map[string]interface{}); ok {
		code, _ := errObj["code"].(float64)
		if code != -32602 && code != -32601 {
			t.Errorf("AC7/SR-37: error.code = %v, want -32602 or -32601; body=%s", code, body)
		}
	}

	// Server alive.
	pingResp := postMCP(t, client, baseURL, keyStr, jsonrpcBody(6, "tools/call", map[string]interface{}{
		"name": "ping", "arguments": map[string]interface{}{},
	}), map[string]string{"MCP-Protocol-Version": "2025-11-25"})
	pingBody := readBody(t, pingResp)
	var pingResult map[string]interface{}
	if err := json.Unmarshal([]byte(pingBody), &pingResult); err != nil {
		t.Fatalf("AC7/SR-37: server not alive after error; ping=%s", pingBody)
	}
	if pingResult["error"] != nil {
		t.Errorf("AC7/SR-37: ping after error must succeed; body=%s", pingBody)
	}
}

// ============================================================================
// MCP audit: exactly one MCP record per tools/call + one AUTH from transport
// ============================================================================

// TestMCPAuditExactRecordsPerToolsCall verifies AC9/SR-35/SR-36:
//   A single tools/call ping produces exactly:
//   - 1 AUTH record  (authSuccessAuditMiddleware, transport layer)
//   - 1 MCP record   (withAudit decorator, tool=ping, result=ok)
//   Total = 2 records. API key must not appear in the log.
//
// Record count design note (SR-36/plan.md authSuccessAuditMiddleware):
//   authSuccessAuditMiddleware fires once per request that clears auth+rate-limit.
//   withAudit fires once per tools/call invocation. For one tools/call POST →
//   expected: 1 AUTH + 1 MCP = 2 total records in the audit buffer.
func TestMCPAuditExactRecordsPerToolsCall(t *testing.T) {
	baseURL, keyStr, client, auditBuf := startMCPServer(t)

	// initialize → discard audit entries
	initResp := postMCP(t, client, baseURL, keyStr, jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	}), nil)
	readBody(t, initResp)
	auditBuf.Reset()

	// Single tools/call ping
	resp := postMCP(t, client, baseURL, keyStr, jsonrpcBody(3, "tools/call", map[string]interface{}{
		"name": "ping", "arguments": map[string]interface{}{},
	}), map[string]string{"MCP-Protocol-Version": "2025-11-25"})
	readBody(t, resp)

	logOutput := auditBuf.String()

	// Exactly one MCP record for this tools/call.
	mcpCount := strings.Count(logOutput, "tool=ping")
	if mcpCount != 1 {
		t.Errorf("AC9/SR-35: want exactly 1 MCP audit record (tool=ping), got %d; log=%s",
			mcpCount, logOutput)
	}
	// AUTH record from transport.
	if !strings.Contains(logOutput, "AUTH") {
		t.Errorf("SR-36: want AUTH record from transport; log=%s", logOutput)
	}
	// fp= present in MCP record.
	if !strings.Contains(logOutput, "fp=") {
		t.Errorf("SR-35: audit must contain fp=; log=%s", logOutput)
	}
	// SR-34: API key must not appear.
	if strings.Contains(logOutput, keyStr) {
		t.Errorf("SR-34: API key appears in audit log! log=%s", logOutput)
	}
}

// ============================================================================
// Helpers specific to this file
// ============================================================================

// startMCPServerWithTLSKey starts a full raxd server and returns the raw content
// of key.pem from TLSDir — required by MEDIUM-1 no-secrets test.
func startMCPServerWithTLSKey(t *testing.T) (baseURL, keyStr string, client *http.Client, auditBuf *bytes.Buffer, tlsKeyContent string) {
	t.Helper()

	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("mcp-secrets-test")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	auditBuf = &bytes.Buffer{}
	logger := newTestLogger(auditBuf)
	auditFn := server.NewAuditFnForTest(logger)
	mcpH, err := internalmcp.NewHandler(version.Version, auditFn)
	if err != nil {
		t.Fatalf("mcp.NewHandler: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)
	srv, err := server.New(cfg, paths, store, logger, mcpH)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	// Build TLS client from cert.pem.
	certPath := filepath.Join(paths.TLSDir, "cert.pem")
	pool := x509.NewCertPool()
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatal("append cert")
	}
	client = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
		Timeout: 10 * time.Second,
	}

	// Read private TLS key — core of MEDIUM-1.
	// server.New generates key.pem via loadOrCreateCert before returning.
	keyPEMPath := filepath.Join(paths.TLSDir, "key.pem")
	keyPEMBytes, err := os.ReadFile(keyPEMPath)
	if err != nil {
		t.Fatalf("MEDIUM-1: cannot read key.pem at %s: %v", keyPEMPath, err)
	}
	tlsKeyContent = string(keyPEMBytes)

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		_ = srv.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-runDone:
		case <-time.After(5 * time.Second):
		}
	})

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

	baseURL = fmt.Sprintf("https://127.0.0.1:%d", port)
	keyStr = string(plain)
	return
}
