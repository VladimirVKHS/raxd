# Plan: Bootstrap CLI — каркас проекта raxd

## Chosen Approach
Каркас по ADR-001: единая точка входа `cmd/raxd/main.go` + вся логика в `internal/*`. Cobra-дерево
команд собирается в `internal/cli`, метаданные сборки — в `internal/version` (ldflags, ADR-002),
пути/конфиг — в `internal/config` (явное построение `$HOME/.config/raxd` с уважением
`XDG_CONFIG_HOME`, D3), баннер — plain-текст в `internal/banner` (без внешней стилизации; lipgloss
откладываем до cli-ux — см. Trade-offs). Заглушки (`key *`, `config port`, `serve`) возвращают
ошибку `errNotImplemented` → cobra даёт ненулевой exit; `version`/`status` печатают и выходят с 0.
TLS/ключи/exec/MCP присутствуют только как TODO-границы (поля путей в config, без логики).

## Modules
- `cmd/raxd/main.go` — точка входа: объявляет package-level `version/commit/date`, передаёт их в
  `version.Set(...)`, вызывает `cli.Execute()`, мапит её ошибку в `os.Exit` (0/1).
- `internal/cli/root.go` — конструктор корневой команды `raxd`, регистрация подкоманд, `PersistentPreRun` баннера.
- `internal/cli/key.go` — `key` + `create|list|delete` (заглушки через общий хелпер).
- `internal/cli/config.go` — `config` + `port` (заглушка).
- `internal/cli/serve.go` — «честная» заглушка `serve` (печать + ненулевой код, без блокировки).
- `internal/cli/version.go` — `version`: печать `version.Info()`, exit 0.
- `internal/cli/status.go` — `status`: печать состояния «не запущен» + путей, exit 0.
- `internal/cli/stub.go` — общий `errNotImplemented` и фабрика `newStub(name)` для заглушек.
- `internal/version/version.go` — хранение/выдача build-метаданных.
- `internal/config/paths.go` — резолв XDG-путей (config/state/tls) по D3, вручную через `os.Getenv` (без `adrg/xdg`).
- `internal/config/config.go` — `Config`-структура, загрузка через viper, дефолты, создание директорий с правами.
- `internal/banner/banner.go` — текст баннера (имя продукта + автор).
- `Dockerfile` — single-stage `golang:1.25`, прогон `go build ./...` и `go test ./...`.

## Contracts
- `version.Set(v, commit, date string)` — записывает метаданные из main (вызвать до Execute).
- `version.Info() string` — формат `raxd <version> (commit <commit>, built <date>)`; дефолты `dev/none/unknown`.
- `cli.Execute() error` — строит root, исполняет; возвращает ошибку команды (nil → exit 0, иначе exit 1).
- `cli.NewRootCmd() *cobra.Command` — собирает дерево; `SilenceUsage/SilenceErrors=true` (контроль вывода в main).
- `newStub(name string) func(*cobra.Command, []string) error` — RunE, всегда возвращает `fmt.Errorf("%s: not implemented yet", name)` (обёртка `errNotImplemented`); cobra → exit ≠0.
- `config.Paths() (PathSet, error)` — `PathSet{ConfigDir, ConfigFile, StateDir, KeysDB, TLSDir string}`;
  `ConfigDir = XDG_CONFIG_HOME/raxd` если переменная задана, иначе `$HOME/.config/raxd`; state — аналогично через `XDG_STATE_HOME`/`$HOME/.local/state`. Ошибка только при недоступном `$HOME`.
- `config.Load() (*Config, error)` — viper читает `config.yaml`; отсутствие файла НЕ ошибка (дефолты, в т.ч. `Port`); ошибка только при битом YAML.
- `config.EnsureDirs(p PathSet) error` — создаёт ConfigDir/StateDir/TLSDir с `0700` (idempotent, `MkdirAll`); файлы, когда появятся, — `0600`. Ошибка → пробрасывается вызывающему.
- `banner.Render() string` — многострочный plain-текст: название `raxd`, краткое описание, строка автора `Vladimir Kovalev, OEM TECH`. Вызывается в `PersistentPreRun` root (на stderr, чтобы не мешать машиночитаемому stdout).
- `status` Run: печатает `state: not running`, `config: <ConfigFile>`, `keys: <KeysDB>`, `tls: <TLSDir>`; exit 0. Формат — выровненные `key: value` строки (визуал/таблицы — за cli-ux).
- Точки расширения (TODO-границы, без реализации): `Config` несёт поля путей для будущих `internal/keystore`, `internal/tls`, `internal/server`; интерфейсы этих пакетов вводят соответствующие task-id.

## Trade-offs
- Баннер **plain (stdlib `fmt`/строки) вместо lipgloss** сейчас. Цена: при подключении lipgloss
  у cli-ux баннер перепишут на стилизацию — но API `banner.Render() string` сохранится, рефактор
  локальный. lipgloss в каркасе = лишняя зависимость без визуальной задачи (дизайн вне scope).
- Когда lipgloss понадобится — брать **`charm.land/lipgloss/v2`** (стабильный v2.0.x), НЕ
  `github.com/charmbracelet/lipgloss/v2` (там только beta). **STACK требует синхронизации**: путь
  в STACK устарел — эскалация STACK-owner (research, открытый вопрос). Не блокер bootstrap-cli.
- **Отклонение от STACK ПРИНЯТО (developer-guardian Issue 3):** `adrg/xdg` НЕ подключается; XDG-пути
  резолвятся вручную через `os.Getenv` (`XDG_CONFIG_HOME` → иначе `$HOME/.config/raxd`; state —
  аналогично). Причина: macOS-дефолт `adrg/xdg` (`~/Library/Application Support`) конфликтует с D3
  (единый `~/.config/raxd` на обеих ОС). Согласуется с research Q4 и формулировкой Chosen Approach
  («явное построение `$HOME/.config/raxd`, не полагаясь на macOS-дефолт adrg/xdg»). Поведенческий AC
  (единый путь + приоритет `XDG_CONFIG_HOME`) не меняется и покрыт тестами. Параграф spec.md про
  «(adrg/xdg + viper)» — иллюстративен и заменён этим решением. Цена: ручной резолв вместо библиотеки
  (несколько строк `os.Getenv`/`filepath.Join`) — закрыт юнит-тестами `config.Paths()`.
- Раскладка `cmd/raxd` + `internal/` вместо плоского `main.go` (ADR-001): цена — чуть больше
  «церемонии» на минимальном каркасе; выигрыш — компиляторная изоляция `internal/` (AC) и рост.
- Версия через ldflags вместо `debug.ReadBuildInfo` (ADR-002): цена — молчаливый дефолт при неверном
  import-path → закрывается юнит-тестом `version.Info()` и осмысленными дефолтами.
- Dockerfile **single-stage `golang:1.25`** вместо multi-stage: цена — большой образ (для dev/test
  норма); рантайм-multi-stage отложен в `distribution`. С кэшем модулей и `CGO_ENABLED=0` (STACK).
- Зависимости каркаса: `cobra v1.10.2` (+ `pflag ≥ v1.0.9`), `viper v1.21.0` — все из STACK, новых нет.
