# Docker-верификация дистрибуции (дирижёр)

> Прогон выполнен ДИРИЖЁРОМ (мандат «проверяй сам в Docker», не доверяя отчёту билдера на слово).
> Среда: macOS-хост (Docker 29.1.3), образы `raxd-test`/`raxd-build`/`raxd-install-test`.
> Дата: 2026-05-22. Ветка `feature/distribution`.

## 1. Найден баг D-1 (§6/SR-112) на первом прогоне devops

Первый `make ci-local VERSION=v0.1.0-test` прошёл (exit 0), НО при инспекции вывода обнаружено
нарушение SECURITY-BASELINE §6 / SR-112 / red-line #4:

- Шаг `test-install` (вызываемый `ci-local` последней строкой через хостовый
  `/Library/Developer/CommandLineTools/usr/bin/make`) ПЕРЕСОБИРАЛ 4 бинаря **на ХОСТЕ**:
  `CGO_ENABLED=0 GOOS=... go build ...` исполнялся вне Docker.
- Причина: цепочка Make-prereq `test-install: release-all → release → build-all`.
- Хостовый Go — **1.26.2** (`/Users/vks/go/.../toolchain@v0.0.1-go1.26.2`), а пин — `golang:1.25`.
  То есть артефакты, реально тестируемые install-flow, были собраны на хосте другим тулчейном,
  перезаписав Docker-сборку (golang:1.25) через volume-mount `dist/`.
- Сама логика install.sh была корректна (3 теста зелёные, включая реальный негативный SHA → код 3),
  дефект — только в провенансе сборки (Makefile-оркестрация).

**Вердикт дирижёра: needs-changes → возврат devops.**

## 2. Фикс devops (commit `1fde912`)

- `DOCKER_GUARD` (`test -f /.dockerenv`) добавлен в `build-linux/darwin-*` и `release` — на хосте
  fail-fast с §6-ошибкой, внутри Docker проходит прозрачно.
- Удалён prereq `release-all` из `test-install` (корень бага) → заменён проверкой `dist/SHA256SUMS`.
- Удалён prereq `build-all` из `release`.
- `ci-local`: единственная сборка артефактов — `docker run raxd-build … make build-all release-all`;
  шаг `test-install` лишь потребляет готовый `dist/`.

Self-check guard на хосте:
```
$ make build-linux-amd64 VERSION=v0.1.0-test
ERROR: 'make build-linux-amd64' нельзя запускать на хосте (SECURITY-BASELINE §6).
  ... (exit 1)
```

## 3. Чистый повторный прогон `make ci-local VERSION=v0.1.0-test` (после фикса)

`rm -rf dist && make ci-local VERSION=v0.1.0-test` → **exit 0**. Проверено по логу:

- **Никакого `go build` на хосте.** Единственные 4 `go build` — внутри
  `docker run raxd-build sh -c "make build-all release-all VERSION=v0.1.0-test"` (golang:1.25).
  После хостового вызова `make test-install` — НИ ОДНОГО `go build` (грепом подтверждено).
- **Unit-тесты в Docker:** `docker run raxd-test` — все пакеты PASS (vet+test, ~400 тестов, 0 FAIL).
- **Кросс-сборка 4 цели:** `dist/raxd_{linux,darwin}_{amd64,arm64}` — linux = ELF статический
  (CGO_ENABLED=0), darwin = Mach-O. Корректно.
- **release-all:** 4× `raxd_v0.1.0-test_<os>_<arch>.tar.gz` + `SHA256SUMS`.
- **SHA256SUMS-консистентность:** пересчёт `shasum -a 256` на хосте == опубликованный SHA256SUMS
  (diff пуст).
- **test-install (чистый debian:stable-slim, мок-HTTP 127.0.0.1):**
  - TEST 1 (позитив): `raxd v0.1.0-test (commit 1fde912, built 2026-05-22)` — формат верен, не `dev`.
  - TEST 2 (идемпотентность): ровно 1 бинарь после повторного запуска, прежняя установка цела.
  - TEST 3 (негатив, реальный): подмена архива мусором → `sha256sum: FAILED` →
    `install.sh` вернул **код 3**, бинарь НЕ установлен. Защита целостности реальна.

## Итог раздела devops-верификации
`make ci-local` — §6-чист, все проверки зелёные, фальш-зелёных не выявлено. AC8/AC10/AC12/AC14/AC15/
AC16 и SR-100/SR-102/SR-107/SR-112 подтверждены живым прогоном. Готово к гейту devops-guardian.

## 4. Docker-верификация qa-тестов (edge TEST4-9)

qa добавил `scripts/test-install-edge.sh` (TEST4-9) + `test-plan.md` (матрица AC1-16, SR-таблица).
Дирижёр прогнал `make test-install-all VERSION=v0.1.0-test` в Docker.

**Найден баг D-2 (тесты qa, НЕ продукт):** первый прогон — 42 PASS / **2 FAIL** в TEST8:
- assert `curl … | bash` ловил СОБСТВЕННУЮ доку install.sh (заголовочный комментарий + `--help`
  heredoc) → ложный FAIL на корректном коде.
- assert `--bind 127.0.0.1` падал из-за ведущего `--` (grep трактовал как опцию) → ложный FAIL,
  хотя test-install.sh:73 реально содержит `--bind 127.0.0.1`.
- (скрытый, ложно-зелёный) assert `chmod 777` использовал `\|` как литерал в ERE → проходил всегда,
  не ловя регрессию.
Продуктовый код (install.sh/test-install.sh) КОРРЕКТЕН — дефекты в самих ассертах. Возврат к qa.

**Фикс qa (commit `72893ec`):** хелперы используют `grep -qE --`; curl|sh-проверка фильтрует
комментарии и heredoc перед grep; chmod-альтернация `chmod\s+(-R\s+)?777`.

**Чистый повторный прогон `make test-install-edge VERSION=v0.1.0-test`:** **36 PASS / 0 FAIL.**

**Проверка не-тавтологичности 3 починенных ассертов (дирижёр, прямой прогон grep/perl):**
- `chmod 777`/`chmod -R 777` → MATCH (тест упал бы = ловит регрессию); `install -m 0755` → clean.
- `--bind 127.0.0.1` → найден в test-install.sh (корректный PASS).
- внедрённый `curl … | bash` в тело main() → ДЕТЕКТИРОВАН (тест упал бы); doc/heredoc `curl|bash` →
  корректно игнорируется (PASS на чистом коде).

### Сводка install-flow тестов (всё в Docker, чистый debian:stable-slim)
- **TEST1** позитив: `raxd v0.1.0-test (commit …, built …)` — формат верен, не `dev`.
- **TEST2** идемпотентность: ровно 1 бинарь, прежняя установка цела.
- **TEST3** негатив (реальный): подмена архива → код 3, бинарь не установлен.
- **TEST4** неподдерж. arch/OS (uname-shim i686/MINGW): код 2, бинарь не установлен.
- **TEST5** усечённый скрипт (обрыв до `main "$@"`): бинарь не появляется (защита AC2/SR-97).
- **TEST6** darwin-ветка статически: `xattr -d com.apple.quarantine` + Gatekeeper-инструкция.
- **TEST7** нет прав на запись (chmod 000, не-root user): код 4 + error:/hint:, бинарь не установлен.
- **TEST8** минимизация: нет eval/демона/`gpg --verify`/`curl|sh` в коде/`chmod 777`; bind 127.0.0.1.
- **TEST9** согласованность имён install.sh↔release.sh↔SHA256SUMS (4 цели).

## 5. Консистентность интерфейса — баг D-3 (язык вывода install.sh)

При финальной проверке (сверка docs против кода) дирижёр заметил: **install.sh печатал все
user-facing сообщения по-русски** (`==> определена платформа…`, `error:…`, `hint:…`, `--help`), тогда
как весь продукт англоязычный (STACK: интерфейс/CLI/docs — английский; Go-CLI выводит `error: cannot
bind to…`, `raxd version` английский; README/docs английские). Пользователь получал русский
`curl|sh`-установщик → английский бинарь. Нарушение конвенции STACK; не «итоговое состояние».

**Фикс devops (commit `f9143d7`):** все user-facing строки install.sh переведены на английский
(53/53 swap, логика не тронута); код-комментарии оставлены на русском (артефакт-конвенция); префиксы
`error:`/`hint:`/`==>`, коды выхода 0-5, структурные паттерны и слово `Gatekeeper` сохранены. Скоуп —
только install.sh (release.sh/test-install*.sh/Makefile — внутренняя CI/dev-оснастка, русский допустим).

**Чистый повторный прогон (`rm -rf dist && make ci-local && make test-install-edge`, VERSION=v0.1.0-test):**
- ci-local: все проверки зелёные; install.sh теперь выводит английский (`==> detected platform: …`,
  `==> SHA256 verified — archive is intact`); `raxd v0.1.0-test (commit f9143d7, built …)`; TEST1-3 PASS.
- edge: **42 PASS / 0 FAIL** (тесты грепают структурные паттерны/префиксы/коды — перевод их не сломал).
- 0 FAIL по обоим логам.

docs (installation.md/troubleshooting.md) синхронизированы с английским выводом install.sh (tech-writer).

## Итог Docker-верификации (devops + qa)
Все install-flow и сборочные проверки зелёные В DOCKER. Пойманы и устранены 3 реальных дефекта:
D-1 (host-build, §6), D-2 (3 фальш-/ложно-зелёных ассерта qa), D-3 (язык вывода install.sh —
консистентность интерфейса). Фальш-зелёных не осталось.

## 6. Живая end-to-end Docker-проверка merge-кандидата (перед merge)

Финальный чистый прогон дирижёра на КОММИТНОМ tip ветки (commit `908ed02`, после всех фиксов
D-1/D-2/D-3 и коммитов спек/докой):

```
$ rm -rf dist && make ci-local VERSION=v0.1.0-test
=== ci-local: ВСЕ ПРОВЕРКИ ПРОШЛИ ===   (0 FAIL)
```

Живой install-flow в чистом `debian:stable-slim` (мок-HTTP 127.0.0.1), английский вывод:
```
==> detected platform: linux/arm64
==> downloading raxd_v0.1.0-test_linux_arm64.tar.gz...
==> downloading SHA256SUMS...
==> verifying SHA256 integrity...
==> SHA256 verified — archive is intact
==> installing to /tmp/raxd-test-install/raxd...
==> raxd installed successfully (v0.1.0-test)
raxd v0.1.0-test (commit 908ed02, built 2026-05-22)
```
Негативный кейс целостности (реальный): подмена архива → `install.sh` код **3**, бинарь НЕ установлен.
§6: после хостового `make test-install` — НИ ОДНОГО `go build` на хосте (D-1 чист).

**Вывод:** merge-кандидат `908ed02` зелёный end-to-end в Docker. Готово к `git merge --no-ff` в develop.
