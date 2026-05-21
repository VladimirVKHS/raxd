# Test Plan: bootstrap-cli — каркас проекта raxd

## Стратегия

Тестируются четыре уровня:

- **Unit** — логика каждого модуля изолированно: `version.Info()`, `banner.Render()`, `config.Paths()`, `config.Load()`, `config.EnsureDirs()`. Файлы: `*_test.go` рядом с пакетом.
- **Integration (CLI)** — сборка дерева команд cobra и их поведение: регистрация команд, exit-коды, форматы вывода, распределение по каналам stdout/stderr. Пакет `internal/cli`, тип `cli_test`.
- **Security-static** — grep-сканирование исходников на запрещённые паттерны (`exec.Command`, `net.Listen`, хардкод секретов, широкие режимы файлов). Пакет корневого модуля `raxd_test`.
- **Install-flow / E2E** — не в scope bootstrap-cli (нет install.sh, нет бинаря, нет systemd/launchd). Scope задачи `distribution`.

Все тесты запускаются исключительно в Docker (SECURITY-BASELINE §6). На хосте `go test` не запускается.

---

## Матрица AC → тест

| AC (из spec.md)                                                         | Уровень      | Тест (файл :: имя)                                              | Статус |
|-------------------------------------------------------------------------|-------------|------------------------------------------------------------------|--------|
| AC-1: `go.mod` — модуль `github.com/vladimirvkhs/raxd`, `go 1.25`     | static      | `security_static_test.go::TestGoModuleNameAndGoVersion`          | green  |
| AC-1: проект собирается без ошибок `go build ./...`                     | build       | `go vet ./... && go build ./...` в Dockerfile stage `build`      | green  |
| AC-2: раскладка `cmd/` + `internal/`, пакеты не импортируются извне    | build       | компилятор Go (internal/ enforcement), `go build ./...`          | green  |
| AC-3: `--help` показывает все подкоманды: key/config/serve/version/status | integration | `cli_test.go::TestSubcommandsRegistered`                        | green  |
| AC-3: `key` содержит `create`/`list`/`delete`                          | integration | `cli_test.go::TestKeySubcommandsRegistered`                      | green  |
| AC-3: `config` содержит `port`                                          | integration | `cli_test.go::TestConfigPortSubcommandRegistered`                | green  |
| AC-4: заглушки завершаются с ненулевым кодом                            | integration | `cli_test.go::TestStubKeyCreate`, `TestStubKeyList`, `TestStubKeyDelete`, `TestStubConfigPort`, `TestStubServe` | green |
| AC-4: сообщение `error: <cmd>: not implemented yet` на stderr           | integration | `cli_gaps_test.go::TestStubErrorMessageContainsCommandName`      | green  |
| AC-4: заглушки не пишут на stdout                                        | integration | `cli_test.go::TestStubsProduceNoStdout`                          | green  |
| AC-4: `serve` — честная заглушка, не блокирует                          | integration | `cli_gaps_test.go::TestServeDoesNotBlock`                        | green  |
| AC-5: `version` печатает версию, commit, дату; exit 0                   | integration | `cli_test.go::TestVersionExitZero`, `TestVersionFormat`          | green  |
| AC-5: формат `raxd <version> (commit <commit>, built <date>)`           | unit        | `version_test.go::TestInfoFormat`, `version_gaps_test.go::TestInfoContainsAllFields` | green |
| AC-5: дефолты `dev`/`none`/`unknown` без ldflags                       | unit        | `version_test.go::TestInfoDefaultValues`, `cli_gaps_test.go::TestVersionDefaultValues` | green |
| AC-5: без литерального `v`-префикса в версии                            | unit/integ  | `version_gaps_test.go::TestInfoNoVPrefix`, `cli_gaps_test.go::TestVersionNoVPrefix` | green |
| AC-6: `status` печатает `state: not running`, exit 0                    | integration | `cli_test.go::TestStatusExitZero`, `cli_gaps_test.go::TestStatusStateNotRunning` | green |
| AC-6: `status` показывает пути config.yaml, keys.db, tls               | integration | `cli_test.go::TestStatusOutputFields`, `cli_gaps_test.go::TestStatusPathSuffixes` | green |
| AC-6: `version`/`status` на stdout, баннер не загрязняет stdout        | integration | `cli_gaps_test.go::TestVersionOnStdout`, `TestStatusOnStdout`, `TestBannerChannelSplit` | green |
| AC-7: XDG — `$HOME/.config/raxd` по умолчанию (Linux и macOS)         | unit        | `paths_test.go::TestPathsDefault`                                | green  |
| AC-7: `XDG_CONFIG_HOME` имеет приоритет                                 | unit        | `paths_test.go::TestPathsXDGOverride`                            | green  |
| AC-7: отсутствие `config.yaml` — не ошибка                              | unit        | `paths_test.go::TestLoadMissingFileReturnsDefaults`              | green  |
| AC-8: директории создаются с правами `0700`                              | unit        | `paths_test.go::TestEnsureDirsCreatesWithMode0700`               | green  |
| AC-8: `EnsureDirs` идемпотентен                                          | unit        | `paths_test.go::TestEnsureDirsIdempotent`                        | green  |
| AC-9: баннер содержит `Vladimir Kovalev, OEM TECH`                      | unit        | `banner_test.go::TestRenderContainsAuthor`                       | green  |
| AC-9: баннер содержит название продукта `raxd`                          | unit        | `banner_test.go::TestRenderContainsProductName`                  | green  |
| AC-9: баннер содержит build-метаданные                                   | unit        | `banner_test.go::TestRenderContainsBuildInfo`                    | green  |
| AC-9: баннер не загрязняет stdout машиночитаемых команд                 | integration | `cli_gaps_test.go::TestBannerChannelSplit`                        | green  |
| AC-10: нет секретов в выводе (version/status/banner)                    | integration | `cli_test.go::TestStatusNoSecrets`, `banner_test.go::TestRenderNoSecrets`, `cli_gaps_test.go::TestVersionOutputNoSecretPatterns`, `TestBannerNoSecretPatterns` | green |
| AC-11: есть `Dockerfile`, внутри проходят `go build` и `go test`       | build       | `docker build --target build` + `--target test` (см. раздел «Как запускать») | green |
| AC-12: unit-тесты покрывают регистрацию команд, exit-коды, XDG         | unit/integ  | весь набор `*_test.go` — зелёный в Docker (см. раздел «Как запускать») | green |

### Дополнительные тесты устойчивости

| Тест (файл :: имя)                                          | Что проверяет                                                                              | Статус |
|-------------------------------------------------------------|-------------------------------------------------------------------------------------------|--------|
| `version_test.go::TestSetPreservesNonEmpty`                 | `version.Set()` корректно сохраняет все три поля; повторный вызов не стирает значения      | green  |
| `banner_test.go::TestRenderHasBoxDrawing`                   | Баннер содержит Unicode box-drawing символы (`┌`/`┐`/`└`/`┘`) — визуальная структура      | green  |
| `cli/security_test.go::TestStubsErrorPrefix`                | Все заглушки выводят `error:`-префикс; дополняет `TestStubErrorMessageContainsCommandName` | green  |

---

## Edge Cases

- **Битый YAML**: `TestLoadBrokenYAMLReturnsError` — явная ошибка с текстом `config file is not valid YAML`.
- **Отсутствует HOME**: контракт `config.Paths()` — ошибка при недоступном `$HOME`; проверяется через детерминированный `t.Setenv("HOME", t.TempDir())` в `TestPathsDefault`.
- **XDG_CONFIG_HOME = ""**: пустая строка приравнивается к «не задано» — проверяется в `TestPathsDefault` через `t.Setenv("XDG_CONFIG_HOME", "")`.
- **Нестандартный umask**: `TestEnsureDirsUmaskIndependent` — umask 022 не влияет на режим 0700.
- **Повторный вызов EnsureDirs**: `TestEnsureDirsIdempotent` — никаких ошибок и расширения прав.
- **Блокирующий serve**: `TestServeDoesNotBlock` — таймаут 2s; падение = реальный баг D4.
- **v-префикс версии**: `TestInfoNoVPrefix` + `TestVersionNoVPrefix` — dev-сборка не производит `vdev`.

---

## Security-тесты

По `security-requirements.md` и `SECURITY-BASELINE.ru.md`:

| Требование                                                              | Тест                                                                 | Статус |
|-------------------------------------------------------------------------|----------------------------------------------------------------------|--------|
| Нет `exec.Command`/`os/exec` в исходниках                              | `security_static_test.go::TestStaticNoExecCommand`                   | green  |
| Нет `net.Listen`/`http.ListenAndServe` в исходниках                    | `security_static_test.go::TestStaticNoNetListen`                     | green  |
| Нет хардкода секретов (`rax_live_`, PEM-заголовки)                     | `security_static_test.go::TestStaticNoHardcodedSecrets`              | green  |
| Нет создания файлов с режимом > 0600 в config-пакете                   | `security_static_test.go::TestStaticNoFileCreationWithWideModes`     | green  |
| EnsureDirs: права 0700 явно, не через umask                             | `config/security_test.go::TestEnsureDirsUmaskIndependent`            | green  |
| EnsureDirs: не создаёт файлы в StateDir/TLSDir (только директории)     | `config/security_test.go::TestEnsureDirsNoFilesCreated`              | green  |
| Дефолты Config без секретов (только Port)                               | `config/security_test.go::TestLoadDefaultsNoSecrets`                 | green  |
| version output без секретов (stdout + stderr)                           | `cli_gaps_test.go::TestVersionOutputNoSecretPatterns`                | green  |
| banner без секретов                                                     | `banner_test.go::TestRenderNoSecrets`, `cli_gaps_test.go::TestBannerNoSecretPatterns` | green |
| status не читает и не печатает содержимое ключей                        | `cli_test.go::TestStatusNoSecrets`                                   | green  |
| serve не блокирует и не открывает порт (D4)                             | `cli_gaps_test.go::TestServeDoesNotBlock` + `TestStaticNoNetListen`  | green  |
| Заглушки не реализуют суррогатную логику ключей                        | `security_static_test.go::TestStaticNoExecCommand` + code review    | green  |

---

## Install-flow тест

Вне scope задачи `bootstrap-cli` (нет install.sh). Тест install-flow — задача `distribution`. Кaркасная проверка: `go.mod` содержит корректный модуль (`TestGoModuleNameAndGoVersion`).

---

## Найденные баги продукта

### BUG-001: `PersistentPreRun` пишет баннер в `os.Stderr` вместо `cmd.ErrOrStderr()` — ИСПРАВЛЕН

**Файл**: `internal/cli/root.go`, строка 28.

**Было**: `fmt.Fprintln(os.Stderr, banner.Render())`

**Стало**: `fmt.Fprintln(cmd.ErrOrStderr(), banner.Render())`

**Статус**: исправлен developer'ом. Тесты `TestStatusOnStdout` и `TestBannerChannelSplit` переведены в полноценный режим: теперь проверяют обе стороны канального разделения (баннер есть в stderr, нет в stdout).

---

## Как запускать

Все команды — только в Docker. На хосте `go test` не запускается (SECURITY-BASELINE §6).

### Unit + Integration + Static (полный прогон)

```bash
# Быстрый вариант (монтирование исходников, без кэша образа):
docker run --rm \
  -v "$PWD":/src \
  -w /src \
  golang:1.25 \
  sh -c "CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test -v -count=1 ./..."
```

### Через Dockerfile (воспроизводимо, с кэшем модулей)

```bash
# Сборка (go vet + go build):
docker build --target build -t raxd-build .

# Тесты (go vet + go test -v):
docker build --target test -t raxd-test . && docker run --rm raxd-test
```

### Только один пакет

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.25 \
  sh -c "CGO_ENABLED=0 go test -v -count=1 -run TestStatic ."
```

### Критерий прохождения

- `go vet ./...` — ноль предупреждений.
- `go test ./...` — все тесты PASS, ни одного FAIL или SKIP.
- Exit code прогона = 0.
- Количество тестов: **50** (20 исходных + 30 добавленных).

---

## Файлы тестов

| Файл                                                                      | Пакет        | Тестов |
|---------------------------------------------------------------------------|-------------|--------|
| `security_static_test.go`                                                 | `raxd_test` | 5      |
| `internal/banner/banner_test.go`                                          | `banner_test` | 5    |
| `internal/cli/cli_test.go`                                                | `cli_test`  | 14     |
| `internal/cli/cli_gaps_test.go` *(новый)*                                 | `cli_test`  | 9      |
| `internal/cli/security_test.go` *(новый)*                                 | `cli_test`  | 3      |
| `internal/config/paths_test.go`                                           | `config_test` | 6   |
| `internal/config/security_test.go` *(новый)*                              | `config_test` | 3   |
| `internal/version/version_test.go`                                        | `version_test` | 3  |
| `internal/version/version_gaps_test.go` *(новый)*                        | `version_test` | 2  |
| **Итого**                                                                 |             | **50** |

*Примечание: `go test -v` выводит каждый `t.Run` sub-test отдельно; базовый счёт — 50 функций верхнего уровня.*

---

*Артефакт задачи: `bootstrap-cli`. Автор: qa. Проверяет: qa-guardian.*
*Автор продукта: Vladimir Kovalev, OEM TECH.*
