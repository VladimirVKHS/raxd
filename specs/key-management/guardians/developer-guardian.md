# Guardian Report: developer-guardian — key-management

## Summary
Реализация `internal/keystore` + CLI `key.go` соответствует plan по модулям/сигнатурам/flock-режимам; §1 SECURITY-BASELINE соблюдён по коду (crypto/rand 32 байта, base64.RawURLEncoding+rax_live_, хранится только sha256(тело+salt)+salt, PlainKey не сериализуется, constant-time subtle.ConstantTimeCompare с перебором всех активных, atomic write temp 0600→fsync→rename→fsync каталога, corrupt→ErrCorrupt без перезаписи, flock корректен). Критических нарушений §1 нет (blocked не выставлен). Четыре пункта к исправлению.

## Issues
- [ ] ISSUE-1 (needs-changes): `runKeyDelete` использует `log.New(os.Stderr)` вместо `cmd.ErrOrStderr()`.
  - Где: `internal/cli/key.go:309` (в create — корректно через cmd.ErrOrStderr()).
  - Почему: ломает перехват в тестах (cmd.SetErr) и единый канал вывода (ux-spec). Не утечка секрета, но нарушение принципа.
  - Что делать: `log.New(cmd.ErrOrStderr())` в runKeyDelete.
- [ ] ISSUE-2 (needs-changes): аудит `key delete` без fingerprint — частичное нарушение SR-24/AC; в impl-notes оформлено как «отклонений нет» вместо эскалации.
  - Корень: при delete тело ключа недоступно, а Fingerprint(presented) считает из тела.
  - Что делать: ПЕРСИСТИТЬ fingerprint в записи (вычислять при create из префикса sha256(тело) — необратим, нечувствителен), тогда create И delete аудит содержат fingerprint из записи (AC выполнен). Обновить impl-notes честно (что и почему изменено).
- [ ] ISSUE-4 (needs-changes): CLI `switch err { case keystore.ErrCorrupt }`, но `Open`/`readDB` оборачивают через `fmt.Errorf("%w", ...)` → ветка не сработает, пользователь получит generic вместо специфичного сообщения с hint.
  - Где: `internal/cli/key.go:99,180,268`.
  - Что делать: заменить на `errors.Is(err, keystore.ErrCorrupt)` (и для прочих sentinel — ErrNotFound/ErrAlreadyRevoked/ErrLabelTooLong, если они тоже оборачиваются).
- [ ] ISSUE-3 (low): мёртвая функция `formatDate` (`internal/cli/key.go:349-355`) не вызывается. Удалить.

## Looks good
- §1: crypto/rand (нет math/rand, grep), 32 байта; sha256(тело+salt)+salt, PlainKey не в Database/Record (тест TestNoPlaintextInDB); constant-time с перебором всех активных (нет timing-leak по позиции); keys.db 0600, atomic write с temp 0600 ДО записи + fsync каталога; corrupt→ErrCorrupt без перезаписи.
- flock-режимы по plan (exclusive: Create/Revoke/FlushUsage; shared: List/Verify); Verify read-only (LastUsed в память); FlushUsage не воскрешает revoked.
- Вывод: ключ только при create (тело на stdout, тест), list/ошибки без тела; revoked скрыты; delete→revoked.
- Зависимости из STACK (charmbracelet/log, tablewriter).

## Verdict
needs-changes
(needs-changes: ISSUE-1, 2, 4; low: ISSUE-3. §1 нарушений нет — blocked не выставлен.)
