# Plan: service-purge — `raxd service uninstall --purge`

## Chosen Approach
Добавляем в `ServiceManager` **отдельный метод** `Purge(ctx, opts) (PurgeReport, error)` вместо
параметризации `Uninstall`. Причина: AC2 требует, чтобы `Uninstall` остался byte-for-byte (тот же
путь, вывод, ошибки) — новый параметр менял бы сигнатуру и нёс риск регрессии существующего кода.
Отдельный метод изолирует необратимую операцию, а возврат `PurgeReport` (что удалено / что уже
отсутствовало) даёт идемпотентный вывод (AC3) и материал для аудита ДО удаления (AC8). Внутри
`Purge` переиспользует существующий `Uninstall` как первый шаг, добавляя удаление пользователя и
каталогов через уже имеющийся exec-без-shell (`runCommandRaw`, SR-91). Барьер `--yes` (AC9) и
маппинг exit-кодов — целиком на уровне CLI, до вызова менеджера.

## Modules
- `internal/service/service.go` — расширить интерфейс `ServiceManager` методом `Purge`; новые типы
  `PurgeOptions`, `PurgeReport`; новые sentinel-ошибки `ErrUserMismatch`, `ErrSuspiciousPath`.
- `internal/service/purge.go` (новый) — платформенно-нейтральная логика: оркестрация порядка шагов
  (stop → uninstall → проверки → удаление), инвариант проверки пути (`validatePurgePath`), сборка
  `PurgeReport`. Граница: НЕ генерирует платформенные команды.
- `internal/service/systemd.go` — реализация `Purge` для Linux: проверка пользователя через
  `getent`/`/etc/passwd`, удаление `userdel`, удаление каталогов `cfg.StateDir`/`cfg.ConfigDir`.
- `internal/service/launchd.go` — реализация `Purge` для macOS: проверка/удаление пользователя через
  `dscl . -read|-delete /Users/raxd`, удаление каталогов macOS-раскладки.
- `internal/cli/service.go` — флаги `--purge`/`--yes` у `uninstall`, барьер необратимости, ветвление
  на `Purge` vs `Uninstall`, вывод отчёта, аудит-лог, маппинг `ErrUserMismatch`/`ErrSuspiciousPath`.
- `internal/cli/service_test.go` — `fakeManager` дополняется методом `Purge` (тестируемость, AC10).

## Contracts
- `PurgeOptions struct { Confirmed bool }`
  - `Confirmed` — прокидывается из CLI (`--yes`). Менеджер при `false` НЕ удаляет ничего и возвращает
    `ErrPurgeNotConfirmed` (дублирующая защита; основной барьер — в CLI, AC9).
- `PurgeReport struct { UserRemoved, UserAbsent bool; DirsRemoved, DirsAbsent []string; Stopped bool; Platform string }`
  - заполняется ПЕРЕД физическим удалением для аудита, дополняется по ходу (AC8); основа вывода (AC3).
- `Purge(ctx context.Context, opts PurgeOptions) (PurgeReport, error)` (метод `ServiceManager`)
  - порядок: privilege-check → (если запущен) `Stop` → `Uninstall` (игнорируя `ErrNotInstalled`) →
    `validatePurgePath` обоих каталогов → проверка пользователя → удаление пользователя → удаление
    каталогов; аудит-запись формируется до удаления каталогов (AC8).
  - возврат: `PurgeReport` с фактами удаления; `error == nil` при успехе и при идемпотентном повторе.
  - ошибки: `ErrPermission` (нет root — НИЧЕГО не удалено, AC5); если `Stop` не удался — возврат
    ошибки ДО удаления пользователя/каталогов (AC4, без частичного состояния); `ErrUserMismatch`
    (AC6); `ErrSuspiciousPath` (AC7); отсутствие пользователя/каталога — НЕ ошибка (AC3).
- `validatePurgePath(path string, allowedRoots []string) error` (в `purge.go`)
  - инвариант: путь непустой; не `/`; не `$HOME` и не его родитель; не корневой системный каталог
    (`/etc`, `/var`, `/usr`, `/usr/local`); после `filepath.EvalSymlinks` остаётся ВНУТРИ ожидаемой
    раскладки (`cfg.StateDir`/`cfg.ConfigDir` из `DefaultConfigForGOOS`); иначе `ErrSuspiciousPath`.
- `verifyTargetUser(ctx, name string) (present bool, err error)` (платформенный, в systemd.go/launchd.go)
  - проверяет, что пользователь `name` — системный аккаунт раксд-раскладки: имя совпадает И shell ∈
    {`/usr/sbin/nologin`, `/sbin/nologin`, `/usr/bin/false`} (нет login-shell). Несоответствие →
    `ErrUserMismatch` (AC6). Отсутствие пользователя → `present=false, err=nil` (идемпотентность, AC3).
- `ErrUserMismatch`, `ErrSuspiciousPath`, `ErrPurgeNotConfirmed` — новые sentinel в `service.go`;
  CLI маппит первые два на exit != 0 с нейтральным сообщением (SR-95), третий — на барьер AC9.
- CLI: `uninstall` получает `cmd.Flags().Bool("purge", ...)` и `Bool("yes", ...)`. Если `--purge`
  без `--yes` — печать предупреждения о необратимости (стирание `keys.db`/аудита) + `return err`
  (exit != 0, ничего не вызвано, AC9). При `--purge --yes` — вызов `Purge`, иначе прежний `Uninstall`.

## Граница для system-dev (service-design.md)
Отдаётся: точные команды и флаги `userdel`/`dscl` и их exit-коды; парсинг `getent passwd`/`dscl -read`
для `verifyTargetUser`; платформенные нюансы chown/прав при удалении каталогов; маппинг кодов
`userdel` (отсутствие пользователя vs отказ прав) на `ErrPermission`/idempotent. cli-ux: финальные
формулировки предупреждения о необратимости и текст отчёта (AC3/AC8).

## Trade-offs
- Выбрали **новый метод `Purge`** вместо **параметра в `Uninstall(opts)`**: цена — расширение
  интерфейса ломает компиляцию всех реализаций и `fakeManager` (нужно дописать метод). Платим этим
  ради сохранения `Uninstall` byte-for-byte (AC2) и изоляции необратимого пути.
- Выбрали **переиспользование `Uninstall` внутри `Purge`** вместо дублирования снятия unit/plist:
  цена — `Purge` зависит от поведения `Uninstall` (связность), но избегаем рассинхрона шагов снятия.
- Новых зависимостей нет: только stdlib (`os`, `os/exec`, `path/filepath`, `context`) — сверено со
  STACK.ru.md (SR-96, stdlib-only) и SECURITY-BASELINE §3 (exec без shell, без root-эскалации).
