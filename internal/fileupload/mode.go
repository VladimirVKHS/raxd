package fileupload

// mode.go — ParseMode: парсинг восьмеричной строки mode + политика ADR-003.
//
// POLICY (ADR-003/SR-73):
//   - Допустимы ТОЛЬКО биты прав в маске 0777 (rwx user/group/other).
//   - Setuid (04000), setgid (02000), sticky (01000) — ЗАПРЕЩЕНЫ → ErrBadMode.
//   - World-writable (0002) — ЗАПРЕЩЁН → ErrBadMode.
//   - Непарсимые строки → ErrBadMode.
//
// Без I/O. Юнит-тестируем напрямую.

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
)

// ErrBadMode возвращается ParseMode при недопустимом значении mode.
// Используется uploadHandler: isError:true, Result:"deny".
// SECURITY (SR-73/ADR-003): запрет setuid/setgid/sticky/world-writable.
var ErrBadMode = errors.New("invalid or forbidden file mode")

// ParseMode парсит восьмеричную строку (напр. "0600") и валидирует по ADR-003.
// Пустая строка → ErrBadMode (handler подставляет DefaultMode ДО вызова).
// Непарсимая / с запрещёнными битами → ErrBadMode.
//
// Contract (plan.md §Contracts):
//
//	ParseMode(s string) (fs.FileMode, error)
func ParseMode(s string) (fs.FileMode, error) {
	if s == "" {
		return 0, fmt.Errorf("%w: empty mode string", ErrBadMode)
	}

	// Парс как восьмеричное (strconv.ParseInt с base=8).
	val, err := strconv.ParseInt(s, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("%w: cannot parse %q as octal: %v", ErrBadMode, s, err)
	}
	if val < 0 {
		return 0, fmt.Errorf("%w: negative mode %q", ErrBadMode, s)
	}

	mode := fs.FileMode(val)

	// ADR-003 §2: маска — только 0777.
	// Биты вне 0777 (setuid 04000, setgid 02000, sticky 01000) → ErrBadMode.
	const specialBits = fs.FileMode(0o7000) // setuid | setgid | sticky
	if mode&specialBits != 0 {
		return 0, fmt.Errorf("%w: mode %s contains forbidden special bits (setuid/setgid/sticky)", ErrBadMode, s)
	}

	// ADR-003 §3: world-writable (0002) → ErrBadMode.
	const worldWritable = fs.FileMode(0o002)
	if mode&worldWritable != 0 {
		return 0, fmt.Errorf("%w: mode %s is world-writable (bit 0002 forbidden)", ErrBadMode, s)
	}

	return mode, nil
}
