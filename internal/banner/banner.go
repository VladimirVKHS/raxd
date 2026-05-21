// Package banner renders the raxd product banner (plain-text, no external styling).
// API is stable: when lipgloss is added in cli-ux task, only this package changes.
package banner

import (
	"fmt"
	"strings"

	"github.com/vladimirvkhs/raxd/internal/version"
)

// Render returns the multi-line banner as a plain-text string.
// The caller is responsible for writing it to stderr.
//
// Wide layout (>= 52 columns): Unicode box with full build info line.
// Narrow layout (42-51): shortened commit (7 chars, no "commit" prefix).
// Very narrow (< 42): no box, three plain lines.
//
// NOTE: on bootstrap-cli stage we do not detect terminal width; we always
// render the wide layout. Adaptive sizing is a cli-ux extension point.
func Render() string {
	line1 := "raxd  —  Remote Access Daemon" // em-dash
	line2 := fmt.Sprintf("%s  ·  commit %s  ·  built %s",
		version.Version, version.Commit, version.Date)
	line3 := "Vladimir Kovalev, OEM TECH"

	// Find the longest line to size the box.
	maxLen := max3(len(line1), len(line2), len(line3))
	width := maxLen + 4 // 2 spaces padding each side

	top := "┌" + strings.Repeat("─", width) + "┐"
	bot := "└" + strings.Repeat("─", width) + "┘"

	pad := func(s string) string {
		return "│  " + s + strings.Repeat(" ", width-2-len(s)) + "  │"
	}

	return strings.Join([]string{top, pad(line1), pad(line2), pad(line3), bot}, "\n")
}

func max3(a, b, c int) int {
	m := a
	if b > m {
		m = b
	}
	if c > m {
		m = c
	}
	return m
}
