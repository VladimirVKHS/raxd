# Docs Outline: distribution — установка raxd (`curl | sh`) + релизные артефакты

Автор продукта: **Vladimir Kovalev, OEM TECH**. Роль: tech-writer. Дата: 2026-05-22.
Ветка: `feature/distribution`. Язык docs/README — английский (по STACK); этот внутренний
артефакт — на русском.

## Созданные / изменённые файлы

| Файл | Действие | Назначение |
|------|----------|------------|
| `docs/installation.md` | **создан** | Основной артефакт задачи: полный гайд по установке (`curl \| sh`, ручная установка, проверка SHA256, trust model, macOS quarantine, сборка из исходников, exit-коды, uninstall). |
| `README.md` | **обновлён** | Раздел статуса/установки приведён к реальности: установщик есть и Docker-верифицирован, публичный хостинг pending (URL — плейсхолдер); новый раздел `## Installation`; таблица «What works today» и `## Coming next` обновлены; ссылка на `docs/installation.md`. |
| `docs/troubleshooting.md` | **обновлён** | Новая секция `## Installation (install.sh)`: коды 2/3/4/5 + отсутствие SHA-утилиты (код 1) + macOS Gatekeeper; в стиле файла. Также убран пред­существовавший «висячий» артефакт `</content>` в конце файла. |
| `specs/distribution/docs-outline.md` | **создан** | Этот план. |

Решено НЕ дублировать (только ссылки):
- регистрация сервиса → `docs/service-management.md` (install.sh лишь печатает hint);
- сборка/тесты в Docker, vendor, ldflags → `docs/development.md`;
- MCP, exec/upload, конфиг, ключи → существующие `docs/{mcp,commands,configuration}.md`.

## Назначение / аудитория / ключевые секции

### `docs/installation.md` (новый — основной артефакт)
- **Цель**: дать пользователю установить бинарь `raxd` на свежий хост честно и безопасно,
  с поправкой на то, что публичный хостинг ещё не настроен.
- **Аудитория**: администратор свежего Linux-сервера / пользователь macOS / релиз-инженер.
- **Ключевые секции**:
  - Status-баннер: установщик готов и верифицирован в Docker, публичный хост — pending (URL placeholder).
  - Supported platforms — таблица linux/darwin × amd64/arm64 + нормализация arch; Windows/32-бит вне scope.
  - Quick install (`curl … | sh`) — каноническая форма + честная пометка про placeholder; `RAXD_BASE_URL`/`RAXD_VERSION`/`RAXD_PREFIX`; флаги `--prefix`/`--version`/`--help`; «что делает установщик и только это».
  - Install path & privileges — авто-детект `/usr/local/bin` → `~/.local/bin`; sudo только явно; `--prefix`/`RAXD_PREFIX`; PATH-hint.
  - Integrity verification (SHA256) — проверка ДО размещения, hard-fail код 3; ручная `sha256sum -c` / `shasum -a 256 -c`.
  - **Trust model (v1)** — TLS + SHA256SUMS; GPG ОТСУТСТВУЕТ (П-1/ОР-1); граница (согласованная подмена не ловится); предупреждение про `RAXD_BASE_URL`/`--prefix` (П-3/ОР-3).
  - Manual installation — скачать архив + `SHA256SUMS`, проверить, распаковать, `install -m 0755`, PATH.
  - macOS Gatekeeper / quarantine — idempotent `xattr -d` + ручная инструкция; нет нотаризации (П-2/ОР-2); ограничение проверки вне Docker (AC13/П-4/ОР-4).
  - Building release artifacts from source — `make build-all release-all` / `ci-local` / `test-install` в Docker (офлайн vendor); 4 tar.gz + SHA256SUMS.
  - Exit codes — таблица 0/1/2/3/4/5.
  - Registering the service — ссылка на `service-management.md`.
  - Uninstall — удалить бинарь; сервис — `raxd service uninstall`; самообновления нет.
  - Note on licensing — LICENSE в репо нет; в архив кладётся только при наличии.
  - Author — Vladimir Kovalev, OEM TECH.

### `README.md` (обновлён)
- **Цель**: точка входа; статус и установка должны соответствовать реальности.
- **Аудитория**: любой, кто впервые открывает проект.
- **Изменения**: статус-баннер (установщик реализован, хост pending); новый `## Installation`
  (каноническая команда + честная пометка про placeholder + ссылка); строки в «What works today»
  про установщик/артефакты/хост/подпись/пакетники; `## Coming next` (публичный хост + подпись +
  нотаризация + пакетники + uninstall); ссылка на `docs/installation.md` в `## Documentation`.

### `docs/troubleshooting.md` (обновлён)
- **Цель**: типовые проблемы и решения; добавлена установка.
- **Аудитория**: оператор, столкнувшийся с ошибкой установки.
- **Новая секция**: коды 2 (неподдерж. платформа), 3 (SHA-несовпадение + нет записи в SHA256SUMS),
  4 (нет прав/нет sudo + PATH-hint), 5 (сбой скачивания, в т.ч. placeholder-URL), 1 (нет sha256sum/shasum),
  macOS «raxd is damaged»/Gatekeeper → quarantine-команда.

## Примеры команд (проверяемые, из реального install.sh / Makefile / STACK)
- `curl -fsSL https://<base-url>/install.sh | bash` — каноническая установка (после настройки хоста).
- `RAXD_BASE_URL=https://artifacts.example.org/raxd RAXD_VERSION=v0.1.0 curl -fsSL …/install.sh | bash` — установка с указанного источника.
- `curl -fsSL https://<base-url>/install.sh | bash -s -- --prefix ~/.local/bin --version v0.1.0` — флаги через пайп.
- `sha256sum -c SHA256SUMS` / `shasum -a 256 -c SHA256SUMS` — ручная проверка целостности.
- `install -m 0755 raxd ~/.local/bin/raxd` — ручная установка в user-каталог.
- `xattr -d com.apple.quarantine /usr/local/bin/raxd` — снятие карантина macOS вручную.
- `docker run --rm -v "$(pwd)/dist:/src/dist" -e VERSION=v0.1.0 -w /src raxd-build sh -c "make build-all release-all VERSION=v0.1.0"` — сборка артефактов в Docker.
- `make ci-local VERSION=v0.1.0` / `make test-install VERSION=v0.1.0` — локальный гейт / install-flow тест в Docker.
- `sudo raxd service install` — регистрация сервиса (ссылка, не дублируем).

## Об авторе (OEM TECH)
**Vladimir Kovalev, OEM TECH** — присутствует в: README (статус-баннер, `## Author`, `## License`),
`docs/installation.md` (`## Author`). Имя автора также во всех CLI-баннерах продукта (см. README §The banner).

## Покрытие AC / SR документацией

| AC/SR | Где задокументировано |
|-------|------------------------|
| AC4 (детект OS×arch) | installation.md §Supported platforms (таблица + нормализация) |
| AC11 (macOS quarantine — снятие + инструкция) | installation.md §macOS Gatekeeper/quarantine; troubleshooting §macOS |
| AC13 (ограничение проверки macOS вне Docker) | installation.md §macOS (явный warning-блок), привязка к П-4/ОР-4 |
| Exit codes (AC6) | installation.md §Exit codes; troubleshooting §Installation |
| SR-105 (trust model v1: TLS+SHA256, нет GPG) | installation.md §Trust model (v1) — закрывает SR-105 способом «инспекция документации»; ссылки на П-1/ОР-1 |
| SR-109 (snap quarantine + инструкция, ограничение macOS) | installation.md §macOS Gatekeeper/quarantine; troubleshooting §macOS |
| П-3/ОР-3 (RAXD_BASE_URL/--prefix как доверенный вход) | installation.md §Pointing the installer + предупреждение в §Trust model |
| П-1/ОР-1 (нет GPG) | installation.md §Trust model; README §Coming next |
| П-2/ОР-2 (нет нотаризации) | installation.md §macOS; README §Coming next |
| AC8/AC10/AC15 (сборка артефактов, ldflags, офлайн vendor) | installation.md §Building release artifacts from source |

## Открытые вопросы
- None по содержанию: вся документация подтверждена кодом (install.sh / release.sh / Makefile /
  Dockerfile.install) и спеками. Расхождений «спека ≠ код» при сверке не выявлено.
- Заметки для reviewer/tech-writer-guardian (НЕ выдумки, честно отражено в доке):
  - Боевой `RAXD_BASE_URL` — плейсхолдер `https://releases.example.com/raxd` (ОР-3); реальный URL
    подставляется при появлении remote → дополнить installation.md §Quick install.
  - GPG-подпись `SHA256SUMS` (ОР-1) и macOS-нотаризация (ОР-2) — отсутствуют в v1, отмечены как
    предстоящие до публичного релиза → дополнить installation.md §Trust model / §macOS.
  - LICENSE в репо отсутствует — отражено честно (§Note on licensing); добавить файл до публ. релиза.
