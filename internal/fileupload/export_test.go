package fileupload

// export_test.go — экспорт internals для white-box тестирования.
//
// Файл компилируется ТОЛЬКО в тестовых сборках (суффикс _test.go).
// В production-бинаре отсутствует.
//
// SetCurrentBytesHook позволяет тестам (package fileupload_test) подменить
// реальный WalkDir-обход детерминированной заглушкой, инжектируя ошибку
// независимо от uid процесса (SR-96/Q4).
//
// Использование в тесте:
//
//	fileupload.SetCurrentBytesHook(func(*os.Root) (int64, error) {
//	    return 0, errors.New("injected walk error")
//	})
//	defer fileupload.SetCurrentBytesHook(nil) // сброс после теста
import "os"

// SetCurrentBytesHook задаёт (или сбрасывает при nil) тестовый хук для currentBytes.
// Вызывать только в тестах; сбрасывать через defer после каждого теста.
func SetCurrentBytesHook(fn func(*os.Root) (int64, error)) {
	currentBytesHook = fn
}
