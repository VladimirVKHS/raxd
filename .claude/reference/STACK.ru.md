# raxd — технологический стек (источник истины)

> Этот файл — общий контракт по стеку для всех агентов команды. Architect, system-dev,
> developer, devops, mcp-engineer и cli-ux обязаны читать его перед работой и НЕ вводить
> другие зависимости без обоснования в `plan.md` (раздел Trade-offs). Версии — ориентир на
> 2025-2026, проверяйте актуальность через research-analyst при сомнениях.

## Продукт

**`raxd`** — Remote Access daemon. Один Go-бинарь, который одновременно:
- системный сервис (systemd на Linux, launchd на macOS);
- CLI-утилита (`raxd <команда>`);
- сетевой сервер (TCP + TLS);
- MCP-сервер для ИИ-агентов.

Платформы: **macOS + Linux**, архитектуры **amd64 + arm64**. Windows — вне scope.
Автор: **Vladimir Kovalev, OEM TECH**.

## Базовые библиотеки

| Назначение | Выбор | Статус / версия | URL |
|---|---|---|---|
| CLI + подкоманды | `spf13/cobra` | v1.10.x, активно | https://github.com/spf13/cobra |
| Кроссплатформенный сервис | `kardianos/service` (+ генерация unit/plist) | maintained | https://github.com/kardianos/service |
| Стилизация вывода | `charmbracelet/lipgloss` (v2) | v2.x, активно | https://github.com/charmbracelet/lipgloss |
| Логи (цветные, человекочитаемые) | `charmbracelet/log` | активно | https://github.com/charmbracelet/log |
| Таблицы (список ключей и т.п.) | `olekukonko/tablewriter` | maintained | https://github.com/olekukonko/tablewriter |
| Сборка/релизы (build-матрица) | `goreleaser` v2 | v2.x, активно | https://goreleaser.com |
| Пути конфигов (XDG, macOS) | `adrg/xdg` | maintained | https://github.com/adrg/xdg |
| Конфигурация | `spf13/viper` | maintained | https://github.com/spf13/viper |
| TLS / сертификаты | `crypto/tls`, `crypto/x509` (stdlib) | Go 1.22+ | https://pkg.go.dev/crypto/tls |
| Rate limiting | `golang.org/x/time/rate` | stdlib-ext | https://pkg.go.dev/golang.org/x/time/rate |
| MCP-сервер | `github.com/modelcontextprotocol/go-sdk/mcp` | официальный, v1.x | https://github.com/modelcontextprotocol/go-sdk |

## Раскладка на диске

- **Конфиг**: `$XDG_CONFIG_HOME/raxd/config.yaml` (Linux), `~/.config/raxd/config.yaml` или `~/Library/Application Support/raxd/` (macOS).
- **Состояние/ключи**: `$XDG_STATE_HOME/raxd/keys.db` (или эквивалент), права **`0600`**.
- **TLS**: серт `0644`, приватный ключ `0600`.
- **Логи**: системный журнал (journald/syslog) + ротация при файловом выводе.

## Кросс-компиляция (goreleaser)

Матрица: `GOOS={linux,darwin} × GOARCH={amd64,arm64}` → 4 бинаря
`raxd_{linux,darwin}_{amd64,arm64}` + архивы (`.tar.gz`) + `SHA256SUMS`.
`CGO_ENABLED=0` (статическая сборка, простая дистрибуция).

## Установка (`curl | sh`)

Скрипт: детект `uname -s`→{linux,darwin}, `uname -m`→{amd64,arm64}; скачивание нужного
архива; проверка SHA256; установка в `/usr/local/bin/raxd` (`0755`); генерация и регистрация
сервиса (systemd unit / launchd plist); на macOS — снятие `com.apple.quarantine`; печать
красивого статус-блока (см. ux-spec) с инфо о приложении, авторе и примерами команд.

## CLI-команды (контракт первой итерации)

- `raxd key create [--name <label>]` — выпуск API-ключа (показывается один раз).
- `raxd key delete <id>` — отзыв ключа.
- `raxd key list` — таблица ключей (id, label, created, last-used).
- `raxd config port <PORT>` — настройка порта прослушивания.
- (служебные) `raxd status`, `raxd version`, `raxd serve` (запуск сервиса).

Подробности безопасности — в `SECURITY-BASELINE.ru.md`; детали MCP — в `MCP-INTEGRATION.ru.md`.
