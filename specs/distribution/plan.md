# Plan: distribution — установка raxd `curl | sh` + воспроизводимый релиз артефактов (4 цели + SHA256SUMS)

Автор плана: architect (raxd). Вход: spec.md (AC1-16 — закон), research.md, ADR-001..005 (accepted),
реальный код (`Makefile build-all/verify-cross`, `Dockerfile` golang:1.25 offline-vendor,
`internal/version` ldflags `Version/Commit/Date`). Образец: `specs/service-install/plan.md`.
Автор продукта: Vladimir Kovalev, OEM TECH. Развилки Q1-Q8 закрыты в ADR-001..005 + ниже.

## Chosen Approach
**Ручной релизный путь поверх существующего Makefile (ADR-001 вариант B), без goreleaser.** Релизная
сборка — новые Make-таргеты `release`/`checksums` (вызывают готовый `build-all`, архивируют каждый из
4 бинарей в `tar.gz` + LICENSE/README, генерируют `SHA256SUMS` нативным `sha256sum`-форматом). Версия
через ldflags `internal/version.{Version,Commit,Date}` (AC10). install.sh — bash-скрипт
(`#!/usr/bin/env bash`, `set -euo pipefail`, тело в `main`, вызов в конце, `trap` cleanup) с
env-параметризацией `RAXD_BASE_URL` для теста без remote (ADR-002). Тест install-flow — мок-HTTP
(`python3 -m http.server`) в чистом debian-контейнере (ADR-002). CI — `.github/workflows/*.yml` как
артефакт + локальный docker-CI Make-таргет как фактический гейт v1 (ADR-004). Всё офлайн из `vendor/`,
ноль новых рантайм-зависимостей продукта (AC15).

## Modules
- `install.sh` — установочный скрипт `curl | sh`: каркас (AC2), детект OS/arch (AC4), скачивание
  архива+SHA256SUMS, проверка хэша ПЕРЕД установкой (AC3), детект пути+sudo (AC9), идемпотентность
  (AC5), macOS quarantine (AC11), сообщения/ошибки (AC6/AC7). Тело в `main`, вызов одной строкой в конце.
- `Makefile` (**расширить**) — таргеты `release` (build-all → 4× `tar.gz` в `dist/`), `checksums`
  (`SHA256SUMS` в `dist/`), `release-all` (release+checksums), `ci-local` (release-all+`test-unit`,
  docker-guard `/.dockerenv`), `test-install` (мок-сервер + install.sh в чистом контейнере). Версия:
  `VERSION ?= $(shell git describe --tags --always)` → ldflags `-X …version.Version=…` (AC10).
- `scripts/release.sh` — тело релизной сборки (архивация+checksums), вызывается из Make-таргета
  `release`/`checksums`; единственный источник имён артефактов (AC16).
- `scripts/test-install.sh` — тело теста install-flow: поднять мок-HTTP над `dist/`, прогнать
  install.sh с `RAXD_BASE_URL`, проверить `raxd version`, идемпотентность, хэш-fail (AC12).
- `Dockerfile.install` (**новый**) — чистый `debian:stable-slim` (без Go/raxd) + `python3`/`curl` для
  AC12-теста; не путать с build-`Dockerfile`.
- `.github/workflows/ci.yml`, `.github/workflows/release.yml` — артефакт CI на будущее (build-матрица
  + тесты из `vendor/`, без `go mod download`); не запускаются без remote (ADR-004, AC14).
- `LICENSE`, `README.md` — включаются внутрь каждого `tar.gz` (AC8, конвенция goreleaser-дефолта).

## Contracts
- **install.sh — контракт интерфейса** (флаги/env/коды; тела нет):
  - env: `RAXD_BASE_URL` (база URL артефактов; ОБЯЗАТЕЛЕН для AC12-теста; дефолт — будущий боевой
    URL-плейсхолдер), `RAXD_VERSION` (тег версии в имени артефакта; дефолт — `latest`/фикс v1),
    `RAXD_PREFIX` (override каталога установки; ADR-003).
  - флаги: `--prefix <dir>` (= `RAXD_PREFIX`), `--version <v>`, `-h|--help`.
  - детект: `uname -s`→{linux,darwin}; `uname -m`→{amd64,arm64} с нормализацией `x86_64`→amd64,
    `aarch64|arm64`→arm64. Неподдерживаемое (Windows/32-бит) → `error:` + код≠0, без установки (AC4).
  - артефакт: `${RAXD_BASE_URL}/raxd_${RAXD_VERSION}_${os}_${arch}.tar.gz` + `${RAXD_BASE_URL}/SHA256SUMS`,
    скачивание `curl -fsSL` во временный `mktemp -d` (AC7); согласовано с release.sh (AC16).
  - проверка хэша ПЕРЕД установкой: Linux `sha256sum -c`, macOS `shasum -a 256 -c` (детект утилиты;
    нет ни одной → `error:` + код≠0). Несовпадение → abort, бинарь НЕ ставится, temp удаляется (AC3).
  - путь: если `/usr/local/bin` writable или `id -u`=0 → туда; иначе если каталог не writable, но есть
    `sudo` и пользователь согласен явно → sudo-установка; иначе fallback `~/.local/bin` + `hint:` про
    PATH, если каталог не в `$PATH` (AC9). `RAXD_PREFIX` перекрывает детект.
  - идемпотентность: атомарная замена (`install`/`mv` поверх существующего) → ровно один `raxd` в PATH,
    повторный запуск не плодит дубликаты и не ломает прежний (AC5).
  - macOS: `xattr -d com.apple.quarantine "$dst" 2>/dev/null || true` (идемпотентно) + `hint:` про
    Gatekeeper (AC11, ADR-005).
  - коды возврата: `0` успех; `1` общая ошибка; `2` неподдерж. платформа (AC4); `3` несовпадение хэша
    (AC3); `4` нет прав на запись/нет sudo (AC9); `5` сбой скачивания. Сообщения: `error:`/`hint:`
    строчными (STACK), без секретов и сырых трасс (AC6).
- `scripts/release.sh` — параметры `VERSION` (env/арг); из `dist/raxd_<os>_<arch>` (готовы `build-all`)
  делает `dist/raxd_<version>_<os>_<arch>.tar.gz` (внутри бинарь как `raxd` + LICENSE + README); затем
  `(cd dist && sha256sum *.tar.gz > SHA256SUMS)` формата `<hash>␣␣<file>` (AC8/AC16). Выход: 4 архива +
  SHA256SUMS в `dist/`; код≠0 при отсутствии любого из 4 бинарей.
- `scripts/test-install.sh` — параметр порт (дефолт 8000); поднимает `python3 -m http.server <port>
  -d dist --bind 127.0.0.1`, ставит trap на kill сервера, прогоняет
  `RAXD_BASE_URL=http://127.0.0.1:<port> RAXD_VERSION=<v> bash install.sh`, проверяет `raxd version`
  ≠ `dev`, повторный запуск (AC5), порчу архива → код 3 (AC3). Выход код≠0 при любом провале (AC12).
- `Makefile release` зависит от `build-all`; `ci-local` и `test-install` — с docker-guard
  `test -f /.dockerenv` (как `verify-cross`), иначе `error:` + код≠0 (baseline §6).

## Trade-offs
- Выбрали **ручной путь (B)** вместо **goreleaser (A)** и **гибрида с `.goreleaser.yaml` (C)**:
  goreleaser офлайн в Docker не ставится (все методы установки требуют сети; `go install @latest`
  требует невышедшего Go 1.26, у нас 1.25 — research, ADR-001). Цена B: пишем архивацию/checksums
  руками, нет «бесплатных» changelog/brew. Цена отказа от C: при появлении remote `.goreleaser.yaml`
  придётся писать с нуля — приемлемо (его ценность уже покрыта `build-all`, риск рассинхрона двух путей
  устранён). STACK §Кросс-компиляция/§Установка называет goreleaser основным → помечаем его
  **опциональным/неиспользуемым** (как `kardianos/service` в service-install ADR-001).
- Выбрали **мок-HTTP + `RAXD_BASE_URL`** вместо `file://` (слабее как тест сети) и docker-compose
  (избыточная инфраструктура) — ADR-002. Цена: `python3` в тест-образе (есть в debian).
- Выбрали **авто-детект пути + override** вместо «только `/usr/local/bin`+sudo» / «только
  `~/.local/bin`» — ADR-003. Цена: больше ветвления/тестов в install.sh; покрывает все 3 теста AC9.
- Выбрали **YAML-артефакт + локальный docker-CI** вместо «только YAML» (непроверяем без remote) —
  ADR-004. Цена: два описания CI → смягчено общими Make-таргетами.
- Выбрали **идемпотентный `xattr -d` + инструкция** вместо нотаризации (нет Apple Developer ID) —
  ADR-005. Цена: возможен Gatekeeper-warning при первом запуске; реальный флоу проверяется на живом
  macOS вне Docker (AC13).
- **Bash, не POSIX sh**: spec AC2 требует `set -euo pipefail` (pipefail непереносим в dash) →
  `#!/usr/bin/env bash`. Цена: хосты без bash не поддержаны (редкость на Linux/macOS — research Q8).
- **Новых рантайм-зависимостей продукта нет** (`tar`/`sha256sum`/`shasum`/`xattr`/`curl`/`uname` —
  системные утилиты; `python3` — только тест-образ; AC15).

## Out of Scope (scope-guard)
Windows/32-бит; пакетные менеджеры (brew/apt/rpm — Q6 отложено, предусловие: remote-хостинг);
реальная подпись/нотаризация Apple Developer ID; реальный remote-релиз (GitHub Release/CDN — только
«артефакты+SHA256SUMS существуют и согласованы»); самообновление/uninstall/даунгрейд; регистрация
сервиса (`raxd service install` — задача service-install; install.sh лишь ОПЦИОНАЛЬНО предлагает hint).

## План тестирования (для qa)
- **AC8/AC10/AC15/AC16:** `make ci-local` в Docker (offline) → ровно 4 `tar.gz` + `SHA256SUMS`; каждый
  хэш совпадает (`sha256sum -c` зелёный); распакованный нативный `raxd version` ≠ `dev` в формате
  `raxd <v> (commit <c>, built <d>)`; имена артефактов = имена, которые ищет install.sh.
- **AC2/AC4/AC11/AC13:** статическая проверка install.sh (наличие `set -euo pipefail`, `main`+вызов в
  конце, `trap`); усечённый скрипт не ставит бинарь; darwin-ветка содержит `xattr -d`/инструкцию.
- **AC1/AC3/AC5/AC6/AC7/AC9/AC12:** `make test-install` в чистом `Dockerfile.install`-контейнере: мок-HTTP
  → install.sh → `raxd version` OK; порча архива → код 3, бинаря нет; повторный запуск → один `raxd`;
  установка без sudo в writable-каталог; нехватка прав → `error:`/`hint:`; `bash -x` показывает только
  перечисленные шаги.

## Хэндофф security (threat-model.md + security-requirements.md)
Покрыть: (1) риск-модель `curl | sh` (исполнение по сети) — защита от обрыва закачки (тело в `main`,
вызов в конце, AC2); (2) проверка целостности `SHA256SUMS` ДО исполнения/размещения бинаря (AC3),
abort при несовпадении; (3) минимизация — никакого кода/загрузок сверх скачанного бинаря (AC7);
(4) не-root установка по умолчанию, sudo только явно и обоснованно, демон не запускается от root (AC9);
(5) temp через `mktemp -d` + `trap` cleanup при любом выходе (AC2); (6) мок-сервер `python3 -m
http.server` — ТОЛЬКО для теста (не рантайм продукта), bind `127.0.0.1`; (7) `RAXD_BASE_URL`/`--prefix`
как доверенный вход — отметить риск подмены источника (целостность держится на `SHA256SUMS`, который
скачивается с того же base URL → в проде нужен доверенный канал к скрипту/SHA256SUMS, зафиксировать).
