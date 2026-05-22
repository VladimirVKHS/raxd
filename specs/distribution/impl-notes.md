# impl-notes: distribution — install.sh / release / test-install / CI

Автор: devops (raxd). Ветка: `feature/distribution`.
Дата: 2026-05-22. Версия: v0.1.0-test (тестовая).

## Что реализовано

### Артефакты

| Файл | Описание |
|------|----------|
| `install.sh` | Установочный bash-скрипт для `curl \| sh` |
| `scripts/release.sh` | Релизная сборка: архивация 4 бинарей → tar.gz + SHA256SUMS |
| `scripts/test-install.sh` | Тест install-flow в чистом Docker-контейнере (мок-HTTP) |
| `Dockerfile.install` | Образ debian:stable-slim для AC12-теста (curl+python3, без Go/raxd) |
| `Makefile` (расширен) | Таргеты: release, checksums, release-all, test-install, ci-local |
| `.github/workflows/ci.yml` | CI-артефакт на будущее (AC14, ADR-004) |
| `.github/workflows/release.yml` | Release-пайплайн на будущее (AC14, ADR-004) |

### Ключевые решения

- **Ручной релизный путь (ADR-001)**: goreleaser отклонён (офлайн-получение невозможно в Docker 
  с Go 1.25). `scripts/release.sh` поверх `build-all`.
- **Мок-HTTP (ADR-002)**: `python3 -m http.server --bind 127.0.0.1` в тест-образе. install.sh 
  параметризован `RAXD_BASE_URL`.
- **Авто-детект пути (ADR-003)**: `/usr/local/bin` если writable, иначе `~/.local/bin` + hint.
- **CI-артефакт + ci-local (ADR-004)**: YAML-файлы на будущее; фактический гейт v1 — `make ci-local`.
- **Quarantine (ADR-005)**: `xattr -d com.apple.quarantine || true` + инструкция в darwin-ветке.
- **ldflags path**: переменные `buildVersion/buildCommit/buildDate` в `package main` (cmd/raxd/main.go). 
  В ldflags используется `main.buildVersion` (не полный import path — package main не поддерживает 
  полный путь через `-X`).

## Соответствие AC/SR

| AC/SR | Статус | Примечание |
|-------|--------|------------|
| AC1 | PASS | install.sh ставит только бинарь, hint на `raxd service install` |
| AC2 | PASS | `set -euo pipefail`, тело в `main()`, вызов одной строкой в конце, `trap cleanup` |
| AC3 | PASS | SHA256 проверяется ДО установки; негативный кейс: код 3, бинарь не ставится |
| AC4 | PASS | Детект linux/darwin × amd64/arm64; неподдерживаемые → код 2 |
| AC5 | PASS | Повторный запуск: ровно 1 бинарь, предыдущая установка не ломается |
| AC6 | PASS | `error:`/`hint:` строчными, без секретов |
| AC7 | PASS | Только: детект, скачивание, SHA256, размещение, quarantine; нет eval, нет демона |
| AC8 | PASS | 4 архива tar.gz + SHA256SUMS в dist/ |
| AC9 | PASS | `/usr/local/bin` если writable; `~/.local/bin` + PATH-hint иначе; sudo только явно |
| AC10 | PASS | `raxd v0.1.0-test (commit c6b34b6, built 2026-05-22)` — не `dev` |
| AC11 | PASS | darwin-ветка: `xattr -d com.apple.quarantine || true` + инструкция |
| AC12 | PASS | test-install в debian:stable-slim: 3 теста пройдены (позитив + идемпотентность + SHA-fail) |
| AC13 | ЗАФИКСИРОВАНО | macOS Gatekeeper проверяется на реальном macOS вне Docker (П-4) |
| AC14 | АРТЕФАКТ | CI YAML-файлы готовы; ci-local Makefile-таргет = фактический гейт v1 |
| AC15 | PASS | Офлайн из vendor/ (-mod=vendor, CGO_ENABLED=0), нет новых рантайм-зависимостей |
| AC16 | PASS | Имена артефактов install.sh = release.sh = SHA256SUMS (единственный источник) |
| SR-97 | PASS | `set -euo pipefail`; тело в `main()`; вызов последней строкой |
| SR-98 | PASS | `mktemp -d` + `trap cleanup EXIT INT TERM`; tmpdir объявлен до trap |
| SR-99 | PASS | Дефолтный `RAXD_BASE_URL` = `https://releases.example.com/raxd`; curl `-fsSL` |
| SR-100 | PASS | SHA256 ДО установки; несовпадение → код 3; бинарь не ставится |
| SR-101 | PASS | Имена: `raxd_<v>_<os>_<arch>.tar.gz` — install.sh = release.sh = SHA256SUMS |
| SR-102 | PASS | GNU sha256sum формат (`<hash>  <file>`); `sha256sum -c` зелёный для всех 4 |
| SR-103 | PASS | Только перечисленные действия; нет eval; нет запуска демона |
| SR-104 | PASS | Строгий детект; неподдерживаемая arch/OS → код 2 |
| SR-105 | ЗАФИКСИРОВАНО | GPG-подпись отсутствует (П-1); зафиксировано в impl-notes и security-requirements |
| SR-106 | PASS | sudo только при системном каталоге + явное сообщение; без запуска демона |
| SR-107 | PASS | Атомарная замена через `install -m 0755`; нет chmod 777 |
| SR-108 | PASS | PATH-hint если каталог не в $PATH; ошибка/hint если нет прав |
| SR-109 | PASS | `xattr -d com.apple.quarantine || true` + инструкция в darwin-ветке |
| SR-110 | PASS | ldflags только Version/Commit/Date; нет секретов в выводе |
| SR-111 | PASS | error:/hint: строчными; нет сырых трасс |
| SR-112 | PASS | test-install и ci-local — только в Docker; docker-guard в build-*/release + prereq-разрыв test-install←release-all устраняют host-build (фикс D-1) |
| SR-113 | PASS | Мок-сервер `--bind 127.0.0.1`; python3 только в Dockerfile.install |

## Чеклист безопасности

- [x] SHA256 проверяется ДО размещения бинаря (SR-100, AC3)
- [x] Тело install.sh в функции main(), вызов в конце файла (SR-97, AC2)
- [x] `trap cleanup EXIT INT TERM` на mktemp -d (SR-98, AC2)
- [x] Временный каталог: mktemp -d (непредсказуемое имя), нет фиксированных путей
- [x] tmpdir объявлен до trap (защита от set -u при раннем exit)
- [x] Не запускается демон raxd (SR-103, SR-106, AC1)
- [x] sudo только явно + сообщение (SR-106, AC9)
- [x] Нет секретов в скриптах/CI (SR-110, baseline §4)
- [x] Мок-сервер ТОЛЬКО 127.0.0.1 (SR-113)
- [x] python3 только в Dockerfile.install (SR-113, AC15)
- [x] RAXD_BASE_URL дефолт = https:// (SR-99)
- [x] darwin-ветка: xattr -d quarantine + инструкция (SR-109, AC11)

## Реальный вывод test-install

### TEST 1: позитивный кейс

```
==> определена платформа: linux/arm64
==> скачивание raxd_v0.1.0-test_linux_arm64.tar.gz...
==> проверка целостности SHA256...
==> SHA256 совпадает — артефакт целостен
==> бинарь установлен: /tmp/raxd-test-install/raxd
raxd v0.1.0-test (commit c6b34b6, built 2026-05-22)
PASS: формат version корректен
PASS: version не 'dev': raxd v0.1.0-test (commit c6b34b6, built 2026-05-22)
```

### TEST 2: идемпотентность

```
(повторный запуск install.sh с теми же параметрами)
PASS: ровно 1 бинарь raxd после повторной установки
PASS: повторная установка не сломала бинарь
```

### TEST 3: НЕГАТИВНЫЙ — подмена архива

```
==> архив подменён мусором — install.sh ДОЛЖЕН вернуть код 3...
127.0.0.1 - "GET /raxd_v0.1.0-test_linux_arm64.tar.gz HTTP/1.1" 200 -  ← мусор отдан
raxd_v0.1.0-test_linux_arm64.tar.gz: FAILED                             ← sha256sum
error: несовпадение SHA256 — архив повреждён или подменён
==> код выхода install.sh при подмене: 3                                 ← HARD FAIL
PASS: install.sh вернул код 3 (несовпадение SHA256) — защита работает
PASS: бинарь НЕ установлен при несовпадении SHA256
```

**SHA256-проверка реально отвергает подмену: install.sh возвращает код 3, бинарь не устанавливается.**

## Фикс D-1: устранение host-build leak (§6/SR-112)

**Проблема (до фикса):** `test-install: release-all` → `release: build-all` образовывал цепочку
Make-prereq, из-за которой хостовый `make test-install` (или `make ci-local` в части шага
test-install) выполнял 4× `go build` на ХОСТЕ — нарушение SECURITY-BASELINE §6.

**Исправления в `Makefile`:**

1. **docker-guard (`define DOCKER_GUARD`)**: макрос `test -f /.dockerenv || { ... exit 1; }`
   добавлен в каждый `build-linux-amd64/arm64`, `build-darwin-amd64/arm64` и `release`.
   На хосте — fail-fast с понятной ошибкой и hint. Внутри Docker (`docker run raxd-build`) —
   `/.dockerenv` есть, guard проходит. Dockerfile-стадии (`RUN go build` напрямую) не
   затронуты — они не вызывают Make-таргеты.

2. **`test-install` без prereq `release-all`**: prereq удалён. Вместо этого — явная проверка
   `test -f dist/SHA256SUMS` с понятной ошибкой и hint. При standalone-запуске без готового
   `dist/` — немедленный fail с инструкцией. При запуске из `ci-local` — `dist/` уже заполнен
   предыдущим шагом `docker run raxd-build make build-all release-all`.

3. **`release` без prereq `build-all`**: prereq удалён, добавлен `DOCKER_GUARD`. `release`
   ожидает, что бинари уже в `dist/` (из предыдущего `make build-all` внутри того же
   Docker-контейнера). Типичный вызов: `make build-all release-all` одной командой в Docker.

**Итог**: ни одна команда Make не выполняет `go build` на хосте. Доказательство: `make -n ci-local` 
и `make -n test-install` не содержат строк `go build`. Прямой вызов `make build-all` на хосте 
прерывается на первом guard с кодом 1 — до выполнения `go build`.

## Статус ci-local (после фикса D-1)

`make ci-local` выполняет последовательно:
1. `docker build --target test && docker run raxd-test` — unit-тесты в Docker: **PASS** (все пакеты)
2. `docker build --target build → raxd-build` — образ для кросс-компиляции
3. `docker run raxd-build make build-all release-all` — 4 бинари + 4 архива + SHA256SUMS **в Docker**: PASS
4. `make test-install` — потребляет готовый `dist/`, 3 теста в debian:stable-slim: **PASS**

Ни шаг 1, ни шаг 3, ни шаг 4 не выполняют `go build` на хосте.

## Остаточные пункты (для прод-релиза)

1. **LICENSE**: файла LICENSE нет в репо → в архивы не включается (предупреждение в release.sh).
   Добавить LICENSE перед публичным релизом (MIT / Apache-2.0 — решение владельца).
2. **Боевой RAXD_BASE_URL**: заменить плейсхолдер `https://releases.example.com/raxd` на реальный
   URL (GitHub Releases или CDN) после создания remote (ОР-3, ОР-5).
3. **GPG-подпись SHA256SUMS**: отсутствует в v1 (П-1, ОР-1). Добавить перед публичным релизом:
   отдельный ключ подписи + публичный ключ out-of-band + `gpg --verify` в install.sh.
4. **Apple-нотаризация**: без Apple Developer ID (П-2, ОР-2). Добавить при наличии сертификата:
   `codesign + notarytool + staple`.
5. **macOS install-flow на реальном macOS**: Docker проверяет только Linux; Gatekeeper-флоу
   проверяется вручную на живом macOS (AC13, ОР-4).
6. **CI на remote runner**: .github/workflows/*.yml не запускаются без remote. При создании
   public remote — активировать CI, зафиксировать боевой RAXD_BASE_URL (ОР-5).
7. **Кэш golang:1.25 в Docker**: Dockerfile использует golang:1.25 без pinning SHA — при обновлении
   образа воспроизводимость может нарушиться. Добавить `@sha256:...` для продакшн-пайплайна.
