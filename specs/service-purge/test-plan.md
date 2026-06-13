# Test Plan: service-purge — `raxd service uninstall --purge`

## Стратегия

- **Unit (логика модулей)** — `internal/service/purge_test.go`: проверяет `validatePurgePath`,
  `isEqualOrAncestor`, `parsePasswdLine`, `parseDsclShellOutput`, `mapUserdelExitCode`,
  `mapDsclDeleteError`, типы `PurgeOptions`/`PurgeReport`, новые sentinel-ошибки.
  Без реальных `userdel`/`dscl` — через exported-helpers (export_test.go). SR-126.
- **CLI-интеграция (fake-manager)** — `internal/cli/service_purge_test.go`: проверяет
  весь CLI-путь через `fakeManager.Purge`, флаги `--purge`/`--yes`, барьер необратимости,
  маппинг ошибок, вывод отчёта, аудит-лог. Ни один системный вызов не совершается. SR-126.
- **E2E** — вне scope этого test-plan: реальный `userdel`/`dscl` требует root на Linux/macOS.
  Принято решение не запускать E2E в Docker (SECURITY-BASELINE §6 запрещает реальные
  системные команды на хосте; в контейнере нет полноценного systemd/launchd).

**Правило запуска:** все прогоны только в Docker (SECURITY-BASELINE §6):
```
docker build --target test -t raxd-test .
docker run --rm raxd-test go test -mod=vendor -count=1 \
    ./internal/service/... ./internal/cli/...
```

---

## Матрица AC → тест

| AC | Краткое описание | Уровень | Тест (файл :: имя) | Статус |
|---|---|---|---|---|
| AC1 | `--purge --yes` выполняет uninstall И удаляет пользователя/каталоги (обе платформы) | CLI | `service_purge_test.go::TestPurge_WithYes_Success_Exit0` | green |
| AC1 | Флаги `--purge` и `--yes` зарегистрированы в `uninstall` | CLI | `service_purge_test.go::TestServiceUninstall_HasPurgeAndYesFlags` | green |
| AC2 | `uninstall` без `--purge` byte-for-byte: показывает "kept", не вызывает Purge | CLI | `service_purge_test.go::TestUninstall_WithoutPurge_ByteForByte` | green |
| AC3 | Идемпотентность: user+dirs absent → exit 0, показывает "absent" / "purge complete" | CLI | `service_purge_test.go::TestPurge_Idempotent_AllAbsent_Exit0` | green |
| AC3 | Несуществующий путь → `validatePurgePath` возвращает nil (idempotent AC3) | Unit | `purge_test.go::TestValidatePurgePath_AbsentPath` | green |
| AC3 | Частичная идемпотентность: PurgeReport корректно кодирует mixed removed/absent | Unit | `purge_test.go::TestPurgeReport_PartialIdempotency` | green |
| AC3 | `mapUserdelExitCode` exit 6 → nil (user not found = idempotent) | Unit | `purge_test.go::TestMapUserdelExitCode_NotFound` | green |
| AC3 | `mapDsclDeleteError` "eDSRecordNotFound" и варианты → nil | Unit | `purge_test.go::TestMapDsclDeleteError_NotFound` | green |
| AC4 | Stop-fail → exit != 0, "error:" в выводе, Purge не производит удалений | CLI | `service_purge_test.go::TestPurge_StopFailed_Exit1` | green |
| AC5 | `ErrPermission` → exit != 0, "error:", hint с sudo, без "purge complete" | CLI | `service_purge_test.go::TestPurge_PermissionError_Exit1` | green |
| AC5 | `mapUserdelExitCode` exit 1 → ErrPermission | Unit | `purge_test.go::TestMapUserdelExitCode_Permission` | green |
| AC5 | `mapUserdelExitCode` exit 10 → ErrPermission | Unit | `purge_test.go::TestMapUserdelExitCode_Permission10` | green |
| AC5 | `mapDsclDeleteError` "Permission denied" и варианты → ErrPermission | Unit | `purge_test.go::TestMapDsclDeleteError_Permission` | green |
| AC6 | `verifyTargetUser`: login-shell → ErrUserMismatch (Linux parsePasswdLine) | Unit | `purge_test.go::TestParsePasswdLine_LoginShell` | green |
| AC6 | `verifyTargetUser`: все допустимые nologin-шеллы → ok | Unit | `purge_test.go::TestParsePasswdLine_AllValidNologinShells` | green |
| AC6 | `verifyTargetUser`: несколько login-shell вариантов → ErrUserMismatch | Unit | `purge_test.go::TestParsePasswdLine_LoginShellVariants` | green |
| AC6 | `verifyTargetUser`: несовпадение имени → ErrUserMismatch (Linux) | Unit | `purge_test.go::TestParsePasswdLine_WrongName` | green |
| AC6 | `verifyTargetUser`: login-shell → ErrUserMismatch (macOS parseDsclShellOutput) | Unit | `purge_test.go::TestParseDsclShellOutput_LoginShell` | green |
| AC6 | `verifyTargetUser`: nologin → ok (macOS parseDsclShellOutput) | Unit | `purge_test.go::TestParseDsclShellOutput_ValidNologin` | green |
| AC6 | CLI: `ErrUserMismatch` → exit != 0, нейтральное "error:", без shell-деталей | CLI | `service_purge_test.go::TestPurge_UserMismatch_Exit1` | green |
| AC7 | `validatePurgePath`: пустой путь → ErrSuspiciousPath | Unit | `purge_test.go::TestValidatePurgePath_EmptyPath` | green |
| AC7 | `validatePurgePath`: `/` → ErrSuspiciousPath | Unit | `purge_test.go::TestValidatePurgePath_Root` | green |
| AC7 | `validatePurgePath`: `$HOME` → ErrSuspiciousPath (через os.UserHomeDir, в Docker HOME=/root) | Unit | `purge_test.go::TestValidatePurgePath_HomeDir` | green |
| AC7 | `validatePurgePath`: родитель `$HOME` → ErrSuspiciousPath (детерминированный, через env-инъекцию) | Unit | `purge_test.go::TestValidatePurgePath_HomeAncestor_ViaEnv` | green |
| AC7 | `validatePurgePath`: точный `$HOME` → ErrSuspiciousPath (детерминированный, через env-инъекцию) | Unit | `purge_test.go::TestValidatePurgePath_HomeDir_ViaEnv` | green |
| AC7 | `isEqualOrAncestor`: candidate==base → true | Unit | `purge_test.go::TestIsEqualOrAncestor_Equal` | green |
| AC7 | `isEqualOrAncestor`: ancestor → true (несколько случаев) | Unit | `purge_test.go::TestIsEqualOrAncestor_AncestorOfBase` | green |
| AC7 | `isEqualOrAncestor`: несвязанные/похожие пути → false | Unit | `purge_test.go::TestIsEqualOrAncestor_NotAncestor` | green |
| AC7 | `validatePurgePath`: системные корни → ErrSuspiciousPath (14 путей) | Unit | `purge_test.go::TestValidatePurgePath_SystemRoots` | green |
| AC7 | `validatePurgePath`: `/var/lib/raxd2` ≠ `/var/lib/raxd` (prefix-collision) | Unit | `purge_test.go::TestValidatePurgePath_SimilarPrefixNotAllowed` | green |
| AC7 | `validatePurgePath`: симлинк наружу раскладки → ErrSuspiciousPath (SR-119) | Unit | `purge_test.go::TestValidatePurgePath_SymlinkOutside` | green |
| AC7 | `validatePurgePath`: относительный путь → ErrSuspiciousPath | Unit | `purge_test.go::TestValidatePurgePath_RelativePath` | green |
| AC7 | `validatePurgePath`: корректный путь внутри allowedRoots → nil | Unit | `purge_test.go::TestValidatePurgePath_ValidPath` | green |
| AC7 | CLI: `ErrSuspiciousPath` → exit != 0, "error:" | CLI | `service_purge_test.go::TestPurge_SuspiciousPath_Exit1` | green |
| AC8 | Аудит-лог присутствует в stderr при успешном purge (содержит "action=purge") | CLI | `service_purge_test.go::TestPurge_AuditLogPresent` | green |
| AC8 | Порядок: аудит-запись до удаления — обеспечен архитектурно (`emitPurgeAuditRecord` вызывается до `deleteUserLinux`/`RemoveAll` в systemd.go шаги 10→11→12) | Код | grep systemd.go: Step 10 до Step 11 | green |
| AC9 | `--purge` без `--yes` → exit != 0, предупреждение с "irreversible" и "keys.db", hint с "--yes" | CLI | `service_purge_test.go::TestPurge_WithoutYes_Exit1_NoDeletion` | green |
| AC9 | `ErrPurgeNotConfirmed` — отдельный sentinel, не равен другим | Unit | `purge_test.go::TestNewSentinels` + `TestPurgeOptions_Unconfirmed_IsSentinel` | green |
| AC9 | `PurgeOptions.Confirmed=false` — zero value, требует явного `true` | Unit | `purge_test.go::TestPurgeOptionsType` | green |
| AC10 | Все ветки покрыты через fakeManager без реальных системных команд | CLI | `service_purge_test.go` (весь файл) | green |
| AC10 | Типы `PurgeOptions`, `PurgeReport`, все поля | Unit | `purge_test.go::TestPurgeOptionsType`, `TestPurgeReportFields` | green |
| AC10 | Новые sentinels существуют и различимы | Unit | `purge_test.go::TestNewSentinels` | green |

---

## Edge cases

| Кейс | Тест | Файл |
|---|---|---|
| Пустой путь | `TestValidatePurgePath_EmptyPath` | `purge_test.go` |
| Относительный путь (не абсолютный) | `TestValidatePurgePath_RelativePath` | `purge_test.go` |
| Путь с prefix-collision (`/var/lib/raxd2` vs `/var/lib/raxd`) | `TestValidatePurgePath_SimilarPrefixNotAllowed` | `purge_test.go` |
| Несуществующий путь (уже удалён — повторный purge) | `TestValidatePurgePath_AbsentPath` | `purge_test.go` |
| Симлинк выходит за пределы раскладки (SR-119) | `TestValidatePurgePath_SymlinkOutside` | `purge_test.go` |
| `$HOME` через env-инъекцию (Docker HOME=/root) | `TestValidatePurgePath_HomeAncestor_ViaEnv`, `TestValidatePurgePath_HomeDir_ViaEnv` | `purge_test.go` |
| Логин-шелл пользователя — несколько вариантов | `TestParsePasswdLine_LoginShellVariants` | `purge_test.go` |
| userdel exit 8 (user logged in) — не idempotent, не ErrPermission | `TestMapUserdelExitCode_UserLoggedIn` | `purge_test.go` |
| Смешанный PurgeReport (одно удалено, другое absent) | `TestPurgeReport_PartialIdempotency` | `purge_test.go` |
| Вывод без секретов при успехе | `TestPurge_NoSecretsInOutput` | `service_purge_test.go` |
| `dscl` parser: name-параметр игнорируется (документационный тест) | `TestParseDsclShellOutput_ValidShell_NameIgnored` | `purge_test.go` |

---

## Security-тесты (SR-114…SR-127)

| SR | Требование | Тест | Файл | Статус |
|---|---|---|---|---|
| SR-114 | `--purge` без `--yes` → деструктивные вызовы не происходят | `TestPurge_WithoutYes_Exit1_NoDeletion` (fakeManager не получает Purge) | `service_purge_test.go` | green |
| SR-115 | Предупреждение о необратимости упоминает `keys.db` и `--yes` | `TestPurge_WithoutYes_Exit1_NoDeletion` (assert на "keys.db", "--yes") | `service_purge_test.go` | green |
| SR-116 | Аудит-запись до физического удаления (порядок шагов 10→11 в systemd.go) | `TestPurge_AuditLogPresent` + code audit | `service_purge_test.go` | green |
| SR-117 | `verifyTargetUser`: nologin→ok, login-shell→ErrUserMismatch, мисматч имени→ErrUserMismatch | `TestParsePasswdLine_*`, `TestParseDsclShellOutput_*` | `purge_test.go` | green |
| SR-118 | `validatePurgePath` отвергает пустой, `/`, `$HOME`, системные корни | `TestValidatePurgePath_*` | `purge_test.go` | green |
| SR-119 | `filepath.EvalSymlinks` + проверка раскладки; симлинк наружу → ErrSuspiciousPath | `TestValidatePurgePath_SymlinkOutside` | `purge_test.go` | green |
| SR-120 | Отсутствие `sh -c` и конкатенации ввода: `userdelBin`/`dsclBin` — абсолютные константы, аргументы раздельные | code grep: нет `sh -c`, нет конкатенации в purge-path | systemd.go, launchd.go | green |
| SR-121 | `ErrPermission` → exit != 0, hint sudo, без деструктивных действий | `TestPurge_PermissionError_Exit1` | `service_purge_test.go` | green |
| SR-122 | Stop-fail → удаление не выполняется (strict order) | `TestPurge_StopFailed_Exit1` | `service_purge_test.go` | green |
| SR-123 | `mapUserdelExitCode`: exit 6 → nil (idempotent), exit 1/10 → ErrPermission, exit 8 → error (не idempotent) | `TestMapUserdelExitCode_*` | `purge_test.go` | green |
| SR-124 | В выводе нет `rax_live_`, PEM-маркеров, паники | `TestPurge_NoSecretsInOutput` | `service_purge_test.go` | green |
| SR-125 | `uninstall` без `--purge` не вызывает Purge, вывод не изменён ("kept" присутствует) | `TestUninstall_WithoutPurge_ByteForByte` | `service_purge_test.go` | green |
| SR-126 | Поведение покрыто через fakeManager; реальные userdel/dscl/rm не вызываются | весь `service_purge_test.go` | `service_purge_test.go` | green |
| SR-127 | Только stdlib; сборка и тесты в Docker | `go.mod` без новых модулей; `Dockerfile` test-stage | Dockerfile, go.mod | green |

---

## Статус $HOME-ancestor gap (специальное внимание)

**Проблема:** `TestValidatePurgePath_HomeAncestor` в Docker пропускается (SKIP), потому что
в контейнере `HOME=/root` и `filepath.Dir("/root") == "/"` — тест справедливо вызывает `t.Skip`.

**Статус до этого test-plan:** инвариант логики `isEqualOrAncestor` не имел детерминированного
теста в Docker-среде.

**Решение (добавлено в рамках этого test-plan):**
1. Экспортирован `IsEqualOrAncestorForTest` в `internal/service/export_test.go`.
2. Добавлены три детерминированных теста через прямой вызов helper:
   - `TestIsEqualOrAncestor_Equal` — candidate==base → true
   - `TestIsEqualOrAncestor_AncestorOfBase` — ancestor → true (несколько случаев)
   - `TestIsEqualOrAncestor_NotAncestor` — несвязанные/похожие → false
3. Добавлены два теста через env-инъекцию `t.Setenv("HOME", "/opt/qa-home-guard")`:
   - `TestValidatePurgePath_HomeAncestor_ViaEnv` — родитель `/opt` отвергается
   - `TestValidatePurgePath_HomeDir_ViaEnv` — точный `$HOME` отвергается

Оригинальный `TestValidatePurgePath_HomeAncestor` (SKIP в Docker) оставлен без изменений —
его skip-условие корректно и честно: в среде, где HOME=/root, тест физически нетестируем.
Инвариант теперь полностью покрыт детерминированными тестами выше.

---

## Как запускать

### Фокусный прогон (purge-специфично)
```sh
docker build --target test -t raxd-test .
docker run --rm raxd-test \
  go test -mod=vendor -v -count=1 \
    ./internal/service/... ./internal/cli/...
```

### Полный прогон (все пакеты)
```sh
docker run --rm raxd-test \
  sh -c "go vet ./... && go test -mod=vendor -count=1 ./..."
```

### Прогон только новых тестов (по имени)
```sh
docker run --rm raxd-test \
  go test -mod=vendor -v -count=1 \
    -run "TestIsEqualOrAncestor|TestValidatePurgePath_HomeAncestor_ViaEnv|TestValidatePurgePath_HomeDir_ViaEnv|TestPurgeReport_Partial|TestPurgeOptions_Unconfirmed|TestParsePasswdLine_All|TestParsePasswdLine_LoginShellVariants|TestMapUserdelExitCode_UserLoggedIn|TestParseDsclShellOutput_ValidShell_NameIgnored" \
    ./internal/service/...
```

---

## Итоговый вердикт по покрытию

| Метрика | Значение |
|---|---|
| Acceptance criteria (AC1–AC10) | 10 из 10 покрыты |
| Security requirements (SR-114–SR-127) | 14 из 14 покрыты |
| Продуктовые баги найдены | 0 |
| t.Skip без детерминированной замены | 0 (HomeAncestor-gap закрыт через ViaEnv-тесты) |
| Тесты, ослабленные ради зелёного | 0 |
| Тесты, прогнанные на хосте (не в Docker) | 0 |
| Docker-прогон `./internal/service/... ./internal/cli/...` | PASS (1 SKIP — корректный) |
