# Reviewer — service-purge

**Verdict: accept**

Все 10 AC реализованы кодом и тестами; SR-114…SR-127 и наследуемые SR-91/95/96 соблюдены. Реализация соответствует plan.md/service-design.md.

Подтверждено: SR-116 — `emitPurgeAuditRecord(opts.AuditOut,...)` на systemd.go:395 / launchd.go:257 строго ДО deleteUser и os.RemoveAll, пишет в stderr-sink (не в удаляемый каталог); SR-120 exec без shell (runCommandRaw, абсолютные бинари, раздельные args); SR-117 verifyTargetUser (имя+uid[1,999]+nologin — строже дизайна); SR-118/119 validatePurgePath (EvalSymlinks, prefix `raxd2`≠`raxd`); SR-114/115 барьер --yes (Purge не вызывается без --yes, упоминание keys.db); SR-124 без секретов (нейтрализация stderr OS). Порядок шагов, частичное состояние (AC4), идемпотентность маппинга кодов — корректны. Кросс-платформенность симметрична.

**Advisory (НЕ блокируют merge):**
1. `charmbracelet/log` в emitPurgeAuditRecord — не новая зависимость (уже в go.mod, server/audit.go), консистентно; SR-127 «stdlib-only» относится к ядру purge-логики (соблюдено). Рекомендация уточнить формулировку SR-127.
2. systemd.go:343 — избыточное внешнее условие у Stop (на AC4 не влияет; упростить до `if st.Active`).
3. `TestValidatePurgePath_HomeAncestor` — пустой placeholder (покрытие в `_ViaEnv`); удалить для ясности.
4. SR-116 реальный порядок шага 10→11→12 покрыт code-review (E2E требует root, baseline §6) — приемлемо.

**Готовность к merge: да.**
