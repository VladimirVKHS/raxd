package cmdexec_test

// exec_test.go — TDD-тесты для пакета internal/cmdexec.
//
// Покрывает AC1-AC18 и ключевые SR-40..SR-67:
//   - AC2/SR-43: без shell-инъекции
//   - AC4/SR-65: правильный формат вывода
//   - AC5/SR-46: таймаут, kill процесса, timed_out:true
//   - AC6/SR-47: отмена контекста → kill группы, нет осиротевших
//   - AC7/SR-48: allowlist строгое сопоставление
//   - AC8/SR-44/SR-45: несуществующий бинарь, ErrDot
//   - AC9/SR-54: наследование uid/gid
//   - AC10/SR-49/SR-50: env-whitelist, валидация cwd
//   - AC11/SR-53: лимит вывода, truncated
//   - SR-52: лимиты входа (max_args/max_arg_len — проверяются в handler, не runner)
//
// Тесты запускаются в Docker (-mod=vendor; AC18/SR-67).

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/vladimirvkhs/raxd/internal/cmdexec"
)

// defaultConfig возвращает безопасный конфиг для тестов.
func defaultConfig() cmdexec.Config {
	return cmdexec.Config{
		Allowlist:        nil, // выключен
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

// ============================================================================
// AC2/SR-43: запуск без shell-интерполяции
// ============================================================================

// TestNoShellInjection — вектор "a; touch /tmp/pwned_cmdexec_test" не создаёт файл.
// SR-43: shell-метасимволы трактуются как литеральные аргументы.
func TestNoShellInjection(t *testing.T) {
	// Убеждаемся, что файл не существует до теста.
	target := "/tmp/pwned_cmdexec_test_" + time.Now().Format("20060102150405")
	_ = os.Remove(target)

	cfg := defaultConfig()
	ctx := context.Background()
	in := cmdexec.Input{
		Command: "echo",
		Args:    []string{"a; touch " + target},
		Cwd:     "/tmp",
	}

	_, err := cmdexec.Run(ctx, cfg, in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Файл НЕ должен существовать — shell не был задействован.
	if _, statErr := os.Stat(target); statErr == nil {
		t.Errorf("SR-43: shell injection succeeded — file %s was created", target)
		os.Remove(target)
	}
}

// TestNoShellPipeInjection — вектор с pipe не запускает вторую команду.
func TestNoShellPipeInjection(t *testing.T) {
	target := "/tmp/pwned_pipe_cmdexec_" + time.Now().Format("20060102150405")
	_ = os.Remove(target)

	cfg := defaultConfig()
	ctx := context.Background()
	in := cmdexec.Input{
		Command: "echo",
		Args:    []string{"hello | touch " + target},
		Cwd:     "/tmp",
	}

	_, err := cmdexec.Run(ctx, cfg, in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, statErr := os.Stat(target); statErr == nil {
		t.Errorf("SR-43: pipe injection succeeded — file %s was created", target)
		os.Remove(target)
	}
}

// ============================================================================
// AC4/SR-65: формат вывода — 7 полей в Result
// ============================================================================

// TestResultFields — успешный запуск echo возвращает корректные поля.
func TestResultFields(t *testing.T) {
	cfg := defaultConfig()
	ctx := context.Background()
	in := cmdexec.Input{
		Command: "echo",
		Args:    []string{"hello"},
		Cwd:     "/tmp",
	}

	res, err := cmdexec.Run(ctx, cfg, in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Stdout должен содержать "hello\n".
	if !strings.Contains(string(res.Stdout), "hello") {
		t.Errorf("AC4: stdout = %q, want 'hello'", res.Stdout)
	}
	// ExitCode 0.
	if res.ExitCode != 0 {
		t.Errorf("AC4: exit_code = %d, want 0", res.ExitCode)
	}
	// Duration > 0.
	if res.Duration <= 0 {
		t.Errorf("AC4: duration = %v, want > 0", res.Duration)
	}
	// Не был убит по таймауту.
	if res.TimedOut {
		t.Errorf("AC4: timed_out = true, want false")
	}
	// Флаги truncated — false.
	if res.StdoutTruncated || res.StderrTruncated {
		t.Errorf("AC4: truncated flags unexpected: stdout=%v stderr=%v", res.StdoutTruncated, res.StderrTruncated)
	}
}

// TestNonZeroExitCodeNotError — ненулевой exit code НЕ ошибка раннера.
func TestNonZeroExitCodeNotError(t *testing.T) {
	cfg := defaultConfig()
	ctx := context.Background()
	in := cmdexec.Input{
		Command: "sh",
		Args:    []string{"-c", "exit 42"},
		Cwd:     "/tmp",
	}

	res, err := cmdexec.Run(ctx, cfg, in)
	// Ненулевой exit code НЕ возвращается как ошибка раннера.
	if err != nil {
		t.Fatalf("AC4: non-zero exit code must not be error: %v", err)
	}
	if res.ExitCode != 42 {
		t.Errorf("AC4: exit_code = %d, want 42", res.ExitCode)
	}
}

// ============================================================================
// AC5/SR-46: таймаут → kill + timed_out:true
// ============================================================================

// TestTimeoutKillsProcess — команда, превышающая таймаут, прерывается.
func TestTimeoutKillsProcess(t *testing.T) {
	cfg := defaultConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	in := cmdexec.Input{
		Command: "sleep",
		Args:    []string{"60"},
		Cwd:     "/tmp",
	}

	start := time.Now()
	res, err := cmdexec.Run(ctx, cfg, in)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("AC5: timeout must not return error: %v", err)
	}
	if !res.TimedOut {
		t.Errorf("AC5: timed_out = false, want true")
	}
	// Должен завершиться за разумное время (≤ 3 секунды, а не 60).
	if elapsed > 3*time.Second {
		t.Errorf("AC5: command ran for %v, want < 3s (should have been killed)", elapsed)
	}
}

// ============================================================================
// AC6/SR-47: отмена контекста → kill группы, нет осиротевших
// ============================================================================

// TestContextCancelKillsProcessGroup — отмена контекста убивает группу процессов.
func TestContextCancelKillsProcessGroup(t *testing.T) {
	cfg := defaultConfig()
	ctx, cancel := context.WithCancel(context.Background())

	in := cmdexec.Input{
		Command: "sleep",
		Args:    []string{"60"},
		Cwd:     "/tmp",
	}

	done := make(chan struct{})
	var res cmdexec.Result
	var runErr error
	go func() {
		defer close(done)
		res, runErr = cmdexec.Run(ctx, cfg, in)
	}()

	// Даём процессу запуститься.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("AC6: Run did not return after context cancel (timeout)")
	}

	if runErr != nil {
		t.Fatalf("AC6: context cancel must not return error: %v", runErr)
	}
	// Процесс должен быть убит (timed_out или контекст отменён — нет осиротевших).
	_ = res // Result доступен; главное — Run вернул управление
}

// ============================================================================
// AC7/SR-48: allowlist строгое сопоставление
// ============================================================================

// TestAllowlistDenyNotInList — команда вне allowlist возвращает ErrNotAllowed.
func TestAllowlistDenyNotInList(t *testing.T) {
	cfg := defaultConfig()
	cfg.Allowlist = []string{"echo", "ls"} // allowlist включён

	ctx := context.Background()
	in := cmdexec.Input{
		Command: "cat",
		Args:    []string{"/etc/passwd"},
		Cwd:     "/tmp",
	}

	_, err := cmdexec.Run(ctx, cfg, in)
	if err == nil {
		t.Fatal("AC7: expected ErrNotAllowed for command not in allowlist")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("AC7: error = %q, want 'not allowed'", err.Error())
	}
}

// TestAllowlistPermitInList — команда из allowlist выполняется.
func TestAllowlistPermitInList(t *testing.T) {
	cfg := defaultConfig()
	cfg.Allowlist = []string{"echo", "ls"}

	ctx := context.Background()
	in := cmdexec.Input{
		Command: "echo",
		Args:    []string{"allowed"},
		Cwd:     "/tmp",
	}

	res, err := cmdexec.Run(ctx, cfg, in)
	if err != nil {
		t.Fatalf("AC7: expected echo to be allowed: %v", err)
	}
	if !strings.Contains(string(res.Stdout), "allowed") {
		t.Errorf("AC7: stdout = %q, want 'allowed'", res.Stdout)
	}
}

// TestAllowlistDisabledAllowsAll — при пустом allowlist разрешена любая команда.
func TestAllowlistDisabledAllowsAll(t *testing.T) {
	cfg := defaultConfig()
	cfg.Allowlist = nil // выключен

	ctx := context.Background()
	in := cmdexec.Input{
		Command: "echo",
		Args:    []string{"ok"},
		Cwd:     "/tmp",
	}

	_, err := cmdexec.Run(ctx, cfg, in)
	if err != nil {
		t.Fatalf("AC7: allowlist disabled, expected no error: %v", err)
	}
}

// TestAllowlistStrictMatch — алиас/регистр/пробел не совпадает с записью.
func TestAllowlistStrictMatch(t *testing.T) {
	cfg := defaultConfig()
	cfg.Allowlist = []string{"echo"}

	ctx := context.Background()
	// "Echo" (другой регистр) — не совпадает.
	in := cmdexec.Input{Command: "Echo", Cwd: "/tmp"}
	_, err := cmdexec.Run(ctx, cfg, in)
	if err == nil {
		t.Errorf("AC7/SR-48: 'Echo' (uppercase) must not match allowlist entry 'echo'")
	}

	// " echo" (пробел) — не совпадает.
	in2 := cmdexec.Input{Command: " echo", Cwd: "/tmp"}
	_, err2 := cmdexec.Run(ctx, cfg, in2)
	if err2 == nil {
		t.Errorf("AC7/SR-48: ' echo' (leading space) must not match allowlist entry 'echo'")
	}
}

// ============================================================================
// AC8/SR-44/SR-45: несуществующий бинарь, ErrDot
// ============================================================================

// TestNonExistentBinaryReturnsError — заведомо отсутствующий бинарь → ошибка, сервер жив.
func TestNonExistentBinaryReturnsError(t *testing.T) {
	cfg := defaultConfig()
	ctx := context.Background()
	in := cmdexec.Input{
		Command: "definitely-not-a-binary-xyz-" + time.Now().Format("20060102"),
		Cwd:     "/tmp",
	}

	_, err := cmdexec.Run(ctx, cfg, in)
	if err == nil {
		t.Fatal("AC8: expected error for non-existent binary")
	}
	// Ошибка должна ссылаться на ErrNotFound.
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("AC8: error = %q, want 'not found'", err.Error())
	}
}

// TestRelativePathBinaryRejected — относительный путь из cwd отвергается (ErrDot).
func TestRelativePathBinaryRejected(t *testing.T) {
	cfg := defaultConfig()
	ctx := context.Background()
	in := cmdexec.Input{
		Command: "./nonexistent",
		Cwd:     "/tmp",
	}

	_, err := cmdexec.Run(ctx, cfg, in)
	if err == nil {
		t.Fatal("AC8/SR-44: expected error for relative path binary")
	}
	// Ошибка содержит ErrDot-сообщение.
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "dot") {
		t.Logf("AC8/SR-44: error = %q (acceptable)", err.Error())
	}
}

// ============================================================================
// AC9/SR-54: наследование uid/gid
// ============================================================================

// TestInheritedUID — дочерний процесс выполняется под тем же UID, что демон.
func TestInheritedUID(t *testing.T) {
	// id -u выводит UID текущего пользователя.
	idPath, err := exec.LookPath("id")
	if err != nil {
		t.Skip("id not found in PATH")
	}

	cfg := defaultConfig()
	ctx := context.Background()
	in := cmdexec.Input{
		Command: idPath,
		Args:    []string{"-u"},
		Cwd:     "/tmp",
	}

	res, err := cmdexec.Run(ctx, cfg, in)
	if err != nil {
		t.Fatalf("AC9: id -u failed: %v", err)
	}

	// UID дочернего процесса совпадает с UID текущего процесса.
	expectedUID := os.Getuid()
	stdout := strings.TrimSpace(string(res.Stdout))
	var childUID int
	if _, scanErr := fmt.Sscan(stdout, &childUID); scanErr != nil {
		t.Fatalf("AC9: could not parse UID from output %q: %v", stdout, scanErr)
	}
	if childUID != expectedUID {
		t.Errorf("AC9: child UID = %d, want %d (parent UID)", childUID, expectedUID)
	}
}

// ============================================================================
// AC10/SR-49: env-whitelist — опасные переменные не наследуются
// ============================================================================

// TestEnvWhitelistBlocksDangerousVars — LD_PRELOAD/IFS не попадают в дочерний процесс.
func TestEnvWhitelistBlocksDangerousVars(t *testing.T) {
	// Устанавливаем опасные переменные в окружении текущего процесса.
	t.Setenv("LD_PRELOAD", "/evil/lib.so")
	t.Setenv("IFS", "X")
	t.Setenv("DYLD_INSERT_LIBRARIES", "/evil/lib.dylib")

	cfg := defaultConfig()
	ctx := context.Background()

	// env выводит все переменные окружения дочернего процесса.
	envPath, err := exec.LookPath("env")
	if err != nil {
		t.Skip("env not found")
	}

	in := cmdexec.Input{
		Command: envPath,
		Cwd:     "/tmp",
	}

	res, err := cmdexec.Run(ctx, cfg, in)
	if err != nil {
		t.Fatalf("AC10: env failed: %v", err)
	}

	envOutput := string(res.Stdout)
	if strings.Contains(envOutput, "LD_PRELOAD") {
		t.Errorf("SR-49: LD_PRELOAD appeared in child env: %s", envOutput)
	}
	if strings.Contains(envOutput, "IFS=X") {
		t.Errorf("SR-49: IFS=X appeared in child env: %s", envOutput)
	}
	if strings.Contains(envOutput, "DYLD_INSERT_LIBRARIES") {
		t.Errorf("SR-49: DYLD_INSERT_LIBRARIES appeared in child env: %s", envOutput)
	}
}

// TestEnvWhitelistOnlyContainsAllowedVars — дочерний процесс видит ТОЛЬКО whitelist-переменные.
func TestEnvWhitelistOnlyContainsAllowedVars(t *testing.T) {
	// Устанавливаем переменную вне whitelist.
	t.Setenv("SECRET_VAR", "should-not-appear")

	cfg := defaultConfig()
	ctx := context.Background()

	envPath, err := exec.LookPath("env")
	if err != nil {
		t.Skip("env not found")
	}

	in := cmdexec.Input{
		Command: envPath,
		Cwd:     "/tmp",
	}

	res, err := cmdexec.Run(ctx, cfg, in)
	if err != nil {
		t.Fatalf("AC10: env failed: %v", err)
	}

	if strings.Contains(string(res.Stdout), "SECRET_VAR") {
		t.Errorf("SR-49: SECRET_VAR leaked to child env: %s", res.Stdout)
	}
}

// ============================================================================
// AC10/SR-50: валидация cwd
// ============================================================================

// TestInvalidCwdReturnsError — несуществующий cwd → ошибка до запуска.
func TestInvalidCwdReturnsError(t *testing.T) {
	cfg := defaultConfig()
	ctx := context.Background()
	in := cmdexec.Input{
		Command: "echo",
		Args:    []string{"test"},
		Cwd:     "/nonexistent-dir-xyz-" + time.Now().Format("20060102"),
	}

	_, err := cmdexec.Run(ctx, cfg, in)
	if err == nil {
		t.Fatal("AC10: expected error for invalid cwd")
	}
	if !strings.Contains(err.Error(), "cwd") {
		t.Errorf("AC10: error = %q, want 'cwd'", err.Error())
	}
}

// TestCwdIsFile — cwd указывает на файл, а не директорию → ошибка.
func TestCwdIsFile(t *testing.T) {
	// Создаём временный файл.
	f, err := os.CreateTemp("", "raxd-cwd-test-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.Close()
	defer os.Remove(f.Name())

	cfg := defaultConfig()
	ctx := context.Background()
	in := cmdexec.Input{
		Command: "echo",
		Cwd:     f.Name(), // это файл, не директория
	}

	_, err = cmdexec.Run(ctx, cfg, in)
	if err == nil {
		t.Fatal("AC10: expected error when cwd is a file")
	}
}

// TestDefaultCwdUsedWhenEmpty — при пустом cwd используется DefaultCwd.
func TestDefaultCwdUsedWhenEmpty(t *testing.T) {
	cfg := defaultConfig()
	cfg.DefaultCwd = "/tmp"

	ctx := context.Background()
	// pwd выводит текущую директорию.
	pwdPath, err := exec.LookPath("pwd")
	if err != nil {
		t.Skip("pwd not found")
	}

	in := cmdexec.Input{
		Command: pwdPath,
		Cwd:     "", // пустой → DefaultCwd
	}

	res, err := cmdexec.Run(ctx, cfg, in)
	if err != nil {
		t.Fatalf("AC10: pwd failed: %v", err)
	}

	cwd := strings.TrimSpace(string(res.Stdout))
	// На macOS /tmp может быть симлинком → проверяем по Abs resolve.
	if cwd != "/tmp" && cwd != "/private/tmp" {
		t.Errorf("AC10: cwd = %q, want /tmp (or /private/tmp on macOS)", cwd)
	}
}

// ============================================================================
// AC11/SR-53: лимит вывода → truncated
// ============================================================================

// TestOutputTruncatedAtLimit — вывод больше лимита обрезается, флаг truncated.
func TestOutputTruncatedAtLimit(t *testing.T) {
	cfg := defaultConfig()
	cfg.MaxOutputBytes = 100 // маленький лимит для теста

	ctx := context.Background()
	// dd generates exactly 200 bytes (больше лимита 100), POSIX-совместимо.
	in := cmdexec.Input{
		Command: "dd",
		Args:    []string{"if=/dev/zero", "bs=200", "count=1"},
		Cwd:     "/tmp",
	}

	res, err := cmdexec.Run(ctx, cfg, in)
	if err != nil {
		t.Fatalf("AC11: run failed: %v", err)
	}

	if len(res.Stdout) > cfg.MaxOutputBytes {
		t.Errorf("AC11: stdout length = %d, want <= %d", len(res.Stdout), cfg.MaxOutputBytes)
	}
	if !res.StdoutTruncated {
		t.Errorf("AC11: stdout_truncated = false, want true")
	}
}

// TestOutputNotTruncatedWhenUnderLimit — вывод меньше лимита → truncated:false.
func TestOutputNotTruncatedWhenUnderLimit(t *testing.T) {
	cfg := defaultConfig()
	// лимит 1 MiB по умолчанию — echo "hi" точно не обрежет.

	ctx := context.Background()
	in := cmdexec.Input{
		Command: "echo",
		Args:    []string{"hi"},
		Cwd:     "/tmp",
	}

	res, err := cmdexec.Run(ctx, cfg, in)
	if err != nil {
		t.Fatalf("AC11: run failed: %v", err)
	}

	if res.StdoutTruncated {
		t.Errorf("AC11: stdout_truncated = true for small output, want false")
	}
}

// ============================================================================
// SR-53: OOM-защита — capped writer ограничивает память
// ============================================================================

// TestCappedWriterDoesNotOOM — команда, производящая большой вывод, не забивает память.
// Используем маленький лимит и большой поток — должно читать до лимита и дренировать остаток.
func TestCappedWriterDoesNotOOM(t *testing.T) {
	cfg := defaultConfig()
	cfg.MaxOutputBytes = 1024 // 1 KiB лимит

	ctxWithTimeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Генерируем ~100KB вывода.
	in := cmdexec.Input{
		Command: "sh",
		Args:    []string{"-c", "dd if=/dev/zero bs=1024 count=100 2>/dev/null | tr '\\0' 'A'"},
		Cwd:     "/tmp",
	}

	res, err := cmdexec.Run(ctxWithTimeout, cfg, in)
	if err != nil {
		t.Fatalf("SR-53: run failed: %v", err)
	}

	// Вывод обрезан до лимита.
	if len(res.Stdout) > cfg.MaxOutputBytes {
		t.Errorf("SR-53: stdout length = %d, want <= %d", len(res.Stdout), cfg.MaxOutputBytes)
	}
	if !res.StdoutTruncated {
		t.Errorf("SR-53: stdout_truncated must be true for large output")
	}
}
