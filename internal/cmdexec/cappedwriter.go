package cmdexec

// cappedwriter.go — io.Writer с потолком N байт и флагом Truncated.
//
// AC11/SR-53: лимит вывода stdout/stderr с OOM-защитой.
// Дизайн: пишет до лимита, остаток ДРЕНИРУЕТ без ошибки Write.
// Это критично: если Write вернёт ошибку после заполнения буфера,
// os/exec подвесит процесс (пайп заблокируется); дренаж предотвращает deadlock.

// CappedWriter — io.Writer, принимающий не более Cap байт.
// Bytes содержит захваченные данные (≤ Cap).
// Truncated = true, если было получено больше Cap байт.
type CappedWriter struct {
	buf       []byte
	cap       int
	Truncated bool
}

// NewCappedWriter создаёт CappedWriter с потолком cap байт.
func NewCappedWriter(cap int) *CappedWriter {
	return &CappedWriter{
		buf: make([]byte, 0, cap),
		cap: cap,
	}
}

// Write реализует io.Writer.
// Записывает данные до потолка cap; лишнее ДРЕНИРУЕТ (отбрасывает) без ошибки.
// Всегда возвращает len(p), nil — гарантирует, что пайп не заблокируется.
func (cw *CappedWriter) Write(p []byte) (int, error) {
	n := len(p)
	if cw.cap == 0 {
		// Нулевой лимит: всё дренируется.
		if n > 0 {
			cw.Truncated = true
		}
		return n, nil
	}

	remaining := cw.cap - len(cw.buf)
	if remaining <= 0 {
		// Буфер уже заполнен.
		if n > 0 {
			cw.Truncated = true
		}
		return n, nil
	}

	if len(p) > remaining {
		// Часть пишем, остаток дренируем.
		cw.buf = append(cw.buf, p[:remaining]...)
		cw.Truncated = true
	} else {
		cw.buf = append(cw.buf, p...)
	}
	return n, nil
}

// Bytes возвращает накопленные данные (≤ Cap байт).
func (cw *CappedWriter) Bytes() []byte {
	return cw.buf
}
