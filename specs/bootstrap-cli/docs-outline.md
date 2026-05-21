# Docs Outline: raxd — bootstrap-cli (каркас)

> Это план продуктовой документации для **текущего состояния каркаса** (`bootstrap-cli`).
> Документируется ТОЛЬКО то, что подтверждено кодом ветки `feature/bootstrap-cli`. Всё, чего нет
> в коде (API-ключи, TLS/сеть, exec, передача файлов, MCP, `curl | sh`, регистрация сервиса),
> идёт в раздел «Coming next» как НЕреализованное и не описывается как рабочее.
>
> Будущая задача `docs` (полная документация продукта) опирается на этот план: расширяет
> разделы по мере появления фич и снимает пометки «not implemented yet».

## Язык документации

- **Продуктовые документы (`docs/**`, корневой `README.md`) — English.**
  Обоснование (из `ux-spec.md` §«Язык интерфейса»): продукт `raxd` международный, все тексты CLI
  (Short/Long/Use, сообщения, баннер, ошибки) — английские. Документация должна совпадать по языку
  с реальным выводом, чтобы примеры были copy-paste-able и не было рассинхрона «русский текст ↔
  английский CLI». English — стандарт для системного инструментария.
- **Проектные артефакты (`spec.md`, `plan.md`, `ux-spec.md`, этот `docs-outline.md`) — русский**,
  как требует CLAUDE.md (язык артефактов проектирования — русский).

## Структура docs/

Документация разделена на корневой README (точка входа для репозитория) + тематические файлы
в `docs/`, чтобы README оставался кратким, а детали не раздували его.

- `README.md` (корень репозитория) — точка входа: что такое raxd, автор, текущий статус (early /
  каркас), быстрый старт через Docker, обзор доступных команд, ссылки на детальные документы,
  «Coming next», блок об авторе. Размещён в корне, потому что это первый файл, который видит
  любой пользователь репозитория на GitHub/в IDE — это канонический вход.
- `docs/commands.md` — command reference: все команды дерева. Рабочие (`version`, `status`) — с
  реальными форматами вывода из кода/ux-spec. Заглушки (`key create|list|delete`, `config port`,
  `serve`) — с честной пометкой «not implemented yet», их usage-строками и реальным выводом ошибки.
- `docs/configuration.md` — пути и конфигурация: единый `~/.config/raxd/`, приоритет
  `XDG_CONFIG_HOME`/`XDG_STATE_HOME`, права директорий `0700`, формат `config.yaml` (поле `port`,
  дефолт 7822), поведение при отсутствующем/битом файле.
- `docs/development.md` — для контрибьюторов: раскладка проекта (`cmd/` + `internal/`), сборка и
  тесты ТОЛЬКО в Docker (точные docker-команды из Dockerfile/impl-notes), как инъектируется версия
  через ldflags.

Файлы, которых на этом этапе НЕТ (появятся в будущих задачах — НЕ создаём сейчас):
- `docs/install.md` (установка `curl | sh`) — задача `distribution`, нет `install.sh`.
- `docs/mcp.md` (MCP integration guide) — задача `mcp-server`, нет MCP-кода.
- `docs/troubleshooting.md` — отдельный файл избыточен для каркаса; типовые проблемы каркаса
  (нет HOME, битый YAML, нет прав) кратко покрыты в `docs/commands.md` и `docs/configuration.md`.
- `man/raxd.1` и подкоманды — man-страницы выпускаются в `distribution`/`docs`, когда стабилизируется
  полный набор команд; на каркасе преждевременны.

## На каждый документ

### README.md (корень)
- **Цель**: дать за минуту понять, что такое raxd, в каком он состоянии и как собрать/запустить.
- **Аудитория**: новый пользователь, потенциальный контрибьютор, оценивающий проект.
- **Ключевые секции**:
  - What is raxd (одно-два предложения + список ролей бинаря из STACK).
  - Project status (early / bootstrap каркас — что работает, чего ещё нет).
  - Requirements (Go 1.25, Docker; сборка/запуск только в Docker по SECURITY-BASELINE §6).
  - Quick start (Docker: build + test одной командой).
  - Available commands (короткая таблица: `version`/`status` — working; остальные — stub).
  - Example output (`version`, `status` — реальные форматы; баннер на stderr).
  - Configuration paths (кратко + ссылка на `docs/configuration.md`).
  - Coming next (Roadmap — НЕреализованные фичи честно).
  - Author (Vladimir Kovalev, OEM TECH).
  - License (None / не определена — честно, файла LICENSE нет).

### docs/commands.md
- **Цель**: полный справочник команд каркаса — что есть, что выводит, какой код возврата.
- **Аудитория**: пользователь CLI, контрибьютор.
- **Ключевые секции**:
  - Command tree (дерево из `raxd --help`).
  - Global behaviour: баннер на stderr перед каждой командой; stdout/stderr-разделение;
    exit codes 0/1.
  - `raxd version` (working): формат, пример, exit 0.
  - `raxd status` (working): поля state/config/keys/tls, суффикс `(not found, defaults applied)`,
    что НЕ показывает (безопасность), exit 0.
  - `raxd key` / `key create` / `key list` / `key delete` (stub): usage, флаг `--name` у create,
    вывод `error: <cmd>: not implemented yet`, exit 1.
  - `raxd config` / `config port` (stub): usage, вывод заглушки, exit 1.
  - `raxd serve` (stub, honest): usage, вывод заглушки, exit 1, явно — НЕ открывает порт.
  - Error format (`error:` / `hint:`; cobra-дефолты для unknown command/flag).

### docs/configuration.md
- **Цель**: объяснить, где raxd хранит конфиг и состояние и как это переопределить.
- **Аудитория**: оператор/пользователь, настраивающий окружение.
- **Ключевые секции**:
  - Path resolution (таблица: ConfigDir/ConfigFile/StateDir/KeysDB/TLSDir и формулы).
  - XDG overrides (`XDG_CONFIG_HOME`, `XDG_STATE_HOME`) с примерами.
  - Directory creation & permissions (создаются при запуске, `0700`).
  - config.yaml format (поле `port`, дефолт 7822; отсутствие файла — не ошибка; битый YAML — ошибка).
  - Что пока НЕ создаётся каркасом (`keys.db`, TLS-файлы — только пути-заготовки).

### docs/development.md
- **Цель**: помочь контрибьютору собрать, протестировать и понять раскладку каркаса.
- **Аудитория**: разработчик команды raxd / внешний контрибьютор.
- **Ключевые секции**:
  - Why Docker only (SECURITY-BASELINE §6 — raxd исполняет команды по сети; на хосте не запускаем).
  - Build & test in Docker (точные команды из Dockerfile/impl-notes: одно-лайнер, build stage,
    test stage).
  - Project layout (`cmd/raxd`, `internal/cli|config|version|banner`; почему `internal/`).
  - Build metadata via ldflags (как подставить version/commit/date; дефолты dev/none/unknown).
  - Dependencies (cobra v1.10.2, viper v1.21.0; почему НЕ `adrg/xdg`; почему пока НЕТ lipgloss).

## Примеры команд

Корректные, проверенные по коду каркаса (`internal/cli/*`, `internal/version`, `internal/config`):

- `raxd version` → `raxd dev (commit none, built unknown)` (dev-сборка) — stdout, exit 0.
- `raxd status` → блок `state/config/keys/tls` — stdout, exit 0.
- `raxd --help` → корень + подкоманды `key`, `config`, `serve`, `version`, `status`.
- `raxd key create --name laptop` → `error: key create: not implemented yet` — stderr, exit 1.
- `raxd key list` → `error: key list: not implemented yet` — stderr, exit 1.
- `raxd key delete <id>` → `error: key delete: not implemented yet` — stderr, exit 1.
- `raxd config port 8080` → `error: config port: not implemented yet` — stderr, exit 1.
- `raxd serve` → `error: serve: not implemented yet` — stderr, exit 1.

Docker (источник истины — `Dockerfile` + `impl-notes.md`):
- `docker build --target test -t raxd-test . && docker run --rm raxd-test` — сборка + тесты.
- `docker build --target build -t raxd-build .` — только сборка бинаря.
- `docker run --rm -v "$PWD":/src -w /src golang:1.25 sh -c "CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./..."`.

> ВАЖНО: `curl -fsSL .../install.sh | sh` НЕ приводится как рабочая команда — `install.sh` ещё нет.
> Установка через `curl | sh` упомянута только в «Coming next» как планируемая.

## Об авторе (OEM TECH)

Обязательный блок: **Vladimir Kovalev, OEM TECH**.
- В `README.md` — раздел «Author» в конце (имя + организация).
- В баннере CLI (через `banner.Render()`) строка автора уже присутствует — упоминается в README и
  `docs/commands.md` как часть вывода.
- Контакты/лицензия не выдумываются: файла LICENSE и контактов в репозитории нет → лицензия честно
  помечается как не определённая (None), контакты не приводятся.

## Что отнесено в «Coming next» (НЕреализовано на каркасе)

Перечислено в README §Coming next как планируемое, с явной пометкой «not yet»:
- API-ключи: реальные `key create/list/delete` (задача `key-management`).
- TLS-транспорт и сетевой TCP/TLS-сервер (задача `tls-transport`).
- Выполнение команд по сети, allowlist, таймауты, аудит-лог (задача `command-exec`).
- MCP-сервер и его tools/transport (задача `mcp-server`).
- Реальный `serve` + регистрация системного сервиса systemd/launchd (задача `service-install`).
- Установка `curl | sh`, goreleaser, релизы, SHA256, нотаризация (задача `distribution`).
- Финальный визуальный дизайн вывода/баннера: lipgloss-стилизация, адаптивная ширина баннера,
  таблица `key list` (задача cli-ux / финальный дизайн).
- `config port` — реальная запись порта в `config.yaml`.

## Открытые вопросы

Расхождения «спека/ux-spec ≠ код» — зафиксированы и отражены в доке честно (документируем КОД):

- [x] Q1 — **Адаптивность баннера не реализована.** `ux-spec.md` описывает wide/narrow/very-narrow
  макеты, но `internal/banner/banner.go` всегда рендерит wide-макет (комментарий в коде:
  «we always render the wide layout. Adaptive sizing is a cli-ux extension point»). Решение: в доке
  баннер описывается как фиксированный (один макет), адаптивность — в «Coming next». Не блокер.
- [x] Q2 — **Версия в баннере/`version` без префикса `v`.** Дефолты `dev/none/unknown`; примеры
  ux-spec с `v1.0.0` — иллюстративны (релизная сборка). В доке примеры даём в dev-форме (как
  выводит каркас по умолчанию) и поясняем, что в релизе значения приходят из ldflags. Не блокер.
- [x] Q3 — **`config.Load` принимает аргумент.** В `plan.md` контракт `config.Load()` без
  аргументов, но код и `impl-notes.md` подтверждают `config.Load(p PathSet) (*Config, error)`.
  Документируется по коду. `Load` пока не вызывается из CLI-команд (точка расширения) — в доке
  `config.yaml` описывается через `status` и контракт, без обещания, что значение порта применяется.
  Не блокер.
- [x] Q4 — **`config port` не пишет порт.** Команда — заглушка (`error: ... not implemented yet`).
  Default-порт 7822 присутствует только как дефолт `viper`/в тексте `--help`. В доке `config port`
  честно описана как stub; «настройка порта» — в «Coming next». Не блокер.

Все вопросы закрыты решением «документируем фактический код, расхождения помечаем». Блокеров для
публикации каркасной документации нет.
