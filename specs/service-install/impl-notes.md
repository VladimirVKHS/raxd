# Impl Notes: service-install

## Что реализовано

### `internal/service/service.go`
Интерфейс `ServiceManager` (Install/Uninstall/Start/Stop/Status с `context.Context`), типизированные ошибки-сентинели (`ErrAlreadyInstalled`, `ErrNotInstalled`, `ErrManagerUnavailable`, `ErrPermission`, `ErrUnsupported`), тип `ServiceError` с `Is()` для `errors.Is`, структуры `Status`{Installed, Active bool; PID, EUID int; State string} и `Config`{ExecPath, Port, User, Group, Label, StateDir, ConfigDir, LogPath}, функция `DefaultConfig()` с дефолтами (user=raxd, port=7822, label=tech.oem.raxd), конструктор `New(cfg Config)` с диспетчеризацией по `runtime.GOOS` (linux→systemdManager, darwin→launchdManager, иначе ErrUnsupported).

### `internal/service/templates.go` (без build-тегов, AC13)
Структура `TemplateData` с типизированным `NeedNetBindCap bool` (не сырая строка, SR-90). Функция `ValidateTemplateData(d)` проверяет ДО рендера: User/Group по regex `^[a-z_][a-z0-9_-]{0,31}$`, Label по `^[a-z][a-z0-9._-]{0,253}$`, ExecPath — абсолютный+нормализованный без управляющих символов, StateDir/ConfigDir/LogPath — абсолютные без управляющих, Port в 1..65535. Функции `RenderUnit(d)` и `RenderPlist(d)` вызывают валидацию перед рендером — ошибка возвращается до записи (SR-90). Шаблон unit генерирует: для Port≥1024 — `NoNewPrivileges=yes`, без AmbientCapabilities; для Port<1024 — `CapabilityBoundingSet=CAP_NET_BIND_SERVICE` + `AmbientCapabilities=CAP_NET_BIND_SERVICE`, без NoNewPrivileges (ADR-003, П-1). Hardening `ProtectSystem=strict/ProtectHome=yes/PrivateTmp=yes` присутствует в обоих вариантах (SR-87). `StateDirectoryMode=0700` явно (SR-89). `StandardError=journal` явно. Plist содержит `KeepAlive.SuccessfulExit=false` (AC4/AC5), `UserName=raxd` (SR-83), `EnvironmentVariables` с XDG_* (ADR-002). `JournaldDropIn()` возвращает содержимое drop-in с `SystemMaxUse=200M`/`SystemMaxFileSize=50M` (SR-94, ADR-004). `TemplateDataFromConfig(cfg)` деривирует `NeedNetBindCap = Port < 1024`.

### `internal/service/exec.go`
`RunManager(ctx, name, args...)` — обёртка над `exec.CommandContext` без shell (SR-91). `exec.ErrNotFound` → `ErrManagerUnavailable`. Не-ExitError (path error, context cancel) → нейтральная ошибка. Сырой stderr захватывается и НЕ пробрасывается в user output (SR-95), только нейтральный текст. Вспомогательные: `runCommandRaw(ctx, name, args...)` для `systemd.go`/`launchd.go`, `isExitCode(err, code)` для проверки exit 9 (useradd already-exists). `neutralizeStderr` всегда возвращает фиксированную строку "manager command failed" — сырой stderr полностью отбрасывается (ISSUE-5 fix, SR-95).

### `internal/service/systemd.go`
`systemdManager`: Install — 8 шагов (privilege check → idempotency check → createUser → renderUnit → writeFile unit 0644 → writeFile drop-in 0644 → daemon-reload → enable). Откат при сбое шагов 5-8 — удаление unit/drop-in (AC11, SR-92). Пользователь raxd НЕ откатывается (ADR-002, П-2). `createUser` вызывает `useradd --system --no-create-home --shell /usr/sbin/nologin` через `runCommandRaw` (SR-91); exit 9 = already exists → OK (SR-83). Uninstall: stop+disable+daemon-reload+rm unit+rm drop-in+daemon-reload; пользователь raxd остаётся (П-2, SR-93). Start/Stop: privilege check + unit-file existence check → ErrNotInstalled. Status: `systemctl show -p MainPID,ActiveState,SubState,UnitFileState raxd`; EUID читается из `/proc/<pid>/status` (AC6). `writeFile` создаёт директории + записывает с заданным mode. `parseSystemctlProps` парсит KEY=VALUE вывод. `readProcEUID` читает строку Uid: из /proc.

### `internal/service/launchd.go`
`launchdManager` (macOS, AC13 — интеграция только на реальном macOS): Install — plist в `/Library/LaunchDaemons/tech.oem.raxd.plist` 0644 (SR-88); `createDirs` создаёт StateDir/LogPath 0700 + chown через `runCommandRaw(ctx, "/usr/sbin/chown", ...)` (ISSUE-7 fix, SR-89, SR-91); launchctl bootstrap + enable (AC3); откат при сбое bootstrap. Uninstall: bootout+disable+rm plist; пользователь kept (П-2). Start: kickstart -k; Stop: kill SIGTERM (AC5). Status: print system/tech.oem.raxd; парсит "state = running" и "pid = N". EUID не читается на macOS (нет /proc).

### `internal/cli/service.go`
cobra-группа `service` + 5 подкоманд через `buildServiceCmd(mgr service.ServiceManager)` с инъекцией менеджера для тестов. `resolveManagerWithPort(injected)` (ISSUE-1 fix): в production читает порт из `config.Load(config.Paths())`, а не из `DefaultConfig()`; возвращает `(manager, port, error)` — порт проводится в success-блоки и status. install: ErrAlreadyInstalled → nil (exit 0) + info-блок без `error:` (AC9); успех → success-block с port и `autostart enabled` (ISSUE-6 fix). status human: строки port и autostart (ISSUE-3 fix). status --json: поля port/autostart/unit_path (ISSUE-2 fix). `mapManagerError` — нейтральный маппинг без raw stderr (SR-95). `printSvcError` — строчные error:/hint:.

### `internal/cli/root.go`
`buildServiceCmd(mgr)` добавлен в `root.AddCommand`. Экспортированы `NewRootCmd()` и `NewRootCmdWithServiceManager(mgr service.ServiceManager)` для тестовой инъекции.

### `security_static_test.go`
`TestStaticNoExecCommand` дополнен вайтлистом `internal/service` (вторая авторизованная точка для `os/exec` — вызовы systemctl/launchctl/useradd по SR-91, plan.md §Modules).

## Отклонения/эскалации

1. **`TestStaticNoExecCommand` — расширение whitelist.** Существующий тест запрещал `os/exec` вне `internal/cmdexec`. Пакет `internal/service/exec.go` легитимно использует `exec.Command` для вызовов менеджера сервисов (SR-91, plan.md). Решение: добавлен `internal/service` в whitelist с комментарием — это не обход требования, а расширение контракта как предписано планом. Эскалации не требуется (план явно предписывает `os/exec` в exec.go).

2. **Комментарий в unitTemplate содержал слово "NoNewPrivileges".** В шаблоне для условного блока (Port<1024) был комментарий с текстом "NoNewPrivileges is NOT set". Тест `TestRenderUnit_PrivilegedPort` проверяет отсутствие строки "NoNewPrivileges" в выводе. Исправлено: комментарий перефразирован без упоминания директивы. Не отклонение от плана — коррекция реализации под тест.

3. **launchd.go — macOS-ограничение (AC13).** Создание пользователя (`dscl`), проверка EUID и интеграция с launchctl не тестируются в Docker (Linux). Это зафиксированное ограничение среды (AC13, ОР-4), не скрытое — полная интеграция на реальном macOS должна выполняться QA вне Docker.

### Исправления по developer-guardian (коммит 4876696)

4. **ISSUE-1 (реальный баг SR-85/ADR-003) — исправлен.** `resolveManager` заменён на `resolveManagerWithPort`, читающий `config.Load(config.Paths()).Port`. Добавлены whitebox-тесты: `TestResolveManagerWithPort_ReadsPortFromConfig` (XDG_CONFIG_HOME → config.yaml port:443 → port==443) и `TestResolveManagerWithPort_DefaultPortWhenNoConfig`.

5. **ISSUE-4 (ложно-зелёный тест) — исправлен.** `TestRunManager_RawStderrNotPropagated` теперь запускает `/bin/ls /nonexistent-raxd-stderr-test-xyzzy`, записывает известный sentinel в stderr, и утверждает, что sentinel отсутствует в `err.Error()`.

6. **ISSUE-2/3/6 (ux-spec) — исправлены.** `jsonStatus` дополнен полями `port/autostart/unit_path`. `printStatusHuman` добавлены строки port и autostart. Install success block добавлены port и autostart enabled.

7. **ISSUE-5 (info) — исправлен.** `neutralizeStderr` упрощена: отбрасывает аргумент, всегда возвращает `"manager command failed"` — dead code удалён.

8. **ISSUE-7 (info) — исправлен.** `launchd.go createDirs`: `exec.Command(...)` заменён на `runCommandRaw(ctx, "/usr/sbin/chown", ...)` + убран импорт `os/exec` из launchd.go.

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
| TestRunManager_RawStderrNotPropagated | SR-95 (реальная проверка sentinel, ISSUE-4 fix) |
| TestResolveManagerWithPort_ReadsPortFromConfig | SR-85/ADR-003, ISSUE-1 fix |
| TestResolveManagerWithPort_DefaultPortWhenNoConfig | DefaultConfig fallback |
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

### Docker-вывод

**Round 1 — до исправлений (верифицирован дирижёром, коммит до 4876696):**

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

479 тестов, 0 FAIL.

**Round 2 — после исправлений ISSUE-1..7 (коммиты 4876696/5a1f6cf, верифицирован дирижёром):**

```
docker build --target test -t raxd-test-svc .  →  OK

docker run --rm raxd-test-svc
# go vet ./... + go test ./... + -race (cmdexec/fileupload/keystore/server/mcp):

ok  github.com/vladimirvkhs/raxd
ok  github.com/vladimirvkhs/raxd/internal/banner
ok  github.com/vladimirvkhs/raxd/internal/cli
ok  github.com/vladimirvkhs/raxd/internal/cmdexec
ok  github.com/vladimirvkhs/raxd/internal/config
ok  github.com/vladimirvkhs/raxd/internal/fileupload
ok  github.com/vladimirvkhs/raxd/internal/keystore
ok  github.com/vladimirvkhs/raxd/internal/mcp        4.3s
ok  github.com/vladimirvkhs/raxd/internal/server
ok  github.com/vladimirvkhs/raxd/internal/service
ok  github.com/vladimirvkhs/raxd/internal/version

# -race: cmdexec 2.18s / fileupload 1.05s / keystore 1.22s / server 4.14s / mcp 6.12s
```

11 пакетов PASS, 0 FAIL. Включает:
- `internal/service` — `TestRunManager_RawStderrNotPropagated` (реальный sentinel-assert, ISSUE-4 fix), `TestResolveManagerWithPort_*` (ISSUE-1 fix)
- `internal/cli` — `resolveManagerWithPort` читает port из config.Load(); status/install output содержат port/autostart/unit_path (ISSUE-2/3/6)

Подтверждено в Docker:
- SR-90 анти-инъекция: `TestValidateTemplateData_UserInjection` (newline/equals), `TestRenderUnit_InjectionRejectedBeforeRender`, `TestRenderPlist_InjectionRejectedBeforeRender` — PASS
- AC13 plist на Linux: `TestRenderPlist_Structure`, `TestRenderPlist_KeepAliveSuccessfulExitFalse` — PASS

**Round 3 — BUG-1 (коммиты 827d736, 70ff715, ожидается Docker-верификация от дирижёра):**

#### Причина бага

`raxd serve` вызывает `config.EnsureDirs` → `os.MkdirAll(/etc/raxd, 0700)`. Под systemd-юнитом `ProtectSystem=strict` запрещает запись в `/etc` для непривилегированного пользователя `raxd`. До фикса `/etc/raxd` не существовал (systemd его не создавал) — `MkdirAll` пытался создать каталог и падал с `permission denied` → crash-loop → `MainPID=0` → euid не поймать. С предсозданным `/etc/raxd owned raxd` `MkdirAll` на существующем каталоге — no-op → `serve` стартует корректно.

Второй аспект (macOS): `DefaultConfig` возвращал `ConfigDir="/etc"` (XDG-родитель, не raxd-каталог), а plist хардкодил linux-пути `/etc` и `/var/lib` — на macOS эти пути под SIP / не существуют.

#### Что изменено

**Коммит 827d736 (Linux systemd):**
- `internal/service/templates.go` — unit-шаблон дополнен директивами `ConfigurationDirectory=raxd` и `ConfigurationDirectoryMode=0700` (рядом со `StateDirectory`). systemd создаёт `/etc/raxd` owned raxd ДО `ExecStart` → `EnsureDirs` становится no-op (SR-89, AC2).

**Коммит 70ff715 (macOS launchd + консистентность):**
- `internal/service/service.go` — `DefaultConfig()` заменён на `DefaultConfigForGOOS(goos string)` (экспортирован для AC13-тестов). Linux: `ConfigDir=/etc/raxd`, `StateDir=/var/lib/raxd`, `LogPath=/var/log/raxd`. Darwin: `ConfigDir=/usr/local/etc/raxd`, `StateDir=/usr/local/var/raxd`, `LogPath=/usr/local/var/log/raxd`. `ConfigDir` — полный raxd-каталог (не XDG-родитель).
- `internal/service/templates.go` — `TemplateData` получил поля `ConfigHome` и `StateHome` (`filepath.Dir` от полных путей). `TemplateDataFromConfig` вычисляет их автоматически. `ValidateTemplateData` проверяет оба поля. Plist-шаблон: хардкод `/etc`/`/var/lib` заменён на `{{.ConfigHome}}`/`{{.StateHome}}` — darwin plist получает `/usr/local/etc` и `/usr/local/var`.
- `internal/service/launchd.go` — `createDirs()` создаёт `ConfigDir` (теперь `/usr/local/etc/raxd`), `StateDir`, `LogPath` — все 0700 + chown raxd:raxd (SR-89, SR-91).

**Инвариант E (проверен тестом):** `filepath.Join(ConfigHome, "raxd") == ConfigDir` для обеих платформ; конкретные значения: linux ConfigHome=`/etc`, darwin ConfigHome=`/usr/local/etc`.

#### Новые тесты (AC13 — все запускаются на Linux в Docker)

| Тест | AC/SR |
|------|-------|
| `TestRenderUnit_DefaultPort` + `TestRenderUnit_PrivilegedPort` (дополнены) | `ConfigurationDirectory=raxd` + `ConfigurationDirectoryMode=0700` в обоих вариантах (SR-89, AC2) |
| `TestPlist_DarwinXDGPaths` | darwin plist: XDG_CONFIG_HOME=`/usr/local/etc`, XDG_STATE_HOME=`/usr/local/var` (AC13, SR-89) |
| `TestDefaultConfigForGOOS_Paths` | linux/darwin пути точно верны (AC2, регресс) |
| `TestTemplateDataFromConfig_InvariantE` | инвариант E + конкретные ConfigHome/StateHome для linux и darwin (AC2, SR-89) |
| `TestPlist_LinuxXDGPathsRegress` | linux plist по-прежнему `/etc` / `/var/lib` (AC13, регресс) |

#### Docker unit + QA systemd-интеграция

Unit-тесты Docker: ожидается Round 3 верификация от дирижёра (локально `go test ./... -count=1` — 12 пакетов PASS, 0 FAIL).

QA systemd-интеграция (подтверждено дирижёром, 62 PASS / 0 FAIL):
- LIVE euid=999 (AC6: демон работает под непривилегированным пользователем raxd)
- Рестарт: PID 179→245 (AC4: перезапуск при сбое)
- AC8 journald: 5.0M ≤ 10M (лимит journald drop-in SR-94 работает корректно)

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
- `neutralizeStderr` всегда возвращает `"manager command failed"`, полностью отбрасывая raw stderr (ISSUE-5 fix: dead code truncation удалён)
- `TestRunManager_RawStderrNotPropagated`: запускает `/bin/ls /nonexistent-raxd-stderr-test-xyzzy`, проверяет sentinel НЕ в err.Error() (ISSUE-4 fix)
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

- **Docker-верификация:** проведена дважды. Round 1 (до ISSUE-1..7 fixes): 479 тестов PASS. Round 2 (коммиты 4876696/5a1f6cf, ISSUE-1..7 fixes): 11 пакетов PASS, 0 FAIL.
- **Кросс-сборка `make build-all`:** Makefile и Dockerfile.systemd созданы system-dev; кросс-сборка компилируется без ошибок (go build проверен локально), финальная верификация с `make verify-cross` — в Docker
- **macOS createUser:** `launchd.go` создаёт директории через `runCommandRaw` + chown (ISSUE-7 fix) — логика присутствует, но не тестируется на Linux. QA проверяет на реальном macOS
- **Расширение security_static_test.go:** потребовалось добавить `internal/service` в whitelist exec-теста; задокументировано в коммите и выше в отклонениях
