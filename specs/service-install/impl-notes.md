# Impl Notes: service-install

## Что реализовано

### `internal/service/service.go`
Интерфейс `ServiceManager` (Install/Uninstall/Start/Stop/Status с `context.Context`), типизированные ошибки-сентинели (`ErrAlreadyInstalled`, `ErrNotInstalled`, `ErrManagerUnavailable`, `ErrPermission`, `ErrUnsupported`), тип `ServiceError` с `Is()` для `errors.Is`, структуры `Status`{Installed, Active bool; PID, EUID int; State string} и `Config`{ExecPath, Port, User, Group, Label, StateDir, ConfigDir, LogPath}, функция `DefaultConfig()` с дефолтами (user=raxd, port=7822, label=tech.oem.raxd), конструктор `New(cfg Config)` с диспетчеризацией по `runtime.GOOS` (linux→systemdManager, darwin→launchdManager, иначе ErrUnsupported).

### `internal/service/templates.go` (без build-тегов, AC13)
Структура `TemplateData` с типизированным `NeedNetBindCap bool` (не сырая строка, SR-90). Функция `ValidateTemplateData(d)` проверяет ДО рендера: User/Group по regex `^[a-z_][a-z0-9_-]{0,31}$`, Label по `^[a-z][a-z0-9._-]{0,253}$`, ExecPath — абсолютный+нормализованный без управляющих символов, StateDir/ConfigDir/LogPath — абсолютные без управляющих, Port в 1..65535. Функции `RenderUnit(d)` и `RenderPlist(d)` вызывают валидацию перед рендером — ошибка возвращается до записи (SR-90). Шаблон unit генерирует: для Port≥1024 — `NoNewPrivileges=yes`, без AmbientCapabilities; для Port<1024 — `CapabilityBoundingSet=CAP_NET_BIND_SERVICE` + `AmbientCapabilities=CAP_NET_BIND_SERVICE`, без NoNewPrivileges (ADR-003, П-1). Hardening `ProtectSystem=strict/ProtectHome=yes/PrivateTmp=yes` присутствует в обоих вариантах (SR-87). `StateDirectoryMode=0700` явно (SR-89). `StandardError=journal` явно. Plist содержит `KeepAlive.SuccessfulExit=false` (AC4/AC5), `UserName=raxd` (SR-83), `EnvironmentVariables` с XDG_* (ADR-002). `JournaldDropIn()` возвращает содержимое drop-in с `SystemMaxUse=200M`/`SystemMaxFileSize=50M` (SR-94, ADR-004). `TemplateDataFromConfig(cfg)` деривирует `NeedNetBindCap = Port < 1024`.

### `internal/service/exec.go`
`RunManager(ctx, name, args...)` — обёртка над `exec.CommandContext` без shell (SR-91). `exec.ErrNotFound` → `ErrManagerUnavailable`. Не-ExitError (path error, context cancel) → нейтральная ошибка. Сырой stderr захватывается и НЕ пробрасывается в user output (SR-95), только нейтральный текст. Вспомогательные: `runCommandRaw(ctx, name, args...)` для `systemd.go`/`launchd.go`, `isExitCode(err, code)` для проверки exit 9 (useradd already-exists). `neutralizeStderr` обрезает >120 байт и удаляет PEM/rax_-маркеры (SR-95).

### `internal/service/systemd.go`
`systemdManager`: Install — 8 шагов (privilege check → idempotency check → createUser → renderUnit → writeFile unit 0644 → writeFile drop-in 0644 → daemon-reload → enable). Откат при сбое шагов 5-8 — удаление unit/drop-in (AC11, SR-92). Пользователь raxd НЕ откатывается (ADR-002, П-2). `createUser` вызывает `useradd --system --no-create-home --shell /usr/sbin/nologin` через `runCommandRaw` (SR-91); exit 9 = already exists → OK (SR-83). Uninstall: stop+disable+daemon-reload+rm unit+rm drop-in+daemon-reload; пользователь raxd остаётся (П-2, SR-93). Start/Stop: privilege check + unit-file existence check → ErrNotInstalled. Status: `systemctl show -p MainPID,ActiveState,SubState,UnitFileState raxd`; EUID читается из `/proc/<pid>/status` (AC6). `writeFile` создаёт директории + записывает с заданным mode. `parseSystemctlProps` парсит KEY=VALUE вывод. `readProcEUID` читает строку Uid: из /proc.

### `internal/service/launchd.go`
`launchdManager` (macOS, AC13 — интеграция только на реальном macOS): Install — plist в `/Library/LaunchDaemons/tech.oem.raxd.plist` 0644 (SR-88); `createDirs` создаёт StateDir/LogPath 0700 + chown (SR-89); launchctl bootstrap + enable (AC3); откат при сбое bootstrap. Uninstall: bootout+disable+rm plist; пользователь kept (П-2). Start: kickstart -k; Stop: kill SIGTERM (AC5). Status: print system/tech.oem.raxd; парсит "state = running" и "pid = N". EUID не читается на macOS (нет /proc).

### `internal/cli/service.go`
cobra-группа `service` + 5 подкоманд через `buildServiceCmd(mgr service.ServiceManager)` с инъекцией менеджера для тестов. install: ErrAlreadyInstalled → nil (exit 0) + info-блок без `error:` (AC9, ux-spec); ErrPermission/ErrManagerUnavailable/ErrUnsupported → error: + hint:; успех → aligned success-block + audit log (ux-spec). uninstall: ErrNotInstalled → nil (exit 0) + info-блок (AC10, ux-spec); успех → removed unit/drop-in/kept user. start: ErrNotInstalled → error + exit 1; stop: аналогично. status: вывод в stdout (`cmd.OutOrStdout()`), ux-spec P-5; флаг `--json` → JSON в stdout с полями installed/active/pid/euid/user/state/manager. `mapManagerError` — нейтральный маппинг ошибок без raw stderr (SR-95). `printSvcError` — строчные error:/hint: (ux-spec).

### `internal/cli/root.go`
`buildServiceCmd(mgr)` добавлен в `root.AddCommand`. Экспортированы `NewRootCmd()` и `NewRootCmdWithServiceManager(mgr service.ServiceManager)` для тестовой инъекции.

### `security_static_test.go`
`TestStaticNoExecCommand` дополнен вайтлистом `internal/service` (вторая авторизованная точка для `os/exec` — вызовы systemctl/launchctl/useradd по SR-91, plan.md §Modules).

## Отклонения/эскалации

1. **`TestStaticNoExecCommand` — расширение whitelist.** Существующий тест запрещал `os/exec` вне `internal/cmdexec`. Пакет `internal/service/exec.go` легитимно использует `exec.Command` для вызовов менеджера сервисов (SR-91, plan.md). Решение: добавлен `internal/service` в whitelist с комментарием — это не обход требования, а расширение контракта как предписано планом. Эскалации не требуется (план явно предписывает `os/exec` в exec.go).

2. **Комментарий в unitTemplate содержал слово "NoNewPrivileges".** В шаблоне для условного блока (Port<1024) был комментарий с текстом "NoNewPrivileges is NOT set". Тест `TestRenderUnit_PrivilegedPort` проверяет отсутствие строки "NoNewPrivileges" в выводе. Исправлено: комментарий перефразирован без упоминания директивы. Не отклонение от плана — коррекция реализации под тест.

3. **launchd.go — macOS-ограничение (AC13).** Создание пользователя (`dscl`), проверка EUID и интеграция с launchctl не тестируются в Docker (Linux). Это зафиксированное ограничение среды (AC13, ОР-4), не скрытое — полная интеграция на реальном macOS должна выполняться QA вне Docker.

## Тесты

### Покрытые acceptance criteria (unit-тесты)

| Test | AC/SR |
|------|-------|
| TestRenderUnit_DefaultPort | AC2, SR-86, SR-87, SR-89 |
| TestRenderUnit_PrivilegedPort | AC7, SR-85, SR-86, SR-87, ADR-003 |
| TestRenderUnit_NoOtherCaps | SR-85 (только CAP_NET_BIND_SERVICE) |
| TestRenderPlist_Structure | AC2, AC13 (на Linux!) |
| TestRenderPlist_KeepAliveSuccessfulExitFalse | AC4, AC5 |
| TestValidateTemplateData_UserInjection | SR-90 (11 векторов) |
| TestValidateTemplateData_ExecPathInjection | SR-90 (5 векторов) |
| TestValidateTemplateData_LabelInjection | SR-90 (4 вектора) |
| TestValidateTemplateData_PortRange | SR-90 (invalid: 0,-1,65536; valid: 1,80,443,7822,65535) |
| TestValidateTemplateData_StateDirInjection | SR-90 |
| TestRenderUnit_InjectionRejectedBeforeRender | SR-90 (инъекция не в выводе) |
| TestRenderPlist_InjectionRejectedBeforeRender | SR-90 |
| TestErrorSentinels | plan.md sentinels |
| TestErrorIs | errors.Is совместимость |
| TestDefaultConfig | Config defaults |
| TestNew_CurrentPlatform | New() dispatch по GOOS |
| TestNew_EmptyExecPath | os.Executable() fallback |
| TestRunManager_NotFound | SR-91, ErrManagerUnavailable |
| TestRunManager_NoShellInterpolation | SR-91 |
| TestServiceCommandRegistered | AC1 |
| TestServiceInstall_AlreadyInstalled_Exit0 | AC9 |
| TestServiceUninstall_NotInstalled_Exit0 | AC10 |
| TestServiceStart_NotInstalled_Exit1 | ux-spec exit codes |
| TestServiceStop_NotInstalled_Exit1 | ux-spec exit codes |
| TestServiceStatus_OutputOnStdout | ux-spec P-5 |
| TestServiceStatus_JSON_OnStdout | ux-spec --json |
| TestServiceError_LowercaseFormat | SR-95, ux-spec |
| TestServiceOutput_NoSecrets | SR-95 |
| TestServiceManagerUnavailable_Error | AC12 |
| TestServiceUnsupported_Error | AC12 |

### Команда запуска

```bash
# В Docker (SECURITY-BASELINE §6):
docker build --target test -t raxd-test . && docker run --rm raxd-test

# Локально (только unit-тесты без systemd):
go vet -mod=vendor ./... && go test -mod=vendor -count=1 ./...
```

### Docker-вывод (верифицирован дирижёром)

```
docker build --target test -t raxd-test-svc .  →  OK

docker run --rm raxd-test-svc
# go vet ./... + go test ./... + -race (cmdexec/fileupload/keystore/server/mcp):

ok  github.com/vladimirvkhs/raxd                     0.009s
ok  github.com/vladimirvkhs/raxd/internal/banner     0.001s
ok  github.com/vladimirvkhs/raxd/internal/cli        0.089s
ok  github.com/vladimirvkhs/raxd/internal/cmdexec    1.179s
ok  github.com/vladimirvkhs/raxd/internal/config     0.005s
ok  github.com/vladimirvkhs/raxd/internal/fileupload 0.035s
ok  github.com/vladimirvkhs/raxd/internal/keystore   0.176s
ok  github.com/vladimirvkhs/raxd/internal/mcp        2.146s
ok  github.com/vladimirvkhs/raxd/internal/server     2.175s
ok  github.com/vladimirvkhs/raxd/internal/service    0.008s
ok  github.com/vladimirvkhs/raxd/internal/version    0.001s

# -race: cmdexec 2.191s / fileupload 1.092s / keystore 1.329s / server 4.122s / mcp 6.219s
```

479 RUN-тестов, 0 FAIL.

Подтверждено в Docker:
- SR-90 анти-инъекция: `TestValidateTemplateData_UserInjection` (newline/equals), `TestRenderUnit_InjectionRejectedBeforeRender`, `TestRenderPlist_InjectionRejectedBeforeRender` — PASS
- AC13 plist на Linux: `TestRenderPlist_Structure`, `TestRenderPlist_KeepAliveSuccessfulExitFalse` — PASS

## Безопасность

### Анти-инъекция в шаблоны (SR-90)
- `ValidateTemplateData` в `templates.go` вызывается до любого рендера
- User/Group: regex `^[a-z_][a-z0-9_-]{0,31}$` — allowlist без пробелов/\n/\r/=/кавычек
- Label: regex `^[a-z][a-z0-9._-]{0,253}$`
- ExecPath: `filepath.IsAbs` + `filepath.Clean` + `hasControlChar` (все \x00-\x1f, \x7f)
- Port: строгий диапазон 1..65535
- `NeedNetBindCap`: типизированный bool, а не строка (SR-90)
- При ошибке — возврат до рендера; plist/unit не записываются

### Выполнение команд без shell (SR-91)
- `RunManager(ctx, name, args...)`: `exec.CommandContext(ctx, name, args...)` — отдельные args
- `runCommandRaw(ctx, name, args...)`: то же для useradd/chown
- Нигде нет `sh -c` или конкатенации аргументов в строку

### Нейтрализация raw stderr (SR-95)
- `RunManager` захватывает stderr в буфер
- `neutralizeStderr` обрезает до 120 байт и удаляет PEM/rax_ маркеры
- В user-facing error выходит только нейтральное сообщение
- CLI `mapManagerError` строит user-facing текст из typed sentinels, не из raw os-ошибки

### Не-root демон (SR-83, SR-84)
- Unit всегда содержит `User=raxd`/`Group=raxd` (проверено в TestRenderUnit_DefaultPort)
- Plist всегда содержит `UserName=raxd` (проверено в TestRenderPlist_Structure)
- install/uninstall/start/stop проверяют `os.Geteuid() == 0` — при нехватке прав → `ErrPermission` (не тихий root-фолбэк)

### Права артефактов (SR-88, SR-89)
- unit/drop-in записываются с mode `0o644` через `writeFile`
- plist записывается с mode `0o644` через `writeFile`
- `StateDirectoryMode=0700` явно в шаблоне unit (проверено TestRenderUnit_DefaultPort)
- macOS: `os.MkdirAll(d, 0o700)` + chown для StateDir/LogPath

### Идемпотентность и откат (SR-92, AC9, AC11)
- Install: проверка `os.Stat(unitPath)` перед записью → `ErrAlreadyInstalled`
- Откат: `rollback(createdUnit, createdDropIn)` при ошибке шагов 5-8
- Пользователь raxd не откатывается (ADR-002, П-2)
- Uninstall при отсутствии unit → `ErrNotInstalled`

### Audit log (ux-spec, SR-19 унаследованный)
- `charmbracelet/log` записывает audit-строку в stderr после install/uninstall/start/stop
- Содержит: action, platform, unit/user — без секретов (SR-95)

### Ограничение роста журнала (SR-94, AC8)
- journald drop-in: `SystemMaxUse=200M`, `SystemMaxFileSize=50M`
- Устанавливается install, удаляется uninstall (SR-93)

## MacOS-ограничение проверки (AC13)

Интеграция launchd НЕ тестируется в Docker (Linux). Выполнено:
- `templates.go` без build-тегов → `RenderPlist` и `ValidateTemplateData` компилируются и тестируются на Linux
- `TestRenderPlist_Structure` и `TestRenderPlist_KeepAliveSuccessfulExitFalse` зелёные на Linux
- Полная интеграция (install→euid!=0→kill→restart→stop→uninstall + dscl createUser) требует реального macOS вне Docker

## Известные ограничения

- **Docker-верификация:** выполнена дирижёром — 479 тестов PASS, 0 FAIL (вывод в разделе «Тесты» выше)
- **Кросс-сборка `make build-all`:** Makefile и Dockerfile.systemd созданы system-dev; кросс-сборка компилируется без ошибок (go build проверен локально), финальная верификация с `make verify-cross` — в Docker
- **macOS createUser:** `launchd.go` создаёт директории через `dscl` — логика присутствует, но не тестируется на Linux. QA проверяет на реальном macOS
- **Расширение security_static_test.go:** потребовалось добавить `internal/service` в whitelist exec-теста; задокументировано в коммите и выше в отклонениях
