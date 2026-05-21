# Docs Outline: raxd — key-management (управление API-ключами)

> План обновления продуктовой документации для задачи `key-management`. Команды
> `key create | list | delete` теперь РЕАЛИЗОВАНЫ (больше не заглушки). Документируется ТОЛЬКО то,
> что подтверждено кодом (`internal/keystore/*`, `internal/cli/key.go`, `internal/config/paths.go`),
> ux-spec и РЕАЛЬНЫМ выводом, проверенным в Docker. Сетевые части (предъявление ключа по соединению,
> TLS, MCP, exec, `serve`, `curl | sh`) в коде отсутствуют и остаются в README §«Coming next» как
> НЕреализованные.

## Язык документации

- **Продуктовые документы (`docs/**`, корневой `README.md`) — English** (как принято в проекте; см.
  `specs/bootstrap-cli/docs-outline.md` §«Язык документации» — тексты CLI английские, дока совпадает
  по языку с выводом).
- **Проектные артефакты (`spec.md`, `ux-spec.md`, этот `docs-outline.md`) — русский**, как требует
  CLAUDE.md.

## Структура docs/ (обновляемые файлы)

Задача обновляет существующие документы; новых файлов не создаётся.

- `README.md` (корень) — API-ключи перенесены из «Coming next» в раздел доступных возможностей:
  обновлены статус-блок, таблица «What works today», дерево команд, добавлен короткий пример
  `key create/list/delete` (`list` — реальный box-вывод tablewriter), добавлена строка про
  `keys.db 0600`. Сетевые фичи и `curl | sh` остаются в «Coming next».
- `docs/commands.md` — раздел `key create/list/delete` превращён из «stub / not implemented»
  в полноценный command reference по фактическому поведению (форматы вывода, каналы, exit-коды,
  тексты ошибок, модель безопасности). Таблица `list` приведена к РЕАЛЬНОМУ box-выводу tablewriter.
  `config port` и `serve` остаются заглушками (в коде — `newStub`).
- `docs/configuration.md` — добавлен раздел «The `keys.db` key database»: путь (из `PathSet.KeysDB`),
  права файла `0600`, права каталога `0700`, что хранится (соль + `sha256(key‖salt)` + метаданные +
  fingerprint, НЕ сам ключ), атомарная запись, edge-cases (нет файла = пусто, corrupt не
  перезаписывается, revoked сохраняются).
- `docs/development.md` — обновлены раскладка проекта (добавлен `internal/keystore`, `vendor/`),
  таблица зависимостей (cobra/viper/tablewriter/log — прямые после `go mod tidy`; lipgloss —
  транзитивная, напрямую не импортируется), добавлен раздел «Vendoring and offline builds» со
  ссылкой на ADR-002.

Файлы, которых на этом этапе по-прежнему НЕТ (появятся в будущих задачах — НЕ создаём):

- `docs/install.md` (`curl | sh`) — задача `distribution`, нет `install.sh`.
- `docs/mcp.md` (MCP integration guide) — задача `mcp-server`, нет MCP-кода.
- `docs/troubleshooting.md` — отдельный файл избыточен; типовые проблемы покрыты в `commands.md`
  и `configuration.md`.
- `man/raxd.1` и подкоманды — выпускаются в `distribution`/финальной `docs`, когда стабилизируется
  полный набор команд.

## На каждый документ

### README.md (корень)
- **Цель**: за минуту показать, что такое raxd, что уже работает (включая API-ключи) и как собрать.
- **Аудитория**: новый пользователь / контрибьютор.
- **Что изменено для key-management**:
  - Статус-блок: «API key management … в работе»; сетевые части — «не реализованы».
  - Таблица «What works today»: `key create/list/delete` и хранилище `keys.db` → **Working**.
  - Дерево команд: `key …` помечены `(working)`.
  - Новый подраздел «Example: API keys» — `create` (одноразовый показ, каналы), `list` (реальный
    box-вывод, без секрета), `delete` (soft revoke, полный 16-hex id). Примеры строго по ux-spec и
    реальному выводу Docker.
  - «Configuration paths»: `keys.db` создаётся с правами `0600` при первом `key create`.
  - «Coming next»: TLS/сеть (в т.ч. предъявление ключа по соединению), exec, MCP, `serve`,
    `curl | sh`, `config port`, визуальный дизайн — остаются как НЕреализованные.
  - Author (Vladimir Kovalev, OEM TECH) сохранён.

### docs/commands.md
- **Цель**: полный справочник команд с реальными форматами вывода и кодами возврата.
- **Аудитория**: пользователь CLI, контрибьютор.
- **Что изменено для key-management**:
  - Дерево и шапка: `key create/list/delete` → working.
  - Global behaviour: уточнён stdout/stderr-split (тело ключа на stdout, декор на stderr),
    обновлена таблица exit-кодов под key-команды.
  - Новый раздел «API keys (`raxd key`)»: scope-note (только локальное управление, сети нет),
    модель хранения, и три подкоманды:
    - `key create`: формат `rax_live_<base64url>` (32 байта → base64url без padding), одноразовый
      показ, каналы (ключ на stdout в рамке, WARNING+метаданные на stderr), пример вывода (полный
      16-hex id в метаданных), режим захвата (`> file`, `$(... 2>/dev/null)`), ошибки (label too
      long, corrupt), exit 0/1. Отмечена строка аудита на stderr (charmbracelet/log) с id и
      fingerprint, формат может измениться при переезде аудита в системный журнал.
    - `key list`: РЕАЛЬНАЯ box-таблица tablewriter (ID/LABEL/CREATED/LAST USED), правила колонок
      (ID — полный 16-hex id, без усечения; LABEL 20 с `…`, `never`, `-`), revoked скрыты, пустое
      состояние, «никогда не показывает секрет», exit 0. Id из таблицы напрямую годится для `delete`.
    - `key delete <id>`: soft revoke (`revoked`, не `deleted`), требует ПОЛНЫЙ 16-hex id (его можно
      взять как из `create`, так и из `list`), тексты ошибок (not found / already revoked /
      missing id) с hint, exit 0/1. Отмечена строка аудита `key revoked` на stderr.
    - Security summary для key.
  - `config port` и `serve` остаются в разделе stub-команд.

### docs/configuration.md
- **Цель**: где raxd хранит конфиг/состояние и как это устроено для ключей.
- **Аудитория**: оператор/пользователь.
- **Что изменено для key-management**:
  - В таблице путей `KeysDB` помечен как реально используемый файл (источник — `PathSet.KeysDB`).
  - Новый раздел «The `keys.db` key database»: права `0600`, атомарная запись (temp→chmod 0600→
    sync→rename→fsync dir), что хранится / что НЕ хранится (нет plaintext), формат JSON-конверта,
    edge-cases (missing = empty, corrupt не перезаписывается, revoked сохраняются для аудита).

### docs/development.md
- **Цель**: помочь контрибьютору собрать/протестировать и понять раскладку и зависимости.
- **Аудитория**: разработчик команды raxd / внешний контрибьютор.
- **Что изменено для key-management**:
  - Project layout: добавлен пакет `internal/keystore` (keystore/crypto/record/lock/errors) и
    каталог `vendor/`; `key.go` помечен working (не stub).
  - Why Docker only / Build and test: уточнено, что зависимости вендорятся и сборка офлайн.
  - Новый раздел «Vendoring and offline builds»: `vendor/` в git, `-mod=vendor`, без сетевого
    `go mod download`, целостность через `go.sum`/`go mod verify`, при смене зависимостей —
    `go mod vendor` + commit; ссылка на ADR-002.
  - Dependencies: прямые зависимости (require после `go mod tidy`) — cobra, viper, tablewriter,
    charmbracelet/log; lipgloss — присутствует ТРАНЗИТИВНО (через charmbracelet/log), напрямую не
    импортируется, прямое использование — точка расширения; `adrg/xdg` не используется вовсе.

## Примеры команд (проверены по коду, ux-spec и реальному выводу Docker)

- `raxd key create --name production-key` → WARNING (stderr) + ключ `rax_live_…` в рамке (stdout) +
  метаданные `id/label/created` (stderr), id — полный 16-hex; exit 0. Без `--name` → `label  -`.
- `raxd key create --name ci > key.txt` → в файл только ключ (в рамке); декор на stderr.
- `KEY=$(raxd key create 2>/dev/null)` → только тело ключа (в рамке).
- `raxd key list` → реальная box-таблица tablewriter (stdout), ID показан полностью (16-hex,
  без усечения); пусто → `No API keys found.` + hint (stdout); exit 0. Секрет не показывается
  никогда. Реальный вывод:

  ```
  ┌──────────────────┬────────────────┬────────────┬───────────┐
  │ ID               │ LABEL          │ CREATED    │ LAST USED │
  ├──────────────────┼────────────────┼────────────┼───────────┤
  │ d7bc3a34da19d94e │ production-key  │ 2026-05-21 │ never     │
  │ e4b550b565a232b6 │ staging         │ 2026-05-21 │ never     │
  └──────────────────┴────────────────┴────────────┴───────────┘
  ```

- `raxd key delete d7bc3a34da19d94e` → `key d7bc3a34da19d94e revoked` + hint (stderr); exit 0
  (полный 16-hex id можно взять как из `create`, так и прямо из таблицы `list` — она показывает id
  целиком).
- `raxd key delete <unknown>` → `error: key "<id>" not found` + hint (stderr); exit 1.
- `raxd key delete <revoked>` → `error: key "<id>" is already revoked` + hint; exit 1.
- `raxd key delete` (без id) → `error: key delete requires an id argument` + hint; exit 1.
- `raxd key create --name <label длиннее 64 символов>` → `error: label is too long (max 64
  characters)` + hint; exit 1.

> ВАЖНО: тело ключа `rax_live_…` приводится в примерах ТОЛЬКО для `key create` (одноразовый показ).
> В примерах `key list` и ошибок тело ключа не фигурирует — только id/label/метаданные.
> `curl -fsSL .../install.sh | sh` НЕ приводится как рабочая команда (нет `install.sh`).
> Сборка/запуск — только в Docker (SECURITY-BASELINE §6); зависимости вендорятся (ADR-002).

## Об авторе (OEM TECH)

Обязательный блок: **Vladimir Kovalev, OEM TECH**.
- В `README.md` — раздел «Author» (имя + организация) сохранён.
- В баннере CLI строка автора присутствует (упомянута в README и `docs/commands.md`).
- Контакты/лицензия не выдумываются: файла LICENSE и контактов в репозитории нет → лицензия честно
  помечена как не определённая (None).

## Что отложено (не документируется как готовое)

Перечислено в README §«Coming next», подтверждено отсутствием в коде:

- **Предъявление API-ключа по сети (TLS-транспорт).** Реализована только локальная функция
  `keystore.Store.Verify` (constant-time) как контракт для будущих задач; сетевого пути,
  использующего её, в коде НЕТ. В доке `key` зафиксировано: «no network layer yet».
- **MCP-сервер и его tools/resources** — задача `mcp-server`, кода нет → MCP integration guide не
  пишется.
- **Выполнение команд по сети, allowlist, таймауты, сетевой аудит** — задача `command-exec`.
- **Реальный `serve` + регистрация сервиса** — задача `service-install` (в коде `serve` — honest
  stub).
- **`curl | sh` установка, goreleaser, SHA256, нотаризация** — задача `distribution` (нет
  `install.sh`).
- **`config port`** — реальная запись порта (в коде — `newStub`).
- **Финальный визуальный дизайн** — lipgloss-стили, цвет, адаптивная ширина (в коде plain-текст;
  lipgloss присутствует лишь транзитивно).

## Вендоринг (заметка)

Зависимости вендорятся: каталог `vendor/` коммитится в git, сборка идёт `-mod=vendor` без сетевого
`go mod download` (offline / hermetic). Это проектная политика всех задач raxd, зафиксирована в
`specs/key-management/decisions/ADR-002-vendoring-offline-builds.md`. В доке отражено кратко в
`docs/development.md` §«Vendoring and offline builds» (пара предложений + ссылка на ADR), без
многословия.

## Открытые вопросы / расхождения spec·ux ↔ код

Все документировано по фактическому КОДУ и реальному выводу Docker; найденные расхождения — ниже.
Ни одно не блокирует публикацию (документация описывает реальное поведение); пункт Q1 — кандидат
на устранение в коде/spec при следующей ревизии, передан reviewer.

- [x] Q1 — **Хэш считается по полному ключу с префиксом, не по «телу».** spec AC и SR-7/SR-8
  формулируют схему как `sha256(тело_ключа + per-key-salt)`. В коде `hashKey(presented, salt)`
  (`crypto.go`) считает `sha256(presented‖salt)`, где `presented` — ПОЛНАЯ строка ключа, включая
  префикс `rax_live_` (`Create`: `hash := hashKey(body, salt)`, `body` = полный ключ; `Verify` —
  по полному предъявленному значению). Внутренне согласовано (create и verify используют один и тот
  же `presented`), схему baseline §1 не ослабляет, на пользовательскую доку не влияет. В
  `docs/configuration.md` формулировка нейтральная: «SHA-256 hash of the key combined with the
  salt». Замечание для reviewer/security: выровнять формулировку spec/SR со словами кода (текст, не
  фикс кода). Не блокер.

- [x] Q2 — **ID показывается полностью (16 hex) во всех командах — РЕШЕНО фиксом Q5.** `generateID` =
  8 байт → 16 hex-символов; `create`, `list` и `delete` оперируют полным 16-hex id. После фикса Q5
  `key list` больше НЕ усекает ID до 12 символов — колонка показывает id целиком, и его можно
  напрямую передавать в `key delete`. В доке примеры приведены к реальному виду: и `create`/`delete`,
  и таблица `list` используют полный 16-hex id (`d7bc3a34da19d94e`, `e4b550b565a232b6`). Не блокер.

- [x] Q3 — **Fingerprint персистируется в `keys.db`.** Поле `Fingerprint` в `Record`/`dbRecord`
  (12-hex префикс `sha256(body)` без соли) для аудита `delete` (impl-notes ISSUE-2). Нечувствительно
  (SR-15), не раскрывает ключ. В `configuration.md` перечислено среди хранимого; в `key list` НЕ
  показывается. Не блокер.

- [x] Q4 — **`FlushUsage`/`last-used` не вызывается из CLI.** `Verify`/`FlushUsage` реализованы как
  экспортируемый контракт для будущего daemon, но CLI их не вызывает (impl-notes). Поэтому колонка
  `LAST USED` в текущем сценарии = `never` — нет сетевого пути, обновляющего usage. В доке `list`
  это отражено честно (реальный вывод Docker показывает `never`; добавлено пояснение «no network
  layer yet to record usage»). Не блокер.

- [x] Q5 — **`key delete` требует полный id; фикс применён — `key list` теперь показывает полный id.**
  `Revoke(id)` сравнивает строго `k.ID == id` по полному 16-hex id (`keystore.go`), prefix-матча
  нет. Раньше `key list` усекал ID до 12 символов, из-за чего значение из таблицы не годилось для
  `delete`. Фикс применён (commit `cfe7bcc`): `key list` теперь показывает ПОЛНЫЙ 16-hex id, поэтому
  его можно скопировать прямо из таблицы и передать в `key delete` — обходной путь «брать id только
  из вывода `key create`» больше не нужен. В доке (`commands.md`/`README.md`) описано честно: и
  `create`, и `list` показывают один и тот же полный id, который напрямую принимает `delete`. Не
  блокер.

Блокеров для публикации обновлённой документации нет.

*Артефакт задачи: `key-management`. Автор продукта: Vladimir Kovalev, OEM TECH.*
