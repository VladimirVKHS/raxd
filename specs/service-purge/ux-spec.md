# UX Spec: service-purge — `raxd service uninstall --purge`

> Контракт по выводу для developer. Go-код не содержится.
> Тексты вывода — английские (продукт англоязычный). Описания секций — русские.
> Автор продукта: **Vladimir Kovalev, OEM TECH** — обязателен в баннере.

---

## Принципы

1. **Авторитетный минимализм.** Каждый символ в терминале зарабатывает своё место. Иерархия
   создаётся выравниванием и отступами, а не декоративными рамками или символами.
2. **Слова несут тяжесть, не цвет.** Серьёзность необратимой операции выражается формулировками
   («irreversible», «destroys», «cannot be undone»), а не красным цветом по всему экрану. Цвет —
   точечный акцент: только метка `warning:` / `error:`.
3. **Предсказуемые потоки.** Все мутации (install, uninstall, purge, start, stop) → stderr.
   Только read-only данные (status, key list) → stdout. Это соответствует существующим
   подкомандам в `internal/cli/service.go`.
4. **Идемпотентность читаема с первого взгляда.** `removed` (действие выполнено) — нормальный
   вес. `absent` (уже отсутствовало) — приглушённый (lipgloss Faint). При повторном запуске
   `--purge` экран почти весь в dimmed-тексте — оператор мгновенно понимает: ничего не делалось.
5. **Барьер без интерактива.** `--purge` без `--yes` ничего не удаляет и завершается exit != 0.
   Предупреждение структурировано, не кричащее. Продукт управляется агентами/скриптами — никаких
   y/n-промптов.
6. **Нейтральные ошибки, actionable хинты.** Формула: `error: <что случилось>\n  hint: <что делать>`.
   Никаких стек-трейсов, никакого сырого вывода OS (SR-95). Одна ошибка — один хинт.
7. **Без секретов в выводе.** Содержимое `keys.db` и любые секреты в вывод не попадают (SR-124).
   В отчёте — только факты: удалён/не удалён пользователь, удалены/отсутствовали каталоги (без
   перечисления содержимого).
8. **Структура блока.** Метка (14 символов, выровнена влево) + значение через двойной пробел.
   Хинты — 2 пробела отступа + `hint:`. Группы разделяются одной пустой строкой.

```
  %-14s  <value>
  hint: <actionable text>
```

---

## Состояния вывода

### 1. Барьер необратимости: `--purge` без `--yes` (AC9, SR-114, SR-115)

**Поток:** stderr. **Exit:** 1. **Мутаций:** ноль.

Цель: дать оператору полную картину того, что будет уничтожено, и явную инструкцию как разрешить.
Тон — спокойный, точный, без паники.

```
warning: this operation is irreversible

  The following will be permanently destroyed:

    user      raxd  (system account, no login shell)
    state     <state-dir>
    config    <config-dir>
    keys.db   all API keys and audit log — cannot be recovered

  hint: to confirm, re-run with --yes:
          sudo raxd service uninstall --purge --yes
```

`<state-dir>` и `<config-dir>` — runtime-значения из `DefaultConfigForGOOS(runtime.GOOS)`,
подставляются в момент вывода. Одна строка на каждый каталог — без платформенных развилок
в макете.

**Детали:**

- Метка `warning:` — lipgloss Yellow (только слово `warning:`), остальной текст — default.
- Блок «The following will be permanently destroyed:» — обычный вес, 2 пробела отступа.
- Перечень (`user`, `state`, `config`, `keys.db`) — 4 пробела отступа. Метки выровнены по 10
  символам.
- `keys.db` явно упоминается с пометкой «cannot be recovered» — соответствие SR-114.
- Нижний `hint:` — 2 пробела + `hint:`, команда с отступом 10 пробелов.
- Одна пустая строка между «will be destroyed» блоком и хинтом.

**NO_COLOR:** убрать Yellow с `warning:`. Читаемость полная — вся информация в тексте.

---

### 2. Успешное выполнение: `--purge --yes`, демон остановлен и удалён (AC1, AC3, AC8)

**Поток:** stderr. **Exit:** 0.

Отчёт пошаговый: сначала действия с сервисом, потом — с пользователем и каталогами, итог.
`removed` — нормальный вес; `absent` — dimmed (lipgloss Faint).

```
  stopped        raxd service

  uninstalled    raxd service
  removed        unit file and autostart registration
  removed        journal size limit drop-in            [Linux only]

  removed        user raxd
  removed        <state-dir>
  removed        <config-dir>

  purge complete   raxd has been fully removed from this host
```

**Аудит-лог** (charmbracelet/log, INFO, в stderr, после блока):

```
INFO service purged action=purge platform=linux user_removed=true dirs_removed="/var/lib/raxd /etc/raxd" stopped=true
```

**Детали:**

- Метка-колонка — 14 символов, `%-14s` (аналог существующих подкоманд `install`/`uninstall`).
- Каждая строка — отдельный факт. Нет многострочных значений в одной строке отчёта.
- `purge complete` — выделяется визуально: перед ней пустая строка. Lipgloss Bold на тексте
  «raxd has been fully removed from this host» (или просто нормальный вес — на усмотрение).
- Пути `<state-dir>`/`<config-dir>` — runtime-значения из `DefaultConfigForGOOS`. Не хардкодить.
- Содержимое каталогов (`keys.db` и т.д.) не перечисляется (SR-124 — без секретов в выводе).
- Linux-специфичные строки (`journal size limit drop-in`) печатаются только на Linux — аналог
  существующего `if runtime.GOOS == "linux"` в `newServiceUninstallCmd`.

---

### 3. Идемпотентный повтор: `--purge --yes`, всё уже удалено (AC3)

**Поток:** stderr. **Exit:** 0.

Все строки dimmed (lipgloss Faint) — сигнал «ничего не делалось». Единая колонка `%-14s`
сохраняется для всех меток (≤14 символов).

```
  not running    raxd service

  not installed  raxd service  [already unregistered]

  absent         user raxd  [already removed]
  absent         <state-dir>
  absent         <config-dir>

  purge complete   nothing to remove — host is already clean
```

**Детали:**

- `not running` (11 симв.), `not installed` (13 симв.), `absent` (6 симв.) — все ≤14, колонка
  выровнена единообразно под `  %-14s  value`.
- Все строки кроме `purge complete` — lipgloss Faint (приглушённый). Оператор видит pale-вывод
  и сразу понимает: операция была no-op.
- Финальная строка `purge complete` — нормальный вес (итог всегда чёткий).
- `[already unregistered]` / `[already removed]` — пояснение в квадратных скобках в поле value,
  без влияния на ширину метки.

---

### 4. Частичная идемпотентность: что-то удалено, что-то отсутствовало (AC3)

**Поток:** stderr. **Exit:** 0.

Смешанный вывод: normal-вес — реальные действия, Faint — то, что уже отсутствовало.

```
  stopped        raxd service

  not installed  raxd service  [already unregistered]

  removed        user raxd
  absent         <state-dir>   [already removed]
  removed        <config-dir>

  purge complete   raxd has been fully removed from this host
```

Dimmed-строки (`absent`, `not installed`) — то, что уже отсутствовало. Normal-строки
(`stopped`, `removed`) — реальные действия этого запуска.

---

### 5. Ошибка: недостаточно прав (AC5, SR-84)

**Поток:** stderr. **Exit:** 1. **Мутаций:** ноль.

```
error: insufficient privileges to run purge
  hint: run as root or with sudo:
          sudo raxd service uninstall --purge --yes
```

- Метка `error:` — lipgloss Red (только слово `error:`).
- Никакого упоминания сырых кодов ошибок OS.
- NO_COLOR: убрать Red, читаемость полная.

---

### 6. Ошибка: демон не остановился (AC4)

**Поток:** stderr. **Exit:** 1. **Мутаций:** ноль (ни пользователь, ни каталоги не тронуты).

```
error: raxd service did not stop within the timeout
  hint: check service status and stop it manually before purging:
          sudo raxd service stop
          sudo raxd service uninstall --purge --yes
```

- Формулировка нейтральная — не «failed to kill», не «timeout exceeded».
- Два hint-шага (остановить вручную, потом запустить purge) — последовательность в одном хинте.

---

### 7. Ошибка: несоответствие пользователя (AC6, SR-95)

**Поток:** stderr. **Exit:** 1. **Мутаций:** ноль.

```
error: system user "raxd" does not match the expected raxd service account
  hint: the account may have been modified; inspect it before removing:
          id raxd
          getent passwd raxd
```

- Нет упоминания деталей несоответствия (shell, uid) — нейтральная формулировка (SR-95).
- Хинт даёт оператору инструменты для самостоятельной диагностики.

---

### 8. Ошибка: подозрительный путь (AC7)

**Поток:** stderr. **Exit:** 1. **Мутаций:** ноль.

```
error: resolved path for state/config directory is outside the expected layout
  hint: inspect the raxd configuration for unexpected symlinks or path overrides:
          raxd service status
```

- Конкретный подозрительный путь НЕ печатается (защита от информационных утечек; оператор
  сам запустит `status` для диагностики).
- Нейтральная формулировка без упоминания «symlink attack» и прочего внутреннего жаргона.

---

### 9. `raxd service uninstall` БЕЗ `--purge` (AC2)

**Поток:** stderr. **Exit:** 0. **Поведение не меняется** по сравнению с текущим кодом.

Существующий вывод (для справки developer — не менять):

```
  uninstalled    raxd service
  removed        unit file and autostart registration
  removed        journal size limit drop-in             [Linux]
  kept           system user "raxd" (no shell, no home, not running)
  hint: to also remove the user: sudo userdel raxd
  hint: data in /var/lib/raxd/ is preserved — remove manually if no longer needed
```

Вывод `uninstall` не меняется, хинт про `--purge` не добавляется (AC2: byte-for-byte).

---

## Баннер автора

Баннер отображается при `raxd --help` и `raxd version`. Не отображается при вызове подкоманд
(install, uninstall, purge, status и т.д.) — сохраняет чистоту pipe-friendly вывода.

```
  ██████╗  █████╗ ██╗  ██╗██████╗
  ██╔══██╗██╔══██╗╚██╗██╔╝██╔══██╗
  ██████╔╝███████║ ╚███╔╝ ██║  ██║
  ██╔══██╗██╔══██║ ██╔██╗ ██║  ██║
  ██║  ██║██║  ██║██╔╝ ██╗██████╔╝
  ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝╚═════╝

  Remote Access Daemon for AI Agents
  Vladimir Kovalev, OEM TECH
```

**Правила баннера:**

- Строка `Vladimir Kovalev, OEM TECH` — ОБЯЗАТЕЛЬНА. Убирать нельзя.
- 2 пробела отступа слева у каждой строки (согласованно с блоками команд).
- ASCII-art опционален при `--no-color` / `NO_COLOR` — можно заменить на текстовый `raxd`:

```
  raxd — Remote Access Daemon for AI Agents
  Vladimir Kovalev, OEM TECH
```

- lipgloss Bold на `raxd` (текстовая версия) и на `Vladimir Kovalev, OEM TECH`.

---

## Цвета и стиль (lipgloss v2)

Импорт: `charm.land/lipgloss/v2` (стабильный v2, согласно STACK.ru.md).

### Палитра ролей

| Роль              | lipgloss-стиль                       | Применение                                      |
|-------------------|--------------------------------------|-------------------------------------------------|
| `warning` label   | `lipgloss.NewStyle().Foreground(lipgloss.Color("3"))` — ANSI Yellow | Только слово `warning:` в барьере |
| `error` label     | `lipgloss.NewStyle().Foreground(lipgloss.Color("1"))` — ANSI Red    | Только слово `error:` в ошибках   |
| `absent` value    | `lipgloss.NewStyle().Faint(true)`    | Идемпотентные строки (уже отсутствовало)        |
| `purge complete`  | `lipgloss.NewStyle().Bold(true)`     | Итоговая строка успешного purge                 |
| `hint:` label     | default (без цвета)                  | Консистентно с существующими хинтами в коде     |
| Обычный текст     | default (без стиля)                  | Все `removed`, `stopped`, `uninstalled` строки  |
| Баннер `raxd`     | `lipgloss.NewStyle().Bold(true)`     | Текстовый вариант баннера                       |
| Баннер автор      | `lipgloss.NewStyle().Bold(true)`     | `Vladimir Kovalev, OEM TECH`                    |

### Принцип точечного цвета

Цвет применяется только к **метке-слову** (`warning:`, `error:`), не к тексту сообщения.
Это соответствует существующему стилю: в `printSvcError` цвета нет совсем — добавляем аккуратно.

### charmbracelet/log (аудит-строки)

```
INFO service purged action=purge platform=linux user_removed=true user_absent=false dirs_removed=[<state-dir> <config-dir>] dirs_absent=[] stopped=true
```

Ключи: `action`, `platform`, `user_removed`, `user_absent`, `dirs_removed`, `dirs_absent`,
`stopped`. Значений типа «содержимое ключей» нет (SR-124).

### olekukonko/tablewriter

Для `raxd service uninstall --purge` таблица не нужна — используется блочный формат
`  %-14s  value`. tablewriter используется в `raxd key list` (отдельная ux-spec).

---

## Тексты команд и ошибок

### Полная таблица текстов (английский)

#### Барьер (--purge без --yes)

```
warning: this operation is irreversible

  The following will be permanently destroyed:

    user      raxd  (system account, no login shell)
    state     <state-dir>
    config    <config-dir>
    keys.db   all API keys and audit log — cannot be recovered

  hint: to confirm, re-run with --yes:
          sudo raxd service uninstall --purge --yes
```

#### Успех: сервис остановлен и удалён

```
  stopped        raxd service
  uninstalled    raxd service
  removed        unit file and autostart registration
  removed        journal size limit drop-in             [Linux]
  removed        user raxd
  removed        <state-dir>
  removed        <config-dir>

  purge complete   raxd has been fully removed from this host
```

#### Успех: идемпотентный повтор (всё уже отсутствует)

```
  not running    raxd service
  not installed  raxd service  [already unregistered]
  absent         user raxd  [already removed]
  absent         <state-dir>
  absent         <config-dir>

  purge complete   nothing to remove — host is already clean
```

#### Ошибка: нет прав (AC5)

```
error: insufficient privileges to run purge
  hint: run as root or with sudo:
          sudo raxd service uninstall --purge --yes
```

Exit: 1.

#### Ошибка: демон не остановился (AC4)

```
error: raxd service did not stop within the timeout
  hint: check service status and stop it manually before purging:
          sudo raxd service stop
          sudo raxd service uninstall --purge --yes
```

Exit: 1.

#### Ошибка: несоответствие пользователя (AC6)

```
error: system user "raxd" does not match the expected raxd service account
  hint: the account may have been modified; inspect it before removing:
          id raxd
          getent passwd raxd
```

Exit: 1.

#### Ошибка: подозрительный путь (AC7)

```
error: resolved path for state/config directory is outside the expected layout
  hint: inspect the raxd configuration for unexpected symlinks or path overrides:
          raxd service status
```

Exit: 1.

#### Ошибка: платформа не поддерживается

```
error: this platform is not supported
  hint: raxd service management is available on Linux and macOS only
```

Exit: 1.

#### Ошибка: общая (fallback)

```
error: purge operation failed
  hint: run "raxd service status" to check current state
```

Exit: 1. Используется когда sentinel-ошибка не распознана. Сырой текст ошибки OS не выводится
(SR-95).

### Маппинг sentinel-ошибок → текст (для developer)

| Sentinel                  | Текст `error:`                                                    | Текст `hint:`                                             | Exit |
|---------------------------|-------------------------------------------------------------------|-----------------------------------------------------------|------|
| `ErrPermission`           | insufficient privileges to run purge                              | run as root or with sudo: `sudo raxd service uninstall --purge --yes` | 1 |
| Stop failed (не sentinel) | raxd service did not stop within the timeout                      | check service status and stop it manually before purging  | 1 |
| `ErrUserMismatch`         | system user "raxd" does not match the expected raxd service account | the account may have been modified; inspect it before removing | 1 |
| `ErrSuspiciousPath`       | resolved path for state/config directory is outside the expected layout | inspect the raxd configuration for unexpected symlinks | 1 |
| `ErrUnsupported`          | this platform is not supported                                    | raxd service management is available on Linux and macOS only | 1 |
| `ErrPurgeNotConfirmed`    | (не должен дойти до printSvcError — барьер на уровне CLI)        | —                                                         | 1 |
| default                   | purge operation failed                                            | run "raxd service status" to check current state          | 1 |

---

## Потоки вывода и exit-коды

| Сценарий                              | Поток   | Exit |
|---------------------------------------|---------|------|
| `--purge` без `--yes` (барьер)        | stderr  | 1    |
| `--purge --yes` успех (полный)        | stderr  | 0    |
| `--purge --yes` успех (идемпотентный) | stderr  | 0    |
| `--purge --yes` — нет прав            | stderr  | 1    |
| `--purge --yes` — демон не остановился| stderr  | 1    |
| `--purge --yes` — несоответствие user | stderr  | 1    |
| `--purge --yes` — подозрительный путь | stderr  | 1    |
| `uninstall` без `--purge`             | stderr  | 0    |

Правило согласованности: все мутирующие команды (`install`, `uninstall`, `purge`, `start`, `stop`)
пишут в stderr. Только `status` пишет в stdout (read-only данные, скриптуемость).

---

## Доступность (NO_COLOR, узкий терминал)

### NO_COLOR / --no-color

При `NO_COLOR=1` в env или флаге `--no-color`:

- `warning:` — печатается как plain-текст без Yellow. Читаемость обеспечена словом «warning».
- `error:` — печатается как plain-текст без Red. Читаемость обеспечена словом «error».
- `absent` строки — липглосс `Faint(true)` не применяется. Строки печатаются с явным
  суффиксом-маркером: `absent [--]` либо просто `absent` (разница с `removed` остаётся
  семантической — в слове, а не в цвете).
- `purge complete` — Bold не применяется. Строка отделяется пустой строкой сверху.
- ASCII-art баннер заменяется на текстовую версию.
- Поведение lipgloss v2: `lipgloss.NewRenderer(os.Stderr)` + проверка `HasDarkBackground()`
  и `ColorProfile()` — при `NO_COLOR` renderer возвращает profile без цвета.

Вывод без цвета полностью читаем: все различия закодированы в словах (`warning`/`error`/
`removed`/`absent`), выравнивании и структуре.

### Узкий терминал (< 60 символов)

Блочный формат `  %-14s  value` не использует tablewriter, поэтому не ломается при сужении.

- Если значение длиннее 40 символов (длинный путь), developer переносит его на следующую строку
  с отступом 18 пробелов (14 + 4), выровнено под начало value-колонки:
  ```
    removed        /very/long/path/that/does/not/
                   fit/on/one/line/raxd/
  ```
- Минимальная ширина для корректного отображения: 40 символов. Ниже — вывод читаем, но
  выравнивание может нарушиться. Это приемлемо для операторской среды.
- Баннер (ASCII-art) при ширине < 40 заменяется на текстовую версию автоматически.
- Строки барьера `warning:` без переносов: каждое слово сообщения умещается в 50 символов.

### Pipe-режим (не-TTY)

Когда stdout/stderr перенаправлены в pipe или файл:

- lipgloss отключает цвет автоматически (нет TTY).
- Вывод идентичен `NO_COLOR`-режиму.
- Аудит-строки (charmbracelet/log) печатаются в machine-friendly формате.

---

## Аудит-записи (AC8)

Аудит-запись формируется **до** физического удаления каталогов (AC8). Порядок:

1. Собрать `PurgeReport` с фактами (что будет удалено / что уже отсутствует).
2. Записать аудит-лог через `charmbracelet/log`.
3. Выполнить физическое удаление.
4. Вывести пошаговый отчёт в stderr.

Формат аудит-строки (INFO):

```
INFO service purged action=purge platform=linux user_removed=true user_absent=false dirs_removed=[/var/lib/raxd /etc/raxd] dirs_absent=[] stopped=true
```

Ключи: `action`, `platform`, `user_removed`, `user_absent`, `dirs_removed`, `dirs_absent`,
`stopped`. `dirs_removed`/`dirs_absent` — список путей каталогов верхнего уровня (не перечень
файлов внутри них); содержимое каталогов в лог не попадает (SR-124). Содержимое ключей
(`keys.db`) в аудит-лог не попадает.

---

## Согласованность с существующим стилем service.go

Существующие подкоманды (`install`, `uninstall`, `start`, `stop`) используют единый формат
строки: 2 пробела отступа + метка в поле шириной 14 символов (`%-14s`) + 2 пробела перед
значением. Ошибки оформляются как `error: <текст>` без отступа, хинты — `  hint: <текст>`
с двумя пробелами. Мутирующие команды пишут весь вывод в stderr; read-only команда `status`
пишет в stdout.

Код `uninstall --purge` должен следовать тому же формату и тем же потокам: `  %-14s  value`
для строк отчёта, `error:`/`  hint:` для ошибок, всё в stderr. Это гарантирует визуальную
согласованность при запуске нескольких подкоманд подряд.
