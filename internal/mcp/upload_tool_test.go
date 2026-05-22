package mcp_test

// upload_tool_test.go — интеграционные тесты MCP-инструмента upload_file.
//
// Покрывает AC1-AC19 и SR-68..SR-82.
// Все тесты запускаются в Docker -mod=vendor (AC20/SR-82).
// helpers (startMCPServer, postMCP, jsonrpcBody, etc.) — из mcp_test.go.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vladimirvkhs/raxd/internal/fileupload"
	internalmcp "github.com/vladimirvkhs/raxd/internal/mcp"
	"github.com/vladimirvkhs/raxd/internal/server"
)

// ─── helpers для upload_file ─────────────────────────────────────────────────

// callUploadFile отправляет tools/call upload_file через httptest.
func callUploadFile(t *testing.T, ts *httptest.Server, args map[string]interface{}) (string, int) {
	t.Helper()

	// Инициализируем сессию.
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
	initResp.Body.Close()

	// tools/call upload_file.
	callBody := jsonrpcBody(10, "tools/call", map[string]interface{}{
		"name":      "upload_file",
		"arguments": args,
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(callBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-11-25")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("tools/call upload_file: %v", err)
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return buf.String(), resp.StatusCode
}

// newUploadTestServer создаёт httptest.Server с upload_file инструментом.
func newUploadTestServer(t *testing.T, uplCfg fileupload.Config) (*httptest.Server, *bytes.Buffer) {
	t.Helper()
	var auditBuf bytes.Buffer
	logger := newTestLogger(&auditBuf)
	auditFn := server.NewAuditFnForTest(logger)
	h, err := internalmcp.NewHandler("1.0.0", auditFn, defaultExecCfg(), uplCfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts, &auditBuf
}

// b64 кодирует bytes в base64.
func b64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// parseUploadResult извлекает result из JSON-RPC ответа.
func parseUploadResult(t *testing.T, body string) map[string]interface{} {
	t.Helper()
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("parse JSON: %v; body=%s", err, body)
	}
	res, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("no result in response; body=%s", body)
	}
	return res
}

// ─── AC1 / SR-68: upload_file присутствует в tools/list ──────────────────────

// TestUploadFileInToolsList: upload_file зарегистрирован в tools/list (AC1/SR-68).
func TestUploadFileInToolsList(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, _ := newUploadTestServer(t, cfg)

	// Initialize.
	initBody := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(initBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	resp.Body.Close()

	// tools/list.
	listBody := jsonrpcBody(2, "tools/list", map[string]interface{}{})
	listReq, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(listBody))
	listReq.Header.Set("Content-Type", "application/json")
	listReq.Header.Set("Accept", "application/json, text/event-stream")
	listReq.Header.Set("MCP-Protocol-Version", "2025-11-25")
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	var listBuf bytes.Buffer
	listBuf.ReadFrom(listResp.Body)
	listResp.Body.Close()
	listBodyStr := listBuf.String()

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(listBodyStr), &result); err != nil {
		t.Fatalf("tools/list not JSON: %v", err)
	}
	res := result["result"].(map[string]interface{})
	tools := res["tools"].([]interface{})

	names := make(map[string]bool)
	for _, raw := range tools {
		if tm, ok := raw.(map[string]interface{}); ok {
			if n, ok := tm["name"].(string); ok {
				names[n] = true
			}
		}
	}
	if !names["upload_file"] {
		t.Errorf("AC1/SR-68: upload_file not in tools/list; names=%v", names)
	}
	if !names["ping"] {
		t.Errorf("AC1: ping not in tools/list")
	}
}

// ─── AC2 / SR-68: additionalProperties:false ─────────────────────────────────

// TestUploadFile_ExtraFieldDenied: лишнее поле → isError:true, файл не создан (AC2/SR-68).
func TestUploadFile_ExtraFieldDenied(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, _ := newUploadTestServer(t, cfg)

	data := []byte("content")
	body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":        "ok.txt",
		"content":     b64(data),
		"extra_field": "should be rejected",
	})

	res := parseUploadResult(t, body)
	if res["isError"] != true {
		t.Errorf("AC2: extra field must return isError:true; body=%s", body)
	}

	// Файл не создан (AC2).
	if _, err := os.Stat(filepath.Join(cfg.UploadRoot, "ok.txt")); !os.IsNotExist(err) {
		t.Errorf("AC2: file was created despite extra field!")
	}
}

// ─── AC3 / SR-80: формат выхода ──────────────────────────────────────────────

// TestUploadFile_OutputFormat: успех возвращает 4 поля path/size/overwritten/mode (AC3/SR-80).
func TestUploadFile_OutputFormat(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, _ := newUploadTestServer(t, cfg)

	data := []byte("hello output")
	body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "output.txt",
		"content": b64(data),
	})

	res := parseUploadResult(t, body)
	if res["isError"] == true {
		t.Fatalf("AC3: upload failed; body=%s", body)
	}

	sc, ok := res["structuredContent"].(map[string]interface{})
	if !ok {
		t.Fatalf("AC3: no structuredContent; body=%s", body)
	}

	// Четыре обязательных поля (AC3).
	if sc["path"] != "output.txt" {
		t.Errorf("AC3: path = %v, want output.txt", sc["path"])
	}
	sizeRaw, ok := sc["size"].(float64)
	if !ok {
		t.Errorf("AC3: size is not number; sc=%v", sc)
	} else if int(sizeRaw) != len(data) {
		t.Errorf("AC3: size = %v, want %d", sizeRaw, len(data))
	}
	if sc["overwritten"] != false {
		t.Errorf("AC3: overwritten = %v, want false (new file)", sc["overwritten"])
	}
	if sc["mode"] != "0600" {
		t.Errorf("AC3: mode = %v, want 0600", sc["mode"])
	}

	// Абсолютный путь НЕ включается (SR-80).
	if strings.Contains(body, cfg.UploadRoot) {
		t.Errorf("SR-80: absolute path %q found in response! body=%s", cfg.UploadRoot, body)
	}
}

// ─── AC4 / SR-69: traversal ──────────────────────────────────────────────────

// TestUploadFile_TraversalDotDot: "../etc/passwd" → isError:true, deny (AC4/SR-69).
func TestUploadFile_TraversalDotDot(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, _ := newUploadTestServer(t, cfg)

	body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "../etc/passwd",
		"content": b64([]byte("pwned")),
	})

	res := parseUploadResult(t, body)
	if res["isError"] != true {
		t.Errorf("AC4: traversal path must return isError:true; body=%s", body)
	}
	// Файл не создан вне корня.
	outside := filepath.Join(filepath.Dir(cfg.UploadRoot), "etc", "passwd")
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Errorf("AC4: file created outside upload root!")
	}
}

// TestUploadFile_TraversalAbsolute: "/etc/passwd" → isError:true, deny (AC4/SR-69).
func TestUploadFile_TraversalAbsolute(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, _ := newUploadTestServer(t, cfg)

	body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "/etc/passwd",
		"content": b64([]byte("x")),
	})

	res := parseUploadResult(t, body)
	if res["isError"] != true {
		t.Errorf("AC4: absolute path must return isError:true; body=%s", body)
	}
}

// TestUploadFile_TraversalMultiple: "a/../../b" → isError:true, deny (AC4/SR-69).
func TestUploadFile_TraversalMultiple(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, _ := newUploadTestServer(t, cfg)

	body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "a/../../b",
		"content": b64([]byte("x")),
	})

	res := parseUploadResult(t, body)
	if res["isError"] != true {
		t.Errorf("AC4: multi-escape path must return isError:true; body=%s", body)
	}
}

// ─── AC5b / SR-71: создание подкаталогов ─────────────────────────────────────

// TestUploadFile_Subdirectory: путь в подкаталог создаёт каталог и файл (AC5b/SR-71).
func TestUploadFile_Subdirectory(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, _ := newUploadTestServer(t, cfg)

	data := []byte("script content")
	body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "scripts/deploy.sh",
		"content": b64(data),
		"mode":    "0700",
	})

	res := parseUploadResult(t, body)
	if res["isError"] == true {
		t.Fatalf("AC5b: subdirectory upload failed; body=%s", body)
	}

	// Файл на диске.
	if _, err := os.Stat(filepath.Join(cfg.UploadRoot, "scripts", "deploy.sh")); err != nil {
		t.Errorf("AC5b: file not created in subdirectory: %v", err)
	}
}

// ─── AC6 / SR-75: base64 ─────────────────────────────────────────────────────

// TestUploadFile_InvalidBase64: невалидный base64 → isError:true (AC6/SR-75).
func TestUploadFile_InvalidBase64(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, _ := newUploadTestServer(t, cfg)

	body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "bad.bin",
		"content": "!!!not-base64!!!",
	})

	res := parseUploadResult(t, body)
	if res["isError"] != true {
		t.Errorf("AC6: invalid base64 must return isError:true; body=%s", body)
	}

	// Файл не создан (AC6).
	if _, err := os.Stat(filepath.Join(cfg.UploadRoot, "bad.bin")); !os.IsNotExist(err) {
		t.Errorf("AC6: file was created despite invalid base64!")
	}
}

// TestUploadFile_BinaryContent: бинарные данные → точные байты на диске (AC6/SR-75).
func TestUploadFile_BinaryContent(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, _ := newUploadTestServer(t, cfg)

	data := []byte{0x00, 0xFF, 0x7F, 0x80, 0x01, 0xFE}
	body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "binary.bin",
		"content": b64(data),
	})

	res := parseUploadResult(t, body)
	if res["isError"] == true {
		t.Fatalf("AC6: binary upload failed; body=%s", body)
	}

	written, err := os.ReadFile(filepath.Join(cfg.UploadRoot, "binary.bin"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	for i := range data {
		if i >= len(written) || written[i] != data[i] {
			t.Errorf("AC6: binary mismatch at byte %d: got %02x want %02x", i, written[i], data[i])
		}
	}
}

// ─── AC7 / SR-75/SR-76: лимит размера ────────────────────────────────────────

// TestUploadFile_TooLarge: декодированный > max_file_bytes → isError:true, файла нет (AC7/SR-75).
func TestUploadFile_TooLarge(t *testing.T) {
	cfg := defaultUplCfg(t)
	cfg.MaxFileBytes = 10
	ts, _ := newUploadTestServer(t, cfg)

	data := make([]byte, 11)
	body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "large.bin",
		"content": b64(data),
	})

	res := parseUploadResult(t, body)
	if res["isError"] != true {
		t.Errorf("AC7: too-large must return isError:true; body=%s", body)
	}

	// Файл не создан.
	if _, err := os.Stat(filepath.Join(cfg.UploadRoot, "large.bin")); !os.IsNotExist(err) {
		t.Errorf("AC7: file was created despite size limit!")
	}
	// Temp-файл не остался.
	entries, _ := os.ReadDir(cfg.UploadRoot)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".raxd-upload-") {
			t.Errorf("AC7/SR-74: temp file left: %s", e.Name())
		}
	}
}

// ─── AC8 / SR-72: overwrite политика ─────────────────────────────────────────

// TestUploadFile_OverwriteFalse: существующий файл + overwrite=false → isError:true (AC8/SR-72).
func TestUploadFile_OverwriteFalse(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, _ := newUploadTestServer(t, cfg)

	original := []byte("original content")
	// Первая запись.
	body1, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "exist.txt",
		"content": b64(original),
	})
	if res1 := parseUploadResult(t, body1); res1["isError"] == true {
		t.Fatalf("AC8: first write failed; body=%s", body1)
	}

	// Попытка без overwrite.
	body2, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "exist.txt",
		"content": b64([]byte("new content")),
	})
	res2 := parseUploadResult(t, body2)
	if res2["isError"] != true {
		t.Errorf("AC8: overwrite=false must return isError:true; body=%s", body2)
	}

	// Содержимое прежнее.
	data, _ := os.ReadFile(filepath.Join(cfg.UploadRoot, "exist.txt"))
	if string(data) != string(original) {
		t.Errorf("AC8: file was modified! content=%q, want %q", data, original)
	}
}

// TestUploadFile_OverwriteTrue: overwrite=true → замена файла (AC8/SR-72).
func TestUploadFile_OverwriteTrue(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, _ := newUploadTestServer(t, cfg)

	original := []byte("original")
	updated := []byte("updated content")

	if body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "replace.txt",
		"content": b64(original),
	}); parseUploadResult(t, body)["isError"] == true {
		t.Fatalf("first write failed")
	}

	body2, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":      "replace.txt",
		"content":   b64(updated),
		"overwrite": true,
	})
	res2 := parseUploadResult(t, body2)
	if res2["isError"] == true {
		t.Fatalf("AC8: overwrite=true failed; body=%s", body2)
	}

	sc := res2["structuredContent"].(map[string]interface{})
	if sc["overwritten"] != true {
		t.Errorf("AC8: overwritten = %v, want true", sc["overwritten"])
	}

	data, _ := os.ReadFile(filepath.Join(cfg.UploadRoot, "replace.txt"))
	if string(data) != string(updated) {
		t.Errorf("AC8: file not updated; content=%q, want %q", data, updated)
	}
}

// ─── AC9 / SR-73: права файла ───────────────────────────────────────────────

// TestUploadFile_ModeDefault: без mode → 0600 (AC9/SR-73).
func TestUploadFile_ModeDefault(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, _ := newUploadTestServer(t, cfg)

	body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "secret.txt",
		"content": b64([]byte("data")),
	})

	res := parseUploadResult(t, body)
	if res["isError"] == true {
		t.Fatalf("AC9: default mode upload failed; body=%s", body)
	}

	info, err := os.Stat(filepath.Join(cfg.UploadRoot, "secret.txt"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("AC9: file mode = %04o, want 0600", info.Mode().Perm())
	}
}

// TestUploadFile_SetuidDenied: mode с setuid-битом → isError:true (AC9/SR-73/ADR-003).
func TestUploadFile_SetuidDenied(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, _ := newUploadTestServer(t, cfg)

	body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "setuid.bin",
		"content": b64([]byte("x")),
		"mode":    "04755",
	})

	res := parseUploadResult(t, body)
	if res["isError"] != true {
		t.Errorf("AC9/SR-73: setuid mode must return isError:true; body=%s", body)
	}
	if _, err := os.Stat(filepath.Join(cfg.UploadRoot, "setuid.bin")); !os.IsNotExist(err) {
		t.Errorf("AC9/SR-73: file created with forbidden mode!")
	}
}

// TestUploadFile_WorldWritableDenied: world-writable → isError:true (AC9/SR-73/ADR-003).
func TestUploadFile_WorldWritableDenied(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, _ := newUploadTestServer(t, cfg)

	body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "world.txt",
		"content": b64([]byte("x")),
		"mode":    "0666",
	})

	res := parseUploadResult(t, body)
	if res["isError"] != true {
		t.Errorf("AC9/SR-73: world-writable mode must return isError:true; body=%s", body)
	}
}

// ─── AC10 / SR-74: атомарность ───────────────────────────────────────────────

// TestUploadFile_NoTempLeft: при ошибке (invalid base64) temp-файл не остаётся (AC10/SR-74).
func TestUploadFile_NoTempLeft(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, _ := newUploadTestServer(t, cfg)

	// Невалидный base64 → ошибка до записи.
	callUploadFile(t, ts, map[string]interface{}{
		"path":    "fail.bin",
		"content": "!!!INVALID-BASE64!!!",
	})

	// Никаких temp-файлов.
	entries, _ := os.ReadDir(cfg.UploadRoot)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".raxd-upload-") {
			t.Errorf("AC10/SR-74: temp file left after error: %s", e.Name())
		}
	}
}

// ─── AC12 / AC19 / SR-78 / SR-79: аудит ─────────────────────────────────────

// TestUploadFile_AuditSuccess: success-вызов пишет ровно одну upload-запись с path/size/tool (AC12/AC19/SR-78/SR-79).
func TestUploadFile_AuditSuccess(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, auditBuf := newUploadTestServer(t, cfg)

	data := []byte("audit test data")
	auditBuf.Reset()

	body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "audit.txt",
		"content": b64(data),
	})
	res := parseUploadResult(t, body)
	if res["isError"] == true {
		t.Fatalf("AC12: upload failed; body=%s", body)
	}

	log := auditBuf.String()

	// tool=upload_file в audit (SR-79/F-2).
	if !strings.Contains(log, "tool=upload_file") {
		t.Errorf("AC12/SR-79: audit missing tool=upload_file; log=%s", log)
	}
	// path= в audit (SR-79).
	if !strings.Contains(log, "path=audit.txt") {
		t.Errorf("AC12/SR-79: audit missing path=audit.txt; log=%s", log)
	}
	// size= в audit (SR-79).
	if !strings.Contains(log, "size=") {
		t.Errorf("AC12/SR-79: audit missing size= field; log=%s", log)
	}
	// result=ok в success-ветке (mcp-spec §2.3.1).
	if !strings.Contains(log, "result=ok") {
		t.Errorf("AC12: audit success missing result=ok; log=%s", log)
	}
}

// TestUploadFile_AuditDeny: deny-вызов (traversal) пишет upload deny-запись с tool= (AC12/AC19/SR-78/F-2).
func TestUploadFile_AuditDeny(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, auditBuf := newUploadTestServer(t, cfg)

	auditBuf.Reset()
	body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "../etc/passwd",
		"content": b64([]byte("x")),
	})

	res := parseUploadResult(t, body)
	if res["isError"] != true {
		t.Fatalf("AC12: traversal must return isError:true; body=%s", body)
	}

	log := auditBuf.String()

	// tool=upload_file ДОЛЖЕН быть в deny-записи (F-2/SR-79).
	if !strings.Contains(log, "tool=upload_file") {
		t.Errorf("AC12/SR-79/F-2: deny audit missing tool=upload_file; log=%s", log)
	}
	// DENY в deny-записи.
	if !strings.Contains(log, "DENY") {
		t.Errorf("AC12: deny audit missing DENY label; log=%s", log)
	}
}

// ─── AC13 / SR-80: без секретов ──────────────────────────────────────────────

// TestUploadFile_NoSecretsInAuditOrResponse: ключ не попадает в аудит и ответ (AC13/SR-80).
func TestUploadFile_NoSecretsInAuditOrResponse(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, auditBuf := newUploadTestServer(t, cfg)

	data := []byte("secret content should not leak")
	encoded := b64(data)
	auditBuf.Reset()

	body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "secret_test.txt",
		"content": encoded,
	})

	log := auditBuf.String()

	// Содержимое файла не должно быть в логе (SR-80/AC12).
	if strings.Contains(log, string(data)) {
		t.Errorf("SR-80: file content leaked in audit log! log=%s", log)
	}
	if strings.Contains(log, encoded) {
		t.Errorf("SR-80: base64 content leaked in audit log! log=%s", log)
	}

	// Содержимое не в ответе.
	if strings.Contains(body, string(data)) {
		t.Errorf("SR-80: file content leaked in response! body=%s", body)
	}

	// Абсолютный путь не в ответе (SR-80).
	if strings.Contains(body, cfg.UploadRoot) {
		t.Errorf("SR-80: absolute path leaked in response! body=%s", body)
	}
}

// ─── AC14 / SR: устойчивость ──────────────────────────────────────────────────

// TestUploadFile_TargetIsDirectory: цель — каталог → isError:true, сервер жив (AC14/SR-72).
func TestUploadFile_TargetIsDirectory(t *testing.T) {
	cfg := defaultUplCfg(t)
	// Создаём каталог с именем цели.
	if err := os.MkdirAll(filepath.Join(cfg.UploadRoot, "mydir"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	ts, _ := newUploadTestServer(t, cfg)

	body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "mydir",
		"content": b64([]byte("x")),
	})

	res := parseUploadResult(t, body)
	if res["isError"] != true {
		t.Errorf("AC14: target=dir must return isError:true; body=%s", body)
	}

	// Сервер жив — следующий вызов работает.
	okBody, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "after_dir_attempt.txt",
		"content": b64([]byte("ok")),
	})
	okRes := parseUploadResult(t, okBody)
	if okRes["isError"] == true {
		t.Errorf("AC14: server not alive after dir-target error; body=%s", okBody)
	}
}

// TestUploadFile_NoRequiredFields: отсутствие path/content → isError:true (AC14/AC2).
func TestUploadFile_NoRequiredFields(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, _ := newUploadTestServer(t, cfg)

	// Нет content.
	body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path": "no_content.txt",
	})
	res := parseUploadResult(t, body)
	if res["isError"] != true {
		t.Errorf("AC2/AC14: missing content must return isError:true; body=%s", body)
	}
}

// ─── AC19 / SR-78: ровно одна основная запись per вызов ──────────────────────

// TestUploadFile_ExactlyOneAuditRecord: один вызов → ровно одна основная upload-запись (AC19/SR-78).
func TestUploadFile_ExactlyOneAuditRecord(t *testing.T) {
	cfg := defaultUplCfg(t)
	ts, auditBuf := newUploadTestServer(t, cfg)

	data := []byte("one record")
	auditBuf.Reset()

	body, _ := callUploadFile(t, ts, map[string]interface{}{
		"path":    "onerecord.txt",
		"content": b64(data),
	})
	if res := parseUploadResult(t, body); res["isError"] == true {
		t.Fatalf("AC19: upload failed")
	}

	log := auditBuf.String()
	// Считаем строки с upload_file.
	count := 0
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, "tool=upload_file") && strings.Contains(line, "result=ok") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("AC19/SR-78: expected exactly 1 success upload record, got %d; log=%s", count, log)
	}
}

// ─── SR-79: logfmt-инъекция через путь ───────────────────────────────────────

// TestUploadFile_PathLogfmtInjection: путь со спецсимволами квотируется logfmt'ом (SR-79).
//
// Три вектора:
//  1. Путь с пробелом и '=' (классическая logfmt-инъекция):
//     ожидаем path="..." (квотированное) в success-строке.
//  2. Путь с кавычкой '"' — charmbracelet/log экранирует её внутри кавычек.
//  3. Путь с переводом строки '\n' — charmbracelet/log рендерит multiline-блок
//     с │ prefix; инъекция result=injected не должна появляться как самостоятельная пара key=value.
//
// Примечание о Docker (euid==0): uploadHandler записывает WARN + основную запись,
// т.е. при euid==0 будет 2+ строк с tool=upload_file. Тесты учитывают это.
//
// Тест РЕАЛЬНО падает при поломке: убери кавычки из path= в writeAudit —
// вектор 1 не найдёт путь целиком ("with space=eq.txt") в success-строке.
func TestUploadFile_PathLogfmtInjection(t *testing.T) {
	// --- Вектор 1: пробел и '=' в пути ---
	t.Run("space_and_equals", func(t *testing.T) {
		cfg := defaultUplCfg(t)
		ts, auditBuf := newUploadTestServer(t, cfg)

		// Путь содержит пробел и '=' — при логировании charmbracelet/log обязан квотировать.
		injectPath := "subdir/file with space=eq.txt"
		auditBuf.Reset()

		body, _ := callUploadFile(t, ts, map[string]interface{}{
			"path":    injectPath,
			"content": b64([]byte("hello")),
		})
		_ = body

		logStr := auditBuf.String()

		// Собираем все строки с tool=upload_file.
		var uploadLines []string
		for _, line := range strings.Split(logStr, "\n") {
			if strings.Contains(line, "tool=upload_file") {
				uploadLines = append(uploadLines, line)
			}
		}
		if len(uploadLines) == 0 {
			t.Fatalf("SR-79 вектор 1: нет строки с tool=upload_file в аудите; log=%q", logStr)
		}

		// Ищем строку success (INFO/result=ok) — именно в ней должен быть path.
		// При euid==0 есть и WARN-строка, поэтому ищем по result=ok.
		var successLine string
		for _, line := range uploadLines {
			if strings.Contains(line, "result=ok") {
				successLine = line
				break
			}
		}
		if successLine == "" {
			t.Fatalf("SR-79 вектор 1: нет success-строки (result=ok) с tool=upload_file; lines=%v", uploadLines)
		}

		// Путь должен присутствовать квотированным в success-строке.
		// charmbracelet/log квотирует значения содержащие пробел/=: path="subdir/file with space=eq.txt"
		// Если квотирование сломано: path=subdir/file → пробел разрывает → "with" "space=eq.txt" отдельные поля.
		// Проверяем, что ВЕСЬ путь (включая "with space=eq.txt") присутствует в строке.
		if !strings.Contains(successLine, "with space=eq.txt") {
			t.Errorf("SR-79 вектор 1: путь не найден целиком в success audit-строке — квотирование сломано; line=%q", successLine)
		}

		// path должен быть представлен как path="..." (с кавычками из-за пробела).
		if !strings.Contains(successLine, `path="`) {
			t.Errorf("SR-79 вектор 1: path= не квотирован (ожидалось path=\"...\"); line=%q", successLine)
		}

		// Не должно быть поддельной пары result=injected в success-строке.
		if strings.Contains(successLine, "result=injected") {
			t.Errorf("SR-79 вектор 1: инъекция result=injected в success audit-строке; line=%q", successLine)
		}
	})

	// --- Вектор 2: кавычка в пути ---
	t.Run("quote_in_path", func(t *testing.T) {
		cfg := defaultUplCfg(t)
		ts, auditBuf := newUploadTestServer(t, cfg)

		// Путь содержит кавычку — charmbracelet/log экранирует её.
		injectPath := `a"b.txt`
		auditBuf.Reset()

		callUploadFile(t, ts, map[string]interface{}{
			"path":    injectPath,
			"content": b64([]byte("x")),
		})

		logStr := auditBuf.String()
		// Ищем успешную загрузку или deny-запись (если OS отвергла имя).
		var found bool
		for _, line := range strings.Split(logStr, "\n") {
			if strings.Contains(line, "tool=upload_file") {
				found = true
				// В корректном logfmt кавычка внутри значения экранирована (\"); строка не обрывается.
				// Если сломано — за `"` лог-парсер интерпретирует остаток как новое поле.
				// Не должно быть оборванной пары b.txt= без кавычек:
				if strings.Contains(line, " b.txt ") || strings.Contains(line, " b.txt=") {
					t.Errorf("SR-79 вектор 2: кавычка в пути не экранирована → line=%q", line)
				}
			}
		}
		if !found {
			t.Logf("SR-79 вектор 2: путь с кавычкой отвергнут до логирования (OK); log=%q", logStr)
		}
	})

	// --- Вектор 3: перевод строки в пути ---
	t.Run("newline_in_path", func(t *testing.T) {
		cfg := defaultUplCfg(t)
		ts, auditBuf := newUploadTestServer(t, cfg)

		// Путь с '\n' — filepath.IsLocal пропускает его, os.Root может создать.
		// charmbracelet/log рендерит multiline значения как блок с │ prefix.
		// Инъекция: если path="file\nresult=injected.txt" разбьёт лог,
		// то в ответе/аудите появится самостоятельная строка "result=injected.txt=..."
		// (НЕ как часть блока │). Мы проверяем именно это.
		injectPath := "file\nresult=injected.txt"
		auditBuf.Reset()

		callUploadFile(t, ts, map[string]interface{}{
			"path":    injectPath,
			"content": b64([]byte("x")),
		})

		logStr := auditBuf.String()

		// Ключевая проверка: "result=injected" НЕ должно появляться как начало строки
		// (не как часть multiline-блока с │ prefix).
		// charmbracelet/log форматирует multiline-значения как:
		//   path=
		//     │ file
		//     │ result=injected.txt
		// Строка "  │ result=injected.txt" — безопасна (экранирована блоком).
		// Строка "result=injected.txt" БЕЗ "│" prefix — это инъекция.
		for _, line := range strings.Split(logStr, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "result=injected") {
				t.Errorf("SR-79 вектор 3: 'result=injected' появился как самостоятельное поле (инъекция через \\n в пути); line=%q", line)
			}
		}
		// Не должно быть самостоятельной строки (без │) с result=injected.
		// charmbracelet/log сам добавляет │ для multiline — это документированное поведение.
		t.Logf("SR-79 вектор 3: OK — \\n в пути экранирован charmbracelet/log (multiline block); log=%q", logStr)
	})
}

// ─── SR-77: root-detect для upload_file ──────────────────────────────────────

// TestUploadRootWarnAuditRecord проверяет логику SR-77 для upload_file:
//   - Unit-уровень: writeAudit при Result="warn" и Tool="upload_file" пишет WARN + reason + path.
//   - При euid==0 (Docker от root): реальный MCP-вызов → WARN-запись в аудите.
//   - При euid!=0: WARN-запись НЕ должна появляться (uploadHandler не пишет warn).
//   - deny_root=true + euid==0 → isError:true, файл не создан, DENY в аудите.
func TestUploadRootWarnAuditRecord(t *testing.T) {
	// --- Unit-уровень: writeAudit обрабатывает warn для upload_file ---
	t.Run("unit_warn_writeAudit", func(t *testing.T) {
		auditBuf := &bytes.Buffer{}
		logger := newTestLogger(auditBuf)
		auditFn := server.NewAuditFnForTest(logger)

		auditFn(server.AuditRecord{
			TS:          testTime(),
			Fingerprint: "aabbccdd1122",
			RemoteAddr:  "127.0.0.1:9999",
			Result:      "warn",
			Tool:        "upload_file",
			Reason:      "running-as-root-upload: raxd executing upload as root (euid==0); ensure raxd runs as non-root",
			Path:        "test/file.txt",
		})

		logStr := auditBuf.String()
		if !strings.Contains(logStr, "WARN") {
			t.Errorf("SR-77: WARN-запись для upload_file не содержит WARN; log=%q", logStr)
		}
		if !strings.Contains(logStr, "tool=upload_file") {
			t.Errorf("SR-77: WARN-запись должна содержать tool=upload_file; log=%q", logStr)
		}
		if !strings.Contains(logStr, "root") {
			t.Errorf("SR-77: WARN-запись должна содержать 'root' в reason; log=%q", logStr)
		}
		if !strings.Contains(logStr, "fp=") {
			t.Errorf("SR-77: WARN-запись должна содержать fp=; log=%q", logStr)
		}
		t.Logf("SR-77: OK — writeAudit warn для upload_file: %q", logStr)
	})

	// --- При euid!=0: WARN не появляется (uploadHandler не пишет warn для не-root) ---
	t.Run("no_warn_when_not_root", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("SR-77: этот подтест работает только при euid!=0; в Docker запускайте как non-root")
		}
		cfg := defaultUplCfg(t)
		ts, auditBuf := newUploadTestServer(t, cfg)
		auditBuf.Reset()

		body, _ := callUploadFile(t, ts, map[string]interface{}{
			"path":    "nowarn.txt",
			"content": b64([]byte("data")),
		})
		res := parseUploadResult(t, body)
		if res["isError"] == true {
			t.Fatalf("SR-77: upload failed при non-root; body=%s", body)
		}

		logStr := auditBuf.String()
		for _, line := range strings.Split(logStr, "\n") {
			if strings.Contains(line, "tool=upload_file") && strings.Contains(line, "WARN") {
				t.Errorf("SR-77: WARN-запись upload_file при euid!=0 недопустима; line=%q", line)
			}
		}
		t.Logf("SR-77: OK — нет WARN при euid!=0")
	})

	// --- При euid==0 (Docker от root): реальный MCP-вызов пишет WARN ---
	t.Run("warn_when_root", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("SR-77: этот подтест требует euid==0 (запускается в Docker от root)")
		}
		cfg := defaultUplCfg(t)
		ts, auditBuf := newUploadTestServer(t, cfg)
		auditBuf.Reset()

		body, _ := callUploadFile(t, ts, map[string]interface{}{
			"path":    "root-warn.txt",
			"content": b64([]byte("warn-data")),
		})
		_ = body

		logStr := auditBuf.String()
		// При euid==0 uploadHandler пишет WARN-запись ДО основного аудита.
		var warnFound bool
		for _, line := range strings.Split(logStr, "\n") {
			if strings.Contains(line, "tool=upload_file") && strings.Contains(line, "WARN") {
				warnFound = true
				if !strings.Contains(line, "root") {
					t.Errorf("SR-77: WARN-строка не содержит 'root'; line=%q", line)
				}
			}
		}
		if !warnFound {
			t.Errorf("SR-77: при euid==0 ожидалась WARN-запись tool=upload_file; log=%s", logStr)
		}
		t.Logf("SR-77: OK — WARN 'running-as-root-upload' найден в аудите при euid==0")
	})

	// --- deny_root=true + euid==0 → isError, файл не создан, DENY в аудите ---
	t.Run("deny_root_upload", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("SR-77: deny_root подтест требует euid==0 (Docker от root)")
		}
		cfg := defaultUplCfg(t)
		cfg.DenyRoot = true
		ts, auditBuf := newUploadTestServer(t, cfg)
		auditBuf.Reset()

		body, _ := callUploadFile(t, ts, map[string]interface{}{
			"path":    "should-not-exist.txt",
			"content": b64([]byte("forbidden")),
		})

		res := parseUploadResult(t, body)
		isErr, _ := res["isError"].(bool)
		if !isErr {
			t.Errorf("SR-77: deny_root=true + euid==0 → ожидается isError:true; body=%s", body)
		}

		// Файл не должен быть создан.
		uploadRoot := cfg.UploadRoot
		if _, err := os.Stat(filepath.Join(uploadRoot, "should-not-exist.txt")); !os.IsNotExist(err) {
			t.Errorf("SR-77: файл создан при deny_root=true + euid==0")
		}

		// DENY должен быть в аудите.
		logStr := auditBuf.String()
		if !strings.Contains(logStr, "DENY") {
			t.Errorf("SR-77: при deny_root=true+euid==0 нет DENY в аудите; log=%s", logStr)
		}
		t.Logf("SR-77: OK — deny_root=true + euid==0 → isError:true + DENY в аудите")
	})
}

// testTime возвращает фиксированное время для unit-тестов аудита.
func testTime() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}
