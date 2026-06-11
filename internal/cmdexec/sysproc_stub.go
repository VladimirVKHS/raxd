//go:build !unix

package cmdexec

import "os/exec"

// applyProcessGroup — заглушка для не-unix платформ (Windows вне scope).
func applyProcessGroup(_ *exec.Cmd) {}

// killGroup — заглушка для не-unix платформ.
func killGroup(_ int) error { return nil }

// reapGroupOrphans — заглушка для не-unix платформ.
func reapGroupOrphans(_ int) {}

const waitDelay = 0
