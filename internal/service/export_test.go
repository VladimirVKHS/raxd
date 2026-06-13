// Package service — export_test.go: exports internal helpers for black-box unit tests.
//
// This file compiles only for "go test" (package service_test), not in production.
// It re-exports private functions under test-friendly names so purge_test.go can
// exercise platform-specific logic without calling real userdel/dscl.
//
// SR-126: testability without real system commands on the host.
package service

import "io"

// ValidatePurgePath exposes validatePurgePath for purge_test.go.
func ValidatePurgePath(path string, allowedRoots []string) error {
	return validatePurgePath(path, allowedRoots)
}

// ParsePasswdLineForTest exposes parsePasswdLine for purge_test.go (Linux verifyTargetUser).
// Parses one line from getent passwd output and checks it against expectedName.
func ParsePasswdLineForTest(line, expectedName string) (present bool, err error) {
	return parsePasswdLine(line, expectedName)
}

// ParseDsclShellOutputForTest exposes parseDsclShellOutput for purge_test.go (macOS verifyTargetUser).
// Parses dscl . -read /Users/<name> UserShell output.
func ParseDsclShellOutputForTest(output, expectedName string) (present bool, err error) {
	return parseDsclShellOutput(output, expectedName)
}

// MapDsclDeleteErrorForTest exposes mapDsclDeleteError for purge_test.go.
func MapDsclDeleteErrorForTest(stderr string) error {
	return mapDsclDeleteError(stderr)
}

// MapUserdelExitCodeForTest exposes mapUserdelExitCode for purge_test.go.
func MapUserdelExitCodeForTest(exitCode int) error {
	return mapUserdelExitCode(exitCode)
}

// IsEqualOrAncestorForTest exposes isEqualOrAncestor for purge_test.go.
// Allows deterministic testing of the HOME-ancestor guard without os.UserHomeDir().
func IsEqualOrAncestorForTest(candidate, base string) bool {
	return isEqualOrAncestor(candidate, base)
}

// EmitPurgeAuditRecordForTest exposes emitPurgeAuditRecord for purge_test.go.
// Allows testing the audit record is written BEFORE RemoveAll (SR-116, AC8).
func EmitPurgeAuditRecordForTest(w io.Writer, platform string, userPresent bool, dirsPresent []string) {
	emitPurgeAuditRecord(w, platform, userPresent, dirsPresent)
}
