# Plan: service-install — регистрация raxd как управляемого системного сервиса (Linux systemd + macOS launchd), не-root

Автор плана: architect (raxd). Вход: spec.md (AC1-16, Q1-Q8 — неизменны), research.md, ADR-001..004
(accepted), реальный код (`internal/cli/{root,serve,status,key}.go`, `internal/config/{paths,config}.go`).
Автор продукта: Vladimir Kovalev, OEM TECH. Развилки Q1-Q8 закрыты в ADR-001..004 + ниже.

## Chosen Approach
Новый пакет **`internal/service`**: интерфейс `ServiceManager` (`Install/Uninstall/Start/Stop/Status`)
+ реализации `systemdManager` (Linux) и `launchdManager` (macOS), выбор по `runtime.GOOS`. Описание
сервиса генерируется **stdlib `text/template`** из встроенных шаблонов unit/plist — **без
`kardianos/service`** (его нет в go.mod/vendor, шаблоны не покрывают security-директивы; ADR-001).
Lifecycle вызывает нативные `systemctl`/`launchctl` через `os/exec`. Пути сервиса = существующий
XDG-резолв, переключаемый через `Environment=`/plist (`XDG_*`, `HOME`) — код `internal/config` НЕ
меняется (ADR-002). CLI-группа `raxd service …` регистрируется в `root.go`. Кросс-сборка — Makefile/
Dockerfile-таргет (без CI; AC14). Всё на stdlib — новых зависимостей нет (AC15).

## Modules
- `internal/service/service.go` — интерфейс `ServiceManager`, типизированные ошибки
  (`ErrAlreadyInstalled/ErrNotInstalled/ErrManagerUnavailable/ErrPermission/ErrUnsupported`), `Status`,
  `Config`, `New(cfg) (ServiceManager, error)` (выбор по `runtime.GOOS`).
- `internal/service/systemd.go` — `systemdManager`: рендер+запись unit `/etc/systemd/system/raxd.service`
  (root,0644), idempotent-создание пользователя `raxd`, drop-in journald, `systemctl daemon-reload/
  enable --now/disable/stop/show`; идемпотентность (AC9/AC10) и откат при сбое install (AC11).
- `internal/service/launchd.go` — `launchdManager`: рендер+запись plist
  `/Library/LaunchDaemons/tech.oem.raxd.plist` (root,0644), `mkdir 0700`+`chown raxd`, `launchctl
  bootstrap/enable/bootout/print`. На Linux собирается; вне darwin — только unit-тест генерации (AC13).
- `internal/service/templates.go` — встроенные `unitTemplate`/`plistTemplate`, `TemplateData`,
  `renderUnit(d)`/`renderPlist(d)` (чистые, без I/O — юнит-тест AC2/AC13).
- `internal/service/exec.go` — `runManager(ctx, name, args...) (string, error)` над `os/exec`:
  захват stderr → нейтральная ошибка; маппинг кодов выхода менеджера в типизированные ошибки.
- `internal/cli/service.go` — cobra-группа `service` + 5 подкоманд, RunE-обёртки (паттерн `key.go`).
- `internal/cli/root.go` — **интеграция**: `newServiceCmd()` в `root.AddCommand(...)`.
- `Makefile` / `Dockerfile.systemd` — **новые**: таргет `cross-build` (4 цели, `CGO_ENABLED=0`,
  `-mod=vendor`) + образ с systemd для интеграции QA (AC14/AC16). Без CI (это distribution).

## Contracts
- `service.New(cfg Config) (ServiceManager, error)` (`service.go`) — `linux`→systemd, `darwin`→launchd,
  иначе `ErrUnsupported`. `Config{ExecPath string; Port int; User, Group, Label string}` — `ExecPath`
  по умолчанию `os.Executable()`; `Port` из `config.Load` (определяет AmbientCapabilities, ADR-003).
- `ServiceManager` (методы принимают `context.Context`, возвращают типизированные ошибки):
  - `Install(ctx) error` — генерирует+регистрирует+enable-on-boot (AC1-AC3). Идемпотентность: уже
    установлен → `ErrAlreadyInstalled` (AC9, spec Q7: «ошибка уже установлен»); сбой на любом шаге →
    откат созданных артефактов (unit/drop-in/каталоги, НЕ пользователь) + ошибка, система в исходном (AC11).
  - `Uninstall(ctx) error` — stop+disable+удаление unit/plist/drop-in (AC10); отсутствует →
    `ErrNotInstalled` (без невнятного падения); после успеха артефактов регистрации нет.
  - `Start(ctx)` / `Stop(ctx) error` — `systemctl start/stop` (AC5: stop=SIGTERM→graceful, без авто-
    рестарта); не установлен → `ErrNotInstalled`.
  - `Status(ctx) (Status, error)` — `Status{Installed, Active bool; PID, EUID int; State string}`;
    не установлен → `Installed:false` без ошибки.
  - Нехватка прав → `ErrPermission`; менеджер недоступен/не systemd → `ErrManagerUnavailable` (AC12).
- `renderUnit(d TemplateData) (string, error)` / `renderPlist(d) (string, error)` (`templates.go`) —
  чистый рендер; ошибка только при сбое шаблона. `TemplateData{ExecPath, User, Group, Label string;
  Port int; StateDir, ConfigDir, LogPath string; NeedNetBindCap bool}`; `NeedNetBindCap = Port<1024`
  (условная директива `AmbientCapabilities`/опуск `NoNewPrivileges`, ADR-003).
- `runManager(ctx, name string, args ...string) (string, error)` (`exec.go`) — ненулевой код → ошибка
  с нейтральным текстом (сырой stderr НЕ пробрасывается, AC12); `exec.ErrNotFound` → `ErrManagerUnavailable`.
- `newServiceCmd() *cobra.Command` (`service.go`) — группа + 5 подкоманд; RunE: машиночит. результат
  (`status`-поля) в stdout (паттерн `status.go`); ошибки `error:`/`hint:` строчными в stderr (STACK)
  + ненулевой код (AC12).

## Шаблоны (генерируемые директивы)
- **unit** `/etc/systemd/system/raxd.service`: `Type=exec`, `ExecStart={{.ExecPath}} serve`,
  `User/Group={{.User}}`, `Restart=on-failure`, `RestartSec=2s` (AC4; SIGTERM=clean-exit→не рестартит
  при stop, AC5), `StateDirectory=raxd`, `StateDirectoryMode=0700` (ADR-002), `Environment=XDG_CONFIG_HOME=/etc
  XDG_STATE_HOME=/var/lib HOME=/var/lib/raxd`, `ProtectSystem=strict ProtectHome=yes PrivateTmp=yes`;
  **условно** `NoNewPrivileges=yes` (Port≥1024) ИЛИ `AmbientCapabilities=CAP_NET_BIND_SERVICE` (Port<1024,
  без NoNewPrivileges — ADR-003); `WantedBy=multi-user.target` (AC3).
- **plist** `/Library/LaunchDaemons/tech.oem.raxd.plist`: `Label=tech.oem.raxd`,
  `ProgramArguments=[{{.ExecPath}}, serve]`, `RunAtLoad=true` (AC3), `KeepAlive={SuccessfulExit=false}`
  (рестарт только при коде≠0 — AC4; graceful код0→не рестартит, AC5), `UserName={{.User}}`,
  `EnvironmentVariables={XDG_*, HOME}`, `StandardErrorPath={{.LogPath}}`.
- **journald drop-in** `/etc/systemd/journald.conf.d/raxd.conf`: `SystemMaxUse=`/`SystemMaxFileSize=`
  (ADR-004, AC8); удаляется при uninstall (AC10).

## Интеграция с существующим кодом
- `root.go` — `newServiceCmd()` в `AddCommand`. `internal/config` НЕ меняется (пути через env unit/plist;
  порт читается `config.Load(paths).Port` в RunE install). `serve` — цель ExecStart; graceful shutdown
  (`signal.NotifyContext(..., SIGTERM)`→return nil→код0) наследуется как есть (AC5).

## План тестирования
- **Unit (офлайн, любая платформа):** `renderUnit/renderPlist` (структура, условный cap/NoNewPriv по
  порту — AC2/AC7/AC13), `KeepAlive.SuccessfulExit`, `New` по GOOS, маппинг ошибок `runManager`.
- **Linux-интеграция (Docker+systemd, baseline §6, research Q9):** install→status enabled/active (AC3)→
  euid!=0 (AC6/AC7)→`kill -9`+wait→смена PID (AC4)→stop graceful stopped (AC5)→uninstall без артефактов
  (AC10)→повторный install идемпотентен (AC9)→сбой-инъекция→откат (AC11)→ошибки `error:`/`hint:` (AC12);
  AC8 — занизить `SystemMaxUse=`, наполнить, `journalctl --disk-usage` ограничен.
- **Кросс-сборка (AC14):** 4 цели `GOOS×GOARCH` `CGO_ENABLED=0`; нативный `raxd version` исполняется; 3
  прочих — `file` (ELF/Mach-O+arch). **macOS (AC13):** интеграция вне Docker; в контейнере — unit генератора plist.

## Out of Scope
`curl|sh`, `.goreleaser`, CI, подпись/нотаризация macOS, quarantine — задача `distribution`. Windows,
самообновление, многоэкземплярность. Функциональность демона (serve/MCP/auth/TLS) — как есть.

## Trade-offs (детали цены — в соответствующих ADR)
- Ручная генерация unit/plist (stdlib) вместо `kardianos/service` (ADR-001): цена — идемпотентность/
  откат/детект менеджера на нас; взамен — ноль зависимостей (offline-vendor AC15) + контроль security-директив.
- Статический пользователь `raxd` вместо `DynamicUser=yes` (ADR-002): цена — создание + ручные каталоги
  macOS; взамен — симметрия платформ + стабильный UID владения состоянием.
- XDG через `Environment=` вместо хардкода путей (ADR-002): цена — пути зависят от env unit/plist; взамен —
  `internal/config` не меняется.
- journald(stderr)+drop-in вместо logrotate (ADR-004): цена — лимиты per-host; взамен — ноль изменений кода.
- Условный `AmbientCapabilities`<1024 с опуском `NoNewPrivileges` вместо setcap (ADR-003): цена — узкое
  окно ослабления hardening для редкого прив. порта; взамен — переживает обновления, штатный механизм.
- Новых зависимостей **нет** (`text/template`/`os/exec`/`embed`/`runtime`/`os`/`context` — stdlib; cobra
  в STACK). `kardianos/service` помечается в STACK как неиспользуемый (ADR-001).

## Хэндофф security (threat-model.md + security-requirements.md)
- **Cap×NoNewPrivileges** (ADR-003): подтвердить корректность опуска NoNewPrivileges + ambient при <1024;
  политика дефолта ≥1024. **Права**: `StateDirectoryMode=0700` (не 0755); unit/plist/drop-in root:root
  0644 (raxd не подменит); keys.db 0600. **Не-root** (AC6): euid!=0; raxd без shell/home. **Uninstall не
  оставляет привилегий** (AC10/AC11): снятие unit/drop-in + политика по пользователю raxd. **Без инъекций**:
  валидация `ExecPath/User/Port` до рендера шаблона (нет инъекции директив из конфига).
