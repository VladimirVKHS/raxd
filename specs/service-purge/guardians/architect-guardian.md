# architect-guardian — service-purge

**Verdict: pass**

Артефакт: `specs/service-purge/plan.md`. Выбран ровно один подход: отдельный метод `Purge(ctx, opts) (PurgeReport, error)` в `ServiceManager` (НЕ параметризация `Uninstall` — защищает AC2 byte-for-byte); `internal/service/purge.go` (оркестрация + `validatePurgePath`); платформенные `Purge`/`verifyTargetUser` в systemd.go/launchd.go; CLI флаги `--purge`/`--yes` + барьер `ErrPurgeNotConfirmed`.

Все AC1–AC10 покрыты явными контрактами; тел функций нет; новых зависимостей нет; ссылки на реальный код (`runCommandRaw` SR-91, `Uninstall(ctx)`, `DefaultConfigForGOOS`, `StateDir`/`ConfigDir`, sentinel-стиль) корректны. `PurgeReport` обслуживает и идемпотентный вывод (AC3), и аудит-до-удаления (AC8). Граница делегирования чёткая: system-dev (команды userdel/dscl, парсинг, exit-коды), cli-ux (тексты).

**Advisory для security:** план ссылается на SR-95/SR-96 — роль `security` должна закрыть эти номера в `security-requirements.md` (exec без shell, нейтральные сообщения, stdlib-only). Мелкая рассинхронизация: `verifyTargetUser` не упомянут в секции Modules (не блокер).
