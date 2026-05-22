package cmdexec_test

// cappedwriter_test.go — TDD-тесты для cappedWriter.
//
// SR-53: лимит вывода с флагом truncated; дренаж остатка без ошибок.

import (
	"strings"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/cmdexec"
)

// TestCappedWriterUnderLimit — запись меньше лимита: данные целые, Truncated=false.
func TestCappedWriterUnderLimit(t *testing.T) {
	cw := cmdexec.NewCappedWriter(100)
	n, err := cw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 5 {
		t.Errorf("n = %d, want 5", n)
	}
	if cw.Truncated {
		t.Errorf("Truncated = true, want false")
	}
	if string(cw.Bytes()) != "hello" {
		t.Errorf("Bytes = %q, want 'hello'", cw.Bytes())
	}
}

// TestCappedWriterExactLimit — запись ровно по лимиту: не усечено.
func TestCappedWriterExactLimit(t *testing.T) {
	cw := cmdexec.NewCappedWriter(5)
	_, err := cw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if cw.Truncated {
		t.Errorf("Truncated = true for exact limit, want false")
	}
}

// TestCappedWriterOverLimit — запись сверх лимита: данные обрезаны, Truncated=true.
func TestCappedWriterOverLimit(t *testing.T) {
	cw := cmdexec.NewCappedWriter(5)
	input := []byte("hello world")
	n, err := cw.Write(input)
	// Write должен вернуть len(input) (не ошибку) — дренаж остатка.
	if err != nil {
		t.Fatalf("Write must not return error (drains excess): %v", err)
	}
	if n != len(input) {
		t.Errorf("n = %d, want %d (full input length — excess drained)", n, len(input))
	}
	if !cw.Truncated {
		t.Errorf("Truncated = false, want true")
	}
	got := string(cw.Bytes())
	if got != "hello" {
		t.Errorf("Bytes = %q, want 'hello'", got)
	}
}

// TestCappedWriterMultipleWrites — несколько вызовов Write до и после лимита.
func TestCappedWriterMultipleWrites(t *testing.T) {
	cw := cmdexec.NewCappedWriter(10)
	_, _ = cw.Write([]byte("hello"))   // 5 байт
	_, _ = cw.Write([]byte(" world!")) // ещё 7 байт → итого 12, лимит 10

	if !cw.Truncated {
		t.Errorf("Truncated = false after exceeding limit")
	}
	got := string(cw.Bytes())
	if got != "hello worl" {
		t.Errorf("Bytes = %q, want 'hello worl'", got)
	}
}

// TestCappedWriterWriteAfterFull — Write после заполнения лимита дренирует без ошибки.
func TestCappedWriterWriteAfterFull(t *testing.T) {
	cw := cmdexec.NewCappedWriter(5)
	_, _ = cw.Write([]byte("hello"))   // ровно лимит
	n, err := cw.Write([]byte("more")) // уже полный
	if err != nil {
		t.Fatalf("Write after full must not error: %v", err)
	}
	if n != 4 {
		t.Errorf("n = %d, want 4 (drained)", n)
	}
	if !cw.Truncated {
		t.Errorf("Truncated must be true after overflow")
	}
	if strings.Contains(string(cw.Bytes()), "more") {
		t.Errorf("'more' must not appear in capped bytes")
	}
}

// TestCappedWriterZeroLimit — нулевой лимит: всё дренируется, Truncated=true.
func TestCappedWriterZeroLimit(t *testing.T) {
	cw := cmdexec.NewCappedWriter(0)
	n, err := cw.Write([]byte("anything"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len("anything") {
		t.Errorf("n = %d, want %d", n, len("anything"))
	}
	if !cw.Truncated {
		t.Errorf("Truncated = false for zero-limit writer, want true")
	}
	if len(cw.Bytes()) != 0 {
		t.Errorf("Bytes not empty for zero-limit writer: %q", cw.Bytes())
	}
}
