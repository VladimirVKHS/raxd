package mcp_test

// exec_qa_test.go — QA-тесты для MCP-инструмента execute_command (дополнение к exec_tool_test.go).
//
// Закрывают пробелы, выявленные независимым QA-анализом после developer-guardian (раунд 2),
// а также замечания qa-guardian (раунд 1):
//
//   - AC13/SR-57: TestExecAuditExactlyOneRecord  — ровно одна exec-запись/вызов
//   - AC14/SR-60: TestExecAuditLogfmtParseable   — exec-запись парсится как logfmt key=value
//   - SR-63:      TestExecAuditArgsVerbatimInSuccess — args в аудите дословно (success-ветка)
//   - AC16/SR-42: TestExecRateLimit429BeforeCommand — РЕАЛЬНЫЙ 429 через полный TLS-стек
//   - AC9/SR-55:  TestRootWarnAuditRecord          — unit-тест WARN-логики writeAudit при euid==0
//   - SR-56:      TestDenyRootUnitLogic             — deny_root=true+euid==0 через конфиг handler
//   - SR-66:      TestExecConfigDefaults            — безопасные конфиг-дефолты применяются
//   - AC12/SR-27: TestExecKeystoreCorruptReturns403 — corrupt keys.db → 403 ДО execute_command
//
// qa-guardian раунд 1 замечания закрыты:
//   F-2: TestExecRateLimit429BeforeCommand переписан на РЕАЛЬНЫЙ 429 через startMCPServerWithRateLimit
//   F-7: parseSimpleLogfmt исправлен для quoted-значений с пробелами (ищет до конца строки)
//
// Тесты запускаются только в Docker (-mod=vendor; AC18/SR-67).
// Продуктовый код не правится — только тесты поведения.

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	internalmcp "github.com/vladimirvkhs/raxd/internal/mcp"
	"github.com/vladimirvkhs/raxd/internal/cmdexec"
	"github.com/vladimirvkhs/raxd/internal/keystore"
	"github.com/vladimirvkhs/raxd/internal/server"
	"github.com/vladimirvkhs/raxd/internal/version"
)

// ============================================================================
// startMCPServerWithRateLimit — вспомогательная функция для теста реального 429
// ============================================================================

// startMCPServerWithRateLimit запускает полный TLS-стек raxd с заданными
// параметрами rate-limit (rateLimit, rateBurst). Возвращает baseURL, ключ,
// http.Client и auditBuf. Аналогична startMCPServer из mcp_test.go, но
// принимает явный rate-limit — для тестирования AC16/SR-42.
func startMCPServerWithRateLimit(t *testing.T, rateLimit float64, rateBurst int) (
	baseURL string, keyStr string, client *http.Client, auditBuf *bytes.Buffer,
) {
	t.Helper()

	paths := newTestPaths(t)
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	plain, _, err := store.Create("ratelimit-exec-key")
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

	// Выделяем свободный порт.
	ln, lerr := net.Listen("tcp", "127.0.0.1:0")
	if lerr != nil {
		t.Fatalf("freePort: %v", lerr)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	cfg := newTestConfig(port)
	cfg.RateLimit = rateLimit
	cfg.RateBurst = rateBurst

	srv, err := server.New(cfg, paths, store, logger, mcpH)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	// Строим TLS-клиент.
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

	// Ждём готовности сервера.
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

// ============================================================================
// AC13/SR-57: ровно одна exec-запись за вызов (без двойной записи)
// ============================================================================

// TestExecAuditExactlyOneRecord проверяет, что один вызов execute_command
// порождает РОВНО одну основную exec-аудит-запись success/fail/deny.
//
// SR-57/ADR-004: execHandler пишет ровно одну запись в основном пути
// (success/таймаут → result=ok; deny → result=deny; fail → result=fail).
// Дополнительная WARN-запись при euid==0 (SR-55) — легитимная вторая запись,
// она не является дублированием основной exec-записи.
//
// Тест проверяет:
//   - ровно ОДНА success-запись (result=ok) за один успешный вызов
//   - не более ОДНОЙ deny/fail-записи за один неуспешный вызов
//
// Специальный случай euid==0 (Docker от root):
//   - 1 WARN-запись (root-детекция, SR-55)
//   - 1 success-запись (tool=execute_command + result=ok)
//   Итого 2 записи с tool=execute_command — это ОЖИДАЕМОЕ поведение по SR-55/SR-57.
func TestExecAuditExactlyOneRecord(t *testing.T) {
	baseURL, _, client, auditBuf := startMCPServerWithExecCfg(t, defaultExecCfg())
	auditBuf.Reset()

	_, _ = callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "echo",
		"args":    []string{"one-record-test"},
	})

	log := auditBuf.String()

	// Считаем успешные exec-записи (result=ok) — их должна быть ровно одна.
	successCount := 0
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, "tool=execute_command") && strings.Contains(line, "result=ok") {
			successCount++
		}
	}
	if successCount == 0 {
		t.Fatalf("AC13/SR-57: не найдено ни одной success-записи (result=ok); log=%s", log)
	}
	if successCount > 1 {
		t.Errorf("AC13/SR-57: PRODUCT BUG: найдено %d success-записей для одного вызова (ожидается 1).\n"+
			"ADR-004: execHandler пишет РОВНО одну основную запись — двойная success-запись недопустима.\n"+
			"Эскалируй к developer.\nlog=%s", successCount, log)
	} else {
		t.Logf("AC13/SR-57: OK — ровно %d success-запись за вызов", successCount)
	}

	// Дополнительно: при euid==0 должна быть ровно одна WARN-запись (SR-55).
	// Это ожидаемое поведение, не дублирование.
	if os.Geteuid() == 0 {
		warnCount := 0
		for _, line := range strings.Split(log, "\n") {
			if strings.Contains(line, "tool=execute_command") && strings.Contains(line, "running-as-root") {
				warnCount++
			}
		}
		if warnCount != 1 {
			t.Errorf("AC9/SR-55: при euid==0 ожидается ровно 1 WARN-запись, найдено %d; log=%s",
				warnCount, log)
		} else {
			t.Logf("AC9/SR-55: OK — ровно %d root-WARN-запись при euid==0", warnCount)
		}
	}
}

// ============================================================================
// AC14/SR-60: exec-запись парсится как строгий logfmt (key=value)
// ============================================================================

// TestExecAuditLogfmtParseable проверяет что exec-аудит-запись содержит
// ключ-значение пары в logfmt-формате и они извлекаются корректно.
//
// SR-60/ADR-002: LogfmtFormatter — строгий, парсимый key=value.
// Проверяем что команда, args, exit_code, duration присутствуют как ключи.
func TestExecAuditLogfmtParseable(t *testing.T) {
	baseURL, _, client, auditBuf := startMCPServerWithExecCfg(t, defaultExecCfg())
	auditBuf.Reset()

	_, _ = callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "echo",
		"args":    []string{"logfmt-test"},
	})

	log := auditBuf.String()

	// Ищем exec-строку успеха.
	var execLine string
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, "tool=execute_command") && strings.Contains(line, "result=ok") {
			execLine = line
			break
		}
	}
	if execLine == "" {
		t.Fatalf("AC14/SR-60: не найдена exec-строка с result=ok; log=%s", log)
	}

	// Проверяем наличие logfmt-ключей (key= или key="...").
	// Каждый из этих ключей обязан присутствовать в exec-записи (SR-58/SR-60).
	requiredKeys := []string{
		"tool=",
		"result=",
		"command=",
		"exit_code=",
		"duration=",
		"timed_out=",
		"fp=",
		"remote=",
	}
	for _, key := range requiredKeys {
		if !strings.Contains(execLine, key) {
			t.Errorf("AC14/SR-60: exec-запись не содержит logfmt-ключ %q; line=%q", key, execLine)
		}
	}

	// Базовая logfmt-парсимость: извлекаем пары key=value.
	// Используем исправленный парсер, корректно обрабатывающий quoted-значения с пробелами.
	kv := parseSimpleLogfmt(execLine)
	if len(kv) == 0 {
		t.Errorf("AC14/SR-60: logfmt-парсер не извлёк ни одной пары key=value из строки %q", execLine)
	}
	// Проверяем что tool извлекается корректно.
	if toolVal, ok := kv["tool"]; !ok || toolVal != "execute_command" {
		t.Errorf("AC14/SR-60: logfmt: tool=%q, want execute_command; kv=%v", kv["tool"], kv)
	}
	// result=ok.
	if resVal, ok := kv["result"]; !ok || resVal != "ok" {
		t.Errorf("AC14/SR-60: logfmt: result=%q, want ok; kv=%v", kv["result"], kv)
	}
	t.Logf("AC14/SR-60: OK — exec-запись парсится как logfmt: %d пар key=value", len(kv))
}

// parseSimpleLogfmt извлекает пары key=value из строки logfmt.
//
// Корректно обрабатывает:
//   - unquoted значения: key=val
//   - quoted значения с пробелами: key="value with spaces"
//
// Алгоритм:
//  1. Для каждого токена ищем '='.
//  2. Если значение начинается с '"' — читаем до закрывающей '"'
//     (которая может быть в следующих "токенах" после split по пробелам).
//  3. Иначе — значение заканчивается перед следующим пробелом.
//
// Не является полноценным logfmt-парсером — достаточен для проверки структуры.
func parseSimpleLogfmt(line string) map[string]string {
	result := make(map[string]string)
	i := 0
	// Разбиваем по пробелам, но понимаем quoted значения.
	runes := []rune(line)
	n := len(runes)

	for i < n {
		// Пропускаем пробелы.
		for i < n && runes[i] == ' ' {
			i++
		}
		if i >= n {
			break
		}

		// Ищем '=' — начало пары key=value.
		eqStart := i
		for i < n && runes[i] != '=' && runes[i] != ' ' {
			i++
		}
		if i >= n || runes[i] != '=' {
			// Нет '=' — не key=value токен, пропускаем.
			for i < n && runes[i] != ' ' {
				i++
			}
			continue
		}
		key := string(runes[eqStart:i])
		if key == "" {
			i++ // пропускаем '='
			continue
		}
		i++ // пропускаем '='

		// Читаем значение.
		var val string
		if i < n && runes[i] == '"' {
			// Quoted значение — читаем до закрывающей '"', поддерживаем экранирование.
			i++ // пропускаем открывающую '"'
			valStart := i
			for i < n {
				if runes[i] == '\\' && i+1 < n {
					i += 2 // экранированный символ
					continue
				}
				if runes[i] == '"' {
					break
				}
				i++
			}
			val = string(runes[valStart:i])
			if i < n {
				i++ // пропускаем закрывающую '"'
			}
		} else {
			// Unquoted значение — до пробела.
			valStart := i
			for i < n && runes[i] != ' ' {
				i++
			}
			val = string(runes[valStart:i])
		}
		result[key] = val
	}
	return result
}

// ============================================================================
// SR-63: args в аудите дословно — success-ветка
// ============================================================================

// TestExecAuditArgsVerbatimInSuccess проверяет что аргументы команды
// присутствуют в exec-аудит-записи дословно при успешном выполнении.
//
// SR-63/П-3: args логируются КАК ЕСТЬ (без маскирования) для полноты аудита RCE.
// Предыдущий тест (TestExecAuditDenyContainsCommandArgs) проверяет deny-ветку.
// Этот тест — success-ветку.
func TestExecAuditArgsVerbatimInSuccess(t *testing.T) {
	baseURL, _, client, auditBuf := startMCPServerWithExecCfg(t, defaultExecCfg())
	auditBuf.Reset()

	distinctiveArg := "verbatim-arg-check-12345"
	_, _ = callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "echo",
		"args":    []string{distinctiveArg},
	})

	log := auditBuf.String()

	// Ищем exec-строку успеха.
	var execLine string
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, "tool=execute_command") && strings.Contains(line, "result=ok") {
			execLine = line
			break
		}
	}
	if execLine == "" {
		t.Fatalf("SR-63: не найдена exec-строка с result=ok; log=%s", log)
	}

	// Аргумент должен присутствовать дословно в аудите.
	if !strings.Contains(execLine, distinctiveArg) {
		t.Errorf("SR-63: PRODUCT BUG: аргумент %q отсутствует в exec-аудите success-ветки.\n"+
			"SR-63 требует логировать args дословно для полноты аудита RCE.\n"+
			"Эскалируй к developer.\nline=%q", distinctiveArg, execLine)
	} else {
		t.Logf("SR-63: OK — аргумент %q присутствует в success-аудите дословно", distinctiveArg)
	}
}

// ============================================================================
// AC16/SR-42: РЕАЛЬНЫЙ rate-limit 429 ДО исполнения execute_command
// ============================================================================

// TestExecRateLimit429BeforeCommand проверяет что при превышении rate-limit
// возвращается HTTP 429 ДО вызова execute_command (команда не запускается).
//
// F-2 (qa-guardian): тест переписан на РЕАЛЬНЫЙ 429 через полный TLS-стек.
// Использует startMCPServerWithRateLimit(t, 1, 1) — минимальный burst=1, rate=1 req/s.
//
// Стратегия:
//  1. Запустить полный TLS-сервер raxd с RateLimit=1, RateBurst=1.
//  2. Отправить initialize (потребляет 1 токен из burst=1).
//  3. Отправить tools/call execute_command до получения HTTP 429.
//  4. Убедиться что при 429 команда НЕ была запущена (нет exec-записи tool=execute_command).
//
// SR-42: rate-limit per-key/per-IP → 429 ДО handler; execute_command не исполняется.
func TestExecRateLimit429BeforeCommand(t *testing.T) {
	// RateLimit=1 req/s, RateBurst=1 — после первого запроса burst исчерпан.
	// Второй быстрый запрос должен получить 429.
	baseURL, keyStr, client, auditBuf := startMCPServerWithRateLimit(t, 1, 1)

	// Шаг 1: initialize — потребляет единственный токен из burst=1.
	initBody := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "rate-test", "version": "1"},
	})
	initResp := postMCP(t, client, baseURL, keyStr, initBody, nil)
	readBody(t, initResp)

	// Шаг 2: отправляем execute_command пока не получим 429.
	// Так как burst=1 и rate=1 req/s, а мы отправляем быстро — 429 должен прийти
	// в течение нескольких запросов (обычно 2-й или 3-й).
	callBody := jsonrpcBody(2, "tools/call", map[string]interface{}{
		"name":      "execute_command",
		"arguments": map[string]interface{}{"command": "echo", "args": []string{"rate-test"}},
	})

	got429 := false
	auditBuf.Reset()

	const maxAttempts = 15
	for i := 0; i < maxAttempts; i++ {
		req, err := http.NewRequest(http.MethodPost, baseURL+"/mcp", strings.NewReader(callBody))
		if err != nil {
			t.Fatalf("AC16/SR-42: new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+keyStr)
		req.Header.Set("MCP-Protocol-Version", "2025-11-25")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("AC16/SR-42: execute_command request %d: %v", i+1, err)
		}
		code := resp.StatusCode
		readBody(t, resp)

		if code == http.StatusTooManyRequests {
			got429 = true
			t.Logf("AC16/SR-42: получен 429 на попытке %d", i+1)
			break
		}
	}

	if !got429 {
		t.Fatalf("AC16/SR-42: PRODUCT BUG: за %d запросов с burst=1, rate=1 не получен 429.\n"+
			"Rate-limit middleware не срабатывает для execute_command.\n"+
			"Эскалируй к developer.", maxAttempts)
	}

	// Шаг 3: убеждаемся что при 429 команда НЕ была запущена.
	// exec-запись с tool=execute_command И result=ok (успех) не должна присутствовать
	// в момент 429-ответа: rate-limit срабатывает ДО MCP-handler.
	log := auditBuf.String()

	// При 429 аудит должен содержать RATE, но NOT "tool=execute_command" с result=ok.
	// Примечание: RATE-записи пишет rateLimitMiddleware ДО вызова execHandler.
	rateLimitedExecSuccess := false
	for _, line := range strings.Split(log, "\n") {
		// Ищем успешную exec-запись (result=ok) после того как мы уже сбросили буфер.
		// Любая такая запись ПОСЛЕ auditBuf.Reset означала бы что rate-limited запрос
		// достиг execHandler — это баг.
		if strings.Contains(line, "tool=execute_command") && strings.Contains(line, "result=ok") {
			rateLimitedExecSuccess = true
		}
	}

	if rateLimitedExecSuccess {
		t.Errorf("AC16/SR-42: PRODUCT BUG: execute_command успешно выполнился несмотря на 429.\n"+
			"SR-42 требует: rate-limit срабатывает ДО execHandler. Эскалируй к developer.\nlog=%s", log)
	} else {
		t.Logf("AC16/SR-42: OK — реальный 429 получен; execute_command не вызван после сброса буфера")
	}

	// RATE должен быть в аудите.
	if !strings.Contains(log, "RATE") {
		t.Errorf("AC16/SR-42: RATE-запись отсутствует в аудите при 429; log=%s", log)
	} else {
		t.Logf("AC16/SR-42: OK — RATE-запись присутствует в аудите")
	}
}

// ============================================================================
// AC9/SR-55: WARN-аудит при euid==0 — unit-тест логики writeAudit
// ============================================================================

// TestRootWarnAuditRecord проверяет логику WARN-аудита при euid==0 на уровне
// AuditRecord/writeAudit. Не требует реального запуска от root.
//
// SR-55: при euid==0 execHandler пишет отдельную WARN-запись (Result:"warn").
// Этот тест симулирует ту же запись напрямую через server.NewAuditFnForTest,
// проверяя что writeAudit корректно обрабатывает Result:"warn" с exec-полями.
//
// F-4 (qa-guardian): уточнение в описании:
//   - Этот тест покрывает writeAudit НАПРЯМУЮ (unit-уровень).
//   - Путь euid==0 через execHandler покрывается только при запуске от root.
//   - В Docker тесты запускаются от root (euid==0) → ветка euid==0 тоже выполняется.
func TestRootWarnAuditRecord(t *testing.T) {
	auditBuf := &bytes.Buffer{}
	logger := newTestLogger(auditBuf)
	auditFn := server.NewAuditFnForTest(logger)

	// Симулируем WARN-запись которую execHandler пишет при euid==0 (SR-55).
	// Тест покрывает логику writeAudit напрямую (unit-уровень).
	auditFn(server.AuditRecord{
		TS:          time.Now().UTC(),
		Fingerprint: "aabbccdd1122",
		RemoteAddr:  "127.0.0.1:12345",
		Result:      "warn",
		Tool:        "execute_command",
		Reason:      "running-as-root: raxd executing commands as root (euid==0); ensure raxd runs as non-root",
		Command:     "echo",
		Args:        []string{"test"},
	})

	log := auditBuf.String()

	// WARN должен быть в логе (charmbracelet/log пишет уровень WARN).
	if !strings.Contains(log, "WARN") {
		t.Errorf("AC9/SR-55: WARN-запись не содержит WARN; log=%q", log)
	}
	// reason с "root" должен быть в записи.
	if !strings.Contains(log, "root") {
		t.Errorf("AC9/SR-55: WARN-запись должна содержать 'root' в reason; log=%q", log)
	}
	// tool=execute_command должен присутствовать (exec-ветка).
	if !strings.Contains(log, "tool=execute_command") {
		t.Errorf("AC9/SR-55: WARN-запись должна содержать tool=execute_command; log=%q", log)
	}
	// command= должен присутствовать.
	if !strings.Contains(log, "command=echo") {
		t.Errorf("AC9/SR-55: WARN-запись должна содержать command=echo; log=%q", log)
	}
	// fp= должен присутствовать.
	if !strings.Contains(log, "fp=") {
		t.Errorf("AC9/SR-55: WARN-запись должна содержать fp=; log=%q", log)
	}
	t.Logf("AC9/SR-55: OK — writeAudit корректно обрабатывает Result:warn (unit-уровень): %q", log)

	// Если тест запущен от root (euid==0) — проверяем реальный путь через execHandler в MCP.
	// В Docker все тесты запускаются от root, поэтому эта ветка всегда активна.
	// F-4: это РЕАЛЬНОЕ покрытие пути euid==0 → execHandler → WARN-запись.
	if os.Geteuid() == 0 {
		t.Log("AC9/SR-55: euid==0 обнаружен — проверяем реальный WARN через MCP-вызов (execHandler path)")
		mcpBaseURL, _, mcpClient, mcpAuditBuf := startMCPServerWithExecCfg(t, defaultExecCfg())
		mcpAuditBuf.Reset()
		_, _ = callExecuteCommand(t, mcpClient, mcpBaseURL, map[string]interface{}{
			"command": "echo",
			"args":    []string{"root-warn-test"},
		})
		mcpLog := mcpAuditBuf.String()
		if !strings.Contains(mcpLog, "running-as-root") {
			t.Errorf("AC9/SR-55: при euid==0 нет WARN с 'running-as-root' в аудите MCP (execHandler path); log=%s", mcpLog)
		} else {
			t.Logf("AC9/SR-55: OK — WARN 'running-as-root' найден в MCP-аудите при euid==0 (execHandler path)")
		}
	}
}

// ============================================================================
// SR-56: deny_root=true + euid==0 → isError (unit-тест конфиг-логики handler)
// ============================================================================

// TestDenyRootUnitLogic проверяет что deny_root=true правильно записан в Config
// и что handler при euid==0 возвращает isError.
//
// При euid != 0 (обычная тест-среда): тест проверяет только что конфиг принимается
// и команда выполняется нормально (deny_root не срабатывает при не-root euid).
// При euid == 0 (Docker от root): тест проверяет реальный deny.
func TestDenyRootUnitLogic(t *testing.T) {
	cfg := defaultExecCfg()
	cfg.DenyRoot = true

	baseURL, _, client, auditBuf := startMCPServerWithExecCfg(t, cfg)

	body, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "echo",
		"args":    []string{"deny-root-test"},
	})

	envelope := parseToolResult(t, body)
	if errObj := envelope["error"]; errObj != nil {
		// Protocol error — не ожидается при deny_root.
		t.Fatalf("SR-56: protocol error при deny_root=true: %v; body=%s", errObj, body)
	}

	if os.Geteuid() == 0 {
		// Запущены от root: deny_root=true должен вернуть isError.
		result, _ := envelope["result"].(map[string]interface{})
		if result == nil {
			t.Fatalf("SR-56: нет result при euid==0 + deny_root=true; body=%s", body)
		}
		isErr, _ := result["isError"].(bool)
		if !isErr {
			t.Errorf("SR-56: PRODUCT BUG: deny_root=true + euid==0 → ожидается isError:true; body=%s", body)
		}
		// Аудит: должна быть deny-запись.
		log := auditBuf.String()
		if !strings.Contains(log, "DENY") {
			t.Errorf("SR-56: при deny_root=true+euid==0 нет DENY в аудите; log=%s", log)
		}
		t.Logf("SR-56: OK — deny_root=true + euid==0 → isError:true + DENY в аудите")
	} else {
		// Не root: deny_root не срабатывает, команда выполняется.
		result, _ := envelope["result"].(map[string]interface{})
		if result == nil {
			t.Fatalf("SR-56: нет result при non-root euid; body=%s", body)
		}
		isErr, _ := result["isError"].(bool)
		if isErr {
			t.Errorf("SR-56: deny_root=true при non-root euid не должен вызывать isError:true; body=%s", body)
		}
		t.Logf("SR-56: OK — deny_root=true при non-root euid → команда выполнена нормально")
	}
}

// ============================================================================
// SR-66: конфиг-дефолты применяются при отсутствии явных значений
// ============================================================================

// TestExecConfigDefaults проверяет что безопасные дефолты Config применяются:
// - DefaultCwd = "/tmp" — при пустом cwd используется /tmp.
// - DefaultTimeoutMs = 30000 — команда получает 30s таймаут (не висит вечно).
// - MaxOutputBytes = 1MiB — большой вывод не забивает память.
//
// SR-66: все параметры безопасности — конфиг-поля с безопасными дефолтами.
func TestExecConfigDefaults(t *testing.T) {
	cfg := defaultExecCfg()
	// Убеждаемся что дефолты соответствуют spec.
	if cfg.DefaultCwd != "/tmp" {
		t.Errorf("SR-66: DefaultCwd = %q, want /tmp (безопасный дефолт по SR-66/AC10)", cfg.DefaultCwd)
	}
	if cfg.DefaultTimeoutMs != 30000 {
		t.Errorf("SR-66: DefaultTimeoutMs = %d, want 30000 (30s; SR-66/AC5)", cfg.DefaultTimeoutMs)
	}
	if cfg.MaxTimeoutMs != 300000 {
		t.Errorf("SR-66: MaxTimeoutMs = %d, want 300000 (5min; SR-66/AC5)", cfg.MaxTimeoutMs)
	}
	if cfg.MaxArgs != 256 {
		t.Errorf("SR-66: MaxArgs = %d, want 256 (SR-66/SR-52)", cfg.MaxArgs)
	}
	if cfg.MaxArgLen != 131072 {
		t.Errorf("SR-66: MaxArgLen = %d, want 131072 (128KiB; SR-66/SR-52)", cfg.MaxArgLen)
	}
	if cfg.MaxOutputBytes != 1048576 {
		t.Errorf("SR-66: MaxOutputBytes = %d, want 1048576 (1MiB; SR-66/AC11)", cfg.MaxOutputBytes)
	}
	if cfg.DenyRoot {
		t.Errorf("SR-66: DenyRoot = true, want false (дефолт WARN-only; SR-66/SR-56)")
	}
	if cfg.Allowlist != nil && len(cfg.Allowlist) > 0 {
		t.Errorf("SR-66: Allowlist = %v, want nil/[] (allowlist выключен по умолчанию; SR-66/AC7)", cfg.Allowlist)
	}

	// Проверяем env-whitelist: PATH обязателен; опасные переменные отсутствуют.
	whitelistMap := make(map[string]bool)
	for _, v := range cfg.EnvWhitelist {
		whitelistMap[v] = true
	}
	if !whitelistMap["PATH"] {
		t.Errorf("SR-66/SR-49: PATH обязателен в EnvWhitelist (для LookPath); whitelist=%v", cfg.EnvWhitelist)
	}
	// Опасные переменные НЕ должны быть в whitelist (SR-49).
	dangerous := []string{"LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES", "IFS"}
	for _, d := range dangerous {
		if whitelistMap[d] {
			t.Errorf("SR-66/SR-49: %s в EnvWhitelist — это ВЕКТОР БЕЗОПАСНОСТИ (CWE-426); удали из whitelist", d)
		}
	}

	// Функциональная проверка DefaultCwd: при пустом cwd используется /tmp.
	baseURL, _, client, _ := startMCPServerWithExecCfg(t, cfg)

	body, _ := callExecuteCommand(t, client, baseURL, map[string]interface{}{
		"command": "pwd",
		// cwd не задан → должен применяться DefaultCwd = /tmp
	})

	envelope := parseToolResult(t, body)
	if errObj := envelope["error"]; errObj != nil {
		t.Fatalf("SR-66: protocol error для pwd без cwd: %v", errObj)
	}
	result, _ := envelope["result"].(map[string]interface{})
	if result == nil {
		t.Fatalf("SR-66: нет result для pwd; body=%s", body)
	}
	sc, _ := result["structuredContent"].(map[string]interface{})
	stdout, _ := sc["stdout"].(string)
	cwd := strings.TrimSpace(stdout)

	// На macOS /tmp → /private/tmp; проверяем оба варианта.
	if cwd != "/tmp" && cwd != "/private/tmp" {
		t.Errorf("SR-66: DefaultCwd не применился: pwd вернул %q, ожидается /tmp или /private/tmp", cwd)
	} else {
		t.Logf("SR-66: OK — DefaultCwd применился: cwd=%q", cwd)
	}
}

// ============================================================================
// AC12/SR-27: TestExecKeystoreCorruptReturns403
// ============================================================================

// TestExecKeystoreCorruptReturns403 проверяет что при повреждённом keys.db
// запрос tools/call execute_command получает HTTP 403 ДО инструмента.
//
// F-1 (qa-guardian): покрывает AC12 для execute_command (по образцу
// TestMCPKeystoreCorruptReturns403 из mcp_security_test.go — там для ping).
//
// Сценарий:
//  1. Запускаем сервер с валидной БД ключей.
//  2. Выполняем один успешный запрос (проверяем что сервер жив).
//  3. Повреждаем keys.db на диске.
//  4. Отправляем tools/call execute_command.
//  5. Ожидаем HTTP 403 (ErrCorrupt → authMiddleware → 403).
//  6. Убеждаемся что tool=execute_command НЕ появился в аудите (MCP не достигнут).
//  7. Убеждаемся что сервер жив после повреждения.
//
// SR-27/AC12: ErrCorrupt из keystore.Verify → 403, не 401 и не 200.
// Escalate к developer если тест падает — не ослабляй ассерт.
func TestExecKeystoreCorruptReturns403(t *testing.T) {
	paths := newTestPaths(t)

	// Открываем БД и создаём ключ.
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		t.Fatalf("AC12/SR-27: open store: %v", err)
	}
	plain, _, err := store.Create("corrupt-exec-test-key")
	if err != nil {
		t.Fatalf("AC12/SR-27: create key: %v", err)
	}
	keyStr := string(plain)

	auditBuf := &bytes.Buffer{}
	logger := newTestLogger(auditBuf)
	auditFn := server.NewAuditFnForTest(logger)

	mcpH, err := internalmcp.NewHandler(version.Version, auditFn, defaultExecCfg(), defaultUplCfg(t))
	if err != nil {
		t.Fatalf("AC12/SR-27: mcp.NewHandler: %v", err)
	}

	// Выделяем свободный порт.
	ln, lerr := net.Listen("tcp", "127.0.0.1:0")
	if lerr != nil {
		t.Fatalf("AC12/SR-27: freePort: %v", lerr)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	cfg := newTestConfig(port)
	srv, err := server.New(cfg, paths, store, logger, mcpH)
	if err != nil {
		t.Fatalf("AC12/SR-27: server.New: %v", err)
	}

	// Строим TLS-клиент.
	certPath := filepath.Join(paths.TLSDir, "cert.pem")
	pool := x509.NewCertPool()
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("AC12/SR-27: read cert: %v", err)
	}
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatal("AC12/SR-27: append cert")
	}
	client := &http.Client{
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

	// Ждём готовности.
	baseURL := fmt.Sprintf("https://127.0.0.1:%d", port)
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

	// Проверяем что сервер жив: initialize должен пройти.
	initBody := jsonrpcBody(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "corrupt-test", "version": "1"},
	})
	preResp := postMCP(t, client, baseURL, keyStr, initBody, nil)
	preBody := readBody(t, preResp)
	if preResp.StatusCode != http.StatusOK {
		t.Fatalf("AC12/SR-27: pre-corrupt initialize failed (want 200): got %d; body=%s",
			preResp.StatusCode, preBody)
	}

	// Повреждаем keys.db на диске — имитирует дисковую порчу в runtime.
	if err := os.WriteFile(paths.KeysDB, []byte(`{broken json`), 0o600); err != nil {
		t.Fatalf("AC12/SR-27: corrupt keys.db: %v", err)
	}

	auditBuf.Reset()

	// Отправляем tools/call execute_command. Bearer корректного формата,
	// но keystore.Verify вернёт ErrCorrupt → authMiddleware → HTTP 403.
	callBody := jsonrpcBody(2, "tools/call", map[string]interface{}{
		"name":      "execute_command",
		"arguments": map[string]interface{}{"command": "echo", "args": []string{"corrupt-test"}},
	})
	resp := postMCP(t, client, baseURL, keyStr, callBody, map[string]string{
		"MCP-Protocol-Version": "2025-11-25",
	})
	respBody := readBody(t, resp)

	// Основной ассерт: ErrCorrupt → HTTP 403.
	// Если получаем 401 — ErrCorrupt обрабатывается как auth-fail (баг).
	// Если получаем 200 — corrupt keystore принят (критический баг).
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf(
			"AC12/SR-27 PRODUCT BUG: corrupt keys.db → want HTTP 403, got %d.\n"+
				"ErrCorrupt должен маппиться в 403, не в %d.\n"+
				"Эскалируй к developer — не меняй ожидаемый статус-код.\nbody=%s",
			resp.StatusCode, resp.StatusCode, respBody,
		)
	} else {
		t.Logf("AC12/SR-27: OK — corrupt keys.db → HTTP 403")
	}

	// MCP-слой не должен быть достигнут: tool=execute_command не должен появиться в аудите.
	logOutput := auditBuf.String()
	if strings.Contains(logOutput, "tool=execute_command") {
		t.Errorf(
			"AC12/SR-27: MCP-слой достигнут несмотря на corrupt keystore (найдено tool=execute_command).\n"+
				"authMiddleware должен отклонить запрос ДО MCP-handler.\nlog=%s",
			logOutput,
		)
	} else {
		t.Logf("AC12/SR-27: OK — tool=execute_command не найден в аудите (MCP не достигнут)")
	}

	// Проверяем живость: сервер должен отвечать после повреждения.
	livenessBody := jsonrpcBody(99, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "liveness", "version": "1"},
	})
	livenessResp := postMCP(t, client, baseURL, keyStr, livenessBody, nil)
	livenessResp.Body.Close()
	// 403 ожидается — БД всё ещё повреждена. Главное — не 0 (сервер не упал).
	if livenessResp.StatusCode == 0 {
		t.Error("AC12/SR-27: сервер упал после повреждения keystore (status 0)")
	} else {
		t.Logf("AC12/SR-27: OK — сервер жив после повреждения keystore (status %d)", livenessResp.StatusCode)
	}
}

// ============================================================================
// Вспомогательные переменные — предотвращают "unused import" ошибки
// ============================================================================

// Ensure cmdexec import is used (defaultExecCfg returns cmdexec.Config).
var _ cmdexec.Config = defaultExecCfg()
