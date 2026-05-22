# Test Plan: distribution — curl|sh install-flow + воспроизводимый релиз raxd

Задача: `distribution`. Ветка: `feature/distribution`.
Автор: qa (raxd). Дата: 2026-05-22.
Источники: spec.md (AC1-16), plan.md, security-requirements.md (SR-97..SR-113),
threat-model.md, SECURITY-BASELINE.ru.md §5/§6.

---

## Стратегия

| Уровень | Что проверяем | Инструмент |
|---------|---------------|------------|
| **Статический** | Наличие обязательных конструкций в install.sh (set -euo pipefail, main, trap, xattr, отсутствие eval); согласованность имён артефактов install.sh↔release.sh↔SHA256SUMS | grep в test-install-static.sh + секция в test-install-edge.sh |
| **Install-flow (Docker)** | Полный curl\|sh в чистом debian:stable-slim: позитив, идемпотентность, SHA256-fail, edge cases платформы, привилегии | scripts/test-install.sh (TEST 1-3) + scripts/test-install-edge.sh (TEST 4-8) в Dockerfile.install |
| **Сборка/CI (Docker)** | 4 архива + SHA256SUMS; офлайн из vendor; ldflags; целостность sha256sum -c | make ci-local → make release-all в raxd-build-контейнере |
| **Ограничение macOS** | darwin-ветка: статически grep + фиксация ОР-4 | test-install-edge.sh TEST 6 (grep) |

Все прогоны — ТОЛЬКО в Docker (SECURITY-BASELINE §6). Команды — docker-команды.
Go-тесты (`go test`) — не применимы к bash-скриптам: логика в shell, тесты — shell.
Новых Go-зависимостей не вводится (AC15).

---

## Матрица AC → тест

| AC | Описание | Уровень | Тест (файл::кейс) | Статус |
|----|----------|---------|-------------------|--------|
| AC1 | install.sh ставит только бинарь; нет логики регистрации сервиса | install-flow + статический | test-install.sh::TEST1 (бинарь установлен); test-install-edge.sh::TEST8 (grep: нет unit/plist генерации) | зелено (docker-verification.md) |
| AC2 | set -euo pipefail; тело в main(); вызов в конце; trap cleanup | статический + install-flow | test-install-edge.sh::TEST4 (статика); test-install-edge.sh::TEST5 (усечённый скрипт) | NEW — тест-edge |
| AC3 | SHA256 проверяется ДО установки; несовпадение → код 3; бинарь не ставится | install-flow | test-install.sh::TEST3 (подмена → код 3, нет бинаря) | зелено (docker-verification.md) |
| AC4 | Детект OS/arch; неподдерживаемая платформа → код 2; нормализация x86_64/aarch64 | install-flow | test-install-edge.sh::TEST4 (uname-shim i686 → код 2, нет бинаря) | NEW — тест-edge |
| AC5 | Идемпотентность: повторный запуск → ровно 1 бинарь | install-flow | test-install.sh::TEST2 (ровно 1 бинарь, бинарь работает) | зелено (docker-verification.md) |
| AC6 | Понятные сообщения; error:/hint:; ненулевой код при ошибке; нет секретов | install-flow | test-install.sh::TEST3 (error: в выводе); test-install-edge.sh::TEST4 (код 2 + error:); test-install-edge.sh::TEST7 (код 4 + error:/hint:) | частично NEW |
| AC7 | Только перечисленные шаги; нет eval; нет посторонних загрузок/запуска демона | статический | test-install-edge.sh::TEST8 (grep: нет eval/запуска демона/посторонних curl) | NEW — тест-edge |
| AC8 | 4 архива tar.gz + SHA256SUMS; хэши совпадают | сборка/CI | make ci-local → sha256sum -c зелено; dist/ содержит ровно 4 *.tar.gz | зелено (docker-verification.md) |
| AC9 | Путь в PATH; sudo только явно; нет прав → код 4 + error:/hint: | install-flow | test-install.sh::TEST1 (установка без sudo в RAXD_PREFIX); test-install-edge.sh::TEST7 (каталог без права записи → код 4) | TEST7 — NEW |
| AC10 | raxd version: формат "raxd <v> (commit <c>, built <d>)", не dev | install-flow | test-install.sh::TEST1 (regexp-проверка + не dev) | зелено (docker-verification.md) |
| AC11 | darwin-ветка: xattr -d + инструкция; или документация | статический | test-install-edge.sh::TEST6 (grep darwin-ветки: xattr -d com.apple.quarantine + Gatekeeper-hint) | NEW — тест-edge |
| AC12 | install-flow в чистом debian/ubuntu — raxd version работает | install-flow | test-install.sh (весь прогон в Dockerfile.install / debian:stable-slim) | зелено (docker-verification.md) |
| AC13 | Ограничение проверки macOS зафиксировано | документация | Зафиксировано в настоящем test-plan (раздел «Зафиксированные ограничения»); тест grep AC11 подтверждает статику | зафиксировано |
| AC14 | CI собирает и тестирует в контейнере | артефакт + локальный гейт | make ci-local (фактический гейт v1); .github/workflows/*.yml (артефакт, ADR-004) | зелено (docker-verification.md) |
| AC15 | Офлайн из vendor; CGO_ENABLED=0; нет новых рантайм-зависимостей | сборка/CI | make ci-local без доступа к proxy.golang.org; grep go.mod — нет новых зависимостей | зелено (docker-verification.md) |
| AC16 | Имена артефактов: install.sh = release.sh = SHA256SUMS | статический + сборка | test-install-edge.sh::TEST9 (grep согласованности имён) + make ci-local sha256sum -c | TEST9 — NEW |

Итого AC с зелёным статусом из docker-verification.md: AC1,AC3,AC5,AC8,AC10,AC12,AC14,AC15.
Новые тесты (test-install-edge.sh): закрывают AC2,AC4,AC6(частично),AC7,AC9,AC11,AC13,AC16.

---

## SR → тест (security-критичные)

| SR | Описание | Тест |
|----|----------|------|
| SR-97 | set -euo pipefail; тело в main; вызов в конце | test-install-edge.sh::TEST4 (статика), TEST5 (усечённый) |
| SR-98 | mktemp -d; trap cleanup EXIT/INT/TERM | test-install-edge.sh::TEST4 (grep) |
| SR-99 | RAXD_BASE_URL дефолт = https://; curl -fsSL | test-install-edge.sh::TEST4 (grep) |
| SR-100 | SHA256 ДО размещения; несовпадение → abort; бинарь не ставится | test-install.sh::TEST3 |
| SR-101 | Имена артефактов согласованы install.sh=release.sh=SHA256SUMS | test-install-edge.sh::TEST9 |
| SR-102 | Формат SHA256SUMS: hash + 2 пробела + filename | make ci-local → sha256sum -c |
| SR-103 | Только перечисленные шаги; нет eval; нет запуска демона | test-install-edge.sh::TEST8 |
| SR-104 | Неподдерживаемая платформа/arch → код 2; без установки | test-install-edge.sh::TEST4 |
| SR-105 | GPG-подпись отсутствует (П-1); нет ложного gpg --verify | test-install-edge.sh::TEST8 (grep: нет gpg --verify) |
| SR-106 | Не-root по умолчанию; sudo только явно | test-install.sh::TEST1 + test-install-edge.sh::TEST7 |
| SR-107 | Атомарная замена; ровно 1 бинарь; нет chmod 777 | test-install.sh::TEST2; test-install-edge.sh::TEST4 (grep: нет chmod 777) |
| SR-108 | PATH-hint если каталог не в $PATH; код 4 при нет прав | test-install-edge.sh::TEST7 |
| SR-109 | darwin-ветка: xattr -d ... 2>/dev/null || true + инструкция | test-install-edge.sh::TEST6 |
| SR-110 | ldflags только Version/Commit/Date; raxd version не dev | test-install.sh::TEST1 |
| SR-111 | error:/hint: без секретов; ненулевой код | test-install.sh::TEST3; test-install-edge.sh::TEST4,TEST7 |
| SR-112 | Docker-guard; офлайн vendor; нет новых зависимостей | make ci-local (docker-guard в Makefile) |
| SR-113 | Мок-сервер только 127.0.0.1; python3 только в тест-образе | test-install-edge.sh::TEST4 (grep: --bind 127.0.0.1) |

---

## Edge cases безопасности

### Уже покрыто test-install.sh (TEST 1-3)

- **Позитивный кейс** (TEST1): полный install-flow в debian:stable-slim; raxd version корректен.
- **Идемпотентность** (TEST2): повторный запуск → ровно 1 бинарь, предыдущая установка жива.
- **SHA256-подмена** (TEST3): мусор вместо архива → install.sh возвращает код 3, бинарь не ставится. Реальная защита целостности подтверждена.

### Новые edge cases (test-install-edge.sh, TEST 4-9)

- **TEST4 — неподдерживаемая архитектура i686** (AC4/SR-104): uname-shim в PATH подделывает `uname -m` → `i686`; ожидаем код 2 и отсутствие бинаря. Регрессия: если убрать case `*)→exit 2` из install.sh — тест упадёт.

- **TEST5 — усечённый скрипт** (AC2/SR-97): копия install.sh, обрезанная до строки `main "$@"`, запускается → бинарь не появляется (функция определена, но вызов отсутствует). Регрессия: если вынести логику из main() на верхний уровень — усечение приведёт к частичной установке, тест поймает это.

- **TEST6 — darwin-ветка статически** (AC11/SR-109/AC13): grep проверяет наличие `xattr -d com.apple.quarantine` и Gatekeeper-инструкции в install.sh. Регрессия: удаление darwin-ветки → grep провалится.

- **TEST7 — нет прав на запись** (AC9/SR-106/SR-108): каталог с chmod 000, запуск от пользователя без root → ожидаем код 4 и наличие `error:`/`hint:` в выводе. Регрессия: если убрать проверку writable → install.sh упадёт с общей ошибкой или тихо, тест поймает.

- **TEST8 — минимизация кода** (AC7/SR-103/SR-105): grep на отсутствие `eval`, `gpg --verify`, запуска `raxd serve`/`systemctl start`, посторонних `curl … | bash`. Регрессия: добавление eval или запуска демона → grep-ассерт провалится.

- **TEST9 — согласованность имён артефактов** (AC16/SR-101): grep в install.sh извлекает шаблон имени артефакта; grep в release.sh извлекает шаблон архива; сравниваем → должны совпасть. Регрессия: рассинхрон имён → grep-сравнение провалится.

---

## Зафиксированные ограничения среды (не снятие требований)

| Ограничение | Обоснование | Компенсация |
|-------------|-------------|-------------|
| **ОР-4 / AC13**: macOS Gatekeeper-флоу не проверяется в Docker | Docker — Linux; интеграционный тест macOS в контейнере невозможен | Статическая проверка darwin-ветки (TEST6 grep); реальный Gatekeeper-флоу проверяется вручную на живом macOS перед macOS-релизом |
| **П-1**: GPG-подпись SHA256SUMS отсутствует в v1 | Нет ключа подписи; ложная защита хуже отсутствия | Тест SR-105 проверяет отсутствие ложного `gpg --verify`; целостность на TLS + SHA256SUMS (SR-99/SR-100) |
| **П-2**: Apple-нотаризация отсутствует | Нет Apple Developer ID | TEST6 проверяет минимум: quarantine-снятие + инструкция |
| **П-3**: RAXD_BASE_URL override | Тест-механизм (http://127.0.0.1) отличается от боевого (https://) | TEST4 grep: боевой дефолт начинается с https://; http:// только в test-install-edge.sh |
| **ОР-5**: CI на remote runner не выполняется | Нет remote в v1 | make ci-local = фактический гейт; .github/workflows/*.yml = артефакт |

---

## Как запускать

Все команды выполняются на хосте (docker-команды); go build / go test / raxd на хосте запрещены (SECURITY-BASELINE §6).

### Полный CI-прогон (гейт перед merge)

```bash
# Из корня репозитория. Собирает, тестирует, прогоняет install-flow — всё в Docker.
make ci-local VERSION=v0.1.0-test
```

Включает: unit-тесты, кросс-сборку 4 артефактов, SHA256SUMS, TEST1-3 (test-install.sh).

### Только edge-тесты (новые TEST4-9)

```bash
# Предусловие: dist/ заполнен (make ci-local или docker run raxd-build make build-all release-all)
make test-install-edge VERSION=v0.1.0-test
```

### Только install-flow (TEST1-3, уже зелёно)

```bash
make test-install VERSION=v0.1.0-test
```

### Все install-тесты вместе (TEST1-9)

```bash
# test-install-all запускает test-install и test-install-edge последовательно
make test-install-all VERSION=v0.1.0-test
```

### Только статические проверки (без Docker, быстрый smoke)

```bash
bash -n install.sh                          # синтаксис
grep "set -euo pipefail" install.sh         # каркас
grep "^main \"" install.sh                  # вызов в конце
grep "trap cleanup" install.sh              # cleanup
grep "eval" install.sh || echo "eval: OK"   # минимизация
```

---

## Найденные баги (для эскалации к devops)

Баги продуктового кода в install.sh по результатам анализа не обнаружены.
Наблюдения devops-guardian (не блокирующие):
- **Н-1** (Н-3 из guardian): golang:1.25 без @sha256: — к продакшн-релизу.
- **Н-3** (Н-3 из guardian): shasum --quiet на старых macOS — функционально безопасно.

Эти наблюдения не блокируют qa; зафиксированы для возврата к devops перед публичным релизом.
