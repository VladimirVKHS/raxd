# Research: Bootstrap CLI — каркас проекта raxd

> Все факты ниже проверены через pkg.go.dev / официальную документацию на 2026-05-21.
> Где источник-агрегатор расходился с pkg.go.dev — за истину взят pkg.go.dev (вкладка versions).
> Я даю факты и варианты для architect; финальную раскладку и выбор версий фиксирует architect.

## Вопросы (привязка к spec)
- Q1: Актуальные на 2026 версии библиотек каркаса (`cobra`, `viper`, `adrg/xdg`, `lipgloss` v2,
  `charmbracelet/log`, `tablewriter`, `kardianos/service`) + breaking changes/нюансы API.
- Q2: Go 1.25 — доступность toolchain, официальный Docker-образ `golang:1.25`, нюансы `go.mod`
  (директивы `go` / `toolchain`).
- Q3: Канонический паттерн инъекции версии/commit/date через `-ldflags -X` (для `raxd version`).
- Q4: Поведение `adrg/xdg` на Darwin vs Linux и как реализовать D3 spec — единый `~/.config/raxd/`
  на обеих платформах без хаков.
- Q5: Раскладка Go-проекта `cmd/raxd` + `internal/...` — современная конвенция, плюсы/минусы для
  одиночного бинаря.
- Q6: Dockerfile для Go dev/test: multi-stage vs single, `CGO_ENABLED=0`, кэш модулей, прогон
  `go test` в контейнере (по SECURITY-BASELINE §6).

---

## Найдено (факт → источник URL)

### Q1. Версии библиотек каркаса (на 2026-05-21)

- **`spf13/cobra` — последняя `v1.10.2`, опубликована 2025-12-03; активно сопровождается.**
  → https://pkg.go.dev/github.com/spf13/cobra?tab=versions
  - **Нюанс / breaking change в линии v1.10:** `v1.10.0` (2025-08-31) поднял `spf13/pflag` до 1.0.8,
    где `ParseErrorsWhitelist` переименован в `ParseErrorsAllowlist`; это «can break builds if both
    pflag and cobra are dependencies». `v1.10.1` восстановил совместимость (pflag v1.0.9). Вывод:
    использовать `cobra v1.10.2` и не пинить `pflag` ниже `v1.0.9`.
    → https://github.com/spf13/cobra/releases
  - Для каркаса (root + подкоманды + `--help`) API стабилен с v1.x; миграция не требуется.

- **`spf13/viper` — последняя `v1.21.0`, опубликована 2025-09-08; поддерживается.**
  → https://pkg.go.dev/github.com/spf13/viper?tab=versions
  - В `v1.20.0` (2025-03-15) добавлены `SetOptions`, `WriteConfigTo`, интерфейсы кодеков и
    `Finder` для поиска конфиг-файлов; для каркаса (чтение `config.yaml` + дефолты) хватает базового API.
    → https://pkg.go.dev/github.com/spf13/viper?tab=versions

- **`adrg/xdg` — последняя `v0.5.3`, опубликована 2024-10-31; поддерживается (релизы в 2024).**
  → https://pkg.go.dev/github.com/adrg/xdg?tab=versions
  - Версия не свежая (последний релиз — конец 2024), но это узкая, стабильная утилита; функционал
    XDG-путей не менялся. Помечаю как «зрелая/стабильная», не «устаревшая» — активность по сути
    не нужна, API минимален. См. Q4 ниже о поведении на Darwin/Linux.
    → https://github.com/adrg/xdg

- **`charmbracelet/log` — последняя `v1.0.0`, опубликована 2026-03-09; активно (вышла из v0.x).**
  → https://pkg.go.dev/github.com/charmbracelet/log?tab=versions
  - Достигла стабильного `v1.0.0` в марте 2026; до этого долго была на `v0.4.x`. Для каркаса
    логирование почти не нужно (можно отложить), но библиотека готова к v1.

- **`olekukonko/tablewriter` — последняя `v1.1.4`, опубликована 2026-03-04; активно.**
  → https://pkg.go.dev/github.com/olekukonko/tablewriter?tab=versions
  - **Крупный breaking change v0.0.x → v1.x:** старый setter-API (`SetHeader`, `Append`, `Render`)
    заменён на builder/options-паттерн (`NewTable(w, opts...)`, конфиг-билдеры). Много гайдов
    в сети написаны под `v0.0.5` — они НЕ применимы к v1. Для bootstrap-cli таблицы не нужны
    (это `key-management`/cli-ux), но при добавлении ориентироваться сразу на v1 API.
    → https://pkg.go.dev/github.com/olekukonko/tablewriter?tab=versions

- **`kardianos/service` — последняя `v1.2.4`, опубликована 2025-07-14; поддерживается.**
  → https://pkg.go.dev/github.com/kardianos/service?tab=versions
  - Между `v1.2.2` (2022) и `v1.2.3/4` (2025) был перерыв — релизы в 2025 подтверждают, что
    проект живой. Для bootstrap-cli НЕ нужен (сервис — задача `service-install`); зафиксировано
    для полноты, но из каркаса можно исключить.

- **`charmbracelet/lipgloss` v2 — ВНИМАНИЕ: канонический модуль сменился.**
  - На `github.com/charmbracelet/lipgloss/v2` (путь из STACK) последнее, что опубликовано —
    pre-release `v2.0.0-beta.3` (2025-07-10); стабильного `v2.0.0` по этому пути нет.
    → https://pkg.go.dev/github.com/charmbracelet/lipgloss/v2?tab=versions
  - **Стабильный v2 живёт по новому пути `charm.land/lipgloss/v2`:** `v2.0.0` (2026-02-24),
    последняя стабильная `v2.0.3` (2026-04-13). README устанавливает именно его
    (`go get charm.land/lipgloss/v2`).
    → https://pkg.go.dev/charm.land/lipgloss/v2?tab=versions
    → https://github.com/charmbracelet/lipgloss
  - Последняя стабильная v1 (старый путь `github.com/charmbracelet/lipgloss`) — `v1.1.0` (2025-03-12).
    → https://pkg.go.dev/github.com/charmbracelet/lipgloss?tab=versions
  - **Breaking changes v1 → v2** (детерминированный рендеринг, ручной downsampling цвета через
    `lipgloss.Println()`, ручное определение фона/adaptive-colors, цвета как `color.Color` вместо
    `TerminalColor`): → https://github.com/charmbracelet/lipgloss/discussions/506
  - Для bootstrap-cli стилизация почти не требуется (баннер можно собрать минимально, дизайн —
    задача cli-ux). Но **STACK содержит устаревший импорт-путь** — это надо поднять architect/STACK-owner.

### Q2. Go 1.25 — toolchain, Docker-образ, go.mod

- **Go 1.25.0 выпущен 2025-08-12; последний патч на сегодня — `go1.25.10` (2026-05-07).**
  → https://go.dev/doc/devel/release
- **Официальный Docker Official Image `golang:1.25` существует**, плюс варианты
  `golang:1.25-bookworm`, `golang:1.25-trixie`, `golang:1.25-alpine` (и пины `1.25.10-*`).
  → https://hub.docker.com/_/golang/tags?name=1.25
- **`go.mod` директивы:**
  - `go 1.25` — это **минимальная требуемая** версия Go. Начиная с Go 1.21 директива не
    «совещательная», а обязательная: toolchain отказывается собирать модуль, объявляющий более
    новую версию Go, чем установленная. → https://go.dev/ref/mod
  - `toolchain` — **рекомендуемая** (не обязательная) версия toolchain; не может быть ниже `go`.
    Добавляется автоматически при `go get`, когда команда обновляет `go`-версию (для
    воспроизводимости). Для каркаса достаточно строки `go 1.25` без явного `toolchain`.
    → https://go.dev/ref/mod
  - Нюанс совместимости: spec задаёт `go 1.25`; собирать в `golang:1.25` корректно. Если в CI
    встанет более старый Go, сборка упадёт по требованию директивы `go 1.25` — это ожидаемое
    поведение, не баг.

### Q3. Инъекция версии/commit/date через `-ldflags -X` (канонический паттерн)

- **Официальное определение линкера:** `-X importpath.name=value` «Set the value of the string
  variable in importpath named name to value. This is only effective if the variable is declared
  in the source code either uninitialized or initialized to a constant string expression. -X will
  not work if the initializer makes a function call or refers to other variables.»
  → https://pkg.go.dev/cmd/link
- **Правила (канон):** переменная должна быть (1) уровня пакета, (2) типа `string`, (3) либо
  не инициализирована, либо инициализирована константным строковым выражением (нельзя вызов
  функции / ссылку на другую переменную). → https://pkg.go.dev/cmd/link
- **Пример паттерна (НЕ код продукта, иллюстрация для architect):** в пакете точки входа объявляются
  три package-level string-переменные с осмысленными дефолтами — например `version = "dev"`,
  `commit = "none"`, `date = "unknown"` — а при сборке их значения подставляются линкером через
  набор флагов `-ldflags "-X 'main.version=…' -X 'main.commit=…' -X 'main.date=…'"`, где значения
  берутся из git (`git rev-parse --short HEAD`) и даты (`date -u …`). Конкретную реализацию пишет
  developer; здесь — только описание паттерна.
  → https://www.digitalocean.com/community/tutorials/using-ldflags-to-set-version-information-for-go-applications
- **Подводный камень:** при неверном import-path переменной сборка проходит «молча», а значение
  остаётся дефолтным — поэтому осмысленные дефолты (`dev`/`none`/`unknown`) обязательны и закрывают
  AC «при сборке без флагов выводятся осмысленные значения по умолчанию».
  → https://www.digitalocean.com/community/tutorials/using-ldflags-to-set-version-information-for-go-applications
- Замечание про goreleaser: он по умолчанию инжектит ровно такие переменные (`version`/`commit`/
  `date`) этим же механизмом, так что выбор имён согласуется с будущей задачей `distribution`
  (вне scope здесь, но имена лучше выбрать совместимыми). → https://goreleaser.com

### Q4. `adrg/xdg` на Darwin vs Linux + реализация D3 (единый `~/.config/raxd/`)

- **Поведение по умолчанию различается по платформам** (README, раздел «Default locations» — таблица
  с отдельной колонкой macOS):
  - Linux/Unix: `xdg.ConfigHome` по умолчанию `~/.config`.
  - macOS (Darwin): `xdg.ConfigHome` по умолчанию `~/Library/Application Support`.
  → https://github.com/adrg/xdg#default-locations
- **Переменная `XDG_CONFIG_HOME` имеет приоритет на ВСЕХ платформах, включая macOS** — README (раздел
  «Default locations») явно: дефолты применяются только для XDG-переменных, которые «empty or not
  present in the environment», то есть выставленная переменная переопределяет дефолт и на Darwin.
  → https://github.com/adrg/xdg#default-locations
- **Как достичь единого `~/.config/raxd/` (D3) без хаков — два честных варианта:**
  - **(а) Не полагаться на дефолт `ConfigHome` на macOS**, а явно строить путь от домашней
    директории: `~/.config/raxd/` (на обеих ОС одинаково). Это осознанное отклонение от
    «нативного» macOS-расположения, но ровно то, что зафиксировано в D3 spec.
  - **(б) Использовать `XDG_CONFIG_HOME`/`xdg.ConfigHome`**, понимая, что на «чистом» macOS без
    выставленной переменной дефолт будет `~/Library/Application Support` — то есть для гарантии
    `~/.config/raxd/` на macOS вариант (б) недостаточен без явного переопределения.
  - **Вывод по выполнимости D3:** да, единый `~/.config/raxd/` достижим без хаков — через явное
    построение пути от home (`$HOME/.config/raxd`) и сверку с `XDG_CONFIG_HOME`, если она задана.
    `adrg/xdg` тут используется как источник правил/хелперов, но дефолт macOS НЕ берётся.
  → https://github.com/adrg/xdg#default-locations
- Сверка со STACK: STACK допускает на macOS оба варианта (`~/.config/raxd/config.yaml` ИЛИ
  `~/Library/Application Support/raxd/`). Spec D3 жёстко выбирает первый — это сужение STACK, не
  конфликт. → внутренний `.claude/reference/STACK.ru.md` (раздел «Раскладка на диске»)

### Q5. Раскладка проекта `cmd/raxd` + `internal/...`

- **Официальная страница Go про layout** рекомендует: поддерживающие пакеты класть в `internal/`
  (компилятор запрещает их импорт из других модулей — `cmd/go` Internal Directories); `cmd/` —
  общепринятая конвенция для одного/нескольких исполняемых файлов, особенно полезна в смешанных
  репозиториях (команды + импортируемые пакеты). → https://go.dev/doc/modules/layout
- **`internal/` — обеспечивается компилятором Go**, не «соглашением»: пакеты под `internal/`
  нельзя импортировать вне модуля. Это прямо закрывает AC «внутренние пакеты не импортируются
  извне модуля». → https://pkg.go.dev/cmd/go#hdr-Internal_Directories
- **`golang-standards/project-layout` — НЕ официальный стандарт** (README прямо: «This is NOT an
  official standard defined by the core Go dev team»). Использовать как ориентир по `cmd/` и
  `internal/`, но как авторитет цитировать официальную страницу Go, а не этот репозиторий.
  → https://github.com/golang-standards/project-layout
- Для **одиночного бинаря** официальная страница допускает и плоскую раскладку (`main.go` в корне +
  `internal/...`). `cmd/raxd/` оправдан, если планируется рост (несколько бинарей/инструментов) или
  ради единообразия с goreleaser-матрицей. → https://go.dev/doc/modules/layout

### Q6. Dockerfile для Go dev/test (SECURITY-BASELINE §6)

- **Официальный образ `golang:1.25` (Debian/Alpine варианты) пригоден** как база для dev/test.
  → https://hub.docker.com/_/golang/tags?name=1.25
- **`CGO_ENABLED=0` уже зафиксирован в STACK** (статическая сборка, простая дистрибуция) — для
  dev/test-образа это же даёт переносимость бинаря и снимает зависимость от libc; согласуется с
  будущим goreleaser. → внутренний `.claude/reference/STACK.ru.md` (раздел «Кросс-компиляция»)
- **Канонический Go-Docker паттерн (факты, не код продукта):**
  - **multi-stage** (builder `golang:1.25` → runtime `scratch`/`distroless`/`alpine`) уместен для
    *релизного* образа: маленький рантайм. Для **dev/test** часто достаточно single-stage на
    `golang:1.25`, где прогоняются `go build ./...` и `go test ./...` (нужен toolchain).
  - **Кэш модулей**: сначала копировать `go.mod`/`go.sum` и делать `go mod download`, затем копировать
    исходники — слой с зависимостями переиспользуется при изменении только кода; дополнительно
    BuildKit cache mounts (`--mount=type=cache`) для `/go/pkg/mod` и build-cache.
  - Для baseline §6 главное требование выполнимо: внутри образа успешно идут `go build ./...` и
    `go test ./...`; запуск/тесты — только в контейнере, не на хосте.
  → https://hub.docker.com/_/golang
- Открытый момент: точная стратегия (multi-stage с runtime-стейджем сейчас или только dev/test-стейдж)
  — решение architect/devops; для bootstrap-cli AC требует лишь успешные `go build`/`go test` в
  контейнере, рантайм-образ можно отложить до `distribution`.

---

## Варианты (для architect)

### Раскладка проекта (главный архитектурный выбор каркаса)
- **A: `cmd/raxd/main.go` + `internal/...`** — плюсы: масштабируется (легко добавить вторые бинари/
  инструменты), единообразно с goreleaser-матрицей, явная точка входа; минусы: чуть больше «церемонии»
  для одного бинаря. Источник: https://go.dev/doc/modules/layout
- **B: плоско — `main.go` в корне + `internal/...`** — плюсы: минимализм, официально допустимо для
  одиночного бинаря; минусы: при появлении второго executable придётся реструктурировать.
  Источник: https://go.dev/doc/modules/layout
- **C: `cmd/` + `pkg/` + `internal/`** — плюсы: разделяет публичное (`pkg/`) и приватное; минусы:
  для raxd публичного API нет (один бинарь) — `pkg/` лишний, добавляет путаницу. `pkg/` — даже не
  часть официальной страницы layout. Источник: https://github.com/golang-standards/project-layout

### lipgloss v2 — какой импорт-путь
- **A: `charm.land/lipgloss/v2` (стабильный v2.0.3)** — плюсы: стабильный релиз, актуальный README;
  минусы: путь отличается от STACK → нужно обновить STACK. Источник: https://pkg.go.dev/charm.land/lipgloss/v2?tab=versions
- **B: `github.com/charmbracelet/lipgloss` v1.1.0 (стабильный v1)** — плюсы: проверенный, привычный
  API; минусы: v1, не v2 (STACK хочет v2). Источник: https://pkg.go.dev/github.com/charmbracelet/lipgloss?tab=versions
- **C: `github.com/charmbracelet/lipgloss/v2` (старый путь)** — минусы: там только pre-release beta,
  стабильного v2 нет → не брать. Источник: https://pkg.go.dev/github.com/charmbracelet/lipgloss/v2?tab=versions

### Dockerfile dev/test
- **A: single-stage на `golang:1.25`** для dev/test (build+test внутри) — плюсы: просто, закрывает
  baseline §6; минусы: образ большой, не для рантайма. Источник: https://hub.docker.com/_/golang
- **B: multi-stage (builder + распиленный runtime)** — плюсы: маленький рантайм-образ; минусы:
  избыточно для каркаса, рантайм-образ уместнее в `distribution`. Источник: https://hub.docker.com/_/golang

---

## Рекомендация (для architect, не финальное решение)

1. **Раскладка — вариант A (`cmd/raxd` + `internal/...`)**: соответствует официальной странице Go,
   `internal/` гарантирует AC «не импортируется извне» на уровне компилятора, и закладывает рост
   (TLS/MCP/exec приедут отдельными internal-пакетами). `pkg/` (вариант C) для одиночного бинаря не
   нужен. → https://go.dev/doc/modules/layout, https://pkg.go.dev/cmd/go#hdr-Internal_Directories
2. **Версии (зафиксировать на момент 2026-05-21):** `go 1.25` (директива), Docker база `golang:1.25`;
   `cobra v1.10.2` (+ `pflag ≥ v1.0.9`), `viper v1.21.0`, `adrg/xdg v0.5.3`. lipgloss/log/tablewriter/
   kardianos для bootstrap-cli по сути **не нужны** (баннер можно собрать без lipgloss, остальное — в
   других задачах); если architect хочет lipgloss уже сейчас — брать **`charm.land/lipgloss/v2`
   v2.0.3** (новый путь). → ссылки в разделах Q1–Q2.
3. **Версия через `-ldflags -X`** на package-level string-переменные с осмысленными дефолтами
   (`dev`/`none`/`unknown`), имена `version`/`commit`/`date` — совместимы с будущим goreleaser.
   → https://pkg.go.dev/cmd/link, https://goreleaser.com
4. **D3 (`~/.config/raxd/` на обеих ОС) достижим без хаков** — строить путь явно от `$HOME/.config`,
   не полагаясь на macOS-дефолт `adrg/xdg`, и уважать `XDG_CONFIG_HOME`, если задана.
   → https://github.com/adrg/xdg#default-locations
5. **Dockerfile — вариант A (single-stage `golang:1.25`)** для dev/test закрывает baseline §6
   минимальной ценой; рантайм-multi-stage отложить до `distribution`. С кэшем модулей
   (`go.mod`/`go.sum` → `go mod download` → исходники) и `CGO_ENABLED=0`.
   → https://hub.docker.com/_/golang

ADR по двум значимым решениям: **ADR-001** (раскладка `cmd/raxd` + `internal/`) и **ADR-002**
(инъекция версии через `-ldflags -X` с дефолтами) — в `decisions/`.

---

## Открытые вопросы
- [ ] **STACK устарел по lipgloss v2:** указан путь `github.com/charmbracelet/lipgloss` (v2), но
      стабильный v2 переехал на `charm.land/lipgloss/v2` (v2.0.0 — 2026-02-24, последняя v2.0.3 —
      2026-04-13). Нужно решение STACK-owner/architect: обновить STACK на новый путь. (Факт подтверждён:
      https://pkg.go.dev/charm.land/lipgloss/v2?tab=versions — это не блокер bootstrap-cli, т.к. lipgloss
      здесь не обязателен, но контракт STACK неточен.)
- [ ] Минимальная версия Go для `charm.land/lipgloss/v2` не подтверждена из README (источник не дал
      число). Не блокер: для bootstrap-cli мы и так на `go 1.25`, что заведомо ≥ требований lipgloss.
      Требует точной проверки только если architect возьмёт lipgloss в каркас.
