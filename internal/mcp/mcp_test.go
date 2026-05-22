package mcp_test

// mcp_test.go — integration tests for internal/mcp package.
//
// Covers AC1–AC10, AC12–AC14 (AC11/AC15 are CLI/docs-level).
// All tests run in Docker via -mod=vendor (SR-39/AC14).
// Run with -race for concurrency (SR-39/R-M7).

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	clog "github.com/charmbracelet/log"
	"github.com/vladimirvkhs/raxd/internal/cmdexec"
	"github.com/vladimirvkhs/raxd/internal/config"
	"github.com/vladimirvkhs/raxd/internal/fileupload"
	"github.com/vladimirvkhs/raxd/internal/keystore"
	internalmcp "github.com/vladimirvkhs/raxd/internal/mcp"
	"github.com/vladimirvkhs/raxd/internal/server"
	"github.com/vladimirvkhs/raxd/internal/version"
)

// defaultExecCfg возвращает безопасный ExecConfig для тестов.
func defaultExecCfg() cmdexec.Config {
	return cmdexec.Config{
		Allowlist:        nil,
		DefaultTimeoutMs: 30000,
		MaxTimeoutMs:     300000,
		DefaultCwd:       "/tmp",
		EnvWhitelist:     []string{"PATH", "HOME", "LANG", "TERM"},
		MaxArgs:          256,
		MaxArgLen:        131072,
		MaxOutputBytes:   1048576,
		DenyRoot:         false,
	}
}

// defaultUplCfg возвращает безопасный fileupload.Config для тестов.
// UploadRoot выставляется в t.TempDir(); тесты, которым нужен другой корень, создают свой Config.
func defaultUplCfg(t *testing.T) fileupload.Config {
	t.Helper()
	return fileupload.Config{
		UploadRoot:   t.TempDir(),
		MaxFileBytes: 716800,
		DefaultMode:  0o600,
		DenyRoot:     false,
	}
}

// ---- helpers ----------------------------------------------------------------

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
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       10 * time.Second,
		MaxHeaderBytes:    1 << 20,
		MaxBodyBytes:      1 << 20,
	}
}

func newTestLogger(buf *bytes.Buffer) *clog.Logger {
	l := clog.New(buf)
	l.SetTimeFormat("2006-01-02T15:04:05Z")
	return l
}

// freePort returns a free TCP port.
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

// startMCPServer starts a full raxd server with MCP handler.
// Returns the base URL, key string, client, and audit log buffer.
func startMCPServer(t *testing.T) (baseURL string, keyStr string, client *http.Client, auditBuf *bytes.Buffer) {
	t.Helper()

	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	plain, _, err := store.Create("mcp-test-key")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	auditBuf = &bytes.Buffer{}
	logger := newTestLogger(auditBuf)

	auditFn := server.NewAuditFnForTest(logger)
	mcpH, err := internalmcp.NewHandler(version.Version, auditFn, defaultExecCfg(), defaultUplCfg(t))
	if err != nil {
		t.Fatalf("mcp.NewHandler: %v", err)
	}

	port := freePort(t)
	cfg := newTestConfig(port)

	srv, err := server.New(cfg, paths, store, logger, mcpH)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	// Build TLS client before starting server.
	certPath := filepath.Join(paths.TLSDir, "cert.pem")
	// cert.pem is created by server.New (loadOrCreateCert).
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

// postMCP sends a POST to /mcp with the given body and bearer key.
func postMCP(t *testing.T, client *http.Client, baseURL, bearer, body string, extraHeaders map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	return resp
}

// readBody reads and closes the response body, returns the string.
func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// jsonrpcBody builds a JSON-RPC 2.0 request body.
func jsonrpcBody(id int, method string, params interface{}) string {
	type req struct {
		JSONRPC string      `json:"jsonrpc"`
		ID      int         `json:"id"`
		Method  string      `json:"method"`
		Params  interface{} `json:"params,omitempty"`
	}
	b, _ := json.Marshal(req{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	return string(b)
}

// ============================================================================
// AC2/AC8/SR-27: unauthenticated MCP → 401
// ============================================================================

func TestMCPNoAuthReturns401(t *testing.T) {
	baseURL, _, client, _ := startMCPServer(t)

	body := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	})
	resp := postMCP(t, client, baseURL, "", body, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("AC2/SR-27: no auth MCP → want 401, got %d", resp.StatusCode)
	}
}

// TestMCPUnknownKeyReturns401 verifies that an unknown/revoked key returns 401 before MCP.
func TestMCPUnknownKeyReturns401(t *testing.T) {
	baseURL, _, client, _ := startMCPServer(t)

	body := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	})
	resp := postMCP(t, client, baseURL, "rax_live_unknown_key_12345", body, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("AC8/SR-27: unknown key MCP → want 401, got %d", resp.StatusCode)
	}
}

// ============================================================================
// AC12/SR-32: invalid Origin → 403 before MCP
// ============================================================================

func TestMCPInvalidOriginReturns403(t *testing.T) {
	baseURL, keyStr, client, _ := startMCPServer(t)

	body := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	})
	resp := postMCP(t, client, baseURL, keyStr, body, map[string]string{
		"Origin": "https://evil.example.com",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("AC12/SR-32: invalid Origin → want 403, got %d", resp.StatusCode)
	}
}

// ============================================================================
// AC1/AC3/SR-31: initialize returns capabilities and serverInfo
// ============================================================================

func TestMCPInitializeCapabilities(t *testing.T) {
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

	// AC1: must be JSON-RPC, not 501.
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(respBody), &result); err != nil {
		t.Fatalf("AC1: response not JSON: %v; body=%s", err, respBody)
	}
	if result["jsonrpc"] != "2.0" {
		t.Errorf("AC1: jsonrpc field = %v, want 2.0", result["jsonrpc"])
	}

	// AC3: result must have serverInfo with name=raxd.
	res, ok := result["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("AC3: no result object in response; body=%s", respBody)
	}
	si, ok := res["serverInfo"].(map[string]interface{})
	if !ok {
		t.Fatalf("AC3: no serverInfo in result; body=%s", respBody)
	}
	if si["name"] != "raxd" {
		t.Errorf("AC3: serverInfo.name = %v, want raxd", si["name"])
	}
	if si["version"] == nil || si["version"] == "" {
		t.Errorf("AC3: serverInfo.version is empty; body=%s", respBody)
	}

	// AC3: capabilities must include tools.
	caps, ok := res["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatalf("AC3: no capabilities in result; body=%s", respBody)
	}
	if _, hasTools := caps["tools"]; !hasTools {
		t.Errorf("AC3: capabilities must include tools; caps=%v", caps)
	}

	// AC3: protocolVersion.
	if res["protocolVersion"] != "2025-11-25" {
		t.Errorf("AC3: protocolVersion = %v, want 2025-11-25", res["protocolVersion"])
	}
}

// ============================================================================
// AC4/SR-31: tools/list returns exactly ping and server_info
// ============================================================================

func TestMCPToolsList(t *testing.T) {
	baseURL, keyStr, client, _ := startMCPServer(t)

	// First: initialize session.
	initBody := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	})
	initResp := postMCP(t, client, baseURL, keyStr, initBody, nil)
	readBody(t, initResp)

	// Then: tools/list.
	listBody := jsonrpcBody(2, "tools/list", map[string]interface{}{})
	resp := postMCP(t, client, baseURL, keyStr, listBody, map[string]string{
		"MCP-Protocol-Version": "2025-11-25",
	})
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("AC4: tools/list → want 200, got %d; body=%s", resp.StatusCode, body)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("AC4: response not JSON: %v; body=%s", err, body)
	}

	res, ok := result["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("AC4: no result in tools/list response; body=%s", body)
	}
	tools, ok := res["tools"].([]interface{})
	if !ok {
		t.Fatalf("AC4: tools field is not array; body=%s", body)
	}

	// Collect tool names.
	names := make(map[string]bool)
	for _, tool := range tools {
		tm, ok := tool.(map[string]interface{})
		if !ok {
			continue
		}
		if n, ok := tm["name"].(string); ok {
			names[n] = true
		}
	}

	if !names["ping"] {
		t.Errorf("AC4: tools/list must include ping; names=%v; body=%s", names, body)
	}
	if !names["server_info"] {
		t.Errorf("AC4: tools/list must include server_info; names=%v; body=%s", names, body)
	}
	// command-exec task: execute_command MUST now be present (AC1/SR-40).
	if !names["execute_command"] {
		t.Errorf("AC1/command-exec: tools/list must include execute_command; names=%v; body=%s", names, body)
	}
	// file-upload task: upload_file MUST now be present (AC1/SR-68).
	if !names["upload_file"] {
		t.Errorf("AC1/file-upload: tools/list must include upload_file; names=%v; body=%s", names, body)
	}
}

// ============================================================================
// AC5/SR-31: tools/call ping → pong, no side effects
// ============================================================================

func TestMCPCallPingReturnsPong(t *testing.T) {
	baseURL, keyStr, client, _ := startMCPServer(t)

	// Initialize.
	initResp := postMCP(t, client, baseURL, keyStr, jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	}), nil)
	readBody(t, initResp)

	// Call ping.
	callBody := jsonrpcBody(3, "tools/call", map[string]interface{}{
		"name":      "ping",
		"arguments": map[string]interface{}{},
	})
	resp := postMCP(t, client, baseURL, keyStr, callBody, map[string]string{
		"MCP-Protocol-Version": "2025-11-25",
	})
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("AC5: tools/call ping → want 200, got %d; body=%s", resp.StatusCode, body)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("AC5: response not JSON: %v; body=%s", err, body)
	}

	res, ok := result["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("AC5: no result in ping response; body=%s", body)
	}
	if res["isError"] == true {
		t.Errorf("AC5: ping result isError = true; body=%s", body)
	}

	// content[0].text must be "pong".
	content, ok := res["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatalf("AC5: ping content is empty; body=%s", body)
	}
	ct, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatalf("AC5: ping content[0] not object; body=%s", body)
	}
	if ct["text"] != "pong" {
		t.Errorf("AC5: ping content[0].text = %v, want pong; body=%s", ct["text"], body)
	}
}

// ============================================================================
// AC6/SR-33/SR-34: tools/call server_info → {name, version, protocolVersion}, no secrets
// ============================================================================

func TestMCPCallServerInfo(t *testing.T) {
	baseURL, keyStr, client, _ := startMCPServer(t)

	initResp := postMCP(t, client, baseURL, keyStr, jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	}), nil)
	readBody(t, initResp)

	callBody := jsonrpcBody(4, "tools/call", map[string]interface{}{
		"name":      "server_info",
		"arguments": map[string]interface{}{},
	})
	resp := postMCP(t, client, baseURL, keyStr, callBody, map[string]string{
		"MCP-Protocol-Version": "2025-11-25",
	})
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("AC6: tools/call server_info → want 200, got %d; body=%s", resp.StatusCode, body)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("AC6: response not JSON: %v; body=%s", err, body)
	}

	res, ok := result["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("AC6: no result in server_info response; body=%s", body)
	}
	if res["isError"] == true {
		t.Errorf("AC6: server_info isError = true; body=%s", body)
	}

	// SR-33: structuredContent must have name, version, protocolVersion.
	sc, ok := res["structuredContent"].(map[string]interface{})
	if !ok {
		t.Fatalf("AC6/SR-33: no structuredContent in server_info result; body=%s", body)
	}
	if sc["name"] != "raxd" {
		t.Errorf("SR-33: structuredContent.name = %v, want raxd", sc["name"])
	}
	if sc["version"] == nil || sc["version"] == "" {
		t.Errorf("SR-33: structuredContent.version is empty")
	}
	if sc["protocolVersion"] != "2025-11-25" {
		t.Errorf("SR-33: structuredContent.protocolVersion = %v, want 2025-11-25", sc["protocolVersion"])
	}
}

// TestMCPServerInfoNoSecrets verifies SR-34: server_info response does not contain
// the API key body, "keys.db", or "key.pem" as substrings.
func TestMCPServerInfoNoSecrets(t *testing.T) {
	baseURL, keyStr, client, _ := startMCPServer(t)

	initResp := postMCP(t, client, baseURL, keyStr, jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	}), nil)
	readBody(t, initResp)

	callBody := jsonrpcBody(4, "tools/call", map[string]interface{}{
		"name":      "server_info",
		"arguments": map[string]interface{}{},
	})
	resp := postMCP(t, client, baseURL, keyStr, callBody, map[string]string{
		"MCP-Protocol-Version": "2025-11-25",
	})
	body := readBody(t, resp)

	// SR-34/AC10: key body must not appear in response.
	if strings.Contains(body, keyStr) {
		t.Errorf("SR-34/AC10: API key appears in server_info response! body=%s", body)
	}
	// "keys.db" must not appear.
	if strings.Contains(body, "keys.db") {
		t.Errorf("SR-34/AC10: 'keys.db' appears in server_info response! body=%s", body)
	}
	// "key.pem" must not appear.
	if strings.Contains(body, "key.pem") {
		t.Errorf("SR-34/AC10: 'key.pem' appears in server_info response! body=%s", body)
	}
}

// ============================================================================
// AC7/SR-30: unknown tool → JSON-RPC error, server survives
// ============================================================================

func TestMCPUnknownToolReturnsError(t *testing.T) {
	baseURL, keyStr, client, _ := startMCPServer(t)

	initResp := postMCP(t, client, baseURL, keyStr, jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	}), nil)
	readBody(t, initResp)

	// Call an unknown tool (not any registered tool).
	callBody := jsonrpcBody(5, "tools/call", map[string]interface{}{
		"name":      "nonexistent_tool",
		"arguments": map[string]interface{}{},
	})
	resp := postMCP(t, client, baseURL, keyStr, callBody, map[string]string{
		"MCP-Protocol-Version": "2025-11-25",
	})
	body := readBody(t, resp)

	// Must return JSON-RPC error, not 501 or panic.
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("AC7: response not JSON: %v; body=%s", err, body)
	}
	if result["error"] == nil {
		t.Errorf("AC7/SR-30: unknown tool must return JSON-RPC error; body=%s", body)
	}
	// Server must still be alive.
	pingResp := postMCP(t, client, baseURL, keyStr, jsonrpcBody(6, "tools/call", map[string]interface{}{
		"name":      "ping",
		"arguments": map[string]interface{}{},
	}), map[string]string{
		"MCP-Protocol-Version": "2025-11-25",
	})
	pingBody := readBody(t, pingResp)
	var pingResult map[string]interface{}
	if err := json.Unmarshal([]byte(pingBody), &pingResult); err != nil {
		t.Fatalf("AC7: server not alive after error; ping body=%s", pingBody)
	}
	if pingResult["error"] != nil {
		t.Errorf("AC7: ping after error must succeed; body=%s", pingBody)
	}
}

// ============================================================================
// AC1/SR-30: GET /mcp → 405
// ============================================================================

func TestMCPGetReturns405(t *testing.T) {
	baseURL, keyStr, client, _ := startMCPServer(t)

	req, err := http.NewRequest(http.MethodGet, baseURL+"/mcp", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+keyStr)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("AC1/SR-30: GET /mcp → want 405, got %d", resp.StatusCode)
	}
}

// ============================================================================
// AC9/SR-35/SR-36: MCP audit record has fingerprint + tool=, no key body
// ============================================================================

func TestMCPAuditHasFingerprintAndTool(t *testing.T) {
	baseURL, keyStr, client, auditBuf := startMCPServer(t)

	initResp := postMCP(t, client, baseURL, keyStr, jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	}), nil)
	readBody(t, initResp)

	auditBuf.Reset() // clear init audit entries

	callBody := jsonrpcBody(3, "tools/call", map[string]interface{}{
		"name":      "ping",
		"arguments": map[string]interface{}{},
	})
	resp := postMCP(t, client, baseURL, keyStr, callBody, map[string]string{
		"MCP-Protocol-Version": "2025-11-25",
	})
	readBody(t, resp)

	logOutput := auditBuf.String()

	// SR-35: audit must contain tool=ping.
	if !strings.Contains(logOutput, "tool=ping") {
		t.Errorf("AC9/SR-35: audit log missing tool=ping; log=%s", logOutput)
	}
	// SR-35: audit must contain fp= field.
	if !strings.Contains(logOutput, "fp=") {
		t.Errorf("AC9/SR-35: audit log missing fp= field; log=%s", logOutput)
	}
	// SR-35 (strengthened): fingerprint must be a REAL hex value — not fp=- and not empty.
	// For a successful tools/call, the fingerprint comes from the authenticated context
	// and MUST be a non-empty hex string. fp=- means it was not propagated.
	// If this assertion fails → product bug: fingerprint not stored in auth context.
	// Escalate to developer; do NOT weaken this assertion.
	assertMCPRealFingerprint(t, "AC9/SR-35/TestMCPAuditHasFingerprintAndTool", logOutput)
	// SR-34/AC10: key body must NOT be in audit log.
	if strings.Contains(logOutput, keyStr) {
		t.Errorf("AC10/SR-34: key body found in audit log! log=%s", logOutput)
	}
}

// TestMCPAuditHasRealRemoteAddr verifies AC9/SR-35 (LOW-1):
// the MCP audit record for a tools/call must contain a REAL remote address
// (not "remote=-"). The remote address is stored in context by authMiddleware
// via server.RemoteAddrFromContext and retrieved by withAudit.
//
// The remote address in the MCP audit record must match the format used by the
// transport AUTH audit record for the same request (IP:port or IP).
// A failure here means remote=- was written, violating AC9/SR-35.
// Do NOT weaken this assertion — escalate to developer if it fails.
func TestMCPAuditHasRealRemoteAddr(t *testing.T) {
	baseURL, keyStr, client, auditBuf := startMCPServer(t)

	// initialize — discard audit
	initResp := postMCP(t, client, baseURL, keyStr, jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	}), nil)
	readBody(t, initResp)
	auditBuf.Reset()

	// tools/call ping
	callBody := jsonrpcBody(3, "tools/call", map[string]interface{}{
		"name":      "ping",
		"arguments": map[string]interface{}{},
	})
	resp := postMCP(t, client, baseURL, keyStr, callBody, map[string]string{
		"MCP-Protocol-Version": "2025-11-25",
	})
	readBody(t, resp)

	logOutput := auditBuf.String()

	// Find the MCP audit line (contains "tool=ping").
	var mcpLine string
	for _, line := range strings.Split(logOutput, "\n") {
		if strings.Contains(line, "tool=ping") {
			mcpLine = line
			break
		}
	}
	if mcpLine == "" {
		t.Fatalf("AC9/SR-35 LOW-1: no MCP audit line with tool=ping found; log=%s", logOutput)
	}

	// Extract remote= value from the MCP audit line.
	idx := strings.Index(mcpLine, "remote=")
	if idx < 0 {
		t.Errorf("AC9/SR-35 LOW-1: MCP audit line has no remote= field; line=%q", mcpLine)
		return
	}
	rest := mcpLine[idx+7:] // after "remote="
	rest = strings.TrimPrefix(rest, `"`)
	end := strings.IndexAny(rest, " \t\n\r\"")
	var remoteVal string
	if end >= 0 {
		remoteVal = rest[:end]
	} else {
		remoteVal = rest
	}
	remoteVal = strings.TrimSuffix(remoteVal, `"`)

	if remoteVal == "-" || remoteVal == "" {
		t.Errorf(
			"AC9/SR-35 LOW-1 PRODUCT BUG: MCP audit record has remote=%q — "+
				"remote address was not propagated from auth context to withAudit.\n"+
				"Escalate to developer — do NOT weaken this assertion.\n"+
				"MCP audit line: %q\nFull log: %s",
			remoteVal, mcpLine, logOutput,
		)
		return
	}

	// Must contain a digit (valid IP) — not a sentinel string.
	hasDigit := false
	for _, c := range remoteVal {
		if c >= '0' && c <= '9' {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		t.Errorf(
			"AC9/SR-35 LOW-1: MCP audit remote=%q has no digit — expected IP:port format; "+
				"line=%q",
			remoteVal, mcpLine,
		)
	}

	// Verify the remote= format matches the transport AUTH record for the same request.
	// Both should contain the same client IP (127.0.0.1 in tests).
	var authLine string
	for _, line := range strings.Split(logOutput, "\n") {
		if strings.Contains(line, "AUTH") {
			authLine = line
			break
		}
	}
	if authLine != "" {
		// Extract remote= from AUTH line.
		aidx := strings.Index(authLine, "remote=")
		if aidx >= 0 {
			authRest := authLine[aidx+7:]
			authRest = strings.TrimPrefix(authRest, `"`)
			authEnd := strings.IndexAny(authRest, " \t\n\r\"")
			var authRemote string
			if authEnd >= 0 {
				authRemote = authRest[:authEnd]
			} else {
				authRemote = authRest
			}
			authRemote = strings.TrimSuffix(authRemote, `"`)

			// IP parts (before ':') must match — same request, same client.
			mcpIP := strings.Split(remoteVal, ":")[0]
			authIP := strings.Split(authRemote, ":")[0]
			if mcpIP != authIP {
				t.Errorf(
					"AC9/SR-35 LOW-1: MCP remote IP %q != AUTH remote IP %q — "+
						"format mismatch between transport and MCP audit records.\n"+
						"MCP line: %q\nAUTH line: %q",
					mcpIP, authIP, mcpLine, authLine,
				)
			}
		}
	}
}

// assertMCPRealFingerprint verifies that every "fp=" occurrence in logOutput
// is followed by a non-empty hex value (not "-"). Mirrors assertRealFingerprint
// in mcp_security_test.go (different package file, same package mcp_test).
//
// A failure means fp=- or empty was written to the audit log for an authenticated
// request — the fingerprint was not propagated from the auth context. Escalate.
func assertMCPRealFingerprint(t *testing.T, label, logOutput string) {
	t.Helper()
	for _, line := range strings.Split(logOutput, "\n") {
		idx := strings.Index(line, "fp=")
		if idx < 0 {
			continue
		}
		rest := line[idx+3:]
		rest = strings.TrimPrefix(rest, `"`)
		end := strings.IndexAny(rest, " \t\n\r\"")
		var fpVal string
		if end >= 0 {
			fpVal = rest[:end]
		} else {
			fpVal = rest
		}
		fpVal = strings.TrimSuffix(fpVal, `"`)

		if fpVal == "-" || fpVal == "" {
			t.Errorf(
				"%s: audit log has fp=- or empty fingerprint for authenticated tools/call.\n"+
					"fingerprint must be a real hex value; fp=- means not propagated from auth context.\n"+
					"Escalate to developer — do NOT weaken this assertion.\nline: %q",
				label, line,
			)
		} else {
			hasHex := false
			for _, c := range fpVal {
				if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
					hasHex = true
					break
				}
			}
			if !hasHex {
				t.Errorf(
					"%s: fingerprint %q has no hex chars; want non-empty hex. line: %q",
					label, fpVal, line,
				)
			}
		}
	}
}

// ============================================================================
// AC9/SR-36: non-MCP AUTH record must NOT have tool= field
// ============================================================================

func TestMCPAuditNonMCPNoToolField(t *testing.T) {
	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("non-mcp-audit")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	auditBuf := &bytes.Buffer{}
	logger := newTestLogger(auditBuf)
	auditFn := server.NewAuditFnForTest(logger)
	mcpH, err := internalmcp.NewHandler(version.Version, auditFn, defaultExecCfg(), defaultUplCfg(t))
	if err != nil {
		t.Fatalf("mcp.NewHandler: %v", err)
	}

	port2 := freePort(t)
	cfg := newTestConfig(port2)
	srv, err := server.New(cfg, paths, store, logger, mcpH)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	// Build TLS client.
	certPath := filepath.Join(paths.TLSDir, "cert.pem")
	pool := x509.NewCertPool()
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	pool.AppendCertsFromPEM(certPEM)
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
		Timeout: 5 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		_ = srv.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-runDone
	})

	// Wait ready.
	addr2 := fmt.Sprintf("127.0.0.1:%d", port2)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := tls.Dial("tcp", addr2, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec
		if dialErr == nil {
			conn.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Hit /healthz — should produce AUTH record, no tool=.
	baseURL := fmt.Sprintf("https://127.0.0.1:%d", port2)
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/healthz", nil)
	req.Header.Set("Authorization", "Bearer "+string(plain))
	healthResp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	healthResp.Body.Close()

	logOutput := auditBuf.String()
	if !strings.Contains(logOutput, "AUTH") {
		t.Errorf("SR-36: /healthz success must produce AUTH record; log=%s", logOutput)
	}
	if strings.Contains(logOutput, "tool=") {
		t.Errorf("SR-36: /healthz AUTH record must NOT contain tool= field; log=%s", logOutput)
	}
}

// ============================================================================
// SR-39/AC14: concurrent tools/call — no data races (use -race flag)
// ============================================================================

func TestMCPConcurrentPing(t *testing.T) {
	baseURL, keyStr, client, _ := startMCPServer(t)

	initResp := postMCP(t, client, baseURL, keyStr, jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	}), nil)
	readBody(t, initResp)

	const workers = 10
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			callBody := jsonrpcBody(100+id, "tools/call", map[string]interface{}{
				"name":      "ping",
				"arguments": map[string]interface{}{},
			})
			resp := postMCP(t, client, baseURL, keyStr, callBody, map[string]string{
				"MCP-Protocol-Version": "2025-11-25",
			})
			body := readBody(t, resp)
			var result map[string]interface{}
			if err := json.Unmarshal([]byte(body), &result); err != nil {
				t.Errorf("SR-39: concurrent ping %d: response not JSON: %v; body=%s", id, err, body)
				return
			}
			if result["error"] != nil {
				t.Errorf("SR-39: concurrent ping %d: got error: %v", id, result["error"])
			}
		}(i)
	}
	wg.Wait()
}

// ============================================================================
// AC1/SR-28: no second auth channel in MCP — MCP package must not call keystore.Verify
// Static check: import of keystore from internal/mcp is forbidden.
// ============================================================================

func TestMCPPackageDoesNotImportKeystore(t *testing.T) {
	// Read internal/mcp source files and verify no import of keystore in import blocks.
	mcpDir := "."
	entries, err := os.ReadDir(mcpDir)
	if err != nil {
		t.Fatalf("read mcp dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(mcpDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		// Check only non-comment lines for keystore import.
		// A line is a comment if its first non-whitespace characters are "//".
		for _, line := range strings.Split(string(content), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue // skip comment lines
			}
			// An import line would look like: "github.com/vladimirvkhs/raxd/internal/keystore"
			if strings.Contains(trimmed, `"github.com/vladimirvkhs/raxd/internal/keystore"`) {
				t.Errorf("SR-28: internal/mcp/%s imports keystore — MCP layer must NOT call Verify", e.Name())
			}
		}
	}
}

// ============================================================================
// httptest-based unit test for NewHandler (no full server needed)
// ============================================================================

// TestNewHandlerReturnsHTTPHandler verifies that NewHandler returns a non-nil http.Handler.
func TestNewHandlerReturnsHTTPHandler(t *testing.T) {
	h, err := internalmcp.NewHandler("1.0.0", func(server.AuditRecord) {}, defaultExecCfg(), defaultUplCfg(t))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	if h == nil {
		t.Fatal("NewHandler returned nil handler")
	}
}

// TestNewHandlerInitializeViaHTTPTest drives initialize via httptest.Server.
// This does NOT require TLS and validates the pure MCP logic.
func TestNewHandlerInitializeViaHTTPTest(t *testing.T) {
	h, err := internalmcp.NewHandler("1.0.0", func(server.AuditRecord) {}, defaultExecCfg(), defaultUplCfg(t))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	ts := httptest.NewServer(h)
	defer ts.Close()

	body := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	respBody := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize via httptest → want 200, got %d; body=%s", resp.StatusCode, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(respBody), &result); err != nil {
		t.Fatalf("response not JSON: %v; body=%s", err, respBody)
	}
	res, ok := result["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("no result; body=%s", respBody)
	}
	si := res["serverInfo"].(map[string]interface{})
	if si["name"] != "raxd" {
		t.Errorf("serverInfo.name = %v, want raxd", si["name"])
	}
}

// TestNewHandlerPingViaHTTPTest drives ping via httptest (no TLS/auth).
func TestNewHandlerPingViaHTTPTest(t *testing.T) {
	h, err := internalmcp.NewHandler("1.0.0", func(server.AuditRecord) {}, defaultExecCfg(), defaultUplCfg(t))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	ts := httptest.NewServer(h)
	defer ts.Close()

	// Initialize first.
	initBody := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	})
	initReq, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(initBody))
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Accept", "application/json, text/event-stream")
	initResp, err := http.DefaultClient.Do(initReq)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	readBody(t, initResp)

	// Call ping.
	callBody := jsonrpcBody(3, "tools/call", map[string]interface{}{
		"name":      "ping",
		"arguments": map[string]interface{}{},
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(callBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-11-25")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	body := readBody(t, resp)

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("response not JSON: %v; body=%s", err, body)
	}
	res := result["result"].(map[string]interface{})
	content := res["content"].([]interface{})
	ct := content[0].(map[string]interface{})
	if ct["text"] != "pong" {
		t.Errorf("ping: want pong, got %v; body=%s", ct["text"], body)
	}
}

// TestNewHandlerServerInfoViaHTTPTest drives server_info via httptest.
func TestNewHandlerServerInfoViaHTTPTest(t *testing.T) {
	h, err := internalmcp.NewHandler("1.0.0", func(server.AuditRecord) {}, defaultExecCfg(), defaultUplCfg(t))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	ts := httptest.NewServer(h)
	defer ts.Close()

	// Initialize.
	initBody := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	})
	initReq, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(initBody))
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Accept", "application/json, text/event-stream")
	initResp, _ := http.DefaultClient.Do(initReq)
	readBody(t, initResp)

	// Call server_info.
	callBody := jsonrpcBody(4, "tools/call", map[string]interface{}{
		"name":      "server_info",
		"arguments": map[string]interface{}{},
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(callBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-11-25")

	resp, _ := http.DefaultClient.Do(req)
	body := readBody(t, resp)

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("response not JSON: %v; body=%s", err, body)
	}
	res := result["result"].(map[string]interface{})
	sc, ok := res["structuredContent"].(map[string]interface{})
	if !ok {
		t.Fatalf("no structuredContent; body=%s", body)
	}
	if sc["name"] != "raxd" {
		t.Errorf("structuredContent.name = %v, want raxd", sc["name"])
	}
	if sc["protocolVersion"] != "2025-11-25" {
		t.Errorf("structuredContent.protocolVersion = %v, want 2025-11-25", sc["protocolVersion"])
	}
}

// TestNewHandlerGetReturns405 verifies GET /mcp returns 405.
func TestNewHandlerGetReturns405(t *testing.T) {
	h, err := internalmcp.NewHandler("1.0.0", func(server.AuditRecord) {}, defaultExecCfg(), defaultUplCfg(t))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	ts := httptest.NewServer(h)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /mcp → want 405, got %d", resp.StatusCode)
	}
}

// TestNewHandlerAuditContainsToolAndFP verifies withAudit writes tool= and fp= to audit.
func TestNewHandlerAuditContainsToolAndFP(t *testing.T) {
	var auditBuf bytes.Buffer
	logger := newTestLogger(&auditBuf)
	auditFn := server.NewAuditFnForTest(logger)

	h, err := internalmcp.NewHandler("1.0.0", auditFn, defaultExecCfg(), defaultUplCfg(t))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	ts := httptest.NewServer(h)
	defer ts.Close()

	// Initialize.
	initBody := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	})
	initReq, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(initBody))
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Accept", "application/json, text/event-stream")
	initResp, _ := http.DefaultClient.Do(initReq)
	readBody(t, initResp)

	auditBuf.Reset()

	// Call ping.
	callBody := jsonrpcBody(3, "tools/call", map[string]interface{}{
		"name":      "ping",
		"arguments": map[string]interface{}{},
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(callBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-11-25")
	resp, _ := http.DefaultClient.Do(req)
	readBody(t, resp)

	logOutput := auditBuf.String()
	if !strings.Contains(logOutput, "tool=ping") {
		t.Errorf("SR-35: audit must contain tool=ping; log=%s", logOutput)
	}
	if !strings.Contains(logOutput, "fp=") {
		t.Errorf("SR-35: audit must contain fp= field; log=%s", logOutput)
	}
	if !strings.Contains(logOutput, "MCP") {
		t.Errorf("SR-36: audit msg-label must be MCP for tool call success; log=%s", logOutput)
	}
}
