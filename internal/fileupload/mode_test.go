package fileupload_test

// mode_test.go — юнит-тесты для ParseMode (ADR-003 / SR-73 / AC9/AC14).
// Без I/O. Запускаются в Docker -mod=vendor.

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/fileupload"
)

// TestParseMode_ValidModes: допустимые режимы парсируются корректно (AC9/SR-73).
func TestParseMode_ValidModes(t *testing.T) {
	cases := []struct {
		input string
		want  fs.FileMode
	}{
		{"0600", 0o600},
		{"0700", 0o700},
		{"0644", 0o644},
		{"0755", 0o755},
		{"0400", 0o400},
		{"0000", 0o000},
		// 0777 содержит world-writable (0002) → НЕ в valid.
		// group-writable (не world-writable) — разрешён.
		{"0660", 0o660},
		{"0640", 0o640},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := fileupload.ParseMode(tc.input)
			if err != nil {
				t.Errorf("ParseMode(%q): unexpected error: %v", tc.input, err)
				return
			}
			if got != tc.want {
				t.Errorf("ParseMode(%q) = %04o, want %04o", tc.input, got, tc.want)
			}
		})
	}
}

// TestParseMode_SetuidBit: mode с setuid-битом → ErrBadMode (ADR-003/SR-73).
func TestParseMode_SetuidBit(t *testing.T) {
	modes := []string{"04755", "04000", "04600"}
	for _, m := range modes {
		t.Run(m, func(t *testing.T) {
			_, err := fileupload.ParseMode(m)
			if err == nil {
				t.Errorf("ParseMode(%q): want ErrBadMode, got nil", m)
				return
			}
			if !errors.Is(err, fileupload.ErrBadMode) {
				t.Errorf("ParseMode(%q): want ErrBadMode, got %v", m, err)
			}
		})
	}
}

// TestParseMode_SetgidBit: mode с setgid-битом → ErrBadMode (ADR-003/SR-73).
func TestParseMode_SetgidBit(t *testing.T) {
	modes := []string{"02755", "02000", "02644"}
	for _, m := range modes {
		t.Run(m, func(t *testing.T) {
			_, err := fileupload.ParseMode(m)
			if err == nil {
				t.Errorf("ParseMode(%q): want ErrBadMode, got nil", m)
				return
			}
			if !errors.Is(err, fileupload.ErrBadMode) {
				t.Errorf("ParseMode(%q): want ErrBadMode, got %v", m, err)
			}
		})
	}
}

// TestParseMode_StickyBit: mode с sticky-битом → ErrBadMode (ADR-003/SR-73).
func TestParseMode_StickyBit(t *testing.T) {
	modes := []string{"01755", "01000", "01644"}
	for _, m := range modes {
		t.Run(m, func(t *testing.T) {
			_, err := fileupload.ParseMode(m)
			if err == nil {
				t.Errorf("ParseMode(%q): want ErrBadMode, got nil", m)
				return
			}
			if !errors.Is(err, fileupload.ErrBadMode) {
				t.Errorf("ParseMode(%q): want ErrBadMode, got %v", m, err)
			}
		})
	}
}

// TestParseMode_WorldWritable: mode с world-writable битом → ErrBadMode (ADR-003/SR-73).
func TestParseMode_WorldWritable(t *testing.T) {
	modes := []string{"0777", "0666", "0002", "0622", "0646"}
	// ВАЖНО: 0777 содержит world-writable (0002) → ErrBadMode (ADR-003 §3).
	for _, m := range modes {
		t.Run(m, func(t *testing.T) {
			_, err := fileupload.ParseMode(m)
			if err == nil {
				t.Errorf("ParseMode(%q): want ErrBadMode for world-writable, got nil", m)
				return
			}
			if !errors.Is(err, fileupload.ErrBadMode) {
				t.Errorf("ParseMode(%q): want ErrBadMode, got %v", m, err)
			}
		})
	}
}

// TestParseMode_InvalidString: непарсимые строки → ErrBadMode (ADR-003/SR-73).
func TestParseMode_InvalidString(t *testing.T) {
	invalids := []string{"", "abc", "0x600", "rwxr-xr-x", "999", "-1", "08000"}
	for _, m := range invalids {
		t.Run(m, func(t *testing.T) {
			_, err := fileupload.ParseMode(m)
			if err == nil {
				t.Errorf("ParseMode(%q): want error, got nil", m)
				return
			}
			if !errors.Is(err, fileupload.ErrBadMode) {
				t.Errorf("ParseMode(%q): want ErrBadMode, got %v", m, err)
			}
		})
	}
}

// TestParseMode_BitsOutside0777: любой бит вне маски 0777 → ErrBadMode (ADR-003 §2, F-1).
// До исправления F-1 режимы "010000" и "017777" ложно проходили валидацию.
func TestParseMode_BitsOutside0777(t *testing.T) {
	// Запрещены: биты вне 0777, не входящие в 0o7000 (например, 010000).
	forbidden := []string{
		"010000", // бит 010000 вне 0777 и вне 0o7000 — ранее пропускался
		"017777", // 010000 | 07777 — старший бит + все биты ниже
		// Убеждаемся, что setuid/setgid/sticky по-прежнему отвергаются новой проверкой:
		"04000", // setuid
		"02000", // setgid
		"01000", // sticky
	}
	for _, m := range forbidden {
		t.Run(m, func(t *testing.T) {
			_, err := fileupload.ParseMode(m)
			if err == nil {
				t.Errorf("ParseMode(%q): want ErrBadMode (bits outside 0777), got nil", m)
				return
			}
			if !errors.Is(err, fileupload.ErrBadMode) {
				t.Errorf("ParseMode(%q): want ErrBadMode, got %v", m, err)
			}
		})
	}
}

// TestParseMode_WorldWritableStillRejected: мировые разрешения записи внутри 0777 по-прежнему
// отвергаются отдельной проверкой 0002 (ADR-003 §3), новая маска их не захватывает.
func TestParseMode_WorldWritableStillRejected(t *testing.T) {
	ww := []string{"0666", "0002", "0777"}
	for _, m := range ww {
		t.Run(m, func(t *testing.T) {
			_, err := fileupload.ParseMode(m)
			if err == nil {
				t.Errorf("ParseMode(%q): want ErrBadMode (world-writable), got nil", m)
				return
			}
			if !errors.Is(err, fileupload.ErrBadMode) {
				t.Errorf("ParseMode(%q): want ErrBadMode, got %v", m, err)
			}
		})
	}
}

// TestParseMode_LegitimateModesStillPass: легитимные режимы 0600/0644/0700/0755/0777-нет
// по-прежнему принимаются после изменения маски F-1.
func TestParseMode_LegitimateModesStillPass(t *testing.T) {
	cases := []struct {
		input string
		want  fs.FileMode
	}{
		{"0600", 0o600},
		{"0644", 0o644},
		{"0700", 0o700},
		{"0755", 0o755},
		{"0400", 0o400},
		{"0660", 0o660},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := fileupload.ParseMode(tc.input)
			if err != nil {
				t.Errorf("ParseMode(%q): unexpected error after F-1 fix: %v", tc.input, err)
				return
			}
			if got != tc.want {
				t.Errorf("ParseMode(%q) = %04o, want %04o", tc.input, got, tc.want)
			}
		})
	}
}
