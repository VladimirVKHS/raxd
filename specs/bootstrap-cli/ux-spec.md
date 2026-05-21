# UX Spec: Bootstrap CLI — каркас проекта raxd

## Принципы

1. **Иерархия информации.** Самое важное — первым. Баннер идентифицирует продукт и автора; `status`
   начинается с состояния демона; ошибка начинается с причины, не с деталей реализации.
2. **Один поток — одна цель.** Баннер и диагностический вывод (stderr) не смешиваются с
   машиночитаемым результатом (stdout). Пайп `raxd status | grep state` работает корректно.
3. **Plain-текст как база.** На стадии bootstrap все выводы — чистый текст с Unicode-рамками и
   выравниванием пробелами. Красота без внешних зависимостей. Цвет и стиль — точки расширения.
4. **Предсказуемость и единый тон.** Все тексты — на английском (см. раздел «Язык интерфейса»).
   Ошибки — всегда `error:` + `hint:`. Заглушки — всегда `<команда>: not implemented yet`.
5. **Дружелюбность без жаргона.** Сообщения об ошибках объясняют, что случилось, и говорят, что
   делать. Никаких Go-стек-трейсов, пакетных путей или кодов возврата в тексте.
6. **Устойчивость к среде.** Вывод корректен при `NO_COLOR`, `--no-color`, узком терминале (60 кол.),
   пайпинге, перенаправлении в файл. Unicode box-drawing — не ANSI-цвет, не отключается.

---

## Язык интерфейса

**Все тексты CLI (Short/Long/Use, сообщения команд, ошибки, баннер) — английский.**

Обоснование: продукт `raxd` международный (Remote Access Daemon, OEM TECH), предназначен для
ИИ-агентов и технических пользователей по всему миру. Английский — стандарт для системного
инструментария. Русский язык — только в артефактах проектирования (этот файл, spec/plan).

---

## Баннер автора

### Назначение и канал вывода

Баннер печатается на **stderr** при каждом вызове любой команды дерева `raxd` (через
`PersistentPreRun` корневой команды). Это не засоряет машиночитаемый stdout. Пайп
`raxd status | jq` не захватывает баннер.

Исключения: баннер не печатается при `--help` (cobra выводит help самостоятельно) и при
`--no-banner` (если флаг будет добавлен — точка расширения).

### Структурная иерархия

```
┌─ визуальный якорь: имя продукта
├─ описание: одна строка, subordinate
├─ метаданные сборки: version · commit · date
└─ автор: всегда последним, всегда присутствует
```

### Wide-макет (терминал >= 52 колонок)

```
┌──────────────────────────────────────────────────┐
│  raxd  —  Remote Access Daemon                   │
│  v1.0.0  ·  commit abc1234  ·  built 2025-06-01  │
│  Vladimir Kovalev, OEM TECH                      │
└──────────────────────────────────────────────────┘
```

Ширина рамки определяется по длине самой длинной внутренней строки + 2 пробела отступа с каждой
стороны. Рамка адаптивна: `┌` + N × `─` + `┐`.

### Narrow-макет (терминал < 52 колонок, либо fallback)

```
┌────────────────────────────────────────┐
│  raxd  —  Remote Access Daemon         │
│  v1.0.0  ·  abc1234  ·  2025-06-01    │
│  Vladimir Kovalev, OEM TECH            │
└────────────────────────────────────────┘
```

В narrow-режиме commit сокращается до 7 символов (без префикса `commit`), дата остаётся полной
(`YYYY-MM-DD`). Если терминал уже 42 символов — рамка опускается, вывод без границ:

```
  raxd  —  Remote Access Daemon
  v1.0.0  ·  abc1234  ·  2025-06-01
  Vladimir Kovalev, OEM TECH
```

### Дефолтные значения build-метаданных

При сборке без ldflags (режим разработки):

```
┌──────────────────────────────────────────────┐
│  raxd  —  Remote Access Daemon               │
│  dev  ·  commit none  ·  built unknown       │
│  Vladimir Kovalev, OEM TECH                  │
└──────────────────────────────────────────────┘
```

Дефолты: version=`dev`, commit=`none`, date=`unknown`. Осмысленны, не пусты.

### Точка расширения: липглосс-стилизация баннера

> Не код продукта — иллюстрация намерения:
>
> Когда подключается `charm.land/lipgloss/v2`, имя `raxd` получает Bold,
> строка описания — Faint, автор — Italic. Рамка рисуется через `lipgloss.Border`.
> Гейт NO_COLOR проверяется до применения стилей (см. раздел «Доступность»).

---

## Состояния вывода

### `raxd version`

**Канал:** stdout. **Код возврата:** 0.

**Формат — однострочный:**

```
raxd 1.0.0 (commit abc1234, built 2025-06-01)
```

Версия печатается как есть из build-метаданных (без хардкода `v`-префикса), чтобы избежать
`vdev` для dev-сборок.

Обоснование однострочного формата: `version` часто используется в скриптах
(`raxd version | grep -o 'v[0-9.]*'`). Одна строка проще парсить; многострочный формат
избыточен для трёх полей.

**Пример при дефолтных значениях (dev-сборка):**

```
raxd dev (commit none, built unknown)
```

Соответствует контракту `version.Info()` из `plan.md`:
`raxd <version> (commit <commit>, built <date>)`.

---

### `raxd status`

**Канал:** stdout. **Код возврата:** 0 (заглушка-состояние).

**Иерархия полей** (от наиболее важного к менее):

1. `state` — что демон делает прямо сейчас (первая строка, главное)
2. `config` — путь к файлу конфигурации
3. `keys` — путь к базе ключей
4. `tls` — директория TLS

**Макет — выровненные key: value строки:**

```
  state    not running
  config   /home/user/.config/raxd/config.yaml
  keys     /home/user/.local/state/raxd/keys.db
  tls      /home/user/.local/state/raxd/tls/
```

Ширина метки — 8 символов (с выравниванием пробелами вправо до фиксированного столбца).
Двухпробельный отступ слева. Значения начинаются с позиции 12.

**Пример на macOS (канонический путь D3):**

```
  state    not running
  config   /Users/alice/.config/raxd/config.yaml
  keys     /Users/alice/.local/state/raxd/keys.db
  tls      /Users/alice/.local/state/raxd/tls/
```

**Пример, когда config.yaml отсутствует** (не ошибка — показываем путь, где он будет):

```
  state    not running
  config   /home/user/.config/raxd/config.yaml  (not found, defaults applied)
  keys     /home/user/.local/state/raxd/keys.db
  tls      /home/user/.local/state/raxd/tls/
```

Суффикс `(not found, defaults applied)` информирует без паники. Код возврата остаётся 0.

**Что НЕ показывает `status`** (требование безопасности):
- содержимое `keys.db` или любых ключей;
- содержимое TLS-директории;
- значение порта (пока нет running-состояния);
- любые чувствительные данные.

**Точка расширения:** когда демон будет запущен (задача `service-install`), `state` меняется:

```
  state    running  (pid 12345, port 7822, uptime 3h 14m)
  config   /home/user/.config/raxd/config.yaml
  keys     /home/user/.local/state/raxd/keys.db
  tls      /home/user/.local/state/raxd/tls/
```

---

### `raxd key list`

**На стадии bootstrap (каркас):** заглушка. **Канал:** stderr. **Код возврата:** 1.

Вывод заглушки:
```
error: key list: not implemented yet
```

**Контракт будущей реализации (задача `key-management`):** когда команда реализована — вывод идёт на stdout с кодом 0. Полный табличный макет ниже — контракт для `key-management`, не поведение каркаса.

**Рекомендуемый стиль: dash-separator без рамки.**

Обоснование: читается лучше box-стиля, не разрушается при узком терминале, совместим с пайпом,
не нуждается в цвете для разборчивости.

**Макет:**

```
  ID            LABEL                CREATED      LAST USED
  ──────────    ────────────────     ──────────   ──────────
  abc123de      production-key       2025-05-10   2025-05-20
  f9e21b00      staging              2025-05-15   never
  c0ffee01      ci-runner            2025-05-01   2025-05-21
```

**Ширина колонок:**

| Колонка   | Ширина  | Правило усечения            |
|-----------|---------|------------------------------|
| ID        | 12 сим. | Обрезать до 12, без `…`      |
| LABEL     | 20 сим. | Обрезать с суффиксом `…`     |
| CREATED   | 10 сим. | Формат `YYYY-MM-DD`          |
| LAST USED | 10 сим. | `YYYY-MM-DD` или `never`     |

Итоговая ширина: ~56 символов — укладывается в 60-колоночный терминал.

**Пустой список:**

```
  No API keys found.
  Hint: create your first key with "raxd key create --name <label>"
```

**Реализуется через `olekukonko/tablewriter`** с отключёнными внешними рамками,
выравниванием по левому краю, разделителем `──` под заголовком.

---

### Статус-блок установки (install)

**Канал:** stderr (install-скрипт). Печатается после успешной установки `curl | sh`.

> Эта задача относится к `distribution` (out of scope bootstrap-cli). Макет зафиксирован здесь
> как контракт для будущей реализации.

```
┌─────────────────────────────────────────────────────┐
│  raxd installed successfully                        │
│                                                     │
│  version   v1.0.0                                   │
│  binary    /usr/local/bin/raxd                      │
│  service   enabled (launchd / systemd)              │
│                                                     │
│  Vladimir Kovalev, OEM TECH                         │
└─────────────────────────────────────────────────────┘

  Get started:

    raxd status           — check daemon state
    raxd key create       — create your first API key
    raxd --help           — full command reference
```

---

### Ошибки

**Канал:** stderr. **Код возврата:** 1 (или специфичный ненулевой).

**Универсальная структура:**

```
error: <что случилось — одно предложение, строчные буквы, без точки>
  hint: <что сделать — одно предложение, начинается с глагола>
```

**Типология ошибок:**

**1. Заглушка (not implemented yet):**

```
error: key create: not implemented yet
```

Без `hint:` — действие пользователя не требуется, это ожидаемое состояние каркаса.

**2. Заглушка `serve`:**

```
error: serve: not implemented yet
```

**3. Отсутствует HOME (критично для XDG):**

```
error: cannot determine config directory: $HOME is not set
  hint: set the HOME environment variable and try again
```

**4. Не удалось создать директорию:**

```
error: cannot create config directory: permission denied
  hint: check that you have write access to ~/.config/raxd/
```

**5. Повреждённый config.yaml:**

```
error: config file is not valid YAML
  hint: edit ~/.config/raxd/config.yaml and fix the syntax, then run again
```

**6. Неизвестная команда (cobra-дефолт, допустим как есть):**

```
Error: unknown command "statu" for "raxd"

Run 'raxd --help' for usage.
```

Cobra выводит это сам при `SilenceErrors=false`. Если нужен кастомный формат — переопределить
`RunE` корневой команды для перехвата `*cobra.NoSuchCommandError`. Это точка расширения для
будущей доработки (не bootstrap-cli).

**7. Неизвестный флаг:**

```
Error: unknown flag: --versioon

Run 'raxd --help' for usage.
```

Аналогично — cobra-дефолт, приемлем на стадии каркаса.

---

## Тексты команд и ошибок

### Тексты `--help`: корневая команда `raxd`

**Use:** `raxd [command]`

**Short:**
```
raxd — remote access daemon for AI agents
```

**Long:**
```
raxd is a remote access daemon that provides secure command execution,
file transfer, and API key management for AI agents.

Use "raxd [command] --help" for more information about a command.
```

---

### `raxd key`

**Use:** `key [command]`

**Short:**
```
Manage API keys
```

**Long:**
```
Create, list, and delete API keys used to authenticate remote access.
```

---

### `raxd key create`

**Use:** `create [--name <label>]`

**Short:**
```
Create a new API key
```

**Long:**
```
Generate a new API key for remote access authentication.
The key is displayed once and cannot be retrieved afterwards.

  Flags:
    --name string   human-readable label for the key
```

**Вывод заглушки (stderr, exit 1):**
```
error: key create: not implemented yet
```

---

### `raxd key list`

**Use:** `list`

**Short:**
```
List all API keys
```

**Long:**
```
Display a table of all API keys with their ID, label, creation date,
and last-used date.
```

**Вывод заглушки (stderr, exit 1):**
```
error: key list: not implemented yet
```

---

### `raxd key delete`

**Use:** `delete <id>`

**Short:**
```
Delete an API key
```

**Long:**
```
Revoke and permanently delete the API key with the given ID.
This action cannot be undone.
```

**Вывод заглушки (stderr, exit 1):**
```
error: key delete: not implemented yet
```

---

### `raxd config`

**Use:** `config [command]`

**Short:**
```
Manage configuration
```

**Long:**
```
View and modify raxd configuration settings.
Configuration is stored in ~/.config/raxd/config.yaml.
```

---

### `raxd config port`

**Use:** `port <PORT>`

**Short:**
```
Set the listening port
```

**Long:**
```
Configure the TCP port that raxd listens on for incoming connections.
Default port is 7822.

  Example:
    raxd config port 8080
```

**Вывод заглушки (stderr, exit 1):**
```
error: config port: not implemented yet
```

---

### `raxd serve`

**Use:** `serve`

**Short:**
```
Start the raxd daemon
```

**Long:**
```
Start raxd as a foreground daemon process.
For production use, register raxd as a system service instead.
```

**Вывод заглушки (stderr, exit 1):**
```
error: serve: not implemented yet
```

Примечание: `serve` — «честная» заглушка (D4 из spec). Не запускает блокирующий процесс.

---

### `raxd version`

**Use:** `version`

**Short:**
```
Print version information
```

**Long:**
```
Print the raxd version, git commit, and build date.
```

**Вывод (stdout, exit 0):**
```
raxd 1.0.0 (commit abc1234, built 2025-06-01)
```

Версия печатается как есть из build-метаданных (без хардкода `v`-префикса), чтобы избежать
`vdev` для dev-сборок.

---

### `raxd status`

**Use:** `status`

**Short:**
```
Show daemon status and configuration paths
```

**Long:**
```
Display the current state of the raxd daemon and the filesystem paths
used for configuration, key storage, and TLS certificates.
```

**Вывод (stdout, exit 0):** — см. раздел «Состояния вывода / raxd status» выше.

---

## Цвета и стиль (lipgloss)

### Текущее состояние: plain-текст

На стадии `bootstrap-cli` стилизация через `charmbracelet/lipgloss` **не подключается**
(решение architect, план Trade-offs). Весь вывод — plain Unicode с пробельным выравниванием.

API `banner.Render() string` возвращает строку — контракт сохранится при добавлении lipgloss,
рефактор локальный (только `internal/banner`).

### Палитра — точка расширения

Зафиксированы роли и будущие цвета. Реализация — в задаче cli-ux финального дизайна.

| Роль            | Описание                           | Цвет (hex, ориентир) |
|-----------------|-------------------------------------|----------------------|
| Primary         | Имя продукта, заголовки таблиц      | `#5FD7FF` (cyan)     |
| Muted           | Пути, вторичный текст, даты         | `#767676` (gray)     |
| Success         | Успешное действие, running-статус   | `#5FFF87` (green)    |
| Warning         | Not running, предупреждения         | `#FFD75F` (yellow)   |
| Error           | Префикс `error:`, критичные статусы | `#FF5F5F` (red)      |
| Author/Accent   | Строка автора в баннере             | `#D7AF87` (warm)     |
| Border          | Рамки баннера и таблиц              | `#3A3A3A` (dark)     |

Палитра ориентирована на тёмный терминал (стандарт для системного инструментария).
При светлом фоне значения `Primary`/`Success`/`Warning` требуют проверки контраста WCAG AA.

### Правила стилизации (будущее)

- `raxd` в баннере: Bold + цвет Primary.
- Описание в баннере: Faint (Muted).
- Автор в баннере: Italic + цвет Author/Accent.
- Рамка баннера: `lipgloss.RoundedBorder()`, цвет Border.
- `state: not running` — значение цветом Warning.
- `state: running` — значение цветом Success.
- Префикс `error:` — цвет Error + Bold.
- Префикс `hint:` — цвет Muted.
- Заголовки таблицы (`key list`) — Bold + цвет Primary.
- Значение `never` в таблице — цвет Muted.

### Путь подключения

На стадии bootstrap-cli lipgloss **не подключается** — это точка расширения.

> При подключении в будущей задаче: `plan.md` (Trade-offs) фиксирует открытый вопрос для STACK-owner — путь `charm.land/lipgloss/v2` (стабильный v2.0.x) расходится со STACK.ru.md (`github.com/charmbracelet/lipgloss`). Перед подключением уточнить актуальный путь у STACK-owner. Не блокер bootstrap-cli.

---

## Доступность (NO_COLOR, узкий терминал)

### NO_COLOR и --no-color

**Стандарт `NO_COLOR`** (https://no-color.org): при наличии переменной окружения `NO_COLOR`
(любое значение) вывод не должен содержать ANSI escape-кодов.

**Текущее поведение (bootstrap-cli):** весь вывод уже plain-текст, ANSI отсутствует.
Никаких изменений не требуется. Unicode box-drawing (`┌─┐│└─┘`) — не ANSI, не отключается.

**Поведение при добавлении lipgloss (точка расширения):**

> Точка расширения (не bootstrap-cli): при наличии `NO_COLOR` (любое значение) или флага `--no-color` вывод не должен содержать ANSI escape-кодов. Unicode box-drawing (┌─┐│└─┘) и разделители таблицы (──) сохраняются — они не ANSI. Lipgloss-стили не применяются; вывод остаётся plain-текстом с пробельным выравниванием.

**Флаг `--no-color`:** персистентный флаг корневой команды (точка расширения, не bootstrap-cli).
При его наличии поведение идентично `NO_COLOR=1`.

### Узкий терминал

**Ширина >= 52 символов:** wide-макет баннера с полной рамкой.

**Ширина 42–51 символ:** narrow-макет баннера, сокращённый commit (7 символов без `commit`).

**Ширина < 42 символов:** рамка отсутствует, вывод тремя строками без обрамления:
```
  raxd  —  Remote Access Daemon
  v1.0.0  ·  abc1234  ·  2025-06-01
  Vladimir Kovalev, OEM TECH
```

**`raxd status`:** key-value строки переносятся естественно, не ломаются. Путь может уходить за
границу — это ожидаемо и корректно (терминал перенесёт). Усечения нет: путь обрезать нельзя,
пользователь должен видеть полный путь.

**`raxd key list`:** при ширине < 56 символов `LABEL` усекается первой (до ширины `LABEL → 12`),
затем при < 46 символах — до минимальной таблицы из ID + LABEL. `olekukonko/tablewriter`
управляет шириной колонок через `SetColWidth`. Заголовки не усекаются никогда.

**Пайп и перенаправление:** вывод корректен при `raxd status > file.txt`,
`raxd key list | less`, `raxd version | grep`. Ширина терминала не предполагается — вывод
не зависит от `COLUMNS`/`tput cols` на стадии bootstrap.

---

## Сводка контрактов вывода

| Команда         | Канал stdout | Канал stderr     | Exit 0 | Exit 1      |
|-----------------|-------------|-------------------|--------|-------------|
| banner          | —           | всегда (PreRun)   | —      | —           |
| `version`       | версия      | баннер            | да     | —           |
| `status`        | state/paths | баннер            | да     | —           |
| `key create`    | —           | баннер + error:   | —      | всегда      |
| `key list`      | —           | баннер + error:   | —      | всегда      |
| `key delete`    | —           | баннер + error:   | —      | всегда      |
| `config port`   | —           | баннер + error:   | —      | всегда      |
| `serve`         | —           | баннер + error:   | —      | всегда      |

Заглушки завершаются с exit 1 — cobra получает ошибку из `RunE` и сообщает через `os.Exit(1)`.

---

*Артефакт задачи: `bootstrap-cli`. Контракт для: developer. Проверяет: cli-ux-guardian.*
*Автор продукта: Vladimir Kovalev, OEM TECH.*
