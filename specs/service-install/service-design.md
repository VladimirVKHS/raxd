# Service Design: service-install — OS-интеграция raxd (Linux systemd + macOS launchd)

> Автор: system-dev, команда raxd.
> Вход: `spec.md` (AC1-16), `plan.md` (выбранная архитектура), `security-requirements.md`
> (SR-83..96), `research.md` (Q0-Q9 + ADR-001..004 — accepted), `.claude/reference/STACK.ru.md`,
> `SECURITY-BASELINE.ru.md` §3/§6, реальный код (`internal/cli/serve.go`,
> `internal/config/config.go`).
> Автор продукта: Vladimir Kovalev, OEM TECH.

**Одно предложение о задаче:** зарегистрировать `raxd serve` как управляемый системный сервис
(Linux systemd, macOS launchd), исполняющийся под непривилегированным пользователем `raxd:raxd`,
с автозапуском, рестартом при сбое, ограничением роста журнала и кросс-сборкой под 4 цели —
без новых внешних зависимостей.

---

## 1. Обзор механизма per-OS

| Аспект | Linux (systemd) | macOS (launchd) |
|--------|-----------------|-----------------|
| Формат описания | INI-подобный unit (`text/template`) | XML plist (`text/template`) |
| Расположение unit/plist | `/etc/systemd/system/raxd.service` | `/Library/LaunchDaemons/tech.oem.raxd.plist` |
| Drop-in журнал | `/etc/systemd/journald.conf.d/raxd.conf` | `/etc/newsyslog.d/raxd.conf` (вне Docker, AC13) |
| Менеджер сервиса | `systemctl` | `launchctl` |
| Создание пользователя | `useradd --system` (идемпотентно) | `dscl` / готовый аккаунт (вне Docker, AC13) |
| Каталог состояния | `StateDirectory=raxd` + `StateDirectoryMode=0700` → `/var/lib/raxd` | `install -d -m 0700 -o raxd /usr/local/var/raxd` при install |
| Каталог конфига | `ConfigurationDirectory=raxd` + `ConfigurationDirectoryMode=0700` (BUG-1) → `/etc/raxd` | `install -d -m 0700 -o raxd /usr/local/etc/raxd` при install (BUG-1) |
| Каталог логов (macOS) | n/a (journald) | `install -d -m 0700 -o raxd /usr/local/var/log/raxd` при install |
| Env пути | `Environment=XDG_*` в unit | `EnvironmentVariables` в plist |
| Capability для <1024 | `AmbientCapabilities=CAP_NET_BIND_SERVICE` (условно) | root-bind / socket activation (открытый вопрос, вне Docker) |
| Ротация журнала | journald drop-in `SystemMaxUse=` / `SystemMaxFileSize=` | `StandardErrorPath` + newsyslog (вне Docker) |
| Проверяемость | Docker-контейнер с systemd (AC16) | unit-тесты генератора + реальный macOS (AC13) |

---

## 2. Точное содержимое systemd unit

### 2.1. Вариант «Дефолт» (порт ≥ 1024, полный hardening)

```ini
# /etc/systemd/system/raxd.service
# Сгенерировано raxd service install. Не редактировать вручную.
# Regenerate: raxd service install (idempotent, AC9).

[Unit]
Description=raxd — Remote Access Daemon for AI agents (OEM TECH)
After=network.target
Documentation=https://github.com/vladimirvkhs/raxd

[Service]
Type=exec
ExecStart={{.ExecPath}} serve
User={{.User}}
Group={{.Group}}

# Restart on failure only (AC4); graceful SIGTERM → clean exit → no restart (AC5).
# systemd.service(5): SIGHUP/SIGINT/SIGTERM/SIGPIPE count as clean exit for on-failure.
Restart=on-failure
RestartSec=2s

# State directory: systemd creates /var/lib/raxd with owner=raxd before start.
# StateDirectoryMode=0700 is EXPLICIT — default is 0755, which is wider than baseline §2.
StateDirectory=raxd
StateDirectoryMode=0700

# Config directory: systemd creates /etc/raxd with owner=raxd before ExecStart.
# REQUIRED to fix BUG-1: config.EnsureDirs calls MkdirAll(ConfigDir=/etc/raxd).
# Under ProtectSystem=strict /etc is read-only for the process, so MkdirAll would
# fail if /etc/raxd does not exist. ConfigurationDirectory creates it as root before
# the process starts and transfers ownership to User=raxd. Once /etc/raxd exists,
# MkdirAll is a no-op (directory present) → EnsureDirs succeeds → serve starts.
# raxd only READS config.yaml from ConfigDir; it never writes there after startup.
# ConfigurationDirectoryMode=0700 satisfies SR-89 (baseline §2 "0700 for state dirs").
ConfigurationDirectory=raxd
ConfigurationDirectoryMode=0700

# Path environment: overrides XDG defaults so internal/config/paths.go resolves
# /etc/raxd (ConfigDir) and /var/lib/raxd (StateDir) without code changes (ADR-002).
Environment=XDG_CONFIG_HOME=/etc
Environment=XDG_STATE_HOME=/var/lib
Environment=HOME=/var/lib/raxd

# Journal: stderr → journald (StandardError default for Type=exec is journal).
# Explicit to satisfy architect-guardian remark and AC8 documentation.
StandardOutput=journal
StandardError=journal
SyslogIdentifier=raxd

# Hardening (SR-87): present for BOTH port variants; ProtectSystem/Home/PrivateTmp
# remain even when NoNewPrivileges is omitted (port<1024, compensates SR-86).
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
```

Параметризуемые поля: `{{.ExecPath}}` (абсолютный путь к бинарю, валидируется перед рендером),
`{{.User}}` / `{{.Group}}` (POSIX-имя, allowlist `[a-z_][a-z0-9_-]*`, SR-90).

### 2.2. Вариант «Привилегированный порт» (порт < 1024, ADR-003)

Отличие от дефолта — **убирается `NoNewPrivileges=yes`** и добавляется блок capability:

```ini
# ... [Unit] и первая часть [Service] идентичны дефолту,
# включая StateDirectory/StateDirectoryMode и ConfigurationDirectory/ConfigurationDirectoryMode ...

# Ambient capability: присутствует ТОЛЬКО при Port<1024 (NeedNetBindCap=true).
# NoNewPrivileges НЕ ставится при наличии AmbientCapabilities (ADR-003, принятое
# отклонение П-1). Остальной hardening СОХРАНЯЕТСЯ (SR-87).
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE

ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
```

Условие в шаблоне: `{{if .NeedNetBindCap}}` / `{{else}}`. Поле `NeedNetBindCap bool` деривируется
из `Port < 1024` — типизированное, не строковое (SR-90 «условные директивы из bool, не строки»).

### 2.3. Полный шаблон unit (псевдокод `text/template`)

```
[Unit]
Description=raxd — Remote Access Daemon for AI agents (OEM TECH)
After=network.target
Documentation=https://github.com/vladimirvkhs/raxd

[Service]
Type=exec
ExecStart={{.ExecPath}} serve
User={{.User}}
Group={{.Group}}
Restart=on-failure
RestartSec=2s
StateDirectory=raxd
StateDirectoryMode=0700
ConfigurationDirectory=raxd
ConfigurationDirectoryMode=0700
Environment=XDG_CONFIG_HOME=/etc
Environment=XDG_STATE_HOME=/var/lib
Environment=HOME=/var/lib/raxd
StandardOutput=journal
StandardError=journal
SyslogIdentifier=raxd
{{- if .NeedNetBindCap}}
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE
{{- else}}
NoNewPrivileges=yes
{{- end}}
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
```

---

## 3. journald drop-in для ограничения роста журнала (AC8, SR-94)

### 3.1. Файл drop-in

```ini
# /etc/systemd/journald.conf.d/raxd.conf
# Installed by: raxd service install
# Removed by:   raxd service uninstall (SR-93)
# Purpose: limit audit log growth (closes command-exec OR-2 / file-upload OR-U4)
#
# NOTE: journald limits are per-host (global), not per-unit (ADR-004, П-3).
# For a dedicated raxd host/container this is sufficient. Per-unit limit
# (logrotate) is documented as fallback in ops docs.

[Journal]
SystemMaxUse=200M
SystemMaxFileSize=50M
```

Значения `SystemMaxUse=200M` и `SystemMaxFileSize=50M` — **рабочие дефолты** для production.

### 3.2. Рецепт теста AC8 (занижение порога в контейнере)

```bash
# 1. При установке в контейнере создать drop-in с заниженным порогом
cat > /etc/systemd/journald.conf.d/raxd.conf <<'EOF'
[Journal]
SystemMaxUse=5M
SystemMaxFileSize=1M
EOF

# 2. Перезапустить journald для применения
systemctl restart systemd-journald

# 3. Заполнить журнал синтетическими записями (>5M)
for i in $(seq 1 50000); do
  logger -t raxd "SYNTHETIC audit msg $i payload padding $(head -c 100 /dev/urandom | base64)"
done

# 4. Проверить: рост ОГРАНИЧЕН
journalctl --disk-usage
# Ожидание: Archived and active journals take up ~5.0M in the filesystem.
# (не растёт выше SystemMaxUse)

# 5. Подтвердить, что старые записи вытеснены (vacuum произошёл)
journalctl --vacuum-size=5M
# Вывод: Vacuuming done, freed Xm of archived journals from /var/log/journal.
```

Критерий приёмки AC8: `journalctl --disk-usage` после наполнения ≤ `SystemMaxUse` + ~10% служебного.

---

## 4. Точное содержимое launchd plist (macOS, AC2/AC13)

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
    "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- /Library/LaunchDaemons/tech.oem.raxd.plist -->
<!-- Generated by: raxd service install. Do not edit manually. -->
<!-- Label: {{.Label}} -->
<plist version="1.0">
<dict>
    <!-- Required: unique job identifier (reverse DNS) -->
    <key>Label</key>
    <string>{{.Label}}</string>

    <!-- Command: raxd serve (AC1) -->
    <key>ProgramArguments</key>
    <array>
        <string>{{.ExecPath}}</string>
        <string>serve</string>
    </array>

    <!-- Autostart at load/boot (AC3) -->
    <key>RunAtLoad</key>
    <true/>

    <!-- Restart on failure only: SuccessfulExit=false means restart when exit!=0.
         Graceful stop returns code 0 → NOT restarted (AC5).
         Kill / panic → exit!=0 → restarted (AC4). -->
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>

    <!-- Non-root execution: launchd starts as root, drops to UserName (AC6, SR-83) -->
    <key>UserName</key>
    <string>{{.User}}</string>

    <key>GroupName</key>
    <string>{{.Group}}</string>

    <!-- Environment: XDG paths derived from DefaultConfigForGOOS("darwin").
         Values are NOT hardcoded in the template — they come from TemplateData fields:
           {{.ConfigHome}} = filepath.Dir(ConfigDir) = /usr/local/etc
           {{.StateHome}}  = filepath.Dir(StateDir)  = /usr/local/var
           {{.StateDir}}   = /usr/local/var/raxd
         This ensures XDG resolution in internal/config/paths.go yields the same
         directories that install created (inversion invariant, see §4 note below).
         macOS /etc is system read-only — /usr/local/etc is the correct prefix (BUG-1). -->
    <key>EnvironmentVariables</key>
    <dict>
        <key>XDG_CONFIG_HOME</key>
        <string>{{.ConfigHome}}</string>
        <key>XDG_STATE_HOME</key>
        <string>{{.StateHome}}</string>
        <key>HOME</key>
        <string>{{.StateDir}}</string>
    </dict>

    <!-- Working directory -->
    <key>WorkingDirectory</key>
    <string>{{.StateDir}}</string>

    <!-- Log output (macOS has no journald; rotation via newsyslog, see §6.3) -->
    <key>StandardOutPath</key>
    <string>{{.LogPath}}/raxd.log</string>

    <key>StandardErrorPath</key>
    <string>{{.LogPath}}/raxd.log</string>
</dict>
</plist>
```

**Параметризуемые поля:**

| Поле | Описание | darwin-дефолт | linux-дефолт |
|------|----------|---------------|--------------|
| `{{.Label}}` | Reverse-DNS идентификатор | `tech.oem.raxd` | n/a |
| `{{.ExecPath}}` | Абсолютный путь к бинарю | — | — |
| `{{.User}}` / `{{.Group}}` | Сервис-пользователь | `raxd` | `raxd` |
| `{{.StateDir}}` | Полный путь каталога состояния | `/usr/local/var/raxd` | `/var/lib/raxd` |
| `{{.ConfigHome}}` | `filepath.Dir(ConfigDir)` → XDG_CONFIG_HOME | `/usr/local/etc` | n/a (systemd unit) |
| `{{.StateHome}}` | `filepath.Dir(StateDir)` → XDG_STATE_HOME | `/usr/local/var` | n/a (systemd unit) |
| `{{.LogPath}}` | Каталог лог-файла | `/usr/local/var/log/raxd` | n/a (journald) |

Дефолты darwin берутся из `DefaultConfigForGOOS("darwin")` в коде. Все значения валидируются
перед рендером (SR-90).

**Инвариант XDG ↔ install (BUG-1):** значения `XDG_CONFIG_HOME={{.ConfigHome}}` и
`XDG_STATE_HOME={{.StateHome}}` в plist деривируются как `filepath.Dir` от реальных ConfigDir/StateDir
(тех же, что создаёт install). Это гарантирует: `XDG_*_HOME + "/raxd"` в `internal/config/paths.go`
разрешается ровно в тот каталог, который существует на диске. Расхождения между env и созданными
каталогами исключены структурно.

**Каталоги создаются при install явно** (нет StateDirectory/ConfigurationDirectory-аналога в launchd).
Инвариант: создаваемый путь == `XDG_*_HOME + "/raxd"` из plist.

```bash
# install выполняет как root (darwin-дефолты из DefaultConfigForGOOS("darwin")):
install -d -m 0700 -o raxd -g raxd /usr/local/var/raxd      # StateDir
install -d -m 0700 -o raxd -g raxd /usr/local/var/log/raxd  # LogPath
# BUG-1 fix: ConfigDir должен существовать до старта (EnsureDirs → MkdirAll → no-op).
# /usr/local/etc доступен для создания подкаталогов; /etc — системный read-only на macOS.
install -d -m 0700 -o raxd -g raxd /usr/local/etc/raxd      # ConfigDir
```

**Примечание о macOS путях (DefaultConfigForGOOS, ADR-002):** на Linux `XDG_CONFIG_HOME=/etc` и
`XDG_STATE_HOME=/var/lib` — FHS-пути, управляемые systemd через `ConfigurationDirectory`/`StateDirectory`.
На macOS используется `/usr/local`-префикс (стандарт для сторонних демонов): `ConfigDir=/usr/local/etc/raxd`,
`StateDir=/usr/local/var/raxd`, `LogPath=/usr/local/var/log/raxd`. Значения в plist — переменные
`{{.ConfigHome}}`/`{{.StateHome}}`, не хардкод, что позволяет переопределить пути при install без
правки шаблона.

**Права plist:** владелец `root:wheel`, режим `0644` (SR-88).

**macOS-ограничение проверки (AC13):** интеграция launchd НЕпроверяема в Docker (Linux-контейнер).
Обязательны: (а) unit-тесты `renderPlist` (структура, `KeepAlive.SuccessfulExit=false`, `UserName`,
`EnvironmentVariables`, права); (б) ручной/CI-прогон на реальном macOS вне Docker.

---

## 5. Lifecycle демона

### 5.1. Install (AC1, AC3, AC9, AC11)

```
1. Проверить права (root/sudo) → ErrPermission если нет
2. Проверить: сервис уже установлен? → ErrAlreadyInstalled (AC9)
3. Создать системного пользователя raxd (идемпотентно):
   Linux: useradd --system --no-create-home --shell /usr/sbin/nologin raxd
          (если exit 9 = "already exists" → OK, не ошибка)
   macOS: проверить через dscl; создать если нет (открытый вопрос, AC13)
4. macOS: создать каталоги явно по DefaultConfigForGOOS("darwin")
   (нет StateDirectory/ConfigurationDirectory-аналога в launchd):
   install -d -m 0700 -o raxd -g raxd /usr/local/var/raxd      ← StateDir
   install -d -m 0700 -o raxd -g raxd /usr/local/var/log/raxd  ← LogPath
   install -d -m 0700 -o raxd -g raxd /usr/local/etc/raxd      ← ConfigDir (BUG-1 fix)
   Идемпотентно: install -d на существующем каталоге обновляет права, не падает.
   Инвариант: эти пути == XDG_STATE_HOME/raxd и XDG_CONFIG_HOME/raxd из plist.
5. Загрузить конфиг (config.Load) → определить Port → NeedNetBindCap = Port < 1024
6. Рендер unit/plist из шаблона с валидацией полей (SR-90)
7. Записать unit/plist в системный каталог (root:root/wheel 0644) — откатная точка A
8. Linux: записать drop-in /etc/systemd/journald.conf.d/raxd.conf (root:root 0644) — откатная точка B
9. Linux: systemctl daemon-reload
   macOS: launchctl bootstrap system /Library/LaunchDaemons/tech.oem.raxd.plist
10. Linux: systemctl enable raxd (автозапуск при загрузке, AC3)
    macOS: launchctl enable system/tech.oem.raxd
11. (Опционально) systemctl start raxd / launchctl kickstart system/tech.oem.raxd
```

**Почему ConfigDir должен существовать до старта (BUG-1):** `raxd serve` вызывает
`config.EnsureDirs`, которая выполняет `MkdirAll(ConfigDir)`. На Linux с `ProtectSystem=strict`
каталог `/etc` — read-only для процесса, поэтому создать `/etc/raxd` из-под `raxd` невозможно
→ `permission denied` → crash-loop. Решение: `ConfigurationDirectory=raxd` в unit даёт systemd
создать `/etc/raxd` как root до запуска ExecStart, с передачей владения `User=raxd`. После этого
`MkdirAll` в `EnsureDirs` — **no-op** (каталог уже существует, `os.MkdirAll` на существующем
каталоге возвращает nil). На macOS — `install -d` при install по той же причине (шаг 4).

**Откат при сбое (AC11):** при ошибке на шагах 7-11 удаляются unit/plist и drop-in (созданные
НА ЭТОМ запуске); пользователь `raxd` и каталоги данных НЕ удаляются (ADR-002 — переиспользуются).
Откатные точки A и B: если сбой после шага 7 но до 8 — удалить unit/plist; если после шага 8 —
удалить unit/plist и drop-in. После отката: сообщение `error: <причина>\n  hint: <подсказка>`,
ненулевой код (AC12).

### 5.2. Uninstall (AC10, SR-93)

```
1. Проверить права → ErrPermission если нет
2. Проверить: сервис установлен? → ErrNotInstalled если нет (AC10, «предсказуемый результат»)
3. Linux: systemctl stop raxd (игнорировать «already stopped»)
   macOS: launchctl bootout system/tech.oem.raxd (игнорировать «not found»)
4. Linux: systemctl disable raxd
   macOS: launchctl disable system/tech.oem.raxd
5. Linux: systemctl daemon-reload
6. Удалить unit/plist: /etc/systemd/system/raxd.service / /Library/LaunchDaemons/tech.oem.raxd.plist
7. Linux: удалить drop-in /etc/systemd/journald.conf.d/raxd.conf (SR-93)
8. Linux: systemctl daemon-reload (повторно, после удаления unit)
9. Status → Installed:false (SR-93: «артефактов регистрации нет»)
```

**ОСОЗНАННО ОСТАЁТСЯ:** системный пользователь `raxd:raxd` без login-shell, без автозапуска
(ADR-002, принятое отклонение П-2). Документируется tech-writer.

### 5.3. Start / Stop / Status

```
Start:
  1. Проверить: установлен? → ErrNotInstalled если нет
  2. systemctl start raxd / launchctl kickstart system/tech.oem.raxd

Stop:
  1. Проверить: установлен? → ErrNotInstalled если нет
  2. systemctl stop raxd / launchctl bootout system/tech.oem.raxd
  # stop → SIGTERM → graceful shutdown → код 0 → Restart=on-failure НЕ срабатывает (AC5)

Status:
  1. Не установлен → Status{Installed: false} без ошибки (AC10)
  2. systemctl show -p MainPID,ActiveState,SubState,UnitFileState raxd
     launchctl print system/tech.oem.raxd
  3. Вернуть Status{Installed, Active bool; PID, EUID int; State string}
  4. EUID берётся из /proc/<PID>/status (Linux) для AC6-проверки euid!=0
```

### 5.4. Идемпотентность и безопасность

| Операция | Уже выполнена | Ещё не выполнена |
|----------|---------------|-----------------|
| Install при установленном | `ErrAlreadyInstalled` (AC9) | — |
| Uninstall при неустановленном | `ErrNotInstalled` (AC10) | — |
| Start при запущенном | 0 / already active (не ошибка) | — |
| Stop при остановленном | 0 / already stopped (не ошибка) | — |
| `useradd` при существующем пользователе | exit 9 → OK, пропустить | — |

---

## 6. Создание сервис-пользователя

### 6.1. Linux

```bash
# Идемпотентное создание (SR-83):
useradd \
  --system \
  --no-create-home \
  --shell /usr/sbin/nologin \
  --comment "raxd daemon" \
  raxd

# exit code 0  → создан
# exit code 9  → уже существует → OK (переиспользуем без модификации)
# иной код    → ошибка → откат install (AC11)
```

Результат: пользователь `raxd` с UID в системном диапазоне (обычно < 1000), без login-shell,
без обычного home. Группа `raxd` создаётся автоматически (дефолт `useradd --system`).

### 6.2. macOS (AC13 — нюанс)

Прямого аналога `useradd --system` на macOS нет. Варианты:

a. `dscl . -create /Users/raxd` + установка `UniqueID`, `PrimaryGroupID`, `UserShell=/usr/bin/false`,
   `RealName="raxd daemon"`, `NFSHomeDirectory=/var/lib/raxd` — полноценный системный пользователь.
b. Использовать существующий непривилегированный системный аккаунт (например `_raxd` в стиле
   macOS-системных демонов с префиксом `_`).

Выбор и точная команда — **открытый вопрос для проверки на реальном macOS** (AC13, ОР-4). Факт-граница:
`UserName=` в `LaunchDaemons` работает с любым существующим непривилегированным пользователем
(launchd.plist(5)). В интеграционных тестах macOS необходимо убедиться, что после install euid!=0.

---

## 7. Capability для порта < 1024 (AC7, SR-85/SR-86, ADR-003)

### 7.1. Как генератор узнаёт порт

При `raxd service install` вызывается `config.Load(paths)` — тот же код, что читает `raxd serve`.
Из `cfg.Port` вычисляется `NeedNetBindCap = cfg.Port < 1024`. Это типизированное bool-поле
`TemplateData` (SR-90 — «условные директивы деривируются из bool, не строки»).

### 7.2. Почему ambient, а не setcap (ADR-003)

- `setcap` хранит capability в xattr бинаря → **теряется при замене бинаря** при обновлении raxd.
- `AmbientCapabilities` выдаётся systemd при каждом старте, не зависит от xattr (переживает обновления).
- file-caps очищают ambient set при execve («executing a program that has any file capabilities set
  will clear the ambient set», capabilities(7)).
- Для дефолтного порта 7822 ≥ 1024 — capability не генерируется вовсе.

### 7.3. Дефолт vs привилегированный порт

| Порт | NoNewPrivileges | AmbientCapabilities | CapabilityBoundingSet |
|------|-----------------|---------------------|----------------------|
| ≥ 1024 (7822) | `yes` | не ставится | не ставится |
| < 1024 | не ставится (П-1) | `CAP_NET_BIND_SERVICE` | `CAP_NET_BIND_SERVICE` |

`CapabilityBoundingSet=CAP_NET_BIND_SERVICE` при <1024 ограничивает набор возможных capabilities
только до нужной, ещё сужая профиль.

### 7.4. macOS (открытый вопрос)

launchd-daemon стартует от root и сбрасывает привилегии через `UserName=`. Механика порта <1024
(root биндит до сброса / socket activation через `Sockets`-ключ plist) — открытый вопрос для
проверки на реальном macOS (AC13). Дефолт 7822 делает этот случай нерелевантным по умолчанию.

---

## 8. Кросс-сборка (AC14, AC15, SR-96)

### 8.1. Матрица целей

| GOOS | GOARCH | Арtefact name | Бинарный формат |
|------|--------|---------------|-----------------|
| linux | amd64 | raxd_linux_amd64 | ELF 64-bit LSB, x86-64 |
| linux | arm64 | raxd_linux_arm64 | ELF 64-bit LSB, AArch64 |
| darwin | amd64 | raxd_darwin_amd64 | Mach-O 64-bit x86_64 |
| darwin | arm64 | raxd_darwin_arm64 | Mach-O 64-bit arm64 |

`CGO_ENABLED=0` — статический бинарь, C-тулчейн не нужен (raxd чисто Go).
`-mod=vendor` — офлайн из `vendor/`, без сетевого `go mod download` (AC15, baseline §6).

### 8.2. Команды сборки

```bash
# Одна цель (пример):
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -mod=vendor \
  -o dist/raxd_linux_amd64 \
  ./cmd/raxd

# Все 4 цели:
for goos in linux darwin; do
  for goarch in amd64 arm64; do
    CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build \
      -mod=vendor \
      -o dist/raxd_${goos}_${goarch} \
      ./cmd/raxd
  done
done
```

### 8.3. Критерий приёмки (spec Q8/AC14)

- **Все 4 цели** компилируются без ошибок.
- **Нативный бинарь** для контейнера (`linux/amd64` или `linux/arm64`) запускается и проходит
  базовую проверку: `./dist/raxd_linux_<arch> version` → выводит версию, код 0.
- **Остальные 3 бинаря** — проверка формата через `file`:
  - `raxd_linux_arm64`: `ELF 64-bit LSB … ARM aarch64`
  - `raxd_darwin_amd64`: `Mach-O 64-bit x86_64 executable`
  - `raxd_darwin_arm64`: `Mach-O 64-bit arm64 executable`
- Примечание: `go version <binary>` выдаёт только версию Go, не GOOS/GOARCH → для проверки цели
  используем `file` (подтверждено research Q8).

Эмуляция QEMU для arm64-под-amd64 — избыточна (AC14 «cross-target = факт компиляции + файловый
формат»).

---

## 9. Рецепт systemd-в-Docker (AC16, baseline §6)

### 9.1. Базовый образ и запуск контейнера

Используется собственный `Dockerfile.systemd` (§10 — файл создан в репозитории).
Образ на основе `ubuntu:22.04` с systemd как PID 1.

```bash
# Сборка образа (из корня репозитория):
docker build -f Dockerfile.systemd -t raxd-systemd-test .

# Запуск контейнера с systemd:
docker run -d \
  --name raxd-svc-test \
  --privileged \
  --cgroupns=host \
  -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
  raxd-systemd-test

# Войти в контейнер:
docker exec -it raxd-svc-test bash
```

**Примечание о cgroup v2:** на современных хостах (Linux ≥ 5.4, Docker Desktop macOS) используется
cgroup v2. Флаги `--cgroupns=host -v /sys/fs/cgroup:/sys/fs/cgroup:rw` обеспечивают корректную
работу systemd в контейнере. На хостах с cgroup v1 использовать `:ro` вместо `:rw`.

### 9.2. Полный сценарий интеграции (AC1-AC12, AC16)

```bash
# Внутри контейнера (после сборки raxd и копирования бинаря):

# 0. Подготовка: скопировать нативный бинарь
cp /src/dist/raxd_linux_amd64 /usr/local/bin/raxd
chmod 0755 /usr/local/bin/raxd

# 1. Install (AC1, AC2, AC3, AC9)
raxd service install
# Ожидание: пользователь raxd создан, unit установлен enabled

# 2. Проверить автозапуск при загрузке (AC3)
systemctl is-enabled raxd
# Ожидание: enabled

# 3. Start + проверить не-root (AC6)
raxd service start
PID=$(systemctl show -p MainPID raxd | cut -d= -f2)
grep Uid /proc/$PID/status
# Ожидание: Uid: NNNN NNNN NNNN NNNN  (все != 0)

# 4. Проверить порт 7822 (AC6)
# (raxd должен listen на 127.0.0.1:7822 — TLS соединение откажет без ключа,
#  но порт открыт без root)
ss -tlnp | grep 7822

# 5. Перезапуск при сбое (AC4)
OLD_PID=$PID
kill -9 $PID
sleep 3  # RestartSec=2s + margin
NEW_PID=$(systemctl show -p MainPID raxd | cut -d= -f2)
test "$NEW_PID" != "$OLD_PID" && echo "AC4 OK: restarted, new PID=$NEW_PID"
grep Uid /proc/$NEW_PID/status  # euid!=0 после рестарта

# 6. Graceful stop (AC5)
raxd service stop
systemctl is-active raxd
# Ожидание: inactive (не перезапустился после SIGTERM)

# 7. Uninstall (AC10)
raxd service uninstall
systemctl is-enabled raxd 2>&1 || echo "AC10 OK: not enabled"
test ! -f /etc/systemd/system/raxd.service && echo "unit removed"
test ! -f /etc/systemd/journald.conf.d/raxd.conf && echo "drop-in removed"
# Проверить: пользователь raxd сохранён (П-2)
id raxd

# 8. Идемпотентность install (AC9)
raxd service install  # установить заново
raxd service install  # повторно → ErrAlreadyInstalled
# Ожидание: второй install печатает ошибку + ненулевой код

# 9. Тест AC8 (ротация журнала)
# Занизить порог (raxd уже uninstalled, можно пересоздать drop-in вручную для теста):
mkdir -p /etc/systemd/journald.conf.d
cat > /etc/systemd/journald.conf.d/raxd.conf <<'EOF'
[Journal]
SystemMaxUse=5M
SystemMaxFileSize=1M
EOF
systemctl restart systemd-journald
# Наполнить журнал:
raxd service install && raxd service start
for i in $(seq 1 30000); do logger -t raxd "SYNTHETIC $i $(head -c 80 /dev/urandom | base64)"; done
journalctl --disk-usage
# Ожидание: ≤ ~5M (с допуском на сжатие)

# 10. Сообщения об ошибках (AC12)
# install без прав (пример — запустить как raxd):
su -s /bin/sh raxd -c "raxd service install" 2>&1 | grep -E "^error:|hint:"
# Ожидание: error: ... + hint: ...
```

### 9.3. Проверка unit/plist с занижением порогов

AC8-тест рекомендуется выполнять в `Makefile`-таргете `test-service` (§10.3), который:
1. Запускает контейнер `raxd-systemd-test`.
2. Собирает нативный бинарь (`make build-linux-amd64`).
3. Прогоняет сценарий §9.2.
4. Выводит результат каждого шага с явным `OK/FAIL`.

---

## 10. Build-инфраструктура (файлы в репозитории)

### 10.1. `Dockerfile.systemd`

Образ для systemd-интеграционных тестов (AC16). Создан в корне репозитория.
Детали — см. сам файл `/Dockerfile.systemd`.

Ключевые аспекты образа:
- Базовый образ: `ubuntu:22.04`.
- Установлены: `systemd`, `dbus`, `procps`, `iproute2` (ss), `bsdutils` (logger, Ubuntu/Debian;
  на RHEL/Fedora — `util-linux`), `useradd` (из `passwd`), `file`.
- Маскированы systemd-юниты, нерелевантные в контейнере: `systemd-resolved.service`,
  `systemd-networkd.service`, `getty@.service`, `serial-getty@.service`.
- Entrypoint: `/sbin/init` (systemd как PID 1).
- Собранный бинарь `raxd` копируется при запуске сценария (не в образ — сборка отдельно).

### 10.2. `Makefile` — таргеты

Создан/дополнен `Makefile` в корне. Таргеты:

```
build-all           Кросс-сборка под все 4 цели (dist/)
build-linux-amd64   linux/amd64
build-linux-arm64   linux/arm64
build-darwin-amd64  darwin/amd64
build-darwin-arm64  darwin/arm64
verify-cross        Проверка форматов 4 артефактов (file + нативный version)
docker-systemd      Сборка образа raxd-systemd-test
test-service        Сборка образа + запуск сценария сервисной интеграции
```

Детали — см. файл `/Makefile`.

### 10.3. Проверка матрицы кросс-сборки

```bash
# В Docker (offline, vendor):
docker build --target build -t raxd-build .

# Внутри или через docker run:
make build-all     # 4 компиляции без ошибок
make verify-cross  # file + нативный ./dist/raxd_linux_$(uname -m) version
```

---

## 11. Валидация полей до рендера (SR-90, анти-инъекция)

Перед `renderUnit` / `renderPlist` каждое поле `TemplateData` проверяется:

| Поле | Правило валидации | Пример невалидного (→ ошибка) |
|------|-------------------|-------------------------------|
| `User`, `Group` | POSIX: `^[a-z_][a-z0-9_-]{0,31}$`, без `\n\r='"` | `"raxd\nExecStart=/bin/sh"` |
| `Label` | reverse-DNS: `^[a-z][a-z0-9._-]{0,253}$`, без пробелов/управляющих | `"x\nUserName=root"` |
| `ExecPath` | `filepath.IsAbs` + `filepath.Clean` + без `\n\r` и управляющих | `"/usr/bin/raxd\nUser=root"` |
| `StateDir`, `ConfigDir`, `LogPath` | `filepath.IsAbs` + `filepath.Clean` + без `\n\r` | `"/var/lib\nUser=root"` |
| `Port` | `1 ≤ Port ≤ 65535` | 0, -1, 99999 |
| `NeedNetBindCap` | bool (деривируется из `Port < 1024`) | строка «true» не принимается |

Невалидное значение → `ErrInvalidTemplateData` до записи артефакта (не «тихий» рендер с инъекцией).
Реализует developer в `internal/service/templates.go` (функция `validateTemplateData`).

---

## 12. Права на артефакты (SR-88, SR-89)

| Артефакт | Путь | Владелец | Режим |
|----------|------|----------|-------|
| unit | `/etc/systemd/system/raxd.service` | `root:root` | `0644` |
| drop-in | `/etc/systemd/journald.conf.d/raxd.conf` | `root:root` | `0644` |
| plist | `/Library/LaunchDaemons/tech.oem.raxd.plist` | `root:wheel` | `0644` |
| StateDir (Linux) | `/var/lib/raxd` | `raxd:raxd` | `0700` |
| StateDir (macOS) | `/usr/local/var/raxd` | `raxd:raxd` | `0700` |
| ConfigDir (Linux) | `/etc/raxd` | `raxd:raxd` | `0700` (SR-89, BUG-1 fix) |
| ConfigDir (macOS) | `/usr/local/etc/raxd` | `raxd:raxd` | `0700` (SR-89, BUG-1 fix) |
| LogDir (macOS) | `/usr/local/var/log/raxd` | `raxd:raxd` | `0700` |
| keys.db | `<StateDir>/keys.db` | `raxd:raxd` | `0600` |
| TLS private key | `<StateDir>/tls/*.key` | `raxd:raxd` | `0600` |

Запись unit/plist/drop-in выполняется от root (install требует прав). Пользователь `raxd`
не может перезаписать описание своего сервиса (SR-88).

---

## 13. Ограничение проверки macOS (AC13, SR-95)

Это НЕ снятие требований — AC1-AC12 применимы к обеим платформам как контракт.

**В Docker-контейнере (Linux):**
- Проверяются все Linux-критерии (AC1-AC12, AC16).
- Unit-тесты `renderPlist` + `renderUnit` (структура, параметры, условный cap, SR-85/SR-86/SR-87).
- Unit-тест `service.New` по GOOS (выбор реализации).

**На реальном macOS (вне Docker):**
- Полный сценарий install → euid!=0 → kill → restart → stop → uninstall.
- Проверка `UserName=raxd` (euid=UID пользователя raxd, ≠ 0).
- Ротация лога через newsyslog.
- Создание пользователя `raxd` через dscl.

Этот факт документируется в документации (tech-writer) и явно фиксируется в `plan.md` (architect).

---

## 14. Зависимости (SR-96, AC15)

Новых внешних зависимостей НЕТ. Реализация только на stdlib:

| Пакет | Назначение | Источник |
|-------|-----------|---------|
| `text/template` | Рендер unit/plist | stdlib |
| `os/exec` | Вызов systemctl/launchctl | stdlib |
| `embed` | Встраивание шаблонов (опц.) | stdlib |
| `runtime` | Выбор GOOS | stdlib |
| `os` | Пути, права, создание каталогов | stdlib |
| `context` | Таймауты вызовов менеджера | stdlib |
| `github.com/spf13/cobra` | CLI-группа service | уже в vendor/ |

`kardianos/service` — НЕ используется (ADR-001, STACK.ru.md обновлён).

---

## 15. Хэндофф

**Developer** читает этот документ для реализации `internal/service/*` и `internal/cli/service.go`:
- Шаблоны из §2.3 и §4 — точное содержимое шаблонов (`unitTemplate`, `plistTemplate`).
- Контракт `TemplateData` и валидация из §11.
- Lifecycle из §5 — порядок шагов install/uninstall/start/stop/status.
- Создание пользователя из §6.1 (Linux); команды menеджера из §5.

**DevOps** читает §8 (кросс-сборка), §9 (Docker-рецепт), §10 (Makefile/Dockerfile.systemd).

**QA** читает §9.2 (полный сценарий интеграции) и §3.2 (AC8-тест журнала).

**Tech-writer** документирует §5.2 (пользователь raxd остаётся после uninstall), §8.3 (критерий
cross-target), §13 (macOS-ограничение проверки), §7 (capability для <1024).
