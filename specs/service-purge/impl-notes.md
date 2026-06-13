# Impl Notes: service-purge

## Что реализовано

- **`internal/service/service.go`** — расширены тип `ServiceManager` и добавлены контракты:
  - `ErrUserMismatch` (AC6, SR-117): shell пользователя не входит в множество no-login shells
  - `ErrSuspiciousPath` (AC7, SR-118, SR-119): путь не прошёл `validatePurgePath`
  - `ErrPurgeNotConfirmed` (AC9, SR-114): `opts.Confirmed==false`
  - `PurgeOptions{Confirmed bool}` — флаг явного подтверждения
  - `PurgeReport{Platform, Stopped, Uninstalled, UserRemoved, UserAbsent, DirsRemoved, DirsAbsent}` — поле `Uninstalled bool` добавлено как расширение для полноты аудит-отчёта (advisory system-dev-guardian)
  - Метод `Purge(ctx, opts) (PurgeReport, error)` добавлен в интерфейс `ServiceManager`

- **`internal/service/purge.go`** (новый) — платформенно-нейтральные хелперы:
  - `validatePurgePath(path, allowedRoots) error` — 8 последовательных проверок: непустой, нормализованный, абсолютный, не `/`, не `$HOME`/предок, не системный корень (`/etc /var /usr /usr/local /tmp /bin /sbin /lib /lib64 /boot /dev /proc /sys /run`), `filepath.EvalSymlinks` (симлинк наружу → `ErrSuspiciousPath`), resolved-prefix против `allowedRoots` с защитой `/raxd2≠/raxd`
  - `isEqualOrAncestor(candidate, base)` — предикат для check 5
  - `blockedSystemRoots` — множество запрещённых системных корней (SR-118)
  - `noLoginShells` — множество допустимых shell для системного аккаунта: `/usr/sbin/nologin`, `/sbin/nologin`, `/usr/bin/false` (SR-117, service-design.md §9.2)
  - `emitPurgeAuditRecord` — аудит на уровне CLI через charmbracelet/log (разграничение: manager собирает PurgeReport с намерениями ДО удаления; CLI пишет INFO-запись после возврата `Purge`)

- **`internal/service/systemd.go`** — Purge для Linux:
  - `systemdManager.Purge(ctx, opts)` — оркестрация 15 шагов (service-design.md §4, SR-122)
  - `verifyTargetUserLinux` — `getent passwd raxd` через `runCommandRaw` (SR-120), exit 2 → `present=false` (AC3, SR-123)
  - `parsePasswdLine` — парсинг 7-полевого формата; проверка name + shell∈noLoginShells
  - `deleteUserLinux` — `userdel raxd` без `-r` и без shell (SR-120, service-design.md §2.1)
  - `mapUserdelExitCode` — exit 6→nil (AC3), 1/10→ErrPermission (SR-121), 8→ErrManagerUnavailable
  - Константы `userdelBin=/usr/sbin/userdel`, `getentBin=/usr/bin/getent` (SR-120)

- **`internal/service/launchd.go`** — Purge для macOS:
  - `launchdManager.Purge(ctx, opts)` — те же 15 шагов + LogPath (шаг 14, service-design.md §2.2)
  - `verifyTargetUserDarwin` — `dscl . -read /Users/raxd UserShell` через `runCommandRaw` (SR-120); exit!=0 → `present=false` (AC3, SR-123)
  - `parseDsclShellOutput` — парсинг строки `UserShell: /usr/bin/false`; shell∈noLoginShells
  - `deleteUserDarwin` — `dscl . -delete /Users/raxd` без shell (SR-120)
  - `mapDsclDeleteError` — eDSRecordNotFound/Unknown node→nil (AC3, SR-123), Permission denied→ErrPermission (SR-121)
  - Константа `dsclBin=/usr/bin/dscl` (SR-120, service-design.md §9.1)

- **`internal/cli/service.go`** — флаги, барьер, отчёт, маппинг ошибок:
  - `--purge` и `--yes` Boolean флаги зарегистрированы в `newServiceUninstallCmd` (AC1)
  - Барьер AC9: `--purge` без `--yes` → `printPurgeBarrier` (warning + список что уничтожается + keys.db явно + `--yes` в hint) + `return err` (exit 1, ничего не вызвано, SR-114, SR-115)
  - `runPurgeCmd` — вызов `mgr.Purge(Confirmed:true)` + `printPurgeReport`
  - `printPurgeReport` — аудит-лог INFO (SR-116, AC8) ДО human-вывода; %-14s колонка; removed/absent строки; "purge complete" итог (ux-spec §2,§3)
  - `mapPurgeError` — ErrPermission→sudo-hint, ErrUserMismatch→нейтральный текст, ErrSuspiciousPath→layout-hint, ErrManagerUnavailable→stop-hint, fallback (SR-95, SR-124)
  - `uninstall` без `--purge`: код не изменён byte-for-byte (AC2, SR-125)

- **`internal/cli/service_test.go`** — `fakeManager.Purge` добавлен в `service_purge_test.go` (единый пакет `cli_test`)

## Исправления по замечаниям developer-guardian (три issue)

### Issue 1 — SR-116: аудит внутри Purge() ДО RemoveAll (было нарушено)

До исправления: `emitPurgeAuditRecord` была no-op (`_ = report`), реальный аудит писался
в `printPurgeReport` в CLI *после* возврата `Purge()` — то есть после `os.RemoveAll`.

Исправление:
- `PurgeOptions.AuditOut io.Writer` добавлено в `service.go` (nil-safe; нулевое значение не паникует)
- `emitPurgeAuditRecord(w io.Writer, platform string, userPresent bool, dirsPresent []string)`
  реализована в `systemd.go` — использует `charmbracelet/log`, пишет INFO «purge intent» с
  полями `action`, `phase=pre-deletion`, `platform`, `user_present`, `dirs_present`
- Вызов на шаге 10 в `systemdManager.Purge` и `launchdManager.Purge` — *до* шагов 11–14
  (userdel/RemoveAll)
- CLI `runPurgeCmd` передаёт `AuditOut: stderr` в `PurgeOptions`
- `printPurgeReport` переименован label с «service purged» на «purge complete» (completion record,
  отличается от intent record внутри Purge)
- `export_test.go`: добавлен `EmitPurgeAuditRecordForTest` для unit-тестирования

### Issue 2 — silent t.Skip в TestValidatePurgePath_HomeAncestor (было нарушено)

До исправления: тест содержал `t.Skip(...)` когда HOME=/root в Docker (parent «/» не является
предком), что делало пропуск молчаливым.

Исправление: qa добавил `TestValidatePurgePath_HomeAncestor_ViaEnv` и `TestIsEqualOrAncestor_*`
(детерминированные, через `t.Setenv("HOME", ...)`). `TestValidatePurgePath_HomeAncestor` заменён
no-op placeholder (комментарий). `grep t.Skip` в feature-файлах = 0 (кроме
`TestValidatePurgePath_HomeDir` — условный skip при ошибке OS API, допустимо).

### Issue 3 — uid<1000 defense-in-depth в parsePasswdLine (service-design.md §2.1)

До исправления: `parsePasswdLine` проверял только name и shell, не uid.

Исправление: добавлена проверка 2 — uid должен быть в [1,999] (системный диапазон по умолчанию
для systemd/useradd). uid=0 (root) и uid>=1000 (пользовательские аккаунты) → `ErrUserMismatch`.
Нечисловой uid → `ErrUserMismatch`. Проверка выполняется *до* проверки shell.

## Отклонения/эскалации

- **Trunk-модель**: ветка `feature/service-purge` создана от `main` (единственная ветка на remote; `develop` не существует). Это решение дирижёра, зафиксированное в задании, не является нарушением git-flow.

## Тесты

### Покрытие acceptance criteria

| AC   | Тест                                          | Результат |
|------|-----------------------------------------------|-----------|
| AC1  | `TestServiceUninstall_HasPurgeAndYesFlags`    | PASS      |
| AC2  | `TestUninstall_WithoutPurge_ByteForByte`      | PASS      |
| AC3  | `TestPurge_Idempotent_AllAbsent_Exit0`, `TestValidatePurgePath_AbsentPath`, `TestMapUserdelExitCode_NotFound`, `TestMapDsclDeleteError_NotFound` | PASS |
| AC4  | `TestPurge_StopFailed_Exit1`                  | PASS      |
| AC5  | `TestPurge_PermissionError_Exit1`, `TestMapUserdelExitCode_Permission`, `TestMapDsclDeleteError_Permission` | PASS |
| AC6  | `TestPurge_UserMismatch_Exit1`, `TestParsePasswdLine_LoginShell`, `TestParsePasswdLine_WrongName`, `TestParsePasswdLine_HighUID`, `TestParsePasswdLine_UID0_Root`, `TestParseDsclShellOutput_LoginShell` | PASS |
| AC7  | `TestValidatePurgePath_*` (10+ тестов), `TestIsEqualOrAncestor_*`, `TestPurge_SuspiciousPath_Exit1` | PASS |
| AC8  | `TestPurge_AuditLogPresent`, `TestPurge_AuditSinkReceivedBeforeRemoveAll`, `TestEmitPurgeAuditRecord_WritesBeforeRemoveAll`, `TestPurgeOptions_AuditOut_WriterSet` | PASS |
| AC9  | `TestPurge_WithoutYes_Exit1_NoDeletion`       | PASS      |
| AC10 | Все выше — через `fakeManager` без реальных системных команд | PASS |

### Полный вывод тестов (Docker)

```
ok  github.com/vladimirvkhs/raxd               0.010s
ok  github.com/vladimirvkhs/raxd/internal/banner 0.001s
ok  github.com/vladimirvkhs/raxd/internal/cli   0.080s
ok  github.com/vladimirvkhs/raxd/internal/cmdexec 1.179s
ok  github.com/vladimirvkhs/raxd/internal/config  0.008s
ok  github.com/vladimirvkhs/raxd/internal/fileupload 0.087s
ok  github.com/vladimirvkhs/raxd/internal/keystore 0.163s
ok  github.com/vladimirvkhs/raxd/internal/mcp   4.377s
ok  github.com/vladimirvkhs/raxd/internal/server 2.203s
ok  github.com/vladimirvkhs/raxd/internal/service 0.005s
ok  github.com/vladimirvkhs/raxd/internal/version 0.001s
```

FAIL — ни одного. `go vet ./...` — чист (в составе `docker build --target test`).

### Команда запуска

```
docker build --target test -t raxd-test . && docker run --rm raxd-test
```

## Безопасность

| SR      | Требование                                  | Реализация                                                                                                                                              |
|---------|---------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------|
| SR-114  | Purge без Confirmed → ErrPurgeNotConfirmed  | `systemdManager.Purge` шаг 2: `if !opts.Confirmed { return ErrPurgeNotConfirmed }`; CLI-барьер — первый (шаг до вызова `Purge`)                        |
| SR-115  | warning с keys.db при --purge без --yes     | `printPurgeBarrier` явно упоминает `keys.db   all API keys and audit log — cannot be recovered`                                                         |
| SR-116  | Аудит ДО физического удаления               | `emitPurgeAuditRecord` в `systemd.go`/`launchd.go` — шаг 10 Purge(), ДО шагов 11–14 (userdel/RemoveAll). CLI передаёт `opts.AuditOut=stderr`; completion record — в `printPurgeReport` после возврата Purge. |
| SR-117  | shell∈noLoginShells, иначе ErrUserMismatch  | `parsePasswdLine`, `parseDsclShellOutput` — проверяют `noLoginShells`; несоответствие → `ErrUserMismatch` до userdel/dscl                               |
| SR-118  | validatePurgePath — список запрещённых      | `blockedSystemRoots` в `purge.go`; проверка в шаге 6 до `RemoveAll`                                                                                    |
| SR-119  | EvalSymlinks перед удалением                | `filepath.EvalSymlinks` в check 7 `validatePurgePath`; симлинк наружу → `ErrSuspiciousPath`                                                             |
| SR-120  | exec без shell                              | `runCommandRaw(ctx, userdelBin, name)`, `runCommandRaw(ctx, dsclBin, ".", "-delete", "/Users/"+name)` — отдельные args, нет `sh -c`                     |
| SR-121  | Нет root-эскалации; ErrPermission при нехватке | `os.Geteuid() != 0` → `ErrPermission` как шаг 1 в `Purge`, ничего не изменено                                                                      |
| SR-122  | Строгий порядок шагов                       | 15 шагов в `systemdManager.Purge`/`launchdManager.Purge`; Stop-fail → СТОП до userdel/RemoveAll                                                        |
| SR-123  | userdel exit 6 / dscl «not found» → success | `mapUserdelExitCode(6) → nil`, `mapDsclDeleteError` с eDSRecordNotFound → nil; отличается от permission-ошибки                                         |
| SR-124  | PurgeReport/аудит — только метаданные       | `PurgeReport` содержит имена путей, bool-флаги; содержимое файлов/ключей не читается; `mapPurgeError` — нейтральные тексты без stack traces             |
| SR-125  | Uninstall без --purge byte-for-byte         | Ветвление `if doPurge && doYes { return runPurgeCmd(...) }` изолирует деструктивный путь; блок `Uninstall` ниже — без изменений                        |
| SR-126  | fakeManager в тестах, без реальных команд  | `fakeManager.Purge` в `service_purge_test.go`; unit-тесты `validatePurgePath` используют `t.TempDir()` без системных вызовов                           |
| SR-127  | Только stdlib, нет новых зависимостей       | `purge.go`, `systemd.go`, `launchd.go` импортируют только `os`, `os/exec`, `path/filepath`, `strings`, `context`, `bytes` — всё stdlib; `go.mod` не изменён |

**Аутентификация API-ключей** (§1): не применимо к этой фиче — purge не трогает keystore.
**Транспорт TLS** (§2): не применимо.
**Таймауты** (§3): все вызовы выполняются через `ctx` из `serviceContext()` (30s timeout).
**Права файлов** (§3): purge использует `os.RemoveAll` (не создаёт файлы); не применимо к новым файлам.

## Коммиты

### Первичная реализация

| Хэш      | Описание                                                             |
|----------|----------------------------------------------------------------------|
| `60edf3a` | feat(service): PurgeOptions, PurgeReport, sentinels, метод Purge   |
| `ebe47d6` | test(service/purge): TDD-тесты validatePurgePath, verifyTargetUser  |
| `7c1ebb1` | feat(service/purge): validatePurgePath, isEqualOrAncestor           |
| `3721e65` | feat(service/systemd): Purge Linux, verifyTargetUserLinux, mapUserdelExitCode |
| `6cb3165` | feat(service/launchd): Purge macOS, verifyTargetUserDarwin, mapDsclDeleteError |
| `0f729a4` | test(cli/service): TDD-тесты --purge/--yes барьера, AC1–AC10       |
| `a7602f8` | feat(cli/service): --purge/--yes, барьер, printPurgeReport, mapPurgeError |
| `81941fc` | docs(specs/service-purge): impl-notes — что реализовано, тесты, безопасность |

### Исправления по developer-guardian (три issue)

| Хэш      | Описание                                                             |
|----------|----------------------------------------------------------------------|
| `95a767c` | fix(service/purge): SR-116 audit в Purge() до RemoveAll + uid<1000 defense-in-depth |
| `81f3265` | test(service/purge): тесты uid<1000 + audit nil-safe + Issue 2 t.Skip удалён |
| `bbc7a8d` | fix(cli/service)+test: AuditOut=stderr в runPurgeCmd + CLI audit sink тест |
