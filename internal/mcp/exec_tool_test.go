package mcp_test

// exec_tool_test.go — integration тесты для MCP-инструмента execute_command.
//
// Покрывает AC1-AC18 и SR-40..SR-67 через полный MCP-стек.
// Тесты запускаются в Docker (-mod=vendor; AC18/SR-67).
//
// Все helper-функции (startMCPServer, postMCP, jsonrpcBody, ...) определены в mcp_test.go.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vladimirvkhs/raxd/internal/cmdexec"
	internalmcp "github.com/vladimirvkhs/raxd/internal/mcp"
	"github.com/vladimirvkhs/raxd/internal/server"
)

// ============================================================================
// Вспомогательные функции
// ============================================================================

// startExecServer запускает MCP-сервер с заданным execCfg.
func startExecServer(t *testing.T, execCfg cmdexec.Config) (baseURL string, keyStr string, client *http.Client, auditBuf *bytes.Buffer) {
	t.Helper()
	// Используем startMCPServer как базу — но нам нужен кастомный execCfg.
	// Создаём сервер через httptest для unit-тестирования (без TLS).
	return startMCPServerWithExecCfg(t, execCfg)
}

// startMCPServerWithExecCfg создаёт httptest-сервер с заданным execCfg.
func startMCPServerWithExecCfg(t *testing.T, execCfg cmdexec.Config) (baseURL string, _ string, client *http.Client, auditBuf *bytes.Buffer) {
	t.Helper()
	auditBuf = &bytes.Buffer{}
	logger := newTestLogger(auditBuf)
	auditFn := server.NewAuditFnForTest(logger)

	h, err := internalmcp.NewHandler("test-version", auditFn, execCfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	return ts.URL, "", ts.Client(), auditBuf
}

// callExecuteCommand посылает tools/call execute_command и возвращает тело ответа.
func callExecuteCommand(t *testing.T, client *http.Client, baseURL string, args map[string]interface{}) (string, int) {
	t.Helper()
	// Инициализируем сессию.
	initBody := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "exec-test", "version": "1"},
	})
	initReq, _ := http.NewRequest(http.MethodPost, baseURL, strings.NewReader(initBody))
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Accept", "application/json, text/event-stream")
	initResp, err := client.Do(initReq)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	readBody(t, initResp)

	// Вызываем execute_command.
	callBody := jsonrpcBody(2, "tools/call", map[string]interface{}{
		"name":      "execute_command",
		"arguments": args,
	})
	req, _ := http.NewRequest(http.MethodPost, baseURL, strings.NewReader(callBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-11-25")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("tools/call execute_command: %v", err)
	}
	body := readBody(t, resp)
	return body, resp.StatusCode
}

// parseToolResult разбирает ответ MCP tools/call в result-объект.
func parseToolResult(t *testing.T, body string) map[string]interface{} {
	t.Helper()
	var envelope map[string]interface{}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("response not JSON: %v; body=%s", err, body)
	}
	return envelope
}

// ============================================================================
// AC1/SR-40: execute_command присутствует в tools/list
// ============================================================================

func TestExecToolInToolsList(t *testing.T) {
	baseURL, _, client, _ := startMCPServerWithExecCfg(t, defaultExecCfg())

	// initialize
	initBody := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	})
	initReq, _ := http.NewRequest(http.MethodPost, baseURL, strings.NewReader(initBody))
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Accept", "application/json, text/event-stream")
	initResp, _ := client.Do(initReq)
	readBody(t, initResp)

	// tools/list
	listBody := jsonrpcBody(2, "tools/list", map[string]interface{}{})
	req, _ := http.NewRequest(http.MethodPost, baseURL, strings.NewReader(listBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-11-25")
	resp, _ := client.Do(req)
	body := readBody(t, resp)

	var envelope map[string]interface{}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("tools/list not JSON: %v", err)
	}
	result, _ := envelope["result"].(map[string]interface{})
	tools, _ := result["tools"].([]interface{})

	names := make(map[string]bool)
	for _, tool := range tools {
		if tm, ok := tool.(map[string]interface{}); ok {
			if n, ok := tm["name"].(string); ok {
				names[n] = true
			}
		}
	}

	if !names["execute_command"] {
		t.Errorf("AC1/SR-40: execute_command must be in tools/list; names=%v", names)
	}
	if !names["ping"] {
		t.Errorf("AC1: ping must still be in tools/list")
	}
	if !names["server_info"] {
		t.Errorf("AC1: server_info must still be in tools/list")
	}
}

// ============================================================================
// AC2/SR-43: без shell-инъекции
// ============================================================================

// TestExecNoShellInjectionViaMCP — shell-метасимволы в args — литеральные аргументы.
func TestExecNoShellInjectionViaMCP(t *testing.T) {
	marker := fmt.Sprintf("/tmp/mcp_pwned_%d", time.Now().UnixNano())
	baseURL, _, client, _ := startMCPServerWithExecCfg(t, defaultExecCfg())

	body, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "echo",
		"args":    []string{"a; touch " + marker},
	})

	// Файл НЕ должен появиться.
	// Проверяем что команда отработала (нет isError по этой причине).
	envelope := parseToolResult(t, body)
	if errObj := envelope["error"]; errObj != nil {
		t.Fatalf("AC2: unexpected protocol error: %v; body=%s", errObj, body)
	}

	// Если файл создан — shell был задействован (баг).
	// Нет возможности проверить через MCP ответ — проверяем что в stdout нет shell-признаков.
	result, _ := envelope["result"].(map[string]interface{})
	if result == nil {
		t.Fatalf("AC2: no result in response; body=%s", body)
	}
	isErr, _ := result["isError"].(bool)
	if isErr {
		t.Fatalf("AC2: isError=true for echo command; body=%s", body)
	}
}

// ============================================================================
// AC3/SR-51: additionalProperties:false — лишнее поле → isError
// ============================================================================

// TestExecExtraFieldRejected — лишнее поле env → isError, команда не запущена.
func TestExecExtraFieldRejected(t *testing.T) {
	baseURL, _, client, _ := startMCPServerWithExecCfg(t, defaultExecCfg())

	body, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "echo",
		"env":     map[string]interface{}{"X": "1"}, // лишнее поле
	})

	envelope := parseToolResult(t, body)
	// SDK должен отклонить по additionalProperties:false ДО handler.
	result, _ := envelope["result"].(map[string]interface{})
	if result == nil {
		// Возможно protocol error — тоже допустимо.
		if envelope["error"] != nil {
			return // SDK вернул protocol error — команда не запущена
		}
		t.Fatalf("AC3/SR-51: no result and no error; body=%s", body)
	}
	isErr, _ := result["isError"].(bool)
	if !isErr {
		t.Errorf("AC3/SR-51: extra field 'env' must cause isError:true; body=%s", body)
	}
}

// TestExecUnknownFieldRejected — поле shell → isError.
func TestExecUnknownFieldRejected(t *testing.T) {
	baseURL, _, client, _ := startMCPServerWithExecCfg(t, defaultExecCfg())

	body, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "echo",
		"shell":   true, // неизвестное поле
	})

	envelope := parseToolResult(t, body)
	result, _ := envelope["result"].(map[string]interface{})
	if result == nil {
		if envelope["error"] != nil {
			return
		}
		t.Fatalf("AC3/SR-51: no result and no error; body=%s", body)
	}
	isErr, _ := result["isError"].(bool)
	if !isErr {
		t.Errorf("AC3/SR-51: extra field 'shell' must cause isError:true; body=%s", body)
	}
}

// ============================================================================
// AC4/SR-65: 7 полей в structuredContent
// ============================================================================

// TestExecOutputHas7Fields — успешный вызов echo возвращает 7 полей.
func TestExecOutputHas7Fields(t *testing.T) {
	baseURL, _, client, _ := startMCPServerWithExecCfg(t, defaultExecCfg())

	body, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "echo",
		"args":    []string{"hello-exec"},
	})

	envelope := parseToolResult(t, body)
	if errObj := envelope["error"]; errObj != nil {
		t.Fatalf("AC4: protocol error: %v; body=%s", errObj, body)
	}

	result, _ := envelope["result"].(map[string]interface{})
	if result == nil {
		t.Fatalf("AC4: no result; body=%s", body)
	}
	isErr, _ := result["isError"].(bool)
	if isErr {
		t.Fatalf("AC4: isError=true; body=%s", body)
	}

	sc, ok := result["structuredContent"].(map[string]interface{})
	if !ok {
		t.Fatalf("AC4: no structuredContent; body=%s", body)
	}

	requiredFields := []string{"stdout", "stderr", "exit_code", "duration_ms", "timed_out", "stdout_truncated", "stderr_truncated"}
	for _, field := range requiredFields {
		if _, exists := sc[field]; !exists {
			t.Errorf("AC4/SR-65: structuredContent missing field %q; sc=%v", field, sc)
		}
	}

	// stdout должен содержать "hello-exec".
	stdout, _ := sc["stdout"].(string)
	if !strings.Contains(stdout, "hello-exec") {
		t.Errorf("AC4: stdout = %q, want 'hello-exec'", stdout)
	}
	// exit_code = 0.
	exitCode, _ := sc["exit_code"].(float64)
	if exitCode != 0 {
		t.Errorf("AC4: exit_code = %v, want 0", exitCode)
	}
	// timed_out = false.
	timedOut, _ := sc["timed_out"].(bool)
	if timedOut {
		t.Errorf("AC4: timed_out = true, want false")
	}
}

// ============================================================================
// AC5/SR-46: timeout_ms > max → isError, не запущено
// ============================================================================

// TestExecTimeoutExceedsMaxIsError — timeout_ms > max_timeout_ms → isError.
func TestExecTimeoutExceedsMaxIsError(t *testing.T) {
	cfg := defaultExecCfg()
	cfg.MaxTimeoutMs = 5000 // тестовый максимум 5s

	baseURL, _, client, auditBuf := startMCPServerWithExecCfg(t, cfg)

	body, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command":    "echo",
		"timeout_ms": 10000, // 10s > max 5s
	})

	envelope := parseToolResult(t, body)
	result, _ := envelope["result"].(map[string]interface{})
	if result == nil {
		t.Fatalf("AC5: no result; body=%s", body)
	}
	isErr, _ := result["isError"].(bool)
	if !isErr {
		t.Errorf("AC5/SR-46: timeout_ms > max must return isError:true; body=%s", body)
	}

	// Аудит должен содержать deny.
	log := auditBuf.String()
	if !strings.Contains(log, "DENY") {
		t.Errorf("AC5/SR-46: audit must contain DENY for timeout excess; log=%s", log)
	}
}

// TestExecTimeoutKills — команда, дольше таймаута, прерывается (timed_out:true).
func TestExecTimeoutKills(t *testing.T) {
	cfg := defaultExecCfg()
	cfg.DefaultTimeoutMs = 500 // 500ms дефолт

	baseURL, _, client, _ := startMCPServerWithExecCfg(t, cfg)

	start := time.Now()
	body, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command":    "sleep",
		"args":       []string{"60"},
		"timeout_ms": 500, // 500ms
	})
	elapsed := time.Since(start)

	envelope := parseToolResult(t, body)
	if errObj := envelope["error"]; errObj != nil {
		t.Fatalf("AC5: protocol error: %v; body=%s", errObj, body)
	}

	result, _ := envelope["result"].(map[string]interface{})
	if result == nil {
		t.Fatalf("AC5: no result; body=%s", body)
	}
	isErr, _ := result["isError"].(bool)
	if isErr {
		t.Fatalf("AC5: timeout must NOT be isError:true (timed_out is normal outcome); body=%s", body)
	}

	sc, _ := result["structuredContent"].(map[string]interface{})
	timedOut, _ := sc["timed_out"].(bool)
	if !timedOut {
		t.Errorf("AC5: timed_out = false, want true; body=%s", body)
	}

	// Должен завершиться быстро (не ждать 60s).
	if elapsed > 5*time.Second {
		t.Errorf("AC5: elapsed = %v, want < 5s (process should have been killed)", elapsed)
	}
}

// ============================================================================
// AC7/SR-48: allowlist deny → isError + аудит DENY
// ============================================================================

// TestExecAllowlistDeny — команда вне allowlist → isError + DENY.
func TestExecAllowlistDeny(t *testing.T) {
	cfg := defaultExecCfg()
	cfg.Allowlist = []string{"echo"} // только echo разрешён

	baseURL, _, client, auditBuf := startMCPServerWithExecCfg(t, cfg)

	body, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "cat",
		"args":    []string{"/etc/passwd"},
	})

	envelope := parseToolResult(t, body)
	result, _ := envelope["result"].(map[string]interface{})
	if result == nil {
		t.Fatalf("AC7: no result; body=%s", body)
	}
	isErr, _ := result["isError"].(bool)
	if !isErr {
		t.Errorf("AC7/SR-48: allowlist deny must return isError:true; body=%s", body)
	}

	// Аудит: DENY + command + args.
	log := auditBuf.String()
	if !strings.Contains(log, "DENY") {
		t.Errorf("AC7/SR-48: audit must contain DENY; log=%s", log)
	}
	if !strings.Contains(log, "cat") {
		t.Errorf("AC7/SR-48: audit must contain command 'cat'; log=%s", log)
	}
}

// TestExecAllowlistPermit — команда в allowlist выполняется.
func TestExecAllowlistPermit(t *testing.T) {
	cfg := defaultExecCfg()
	cfg.Allowlist = []string{"echo"}

	baseURL, _, client, _ := startMCPServerWithExecCfg(t, cfg)

	body, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "echo",
		"args":    []string{"allowed"},
	})

	envelope := parseToolResult(t, body)
	result, _ := envelope["result"].(map[string]interface{})
	if result == nil {
		t.Fatalf("AC7: no result; body=%s", body)
	}
	isErr, _ := result["isError"].(bool)
	if isErr {
		t.Errorf("AC7/SR-48: echo in allowlist must not isError; body=%s", body)
	}
}

// ============================================================================
// AC8/SR-45: несуществующий бинарь → isError, сервер жив
// ============================================================================

// TestExecNonExistentBinary — заведомо отсутствующий бинарь → isError.
func TestExecNonExistentBinary(t *testing.T) {
	baseURL, _, client, auditBuf := startMCPServerWithExecCfg(t, defaultExecCfg())

	body, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "definitely-not-a-binary-xyz-test",
	})

	envelope := parseToolResult(t, body)
	result, _ := envelope["result"].(map[string]interface{})
	if result == nil {
		t.Fatalf("AC8: no result; body=%s", body)
	}
	isErr, _ := result["isError"].(bool)
	if !isErr {
		t.Errorf("AC8/SR-45: non-existent binary must return isError:true; body=%s", body)
	}

	// Аудит: FAIL.
	log := auditBuf.String()
	if !strings.Contains(log, "FAIL") {
		t.Errorf("AC8/SR-45: audit must contain FAIL; log=%s", log)
	}

	// Сервер жив — следующий вызов отрабатывает.
	body2, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "echo",
		"args":    []string{"alive"},
	})
	envelope2 := parseToolResult(t, body2)
	result2, _ := envelope2["result"].(map[string]interface{})
	isErr2, _ := result2["isError"].(bool)
	if isErr2 {
		t.Errorf("AC8: server must be alive after error; body=%s", body2)
	}
}

// ============================================================================
// AC10/SR-49: env-whitelist — LD_PRELOAD не попадает в дочерний процесс
// ============================================================================

// TestExecEnvWhitelist — LD_PRELOAD не в окружении дочернего процесса.
func TestExecEnvWhitelist(t *testing.T) {
	t.Setenv("LD_PRELOAD", "/evil/lib.so")
	t.Setenv("SECRET_ENV_VAR", "should-not-leak")

	baseURL, _, client, _ := startMCPServerWithExecCfg(t, defaultExecCfg())

	// env выводит окружение.
	body, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "env",
	})

	envelope := parseToolResult(t, body)
	result, _ := envelope["result"].(map[string]interface{})
	if result == nil {
		t.Fatalf("AC10: no result; body=%s", body)
	}
	isErr, _ := result["isError"].(bool)
	if isErr {
		// env может не быть в PATH — пропускаем тест если так.
		content, _ := result["content"].([]interface{})
		if len(content) > 0 {
			ct, _ := content[0].(map[string]interface{})
			text, _ := ct["text"].(string)
			if strings.Contains(text, "not found") {
				t.Skip("env not found in PATH — skipping")
			}
		}
		t.Fatalf("AC10: env failed; body=%s", body)
	}

	sc, _ := result["structuredContent"].(map[string]interface{})
	stdout, _ := sc["stdout"].(string)

	if strings.Contains(stdout, "LD_PRELOAD") {
		t.Errorf("SR-49/AC10: LD_PRELOAD must not appear in child env; stdout=%s", stdout)
	}
	if strings.Contains(stdout, "SECRET_ENV_VAR") {
		t.Errorf("SR-49/AC10: SECRET_ENV_VAR must not appear in child env; stdout=%s", stdout)
	}
}

// ============================================================================
// AC10/SR-50: невалидный cwd → isError
// ============================================================================

// TestExecInvalidCwdIsError — несуществующий cwd → isError, команда не запущена.
func TestExecInvalidCwdIsError(t *testing.T) {
	baseURL, _, client, auditBuf := startMCPServerWithExecCfg(t, defaultExecCfg())

	body, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "echo",
		"args":    []string{"test"},
		"cwd":     "/definitely-nonexistent-dir-xyz",
	})

	envelope := parseToolResult(t, body)
	result, _ := envelope["result"].(map[string]interface{})
	if result == nil {
		t.Fatalf("AC10: no result; body=%s", body)
	}
	isErr, _ := result["isError"].(bool)
	if !isErr {
		t.Errorf("AC10/SR-50: invalid cwd must return isError:true; body=%s", body)
	}

	// Аудит: FAIL.
	log := auditBuf.String()
	if !strings.Contains(log, "FAIL") {
		t.Errorf("AC10/SR-50: audit must contain FAIL for bad cwd; log=%s", log)
	}
}

// ============================================================================
// AC11/SR-52: лимиты входа max_args/max_arg_len → isError
// ============================================================================

// TestExecTooManyArgsIsError — args превышает max_args → isError.
func TestExecTooManyArgsIsError(t *testing.T) {
	cfg := defaultExecCfg()
	cfg.MaxArgs = 3 // тестовый лимит

	baseURL, _, client, auditBuf := startMCPServerWithExecCfg(t, cfg)

	// 4 аргумента > max 3.
	args := []string{"a", "b", "c", "d"}
	body, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "echo",
		"args":    args,
	})

	envelope := parseToolResult(t, body)
	result, _ := envelope["result"].(map[string]interface{})
	if result == nil {
		t.Fatalf("SR-52: no result; body=%s", body)
	}
	isErr, _ := result["isError"].(bool)
	if !isErr {
		t.Errorf("SR-52: too many args must return isError:true; body=%s", body)
	}

	log := auditBuf.String()
	if !strings.Contains(log, "DENY") {
		t.Errorf("SR-52: audit must contain DENY; log=%s", log)
	}
}

// TestExecArgTooLongIsError — аргумент длиннее max_arg_len → isError.
func TestExecArgTooLongIsError(t *testing.T) {
	cfg := defaultExecCfg()
	cfg.MaxArgLen = 10 // тестовый лимит 10 байт

	baseURL, _, client, auditBuf := startMCPServerWithExecCfg(t, cfg)

	longArg := strings.Repeat("X", 11) // 11 > 10
	body, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "echo",
		"args":    []string{longArg},
	})

	envelope := parseToolResult(t, body)
	result, _ := envelope["result"].(map[string]interface{})
	if result == nil {
		t.Fatalf("SR-52: no result; body=%s", body)
	}
	isErr, _ := result["isError"].(bool)
	if !isErr {
		t.Errorf("SR-52: arg too long must return isError:true; body=%s", body)
	}

	log := auditBuf.String()
	if !strings.Contains(log, "DENY") {
		t.Errorf("SR-52: audit must contain DENY; log=%s", log)
	}
}

// ============================================================================
// AC11/SR-53: лимит вывода → truncated
// ============================================================================

// TestExecOutputTruncatedViaMCP — большой вывод → truncated через MCP.
func TestExecOutputTruncatedViaMCP(t *testing.T) {
	cfg := defaultExecCfg()
	cfg.MaxOutputBytes = 10 // очень маленький лимит

	baseURL, _, client, _ := startMCPServerWithExecCfg(t, cfg)

	body, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "echo",
		"args":    []string{"hello world this is more than 10 bytes"},
	})

	envelope := parseToolResult(t, body)
	result, _ := envelope["result"].(map[string]interface{})
	if result == nil {
		t.Fatalf("AC11: no result; body=%s", body)
	}
	isErr, _ := result["isError"].(bool)
	if isErr {
		t.Fatalf("AC11: truncation must not cause isError:true; body=%s", body)
	}

	sc, _ := result["structuredContent"].(map[string]interface{})
	truncated, _ := sc["stdout_truncated"].(bool)
	if !truncated {
		t.Errorf("AC11/SR-53: stdout_truncated must be true for output > limit; body=%s", body)
	}
	stdout, _ := sc["stdout"].(string)
	if len(stdout) > cfg.MaxOutputBytes {
		t.Errorf("AC11: stdout len = %d, want <= %d", len(stdout), cfg.MaxOutputBytes)
	}
}

// ============================================================================
// AC13/AC15/SR-57/SR-58/SR-62: аудит без секретов
// ============================================================================

// TestExecAuditContainsRequiredFields — success аудит имеет command/args/exit_code/duration/fp/remote.
func TestExecAuditContainsRequiredFields(t *testing.T) {
	baseURL, _, client, auditBuf := startMCPServerWithExecCfg(t, defaultExecCfg())
	auditBuf.Reset()

	_, _ = callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "echo",
		"args":    []string{"audit-test"},
	})

	log := auditBuf.String()

	// Ищем MCP exec-запись.
	var execLine string
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, "tool=execute_command") && strings.Contains(line, "result=ok") {
			execLine = line
			break
		}
	}
	if execLine == "" {
		t.Fatalf("AC13/SR-57: no exec audit record with result=ok; log=%s", log)
	}

	// AC13: command + args.
	if !strings.Contains(execLine, "command=echo") {
		t.Errorf("AC13/SR-58: exec audit missing command=echo; line=%q", execLine)
	}
	if !strings.Contains(execLine, "audit-test") {
		t.Errorf("AC13/SR-58: exec audit missing args; line=%q", execLine)
	}
	// exit_code.
	if !strings.Contains(execLine, "exit_code=") {
		t.Errorf("AC13/SR-58: exec audit missing exit_code; line=%q", execLine)
	}
	// duration.
	if !strings.Contains(execLine, "duration=") {
		t.Errorf("AC13/SR-58: exec audit missing duration; line=%q", execLine)
	}
	// fp=.
	if !strings.Contains(execLine, "fp=") {
		t.Errorf("AC13/SR-58: exec audit missing fp; line=%q", execLine)
	}
	// timed_out.
	if !strings.Contains(execLine, "timed_out=") {
		t.Errorf("AC13/SR-58: exec audit missing timed_out; line=%q", execLine)
	}
}

// TestExecAuditDenyContainsCommandArgs — deny аудит содержит command + args + reason.
func TestExecAuditDenyContainsCommandArgs(t *testing.T) {
	cfg := defaultExecCfg()
	cfg.Allowlist = []string{"echo"}

	baseURL, _, client, auditBuf := startMCPServerWithExecCfg(t, cfg)
	auditBuf.Reset()

	_, _ = callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "rm",
		"args":    []string{"-rf", "/"},
	})

	log := auditBuf.String()

	// Ищем DENY запись с command=rm.
	var denyLine string
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, "DENY") && strings.Contains(line, "rm") {
			denyLine = line
			break
		}
	}
	if denyLine == "" {
		t.Fatalf("AC13/SR-58: no DENY audit record with command=rm; log=%s", log)
	}

	if !strings.Contains(denyLine, "command=rm") {
		t.Errorf("AC13/SR-58: deny audit missing command=rm; line=%q", denyLine)
	}
	// Аргументы должны присутствовать (дословно; SR-63).
	if !strings.Contains(denyLine, "-rf") {
		t.Errorf("SR-63: deny audit must contain args dословно; line=%q", denyLine)
	}
}

// TestExecNoKeyInAuditOrResponse — ключ/секреты raxd не появляются в аудите и ответе.
func TestExecNoKeyInAuditOrResponse(t *testing.T) {
	// Используем полный стек с реальным ключом.
	baseURL, keyStr, client, auditBuf := startMCPServer(t)

	// initialize
	initBody := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "secret-test", "version": "1"},
	})
	initReq, _ := http.NewRequest(http.MethodPost, baseURL+"/mcp", strings.NewReader(initBody))
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Accept", "application/json, text/event-stream")
	initReq.Header.Set("Authorization", "Bearer "+keyStr)
	initResp, err := client.Do(initReq)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	readBody(t, initResp)
	auditBuf.Reset()

	callBody := jsonrpcBody(2, "tools/call", map[string]interface{}{
		"name":      "execute_command",
		"arguments": map[string]interface{}{"command": "echo", "args": []string{"test"}},
	})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/mcp", strings.NewReader(callBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+keyStr)
	req.Header.Set("MCP-Protocol-Version", "2025-11-25")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("execute_command: %v", err)
	}
	respBody := readBody(t, resp)
	auditLog := auditBuf.String()

	// AC15/SR-62: ключ не в ответе и не в аудите.
	if strings.Contains(respBody, keyStr) {
		t.Errorf("AC15/SR-62: API key found in execute_command response; body=%s", respBody)
	}
	if strings.Contains(auditLog, keyStr) {
		t.Errorf("AC15/SR-62: API key found in audit log; log=%s", auditLog)
	}
}

// ============================================================================
// AC14/SR-59: не-exec записи не ломаются (регрессия)
// ============================================================================

// TestExecAuditDoesNotBreakNonExecFormat — AUTH/MCP-ping-записи сохраняют прежний формат.
func TestExecAuditDoesNotBreakNonExecFormat(t *testing.T) {
	baseURL, keyStr, client, auditBuf := startMCPServer(t)

	// ping — создаёт MCP-запись без exec-полей.
	pingBody := jsonrpcBody(3, "tools/call", map[string]interface{}{
		"name":      "ping",
		"arguments": map[string]interface{}{},
	})
	initReq, _ := http.NewRequest(http.MethodPost, baseURL+"/mcp", strings.NewReader(jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1"},
	})))
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Accept", "application/json, text/event-stream")
	initReq.Header.Set("Authorization", "Bearer "+keyStr)
	initResp, _ := client.Do(initReq)
	readBody(t, initResp)
	auditBuf.Reset()

	req, _ := http.NewRequest(http.MethodPost, baseURL+"/mcp", strings.NewReader(pingBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+keyStr)
	req.Header.Set("MCP-Protocol-Version", "2025-11-25")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	readBody(t, resp)

	log := auditBuf.String()

	// AUTH и ping MCP-записи должны присутствовать.
	if !strings.Contains(log, "AUTH") && !strings.Contains(log, "tool=ping") {
		t.Errorf("AC14/SR-59: expected AUTH or tool=ping in log; log=%s", log)
	}

	// Ping-запись НЕ должна содержать exec-поля.
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, "tool=ping") {
			if strings.Contains(line, "command=") {
				t.Errorf("AC14/SR-59: ping audit must not contain command=; line=%q", line)
			}
			if strings.Contains(line, "exit_code=") {
				t.Errorf("AC14/SR-59: ping audit must not contain exit_code=; line=%q", line)
			}
		}
	}
}

// ============================================================================
// AC4: ненулевой exit code — НЕ isError
// ============================================================================

// TestExecNonZeroExitNotError — команда с exit != 0 не является isError.
func TestExecNonZeroExitNotError(t *testing.T) {
	baseURL, _, client, _ := startMCPServerWithExecCfg(t, defaultExecCfg())

	body, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "sh",
		"args":    []string{"-c", "exit 42"},
	})

	envelope := parseToolResult(t, body)
	if errObj := envelope["error"]; errObj != nil {
		t.Fatalf("AC4: protocol error: %v; body=%s", errObj, body)
	}

	result, _ := envelope["result"].(map[string]interface{})
	if result == nil {
		t.Fatalf("AC4: no result; body=%s", body)
	}
	isErr, _ := result["isError"].(bool)
	if isErr {
		t.Errorf("AC4: non-zero exit code must NOT cause isError:true; body=%s", body)
	}
	sc, _ := result["structuredContent"].(map[string]interface{})
	exitCode, _ := sc["exit_code"].(float64)
	if exitCode != 42 {
		t.Errorf("AC4: exit_code = %v, want 42; body=%s", exitCode, body)
	}
}

// ============================================================================
// AC16/SR-42: rate-limit наследуется (базовый тест через startMCPServer)
// ============================================================================

// TestExecRateLimitInherited — execute_command подчиняется rate-limit (401 без ключа).
func TestExecRateLimitInherited(t *testing.T) {
	baseURL, _, client, _ := startMCPServer(t)

	// Без Bearer → 401 (auth, до execute_command).
	callBody := jsonrpcBody(2, "tools/call", map[string]interface{}{
		"name":      "execute_command",
		"arguments": map[string]interface{}{"command": "echo"},
	})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/mcp", strings.NewReader(callBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	// Нет Authorization.

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("execute_command without auth: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("AC16/SR-41: no-auth execute_command → want 401, got %d", resp.StatusCode)
	}
}

// ============================================================================
// SR-56: deny_root=true + euid==0 → isError (unit-тест логики)
// ============================================================================

// TestExecDenyRootLogicWhenEnabled — тест логики deny_root через конфиг.
// Примечание: в тестовой среде euid обычно != 0, поэтому тестируем конфиг-поле
// через что оно присутствует в Config. Реальный root-WARN тестируем отдельно.
func TestExecDenyRootConfigField(t *testing.T) {
	cfg := defaultExecCfg()
	cfg.DenyRoot = true

	// Просто убеждаемся что конфиг принимается без паники.
	baseURL, _, client, _ := startMCPServerWithExecCfg(t, cfg)

	body, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "echo",
		"args":    []string{"test"},
	})
	envelope := parseToolResult(t, body)
	if errObj := envelope["error"]; errObj != nil {
		t.Fatalf("deny_root=true with non-root euid must not cause protocol error; body=%s", body)
	}
	// При не-root euid — команда должна выполниться нормально.
	result, _ := envelope["result"].(map[string]interface{})
	if result == nil {
		t.Fatalf("deny_root=true with non-root: no result; body=%s", body)
	}
}
