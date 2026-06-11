//go:build unix && !linux

package cmdexec

// sysproc_unix_other.go — заглушки для unix-систем кроме Linux (darwin, *BSD).
//
// prctl(PR_SET_CHILD_SUBREAPER) — Linux-специфичный syscall; на других Unix
// системах недоступен. На macOS/BSD orphan-зомби reap-аются init быстро и
// pidfd_open не используется (Go применяет pid-based подход), поэтому проблема
// CI-специфична для Linux и эти заглушки безопасны.

// setSelfSubreaper — no-op на не-Linux Unix.
// На macOS/BSD prctl недоступен; Sub-reaper семантика не требуется.
func setSelfSubreaper() {}

// reapGroupOrphans — no-op на не-Linux Unix.
// На macOS/BSD pidfd не используется, зомби-проблема не воспроизводится.
func reapGroupOrphans(_ int) {}
