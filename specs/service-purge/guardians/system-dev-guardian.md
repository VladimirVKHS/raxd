# system-dev-guardian — service-purge

**Verdict: pass**

Артефакт: `specs/service-purge/service-design.md` (кодовых правок нет — только документ, что корректно для этого шага).

Подтверждено: exec БЕЗ shell (SR-120) — все вызовы `runCommandRaw(ctx, bin, args...)` с позиционными аргументами, фиксированные абсолютные пути бинарей, без `sh -c`/конкатенации; verifyTargetUser (имя + nologin-shell) ПЕРЕД удалением, несоответствие→ErrUserMismatch (SR-117/AC6); validatePurgePath 8 проверок с EvalSymlinks внутри раскладки, защита от `/var/lib/raxd2`≠`/var/lib/raxd` (SR-118/119/AC7); маппинг exit-кодов userdel (6=absent идемпотентно, 1/10=ErrPermission) и dscl (pattern-matching stderr); порядок 15 шагов с аудитом ДО физического удаления (SR-116/AC8); привилегии через Geteuid без эскалации (SR-121); кросс-платформенность без build-тегов (тест через fakeManager); stdlib-only (SR-127); Docker (§6).

Advisory для developer (не блокеры):
1. Поле `Uninstalled bool` в `PurgeReport` — расширение относительно plan.md §Contracts; задокументировать как осознанное.
2. Разграничить: на шаге 10 эмитируется ПРЕДВАРИТЕЛЬНАЯ аудит-запись (намерение: user_present/dirs_present); итоговый `PurgeReport` (UserRemoved/DirsRemoved) формируется после шагов 11–14.
3. Асимметрия verifyTargetUser (Linux: имя+uid-диапазон+shell; macOS: только shell) — допустима, но developer может уточнить.
