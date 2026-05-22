package cmdexec_test

// exec_qa_test.go — QA-тесты раннера internal/cmdexec (дополнение к exec_test.go).
//
// Закрывают пробелы, выявленные независимым QA-анализом:
//   - AC6/SR-47: TestContextCancelKillsChildren — дочерние процессы убиты после cancel
//   - SR-49:     TestEnvWhitelistBlocksLdLibraryPath — LD_LIBRARY_PATH не в дочернем
//
// Тесты запускаются только в Docker (-mod=vendor; AC18/SR-67).
// Не правят продуктовый код — тесты поведения.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/vladimirvkhs/raxd/internal/cmdexec"
)

// ============================================================================
// AC6/SR-47: process-group kill — дочерние процессы убиты после cancel
// ============================================================================

// TestContextCancelKillsChildren проверяет, что после отмены контекста дочерний
// процесс (потомок основного процесса команды) действительно мёртв.
//
// SR-47: "kill всего дерева, осиротевших процессов не остаётся".
// Стратегия: запустить sh -c "sleep 120 & wait", дать ему запустить sleep,
// отменить контекст, дождаться завершения Run, затем проверить что sleep
// (дочерний процесс sh) мёртв через os.FindProcess + Process.Signal(0).
//
// На Darwin/Linux: Signal(0) на мёртвый процесс → ESRCH или EPERM (не nil == жив).
// После killGroup(-pgid) sleep должен быть мёртв → Signal(0) вернёт ESRCH.
func TestContextCancelKillsChildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group kill не реализован на Windows (вне scope)")
	}

	// Маркер-файл: sleep запишет свой PID туда, чтобы мы его нашли.
	markerFile := fmt.Sprintf("/tmp/qa_child_pid_%d.txt", time.Now().UnixNano())
	_ = os.Remove(markerFile)
	defer os.Remove(markerFile)

	cfg := defaultConfig()

	ctx, cancel := context.WithCancel(context.Background())

	// Запускаем sh -c "sleep 120 & echo $! > <marker>; wait".
	// sleep 120 — долгоживущий дочерний процесс.
	// echo $! записывает PID sleep в маркер, чтобы тест мог его найти.
	// wait — sh ждёт sleep (не завершается сразу).
	script := fmt.Sprintf("sleep 120 & echo $! > %s; wait", markerFile)
	in := cmdexec.Input{
		Command: "sh",
		Args:    []string{"-c", script},
		Cwd:     "/tmp",
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = cmdexec.Run(ctx, cfg, in)
	}()

	// Ждём пока маркер появится (sleep запустился и записал свой PID).
	var childPID int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(markerFile)
		if err == nil && len(strings.TrimSpace(string(data))) > 0 {
			pidStr := strings.TrimSpace(string(data))
			if _, scanErr := fmt.Sscan(pidStr, &childPID); scanErr == nil && childPID > 0 {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	if childPID == 0 {
		t.Fatal("AC6/SR-47: не удалось получить PID дочернего sleep-процесса за 5s")
	}
	t.Logf("AC6/SR-47: дочерний sleep PID = %d; отменяем контекст...", childPID)

	// Отменяем контекст → Run должен вызвать killGroup(-pgid, SIGKILL).
	cancel()

	// Ждём завершения Run.
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("AC6/SR-47: Run не вернул управление за 10s после cancel")
	}

	// Проверяем что дочерний процесс мёртв.
	// Даём 500ms на завершение (kill асинхронен).
	time.Sleep(500 * time.Millisecond)

	proc, err := os.FindProcess(childPID)
	if err != nil {
		// На некоторых системах FindProcess с несуществующим PID возвращает ошибку.
		t.Logf("AC6/SR-47: os.FindProcess(%d) error (процесс скорее всего мёртв): %v", childPID, err)
		return
	}

	// Signal(0) — нулевой сигнал: проверяет живость без реального сигнала.
	// Если процесс мёртв → ESRCH (no such process) или EPERM (нет прав, но процесс есть).
	// Мёртвый зомби даёт ESRCH после reap. Живой процесс даёт nil или EPERM.
	signalErr := proc.Signal(syscall.Signal(0))
	if signalErr == nil {
		// Signal(0) вернул nil → процесс ещё жив.
		t.Errorf("AC6/SR-47: PRODUCT BUG: дочерний sleep (PID %d) жив после killGroup!\n"+
			"SR-47 требует kill всего дерева процессов при cancel. Эскалируй к developer.",
			childPID)
	} else if errors.Is(signalErr, syscall.ESRCH) {
		// ESRCH: процесс не существует → мёртв. Это ожидаемый результат.
		t.Logf("AC6/SR-47: OK — дочерний sleep (PID %d) мёртв (ESRCH)", childPID)
	} else if errors.Is(signalErr, os.ErrProcessDone) {
		t.Logf("AC6/SR-47: OK — дочерний sleep (PID %d) мёртв (ErrProcessDone)", childPID)
	} else {
		// Другая ошибка (например EPERM — нет прав посылать сигнал).
		// В Docker обычно не бывает EPERM для собственных процессов.
		// Логируем как информацию — не считаем проблемой безопасности.
		t.Logf("AC6/SR-47: Signal(0) для PID %d вернул %v (ожидается ESRCH; возможно уже завершился)", childPID, signalErr)
	}
}

// ============================================================================
// SR-49: env-whitelist — LD_LIBRARY_PATH не передаётся в дочерний процесс
// ============================================================================

// TestEnvWhitelistBlocksLdLibraryPath — LD_LIBRARY_PATH (линуксовый аналог
// DYLD_INSERT_LIBRARIES) не должен попасть в окружение дочернего процесса.
//
// SR-49: env-whitelist дефолт ["PATH","HOME","LANG","TERM"]; LD_LIBRARY_PATH
// не в списке — даже если задан в окружении демона, дочерний не получает.
// CWE-426/427: динамический загрузчик использует LD_LIBRARY_PATH для
// перехвата библиотек (аналог LD_PRELOAD).
func TestEnvWhitelistBlocksLdLibraryPath(t *testing.T) {
	// Устанавливаем LD_LIBRARY_PATH в окружении текущего процесса (демон).
	t.Setenv("LD_LIBRARY_PATH", "/evil/lib:/another/evil")

	cfg := defaultConfig()
	// Убеждаемся, что LD_LIBRARY_PATH нет в whitelist.
	for _, v := range cfg.EnvWhitelist {
		if v == "LD_LIBRARY_PATH" {
			t.Fatalf("SR-49: LD_LIBRARY_PATH не должен быть в EnvWhitelist: %v", cfg.EnvWhitelist)
		}
	}

	envPath, err := exec.LookPath("env")
	if err != nil {
		t.Skip("env не найден в PATH — пропускаем тест")
	}

	ctx := context.Background()
	in := cmdexec.Input{
		Command: envPath,
		Cwd:     "/tmp",
	}

	res, err := cmdexec.Run(ctx, cfg, in)
	if err != nil {
		t.Fatalf("SR-49: env завершился с ошибкой: %v", err)
	}

	envOutput := string(res.Stdout)
	if strings.Contains(envOutput, "LD_LIBRARY_PATH") {
		t.Errorf("SR-49: PRODUCT SECURITY BUG: LD_LIBRARY_PATH передан в дочерний процесс!\n"+
			"Это вектор перехвата исполнения (CWE-426/427).\n"+
			"Эскалируй к developer — НЕ ослабляй этот ассерт.\n"+
			"env output: %s", envOutput)
	} else {
		t.Logf("SR-49: OK — LD_LIBRARY_PATH отсутствует в окружении дочернего процесса")
	}
}
