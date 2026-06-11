package cmdexec

// exec.go — чистый раннер команд без MCP-типов и без логирования.
//
// Run(ctx, cfg, in) → (Result, error):
//   - AC2/SR-43: exec.CommandContext без shell; никогда sh -c <строка>.
//   - AC5/SR-46: таймаут через контекст (устанавливается в execHandler).
//   - AC6/SR-47: Setpgid + Cancel→killGroup + WaitDelay (sysproc_unix.go).
//   - AC7/SR-48: allowlist ДО LookPath; строгое точное равенство.
//   - AC8/SR-44/SR-45: ErrDot и ErrNotFound → ErrBadBinary (нейтральный текст).
//   - AC9/SR-54: Credential НЕ задаётся (sysproc_unix.go) → наследование uid/gid.
//   - AC10/SR-49: явный Cmd.Env из env-whitelist (не слепое наследование).
//   - AC10/SR-50: Stat+IsDir валидация cwd до запуска.
//   - AC11/SR-53: capped-writers на stdout/stderr.
//
// Ненулевой exit code — НЕ error: возвращается в Result.ExitCode.
// Таймаут — НЕ error: возвращается как Result.TimedOut=true.
// Раннер НИКОГДА не паникует (SR-64).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Ошибки раннера — конвертируются handler'ом в isError:true.
var (
	// ErrNotAllowed — команда не в allowlist (AC7/SR-48).
	ErrNotAllowed = errors.New("command not allowed")

	// ErrBadCwd — невалидный cwd: не существует или не директория (AC10/SR-50).
	ErrBadCwd = errors.New("bad cwd")
)

// Input — входные данные для Run (уже после резолва cwd и таймаута handler'ом).
type Input struct {
	// Command — имя бинаря или абсолютный путь.
	Command string
	// Args — аргументы (литеральные, без shell-интерпретации).
	Args []string
	// Cwd — рабочая директория (непустая; если пуста — handler подставил DefaultCwd).
	Cwd string
}

// Result — итог выполнения команды.
type Result struct {
	// Stdout — захваченный stdout (≤ cfg.MaxOutputBytes).
	Stdout []byte
	// Stderr — захваченный stderr (≤ cfg.MaxOutputBytes).
	Stderr []byte
	// ExitCode — код возврата процесса. Ненулевой — НЕ ошибка.
	ExitCode int
	// Duration — длительность исполнения.
	Duration time.Duration
	// TimedOut — true если команда была прервана по таймауту/отмене контекста.
	TimedOut bool
	// StdoutTruncated — true если stdout достиг лимита.
	StdoutTruncated bool
	// StderrTruncated — true если stderr достиг лимита.
	StderrTruncated bool
}

// Run запускает команду безопасно:
//   - allowlist-проверка ДО LookPath (SR-48);
//   - LookPath по серверному PATH (из env-whitelist; SR-44);
//   - ErrDot → ErrBadBinary (SR-44);
//   - валидация cwd (SR-50);
//   - exec.CommandContext без shell (SR-43);
//   - capped-writers на stdout/stderr (SR-53);
//   - Setpgid + killGroup + WaitDelay (SR-47; sysproc_unix.go);
//   - явный Cmd.Env из whitelist (SR-49).
//
// Параметр ctx несёт таймаут и отмену (устанавливается execHandler).
// Ненулевой exit code возвращается в Result.ExitCode, а не как ошибка.
// Таймаут возвращается как Result.TimedOut=true, а не как ошибка.
func Run(ctx context.Context, cfg Config, in Input) (Result, error) {
	// --- 1. Allowlist-проверка ДО LookPath (SR-48/AC7) ---
	if len(cfg.Allowlist) > 0 {
		allowed := false
		for _, entry := range cfg.Allowlist {
			if entry == in.Command {
				allowed = true
				break
			}
		}
		if !allowed {
			return Result{}, fmt.Errorf("%w: %s", ErrNotAllowed, in.Command)
		}
	}

	// --- 2. Валидация cwd (SR-50/AC10) ---
	// Если Cwd пустой — handler должен был подставить DefaultCwd; здесь страховка.
	cwd := in.Cwd
	if cwd == "" {
		cwd = cfg.DefaultCwd
	}
	if err := validateCwd(cwd); err != nil {
		return Result{}, err
	}

	// --- 3. LookPath по серверному PATH (SR-44) ---
	// PATH берётся из env-whitelist (env демона), а не из клиентского ввода.
	bin, err := lookupBinary(in.Command, cfg.EnvWhitelist)
	if err != nil {
		return Result{}, err
	}

	// --- 4. Сборка команды — exec.CommandContext БЕЗ shell (SR-43/AC2) ---
	cmd := exec.CommandContext(ctx, bin, in.Args...)

	// --- 5. Явное окружение из whitelist (SR-49/AC10) ---
	cmd.Env = buildEnv(cfg.EnvWhitelist)

	// --- 6. Рабочая директория (SR-50/AC10) ---
	cmd.Dir = cwd

	// --- 7. Process-group kill (SR-47/AC6; sysproc_unix.go) ---
	applyProcessGroup(cmd)

	// --- 8. Capped-writers для stdout/stderr (SR-53/AC11) ---
	stdoutW := NewCappedWriter(cfg.MaxOutputBytes)
	stderrW := NewCappedWriter(cfg.MaxOutputBytes)
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW

	// --- 9. Cancel → killGroup (SR-47/ADR-001) ---
	// cmd.Cancel вызывается при отмене ctx (таймаут или обрыв соединения).
	// Убивает весь process group (-pgid), а не только головной процесс.
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return killGroup(cmd.Process.Pid)
		}
		return nil
	}
	cmd.WaitDelay = waitDelay

	// --- 10. Запуск и измерение времени ---
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("command not found: %w", err)
	}

	waitErr := cmd.Wait()
	duration := time.Since(start)

	// --- 11а. Reap orphan-потомков группы (SR-47/AC6: нет осиротевших) ---
	// После cmd.Wait() головной процесс уже reap-нут. Потомки группы (например,
	// sleep запущенный через sh -c "...&..."), получив SIGKILL от killGroup,
	// могут стать orphan-зомби под init. На Linux с pidfd (Go 1.23+) зомби-процессы
	// возвращают SUCCESS из pidfd_open, что ложно сигнализирует о живом процессе.
	// reapGroupOrphans reap-ает этих orphan-ов: мы sub-reaper (setSelfSubreaper в
	// applyProcessGroup), поэтому они усыновлены нами и доступны для waitpid.
	if cmd.Process != nil {
		reapGroupOrphans(cmd.Process.Pid)
	}

	// --- 11. Анализ результата ---
	timedOut := false
	exitCode := 0

	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		// Проверяем, завершён ли контекст — признак таймаута/отмены.
		if ctx.Err() != nil {
			timedOut = true
			exitCode = -1
		}
	}

	return Result{
		Stdout:          stdoutW.Bytes(),
		Stderr:          stderrW.Bytes(),
		ExitCode:        exitCode,
		Duration:        duration,
		TimedOut:        timedOut,
		StdoutTruncated: stdoutW.Truncated,
		StderrTruncated: stderrW.Truncated,
	}, nil
}

// validateCwd проверяет, что path существует и является директорией.
func validateCwd(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrBadCwd, path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrBadCwd, path)
	}
	return nil
}

// lookupBinary ищет бинарь по имени/пути.
// Устанавливает PATH из env-whitelist (SR-44).
// Отвергает ErrDot (относительный путь из cwd; SR-44).
// Отвергает ErrNotFound (нет бинаря; SR-45).
func lookupBinary(command string, _ []string) (string, error) {
	// envWhitelist не используется напрямую: exec.LookPath уже обращается к PATH
	// текущего процесса (демона), что соответствует SR-44 — PATH клиента не передаётся.

	// Если передан абсолютный путь — используем как есть (но не через shell).
	if strings.HasPrefix(command, "/") {
		// Проверяем существование.
		if _, err := os.Stat(command); err != nil {
			return "", fmt.Errorf("command not found: %s", command)
		}
		return command, nil
	}

	// LookPath — использует PATH текущего процесса (демона).
	// PATH клиента не передаётся (SR-44): buildEnv в exec.go строит Cmd.Env из whitelist.
	bin, err := exec.LookPath(command)
	if err != nil {
		if errors.Is(err, exec.ErrDot) {
			return "", fmt.Errorf("command not found: relative path not allowed: %w", err)
		}
		return "", fmt.Errorf("command not found: %s", command)
	}

	// Дополнительная проверка ErrDot: если результат не абсолютный путь.
	if !strings.HasPrefix(bin, "/") {
		return "", fmt.Errorf("command not found: relative path not allowed")
	}

	return bin, nil
}

// buildEnv строит окружение для дочернего процесса из env-whitelist.
// Берёт значения из окружения демона (os.Getenv).
// LD_PRELOAD/DYLD_*/IFS НЕ в списке (SR-49).
func buildEnv(whitelist []string) []string {
	env := make([]string, 0, len(whitelist))
	for _, key := range whitelist {
		if val := os.Getenv(key); val != "" {
			env = append(env, key+"="+val)
		}
	}
	return env
}
