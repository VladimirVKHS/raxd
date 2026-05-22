# UX Spec: service-install — консольный вывод команд `raxd service`

Автор: cli-ux (raxd). Задача: `service-install`. Источники истины: `spec.md` (AC1, AC9–AC12),
`plan.md` (CLI-контракт, типизированные ошибки), `security-requirements.md` (SR-95 — нет секретов
в выводе), `.claude/reference/STACK.ru.md` (конвенции вывода), существующий код
`internal/cli/{key,serve,status}.go`, `internal/banner/banner.go`.

---

## Принципы

### П-1. Сигнал — первым
Первая содержательная строка после баннера всегда сообщает результат: успех, уже установлен,
ошибка. Пользователь не должен читать несколько строк, чтобы понять, что произошло.

### П-2. Иерархия строк
```
1. Сигнал-строка    — успех / предупреждение / ошибка
2. Что изменилось   — что установлено/снято/запущено, где лежит
3. Ориентация       — что делать дальше (hint)
4. Audit-строка     — charmbracelet/log, только при действии (не при status)
```

### П-3. Выравнивание: %-12s для service-блоков
Существующий `status.go` использует `%-8s`. Для блоков команды `service` принят шаг `%-12s`
(метка 12 символов, значение с колонки 14 при 2-пробельном левом отступе) — достаточно для
`autostart`, `unit`, `manager`. Все метки строчные, без точки в конце.

```
  installed     raxd service
  unit          /etc/systemd/system/raxd.service
  user          raxd  [not root]
  port          7822
  autostart     enabled
```

### П-4. Три типа блоков вывода
- **Успех-блок**: метка-значение, 2-пробельный отступ, **stderr** (мутирующие команды:
  install/uninstall/start/stop). Начинается с сигнала, заканчивается подсказкой `hint:` о следующем
  шаге.
- **Уже-установлен / не-установлен**: только сигнальная строка + одна `hint:` в stderr. Никакого
  повторения деталей установки (AC9, AC10, П-2).
- **Ошибка-блок**: строчные `error: ...` + `  hint: ...` в stderr — по образцу `key.go`/`serve.go`.
  Без сырых трасс, без секретов (SR-95).
- **Status-блок**: query-команда `raxd service status` — первичный вывод (и человекочитаемый, и
  `--json`) → **stdout**, согласованно с поведением `raxd status` (`status.go`). Баннер, `error:`,
  `hint:` — по-прежнему в stderr.

### П-5. Потоки вывода
| Тип вывода                                          | Поток  |
|-----------------------------------------------------|--------|
| Баннер (`banner.Render()`)                          | stderr |
| Успех-блок мутирующих команд (install/uninstall/start/stop) | stderr |
| Уже-установлен / не-установлен (install/uninstall)  | stderr |
| `error:` / `hint:`                                  | stderr |
| `raxd service status` — человекочитаемый блок       | **stdout** |
| `raxd service status --json`                        | **stdout** |
| `charmbracelet/log` audit-строки                    | stderr |

**Разграничение `raxd service status` и `raxd status`:**
- `raxd status` (существующая команда, `internal/cli/status.go`) — показывает пути конфигов и
  состояние демона как foreground-процесса; вывод в stdout (так реализовано сейчас).
- `raxd service status` (новая команда) — показывает состояние системного сервиса (установлен ли,
  запущен ли менеджером, PID, EUID, автозапуск, путь unit/plist); вывод также в stdout, по тому же
  принципу. Обе команды — query, не мутируют; оба результата пригодны для скриптования через pipe.

### П-6. Кроссплатформенные метки
Метки нейтральны к платформе: `unit`, `manager`, `autostart` — не `systemd`/`launchd` в качестве
метки. Платформенные детали — только в значении (путь к unit/plist, имя менеджера). Это согласовано
с тем, что различия платформ скрыты внутри (AC1).

### П-7. Безопасность в выводе: нет секретов
Можно выводить: пути к unit/plist, имя пользователя `raxd`, порт, PID, статус, значение EUID.
Нельзя: тело API-ключа, приватный TLS-ключ, сырой stderr от `systemctl`/`launchctl`, stack trace
(SR-95, AC12). `runManager` захватывает сырой stderr менеджера внутри и заменяет типизированной
ошибкой.

### П-8. Акцент не-root — инлайн
Гарантия не-root — ключевой security-факт (AC6, SR-83). Выводится инлайном в строке `user`, без
отдельной строки-предупреждения:
```
  user          raxd  [not root]
```

### П-9. Баннер — на каждой команде, через PersistentPreRun
`raxd service install|uninstall|start|stop|status` — cobra-подкоманды. Баннер печатается
`PersistentPreRun` корневой команды (как сейчас). Никакого второго баннера внутри команды.

---

## Состояния вывода

### `raxd service install` — успех (первая установка)

Баннер уже напечатан PersistentPreRun. Далее — успех-блок в stderr:

```
  installed     raxd service
  unit          /etc/systemd/system/raxd.service
  drop-in       /etc/systemd/journald.conf.d/raxd.conf
  user          raxd  [not root]
  port          7822
  autostart     enabled
  hint: start the service now with "raxd service start"
```

Для macOS (launchd):
```
  installed     raxd service
  unit          /Library/LaunchDaemons/tech.oem.raxd.plist
  user          raxd  [not root]
  port          7822
  autostart     enabled
  hint: start the service now with "raxd service start"
```

После успех-блока — audit-строка `charmbracelet/log` в stderr (уровень `info`, logfmt):
```
time=2026-05-22T10:00:00Z level=info msg="service installed" action=install platform=linux unit=/etc/systemd/system/raxd.service user=raxd
```

Полный вид терминала (Linux, первая установка):
```
┌───────────────────────────────────────────────────────┐
│  raxd  —  Remote Access Daemon                        │
│  v0.4.0  ·  commit a1b2c3d  ·  built 2026-05-22      │
│  Vladimir Kovalev, OEM TECH                           │
└───────────────────────────────────────────────────────┘

  installed     raxd service
  unit          /etc/systemd/system/raxd.service
  drop-in       /etc/systemd/journald.conf.d/raxd.conf
  user          raxd  [not root]
  port          7822
  autostart     enabled
  hint: start the service now with "raxd service start"

time=2026-05-22T10:00:00Z level=info msg="service installed" action=install platform=linux unit=/etc/systemd/system/raxd.service user=raxd
```

Код выхода: `0`.

---

### `raxd service install` — идемпотентный повтор (AC9)

Сервис уже установлен. CLI-слой получает sentinel `ErrAlreadyInstalled` от менеджера и мапит его в
**exit 0** + информационный блок без `error:`-префикса (AC9: «операция безопасно завершается
успехом»). Никаких деталей установки повторно. Никакого audit-лога действия (ничего не изменилось).

```
  already installed   raxd service
  hint: use "raxd service status" to check the current state
```

Код выхода: `0`.

---

### `raxd service install` — сбой с откатом (AC11)

Установка прервана на полпути; откат выполнен; система в исходном состоянии. Ошибка:

```
error: service installation failed: could not register the service
  hint: check that the service manager is running and try again
  hint: run as root or with sudo: sudo raxd service install
```

Если сбой произошёл после частичной записи артефактов — выводится дополнительная строка
об откате (информационная, не ошибка):

```
error: service installation failed: could not enable autostart
  hint: the installer has removed any partially created files
  hint: run as root or with sudo: sudo raxd service install
```

Никакого вывода имён временных файлов, сырых ошибок `systemctl`, stack trace (SR-95).

Код выхода: `1`.

---

### `raxd service install` — нет прав (SR-84, AC12)

Запуск без достаточных привилегий:

```
error: insufficient privileges to install the service
  hint: run as root or with sudo: sudo raxd service install
  hint: installation requires root to write system service files
```

Код выхода: `1`.

> Пояснение к хинту: install требует root для записи в системные каталоги, но сам демон
> будет работать под непривилегированным пользователем `raxd` — это различие отражено в
> макете `raxd service status` (строка `user raxd [not root]`).

---

### `raxd service uninstall` — успех

Автозапуск снят, unit/plist/drop-in удалены. Осознанно остался пользователь `raxd` (П-2 в
`security-requirements.md`):

```
  uninstalled   raxd service
  removed       unit file and autostart registration
  removed       journal size limit drop-in
  kept          system user "raxd" (no shell, no home, not running)
  hint: to also remove the user: sudo userdel raxd
  hint: data in /var/lib/raxd is preserved — remove manually if no longer needed
```

Для macOS:
```
  uninstalled   raxd service
  removed       plist file and autostart registration
  kept          system user "raxd" (no shell, no home, not running)
  hint: to also remove the user: sudo dscl . -delete /Users/raxd
  hint: data in /var/lib/raxd is preserved — remove manually if no longer needed
```

Строка `kept` объясняет, почему пользователь остался — это осознанное поведение, не забытый
артефакт (SR-93, П-2). Audit-строка после блока:

```
time=2026-05-22T10:05:00Z level=info msg="service uninstalled" action=uninstall platform=linux
```

Код выхода: `0`.

---

### `raxd service uninstall` — идемпотентный повтор (AC10)

Сервис не установлен. CLI-слой получает sentinel `ErrNotInstalled` от менеджера и мапит его в
**exit 0** + информационный блок без `error:`-префикса (AC10: «операция безопасно завершается»).

```
  not installed   raxd service
  hint: use "raxd service install" to set up the service
```

Код выхода: `0`.

---

### `raxd service uninstall` — частичное состояние

Обнаружены некоторые артефакты (например, unit-файл есть, но сервис не зарегистрирован):

```
  uninstalled   raxd service (partial state cleaned up)
  removed       /etc/systemd/system/raxd.service
  hint: run "raxd service status" to verify the service is fully removed
```

Код выхода: `0` (очистка выполнена успешно).

---

### `raxd service start` — успех

```
  started       raxd service
  pid           1234
  hint: check status with "raxd service status"
```

Audit:
```
time=2026-05-22T10:10:00Z level=info msg="service started" action=start pid=1234
```

Код выхода: `0`.

---

### `raxd service start` — уже запущен

```
  already running   raxd service (pid 1234)
  hint: use "raxd service stop" to stop it
```

Код выхода: `0`.

---

### `raxd service start` — сервис не установлен

```
error: raxd service is not installed
  hint: install it first with "raxd service install"
```

Код выхода: `1`.

---

### `raxd service start` — сбой запуска

```
error: failed to start the service
  hint: check the service logs for details
  hint: run "raxd service status" to see the current state
```

Никакого вывода сырого stderr `systemctl start` (SR-95).

Код выхода: `1`.

---

### `raxd service stop` — успех

```
  stopped       raxd service
  hint: start again with "raxd service start"
```

Audit:
```
time=2026-05-22T10:15:00Z level=info msg="service stopped" action=stop
```

Код выхода: `0`.

---

### `raxd service stop` — уже остановлен

```
  already stopped   raxd service
  hint: start again with "raxd service start"
```

Код выхода: `0`.

---

### `raxd service stop` — сервис не установлен

```
error: raxd service is not installed
  hint: install it first with "raxd service install"
```

Код выхода: `1`.

---

### `raxd service stop` — сбой остановки

```
error: failed to stop the service
  hint: check the service logs for details
  hint: run "raxd service status" to see the current state
```

Никакого вывода сырого stderr `systemctl stop` (SR-95).

Код выхода: `1`.

---

### `raxd service status` — человекочитаемый блок (stdout)

Query-команда; весь первичный вывод — в **stdout** (согласованно с `raxd status` в `status.go`).
Баннер по-прежнему в stderr (через `PersistentPreRun`). Ошибки и `hint:` — в stderr.

Сервис установлен и запущен (полное состояние):

```
  installed     yes
  running       yes
  pid           1234
  euid          1001
  user          raxd  [not root]
  port          7822
  autostart     enabled
  unit          /etc/systemd/system/raxd.service
  manager       systemd
  state         active (running)
```

Сервис установлен, но остановлен:

```
  installed     yes
  running       no
  pid           -
  user          raxd  [not root]
  port          7822
  autostart     enabled
  unit          /etc/systemd/system/raxd.service
  manager       systemd
  state         inactive (dead)
  hint: start with "raxd service start"
```

> Примечание: строка `hint:` при `running: no` выводится в **stdout** вместе с остальным блоком
> (не в stderr) — она часть читаемого статус-блока, а не сообщение об ошибке.

Сервис не установлен:

```
  installed     no
  hint: install with "raxd service install"
```

Полный вид терминала (сервис запущен, Linux; баннер идёт в stderr, статус — в stdout):
```
┌───────────────────────────────────────────────────────┐   ← stderr
│  raxd  —  Remote Access Daemon                        │
│  v0.4.0  ·  commit a1b2c3d  ·  built 2026-05-22      │
│  Vladimir Kovalev, OEM TECH                           │
└───────────────────────────────────────────────────────┘

  installed     yes                                         ← stdout
  running       yes
  pid           1234
  euid          1001
  user          raxd  [not root]
  port          7822
  autostart     enabled
  unit          /etc/systemd/system/raxd.service
  manager       systemd
  state         active (running)
```

Код выхода: `0` (в том числе когда `installed: no` — статус-запрос, не ошибка).

---

### `raxd service status --json` — машиночитаемый вывод (stdout)

Флаг `--json` переключает вывод на JSON в **stdout**; человекочитаемый блок не печатается.
Баннер по-прежнему идёт в stderr и не мешает парсингу stdout скриптами.

```json
{
  "installed": true,
  "active": true,
  "pid": 1234,
  "euid": 1001,
  "user": "raxd",
  "state": "active (running)",
  "port": 7822,
  "autostart": "enabled",
  "unit_path": "/etc/systemd/system/raxd.service",
  "manager": "systemd"
}
```

Не установлен:
```json
{
  "installed": false,
  "active": false,
  "pid": 0,
  "euid": 0,
  "user": "",
  "state": "not installed",
  "port": 0,
  "autostart": "disabled",
  "unit_path": "",
  "manager": "systemd"
}
```

Поля зеркалят структуру `Status` из `plan.md §Contracts`, дополненную `port`, `autostart`,
`unit_path`, `manager` для полноты статусного блока.

---

## Баннер автора

Баннер печатается через `banner.Render()` в `PersistentPreRun` корневой команды (уже реализовано
в `root.go`). Команды `service` — cobra-подкоманды; баннер выводится автоматически. Второй баннер
внутри `service`-команды не нужен.

Формат баннера (из `internal/banner/banner.go`, неизменён):

```
┌───────────────────────────────────────────────────────┐
│  raxd  —  Remote Access Daemon                        │
│  v0.4.0  ·  commit a1b2c3d  ·  built 2026-05-22      │
│  Vladimir Kovalev, OEM TECH                           │
└───────────────────────────────────────────────────────┘
```

Строка `Vladimir Kovalev, OEM TECH` — **обязательна**, третья строка баннера, без изменений.

Баннер идёт в **stderr** (как сейчас). При `NO_COLOR` — те же Unicode-символы рамки (рамка не ANSI,
не зависит от цвета). При терминале уже `< 42` колонки — `banner.go` уже реализует деградацию до
трёх plain-строк без рамки (см. комментарий в `banner.go`; адаптация по ширине — расширение cli-ux
отмечено там как extension point).

---

## Цвета и стиль (lipgloss)

Стилизация через `charmbracelet/lipgloss` v2. Команды `service` следуют той же схеме, что
планировалась для всего raxd CLI.

### Палитра

| Роль               | Имя токена          | Hex (dark bg) | Применение                           |
|--------------------|---------------------|---------------|--------------------------------------|
| Успех              | `colorSuccess`      | `#22C55E`     | Метка `installed`, `started`         |
| Предупреждение     | `colorWarning`      | `#FBBF24`     | `already installed`, `kept`          |
| Ошибка             | `colorError`        | `#EF4444`     | Метка `error:`                       |
| Подсказка          | `colorHint`         | `#94A3B8`     | Строки `hint:`                       |
| Приглушённый текст | `colorMuted`        | `#64748B`     | Значения метаданных (pid, путь)      |
| Акцент             | `colorAccent`       | `#38BDF8`     | Метки в status-блоке                 |
| Фон рамки баннера  | —                   | plain text    | Баннер — без цвета (Unicode-рамка)   |

Цвета не применяются к самим значениям (путям, PID): цвет — только для метки, чтобы не
перегружать строку. Ошибочный `error:` — красным, `hint:` — серым приглушённым.

### Стили по типам строк

```
labelStyle    = lipgloss.NewStyle().Foreground(colorAccent)
successLabel  = lipgloss.NewStyle().Foreground(colorSuccess)
warnLabel     = lipgloss.NewStyle().Foreground(colorWarning)
errorLabel    = lipgloss.NewStyle().Foreground(colorError)
hintLabel     = lipgloss.NewStyle().Foreground(colorHint)
mutedValue    = lipgloss.NewStyle().Foreground(colorMuted)
notRootBadge  = lipgloss.NewStyle().Foreground(colorSuccess)  // "[not root]"
```

`[not root]` — зелёным inline после значения `user`: зримый security-сигнал.

### `NO_COLOR` и `--no-color`

При `NO_COLOR=1` или флаге `--no-color` — липглосс возвращает plain-text без ANSI-кодов. Вся
иерархия и читаемость держится на выравнивании `%-12s`, а не на цвете. Тесты должны проверять
вывод с `NO_COLOR`.

### Таблица (`olekukonko/tablewriter`)

Команды `raxd service` не используют таблицу — у каждой одна сущность (один сервис).
Таблица остаётся для `raxd key list`. Если в будущем появится `raxd service list` (multi-instance
— вне scope v1), применить паттерн из `key.go`: `tw.Border{Left: Off, Right: Off, Top: Off, Bottom: Off}`,
`tw.AlignLeft`, 2-пробельный indent первого столбца.

---

## Тексты команд и ошибок

### Таблица ошибок

| Ситуация                               | Код ошибки             | Точный текст + exit   |
|----------------------------------------|------------------------|-----------------------|
| Нет прав для регистрации сервиса       | `ErrPermission`        | `error: insufficient privileges to install the service`<br>`  hint: run as root or with sudo: sudo raxd service install`<br>`  hint: installation requires root to write system service files`<br>exit `1` |
| Сервис уже установлен (при install)    | `ErrAlreadyInstalled`  | *(не `error:`, информационный блок)*<br>`  already installed   raxd service`<br>`  hint: use "raxd service status" to check the current state`<br>exit `0` |
| Сервис не установлен (при start/stop)  | `ErrNotInstalled`      | `error: raxd service is not installed`<br>`  hint: install it first with "raxd service install"`<br>exit `1` |
| Сервис не установлен (при uninstall)   | `ErrNotInstalled`      | *(не `error:`, информационный блок)*<br>`  not installed   raxd service`<br>`  hint: use "raxd service install" to set up the service`<br>exit `0` |
| Менеджер сервисов недоступен           | `ErrManagerUnavailable`| `error: service manager is not available`<br>`  hint: ensure systemd (Linux) or launchd (macOS) is running`<br>exit `1` |
| Неподдерживаемая платформа             | `ErrUnsupported`       | `error: this platform is not supported`<br>`  hint: raxd service management is available on Linux and macOS only`<br>exit `1` |
| Сбой запуска сервиса                   | общая ошибка           | `error: failed to start the service`<br>`  hint: check the service logs for details`<br>`  hint: run "raxd service status" to see the current state`<br>exit `1` |
| Сбой остановки сервиса                 | общая ошибка           | `error: failed to stop the service`<br>`  hint: check the service logs for details`<br>`  hint: run "raxd service status" to see the current state`<br>exit `1` |
| Сбой установки + откат (AC11)          | общая ошибка           | `error: service installation failed: could not register the service`<br>`  hint: the installer has removed any partially created files`<br>`  hint: run as root or with sudo: sudo raxd service install`<br>exit `1` |
| Сбой удаления                          | общая ошибка           | `error: failed to uninstall the service`<br>`  hint: run "raxd service status" to check current state`<br>`  hint: you may need to clean up manually: sudo systemctl disable raxd`<br>exit `1` |

> **Примечание: `ErrNotInstalled` — один sentinel, два exit-кода (Issue 4).**
> Маппинг зависит от семантики вызывающей команды, а не от самой ошибки:
> - `uninstall` + `ErrNotInstalled` → **exit 0** (идемпотентность, AC10: «операция безопасно
>   завершается»; удалить то, чего нет — ожидаемый исход).
> - `start` + `ErrNotInstalled` → **exit 1** (операция неприменима: нельзя запустить
>   несуществующий сервис).
> - `stop` + `ErrNotInstalled` → **exit 1** (операция неприменима: нельзя остановить
>   несуществующий сервис).
>
> CLI-слой (`internal/cli/service.go`) реализует это ветвлением в RunE каждой подкоманды:
> `uninstall` обрабатывает `ErrNotInstalled` как non-error; `start`/`stop` — как ошибку с exit 1.

### Правила формулировок

- Строчные буквы везде: `error:`, `hint:`, метки (`installed`, `running`, `user`).
- `error:` описывает **что произошло** (факт, прошедшее); `hint:` — **что сделать** (действие,
  повелительное наклонение).
- Никаких сырых системных сообщений (`exit status 1`, `Unit raxd.service not found` от systemctl —
  только типизированные нейтральные тексты выше).
- Команды в `hint:` — в кавычках и с полным именем: `"raxd service install"`, `"raxd service status"`.
- Для привилегированных операций — всегда `sudo raxd service ...` в hint (не `sudo systemctl`).

### Тексты успешных блоков

| Команда                   | Сигнальная строка                         | Ключевые метки                                          |
|---------------------------|-------------------------------------------|---------------------------------------------------------|
| `service install` (успех) | `installed   raxd service`                | unit, drop-in (Linux), user, port, autostart; hint start |
| `service install` (повтор)| `already installed   raxd service`        | hint status; exit 0                                     |
| `service uninstall`       | `uninstalled   raxd service`              | removed (unit), removed (drop-in), kept (user); hint data |
| `service uninstall` (нет) | `not installed   raxd service`            | hint install; exit 0                                    |
| `service start`           | `started   raxd service`                  | pid; hint status                                        |
| `service start` (уже)     | `already running   raxd service (pid N)`  | hint stop                                               |
| `service stop`            | `stopped   raxd service`                  | hint start                                              |
| `service stop` (уже)      | `already stopped   raxd service`          | hint start                                              |
| `service status` (запущен)| поле `running   yes` → stdout             | installed, running, pid, euid, user [not root], port, autostart, unit, manager, state |
| `service status` (стоп)   | поле `running   no` → stdout              | как выше, без pid; hint start                           |
| `service status` (нет)    | поле `installed   no` → stdout            | hint install                                            |

---

## Доступность (NO_COLOR, узкий терминал)

### NO_COLOR и `--no-color`

При `NO_COLOR=1` (env) или флаге `--no-color`:
- `charmbracelet/lipgloss` автоматически отключает ANSI-escape-коды (липглосс v2 уважает
  `NO_COLOR` из коробки).
- Вся читаемость держится на выравнивании `%-12s` и символах `error:` / `hint:`. Порядок и
  иерархия строк не зависят от цвета.
- `[not root]` остаётся в тексте — просто без зелёного цвета.
- Рамка баннера (`┌─┐`/`│`/`└─┘`) — Unicode, не ANSI-цвет; остаётся при NO_COLOR.
- Тесты вывода должны гоняться с `NO_COLOR=1` и сравнивать plain-text.

### Узкий терминал (< 52 / < 42 колонок)

**Баннер** (уже реализовано в `banner.go`):
- `>= 52` — полная рамка (широкий).
- `42–51` — укороченный commit (7 символов, без «commit»).
- `< 42` — три plain-строки без рамки.
- Адаптация по ширине отмечена в `banner.go` как extension point (cli-ux задача).

**Блоки service** — отдельных рамок нет, только выровненный текст. При очень узком терминале
(< 40 колонок):
- Длинные пути переносятся или усекаются. Правило усечения: усекать значение, а не метку;
  добавлять `…` в конце:
  ```
    unit          /etc/systemd/system/rax…
  ```
- Ширина метки `%-12s` не изменяется — это якорь выравнивания.
- Строки `hint:` при ширине < 40 допустимо переносить с отступом `          ` (10 пробелов,
  под текст после `  hint: `), чтобы не разрывалась команда посередине.

**Таблица** (`raxd service status --json` не таблица, не проблема). Если в будущем появится
таблица сервисов — паттерн `key.go`: tablewriter без внешних рамок, перенос по содержимому.

**Рекомендация**: при реализации проверять вывод при `COLUMNS=40` и `COLUMNS=80`.

---

## Хэндофф

Этот документ — контракт по выводу для **developer** (`internal/cli/service.go`) и
**cli-ux-guardian**. Developer реализует:
- `internal/cli/service.go`: cobra-группа `service` + 5 подкоманд, следуя паттерну `key.go`.
- Функции `printServiceSuccess`, `printServiceError`, `printServiceStatus` по образцу `printError`
  из `key.go` и `printStartBlock` из `serve.go`, с шаблонами строк из этого документа.
- `raxd service status` пишет человекочитаемый блок в **stdout** (`cmd.OutOrStdout()`), по образцу
  `status.go`. Флаг `--json` также в stdout.
- Маппинг `ErrNotInstalled`: в `uninstall.RunE` — возврат `nil` (exit 0) + информационный блок;
  в `start.RunE` / `stop.RunE` — возврат ошибки (exit 1) + `error:`-блок.
- Маппинг `ErrAlreadyInstalled`: в `install.RunE` — возврат `nil` (exit 0) + информационный блок.
- Проверка `NO_COLOR` уже обеспечена lipgloss; явно тестировать с `NO_COLOR=1`.

Автор продукта: **Vladimir Kovalev, OEM TECH**.
