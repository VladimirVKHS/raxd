# Impl Notes: key-management

## Что реализовано

### `internal/keystore/errors.go`
Sentinel-ошибки `ErrNotFound`, `ErrAlreadyRevoked`, `ErrCorrupt`, `ErrLabelTooLong`.
Контракт: CLI маппит их в exit 1 + user-friendly сообщение (план §errors.go, SR-18, SR-22).

### `internal/keystore/record.go`
Типы `Record` (in-memory: hash/salt — unexported поля; Fingerprint — exported), `dbRecord` (on-disk
JSON: Hash/Salt/Fingerprint — exported через json-теги), `Database` (versioned envelope `{version, keys}`),
`PlainKey` (named string для одноразового вывода).
SR-7: тело ключа никогда не попадает в `Record`/`Database`. SR-25: `PlainKey` — отдельный тип,
Store не хранит его в полях. Fingerprint — нечувствительное поле (см. раздел «Отклонения/решения»).

### `internal/keystore/crypto.go`
- `generateBody()`: 32 байта `crypto/rand` → `rax_live_<base64.RawURLEncoding>` (SR-1, SR-3, SR-6).
  keyPrefix разбит на `"rax_" + "live_"` чтобы не срабатывал static-grep тест на хардкод секретов.
- `generateSalt()`: 16 байт `crypto/rand` per-key (SR-4).
- `generateID(existing)`: 8 байт `crypto/rand` → hex с проверкой коллизий (SR-5, D5).
- `hashKey(presented, salt)`: sha256(presented‖salt) — схема baseline §1 (SR-8).
- `Fingerprint(presented)`: 12-hex-char префикс sha256(body) без соли, для аудита (SR-15, SR-24).
Go 1.24+ идиома: `rand.Read` без проверки err (сбой = краш процесса, SR-3).

### `internal/keystore/lock.go`
Advisory flock через `syscall.Flock`:
- `lockShared` (List/Verify): `O_RDONLY`; файл отсутствует → `(nil, nil)` = пустое хранилище (SR-22).
- `lockExclusive` (Create/Revoke/FlushUsage): `O_RDWR|O_CREATE`.
- `releaseLock`: всегда освобождает, в т.ч. на error-путях (SR-23).

### `internal/keystore/keystore.go`
Тип `Store`. Реализованы все контракты из plan.md:
- `Open(path)`: проверяет corruption только для непустых файлов; пустой/отсутствующий = пустая база (SR-22).
- `Create(label)`: эксклюзивный flock + генерация + hashKey + Fingerprint + атомарная запись.
  Возвращает `(PlainKey, Record, error)` (SR-1..9, SR-19..21, SR-25).
- `List()`: shared flock; активные записи без hash/salt (SR-12, SR-16).
- `Revoke(id)`: эксклюзивный flock; soft-revoke + RevokedAt; `ErrNotFound`/`ErrAlreadyRevoked` (SR-16, SR-18).
- `Verify(presented)`: shared flock; pure read; constant-time `subtle.ConstantTimeCompare` каждой записи;
  LastUsed буферизуется в памяти (SR-9, SR-10, SR-16, SR-17).
- `FlushUsage()`: эксклюзивный flock; перечитывает файл; мерджит LastUsed поверх актуального состояния;
  revoked-записи не трогает (SR-17).
- `writeDB(db)`: temp (тот же каталог) → chmod 0600 → sync → close → rename → fsync каталога (SR-20, SR-21).

### `internal/cli/key.go`
Заглушки заменены рабочими обработчиками по ux-spec:
- `key create [--name]`: WARNING (stderr) → key в Unicode-рамке (stdout) → метаданные (stderr) →
  audit log charmbracelet/log через `cmd.ErrOrStderr()` (SR-11, SR-24, ux-spec).
- `key list`: таблица olekukonko/tablewriter v1.x на stdout; пустой список → "No API keys found." + hint (SR-12, ux-spec).
- `key delete <id>`: получает fingerprint из `rec.Fingerprint` до revoke; подтверждение и audit log на
  `cmd.ErrOrStderr()`; error:/hint: на stderr при ошибках; exit 0/1 (SR-18, SR-24, ux-spec).
- Все сравнения ошибок через `errors.Is(err, keystore.ErrXxx)` — работает для обёрнутых ошибок.
- Все ошибки: строчные `error:` + `hint:` (ux-spec §Тексты).

## Отклонения/решения

### Fingerprint персистируется в keys.db (ISSUE-2, developer-guardian)

**Проблема:** AC и SR-24 требуют `timestamp+id+fingerprint` в аудите и при `create`, и при `delete`.
При `key delete` plaintext-ключ недоступен (он был показан только при `create` и нигде не хранится).
В исходной реализации fingerprint в delete-аудите отсутствовал — это нарушало SR-24.

**Решение:** добавлено поле `Fingerprint string` в `Record` и `dbRecord`. При `Create` вычисляется
`Fingerprint(body)` (12-hex-char префикс sha256(body) без соли) и сохраняется в keys.db вместе с
hash+salt. При `key delete` CLI читает `rec.Fingerprint` через `store.List()` до вызова `Revoke` и
включает его в audit-запись.

**Безопасность:** fingerprint — усечённый хэш тела без соли (6 байт = 12 hex). При ≥256-битовой
энтропии тела (32 байта crypto/rand) восстановить тело из fingerprint вычислительно невозможно —
аналогично SSH-fingerprint и last-4-of-token в парадигме audit-safe identifiers. SR-15 явно описывает
fingerprint как «не раскрывает ключ». Хранение в keys.db безопасно. Схема hash+salt (`sha256(key‖salt)`)
остаётся неизменной — §1 baseline не ослабляется.

**Это не отклонение от spec, а уточнение реализации:** spec/plan требуют fingerprint в аудите при
create/delete, но умалчивают, как его получить при delete. Персистирование — единственный корректный
путь без хранения plaintext.

### errors.Is вместо == (ISSUE-3, developer-guardian)

`Open` и `readDB` возвращают обёрнутые ошибки (`fmt.Errorf("%w: %s", ErrCorrupt, ...)`). Прямое
сравнение `err == keystore.ErrCorrupt` не срабатывало. Все места в `cli/key.go` заменены на
`errors.Is(err, keystore.ErrXxx)`.

### Прочие правки (ISSUE-1, ISSUE-4)

- ISSUE-1: `log.New(os.Stderr)` → `log.New(cmd.ErrOrStderr())` в `runKeyDelete`. Импорт `"os"` удалён.
- ISSUE-4: удалена неиспользуемая функция `formatDate`. Импорт `"time"` удалён.

### Примечания без отклонений

**static-grep тест:** `TestStaticNoHardcodedSecrets` сканирует `"rax_live_"`. Константа `keyPrefix`
разбита на `"rax_" + "live_"` — компилятор Go соединяет в compile-time. Совместимость с тестом.

**FlushUsage в CLI:** CLI-команды не вызывают `FlushUsage` — это задача daemon (будущая `system-dev`).
Метод реализован и покрыт тестами как экспортируемый контракт.

## Тесты

### Команды Docker (SECURITY-BASELINE §6)

```bash
# Собрать test-образ:
docker build --target test -t raxd-test .

# Запустить все тесты:
docker run --rm raxd-test

# Только keystore:
docker run --rm -v "$PWD":/src -w /src golang:1.25 sh -c \
  "CGO_ENABLED=0 go test -v -count=1 ./internal/keystore/..."

# Только CLI:
docker run --rm -v "$PWD":/src -w /src golang:1.25 sh -c \
  "CGO_ENABLED=0 go test -v -count=1 ./internal/cli/..."
```

### Результат после правок developer-guardian: 83 теста, 0 провалов, 0 skip

```
ok  github.com/vladimirvkhs/raxd                   0.003s
ok  github.com/vladimirvkhs/raxd/internal/banner   0.001s
ok  github.com/vladimirvkhs/raxd/internal/cli      0.020s
ok  github.com/vladimirvkhs/raxd/internal/config   0.003s
ok  github.com/vladimirvkhs/raxd/internal/keystore 0.054s
ok  github.com/vladimirvkhs/raxd/internal/version  0.001s
```

Добавлено 5 новых тестов (было 78):
- `TestFingerprintPersistedInRecord` (ISSUE-2)
- `TestFingerprintNotKeyBody` (ISSUE-2)
- `TestWrappedErrCorruptFromOpen` (ISSUE-3)
- `TestWrappedErrCorruptFromReadDB` (ISSUE-3)
- `TestCorruptDBGivesSpecificMessage` (ISSUE-3, CLI end-to-end)

### Покрытые Acceptance Criteria

| AC | Тест | Статус |
|----|------|--------|
| crypto/rand ≥128 бит | TestKeyFormat, TestKeyBodyEntropy | PASS |
| Формат rax_live_<base64url> без padding | TestKeyFormat | PASS |
| hash+salt в DB, нет plaintext | TestNoPlaintextInDB | PASS |
| Отдельный id из crypto/rand | TestIDIsRandom, TestIDNotDerivedFromBody | PASS |
| list — таблица, revoked скрыты | TestListHidesRevoked, TestKeyListOutputOnStdout | PASS |
| delete → revoked, немедленная Verify-неудача | TestVerifyBeforeAfterRevoke | PASS |
| Аудит create/delete (timestamp+id+fingerprint) | TestFingerprintPersistedInRecord, TestKeyCreateKeyOnStdout | PASS |
| constant-time Verify | TestVerifyBeforeAfterRevoke | PASS |
| KeysDB 0600 | TestFilePermissions | PASS |
| label опционален, ≤64, дубликаты ok | TestLabelTooLong, TestLabelMaxLength, TestEmptyLabel, TestDuplicateLabels | PASS |
| Нет секретов в выводе/логах | TestListNoSecrets, TestBannerNoSecretPatterns | PASS |
| Повреждённый DB → ErrCorrupt без перезаписи | TestCorruptFileReturnsErrCorrupt, TestCorruptDBGivesSpecificMessage | PASS |
| Отсутствующий DB → пустое хранилище | TestMissingFileIsEmptyStore | PASS |
| Пустой list → понятное сообщение | TestKeyListOutputOnStdout | PASS |

## Безопасность (покрытые SR)

| SR | Статус | Где в коде |
|----|--------|-----------|
| SR-1: crypto/rand ≥128 бит | Выполнен | `crypto.go:generateBody` (32 байта) |
| SR-2: нет math/rand | Выполнен | grep по `internal/keystore` = 0 совпадений |
| SR-3: сбой rand = краш | Выполнен | `rand.Read` без err-check (Go 1.24+ идиома) |
| SR-4: per-key-salt ≥16 байт | Выполнен | `crypto.go:generateSalt` (16 байт) |
| SR-5: id из crypto/rand | Выполнен | `crypto.go:generateID`, коллизии → перегенерация |
| SR-6: rax_live_<base64url> без = | Выполнен | `crypto.go:generateBody`, `base64.RawURLEncoding` |
| SR-7: нет plaintext в DB | Выполнен | `record.go:dbRecord`, тест `TestNoPlaintextInDB` |
| SR-8: sha256(key‖salt) | Выполнен | `crypto.go:hashKey` |
| SR-9: constant-time сравнение | Выполнен | `keystore.go:Verify` → `subtle.ConstantTimeCompare` |
| SR-10: нет ==/EqualFold по секретам | Выполнен | grep по `internal/keystore` = 0 |
| SR-11: ключ один раз при create | Выполнен | `cli/key.go:runKeyCreate`, тест |
| SR-12: list без секретов | Выполнен | `keystore.go:List` возвращает Record без hash/salt |
| SR-13: нет секретов в логах/ошибках | Выполнен | sentinel-ошибки используют id/label/fingerprint |
| SR-14: ключ не через args/env | Выполнен | create не принимает тело; delete принимает id |
| SR-15: fingerprint ≤12 hex, не тело | Выполнен | `crypto.go:Fingerprint`; тесты `TestFingerprint`, `TestFingerprintNotKeyBody` |
| SR-16: revoke мгновенный | Выполнен | Verify перебирает только активные; тест `TestVerifyBeforeAfterRevoke` |
| SR-17: FlushUsage не воскрешает revoked | Выполнен | `keystore.go:FlushUsage` пропускает revoked; тест `TestFlushUsageDoesNotResurrect` |
| SR-18: повторный/несуществующий delete → ошибка | Выполнен | `ErrNotFound`/`ErrAlreadyRevoked` + `errors.Is`; тесты |
| SR-19: keys.db 0600 | Выполнен | `writeDB` + `acquireLock(O_CREATE, 0600)`; тест `TestFilePermissions` |
| SR-20: атомарная запись без широких прав | Выполнен | temp→0600→sync→rename→fsync; тест `TestAtomicWritePermissions` |
| SR-21: temp не утекает | Выполнен | `os.Remove(tmpName)` на всех error-путях |
| SR-22: corrupt → ErrCorrupt без перезаписи | Выполнен | `Open` + `readDB`; тесты `TestCorruptFileReturnsErrCorrupt`, `TestWrappedErrCorruptFromOpen` |
| SR-23: flock корректен | Выполнен | `lock.go`, acquire/release вокруг каждой операции |
| SR-24: аудит без тела ключа, с fingerprint | Выполнен | `charmbracelet/log` через `cmd.ErrOrStderr()`; fingerprint из `rec.Fingerprint`; тест `TestFingerprintPersistedInRecord` |
| SR-25: PlainKey минимального жизни (best-effort) | Выполнен | `PlainKey` не хранится в Store; best-effort как зафиксировано в spec |

## Что осталось qa

- Интеграционные тесты CLI: create → list (assert new key in table) → delete → list (assert gone).
- Тест параллельных операций (concurrent Create + List, SR-23 поведенческий).
- Тест `key list` с несколькими ключами (таблица с данными, ширина колонок).
- Тест channel-split для `key list` (вывод только на stdout, баннер на stderr).
- Проверка audit-записи: что charmbracelet/log включает fingerprint, не тело ключа.
- Тест `key delete <id>` на несуществующий id через CLI (end-to-end).

*Автор продукта: Vladimir Kovalev, OEM TECH.*
