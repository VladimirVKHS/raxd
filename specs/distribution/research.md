# Research: distribution — установка raxd `curl | sh` + воспроизводимый релиз артефактов

> Автор research: research-analyst, команда raxd. Вход: `specs/distribution/spec.md` (16 AC,
> Q1–Q7), `CLAUDE.md`, `.claude/reference/{STACK,SECURITY-BASELINE}.ru.md`, существующий
> `Makefile` (`build-all` уже даёт 4 бинаря с `CGO_ENABLED=0 -mod=vendor`, `verify-cross` с
> docker-guard), `Dockerfile` (offline `-mod=vendor`, golang:1.25), `internal/version/version.go`
> (ldflags-переменные `Version/Commit/Date`). Образец стандарта строгости: `specs/service-install/research.md`.
> Задача — собрать факты С URL и дать обоснованные варианты для **architect** (он выбирает финал).
> Код не пишется.
>
> **Ревизия после research-guardian (needs-changes, 5 issue, исправлены 2026-05-22):** (1) Q1 — факт
> «goreleaser требует Go 1.26» уточнён: дословная цитата со страницы установки сохранена («Requires
> Go 1.26»), но добавлен критический нюанс корректности — **Go 1.26 на май 2026 ещё НЕ выпущен**
> (актуальная стабильная — 1.25), что лишь усиливает аргумент против `go install`. (2) Q2 — исправлен
> несуществующий URL `…/customization/package/checksum/` на корректный `…/customization/checksum/`;
> формулировка про построчный формат checksums уточнена («не специфицирован в доке на 2026-05-22»).
> (3) ADR-001 — секция «Решение» переформулирована из декларатива в рекомендацию (research не выбирает
> архитектуру; синхронизировано с research.md). (4) Q8 — для macOS-утилиты `shasum -a 256` дан
> релевантный источник (man-page shasum, не Linux-coreutils), а спорный факт «ships by default на
> чистом macOS без Homebrew» вынесен в Открытые вопросы. (5) Q6 — нерелевантный URL `install/oss`
> заменён на релевантные разделы кастомизации goreleaser (homebrew/nfpm).
>
> **Граничные условия проекта (research их НЕ нарушает):**
> - **Вендоринг + офлайн-сборка** обязательны: `proxy.golang.org` недоступен в Docker, сборки идут
>   `-mod=vendor` без `go mod download` (STACK §Кросс-компиляция; AC15). Любой новый build-tool,
>   который надо тянуть из сети внутри контейнера, конфликтует с этим.
> - **Только macOS + Linux**, amd64/arm64 (Windows/32-бит вне scope). `CGO_ENABLED=0` (статика).
> - **Remote отсутствует** (push/PR не делаем) → install-flow и CI должны быть проверяемы локально в
>   Docker без публичного хоста (AC12, AC14, Q3, Q5).
> - **Сервис вне scope distribution**: install.sh ставит ТОЛЬКО бинарь; регистрация — `raxd service
>   install` (задача service-install, готова).
>
> **Несоответствие STACK ↔ реальность, требующее решения architect (см. ADR-001):** STACK.ru.md
> §Кросс-компиляция называет инструментом сборки `goreleaser` v2 («Сборка/релизы: goreleaser v2,
> активно»), НО фактически 4 артефакта уже собирает **ручной Makefile** (`build-all`), а goreleaser в
> проект НЕ внесён (нет `.goreleaser.yaml`, нет бинаря, нет вендоринга goreleaser). То есть «выбор на
> бумаге» против работающего ручного пути. Развилку надо разрешить осознанно (ADR-001), и при выборе
> ручного пути — поправить STACK.

---

## Вопросы (привязка к spec Open Questions / AC)

- Q1 (AC8/AC15, spec Q1): инструмент релизной сборки — goreleaser vs ручной скрипт поверх Makefile;
  ключевой констрейнт — офлайн-сборка из `vendor/` внутри Docker (proxy недоступен).
- Q2 (AC2/AC8/AC16, spec Q2): формат архивов и схема именования артефактов + формат `SHA256SUMS`.
- Q3 (AC12, spec Q3, КРИТИЧНО): как тестировать `curl|sh` install БЕЗ публичного remote — мок-сервер
  артефактов в контейнере + параметризация URL.
- Q4 (AC9, spec Q4): путь установки (`/usr/local/bin` vs `~/.local/bin`), когда нужен sudo, как
  install.sh решает; конвенции curl|sh-инсталляторов.
- Q5 (AC14, spec Q5): CI без remote — GitHub Actions YAML как артефакт + локально-прогоняемый CI в Docker.
- Q6 (spec Q6): пакетные менеджеры (brew/apt/rpm) в v1 — делаем или откладываем.
- Q7 (AC11, spec Q7): macOS подпись/нотаризация без Apple Developer ID; минимум — quarantine + инструкция.
- Q8 (AC2, baseline §5): hardening-каркас install.sh (best-practices по источникам).

---

## Q1. Инструмент релизной сборки: goreleaser vs ручной скрипт поверх Makefile → ADR-001

### Найдено (факт → источник)
- **goreleaser жив и активен (2025-2026):** последняя версия линейки — **v2.15.4** (страница установки
  OSS, проверено 2026-05-22), проект активно сопровождается (релиз-блог v2.14 и далее). →
  https://goreleaser.com/getting-started/install/oss/ , https://goreleaser.com/blog/goreleaser-v2.14/
- **`goreleaser release --snapshot --clean` собирает ВСЁ локально без публикации и без git-тега:**
  «Sometimes we want to generate a full build of our project, but neither want to validate anything
  nor upload it to anywhere» — артефакты «won't be uploaded and will only be generated into the `dist`
  directory». То есть snapshot даёт матрицу архивов + checksums локально, без сети для публикации.
  → https://goreleaser.com/customization/snapshots/
- **`go install` свежего goreleaser требует НЕВЫШЕДШУЮ версию Go и тянет зависимости из сети:** дословно
  со страницы установки (проверено 2026-05-22) — для `go install github.com/goreleaser/goreleaser/v2@latest`
  указано **«Requires Go 1.26.»**. **Критический нюанс корректности:** на дату research (май 2026)
  **Go 1.26 ещё НЕ выпущен** — актуальная стабильная версия Go — **1.25** (1.26 ожидается ~авг 2026).
  То есть `@latest`-goreleaser сейчас **вообще нельзя поставить** через `go install` ни в проекте (Go
  1.25 в Dockerfile), ни где-либо ещё на released-тулчейне — это **усиливает** аргумент против go
  install (требуется будущая версия Go). Плюс `go install` тянет зависимости из сети (proxy в Docker
  недоступен). → https://goreleaser.com/getting-started/install/oss/ (цитата «Requires Go 1.26»),
  https://go.dev/dl/ (актуальная стабильная — 1.25 на май 2026)
- **Все официальные способы установки goreleaser требуют сети при установке:** go install, curl-скрипт
  (`https://goreleaser.com/static/run`), precompiled-бинари с GitHub Releases, deb/rpm/apk, brew,
  docker-образ `ghcr.io/goreleaser/goreleaser` — каждый качает что-то из сети в момент установки.
  → https://goreleaser.com/getting-started/install/oss/
- **Офлайн-режим goreleaser касается ТОЛЬКО лицензии Pro (air-gapped license-verify), а не самой
  установки/сборки OSS:** «If you run GoReleaser in an environment without internet access (air-gapped),
  you can export an offline license» — это про Pro-лицензию, не про получение OSS-бинаря в офлайне.
  → https://goreleaser.com/pro/
- **goreleaser-сборка из vendor возможна** (он вызывает `go build`, можно прокинуть `-mod=vendor`
  через `builds.flags`/env `GOFLAGS`), но это конфигурируется и проверяется отдельно; сам
  факт офлайн-`go build` из `vendor/` уже подтверждён в проекте (Dockerfile). → https://goreleaser.com/customization/builds/
- **Ручной путь уже работает офлайн:** `Makefile` цель `build-all` собирает 4 бинаря
  `raxd_{linux,darwin}_{amd64,arm64}` через `CGO_ENABLED=0 GOOS/GOARCH go build -mod=vendor`
  `-ldflags="-s -w"` без сети; `verify-cross` проверяет форматы (`file`) и запускает нативный бинарь
  с docker-guard. → прочитан `Makefile` (цели `build-all`, `build-*`, `verify-cross`).

### Варианты
- **A: goreleaser v2 (`release --snapshot --clean`), офлайн-бинарь добавляется в репозиторий/образ**
  — плюсы: «из коробки» матрица сборки + архивы + `checksums.txt` + changelog + (опц.) brew-cask +
  единая декларативная конфигурация (`.goreleaser.yaml`); индустриальный стандарт для Go-релизов;
  snapshot даёт всё локально без публикации (AC8 проверяем в контейнере). → https://goreleaser.com/customization/snapshots/
  Минусы: **проблема офлайн-установки goreleaser в Docker** — все официальные методы установки требуют
  сети, `go install@latest` требует Go 1.26 (невышедшая версия; у нас 1.25); чтобы остаться hermetic,
  надо ЛИБО вендорить сам goreleaser как зависимость и собирать его офлайн из `vendor/` (раздувает
  vendor, версия goreleaser завязывается на Go-тулчейн), ЛИБО класть прекомпилированный
  goreleaser-бинарь в образ/репо (доп. бинарный артефакт в git, обновляемый вручную) — обе опции
  добавляют операционную сложность поверх уже работающего Makefile. → https://goreleaser.com/getting-started/install/oss/
- **B: ручной скрипт сборки поверх существующего Makefile** (`build-all` + генерация архивов
  `tar.gz` + `sha256sum > SHA256SUMS`) — плюсы: **полностью офлайн уже сегодня** (только Go-тулчейн +
  coreutils `tar`/`sha256sum`, всё есть в `golang:1.25`-образе), ноль новых зависимостей и вендоринга,
  минимум «магии», прямой контроль над именами/раскладкой/форматом `SHA256SUMS` (точное соответствие
  `sha256sum -c`, AC3/AC16); переиспользует уже отлаженные `build-all`/`verify-cross`. → прочитан `Makefile`
  Минусы: нет «бесплатных» changelog/brew/подписи; больше ручного кода для архивации и проверки
  целостности (но объём небольшой и уже наполовину есть).
- **C: гибрид — ручная сборка (Makefile) сейчас + `.goreleaser.yaml` как «спящий» артефакт для
  будущего remote-релиза** — плюсы: сегодня всё офлайн и просто (B), а конфиг goreleaser лежит готовым
  на момент появления публичного remote/CI с сетью (тогда `go install`/docker-образ goreleaser
  доступны — при условии вышедшего Go 1.26 для `@latest` или пиннинга версии goreleaser под Go 1.25).
  Минусы: два пути сборки на бумаге (риск рассинхронизации имён/версий между Makefile и
  `.goreleaser.yaml`) — нужен тест согласованности (AC16).

### Рекомендация
**Для v1 без remote и при офлайн-Docker-констрейнте research склоняется к B (ручной скрипт поверх
Makefile)** как основному пути релизной сборки: он уже работает офлайн из `vendor/`, не вводит
проблему «как поставить goreleaser в контейнер без сети» и даёт точный контроль над `SHA256SUMS`-
форматом, критичным для install.sh (AC3/AC16). goreleaser остаётся отличным инструментом, но его
**ценность (матрица/архивы) у нас почти полностью покрыта `build-all`**, а его цена в нашей среде —
нерешённая офлайн-установка в Docker и требование `go install ...@latest` к Go 1.26 (версия ещё не
выпущена на май 2026). **C (гибрид)** — разумный компромисс, если architect хочет сохранить путь к
goreleaser на будущее (когда появится remote/CI с сетью и/или выйдет Go 1.26): тогда `.goreleaser.yaml`
пишется как артефакт, но в обязательном пути v1 не исполняется. **A (goreleaser как основной путь
сейчас)** — наименее удобен из-за офлайн-установки. Финал — за architect; зафиксировать в **ADR-001** и
при выборе B/C поправить STACK (он называет goreleaser основным инструментом).

---

## Q2. Формат архивов, именование артефактов и формат `SHA256SUMS` → влияет на AC16

### Найдено (факт → источник)
- **Конвенция архива (goreleaser-дефолт) = `tar.gz` для unix:** «Default: ['tar.gz']».
  → https://goreleaser.com/customization/archive/
- **Конвенция имени архива (goreleaser-дефолт):** name_template
  `{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}…` → т.е.
  **`raxd_<version>_<os>_<arch>.tar.gz`** (например `raxd_1.0.0_linux_amd64.tar.gz`,
  `raxd_1.0.0_darwin_arm64.tar.gz`). → https://goreleaser.com/customization/archive/
- **Что внутри архива (goreleaser-дефолт):** бинарь + автоматически включаемые
  `['LICENSE*','README*','CHANGELOG','license*','readme*','changelog']`. → https://goreleaser.com/customization/archive/
- **Имя checksum-файла (goreleaser-дефолт):** дословно «Default: '{{ .ProjectName }}_{{ .Version
  }}_checksums.txt', or, when split is set: '{{ .ArtifactName }}.{{ .Algorithm }}'» (т.е.
  `raxd_<version>_checksums.txt`); алгоритм по умолчанию — **sha256**: дословно «Default: 'sha256'».
  → https://goreleaser.com/customization/checksum/
- **Формат строки `SHA256SUMS` (авторитет — coreutils `sha256sum(1)`):** строка = «checksum, a space,
  a character indicating input mode ('*' for binary, ' ' for text), and name for each FILE»; для
  проверки `-c` файл должен содержать строки в формате вывода `sha256sum` — **`<hash>` + ДВА пробела
  (текстовый режим) + `<filename>`**. На GNU-системах text/binary дают одинаковый хэш. →
  https://man7.org/linux/man-pages/man1/sha256sum.1.html
- **goreleaser НЕ специфицирует точный построчный формат своего `checksums.txt`** — на странице
  кастомизации checksum (проверено 2026-05-22) описаны только алгоритм/имя файла/опция `split`, но
  формат строк внутри файла НЕ задокументирован → совместимость с `sha256sum -c` первоисточником
  goreleaser не подтверждена. Если нужна гарантированная совместимость — ручной путь генерирует ровно
  нативный формат `sha256sum *.tar.gz > SHA256SUMS`. → https://goreleaser.com/customization/checksum/

### Варианты
- **A: `tar.gz` + имя `raxd_<version>_<os>_<arch>.tar.gz` + файл `SHA256SUMS` нативного
  `sha256sum`-формата (`<hash>␣␣<filename>`), внутри архива бинарь + LICENSE/README** — плюсы:
  совпадает с индустриальной конвенцией (goreleaser-дефолт) И с уже существующими именами бинарей
  `raxd_<os>_<arch>` в `Makefile` (минимальный разрыв); `SHA256SUMS` напрямую проверяем
  `sha256sum -c` в install.sh (AC3); LICENSE/README в архиве — хорошая практика. → обе ссылки выше
- **B: голые бинари без архива (`raxd_<os>_<arch>` + `SHA256SUMS`)** — плюсы: install.sh проще (не
  распаковывать). Минусы: нет места для LICENSE/README; расходится с конвенцией; spec AC8 говорит
  «архивы». Хуже A. → spec AC8
- **C: имя файла `SHA256SUMS` vs `raxd_<version>_checksums.txt`** — spec и baseline называют файл
  **`SHA256SUMS`** (явно в AC3/AC8/AC16), что отличается от goreleaser-дефолта `*_checksums.txt`. →
  при ручном пути берём имя `SHA256SUMS` (как в spec); при goreleaser — переопределить `name_template`
  на `SHA256SUMS`. → spec AC3/AC8/AC16, https://goreleaser.com/customization/checksum/

### Рекомендация
**A:** архивы `tar.gz` с именем **`raxd_<version>_<os>_<arch>.tar.gz`**, внутри — бинарь `raxd`
(переименованный из `raxd_<os>_<arch>`) + `LICENSE`/`README`; файл целостности — **`SHA256SUMS`**
(имя из spec, не goreleaser-дефолт) в нативном `sha256sum`-формате `<hash>␣␣<filename>`, чтобы
install.sh проверял через `sha256sum -c` без парсинга. Это даёт сквозную согласованность «релиз →
имя, которое ищет install.sh → запись в SHA256SUMS» (AC16). Версия в имени берётся из той же
ldflags-версии, что и `raxd version` (AC10). Финал имён/раскладки — за architect/devops.

---

## Q3 (КРИТИЧНО). Тест `curl|sh` install БЕЗ публичного remote → ADR-002

### Найдено (факт → источник)
- **`python3 -m http.server` — стандартная библиотека, поднимает статический файл-сервер одной
  командой:** «`http.server` is part of The Python Standard Library»; CLI `python -m http.server
  [port]` (порт по умолчанию 8000), флаги `--directory/-d` (каталог раздачи) и `--bind/-b` (адрес).
  Явное предупреждение: «`http.server` is not recommended for production. It only implements basic
  security checks» — т.е. годится ровно для теста/dev, что нам и нужно. →
  https://docs.python.org/3/library/http.server.html
- **Параметризация URL у реальных инсталляторов — общепринятый паттерн:** deno install.sh берёт корень
  установки/источник из env (`DENO_INSTALL`), rustup — платформо-зависимый источник; т.е. инсталляторы
  штатно параметризуют источник/каталог через переменные окружения. →
  https://docs.deno.com/runtime/getting_started/installation/ ,
  https://rust-lang.github.io/rustup/installation/other.html
- **baseline §6 прямо разрешает мок-источник:** «Проверка install-flow (`curl | sh`) прогоняется в
  чистом Linux-контейнере (debian/ubuntu)»; spec AC12 «допускается прогон против локального/мок-
  источника артефактов внутри контейнера». → `.claude/reference/SECURITY-BASELINE.ru.md` §6, spec AC12
- **`curl` поддерживает `file://`** (локальный путь как URL) — альтернатива HTTP-серверу для самого
  простого случая; но HTTP ближе к боевому сценарию (тестирует реальный сетевой путь
  скачивания/редиректы). → https://man7.org/linux/man-pages/man1/curl.1.html

### Варианты (воспроизводимый рецепт-кандидат)
- **A: мок-HTTP-сервер артефактов внутри контейнера + `RAXD_BASE_URL`** — install.sh параметризует
  базовый URL через env (дефолт — будущий боевой URL, в тесте — `http://127.0.0.1:8000`); в чистом
  debian/ubuntu-контейнере: положить `dist/` (4 архива + `SHA256SUMS`) в каталог раздачи, поднять
  `python3 -m http.server 8000 -d /artifacts --bind 127.0.0.1 &`, затем
  `RAXD_BASE_URL=http://127.0.0.1:8000 sh install.sh` (или `curl … | sh`). Плюсы: ближе всего к
  боевому `curl|sh`-флоу (реальное HTTP-скачивание, проверка хэша, раскладка); один контейнер;
  воспроизводимо; параметризация URL — стандартный приём (deno). Минусы: нужен python3 в тест-образе
  (есть в debian/ubuntu или ставится; альтернатива — busybox httpd/nginx). →
  https://docs.python.org/3/library/http.server.html , https://docs.deno.com/runtime/getting_started/installation/
- **B: `file://`-источник (без HTTP-сервера)** — `RAXD_BASE_URL=file:///artifacts`, curl читает
  локальные файлы. Плюсы: ещё проще (ни сервера, ни порта). Минусы: не проверяет реальный сетевой путь
  (редиректы, HTTP-коды, обрыв), дальше от боевого сценария → слабее как тест install-flow. →
  https://man7.org/linux/man-pages/man1/curl.1.html
- **C: два сервиса в docker-compose (artifact-server + clean-client)** — отдельный контейнер-сервер
  раздаёт артефакты, отдельный чистый debian-клиент гоняет install.sh против него по сети между
  контейнерами. Плюсы: максимально честная изоляция «сервер ≠ клиент» (клиент реально чист). Минусы:
  больше инфраструктуры (compose, сеть), сложнее, чем A; выигрыш для v1 невелик. → baseline §6

### Рекомендация
**A: мок-HTTP-сервер (`python3 -m http.server`, либо busybox httpd) в чистом debian/ubuntu-контейнере
+ параметризация `RAXD_BASE_URL`** — лучший баланс «честность теста ↔ простота»: реальный HTTP-путь
скачивания и проверки хэша (AC12) в одном воспроизводимом контейнере, с env-параметризацией URL по
конвенции реальных инсталляторов (deno). Обязательно: install.sh должен ПОЗВОЛЯТЬ переопределить
базовый URL/версию через env (без хардкода единственного боевого хоста), иначе тест без remote
невозможен — это требование к дизайну install.sh, не только к тесту. **C (compose)** — если
architect/qa захотят более строгую изоляцию «сервер ≠ клиент». **B (file://)** — запасной самый
простой, но менее показательный. Развилка значима (затрагивает дизайн install.sh) → **ADR-002**.

---

## Q4. Путь установки и sudo → ADR-003

### Найдено (факт → источник)
- **rustup-init.sh ставит в пользовательский каталог без sudo:** standard way — `curl … | sh`
  скачивает rustup-init, который ставит toolchain в `~/.cargo`/`~/.rustup` (пользовательский home, без
  root). → https://rust-lang.github.io/rustup/installation/other.html
- **deno install.sh: пользовательский каталог по умолчанию + env-override:** «`DENO_INSTALL` … defaults
  to `$HOME/.deno`, with the executable placed in `$DENO_INSTALL/bin`»; «defaults to `~/.deno/bin` on
  macOS and Linux»; переопределяется `DENO_INSTALL`. → https://docs.deno.com/runtime/getting_started/installation/
- **`/usr/local/bin` — стандартный системный каталог для локально-устанавливаемых бинарей, обычно в
  `PATH` по умолчанию**, но запись в него требует root/sudo (владелец root). `~/.local/bin` — каталог
  пользователя по freedesktop/XDG-конвенции для пользовательских исполняемых файлов, НЕ требует sudo,
  но **может отсутствовать в `PATH`** на свежем хосте. → https://specifications.freedesktop.org/basedir-spec/latest/
  (XDG base-dir; `~/.local/bin` как пользовательский bin — широко принятая конвенция),
  https://refspecs.linuxfoundation.org/FHS_3.0/fhs/ch04s09.html (FHS: `/usr/local/bin` для локального ПО)
- **STACK по умолчанию называет `/usr/local/bin/raxd` (0755):** «установка в `/usr/local/bin/raxd`».
  → `.claude/reference/STACK.ru.md` §Установка
- **spec AC9 — критерий доступности независим от выбора пути:** после установки либо `command -v raxd`
  успешен (каталог в `$PATH`), либо скрипт ЯВНО печатает hint, как добавить каталог в `PATH`.
  → specs/distribution/spec.md AC9

### Варианты
- **A: дефолт `/usr/local/bin`, при отсутствии прав на запись — запрос sudo (явно) или fallback на
  `~/.local/bin` + PATH-hint** — плюсы: совпадает со STACK; `/usr/local/bin` обычно уже в `PATH`
  (бинарь сразу доступен — AC9); системно «правильно» (FHS). Минусы: требует sudo на запись (root) →
  для системной установки это ожидаемо, но AC9 требует НЕ повышать привилегии молча. → FHS,
  STACK §Установка, spec AC9
- **B: дефолт `~/.local/bin` (без sudo), с PATH-hint если не в `PATH`** — плюсы: установка без root
  (как deno/rustup — дружелюбно к непривилегированному пользователю, AC9 «без sudo»); нет повышения
  привилегий. Минусы: `~/.local/bin` часто НЕ в `PATH` на свежем сервере → нужен обязательный hint
  (AC9 это допускает); расходится со STACK-дефолтом. → deno docs, spec AC9
- **C: авто-детект прав + `--prefix`/env override** — если есть права на запись в `/usr/local/bin`
  (или пользователь root) → туда; иначе → `~/.local/bin` + hint; всегда можно переопределить
  `--prefix <dir>` или env. Плюсы: «делает правильное по умолчанию» на обеих ролях (root и обычный
  пользователь), покрывает AC9 тесты №1/№2/№3, гибко. Минусы: чуть больше логики детекта/ветвления в
  install.sh. → spec AC9, deno docs (env-override как образец)

### Рекомендация
**C (авто-детект прав + переопределение `--prefix`/env), с дефолтным предпочтением `/usr/local/bin`
когда есть права/root, иначе `~/.local/bin` + явный PATH-hint** — это закрывает все три теста AC9:
(№1) обычный пользователь ставит без sudo (в `~/.local/bin`); (№2) после установки либо `command -v
raxd` успешен, либо печатается hint; (№3) при нехватке прав — понятная ошибка/подсказка, не молчаливое
падение. sudo запрашивается ТОЛЬКО когда выбран системный каталог и явно сообщается (AC9). Дефолт
`/usr/local/bin` сохраняет совместимость со STACK (но STACK стоит дополнить упоминанием fallback).
Финал политики путей — за architect; развилка значима → **ADR-003**.

---

## Q5. CI без remote: GitHub Actions YAML (артефакт) + локальный CI в Docker → ADR-004

### Найдено (факт → источник)
- **GitHub Actions поддерживает матрицу GOOS/GOARCH через `strategy.matrix`** (стандартный способ
  кросс-сборки Go в CI), `actions/setup-go@v5` ставит нужную версию Go. → https://github.com/actions/setup-go ,
  https://docs.github.com/actions/automating-builds-and-tests/building-and-testing-go
- **GitHub-hosted runner имеет предустановленный Go** (одна патч-версия на каждую поддерживаемую
  minor); указание точной предустановленной версии в `setup-go` ускоряет setup (скачивание не нужно).
  → https://github.com/actions/setup-go (advanced-usage), https://docs.github.com/actions/automating-builds-and-tests/building-and-testing-go
- **`actions/setup-go` кэширует модули по умолчанию** (cache по хэшу `go.mod`); НО для нашего
  офлайн-`-mod=vendor` модульный кэш не нужен — сборка идёт из коммитнутого `vendor/` (никакого
  `go mod download`). → https://github.com/actions/setup-go , STACK §Кросс-компиляция (AC15)
- **baseline §6: «CI прогоняет сборку и тесты в контейнере»** — это требование можно выполнить
  локально-прогоняемым docker-таргетом, без публичного CI-runner; spec AC14 явно учитывает отсутствие
  remote. → `.claude/reference/SECURITY-BASELINE.ru.md` §6, spec AC14
- **Локальный CI уже частично есть:** `Dockerfile` target `test` (`go vet`+`go test`+`-race` для части
  пакетов, всё из `vendor/`), `Makefile` `build-all`/`verify-cross`/`test-unit`/`test-service`. →
  прочитаны `Dockerfile`, `Makefile`

### Варианты
- **A: написать `.github/workflows/*.yml` как АРТЕФАКТ (build-матрица + тесты в контейнере, из
  vendor), но НЕ запускать сейчас + локально-прогоняемый CI-скрипт/Make-таргет в Docker как фактический
  гейт v1** — плюсы: YAML готов к моменту появления remote (AC14 «задокументирован процесс»), а
  реальная проверяемость СЕГОДНЯ обеспечивается локальным docker-прогоном (сборка матрицы + тесты из
  `vendor/` без сети, AC14/AC15); переиспользует существующий Dockerfile/Makefile. Минусы: два
  описания CI (YAML + локальный скрипт) — риск рассинхрона (смягчается тем, что оба зовут одни и те же
  Make-таргеты). → spec AC14, github.com/actions/setup-go
- **B: только GitHub Actions YAML** — плюсы: одна точка. Минусы: без remote НЕ запускается → AC14
  «процесс работает» не проверяем сейчас. Не подходит как единственное. → spec AC14
- **C: только локальный docker-CI без YAML** — плюсы: проверяемо сейчас. Минусы: при появлении remote
  CI придётся писать заново; AC14 просит задокументированный автоматизированный процесс — YAML это
  формализует. → spec AC14

### Рекомендация
**A: и YAML-артефакт (`.github/workflows/release.yml`/`ci.yml`), и локально-прогоняемый CI в Docker
(Make-таргет/скрипт, вызывающий `build-all`+`SHA256SUMS`+`test-unit` из `vendor/` офлайн).** Локальный
docker-прогон — фактический гейт v1 (проверяет AC14/AC15 сейчас, без remote), а YAML — готовый
автоматизированный процесс на будущее (когда появится публичный repo/CI с сетью; тогда
`actions/setup-go` и предустановленный Go доступны). Оба должны звать ОДНИ И ТЕ ЖЕ Make-таргеты, чтобы
не разъехались. Кэш модулей в `setup-go` для нас не нужен (сборка из `vendor/`). Развилка значима →
**ADR-004**. Финал — за architect/devops.

---

## Q6. Пакетные менеджеры (brew/apt/rpm) в v1

### Найдено (факт → источник)
- **goreleaser умеет генерировать Homebrew (Cask) и linux-пакеты (deb/rpm/apk) как часть релиза:**
  - Homebrew: «After releasing to GitHub, GitLab, or Gitea, GoReleaser can generate and publish a
    _Homebrew Cask_ into a repository (_Tap_) that you have access to» (с v2.10 акцент на Cask;
    formula-секция помечена deprecated). → https://goreleaser.com/customization/homebrew/
  - Linux-пакеты: «GoReleaser can be wired to nfpm to generate and publish `.deb`, `.rpm`, `.apk`,
    `.ipk`, and Archlinux packages». → https://goreleaser.com/customization/nfpm/
  Т.е. если в будущем выбрать goreleaser, brew/пакеты добавляются конфигом, а не отдельной большой
  работой (но требуют публикации в repo/tap — см. ниже).
- **Для brew-tap/cask нужен публичный git-репозиторий + опубликованные артефакты:** homebrew tap = git-repo
  с Ruby-cask/formula, ссылающейся на release-архив + sha256; goreleaser-генерация brew привязана к
  релизу «After releasing to GitHub/GitLab/Gitea» → требует remote/хостинга артефактов, которого
  сейчас нет. → https://goreleaser.com/customization/homebrew/ , spec Q3 (хостинг — открытый)
- **spec прямо относит пакетные менеджеры к Out of Scope v1:** «Пакетные менеджеры
  (Homebrew/apt/rpm/AUR …) … НЕ обязательны в v1; возможны как отдельная будущая задача». →
  specs/distribution/spec.md Out of Scope, Q6

### Рекомендация
**Отложить brew/apt/rpm за пределы v1** — `curl|sh` достаточно для цели «установить на свежий хост
одной командой», а brew/пакеты требуют публичного хостинга артефактов (которого нет, Q3) и отдельной
инфраструктуры (tap-repo, пакетные метаданные). Зафиксировать как будущую задачу. **Что нужно для
brew в будущем (для планирования):** публичный URL release-архивов + их sha256, отдельный
homebrew-tap git-repo с cask/formula (или goreleaser-генерация через nfpm/homebrew-секции). Отдельный
ADR не обязателен (spec уже относит это к Out of Scope) — достаточно записи в plan «отложено,
предусловия: remote-хостинг».

---

## Q7. macOS подпись/нотаризация без Apple Developer ID → ADR-005 (минимум quarantine)

### Найдено (факт → источник)
- **Нотаризация ТРЕБУЕТ Apple Developer ID-сертификат и членство в Apple Developer Program:**
  «notarization requires an Apple Developer ID account/certificate … валидный Developer ID
  certificate … Apple Developer Program membership … submit your signed software to Apple's
  notarization service». Подпись (Developer ID) — фундамент, нотаризация — облачное сканирование
  Apple, Gatekeeper — runtime-проверка подписи+нотаризации (macOS 10.15+). →
  https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution
- **КРИТИЧНАЯ НАХОДКА: curl НЕ ставит `com.apple.quarantine`** — официальная позиция Apple (Quinn,
  DTS Engineer, Apple Developer Forums): «Most Unix-y tools don't quarantine their downloads,
  including curl and scp»; это «expected behavior … not considered a security gap». Атрибут карантина
  ставят GUI-загрузчики (браузеры, Dropbox), а НЕ `/usr/bin/curl`/`/usr/bin/wget`. →
  https://developer.apple.com/forums/thread/666452
- **Снятие quarantine:** `xattr -d com.apple.quarantine <file>` (если атрибут есть). →
  https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution
  (раздел про quarantine), https://developer.apple.com/forums/thread/666452
- **baseline §5:** «macOS: подпись + нотаризация для „правильной" дистрибуции; минимум — снятие
  quarantine (`xattr -d com.apple.quarantine`) и понятная инструкция». →
  `.claude/reference/SECURITY-BASELINE.ru.md` §5

### Следствие для дизайна (важно для architect)
Поскольку **наш способ доставки — именно `curl|sh`**, а curl карантин НЕ ставит, бинарь, скачанный
самим install.sh через curl, в типичном случае **не будет иметь** `com.apple.quarantine` → шага
снятия может и не потребоваться. Однако `xattr -d com.apple.quarantine` **идемпотентен и безвреден**
(если атрибута нет — no-op), поэтому держать его в darwin-ветке как «пояс безопасности» оправдано: он
покрывает кейс, когда пользователь получил архив иным путём (браузер/AirDrop → карантин есть).
Полноценная подпись/нотаризация без сертификата (см. Out of Scope) **невозможна** и не проверяема в
рамках задачи (требует реального macOS + Apple-аккаунта, вне Docker, AC13).

### Рекомендация
**Минимум по AC11/baseline §5: darwin-ветка install.sh выполняет `xattr -d com.apple.quarantine` на
установленном бинаре (идемпотентно, без ошибки если атрибута нет) И печатает короткую инструкцию на
случай, если Gatekeeper всё же блокирует** (как снять вручную / «Открыть» в System Settings → Privacy
& Security). В документации зафиксировать: (1) при `curl|sh` карантин обычно не ставится (Apple/Quinn),
(2) полноценная нотаризация — отдельная задача при наличии Apple Developer ID ($99/год). Развилка
(делать ли нотаризацию / как именно вести себя в darwin-ветке) → **ADR-005** (proposed). Реальный
Gatekeeper-флоу проверяется на живом macOS вне Docker (AC13) — это ограничение среды, не снятие
требования.

---

## Q8. Hardening-каркас install.sh (baseline §5) — best-practices

### Найдено (факт → источник)
- **`set -u` + main-функция, вызываемая в конце (защита от обрыва `curl|sh`):** rustup-init.sh — `#!/bin/sh`,
  `set -u`, всё тело в `main()`, вызов в конце `main "$@" || exit 1`. Это и есть защита: при обрыве
  закачки исполняется только то, что уже целиком прочитано; пока функция не вызвана в самом конце —
  установка не начинается. → https://raw.githubusercontent.com/rust-lang/rustup/master/rustup-init.sh
  (см. также https://github.com/rust-lang/rustup)
- **Детект OS/arch через `uname`:** rustup `_ostype="$(uname -s)"`, `_cputype="$(uname -m)"` — ровно
  как требует spec AC4 (`uname -s`→{linux,darwin}, `uname -m`→{amd64,arm64} с нормализацией
  x86_64→amd64, aarch64/arm64→arm64). → https://raw.githubusercontent.com/rust-lang/rustup/master/rustup-init.sh ,
  https://man7.org/linux/man-pages/man1/uname.1.html
- **Временный каталог + очистка:** rustup использует `mktemp -d` и удаляет файл/каталог в конце
  `main()` (`rm`/`rmdir`). baseline §5 и spec AC2 требуют `trap` на cleanup при ЛЮБОМ выходе (надёжнее
  явных rm, т.к. срабатывает и при ошибке/прерывании). → https://raw.githubusercontent.com/rust-lang/rustup/master/rustup-init.sh ,
  https://man7.org/linux/man-pages/man1/mktemp.1.html
- **Безопасные флаги curl:** `-fsSL` = fail on HTTP error (`-f`) + silent (`-s`) + show-error (`-S`) +
  follow-redirects (`-L`); используются и rustup, и deno. → https://docs.deno.com/runtime/getting_started/installation/ ,
  https://man7.org/linux/man-pages/man1/curl.1.html
- **Проверка sha256 ПЕРЕД размещением:** на Linux — `sha256sum -c` против `SHA256SUMS` (формат
  `<hash>␣␣<file>`, см. Q2). → https://man7.org/linux/man-pages/man1/sha256sum.1.html
- **macOS-утилита проверки хэша — `shasum`:** на macOS GNU `sha256sum` обычно НЕ установлен, штатная
  утилита — `shasum` (Perl-скрипт, поставляемый с системным Perl); опция `-a` выбирает алгоритм
  («`-a, --algorithm 1 (default), 224, 256, 384, 512`»), т.е. `shasum -a 256` считает SHA-256; shasum
  «mimics the behavior of the combined GNU sha1sum, sha224sum, sha256sum…». Т.е. install.sh на darwin
  должен использовать `shasum -a 256` (а не `sha256sum`). → https://ss64.com/mac/shasum.html ,
  https://keith.github.io/xcode-man-pages/shasum.1.html
  (ВАЖНО: факт «`shasum` присутствует по умолчанию на чистом macOS без Homebrew» первоисточником
  Apple в рамках этого research НЕ подтверждён дословно → см. Открытые вопросы.)
- **`set -euo pipefail` нюанс переносимости:** `pipefail` — это bash/ksh/zsh, в чистом POSIX `sh`
  (dash) `set -o pipefail` НЕ всегда доступен; rustup поэтому таргетит dash и использует только `set -u`
  + явные проверки. spec AC2 требует именно `set -euo pipefail` → значит шебанг install.sh должен быть
  `#!/usr/bin/env bash` (или `#!/bin/bash`), не `/bin/sh`. → https://www.gnu.org/software/bash/manual/bash.html#The-Set-Builtin
  (раздел `pipefail`), spec AC2

### Рекомендация (для architect/devops — это требования к каркасу, не выбор)
Подтверждённые источниками best-practices, прямо ложащиеся на AC2/AC4/AC7:
1. Шебанг **`#!/usr/bin/env bash`** + **`set -euo pipefail`** (раз spec требует `pipefail` — нужен
   bash, не dash). → bash manual, spec AC2
2. **Всё тело в функции** (напр. `main`) с **единственным вызовом в самом конце** (`main "$@"`) —
   защита от частичного исполнения при обрыве закачки (паттерн rustup). → rustup-init.sh, spec AC2
3. **`trap '<cleanup>' EXIT INT TERM`** на удаление `mktemp -d`-каталога при любом выходе (надёжнее
   явных rm). → mktemp(1), spec AC2
4. **Детект OS/arch через `uname -s`/`uname -m`** + нормализация `x86_64→amd64`,
   `aarch64|arm64→arm64`; неподдерживаемое → понятная ошибка без установки. → uname(1), spec AC4
5. **Проверка sha256 ПЕРЕД размещением**: Linux — `sha256sum -c`; macOS — `shasum -a 256`; при
   несовпадении — abort с ненулевым кодом, бинарь не ставится (AC3). → sha256sum(1), shasum man-page
6. **curl `-fsSL`** для скачивания (fail/silent/show-error/follow-redirects). → curl(1), deno docs
7. **Минимум действий** (детект → скачать выбранный артефакт → проверить хэш → разместить → опц.
   quarantine на macOS), без лишних загрузок/запусков (AC7). → spec AC7

---

## Сводная таблица рекомендаций (1-2 строки на Q)

| Q | Рекомендация research (финал — за architect) | ADR |
|---|---|---|
| Q1 | Ручной скрипт поверх `Makefile build-all` (офлайн уже работает); goreleaser отложить из-за нерешённой офлайн-установки в Docker и требования `go install ...@latest` к Go 1.26 (версия ещё не выпущена). Гибрид (C) — если хочется `.goreleaser.yaml` на будущее. | ADR-001 |
| Q2 | `tar.gz`, имя `raxd_<version>_<os>_<arch>.tar.gz`, внутри бинарь+LICENSE/README; файл `SHA256SUMS` нативного `sha256sum`-формата (`<hash>␣␣<file>`). | (в ADR-001) |
| Q3 | Мок-HTTP-сервер (`python3 -m http.server`) в чистом debian/ubuntu-контейнере + параметризация `RAXD_BASE_URL`; install.sh ОБЯЗАН поддерживать env-override URL/версии. | ADR-002 |
| Q4 | Авто-детект прав + `--prefix`/env override: дефолт `/usr/local/bin` при правах/root, иначе `~/.local/bin` + явный PATH-hint; sudo только явно. | ADR-003 |
| Q5 | И YAML `.github/workflows/*` как артефакт на будущее, И локально-прогоняемый docker-CI (одни Make-таргеты) как фактический гейт v1. | ADR-004 |
| Q6 | Отложить brew/apt/rpm за пределы v1 (curl\|sh достаточно; brew/cask требует publ. хостинга + tap-repo). | — |
| Q7 | darwin-ветка: идемпотентный `xattr -d com.apple.quarantine` + инструкция; нотаризация невозможна без Apple Developer ID → отдельная задача. Важно: curl карантин НЕ ставит. | ADR-005 |
| Q8 | bash + `set -euo pipefail`, тело в `main` с вызовом в конце, `trap` cleanup, `uname`-детект, sha256 перед размещением (`sha256sum`/`shasum -a 256`), curl `-fsSL`. | (требования к каркасу) |

---

## Риски / неизвестные

- **Офлайн-установка goreleaser в Docker не решена «бесплатно»** (все методы установки требуют сети;
  `go install ...@latest` требует Go 1.26 — версия ещё не выпущена на май 2026, у нас 1.25). Если
  architect всё же выберет goreleaser — нужен явный механизм его офлайн-получения (вендоринг самого
  goreleaser ИЛИ прекомпилированный бинарь в образе/репо, с пиннингом версии goreleaser, совместимой с
  Go 1.25), что добавляет операционную сложность. → https://goreleaser.com/getting-started/install/oss/ ,
  https://go.dev/dl/
- **Тест install-flow без remote** опирается на то, что install.sh **спроектирован** с env-override
  URL/версии. Если install.sh захардкодит единственный боевой URL — AC12 станет непроверяемым без
  remote. Это требование к ДИЗАЙНУ скрипта, не только к тесту (для architect/cli-ux).
- **macOS-проверки непроверяемы в Docker (AC13):** реальный Gatekeeper-флоу, фактическое снятие
  quarantine и поведение `xattr` верифицируются только на живом macOS вне Docker. Research подтверждает
  лишь: (а) curl не ставит карантин (Apple/Quinn), (б) `xattr -d` идемпотентен, (в) нотаризация требует
  Apple Developer ID. Сам прогон на macOS — вне scope этой среды.
- **Реальный remote-релиз (GitHub Release/CDN) не выполняется и не проверяется** — задача требует лишь
  «артефакты + SHA256SUMS существуют и согласованы» (spec Out of Scope). Фактическая публикация и
  работа GitHub Actions на runner'е — за рамками текущей проверяемости (нет remote).
- **goreleaser-формат `checksums.txt` построчно не задокументирован** — на странице кастомизации
  checksum (проверено 2026-05-22) формат строк не специфицирован; совместимость с `sha256sum -c` у
  goreleaser первоисточником не подтверждена. При ручном пути этот риск снимается (генерируем нативный
  `sha256sum`-формат). → https://goreleaser.com/customization/checksum/
- **`set -o pipefail` непортируем на чистый POSIX `sh`/dash** — spec AC2 требует `pipefail`, значит
  install.sh должен быть bash-скриптом (`#!/usr/bin/env bash`), а не `/bin/sh`. Это сужает совместимость
  на хосты без bash (редкость на Linux-серверах; на macOS bash есть, хоть и старый 3.2). →
  https://www.gnu.org/software/bash/manual/bash.html#The-Set-Builtin

## Открытые вопросы

- [ ] Q3-detail: точная политика дефолтного боевого URL (когда remote появится) и формат версии
      (latest vs пиннинг) в install.sh — за architect/devops (research подтвердил только МЕХАНИЗМ
      env-override, не конкретный боевой URL, которого ещё нет).
- [ ] Q4-detail: на macOS дефолтный системный каталог `/usr/local/bin` принадлежит root и на Apple
      Silicon homebrew использует `/opt/homebrew/bin` — нужно ли install.sh учитывать это для macOS
      (research подтвердил `/usr/local/bin` как FHS-конвенцию для Linux; точная macOS-раскладка не
      проверена первоисточником в рамках этого research) → за architect.
- [ ] Q7-detail: фактическое поведение Gatekeeper для бинаря, установленного `curl|sh` на конкретных
      версиях macOS (15/26), не проверено (вне Docker, AC13) — подтверждена только официальная позиция
      Apple, что curl не квалифицирует карантин.
- [ ] Q8-detail (новый, по итогам ревизии): подтвердить first-party-источником (Apple), что `shasum`
      присутствует по умолчанию на чистом macOS без Homebrew. В рамках research подтверждено лишь, ЧТО
      делает `shasum -a 256` (man-page) и что это BSD/macOS-утилита; «ships by default» дословно не
      подтверждён. Резервный план для install.sh: детект доступной хэш-утилиты (`shasum -a 256` ИЛИ
      `sha256sum`) с понятной ошибкой, если ни одной нет. → за architect/devops.
</content>
