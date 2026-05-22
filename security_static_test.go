package raxd_test

// security_static_test.go — static source-code security invariant tests.
//
// These tests scan the source files of the bootstrap-cli skeleton to verify
// the absence of forbidden patterns. They complement behavioural tests and
// satisfy security-requirements.md "проверяемо: grep по internal/".
//
// Running: go test -v -run TestStatic ./
// Docker:  docker run --rm -v "$PWD":/src -w /src golang:1.25 sh -c "CGO_ENABLED=0 go test -v -count=1 ./"

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goSourceFiles returns all *.go (non-test) files under the given directory.
func goSourceFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %q: %v", root, err)
	}
	return files
}

// grepFiles searches all files for any line containing the given pattern.
// Returns a list of "file:linenum: line" strings for each match.
func grepFiles(t *testing.T, files []string, pattern string) []string {
	t.Helper()
	var matches []string
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			t.Fatalf("open %q: %v", f, err)
		}
		scanner := bufio.NewScanner(fh)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			// Skip comment lines — they may describe the absence of a pattern.
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if strings.Contains(line, pattern) {
				matches = append(matches, filepath.Base(f)+":"+strings.TrimSpace(line))
			}
		}
		fh.Close()
	}
	return matches
}

// TestStaticNoExecCommand verifies that no production source file in internal/
// or cmd/ imports or calls exec.Command / os/exec — EXCEPT internal/cmdexec,
// which is the single authorized package for subprocess execution (command-exec task).
//
// Security requirement: "ни одна команда/заглушка каркаса не вызывает exec.Command/sh -c/os/exec"
// baseline §3. Единственное исключение: internal/cmdexec (spec.md command-exec AC2).
func TestStaticNoExecCommand(t *testing.T) {
	// Collect all production (non-test) sources EXCEPT the authorized cmdexec package.
	var files []string
	for _, f := range append(goSourceFiles(t, "internal"), goSourceFiles(t, "cmd")...) {
		// internal/cmdexec is the single authorized package for os/exec usage.
		if strings.Contains(filepath.ToSlash(f), "internal/cmdexec") {
			continue
		}
		files = append(files, f)
	}

	forbiddenPatterns := []string{
		`"os/exec"`,
		`exec.Command`,
		`exec.LookPath`,
	}

	for _, pat := range forbiddenPatterns {
		if matches := grepFiles(t, files, pat); len(matches) > 0 {
			t.Errorf("SECURITY: forbidden pattern %q found in production sources outside internal/cmdexec:\n  %s",
				pat, strings.Join(matches, "\n  "))
		}
	}
}

// TestStaticNoNetListen verifies that network listener calls are only in the
// server transport package (internal/server), not in CLI or cmd code.
//
// Before tls-transport: serve was an honest stub — no net.Listen anywhere.
// After tls-transport: internal/server legitimately uses net.Listen (it IS
// the transport layer). CLI/cmd must still not open listeners directly.
//
// Security requirement: "CLI-заглушки и cmd не открывают сетевые порты напрямую;
// сетевой транспорт инкапсулирован в internal/server"
// baseline §3/§6.
func TestStaticNoNetListen(t *testing.T) {
	// internal/server IS the transport — net.Listen is allowed there.
	// All other packages (cli, cmd, config, keystore, banner, version) must not
	// open listeners directly.
	var files []string
	for _, dir := range []string{"internal/cli", "internal/config", "internal/keystore",
		"internal/banner", "internal/version", "cmd"} {
		files = append(files, goSourceFiles(t, dir)...)
	}

	forbiddenPatterns := []string{
		`net.Listen`,
		`http.ListenAndServe`,
		`http.ListenAndServeTLS`,
		`tls.Listen`,
	}

	for _, pat := range forbiddenPatterns {
		if matches := grepFiles(t, files, pat); len(matches) > 0 {
			t.Errorf("SECURITY: forbidden networking pattern %q found outside internal/server:\n  %s",
				pat, strings.Join(matches, "\n  "))
		}
	}
}

// TestStaticNoHardcodedSecrets verifies that no production source file contains
// known secret patterns (API key prefixes, PEM headers, base64 token blobs).
// Security requirement: "в исходниках нет хардкода ключей/токенов/сертификатов"
// baseline §4, AC "скелет не содержит секретов".
func TestStaticNoHardcodedSecrets(t *testing.T) {
	files := append(
		goSourceFiles(t, "internal"),
		goSourceFiles(t, "cmd")...,
	)

	forbiddenPatterns := []string{
		`rax_live_`,
		`BEGIN PRIVATE KEY`,
		`BEGIN RSA PRIVATE KEY`,
		`BEGIN EC PRIVATE KEY`,
		`BEGIN CERTIFICATE`,
	}

	for _, pat := range forbiddenPatterns {
		if matches := grepFiles(t, files, pat); len(matches) > 0 {
			t.Errorf("SECURITY: hardcoded secret pattern %q found:\n  %s",
				pat, strings.Join(matches, "\n  "))
		}
	}
}

// isWideModeFsCall reports whether a source line contains both a wide mode literal
// (0644/0o644/0755/0o755/0666/0o666/0777/0o777) and a file-creation or permission
// API call (WriteFile, OpenFile, Mkdir, MkdirAll, Chmod, Create).
//
// Rationale: mode literals also appear legitimately in validation masks such as
//   if mode &^ fs.FileMode(0o777) != 0 { … }
// — this is mode validation, not file creation, and must NOT be flagged.
// Only lines that pass a wide mode directly to an fs-creation/chmod call are violations.
//
// Self-check (see TestStaticNoFileCreationWithWideModes sub-tests):
//   os.WriteFile(p, d, 0o644)   → violation (WriteFile + wide mode on same line)
//   if mode &^ fs.FileMode(0o777) != 0 {  → NOT a violation (no perms-sink on line)
func isWideModeFsCall(line string) bool {
	wideModes := []string{
		"0644", "0o644",
		"0755", "0o755",
		"0666", "0o666",
		"0777", "0o777",
	}
	permSinks := []string{
		"WriteFile",
		"OpenFile",
		"Mkdir",
		"MkdirAll",
		"Chmod",
		"Create",
	}

	hasWideMode := false
	for _, m := range wideModes {
		if strings.Contains(line, m) {
			hasWideMode = true
			break
		}
	}
	if !hasWideMode {
		return false
	}

	for _, sink := range permSinks {
		if strings.Contains(line, sink) {
			return true
		}
	}
	return false
}

// grepFilesWideMode searches files for lines that contain a wide mode literal
// AND a file-creation/permission API call on the same line.
// Returns "file:line" strings for each violation.
func grepFilesWideMode(t *testing.T, files []string) []string {
	t.Helper()
	var matches []string
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			t.Fatalf("open %q: %v", f, err)
		}
		scanner := bufio.NewScanner(fh)
		for scanner.Scan() {
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if isWideModeFsCall(line) {
				matches = append(matches, filepath.Base(f)+":"+trimmed)
			}
		}
		fh.Close()
	}
	return matches
}

// TestStaticNoFileCreationWithWideModes verifies that the config package does not
// create or chmod files with modes wider than 0600 (the security contract for
// keys.db / TLS key files: baseline §1/§2, SR-73/ADR-003).
//
// A violation is a source line that contains BOTH:
//   (a) a wide mode literal — 0644, 0o644, 0755, 0o755, 0666, 0o666, 0777, 0o777
//   (b) a file-creation or permission call — WriteFile, OpenFile, Mkdir, MkdirAll,
//       Chmod, Create
//
// Mode validation masks such as `if mode &^ fs.FileMode(0o777) != 0` are intentionally
// excluded: they validate the caller-supplied mode rather than creating a file, so they
// do not weaken the security guarantee this test enforces.
//
// The test includes a self-check (sub-tests "matcher_*") that verifies the matcher
// itself is non-trivial: a genuine violation string IS flagged and a validation mask
// string is NOT flagged.  This prevents a future "fix" from emptying the matcher.
func TestStaticNoFileCreationWithWideModes(t *testing.T) {
	// --- Self-check: verify the matcher is non-trivial ---
	t.Run("matcher_violation_detected", func(t *testing.T) {
		// A real violation: wide mode passed to WriteFile on the same line.
		violationLine := `os.WriteFile(p, data, 0o644)`
		if !isWideModeFsCall(violationLine) {
			t.Errorf("matcher self-check FAILED: %q should be detected as a violation (WriteFile + 0o644), but was not", violationLine)
		}
	})

	t.Run("matcher_validation_mask_excluded", func(t *testing.T) {
		// A legitimate validation mask (introduced by F-1 fix in parseModeStr / ParseMode).
		// Must NOT be flagged — it validates a mode, it does not create a file.
		maskLine := `if mode&^fs.FileMode(0o777) != 0 {`
		if isWideModeFsCall(maskLine) {
			t.Errorf("matcher self-check FAILED: %q should NOT be detected as a violation (validation mask, no perms-sink), but was flagged", maskLine)
		}
	})

	// --- Real check: scan internal/config non-test sources ---
	files := goSourceFiles(t, "internal/config")
	if matches := grepFilesWideMode(t, files); len(matches) > 0 {
		t.Errorf("SECURITY: wide file mode used in file-creation/chmod call in config sources:\n  %s",
			strings.Join(matches, "\n  "))
	}
}

// TestStaticNoMathRand verifies that neither internal/keystore nor internal/cli/key.go
// import or reference math/rand or math/rand/v2.
// SR-2: "math/rand отсутствует во всей key-логике — ни тело, ни salt, ни id не используют math/rand".
// Scope: only key-management source files (not the whole project).
func TestStaticNoMathRand(t *testing.T) {
	// Collect production (non-test) sources in the key-management scope.
	files := append(
		goSourceFiles(t, "internal/keystore"),
		"internal/cli/key.go",
	)

	forbiddenPatterns := []string{
		`"math/rand"`,
		`"math/rand/v2"`,
		`math/rand.`,
		`rand.Intn`,
		`rand.Int63`,
		`rand.Float`,
		`rand.New(`,
		`rand.NewSource`,
	}

	for _, pat := range forbiddenPatterns {
		if matches := grepFiles(t, files, pat); len(matches) > 0 {
			t.Errorf("SR-2 VIOLATION: math/rand pattern %q found in key-management sources (must use crypto/rand only):\n  %s",
				pat, strings.Join(matches, "\n  "))
		}
	}
}

// TestGoModuleNameAndGoVersion verifies that go.mod declares the correct
// module name and minimum Go version as specified in spec.md D1/D2.
// AC: "go.mod с именем модуля github.com/vladimirvkhs/raxd и минимальной версией go 1.25".
func TestGoModuleNameAndGoVersion(t *testing.T) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("cannot read go.mod: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "module github.com/vladimirvkhs/raxd") {
		t.Errorf("go.mod must declare module github.com/vladimirvkhs/raxd;\ngo.mod:\n%s", content)
	}
	if !strings.Contains(content, "go 1.25") {
		t.Errorf("go.mod must declare go 1.25 minimum version;\ngo.mod:\n%s", content)
	}
}
