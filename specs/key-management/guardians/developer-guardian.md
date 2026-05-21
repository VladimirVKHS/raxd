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

## Verdict (раунд 1)
needs-changes
(needs-changes: ISSUE-1, 2, 4; low: ISSUE-3. §1 нарушений нет — blocked не выставлен.)

---

## Повторная проверка (раунд 2)
Все четыре пункта закрыты:
- ISSUE-1: аудит delete через `cmd.ErrOrStderr()`; импорт os удалён; консистентно с create.
- ISSUE-2: поле `Fingerprint string` персистится в Record/dbRecord (12-hex префикс sha256(тело), необратим/нечувствителен — §1 НЕ ослаблен: тело не хранится, hash+salt не изменены); create И delete аудит содержат timestamp+id+fingerprint (AC/SR-24); impl-notes честно описывает решение; тесты TestFingerprintPersistedInRecord/TestFingerprintNotKeyBody.
- ISSUE-3: formatDate удалена, импорты os/time вычищены.
- ISSUE-4: сравнения через errors.Is; тест TestCorruptDBGivesSpecificMessage (обёрнутый ErrCorrupt → специфичное сообщение+hint).
Побочных регрессий нет (TestListNoSecrets/TestNoPlaintextInDB остаются валидны). Docker: go vet чисто, 83/83 PASS, 0 skip.

## Verdict (раунд 2)
pass

---

## Раунд 3 — фиксы по reviewer (data race и др.)
- Issue 1 (data race): добавлен `mu sync.Mutex`; запись usageBuf в Verify под mu; FlushUsage snapshot-паттерн (копия+очистка под mu → file I/O без mu; restore при ошибке не перетирает более свежие значения из concurrent Verify и не трогает revoked на диске). flock сохранён. `go test -race ./internal/keystore/...` — 0 гонок. Двойных lock/забытых unlock нет.
- Issue 2: `utf8.RuneCountInString(label) > 64`.
- Issue 3: единый printStoreError, friendlyErr удалён.
- Bonus: Open через os.ReadFile+json.Unmarshal (детект trailing garbage; ErrCorrupt без перезаписи).
- §1 не ослаблен.

ЗАМЕЧАНИЕ (для reviewer/последующего фикса): после рефакторинга printStoreError у `ErrAlreadyRevoked` пропал `hint:` (ux-spec требует «hint: run "raxd key list" to see active keys»); формат ErrNotFound стал `<id>: key not found` вместо ux-spec `key "<id>" not found`. Не нарушение §1/exit-кодов; зона ux-spec-соответствия.

## Verdict (раунд 3)
pass (с замечанием по ux-текстам — к исправлению до финального reviewer)

---

## Раунд 4 — фикс ux-текстов
- ErrNotFound: `error: key "<id>" not found` + `hint: run "raxd key list" to see available key IDs` — точно по ux-spec.
- ErrAlreadyRevoked: `error: key "<id>" is already revoked` + `hint: run "raxd key list" to see active keys` — hint восстановлен, по ux-spec.
- ErrCorrupt/ErrLabelTooLong — без изменений (уже соответствовали).
- Тесты TestKeyDeleteNotFoundCLI/TestKeyDeleteAlreadyRevokedCLI усилены до точных строк (fmt.Sprintf), exit≠0, без секретов.
- §1 не затронут.

## Verdict (финал)
pass

---

## Раунд 5 — Q5-фикс (полный id в `key list`), 2026-05-21

Коммиты `cfe7bcc` + `6cc39ab`. `internal/cli/key.go:221-223` — `idDisplay := "  " + r.ID`,
усечение до 12 убрано; мёртвая функция `truncate` (без эллипсиса) удалена; `truncateEllipsis`
(LABEL, 20 рун) сохранена. Тест `TestKeyListIDUsableWithDelete` (`cli_key_qa_test.go:94-133`):
create → полный id из stderr → наличие в `key list` → `key delete <id>` exit 0.

- SR-12: тело ключа в `key list` не печатается (`TestKeyListNoSecretsOnStdout`); id — случайный hex,
  не связан с телом.
- SR-15: id несекретен; fingerprint (sha256(тело)) хранится отдельно, в list не выводится.
- SR-9/SR-10: `Verify`/`subtle.ConstantTimeCompare` не изменялись.
- Docker (offline/vendor): `go vet`, `go test ./...`, `-race` keystore — PASS, 88 тестов, 0 race.

Находка (cosmetic, не блокер): `key.go:206-218` `WithBorders{все Off}` + комментарий
«no outer border / 2-space left indent» вводят в заблуждение — tablewriter v1.1.4 рендерит
полную рамку и стрипит ведущие пробелы; вывод корректен и принят командой. **Дирижёр: отложено
как косметика (правка комментария не оправдывает холодную пересборку в Docker), зафиксировано.**

## Verdict (раунд 5)
pass



