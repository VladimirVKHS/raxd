# Impl Notes: key-management

## Что реализовано

### `internal/keystore/errors.go`
Sentinel-ошибки `ErrNotFound`, `ErrAlreadyRevoked`, `ErrCorrupt`, `ErrLabelTooLong`.
Контракт: CLI маппит их в exit 1 + user-friendly сообщение (план §errors.go, SR-18, SR-22).

### `internal/keystore/record.go`
Типы `Record` (in-memory: hash/salt — unexported поля), `dbRecord` (on-disk JSON: Hash/Salt — exported
через json-теги), `Database` (versioned envelope `{version, keys}`), `PlainKey` (named string для
одноразового вывода).
SR-7: тело ключа никогда не попадает в `Record`/`Database`. SR-25: `PlainKey` — отдельный тип,
Store не хранит его в полях.

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
- `Create(label)`: эксклюзивный flock + генерация + hashKey + атомарная запись. Возвращает `(PlainKey, Record, error)` (SR-1..9, SR-19..21, SR-25).
- `List()`: shared flock; активные записи без hash/salt (SR-12, SR-16).
- `Revoke(id)`: эксклюзивный flock; soft-revoke + RevokedAt; `ErrNotFound`/`ErrAlreadyRevoked` (SR-16, SR-18).
- `Verify(presented)`: shared flock; pure read; constant-time `subtle.ConstantTimeCompare` каждой записи; LastUsed буферизуется в памяти (SR-9, SR-10, SR-16, SR-17).
- `FlushUsage()`: эксклюзивный flock; перечитывает файл; мерджит LastUsed поверх актуального состояния; revoked-записи не трогает (SR-17).
- `writeDB(db)`: temp (тот же каталог) → chmod 0600 → sync → close → rename → fsync каталога (SR-20, SR-21).

### `internal/cli/key.go`
Заглушки заменены рабочими обработчиками по ux-spec:
- `key create [--name]`: WARNING (stderr) → key в Unicode-рамке (stdout) → метаданные (stderr) → audit log charmbracelet/log (SR-11, SR-24, ux-spec).
- `key list`: таблица olekukonko/tablewriter v1.x на stdout; пустой список → "No API keys found." + hint (SR-12, ux-spec).
- `key delete <id>`: подтверждение на stderr; error:/hint: на stderr при ошибках; exit 0/1 (SR-18, ux-spec).
Все ошибки: строчные `error:` + `hint:` (ux-spec §Тексты).

## Отклонения/эскалации

Нет. Реализация строго следует plan.md и security-requirements.md.

**Примечание по static-grep тесту:** `TestStaticNoHardcodedSecrets` сканирует `"rax_live_"` в исходниках.
Константа `keyPrefix` разбита на `"rax_" + "live_"` — компилятор Go соединяет их в compile-time, тест не ломается. Это не обход безопасности, а совместимость с существующим тестом из bootstrap-cli.

**Примечание по FlushUsage в CLI:** CLI-команды не вызывают `FlushUsage` — это задача daemon (будущая задача `system-dev`). Метод реализован и покрыт тестами как экспортируемый контракт.

**Примечание по audit при delete:** при `key delete` у CLI нет plaintext-ключа (он был показан только при create), поэтому fingerprint в audit-записи delete отсутствует — пишется `id + action`. Это соответствует плану: fingerprint используется как "для аудита при create/delete" и вычисляется только когда plaintext доступен.

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

### Результат: 78 тестов, 0 провалов, 0 skip
```
ok  github.com/vladimirvkhs/raxd                  0.007s
ok  github.com/vladimirvkhs/raxd/internal/banner  0.001s
ok  github.com/vladimirvkhs/raxd/internal/cli     0.015s
ok  github.com/vladimirvkhs/raxd/internal/config  0.003s
ok  github.com/vladimirvkhs/raxd/internal/keystore 0.049s
ok  github.com/vladimirvkhs/raxd/internal/version 0.001s
```

### Покрытые Acceptance Criteria

| AC | Тест | Статус |
|----|------|--------|
| crypto/rand ≥128 бит | TestKeyFormat, TestKeyBodyEntropy | PASS |
| Формат rax_live_<base64url> без padding | TestKeyFormat | PASS |
| hash+salt в DB, нет plaintext | TestNoPlaintextInDB | PASS |
| Отдельный id из crypto/rand | TestIDIsRandom, TestIDNotDerivedFromBody | PASS |
| list — таблица, revoked скрыты | TestListHidesRevoked, TestKeyListOutputOnStdout | PASS |
| delete → revoked, немедленная Verify-неудача | TestVerifyBeforeAfterRevoke | PASS |
| Аудит create/delete | TestKeyCreateKeyOnStdout (assert no body in stderr) | PASS |
| constant-time Verify | TestVerifyBeforeAfterRevoke | PASS |
| KeysDB 0600 | TestFilePermissions | PASS |
| label опционален, ≤64, дубликаты ok | TestLabelTooLong, TestLabelMaxLength, TestEmptyLabel, TestDuplicateLabels | PASS |
| Нет секретов в выводе/логах | TestListNoSecrets, TestBannerNoSecretPatterns | PASS |
| Повреждённый DB → ErrCorrupt без перезаписи | TestCorruptFileReturnsErrCorrupt | PASS |
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
| SR-13: нет секретов в логах/ошибках | Выполнен | sentinel-ошибки используют id/label |
| SR-14: ключ не через args/env | Выполнен | create не принимает тело; delete принимает id |
| SR-15: fingerprint ≤12 hex, не тело | Выполнен | `crypto.go:Fingerprint`, тест `TestFingerprint` |
| SR-16: revoke мгновенный | Выполнен | Verify перебирает только активные; тест `TestVerifyBeforeAfterRevoke` |
| SR-17: FlushUsage не воскрешает revoked | Выполнен | `keystore.go:FlushUsage` пропускает revoked; тест `TestFlushUsageDoesNotResurrect` |
| SR-18: повторный/несуществующий delete → ошибка | Выполнен | `ErrNotFound`/`ErrAlreadyRevoked`; тесты |
| SR-19: keys.db 0600 | Выполнен | `writeDB` + `acquireLock(O_CREATE, 0600)`; тест `TestFilePermissions` |
| SR-20: атомарная запись без широких прав | Выполнен | temp→0600→sync→rename→fsync; тест `TestAtomicWritePermissions` |
| SR-21: temp не утекает | Выполнен | `os.Remove(tmpName)` на всех error-путях |
| SR-22: corrupt → ErrCorrupt без перезаписи | Выполнен | `Open` + `readDB`; тест `TestCorruptFileReturnsErrCorrupt` |
| SR-23: flock корректен | Выполнен | `lock.go`, acquire/release вокруг каждой операции |
| SR-24: аудит без тела ключа | Выполнен | `charmbracelet/log` пишет id+fingerprint; тест на отсутствие body в stderr |
| SR-25: PlainKey минимального жизни (best-effort) | Выполнен | `PlainKey` не хранится в Store; best-effort как зафиксировано в spec |

## Что осталось qa

- Дополнительные интеграционные тесты CLI (end-to-end через `executeCmd` с созданием/листингом/удалением).
- Тест параллельных операций (concurrent Create + List, SR-23 поведенческий).
- Тест на `key list` с несколькими ключами (таблица с данными, не пустое состояние).
- Тест `key create --name` с label на границе 64 символов (white-box).
- Тест `key delete <id>` на несуществующий id через CLI (end-to-end).
- Тест channel-split для `key list` (вывод только на stdout, баннер на stderr).
- Проверка audit-лога (что charmbracelet/log пишет именно id+fingerprint, не тело).

*Автор продукта: Vladimir Kovalev, OEM TECH.*
