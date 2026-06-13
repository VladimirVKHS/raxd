# Reviewer — upload-quota

**Verdict: accept**

Фича реализована строго по plan.md, закрывает все 12 AC и SR-90…SR-102. Код корректен, безопасность не ослаблена, регрессов в `upload_file` нет.

Подтверждено: deny ДО фиксации без следов (MkdirAll после квоты, `TestQuota_DenyBeforeMkdirAll`); TOCTOU под мьютексом из `sync.Map` (Lock до OpenRoot, один `*os.Root` на критсекцию, Config value-тип чист для go vet); обход только regular через `root.FS()` (SR-93); fail-closed (`TestQuota_FailClosedOnWalkError` через безопасный тест-хук); нейтральный deny-аудит; граница `>` (ровно лимит разрешён); overwrite-дельта без двойного учёта; независимость от max_file_bytes; дефолт 0 = нулевая цена. Тест-хук `currentBytesHook` приемлем (unexported, nil в prod, тест-сборка only).

**Находка I-1 (minor, НЕ блокирует merge):** нет MCP-handler-теста именно на quota-deny audit-ветку (`upload_tool.go` case ErrQuotaExceeded). Покрыто косвенно (тип ошибки в quota_test.go; структурно идентичные deny-ветки покрыты `TestUploadFile_AuditDeny`/`ExactlyOneAuditRecord`). Рекомендация: добавить quota-deny audit-тест при следующем касании.

**Готовность к merge: да.**
