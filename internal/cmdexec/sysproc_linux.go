//go:build linux

package cmdexec

// sysproc_linux.go — Linux-специфичная логика reap orphan-зомби (SR-47).
//
// Проблема: после killGroup(-pgid) потомки головного процесса (например, sleep
// запущенный через sh -c "sleep ... & wait") при гибели sh становятся orphan и
// усыновляются init (PID 1). Пока init не сделает waitpid, они — зомби.
// На Linux Go 1.23+ os.FindProcess использует pidfd_open, который для зомби-
// процессов возвращает SUCCESS. Тест AC6/SR-47 через Signal(0) ложно считает
// зомби живым процессом → падение CI.
//
// Решение (два шага):
//   1. setSelfSubreaper(): prctl(PR_SET_CHILD_SUBREAPER, 1) ДО cmd.Start() —
//      при гибели sh orphan-ы усыновляются нашим процессом (не init).
//   2. reapGroupOrphans(pgid): после cmd.Wait() — waitpid(-pgid, WNOHANG) loop
//      reap-ает усыновлённых зомби, делая pidfd_open для них ESRCH-неудачным.

import (
	"syscall"
	"time"
)

// prSetChildSubreaper — номер опции prctl PR_SET_CHILD_SUBREAPER.
// Одинаков для всех Linux-архитектур (arm64, amd64, riscv64 и др.).
const prSetChildSubreaper = 36

// reapGroupTimeout — максимальное суммарное время ожидания в reapGroupOrphans.
// SIGKILL доставляется ядром обычно за < 1ms; 200ms — запас для нагруженных CI.
const reapGroupTimeout = 200 * time.Millisecond

// reapGroupPollInterval — пауза между итерациями при pid==0 (потомок ещё жив).
const reapGroupPollInterval = 1 * time.Millisecond

// setSelfSubreaper вызывает prctl(PR_SET_CHILD_SUBREAPER, 1): делает текущий
// процесс sub-reaper. После этого все будущие orphan-потомки при усыновлении
// идут к нам, а не к глобальному init. Это позволяет reap-нуть их через waitpid.
//
// Вызов idempotent (повторные вызовы безвредны).
// Ошибки игнорируются: на ядрах < 3.4 опция недоступна (EINVAL) — в этом
// случае поведение без subreaper сохраняется (более медленный reap через init).
// RawSyscall используется намеренно: prctl быстр и не блокирует поток.
func setSelfSubreaper() {
	//nolint:errcheck
	syscall.RawSyscall(syscall.SYS_PRCTL, uintptr(prSetChildSubreaper), 1, 0)
}

// reapGroupOrphans reap-ает оставшихся зомби-потомков процессной группы pgid.
// Должна вызываться после cmd.Wait() (который уже reap-нул головной процесс sh).
//
// Поскольку setSelfSubreaper() сделал нас sub-reaper до cmd.Start(), orphan-ы
// (потомки sh, убитые SIGKILL, но ещё не reap-нутые) усыновлены нашим процессом.
// waitpid(-pgid, WNOHANG) reap-ает их без блокировки.
//
// После reap pidfd_open(pid) для данного PID вернёт ESRCH — тест AC6/SR-47 пройдёт.
func reapGroupOrphans(pgid int) {
	deadline := time.Now().Add(reapGroupTimeout)
	for time.Now().Before(deadline) {
		var ws syscall.WaitStatus
		pid, err := syscall.Wait4(-pgid, &ws, syscall.WNOHANG, nil)
		if pid > 0 {
			// Reap-нули ещё одного потомка — продолжаем
			continue
		}
		if pid == 0 {
			// Потомки живы, но ещё не умерли — ждём и повторяем
			time.Sleep(reapGroupPollInterval)
			continue
		}
		// pid < 0: ECHILD (больше нет потомков) или другая ошибка → выходим.
		_ = err
		return
	}
}
