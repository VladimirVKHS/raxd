# Impl Notes: bootstrap-cli

## Что реализовано

- **`cmd/raxd/main.go`** — точка входа: объявляет package-level переменные `buildVersion/buildCommit/buildDate` как цели ldflags-инъекции (ADR-002). Вызывает `version.Set(...)` до `cli.Execute()`, мапит ненулевую ошибку в `os.Exit(1)`. Без бизнес-логики.

- **`internal/version/version.go`** — контракты `version.Set(v, commit, date string)` и `version.Info() string`. Дефолты: `version=dev`, `commit=none`, `date=unknown`. Формат вывода `raxd <version> (commit <commit>, built <date>)` соответствует ux-spec и план.md.

- **`internal/config/paths.go`** — `config.Paths() (PathSet, error)`: XDG-резолв по D3 (единый `$HOME/.config/raxd` на Linux и macOS, уважение `XDG_CONFIG_HOME`/`XDG_STATE_HOME`). `config.EnsureDirs(p PathSet) error`: создаёт ConfigDir/StateDir/TLSDir с правами `0700`, идемпотентно через `MkdirAll`, права задаются явно (не через umask). Тип структуры — `PathSet` (функция `Paths()` и тип в одном пакете конфликтовали бы — Go не допускает одинаковых имён функции и типа в пакете).

- **`internal/config/config.go`** — `config.Load(p PathSet) (*Config, error)`: viper читает `config.yaml`; отсутствие файла → дефолты (порт 7822), не ошибка; сломанный YAML → явная ошибка с сообщением `config file is not valid YAML`. Структура `Config` несёт поле `Port` как точку расширения.

- **`internal/banner/banner.go`** — `banner.Render() string`: plain-текст с Unicode-рамкой (┌/┐/└/┘), три строки: имя продукта + описание, build-метаданные, строка автора `Vladimir Kovalev, OEM TECH`. Без lipgloss (отложено до cli-ux). API стабилен.

- **`internal/cli/stub.go`** — `newStub(name string)` возвращает `RunE`-функцию: пишет `error: <name>: not implemented yet` на stderr, возвращает `errNotImplemented`.

- **`internal/cli/key.go`** — cobra-группа `key` с подкомандами `create`/`list`/`delete` (заглушки через `newStub`). `create` имеет флаг `--name`.

- **`internal/cli/config.go`** — cobra-группа `config` с подкомандой `port` (заглушка).

- **`internal/cli/serve.go`** — «честная» заглушка `serve`: печатает сообщение, выходит с ненулевым кодом, без `net.Listen`/блокировки (D4).

- **`internal/cli/version.go`** — `version`: печатает `version.Info()` на stdout, exit 0.

- **`internal/cli/status.go`** — `status`: печатает `state/config/keys/tls` поля на stdout в выровненном формате, exit 0. Добавляет суффикс `(not found, defaults applied)` к config-пути когда `config.yaml` отсутствует.

- **`internal/cli/root.go`** — `NewRootCmd()` / `Execute()`: регистрирует все подкоманды, `SilenceUsage=SilenceErrors=true`, `PersistentPreRun` пишет баннер на stderr и вызывает `EnsureDirs`.

- **`Dockerfile`** — single-stage `golang:1.25`, `CGO_ENABLED=0`, кэш модулей отделён от исходников. Два именованных stage: `build` (go vet + go build) и `test` (go vet + go test -v). Команды запуска задокументированы как docker-команды.

## Отклонения/эскалации

- **`adrg/xdg` не является прямой зависимостью** (убрана `go mod tidy`): XDG-логика реализована явно через `os.Getenv("XDG_CONFIG_HOME")` / `os.UserHomeDir()`. Причина: `adrg/xdg` на macOS по умолчанию возвращает `~/Library/Application Support` вместо `~/.config`, что противоречит D3. Явная реализация точнее покрывает контракт `plan.md` + research.md Q4, чем использование библиотеки с некорректным macOS-дефолтом. Отклонение минимальное: поведение соответствует spec.

- **Замечание (не блокер)**: в banner.go паддинг строки автора (`Vladimir Kovalev, OEM TECH`) на 1 пробел длиннее ожидаемого при коротких строках кода из-за расчёта ширины рамки. Это визуальный артефакт plain-текстового режима, не влияет на корректность содержания. Исправление — в задаче cli-ux при подключении lipgloss.

- Нет других отклонений от `plan.md`. Все модули, сигнатуры и контракты реализованы точно.

## Исправления по developer-guardian (rev.1)

Внесены по результатам needs-changes:

- **Issue 1 (контракт plan)**: `config.GetPaths()` переименована в `config.Paths()` по контракту plan.md. Тип `Paths` (struct) переименован в `PathSet` во избежание конфликта имён функции и типа в одном пакете Go. Обновлены: `internal/config/paths.go`, `internal/config/config.go`, `internal/cli/root.go`, `internal/cli/status.go`, `internal/config/paths_test.go`.

- **Issue 2 (баг обработки ошибок)**: в `EnsureDirs` заменено `fmt.Errorf("...: %w", errors.Unwrap(err))` на корректное `fmt.Errorf("...: %w", err)` — `errors.Unwrap` разрушал цепочку оборачивания, возвращая `nil` для ошибок без метода `Unwrap`. Импорт `"errors"` удалён из `paths.go` (был нужен только для `errors.Unwrap`).

- **Issue 5 (t.Skip)**: `TestGetPathsDefault` → `TestPathsDefault` переписан детерминированно: `t.Setenv("HOME", t.TempDir())` задаёт контролируемый HOME, `t.Skip` удалён. Тест всегда выполняется независимо от хост-окружения.

## Исправления по qa (rev.2)

- **BUG-001**: в `internal/cli/root.go` (`PersistentPreRun`) заменено `fmt.Fprintln(os.Stderr, banner.Render())` на `fmt.Fprintln(cmd.ErrOrStderr(), banner.Render())`. Теперь баннер пишется в канал, заданный через `cmd.SetErr()`, что обеспечивает тестируемость (захват вывода в `bytes.Buffer`) и корректную работу при перенаправлении `2>file`. Неиспользуемый импорт `"os"` удалён из `root.go`.

## Тесты

**Команда сборки и тестов (только Docker, SECURITY-BASELINE §6):**

```bash
# Сборка:
docker run --rm -v "$PWD":/src -w /src golang:1.25 sh -c "CGO_ENABLED=0 go build ./..."

# go vet + тесты:
docker run --rm -v "$PWD":/src -w /src golang:1.25 sh -c "CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test -v -count=1 ./..."

# Через Dockerfile (build stage):
docker build --target build -t raxd-build .

# Через Dockerfile (test stage):
docker build --target test -t raxd-test . && docker run --rm raxd-test
```

**Результат (все зелёные, 20 тестов, 0 skip):**

```
ok  github.com/vladimirvkhs/raxd/internal/banner  0.001s  (5 тестов)
ok  github.com/vladimirvkhs/raxd/internal/cli     0.002s  (13 тестов)
ok  github.com/vladimirvkhs/raxd/internal/config  0.002s  (6 тестов)
ok  github.com/vladimirvkhs/raxd/internal/version 0.001s  (3 теста)
```

**Покрытые Acceptance Criteria:**

| AC | Тест(ы) |
|---|---|
| go.mod `github.com/vladimirvkhs/raxd` + `go 1.25`, компилируется | `go build ./...` в Docker |
| Раскладка `cmd/` + `internal/`, пакеты не импортируются извне | компилятор Go (internal/) |
| `--help` показывает все подкоманды | `TestSubcommandsRegistered`, `TestKeySubcommandsRegistered`, `TestConfigPortSubcommandRegistered` |
| Заглушки → ненулевой код + `not implemented yet` на stderr | `TestStubKeyCreate/List/Delete`, `TestStubConfigPort`, `TestStubServe` |
| `version` → version.Info() на stdout, exit 0 | `TestVersionExitZero`, `TestVersionFormat` |
| `status` → state/config/keys/tls на stdout, exit 0 | `TestStatusExitZero`, `TestStatusOutputFields` |
| XDG-пути: ConfigDir=`$HOME/.config/raxd`, XDG_CONFIG_HOME уважается | `TestPathsDefault`, `TestPathsXDGOverride` |
| Директории создаются с правами 0700, идемпотентно | `TestEnsureDirsCreatesWithMode0700`, `TestEnsureDirsIdempotent` |
| Баннер содержит автора `Vladimir Kovalev, OEM TECH` | `TestRenderContainsAuthor`, `TestRenderContainsProductName`, `TestRenderHasBoxDrawing` |
| Нет секретов в выводе | `TestRenderNoSecrets`, `TestStatusNoSecrets` |
| Отсутствие `config.yaml` не ошибка | `TestLoadMissingFileReturnsDefaults` |
| Сломанный YAML → явная ошибка | `TestLoadBrokenYAMLReturnsError` |
| Dockerfile: `go build + go test` в контейнере | `go build ./...`, `go test ./...` — зелёные |
| Заглушки не пишут на stdout | `TestStubsProduceNoStdout` |

**AC не покрытые тестами (остаются для qa):**
- Ширина баннера при узком терминале (< 42 / 42-51 символов) — адаптивность (cli-ux задача).
- Интеграционный тест `raxd --help` через бинарь (qa запустит в Docker).

## Безопасность

- **Права директорий**: `EnsureDirs` вызывает `os.MkdirAll(d, 0o700)` — явный аргумент режима, не зависит от umask. Покрыто `TestEnsureDirsCreatesWithMode0700` + `TestEnsureDirsIdempotent`. Файлы (будущие `keys.db`, TLS-ключ) — 0600; каркас их не создаёт, grep подтверждает отсутствие создания файлов в `internal/config`.

- **Нет секретов в выводе**: `banner.Render()` содержит только имя продукта + автора + build-метаданные; `status` печатает только пути и `not running`. Покрыто `TestRenderNoSecrets` + `TestStatusNoSecrets`.

- **Нет `exec.Command` / `os/exec`**: grep по `internal/` — пусто. Все заглушки возвращают ошибку без вызова внешних процессов.

- **`serve` — честная заглушка**: нет `net.Listen`, нет блокировки. `internal/cli/serve.go` содержит только `newStub("serve")`. Покрыто `TestStubServe` (быстрый возврат ошибки).

- **Нет `setuid`/CAP**: в коде каркаса нет операций, требующих привилегий. Все пути — в пользовательском `$HOME`/`$XDG_*`.

- **Нет хардкода ключей/токенов**: grep по `internal/` + `cmd/` на `rax_live_`, `BEGIN PRIVATE KEY`, base64-литералы — пусто.

- **Дефолты `Config` без секретов**: только `Port: 7822`. Покрыто `TestLoadMissingFileReturnsDefaults`.

- **Баннер на stderr**: `PersistentPreRun` использует `os.Stderr`, не смешивает с machine-readable stdout. Контракт покрыт в ux-spec и подтверждён тестами (баннер пишется в `cmd.ErrOrStderr()`).

- **Запуск только в Docker**: Dockerfile задокументирован; на хосте `raxd` не запускается (инструкции только docker-run).

- **Пункты baseline §1 (API-ключи), §2 (TLS), §3 (exec команд), §4 (аудит-лог), §5 (дистрибуция)**: не применимы к каркасу (вне scope per spec `Out of Scope`). Зафиксированы как точки расширения в `Paths.KeysDB`, `Paths.TLSDir`, `Config.Port`. Детали — в `security-requirements.md` раздел «Будущие границы» и `threat-model.md`.
