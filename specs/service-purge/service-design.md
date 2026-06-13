# Service Design: service-purge — `raxd service uninstall --purge`

> Документ разработан по шаблону `.claude/agents/system-dev/templates/service-design.template.md`.
> Источники истины: `plan.md`, `security-requirements.md` (SR-114…SR-127), `spec.md` (AC1–AC10),
> `.claude/reference/SECURITY-BASELINE.ru.md` (§3,§4,§6).
> Существующий код: `internal/service/{service,systemd,launchd,exec,templates}.go`.

---

## 1. Что именно делает этот дизайн

Метод `Purge` (выбран architect в `plan.md`) — необратимое полное удаление присутствия raxd:
системный пользователь ОС + каталоги данных/конфига. Дизайн специфицирует точные OS-команды,
инварианты проверки пользователя/пути, маппинг кодов завершения и порядок шагов с гарантией
«без частичного состояния» на Linux и macOS.

---

## 2. Механизм per-OS

### 2.1 Linux (systemd) — `systemdManager.Purge`

**Проверка пользователя (`verifyTargetUser`) — через `getent passwd`**

```
/usr/bin/getent passwd raxd
```

Вызов: `runCommandRaw(ctx, "/usr/bin/getent", "passwd", "raxd")`

- Вывод: одна строка `raxd:x:UID:GID:comment:home:shell` или пустой вывод при отсутствии пользователя.
- Пустой вывод (exit 0, пустой stdout) → `present=false, err=nil` (идемпотентность, AC3, SR-123).
- `getent` exit 2 → «не найден» → `present=false, err=nil` (тот же случай, SR-123).
- Ошибка парсинга или exit != 0,2 → `err` с нейтральным сообщением.

**Парсинг строки passwd для `verifyTargetUser`:**

Поля разделены `:`. Порядок: `name:password:uid:gid:gecos:home:shell` (7 полей).

Проверки:
1. Поле 0 (имя) == `cfg.User` (обычно `"raxd"`).
2. Поле 2 (uid) — числовое значение. Системный аккаунт: uid < 1000 (диапазон systemd-default:
   uid ∈ [1,999]). Это не единственная проверка — основная защита через поле shell.
3. Поле 6 (shell) ∈ `{"/usr/sbin/nologin", "/sbin/nologin", "/usr/bin/false"}`.
   Несоответствие → `ErrUserMismatch` (SR-117).

Если поле 0 совпадает, но shell не входит в допустимое множество — `ErrUserMismatch`, НИЧЕГО не
удаляется (AC6, SR-117).

**Удаление пользователя — `userdel`**

```
/usr/sbin/userdel raxd
```

Вызов: `runCommandRaw(ctx, "/usr/sbin/userdel", "raxd")`

Флаги: НИ `-r` (remove home), НИ `--force` — голый `userdel <username>`.

Обоснование отказа от `-r`: flag `-r` удаляет `$HOME` пользователя и почтовый ящик. Каталог
`StateDir` (`/var/lib/raxd`) задаётся в unit через `StateDirectory=raxd` — он НЕ является
`$HOME` пользователя в реестре (home в `/etc/passwd` для системного аккаунта без-login часто
`/` или `/nonexistent`). Удаление каталогов данных выполняется ОТДЕЛЬНО через `os.RemoveAll`
с предварительной проверкой `validatePurgePath`, что даёт полный контроль над тем, ЧТО
именно удаляется. Использование `-r` внесло бы неопределённость: что именно systemd считает
home-директорией системного пользователя и не удалит ли что-то лишнее.

**Маппинг exit-кодов `userdel`:**

| Exit-код | Значение                               | Действие                                           |
|----------|----------------------------------------|----------------------------------------------------|
| 0        | Пользователь удалён успешно            | `UserRemoved=true`, продолжить                     |
| 6        | Пользователь не существует             | `UserAbsent=true`, НЕ ошибка (AC3, SR-123)         |
| 8        | Пользователь залогинен прямо сейчас    | `error` с нейтральным сообщением                   |
| 10       | Нельзя обновить group-файл             | `ErrPermission` (SR-123)                           |
| 12       | Нельзя удалить home-dir (если `-r`)    | не применимо (мы не используем `-r`)               |
| 1        | Нет разрешения / прочая ошибка         | `ErrPermission` (SR-121, SR-123)                   |
| иное     | Неизвестная ошибка                     | `error` с нейтральным сообщением                   |

Принцип: exit 6 («нет такого пользователя») — это идемпотентный успех, а не ошибка. Все
остальные ненулевые коды НЕ приравниваются к «уже удалено» (SR-123).

**Удаление каталогов (Linux):**

Каталоги из `DefaultConfigForGOOS("linux")`:
- `StateDir = /var/lib/raxd`
- `ConfigDir = /etc/raxd`

Удаление через `os.RemoveAll(path)` (stdlib, без shell, без внешних команд).
Порядок: сначала `StateDir` (содержит `keys.db`), затем `ConfigDir`.

### 2.2 macOS (launchd) — `launchdManager.Purge`

**Проверка пользователя (`verifyTargetUser`) — через `dscl`**

Шаг 1 — проверка существования и чтение UserShell:

```
/usr/bin/dscl . -read /Users/raxd UserShell
```

Вызов: `runCommandRaw(ctx, "/usr/bin/dscl", ".", "-read", "/Users/raxd", "UserShell")`

- Успех (exit 0): вывод содержит строку вида `UserShell: /usr/bin/false`. Парсим значение.
- Exit != 0 (DSStatus eDSUnknownNodeName или eDSRecordNotFound, типично exit 56 или 1):
  пользователь отсутствует → `present=false, err=nil` (AC3).

**Парсинг вывода `dscl . -read`:**

Формат: `UserShell: /usr/bin/false` (ключ, двоеточие, пробел, значение).
`strings.TrimPrefix(line, "UserShell: ")` после поиска строки, начинающейся с `"UserShell:"`.

Проверки для `verifyTargetUser` на macOS:
1. Пользователь существует (exit 0).
2. Значение `UserShell` ∈ `{"/usr/sbin/nologin", "/sbin/nologin", "/usr/bin/false"}`.
   Несоответствие → `ErrUserMismatch` (SR-117).

**Удаление пользователя — `dscl . -delete`**

```
/usr/bin/dscl . -delete /Users/raxd
```

Вызов: `runCommandRaw(ctx, "/usr/bin/dscl", ".", "-delete", "/Users/raxd")`

Маппинг exit-кодов `dscl . -delete`:

| Поведение                                       | Действие                              |
|-------------------------------------------------|---------------------------------------|
| Exit 0                                          | `UserRemoved=true`, продолжить        |
| Exit != 0, stderr содержит «eDSRecordNotFound» или «Unknown node» | `UserAbsent=true`, не ошибка (AC3, SR-123) |
| Exit != 0, stderr содержит «Permission denied» / «Operation not permitted» | `ErrPermission` (SR-121, SR-123) |
| Exit != 0, иное                                 | error с нейтральным сообщением        |

Важно: при проверке exit-кода dscl сырой stderr ЧИТАЕТСЯ только для маппинга на sentinel,
но НЕ пробрасывается в пользовательский вывод (SR-124, наследует SR-95).

Для определения «user not found» vs «permission denied» используется маппинг через распознавание
паттернов в stderr (без прямого прокидывания):

```go
// Внутренняя функция (не публичный API):
func mapDsclDeleteError(stderr string) error {
    switch {
    case strings.Contains(stderr, "eDSRecordNotFound"),
         strings.Contains(stderr, "Unknown node"),
         strings.Contains(stderr, "No such record"):
        return nil // user absent — идемпотентно, AC3
    case strings.Contains(stderr, "Permission denied"),
         strings.Contains(stderr, "Operation not permitted"),
         strings.Contains(stderr, "eDSPermissionError"):
        return wrapErr(ErrPermission, "dscl delete failed: insufficient privileges")
    default:
        return wrapErr(ErrManagerUnavailable, "dscl delete failed")
    }
}
```

Нейтральное сообщение (без содержимого stderr) — в `wrapErr`.

**Удаление каталогов (macOS):**

Каталоги из `DefaultConfigForGOOS("darwin")`:
- `StateDir = /usr/local/var/raxd`
- `ConfigDir = /usr/local/etc/raxd`
- `LogPath = /usr/local/var/log/raxd`

Удаление через `os.RemoveAll(path)`.
Порядок: `StateDir` → `ConfigDir` → `LogPath`.

Примечание по LogPath: на macOS `launchd.createDirs()` создаёт `cfg.LogPath` при Install.
При Purge его тоже нужно удалить, чтобы не оставить `raxd.log`. Это единственная платформа,
где LogPath входит в scope Purge (на Linux логи хранит journald, физического каталога нет).

---

## 3. Инвариант `validatePurgePath`

Функция располагается в `internal/service/purge.go` (платформенно-нейтральная).

**Сигнатура:**

```go
func validatePurgePath(path string, allowedRoots []string) error
```

`allowedRoots` — список префиксов, внутри которых путь обязан остаться после EvalSymlinks.
Передаётся платформенным вызывающим: на Linux `["/var/lib/raxd", "/etc/raxd"]`, на macOS
`["/usr/local/var/raxd", "/usr/local/etc/raxd", "/usr/local/var/log/raxd"]`.

**Порядок проверок (все выполняются до `os.RemoveAll`):**

1. Путь непустой → иначе `ErrSuspiciousPath`.
2. `filepath.Clean(path) == path` (нормализованный, без `..`) → иначе `ErrSuspiciousPath`.
3. `filepath.IsAbs(path)` → иначе `ErrSuspiciousPath`.
4. Путь != `"/"` → иначе `ErrSuspiciousPath`.
5. Путь не является `os.UserHomeDir()` и не является его предком:
   `!isEqualOrAncestor(path, homeDir)` → иначе `ErrSuspiciousPath`.
6. Путь не является системным корнем:
   запрещённые значения `{"/etc", "/var", "/usr", "/usr/local", "/tmp", "/bin", "/sbin", "/lib",
   "/lib64", "/boot", "/dev", "/proc", "/sys", "/run"}` → иначе `ErrSuspiciousPath`.
7. `resolved, err := filepath.EvalSymlinks(path)`: если path не существует (os.IsNotExist),
   это допустимо (уже удалён при идемпотентном повторе) → пропустить проверку 8.
   Иные ошибки EvalSymlinks → `ErrSuspiciousPath`.
8. `resolved` обязан иметь один из `allowedRoots` как точный prefix:
   `strings.HasPrefix(resolved+"/", root+"/")` (с защитой от ложных совпадений: `/var/lib/raxd2`
   не является prefix `/var/lib/raxd`) → иначе `ErrSuspiciousPath`.

**Вспомогательная функция:**

```go
func isEqualOrAncestor(candidate, base string) bool {
    // returns true if candidate == base OR base starts with candidate+"/"
    if candidate == base {
        return true
    }
    return strings.HasPrefix(base, candidate+"/")
}
```

**При любом нарушении — `ErrSuspiciousPath`, НИЧЕГО не удаляется (AC7, SR-118, SR-119).**

---

## 4. Порядок шагов `Purge` и поведение при сбое

Порядок обязателен (SR-122). Вся логика оркестрации — в `internal/service/purge.go`.

```
1. Privilege-check: os.Geteuid() == 0
   Сбой → ErrPermission, СТОП, ничего не удалено (AC5, SR-121)

2. Confirmed check: opts.Confirmed == true
   Сбой → ErrPurgeNotConfirmed, СТОП (дублирующая защита, основная — в CLI, SR-114)

3. Status(): проверить, активен ли сервис
   Ошибка Status → не критично, считать как «неизвестно, безопаснее остановить»

4. Stop() — если сервис активен (или статус неизвестен)
   Сбой Stop → error, СТОП (AC4, SR-122; удаление пользователя/каталогов НЕ выполняется)

5. Uninstall() — снятие unit/plist
   ErrNotInstalled → игнорировать (идемпотентно, AC3)
   Иная ошибка → error, СТОП (SR-122)

6. validatePurgePath(StateDir, allowedRoots)
   Сбой → ErrSuspiciousPath, СТОП (AC7, SR-118, SR-119)

7. validatePurgePath(ConfigDir, allowedRoots)
   Сбой → ErrSuspiciousPath, СТОП

8. (macOS only) validatePurgePath(LogPath, allowedRoots)
   Сбой → ErrSuspiciousPath, СТОП

9. verifyTargetUser(ctx, cfg.User)
   present=false → UserAbsent=true, пропустить шаги 10 (AC3, SR-123)
   ErrUserMismatch → СТОП (AC6, SR-117)
   иная ошибка → СТОП

10. [АУДИТ] Эмитировать аудит-запись с PurgeReport (что БУДЕТ удалено / что уже отсутствует)
    ДО физического удаления каталогов (AC8, SR-116)

11. Удалить пользователя (userdel/dscl . -delete)
    exit 0 → UserRemoved=true
    «not found» код → UserAbsent=true (AC3)
    ErrPermission → СТОП (SR-121)
    иное → СТОП

12. os.RemoveAll(StateDir)
    os.IsNotExist → DirsAbsent += StateDir (AC3)
    иная ошибка → СТОП (каталоги дальше НЕ удаляются)

13. os.RemoveAll(ConfigDir)
    os.IsNotExist → DirsAbsent += ConfigDir (AC3)
    иная ошибка → СТОП

14. (macOS only) os.RemoveAll(LogPath)
    os.IsNotExist → DirsAbsent += LogPath (AC3)
    иная ошибка → СТОП

15. Вернуть PurgeReport, nil
```

**Принцип «без частичного состояния»:** Все проверки (шаги 1–9) выполняются ДО любого
деструктивного действия. Если шаг 4 (Stop) не удался — шаги 11–14 не запускаются.
Если шаг 11 (userdel) завершился ошибкой — шаги 12–14 не выполняются.

Единственное допустимое «частичное» состояние: если `os.RemoveAll(StateDir)` (шаг 12) прошёл,
а `os.RemoveAll(ConfigDir)` (шаг 13) упал — пользователь уже удалён. Это неустранимо без
транзакций FS. Данное состояние фиксируется в частично заполненном `PurgeReport` как ошибка,
а при повторном запуске `--purge --yes` идемпотентность (UserAbsent, DirsAbsent) гарантирует
корректное завершение.

---

## 5. `PurgeReport` и аудит

### 5.1 Структура

```go
// PurgeReport (plan.md §Contracts) — заполняется в ходе Purge.
// Поля фиксируются ДО физического удаления для аудита (AC8, SR-116).
type PurgeReport struct {
    Platform     string   // "linux" / "darwin"
    Stopped      bool     // сервис был остановлен на шаге 4
    Uninstalled  bool     // unit/plist снят на шаге 5
    UserRemoved  bool     // пользователь удалён на шаге 11
    UserAbsent   bool     // пользователь уже отсутствовал
    DirsRemoved  []string // каталоги, удалённые на шагах 12–14
    DirsAbsent   []string // каталоги, которые уже отсутствовали
}
```

### 5.2 Когда эмитируется аудит-запись

Аудит-запись формируется и эмитируется на шаге 10 — после всех проверок, но ДО физического
удаления каталогов (SR-116). Запись содержит:
- `action: "purge"`
- `platform`
- `user`: имя пользователя (не содержимое)
- `dirs`: список путей к каталогам (только имена, не содержимое)
- `user_present`: bool (будет ли пользователь удалён)
- `dirs_present`: []string (какие каталоги существуют)

Запись НЕ содержит: содержимое `keys.db`, ключи, данные файлов, сырые трассы ошибок (SR-124).

---

## 6. Новые sentinel-ошибки

Добавляются в `internal/service/service.go` рядом с существующими:

```go
// ErrUserMismatch — целевой пользователь ОС не соответствует ожидаемому системному аккаунту
// raxd-раскладки (имя совпадает, но shell — login-shell). AC6, SR-117.
var ErrUserMismatch = errors.New("user account does not match expected raxd system account")

// ErrSuspiciousPath — путь к state/config-каталогу не прошёл инвариант validatePurgePath.
// AC7, SR-118, SR-119.
var ErrSuspiciousPath = errors.New("suspicious path rejected by purge safety check")

// ErrPurgeNotConfirmed — Purge вызван с opts.Confirmed=false.
// Дублирующая защита менеджера; основной барьер — в CLI (--yes). AC9, SR-114.
var ErrPurgeNotConfirmed = errors.New("purge requires explicit confirmation (--yes flag)")
```

---

## 7. Расширение интерфейса `ServiceManager`

```go
// Purge необратимо удаляет системного пользователя raxd и каталоги данных/конфига.
// Требует opts.Confirmed=true (дублирующая защита). Вызывать только после CLI-барьера --yes.
// Порядок: privilege-check → stop → uninstall → validatePurgePath → verifyTargetUser →
//           аудит → userdel/dscl → os.RemoveAll.
// Идемпотентность: отсутствие пользователя/каталогов — не ошибка (AC3).
// Ошибки: ErrPermission, ErrUserMismatch, ErrSuspiciousPath, ErrPurgeNotConfirmed.
Purge(ctx context.Context, opts PurgeOptions) (PurgeReport, error)
```

```go
// PurgeOptions — параметры вызова Purge.
type PurgeOptions struct {
    Confirmed bool // true = --yes передан; false → ErrPurgeNotConfirmed
}
```

---

## 8. Распределение по файлам

| Файл                                  | Что содержит                                                                 |
|---------------------------------------|------------------------------------------------------------------------------|
| `internal/service/service.go`         | `PurgeOptions`, `PurgeReport`, новые sentinels, расширение `ServiceManager`  |
| `internal/service/purge.go` (новый)  | `validatePurgePath`, `isEqualOrAncestor`, платформенно-нейтральная оркестрация Purge (вызывает платформенные хелперы) |
| `internal/service/systemd.go`         | `(m *systemdManager) Purge(...)`, `verifyTargetUser` для Linux, `mapUserdelExitCode`, удаление каталогов Linux |
| `internal/service/launchd.go`         | `(m *launchdManager) Purge(...)`, `verifyTargetUser` для macOS, `mapDsclDeleteError`, удаление каталогов macOS |
| `internal/service/exec.go`            | без изменений (уже содержит `runCommandRaw`, `isExitCode`)                   |
| `internal/cli/service.go`             | флаги `--purge`/`--yes`, барьер AC9, вызов `Purge` vs `Uninstall`, форматирование `PurgeReport`, аудит-лог |
| `internal/cli/service_test.go`        | расширение `fakeManager` методом `Purge`; все тест-кейсы AC10/SR-126        |

---

## 9. Кросс-платформенность

Оба файла `systemd.go` и `launchd.go` компилируются на обеих платформах (нет build-тегов,
такой же подход как в существующем коде). Платформенные системные вызовы достигаются только
в runtime на соответствующей ОС. Это позволяет тестировать сигнатуры и логику `PurgeReport`
в Docker на Linux для обеих ОС через `fakeManager` (SR-126, SR-127).

### 9.1 Конкретные бинари и пути (фиксированные константы)

**Linux:**
```go
const (
    userdelBin = "/usr/sbin/userdel"
    getentBin  = "/usr/bin/getent"
)
```

**macOS:**
```go
const (
    dsclBin = "/usr/bin/dscl"
)
```

Все пути — абсолютные константы, не вычисляются из окружения (SR-120, baseline §3).

### 9.2 Допустимые nologin-shell (общее множество для обеих платформ)

```go
var noLoginShells = map[string]bool{
    "/usr/sbin/nologin": true,
    "/sbin/nologin":     true,
    "/usr/bin/false":    true,
}
```

На Linux `createUser` выставляет `--shell /usr/sbin/nologin`.
На macOS `dscl . -create /Users/raxd UserShell /usr/bin/false` (как указано в существующем Install-коде).

---

## 10. Безопасность: что developer ОБЯЗАН соблюдать

| SR     | Требование                                                                              |
|--------|-----------------------------------------------------------------------------------------|
| SR-114 | `opts.Confirmed=false` → `ErrPurgeNotConfirmed`, деструктивных вызовов нет             |
| SR-117 | `verifyTargetUser`: shell ∉ noLoginShells → `ErrUserMismatch` до userdel/dscl          |
| SR-118 | `validatePurgePath`: список запрещённых путей, проверяется до `os.RemoveAll`           |
| SR-119 | `filepath.EvalSymlinks` внутри `validatePurgePath`, симлинк наружу → `ErrSuspiciousPath` |
| SR-120 | `runCommandRaw(ctx, bin, arg1, arg2, ...)` — никогда `sh -c`, никогда конкатенация     |
| SR-121 | `os.Geteuid() != 0` на шаге 1 → `ErrPermission`, СТОП                                 |
| SR-122 | Строгий порядок шагов 1–15; Stop-fail → СТОП перед шагом 11                            |
| SR-123 | `userdel` exit 6 / `dscl` «not found» → success, а не error                           |
| SR-124 | `PurgeReport` и аудит — только метаданные; сырой stderr → только в маппинг sentinel    |
| SR-116 | Аудит-эмит (шаг 10) ДО `os.RemoveAll` (шаги 12–14)                                    |
| SR-125 | Путь удаления недостижим без `--purge`; ветвление CLI гарантирует `Uninstall` byte-for-byte |

---

## 11. Граница: что отдаётся developer

Developer реализует следующие единицы по этому дизайну:

**`internal/service/service.go`** — добавить:
- `PurgeOptions`, `PurgeReport`, `ErrUserMismatch`, `ErrSuspiciousPath`, `ErrPurgeNotConfirmed`
- метод `Purge(ctx, opts)` в интерфейс `ServiceManager`

**`internal/service/purge.go`** (новый файл) — реализовать:
- `validatePurgePath(path string, allowedRoots []string) error` согласно §3
- `isEqualOrAncestor(candidate, base string) bool`
- Платформенную функцию-оркестратор (вызывается платформенными реализациями) — или общую
  `runPurge(ctx, opts, stop, uninstall, verifyUser, deleteUser, dirs, audit)` через функциональные аргументы (testability, SR-126)

**`internal/service/systemd.go`** — добавить:
- `(m *systemdManager) Purge(ctx, opts) (PurgeReport, error)` — вызывает purge.go
- `verifyTargetUserLinux(ctx, name string) (present bool, err error)` через `getent passwd`
- `mapUserdelExitCode(err error) error` согласно таблице §2.1

**`internal/service/launchd.go`** — добавить:
- `(m *launchdManager) Purge(ctx, opts) (PurgeReport, error)` — вызывает purge.go
- `verifyTargetUserDarwin(ctx, name string) (present bool, err error)` через `dscl . -read`
- `mapDsclDeleteError(stderr string) error` согласно §2.2

**`internal/cli/service.go`** — добавить:
- `--purge` (Bool flag), `--yes` (Bool flag) к команде `uninstall`
- Барьер: `--purge` без `--yes` → warn + exit != 0, без вызова `Purge` (SR-114, SR-115, AC9)
- Ветвление: `--purge --yes` → `mgr.Purge(ctx, PurgeOptions{Confirmed: true})`; иначе прежний `mgr.Uninstall(ctx)` (SR-125: Uninstall byte-for-byte, деструктивный путь изолирован в `Purge`)
- Форматирование `PurgeReport` для CLI (что удалено / что отсутствовало) (AC3)
- Аудит-лог факта purge (SR-116)

**`internal/cli/service_test.go`** — добавить:
- Метод `Purge` к `fakeManager`
- Тест-кейсы согласно AC10/SR-126

---

## 12. Что НЕ входит в scope system-dev

- Финальные формулировки предупреждения о необратимости и текст отчёта → `cli-ux` (`ux-spec.md`).
- Резервное копирование `keys.db` перед удалением — не делаем (spec.md Out of Scope).
- Изменение логики Install/Start/Stop/Status — не трогаем (AC2).
- Windows — вне scope (CLAUDE.md Non-goals).
- Удаление бинаря `raxd` и install-скрипта — не задача сервиса (spec.md Out of Scope).
- Тесты systemd-интеграции на реальном systemd — в контейнере с systemd; на хост-машину
  разработчика ничего не ставится (SECURITY-BASELINE §6).
