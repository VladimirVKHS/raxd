# developer-guardian — service-purge

**Verdict: pass** (повторная проверка после фикса 3 issue)

Коммиты фикса: 95a767c (SR-116 audit + uid<1000), 81f3265 (тесты + skip removed), bbc7a8d (AuditOut=stderr + CLI-тест), b3943ad (impl-notes).

Три issue закрыты:
- **Issue 1 / SR-116 (критичный):** `PurgeOptions.AuditOut io.Writer`; `emitPurgeAuditRecord` пишет предварительную запись (action/phase=pre-deletion/platform/user_present/dirs_present, без секретов — SR-124) ВНУТРИ Purge на шаге 10, ДО userdel (шаг 11) и os.RemoveAll (шаги 12–13) — порядок подтверждён в systemd.go:395 и launchd.go:257. CLI прокидывает stderr. Тесты `TestEmitPurgeAuditRecord_WritesBeforeRemoveAll`, `TestPurge_AuditSinkReceivedBeforeRemoveAll`. Сигнатура Purge не сломана.
- **Issue 2:** silent `t.Skip` для ancestor убран; остался только оправданный guard в `TestValidatePurgePath_HomeDir` (os.UserHomeDir error). Ancestor покрыт детерминированно (`_ViaEnv`, `TestIsEqualOrAncestor_*`).
- **Issue 3:** uid-проверка `[1,999]` в `parsePasswdLine` (имя+uid+nologin); тесты HighUID/UID0/ValidUID/NonNumeric.

Регрессий нет: exec без shell (SR-120), барьер --yes (SR-114/115), validatePurgePath (SR-118/119), идемпотентность (AC3), ErrPermission (AC5/SR-121), AC2 byte-for-byte. Без новых зависимостей в go.mod; scope только service-purge.

Advisory (не блокеры): (B) `charmbracelet/log` в emitPurgeAuditRecord — не новая зависимость (уже в go.mod, используется в server/audit.go; делает аудит консистентным с остальным raxd); (C) пустой placeholder `TestValidatePurgePath_HomeAncestor` — косметика, покрытие в `_ViaEnv`.
