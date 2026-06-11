//go:build unix

package cmdexec

// sysproc_unix.go — платформозависимая логика управления группами процессов (Linux+darwin).
//
// AC6/SR-47/ADR-001: Setpgid:true + Cancel→killGroup + WaitDelay.
// Windows вне scope (baseline §6, CLAUDE.md).
//
// БЕЗОПАСНОСТЬ (SR-54): SysProcAttr.Credential НЕ задаётся — дочерний процесс
// наследует uid/gid демона как есть (нет setuid/повышения привилегий).

import (
	"os/exec"
	"syscall"
	"time"
)

// applyProcessGroup настраивает cmd для работы в новой группе процессов (Setpgid:true).
// Должна вызываться ДО cmd.Start().
// На Linux также устанавливает текущий процесс как sub-reaper (см. sysproc_linux.go),
// чтобы orphan-потомки группы усыновлялись нами — это гарантирует reap зомби SR-47.
// Credential НЕ устанавливается (AC9/SR-54: наследование uid/gid).
func applyProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		// Credential намеренно не задаётся — наследование uid/gid демона (SR-54).
	}
	// setSelfSubreaper — платформо-специфичная функция:
	//   Linux (sysproc_linux.go): prctl(PR_SET_CHILD_SUBREAPER, 1)
	//   Другие Unix (sysproc_unix_other.go): no-op
	setSelfSubreaper()
}

// killGroup отправляет SIGKILL всей группе процессов (kill -pgid).
// pgid — PID головного процесса (= PGID при Setpgid:true).
// Уничтожает головной процесс И всех потомков одним сигналом.
func killGroup(pgid int) error {
	return syscall.Kill(-pgid, syscall.SIGKILL)
}

// waitDelay — страховка от зависших пайпов/потомков после Cancel.
// ADR-001: ненулевой WaitDelay гарантирует завершение cmd.Wait() даже если
// потомки удерживают ссылки на пайпы.
const waitDelay = 5 * time.Second
