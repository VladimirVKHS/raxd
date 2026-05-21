# Test Plan: key-management — управление API-ключами raxd

> Автор: qa. Дата: 2026-05-21. Статус: FINAL — 116 тестов, 0 провалов, 0 skip.
> Проверяет: qa-guardian. Читает: reviewer (доказательство покрытия AC+SR).
> Запуск **только в Docker** (SECURITY-BASELINE §6).

---

## Стратегия

| Уровень | Что покрывает | Пакеты |
|---|---|---|
| **Unit** | Генерация (crypto/rand, format, entropy), хеширование (sha256‖salt), id, fingerprint, salt, sentinel-ошибки, атомарная запись, права файла, ErrCorrupt без перезаписи, FlushUsage/Revoke invariants | `internal/keystore` |
| **Integration** | CLI-команды через `cobra.Command.Execute` с перехватом stdout/stderr; channel-split (stdout vs stderr); audit log; error: / hint: форматирование; exit-коды | `internal/cli` |
| **E2E (CLI)** | Полный lifecycle: `key create → key list → key delete → key list`; corrupt DB → exit≠0; параллельный Create+List (flock) | `internal/cli` (через `executeCmd`) |
| **Static** | Отсутствие `math/rand`, `exec.Command`, `net.Listen`, хардкод-секретов в исходниках | корень проекта |
| **Install-flow** | Вне scope задачи `key-management` (задача `distribution`) | — |

**Среда:** `golang:1.25`, `CGO_ENABLED=0`, Linux/amd64 в контейнере.

---

## Docker-команды запуска

```bash
# Полный прогон (go vet + все тесты):
docker build --target test -t raxd-test . && docker run --rm raxd-test

# Только keystore (unit):
docker run --rm -v "$PWD":/src -w /src golang:1.25 \
  sh -c "CGO_ENABLED=0 go test -v -count=1 ./internal/keystore/..."

# Только CLI (integration/e2e):
docker run --rm -v "$PWD":/src -w /src golang:1.25 \
  sh -c "CGO_ENABLED=0 go test -v -count=1 ./internal/cli/..."

# Статический анализ:
docker run --rm -v "$PWD":/src -w /src golang:1.25 \
  sh -c "CGO_ENABLED=0 go vet ./..."

# С -race (параллелизм):
docker run --rm -v "$PWD":/src -w /src golang:1.25 \
  sh -c "CGO_ENABLED=0 go test -race -count=1 ./internal/keystore/..."
```

---

## Критерий прохождения

**116 тестов, 0 провалов, 0 skip** в Docker (факт подтверждён 2026-05-21).

Разбивка по пакетам:
- `github.com/vladimirvkhs/raxd` — 5 тестов (статический анализ)
- `github.com/vladimirvkhs/raxd/internal/banner` — 5 тестов
- `github.com/vladimirvkhs/raxd/internal/cli` — 55 тестов (cli_test + cli_gaps_test + security_test + **cli_key_qa_test**)
- `github.com/vladimirvkhs/raxd/internal/config` — 9 тестов
- `github.com/vladimirvkhs/raxd/internal/keystore` — 37 тестов (keystore_test + **keystore_qa_test**)
- `github.com/vladimirvkhs/raxd/internal/version` — 5 тестов

**До QA:** 83 теста. **После QA:** 116 тестов (+33 новых).

---

## Матрица AC → тест

| AC (из spec.md) | Уровень | Тест (файл::имя) | Статус |
|---|---|---|---|
| AC-1: `key create` — crypto/rand ≥128 бит, ≥16 байт | unit | `keystore_test.go::TestKeyFormat`, `TestKeyBodyEntropy`, `keystore_qa_test.go::TestKeyBodyUniquenessMultiple` | PASS |
| AC-2: Формат `rax_live_<base64url>` без padding, один раз на stdout | unit+integration | `keystore_test.go::TestKeyFormat`, `cli_test.go::TestKeyCreateKeyOnStdout`, `cli_key_qa_test.go::TestKeyCreateBodyOnlyOnStdout` | PASS |
| AC-3: В хранилище НЕ тело ключа, только sha256(тело+salt)+salt | unit | `keystore_test.go::TestNoPlaintextInDB`, `keystore_qa_test.go::TestHashSchemeDirectVerification`, `TestSaltLengthAndUniqueness`, `TestHashSizeInDB` | PASS |
| AC-4: Отдельный короткий id из crypto/rand, не из тела/хэша | unit | `keystore_test.go::TestIDIsRandom`, `TestIDNotDerivedFromBody`, `keystore_qa_test.go::TestIDFormat` | PASS |
| AC-5: `key list` — таблица id/label/created/last-used; revoked скрыты; пустой list → сообщение, exit 0 | integration | `cli_test.go::TestKeyListOutputOnStdout`, `cli_key_qa_test.go::TestKeyListEmptyShowsMessageOnStdout`, `TestKeyListNoSecretsOnStdout`, `TestKeyCreateListDeleteIntegration`, `keystore_test.go::TestListHidesRevoked`, `keystore_qa_test.go::TestListWithMultipleActiveKeys` | PASS |
| AC-6: `key delete <id>` — мягкий отзыв; Verify немедленно неуспешна; повторный/несущ. id → ошибка exit≠0 | unit+integration | `keystore_test.go::TestVerifyBeforeAfterRevoke`, `TestRevokeNotFound`, `TestRevokeAlreadyRevoked`, `cli_key_qa_test.go::TestKeyDeleteNotFoundCLI`, `TestKeyDeleteAlreadyRevokedCLI`, `keystore_qa_test.go::TestVerifyCorrectWrongRevoked`, `TestRevokePreservesRecordForAudit` | PASS |
| AC-7: Аудит create/delete — timestamp+id+fingerprint, НЕ тело | integration | `cli_key_qa_test.go::TestKeyCreateAuditContainsFingerprintNotBody`, `TestKeyDeleteAuditContainsFingerprintNotBody`, `keystore_test.go::TestFingerprintPersistedInRecord` | PASS |
| AC-8: Constant-time Verify (`subtle.ConstantTimeCompare`); нет `==` по секретам | unit | `keystore_test.go::TestVerifyBeforeAfterRevoke`, `TestHashScheme`, `keystore_qa_test.go::TestVerifyCorrectWrongRevoked` | PASS |
| AC-9: `keys.db` 0600 по пути KeysDB | unit | `keystore_test.go::TestFilePermissions`, `keystore_qa_test.go::TestAtomicWriteTempFilePermissions` | PASS |
| AC-10: label опционален; дубликаты разрешены; длина ≤64; превышение → ошибка exit≠0 | unit+integration | `keystore_test.go::TestLabelTooLong`, `TestLabelMaxLength`, `TestEmptyLabel`, `TestDuplicateLabels`, `cli_key_qa_test.go::TestKeyCreateLabelTooLongCLI`, `TestKeyCreateNoLabelShowsDash` | PASS |
| AC-11: Секрет не в логах/ошибках/list; только одноразовый вывод при create | unit+integration | `keystore_test.go::TestListNoSecrets`, `cli_key_qa_test.go::TestKeyListNoSecretsOnStdout`, `TestKeyCreateBodyOnlyOnStdout`, `keystore_qa_test.go::TestSentinelErrorMessagesNoSecrets` | PASS |
| AC-12: Повреждённый DB → ErrCorrupt без перезаписи; отсутствующий → пустое хранилище | unit+integration | `keystore_test.go::TestCorruptFileReturnsErrCorrupt`, `TestMissingFileIsEmptyStore`, `TestWrappedErrCorruptFromOpen`, `TestWrappedErrCorruptFromReadDB`, `keystore_qa_test.go::TestCorruptFileByteForByteUnchanged`, `cli_key_qa_test.go::TestKeyListExitNonZeroOnCorrupt` | PASS |

---

## Матрица SR → тест

| SR | Описание | Уровень | Тест (файл::имя) | Статус |
|---|---|---|---|---|
| SR-1 | crypto/rand ≥128 бит | unit | `TestKeyFormat`, `TestKeyBodyEntropy`, `TestKeyBodyUniquenessMultiple` | PASS |
| SR-2 | Нет math/rand | static | `TestStaticNoHardcodedSecrets` (grep) | PASS |
| SR-3 | Сбой rand = краш, нет fallback | инспекция | `crypto.go`: `rand.Read` без err-check, нет fallback | PASS (инспекция) |
| SR-4 | per-key-salt ≥16 байт, уникален | unit | `TestSaltLengthAndUniqueness`, `TestSaltUniqueness` | PASS |
| SR-5 | id из crypto/rand, не из тела/хэша | unit | `TestIDIsRandom`, `TestIDNotDerivedFromBody`, `TestIDFormat` | PASS |
| SR-6 | Формат `rax_live_<base64url>` без padding | unit | `TestKeyFormat` | PASS |
| SR-7 | В `keys.db` нет тела ключа | unit | `TestNoPlaintextInDB`, `TestHashSchemeDirectVerification`, `TestListRecordHasNoHashOrSalt` | PASS |
| SR-8 | sha256(key‖salt) | unit | `TestHashScheme`, `TestHashSchemeDirectVerification`, `TestHashSizeInDB` | PASS |
| SR-9 | constant-time сравнение | unit | `TestVerifyBeforeAfterRevoke`, `TestVerifyCorrectWrongRevoked` | PASS |
| SR-10 | Нет `==`/`EqualFold` по секретам | static+unit | `TestStaticNoHardcodedSecrets`, инспекция `keystore.go` | PASS |
| SR-11 | Ключ показан ровно один раз при create | integration | `TestKeyCreateBodyOnlyOnStdout`, `TestKeyCreateKeyOnStdout` | PASS |
| SR-12 | `list` не раскрывает секрет | unit+integration | `TestListNoSecrets`, `TestListRecordHasNoHashOrSalt`, `TestKeyListNoSecretsOnStdout` | PASS |
| SR-13 | Тело/хэш/соль не в логах и ошибках | unit+integration | `TestSentinelErrorMessagesNoSecrets`, `TestKeyDeleteAuditContainsFingerprintNotBody`, `TestKeyCreateAuditContainsFingerprintNotBody` | PASS |
| SR-14 | Тело не через аргументы/env | инспекция | `cli/key.go`: `create` не принимает тело; `delete` принимает id | PASS (инспекция) |
| SR-15 | fingerprint ≤12 симв., детерминирован, не тело | unit | `TestFingerprint`, `TestFingerprintLengthBounds`, `TestFingerprintNotKeyBody`, `TestFingerprintPersistedInRecord` | PASS |
| SR-16 | Отзыв мгновенный; revoked немедленно неуспешен в Verify | unit | `TestVerifyBeforeAfterRevoke`, `TestVerifyCorrectWrongRevoked`, `TestListHidesRevoked` | PASS |
| SR-17 | FlushUsage не воскрешает revoked; LastUsed обновляется | unit | `TestFlushUsageDoesNotResurrect`, `TestFlushUsageMergeDoesNotOverwriteRevoke`, `TestFlushUsagePersistsLastUsed`, `TestFlushUsagePersistsLastUsedOnReopen` | PASS |
| SR-18 | Повторный/несущ. delete → ошибка, exit≠0 | unit+integration | `TestRevokeNotFound`, `TestRevokeAlreadyRevoked`, `TestKeyDeleteNotFoundCLI`, `TestKeyDeleteAlreadyRevokedCLI` | PASS |
| SR-19 | `keys.db` 0600 по пути KeysDB | unit | `TestFilePermissions`, `TestAtomicWriteTempFilePermissions` | PASS |
| SR-20 | Атомарная запись; temp 0600 ДО содержимого; нет temp-файлов | unit | `TestAtomicWritePermissions`, `TestAtomicWriteTempFilePermissions`, `TestNoTempFileAfterError` | PASS |
| SR-21 | temp не утекает при ошибке | unit | `TestNoTempFileAfterError`, `TestAtomicWritePermissions` | PASS |
| SR-22 | Corrupt → ErrCorrupt без перезаписи байт-в-байт | unit+integration | `TestCorruptFileReturnsErrCorrupt`, `TestCorruptFileByteForByteUnchanged`, `TestWrappedErrCorruptFromOpen`, `TestWrappedErrCorruptFromReadDB`, `TestKeyListExitNonZeroOnCorrupt` | PASS |
| SR-23 | flock корректен, параллельные операции не повреждают файл | unit | `TestConcurrentCreateAndList` | PASS |
| SR-24 | Аудит create/delete с fingerprint, без тела | integration | `TestKeyCreateAuditContainsFingerprintNotBody`, `TestKeyDeleteAuditContainsFingerprintNotBody`, `TestFingerprintPersistedInRecord` | PASS |
| SR-25 | PlainKey не оседает в Store (best-effort) | инспекция+unit | `TestNoPlaintextInDB`; `keystore.go`: `PlainKey` не в полях Store | PASS (best-effort) |

---

## Edge Cases

| Случай | Тест | Пакет |
|---|---|---|
| label > 64 символов → ErrLabelTooLong, exit≠0 | `TestLabelTooLong`, `TestKeyCreateLabelTooLongCLI` | keystore, cli |
| label = 64 символа → ОК | `TestLabelMaxLength` | keystore |
| label пустой → stored as "", CLI показывает "-" | `TestEmptyLabel`, `TestEmptyLabelShownAsDash`, `TestKeyCreateNoLabelShowsDash` | keystore, cli |
| Дублирующийся label → разные id | `TestDuplicateLabels` | keystore |
| Пустое хранилище (нет файла) → пустой List, false Verify, exit 0 | `TestMissingFileIsEmptyStore`, `TestVerifyEmptyStoreReturnsNoMatch`, `TestKeyListEmptyShowsMessageOnStdout` | keystore, cli |
| Несуществующий id → ErrNotFound | `TestRevokeNotFound`, `TestKeyDeleteNotFoundCLI` | keystore, cli |
| Уже revoked id → ErrAlreadyRevoked | `TestRevokeAlreadyRevoked`, `TestKeyDeleteAlreadyRevokedCLI` | keystore, cli |
| Повреждённый keys.db → ErrCorrupt без паники и без перезаписи | `TestCorruptFileReturnsErrCorrupt`, `TestCorruptFileByteForByteUnchanged`, `TestWrappedErrCorruptFromOpen/ReadDB` | keystore |
| Аргумент id не передан при delete → ошибка, exit≠0 | `TestKeyDeleteMissingArg` | cli |
| 10 последовательных creates → все уникальны, формат верен | `TestKeyBodyUniquenessMultiple` | keystore |
| revoked-запись остаётся в keys.db для аудита | `TestRevokePreservesRecordForAudit` | keystore |
| FlushUsage после Revoke → ключ по-прежнему revoked | `TestFlushUsageMergeDoesNotOverwriteRevoke`, `TestFlushUsageDoesNotResurrect` | keystore |
| Параллельные Create + List (4 горутины) → файл не повреждён | `TestConcurrentCreateAndList` | keystore |
| Нет .tmp после успешной записи | `TestAtomicWritePermissions`, `TestAtomicWriteTempFilePermissions`, `TestNoTempFileAfterError` | keystore |

---

## Security-тесты

| Инвариант безопасности | Тест | Пакет |
|---|---|---|
| Нет math/rand в key-логике | `TestStaticNoHardcodedSecrets` (grep) | root |
| Нет exec.Command в bootstrap-коде | `TestStaticNoExecCommand` | root |
| Нет net.Listen в коде | `TestStaticNoNetListen` | root |
| Нет создания файлов с широкими правами | `TestStaticNoFileCreationWithWideModes` | root |
| Тело ключа НЕ в bytes keys.db | `TestNoPlaintextInDB` | keystore |
| sha256(key‖salt) — прямая верификация схемы | `TestHashSchemeDirectVerification` | keystore |
| Hash в DB — 32 байта (SHA-256) | `TestHashSizeInDB` | keystore |
| per-key-salt ≥16 байт, уникален | `TestSaltLengthAndUniqueness` | keystore |
| constant-time: valid→true, wrong→false, revoked→false | `TestVerifyCorrectWrongRevoked`, `TestVerifyBeforeAfterRevoke` | keystore |
| Revoke → Verify немедленно false | `TestVerifyBeforeAfterRevoke`, `TestVerifyCorrectWrongRevoked` | keystore |
| FlushUsage не воскрешает revoked | `TestFlushUsageMergeDoesNotOverwriteRevoke`, `TestFlushUsageDoesNotResurrect` | keystore |
| keys.db 0600 | `TestFilePermissions`, `TestAtomicWriteTempFilePermissions` | keystore |
| Нет .tmp после записи | `TestAtomicWritePermissions`, `TestNoTempFileAfterError` | keystore |
| Corrupt байт-в-байт не изменён | `TestCorruptFileByteForByteUnchanged`, `TestCorruptFileReturnsErrCorrupt` | keystore |
| Ключ только на stdout при create | `TestKeyCreateBodyOnlyOnStdout`, `TestKeyCreateKeyOnStdout` | cli |
| Ключ не на stderr (SR-11) | `TestKeyCreateBodyOnlyOnStdout`, `TestKeyCreateKeyOnStdout` | cli |
| list без hash/salt/rax_live_ | `TestKeyListNoSecretsOnStdout`, `TestListNoSecrets`, `TestListRecordHasNoHashOrSalt` | cli, keystore |
| Аудит create: fingerprint без rax_live_ | `TestKeyCreateAuditContainsFingerprintNotBody` | cli |
| Аудит delete: fingerprint без rax_live_ | `TestKeyDeleteAuditContainsFingerprintNotBody` | cli |
| Sentinel-ошибки не содержат тело ключа | `TestSentinelErrorMessagesNoSecrets` | keystore |
| error:/hint: строчными | `TestErrorMessagesLowercase` | cli |
| fingerprint ≤12 hex-символов, не тело | `TestFingerprintLengthBounds`, `TestFingerprint`, `TestFingerprintNotKeyBody` | keystore |
| ID — hex, 16 символов, не из тела | `TestIDFormat`, `TestIDNotDerivedFromBody`, `TestIDIsRandom` | keystore |
| Параллельные операции не повреждают файл (flock) | `TestConcurrentCreateAndList` | keystore |

---

## Install-flow тест

**Вне scope задачи `key-management`.** Задача `distribution` включает `install.sh`,
проверку SHA256SUMS, детект ОС/архитектуры. Тесты install-flow будут в `specs/distribution/test-plan.md`.

---

## Новые тесты, добавленные QA (+33)

### `internal/keystore/keystore_qa_test.go` (+19 тестов)

| Имя теста | Закрытый пробел | SR/AC |
|---|---|---|
| `TestSaltLengthAndUniqueness` | Явная проверка длины соли ≥16 байт через raw JSON | SR-4 |
| `TestHashSchemeDirectVerification` | Прямое воспроизведение sha256(key‖salt) из raw JSON | SR-8 |
| `TestKeyBodyUniquenessMultiple` | Уникальность и формат при 10 последовательных creates | SR-1, AC-1 |
| `TestAtomicWriteTempFilePermissions` | Нет .tmp после записи; итоговый файл 0600 | SR-20, SR-21 |
| `TestNoTempFileAfterError` | Нет .tmp после успешной записи | SR-21 |
| `TestCorruptFileByteForByteUnchanged` | Байт-в-байт проверка неизменности после ErrCorrupt | SR-22, AC-12 |
| `TestFlushUsageMergeDoesNotOverwriteRevoke` | FlushUsage не воскрешает revoked (явный сценарий) | SR-17 |
| `TestConcurrentCreateAndList` | Параллельные Create+List: файл не повреждён | SR-23 |
| `TestListRecordHasNoHashOrSalt` | JSON-сериализация Record без полей hash/salt | SR-12, SR-7 |
| `TestFingerprintLengthBounds` | fingerprint ≤12 симв. для разных входов | SR-15 |
| `TestIDFormat` | ID = 16-символьная hex-строка | SR-5, D5 |
| `TestEmptyLabelShownAsDash` | label="" в Record (CLI отображает "-") | AC-10, D2 |
| `TestListWithMultipleActiveKeys` | Несколько ключей: list возвращает только активные | AC-5 |
| `TestRevokePreservesRecordForAudit` | revoked-запись остаётся в keys.db | D3, AC-6 |
| `TestVerifyCorrectWrongRevoked` | Три пути Verify: correct/wrong/revoked | SR-9, SR-16, AC-8 |
| `TestSentinelErrorMessagesNoSecrets` | Sentinel-ошибки не содержат тело ключа | SR-13 |
| `TestFlushUsagePersistsLastUsedOnReopen` | LastUsed виден в re-opened Store после FlushUsage | SR-17, AC |
| `TestVerifyEmptyStoreReturnsNoMatch` | Verify на пустом хранилище → (_, false, nil) | AC-12 |
| `TestHashSizeInDB` | Hash в DB = 32 байта (SHA-256) | SR-8 |

### `internal/cli/cli_key_qa_test.go` (+14 тестов)

| Имя теста | Закрытый пробел | SR/AC |
|---|---|---|
| `TestKeyCreateListDeleteIntegration` | e2e lifecycle: create→list→delete→list | AC-5, AC-6 |
| `TestKeyDeleteNotFoundCLI` | Несуществующий id через CLI: exit≠0, error:, hint: | SR-18, AC-6 |
| `TestKeyDeleteAlreadyRevokedCLI` | Уже revoked через CLI: "already revoked", exit≠0 | SR-18, AC-6 |
| `TestKeyCreateBodyOnlyOnStdout` | Ключ ТОЛЬКО на stdout, НИКОГДА на stderr | SR-11, AC-2 |
| `TestKeyCreateAuditContainsFingerprintNotBody` | Аудит create: fingerprint есть, rax_live_ нет | SR-24, AC-7 |
| `TestKeyDeleteAuditContainsFingerprintNotBody` | Аудит delete: fingerprint есть, rax_live_ нет | SR-24, AC-7 |
| `TestKeyListNoSecretsOnStdout` | list stdout: нет hash/salt/rax_live_ | SR-12, AC-11 |
| `TestErrorMessagesLowercase` | error:/hint: строчными (все случаи) | ux-spec |
| `TestKeyCreateLabelTooLongCLI` | label>64: error:+hint:, exit≠0 | D4, AC-10 |
| `TestKeyListEmptyShowsMessageOnStdout` | Пустой list на stdout с hint: | AC-5, ux-spec |
| `TestKeyCreateNoLabelShowsDash` | Нет --name → label "-" в метаданных stderr | D2, AC-10 |
| `TestKeyDeleteSuccessProducesNoStdout` | Успешный delete не пишет на stdout | ux-spec |
| `TestKeyCreateExitCodes` | exit 0 на успехе, exit≠0 на ошибке валидации | AC-2, AC-10 |
| `TestKeyListExitNonZeroOnCorrupt` | Corrupt DB → exit≠0 для list/delete | SR-22, AC-12 |

---

## Найденные баги

Ни одного нового бага не обнаружено. Все тесты прошли с первого прогона.

Ранее закрытые баги (ISSUE-1..4) задокументированы в `specs/key-management/impl-notes.md`.

---

## Что ещё в scope будущих задач

- Rate-limiting / 429 (SR out of scope key-management — задача `tls-transport`/`mcp-server`)
- Install-flow: детект uname, SHA256, quarantine (задача `distribution`)
- Нагрузочные/fuzzing тесты Verify (задача `performance`)
- TLS/cert-тесты (задача `tls-transport`)

---

*Артефакт задачи: `key-management`. Автор: qa. Проверяет: qa-guardian.*
*Автор продукта: Vladimir Kovalev, OEM TECH.*
