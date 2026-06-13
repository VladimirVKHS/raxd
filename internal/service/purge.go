// Package service — purge.go: platform-neutral orchestration helpers for Purge.
//
// Contains:
//   - validatePurgePath(path, allowedRoots) — path safety check (AC7, SR-118, SR-119)
//   - isEqualOrAncestor(candidate, base) — helper for home-ancestor detection
//
// Platform-specific Purge implementations are in systemd.go and launchd.go.
// No build tags: compiles on all platforms (service-design.md §9).
//
// SR-118: list of blocked system roots is enforced here.
// SR-119: filepath.EvalSymlinks applied before allowedRoots check.
// SR-120: no shell interpolation anywhere in this file.
// SR-127: stdlib only (os, path/filepath, strings).
package service

import (
	"os"
	"path/filepath"
	"strings"
)

// blockedSystemRoots is the set of paths that are unconditionally rejected by
// validatePurgePath regardless of allowedRoots (SR-118, service-design.md §3 check 6).
var blockedSystemRoots = map[string]bool{
	"/etc":      true,
	"/var":      true,
	"/usr":      true,
	"/usr/local": true,
	"/tmp":      true,
	"/bin":      true,
	"/sbin":     true,
	"/lib":      true,
	"/lib64":    true,
	"/boot":     true,
	"/dev":      true,
	"/proc":     true,
	"/sys":      true,
	"/run":      true,
}

// validatePurgePath checks that path is safe to pass to os.RemoveAll.
//
// Checks (service-design.md §3, in order):
//  1. Path is non-empty.
//  2. Path equals filepath.Clean(path) (no ".." segments).
//  3. Path is absolute.
//  4. Path != "/".
//  5. Path is not $HOME and not an ancestor of $HOME.
//  6. Path is not in blockedSystemRoots.
//  7. filepath.EvalSymlinks: if path does not exist → skip check 8 (idempotent AC3).
//     Other EvalSymlinks errors → ErrSuspiciousPath.
//  8. Resolved path has one of allowedRoots as a proper prefix
//     (with "/" suffix to avoid /var/lib/raxd2 matching /var/lib/raxd).
//
// Returns nil on success, ErrSuspiciousPath on any violation.
func validatePurgePath(path string, allowedRoots []string) error {
	// Check 1: non-empty.
	if path == "" {
		return wrapErr(ErrSuspiciousPath, "empty path")
	}

	// Check 2: clean (no ".." or redundant separators).
	if filepath.Clean(path) != path {
		return wrapErr(ErrSuspiciousPath, "path is not clean")
	}

	// Check 3: absolute.
	if !filepath.IsAbs(path) {
		return wrapErr(ErrSuspiciousPath, "path is not absolute")
	}

	// Check 4: not root "/".
	if path == "/" {
		return wrapErr(ErrSuspiciousPath, "root path rejected")
	}

	// Check 5: not $HOME and not its ancestor.
	homeDir, err := os.UserHomeDir()
	if err == nil {
		if isEqualOrAncestor(path, homeDir) {
			return wrapErr(ErrSuspiciousPath, "path is HOME or HOME ancestor")
		}
	}

	// Check 6: not a blocked system root.
	if blockedSystemRoots[path] {
		return wrapErr(ErrSuspiciousPath, "path is a blocked system root")
	}

	// Check 7: resolve symlinks.
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Already deleted — idempotent success (AC3, service-design.md §3 check 7).
			return nil
		}
		return wrapErr(ErrSuspiciousPath, "cannot resolve symlinks")
	}

	// Check 8: resolved path must be inside one of the allowed roots.
	// Use "resolved+/" vs "root+" to prevent /var/lib/raxd2 from matching /var/lib/raxd.
	for _, root := range allowedRoots {
		if strings.HasPrefix(resolved+"/", root+"/") {
			return nil
		}
		// Also accept exact match (resolved == root after symlink resolution).
		if resolved == root {
			return nil
		}
	}

	return wrapErr(ErrSuspiciousPath, "resolved path is outside expected layout")
}

// isEqualOrAncestor returns true if candidate == base OR if base is inside candidate
// (i.e., base starts with candidate+"/"). Used to reject $HOME and its ancestors.
//
// Example: candidate="/home/user", base="/home/user/foo" → true (candidate is ancestor of base).
// Example: candidate="/home", base="/home/user" → true.
// Example: candidate="/var/lib/raxd", base="/home/user" → false.
func isEqualOrAncestor(candidate, base string) bool {
	if candidate == base {
		return true
	}
	return strings.HasPrefix(base, candidate+"/")
}
