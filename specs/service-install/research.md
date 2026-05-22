# Research: service-install — регистрация raxd как управляемого системного сервиса (Linux systemd + macOS launchd), не-root

> Автор research: research-analyst, команда raxd. Вход: `specs/service-install/spec.md` (16 AC,
> Q1–Q8), `CLAUDE.md`, `.claude/reference/{STACK,SECURITY-BASELINE,MCP-INTEGRATION}.ru.md`,
> образец `specs/file-upload/research.md`, контекст кода `internal/cli/serve.go` и
> `internal/config/paths.go`. Задача: собрать факты С URL и дать обоснованные варианты для
> **architect** (финальную архитектуру выбирает он). Код не пишется. Все факты проверены по
> первоисточникам (man-pages freedesktop/man7.org, Apple/xcode launchd.plist(5), go.dev,
> pkg.go.dev/github.com).
>
> **Ревизия после research-guardian (needs-changes, 3 дефекта строгости источников, исправлены
> 2026-05-22):** (1) Q4 — убран сторонний URL `docs.arbitrary.ch`; утверждение про
> AmbientCapabilities+NoNewPrivileges переформулировано к тому, что дословно подтверждает
> systemd.exec(5) (ambient-капы для непривилегированного пользователя; auto `keep-caps`), а явная
> «совместимость с NoNewPrivileges» вынесена в Открытые вопросы. (2) Q7 — дана дословная цитата
> определения «clean exit» из systemd.service(5) (включая список сигналов SIGHUP/SIGINT/SIGTERM/
> SIGPIPE как clean-exit и исключение «aforementioned four signals» при on-failure); вывод о
> graceful-stop по SIGTERM сохранён и подтверждён. (3) Q8 — для «`-race` требует cgo» добавлен
> официальный URL go.dev/doc/articles/race_detector с дословной цитатой.
>
> Граничные условия (из STACK / go.mod / baseline), которые research НЕ нарушает:
> - **Go 1.25.0** (`go.mod`: `go 1.25.0`), stdlib предпочтительна; проект **вендорится**
>   (`-mod=vendor`, `proxy.golang.org` недоступен в Docker) → новые внешние зависимости требуют
>   `go mod vendor` + коммит `vendor/` и обоснования.
> - Платформы: **Linux + darwin**, amd64/arm64. Windows вне scope.
> - `CGO_ENABLED=0` для релизных бинарей (STACK §Кросс-компиляция); `-race` требует CGO (только для
>   тестов, не релиз).
> - Дефолтный порт **7822** (НЕ привилегированный) — root для запуска НЕ нужен; capability нужна
>   ТОЛЬКО при ручной смене на <1024 (spec AC6/AC7).
> - Текущий резолв путей — XDG через `os.Getenv` (`internal/config/paths.go`):
>   `~/.config/raxd` (или `$XDG_CONFIG_HOME/raxd`), `~/.local/state/raxd` (или `$XDG_STATE_HOME`).
>
> **Важная находка по стеку (требует решения architect):** STACK.ru.md называет
> `kardianos/service` выбором для кроссплатформенного сервиса, НО в `go.mod` и `vendor/` его **нет**
> (grep по `*.go`: единственное совпадение `kardianos`/`systemd`/`launchd` — stdlib-файл
> `vendor/golang.org/x/sys/windows/dll_windows.go`, не связан). То есть это пока «выбор на бумаге»,
> а не вендоренная зависимость → ADR-001 (использовать библиотеку vs ручная генерация unit/plist).

---

## Вопросы (привязка к spec Open Questions / AC)

- Q1 (AC6, spec Q1): сервис-пользователь Linux — `DynamicUser=yes` vs статический системный
  пользователь (`useradd --system` / `sysusers.d`); как уживается с персистентным состоянием.
- Q1-mac (AC6, spec Q1): под кем гонять launchd-сервис — системный daemon `/Library/LaunchDaemons`
  + `UserName=` vs user agent; создание/выбор сервис-пользователя на macOS.
- Q3 (AC6/AC8, spec Q3): системные пути для не-root сервиса vs домашние XDG; как состыковать с
  текущим XDG-резолвингом raxd.
- Q4 (AC7, spec Q4): привилегированный порт <1024 без root — systemd `AmbientCapabilities=` vs
  `setcap` file-capabilities; macOS-аналог.
- Q5 (AC8, spec Q5): ротация/ограничение роста аудит-журнала — journald vs logrotate; macOS.
- Q6 (AC2, spec Q6): формат/расположение unit/plist; минимально-безопасный набор директив.
- Q7 (AC4/AC5, spec Q7): restart-семантика «перезапуск при сбое, НЕ при штатной остановке».
- Q8 (AC14/AC15, spec Q8): кросс-сборка Go (матрица, CGO, критерий «рабочести» не-нативного бинаря).
- Q9 (AC16, baseline §6): воспроизводимый рецепт systemd-в-Docker для install→start→...→uninstall.
- Q0 (стек): библиотека `kardianos/service` vs ручная генерация unit/plist (сквозной для Q4/Q6/Q7).

---

## Q0 (сквозной). Библиотека `kardianos/service` vs ручная генерация unit/plist → ADR-001

### Найдено (факт → источник)
- `kardianos/service` — «Run go programs as a service on major platforms»; поддержка
  «Windows XP+, Linux (systemd | Upstart | SysV), OSX/Launchd, FreeBSD, Solaris, AIX».
  **Активно сопровождается:** последний тег **v1.2.4 от 14 июля 2025** (коммит «go.mod: bump sys»);
  предыдущая значимая версия v1.2.2 — окт 2022 (т.е. был длительный перерыв, потом обновление 2025).
  → https://github.com/kardianos/service , https://github.com/kardianos/service/tags
- **Config-поля (pkg.go.dev v1.2.4):** `Name`, `DisplayName`, `Description`, `UserName` («Run as
  username»), `Arguments`, `Executable`, `Dependencies`, `WorkingDirectory`, `ChRoot`,
  `Option KeyValue` («System specific options»), `EnvVars map[string]string`.
  → https://pkg.go.dev/github.com/kardianos/service
- **Опции systemd (Option-ключи):** `SystemdScript` (полностью кастомный шаблон unit),
  `UserService`, `ReloadSignal`, `Restart` («How shall service be restarted», дефолт `"always"`),
  `SuccessExitStatus`, `PIDFile`, `LogOutput`, `LogDirectory` (дефолт `/var/log`), `LimitNOFILE`.
  → https://pkg.go.dev/github.com/kardianos/service
- **Опции launchd (Option-ключи):** `LaunchdConfig` (полностью кастомный plist), `KeepAlive`
  (bool, дефолт true — «Prevent system from stopping service automatically»), `RunAtLoad` (bool,
  дефолт false), `SessionCreate`. → https://pkg.go.dev/github.com/kardianos/service
- **Ограничения (README + pkg.go.dev):** поле `Dependencies` «Not yet fully implemented on Linux
  or OS X» (формально «not implemented for Linux systems and Launchd»). **Нет** Option-ключей и
  полей для `AmbientCapabilities`/capabilities, `DynamicUser`, hardening-директив
  (`ProtectSystem`/`NoNewPrivileges` и т.п.), и для launchd — **нет** `KeepAlive`-словаря с
  `SuccessfulExit` (только булев `KeepAlive`). → https://github.com/kardianos/service ,
  https://pkg.go.dev/github.com/kardianos/service
- **kardianos/service в проекте отсутствует:** в `go.mod` его нет; grep по `*.go` не находит
  использования (единственное совпадение `kardianos`/`systemd`/`launchd` —
  `vendor/golang.org/x/sys/windows/dll_windows.go`, нерелевантно). → `go.mod`, grep репозитория.

### Варианты
- **A: `kardianos/service` как абстракция жизненного цикла + кастомные шаблоны (`SystemdScript`/
  `LaunchdConfig`)** — плюсы: готовый кросс-платформенный install/uninstall/start/stop/status, не
  надо писать платформенный код управления; активно сопровождается (v1.2.4 2025). Минусы: его
  встроенные шаблоны НЕ покрывают нужные нам безопасные директивы (нет `DynamicUser`,
  `AmbientCapabilities`, `StateDirectory`, hardening; launchd — нет `SuccessfulExit`) →
  пришлось бы всё равно подменять весь unit/plist через `SystemdScript`/`LaunchdConfig`, т.е. сам
  текст генерируем мы. **Новая внешняя зависимость** (вендоринг). → https://github.com/kardianos/service
- **B: ручная генерация unit/plist + вызов нативных менеджеров (`systemctl`/`launchctl`) через
  `os/exec`** — плюсы: полный контроль над текстом unit/plist (все hardening/capability/StateDirectory
  директивы), без новой зависимости (stdlib `text/template` + `os/exec`); сам контракт операций
  (install/uninstall/start/stop/status) тонкий. Минусы: больше платформенного кода и идемпотентности
  на нас (откат AC11, детект менеджера AC12); надо самим аккуратно реализовать. → spec AC1/AC9–AC12
- **C: гибрид — `kardianos/service` ТОЛЬКО для lifecycle-операций (Install/Start/Stop/Status), но с
  полностью кастомными `SystemdScript`/`LaunchdConfig`-шаблонами** — плюсы: переиспользуем
  отлаженные install/uninstall и детект init-системы, но текст сервиса полностью наш (безопасный).
  Минусы: всё равно новая зависимость; полу-контроль (часть поведения — внутри библиотеки).
  → https://pkg.go.dev/github.com/kardianos/service

### Рекомендация
Это **значимая развилка для architect → ADR-001**. Факты: библиотека жива (v1.2.4, 2025) и даёт
готовый lifecycle, НО её дефолтные шаблоны не покрывают обязательные для нас безопасные директивы —
в любом случае текст unit/plist придётся писать самим (через `SystemdScript`/`LaunchdConfig` в
варианте A/C, или напрямую в B). Поскольку «ценность» библиотеки (генерация unit/plist) для нас
обнуляется требованиями безопасности, а её цена — новая вендоренная зависимость, **research склоняется
к B (ручная генерация + нативные менеджеры) либо C (гибрид)**; финальный выбор «зависимость ради
lifecycle-абстракции vs полностью свой код» — за architect. Зафиксировать: STACK.ru.md называет
kardianos/service, но его в проекте нет — несоответствие STACK ↔ go.mod надо разрешить решением
ADR-001 (и при необходимости поправить STACK).

---

## Q1 (Linux). Сервис-пользователь: `DynamicUser=yes` vs статический системный пользователь → ADR-002

### Найдено (факт → источник)
- **`DynamicUser=`:** «If set, a UNIX user and group pair is allocated dynamically when the unit is
  started, and released as soon as it is stopped. The user and group will not be added to
  /etc/passwd or /etc/group, but are managed transiently during runtime.»
  → https://man7.org/linux/man-pages/man5/systemd.exec.5.html
- **`DynamicUser` + персистентное состояние:** «If DynamicUser= is used, the logic for
  CacheDirectory=, LogsDirectory= and StateDirectory= is slightly altered: the directories are
  created below /var/cache/private, /var/log/private and /var/lib/private, respectively, which are
  host directories made inaccessible to unprivileged users, which ensures that access to these
  directories cannot be gained through dynamic user ID recycling. Symbolic links are created to hide
  this difference in behaviour.» → https://man7.org/linux/man-pages/man5/systemd.exec.5.html
- **`StateDirectory` персистентен:** «The directories [StateDirectory=, ConfigurationDirectory=…]
  are not removed when the unit is stopped» → состояние (`keys.db`, `tls/`, `uploads/`, аудит)
  переживает рестарт даже при DynamicUser. → https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html
- **Риск UID-recycle (мотивация StateDirectory):** «Use StateDirectory=, CacheDirectory= and
  LogsDirectory= in order to assign a set of writable directories for specific purposes to the
  service in a way that they are **protected from vulnerabilities due to UID reuse**.»
  → https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html
- **Статический системный пользователь (альтернатива DynamicUser):** «If DynamicUser= is not used
  the specified user and group must have been created statically in the user database no later than
  the moment the service is started, for example using the **sysusers.d(5)** facility, which is
  applied at boot or package install time.» → https://man7.org/linux/man-pages/man5/systemd.exec.5.html
- **`sysusers.d` / `systemd-sysusers`:** «systemd-sysusers creates system users and groups, based
  on files in the format described in sysusers.d(5)… at package installation or boot time». Тип `u`
  создаёт системного пользователя+группу, если их ещё нет. Пример install-скрипта (RPM):
  `echo 'u radvd - "radvd daemon"' | systemd-sysusers --replace=/usr/lib/sysusers.d/radvd.conf -`.
  → https://man7.org/linux/man-pages/man8/systemd-sysusers.8.html ,
  https://man7.org/linux/man-pages/man5/sysusers.d.5.html
- **`useradd --system`:** традиционный путь создать системного пользователя при установке.
  → https://man7.org/linux/man-pages/man8/useradd.8.html
- baseline §3: «Демон работает НЕ от root: выделенный системный пользователь…».
  → `.claude/reference/SECURITY-BASELINE.ru.md`

### Варианты
- **A: `DynamicUser=yes` + `StateDirectory=raxd`** — плюсы: пользователя НЕ нужно создавать самим
  (нет шага `useradd`/`sysusers.d` при install → проще идемпотентность AC9, меньше «осиротевших»
  артефактов AC10); транзиентный UID + `/var/lib/private` защищают от UID-reuse; сильнейшая
  изоляция «из коробки». Минусы: состояние живёт в `/var/lib/private/raxd` (через симлинк
  `/var/lib/raxd`), путь не «обычный» — нужно учесть при резолве путей (Q3); UID непостоянен →
  файлы, созданные руками вне сервиса, могут стать недоступны; **только Linux/systemd** (на macOS
  аналога нет → асимметрия с launchd). → https://man7.org/linux/man-pages/man5/systemd.exec.5.html
- **B: статический системный пользователь (`raxd`) через `sysusers.d` (или `useradd --system`) +
  `User=raxd` + `StateDirectory=raxd`** — плюсы: стабильный UID/GID (предсказуемые владельцы файлов,
  проще аудит и ручное обслуживание); концептуально симметрично с macOS (там тоже именованный
  `UserName`); `StateDirectory` всё равно даёт `/var/lib/raxd` с нужными правами. Минусы: нужен шаг
  создания пользователя при install (идемпотентность: «создать, если нет»; политика «уже существует»
  — spec Q1), и аккуратный uninstall (оставлять ли пользователя — обычно да). → sysusers.d(5),
  useradd(8), systemd.exec(5)
- **C: запуск под существующим непривилегированным пользователем (напр. `nobody`)** — плюсы:
  ничего не создавать. Минусы: `nobody` шарится многими сервисами → нарушает изоляцию (UID-reuse,
  пересечение прав на файлы) — антипаттерн; противоречит «выделенный пользователь» baseline §3.
  Отвергнут. → `.claude/reference/SECURITY-BASELINE.ru.md` §3

### Рекомендация
**B (статический системный пользователь) как основной кандидат**, потому что: (1) даёт симметрию с
macOS (`UserName=raxd` в plist) — единый ментальный контракт «выделенный пользователь raxd» на обеих
платформах; (2) стабильный UID упрощает владение персистентным состоянием (keys.db/tls/uploads/аудит)
и его обслуживание; (3) `StateDirectory=raxd` всё равно решает права/создание каталога. **A
(DynamicUser)** — привлекательная Linux-only альтернатива с лучшей изоляцией, но создаёт асимметрию
платформ и нестабильный UID для состояния; стоит обсудить как осознанную развилку. **C — отвергнут.**
Развилка значима → **ADR-002** (совместно с Q3-путями). Финальный выбор за architect/security.

---

## Q1 (macOS). Под кем гонять launchd-сервис → ADR-002

### Найдено (факт → источник)
- **Системный daemon vs user agent:** «System-wide daemons reside in `/Library/LaunchDaemons`.
  Per-user agents use `~/Library/LaunchAgents`.»
  → https://keith.github.io/xcode-man-pages/launchd.plist.5.html
- **`UserName` / `GroupName`:** «This optional key specifies the user to run the job as.» **«UserName
  is only applicable for services that are loaded into the privileged system domain.»** (т.е. для
  LaunchDaemons — да; для per-user LaunchAgents — нет). → https://keith.github.io/xcode-man-pages/launchd.plist.5.html
- Apple «Creating Launch Daemons and Agents»: daemons в `/Library/LaunchDaemons` — системные сервисы
  без привязки к пользовательской сессии; агенты — в контексте залогиненного пользователя.
  → https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html
- raxd — фоновой сетевой демон без UI и без привязки к сессии → концептуально это **daemon**, не agent.

### Варианты
- **A: системный daemon в `/Library/LaunchDaemons` + `UserName=<непривилегированный>`** — плюсы:
  стартует при загрузке без логина (как и нужно сетевому демону, AC3); launchd запускается от root
  и через `UserName=` сбрасывает привилегии на указанного пользователя (AC6 «euid != 0»); симметрично
  с Linux `User=`. Минусы: установка в `/Library/LaunchDaemons` требует root/admin (ожидаемо для
  системного сервиса); нужен непривилегированный пользователь (см. ниже).
  → https://keith.github.io/xcode-man-pages/launchd.plist.5.html
- **B: per-user agent в `~/Library/LaunchAgents`** — плюсы: не требует root для установки. Минусы:
  работает только когда пользователь залогинен (нет автозапуска при загрузке системы до логина) →
  **противоречит модели «сервер, стартующий при загрузке»** (AC3); `UserName` неприменим. Не
  подходит для серверного демона. → https://keith.github.io/xcode-man-pages/launchd.plist.5.html

### Создание/выбор сервис-пользователя на macOS (открытый нюанс)
- На macOS нет `useradd`/`sysusers.d`; создание системного пользователя — через `dscl`
  (Directory Services) или запуск под уже существующим непривилегированным аккаунтом. Подтверждённого
  first-party-источника по точной процедуре создания «raxd»-пользователя в рамках этого research я
  **не зафиксировал** → см. Открытые вопросы (для architect/system-dev; интеграция macOS вне Docker,
  AC13). Факт-граница: `UserName=` в LaunchDaemon работает с любым существующим непривилегированным
  пользователем. → https://keith.github.io/xcode-man-pages/launchd.plist.5.html

### Рекомендация
**A: системный daemon (`/Library/LaunchDaemons`) с `UserName=<непривилегированный>`** — единственный
вариант, дающий автозапуск при загрузке (AC3) и не-root исполнение (AC6) для серверного демона; B
(agent) отвергнут как не отвечающий модели сервера. Процедура создания/выбора macOS-пользователя —
открытый вопрос для architect/system-dev (проверяется на реальном macOS вне Docker, AC13). Деталь в
**ADR-002**.

---

## Q3. Пути конфига/состояния для не-root сервиса: системные vs домашние XDG → ADR-002

### Найдено (факт → источник)
- **systemd `StateDirectory=` / `ConfigurationDirectory=` / `LogsDirectory=`** для системного
  сервиса маппятся в: StateDirectory → `/var/lib/`, ConfigurationDirectory → `/etc/`,
  LogsDirectory → `/var/log/` (т.е. `StateDirectory=raxd` → `/var/lib/raxd`). Каталоги создаются
  systemd с владельцем = `User=`/`DynamicUser`, **до старта** процесса.
  → https://man7.org/linux/man-pages/man5/systemd.exec.5.html
- **Права создаваемых каталогов:** `RuntimeDirectoryMode=`, `StateDirectoryMode=`,
  `CacheDirectoryMode=`, `LogsDirectoryMode=`, `ConfigurationDirectoryMode=` — «Defaults to **0755**».
  → https://man7.org/linux/man-pages/man5/systemd.exec.5.html
  (Замечание для security: baseline требует `0700` для каталогов состояния/`0600` для keys.db —
  значит `StateDirectoryMode=0700` надо задать явно, дефолт 0755 шире, чем baseline.)
- **Текущий резолв путей raxd** — XDG через переменные окружения (`internal/config/paths.go`):
  `ConfigDir = $XDG_CONFIG_HOME/raxd` иначе `$HOME/.config/raxd`; `StateDir = $XDG_STATE_HOME/raxd`
  иначе `$HOME/.local/state/raxd`. То есть пути **полностью управляются env-переменными
  `XDG_CONFIG_HOME`/`XDG_STATE_HOME`** (если `$HOME` не задан — ошибка). → прочитан
  `internal/config/paths.go`
- **systemd может задать env сервису:** `Environment=` / `EnvironmentFile=` в unit (systemd.exec(5))
  → можно выставить `XDG_STATE_HOME=/var/lib`, `XDG_CONFIG_HOME=/etc` (тогда raxd сам разрешит
  `/var/lib/raxd`, `/etc/raxd` своим существующим кодом — без правки резолва).
  → https://man7.org/linux/man-pages/man5/systemd.exec.5.html
- **macOS file-system layout:** для сторонних демонов конфиг обычно `/usr/local/etc/<app>`, данные —
  `/usr/local/var/<app>` или `/Library/Application Support/<app>`; домашних `~/.config` у системного
  daemon нет. (Apple File System Programming Guide — общая раскладка; точные дефолты для нашего
  пользователя — решение architect.) → https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html
- launchd `StandardErrorPath`/`StandardOutPath` — путь файла для stderr/stdout демона: «Files are
  created if missing, reflecting specified user/group ownership and umask permissions.»
  → https://keith.github.io/xcode-man-pages/launchd.plist.5.html

### Варианты (как состыковать системные пути с XDG-резолвингом raxd)
- **A: задать сервису `Environment=XDG_CONFIG_HOME=/etc` и `XDG_STATE_HOME=/var/lib` (Linux) /
  аналогичные env в plist (macOS), плюс `StateDirectory=raxd` (+`StateDirectoryMode=0700`)** — плюсы:
  **НЕ требует правки кода резолва** — raxd сам разрешит `/etc/raxd`, `/var/lib/raxd`; systemd создаёт
  `/var/lib/raxd` с владельцем-сервисом и нужным режимом; путь предсказуем. Минусы: на macOS нет
  `StateDirectory`-аналога → каталоги/права создаём при install сами (mkdir+chown+chmod); надо
  выставить `StateDirectoryMode=0700` (дефолт 0755 шире baseline). → systemd.exec(5),
  `internal/config/paths.go`
- **B: захардкодить системные пути в коде при детекте «сервисного» режима** (напр. `/var/lib/raxd`,
  `/etc/raxd`) — плюсы: явный контроль. Минусы: дублирует/ветвит логику резолва, расходится с уже
  работающим XDG-механизмом; больше кода и тестов. Хуже A. → `internal/config/paths.go`
- **C: оставить домашние `~/.config`/`~/.local/state` сервис-пользователя** — плюсы: ничего не
  менять. Минусы: у системного пользователя `raxd` обычно нет «нормального» `$HOME`
  (или он `/var/lib/raxd`/`/nonexistent`); `os.UserHomeDir()` может вернуть пустое/ошибку → текущий
  код вернёт ошибку «$HOME is not set». Хрупко и не по FHS. Отвергнут. → `internal/config/paths.go`,
  systemd.exec(5)

### Рекомендация
**A: переиспользовать существующий XDG-резолвинг, задав сервису `XDG_CONFIG_HOME`/`XDG_STATE_HOME`
через `Environment=` (unit) / `EnvironmentVariables` (plist), плюс `StateDirectory=raxd` с
`StateDirectoryMode=0700` на Linux.** Это даёт системные пути (`/etc/raxd`, `/var/lib/raxd`) без
правки кода резолва, с владельцем-сервисом и baseline-правами. macOS — каталоги/права при install
создаём явно (нет StateDirectory). Числовые дефолты путей macOS (`/usr/local/etc`,
`/usr/local/var` vs `/Library/Application Support`) — за architect (Открытый вопрос). Деталь в
**ADR-002**. Подчеркнуть security: дефолт `StateDirectoryMode` = 0755 шире baseline 0700 → задать
явно.

---

## Q4. Привилегированный порт <1024 без root: `AmbientCapabilities` vs `setcap` → ADR-003

> Контекст: дефолт raxd — порт **7822** (НЕ привилегированный), root не нужен (AC6). Этот вопрос
> релевантен ТОЛЬКО когда оператор вручную меняет порт на <1024 (AC7).

### Найдено (факт → источник)
- **`CAP_NET_BIND_SERVICE`:** «Bind a socket to Internet domain privileged ports (port numbers less
  than 1024).» → https://man7.org/linux/man-pages/man7/capabilities.7.html
- **systemd `AmbientCapabilities=` (дословно):** «Controls which capabilities to include in the
  ambient capability set… **Ambient capability sets are useful if you want to execute a process as a
  non-privileged user but still want to give it some capabilities. Note that, in this case, option
  keep-caps is automatically added to SecureBits= to retain the capabilities over the user change.**»
  → https://man7.org/linux/man-pages/man5/systemd.exec.5.html
- **Ambient set переживает execve у непривилегированного процесса (дословно):** ambient — «a set of
  capabilities that are preserved across an execve(2) of a program that is not privileged».
  → https://man7.org/linux/man-pages/man7/capabilities.7.html
- **Вывод (НЕ дословная цитата, а интерпретация фактов выше):** AmbientCapabilities — штатный
  systemd-механизм дать capability процессу, исполняемому от непривилегированного `User=` (через
  auto `keep-caps`), и ambient-капы по определению переживают execve непривилегированного процесса.
  Это и есть основание выбрать ambient вместо file-caps. ОДНАКО **дословной фразы «AmbientCapabilities
  работает совместно с NoNewPrivileges=yes»** в systemd.exec(5)/capabilities(7) в рамках этого
  research я **не зафиксировал** → вынесено в Открытые вопросы (для architect/security при
  утверждении unit; на саму рекомендацию не влияет — конфликта с ambient нет, в отличие от file-caps,
  см. ниже). При необходимости сузить порт — `SocketBindAllow=`/`SocketBindDeny=` (директивы
  systemd.exec(5)). → https://man7.org/linux/man-pages/man5/systemd.exec.5.html
- **`setcap` file-capabilities:** «Since Linux 2.6.24, the kernel supports associating capability
  sets with an executable file using setcap(8)»; capabilities хранятся в **extended attribute**
  `security.capability` на самом файле; синтаксис `cap_net_bind_service=+ep` (permitted+effective).
  → https://man7.org/linux/man-pages/man7/capabilities.7.html ,
  https://man7.org/linux/man-pages/man8/setcap.8.html
- **Подводные камни file-caps:** (1) хранятся в xattr файла → **теряются при замене/перекомпиляции/
  копировании бинаря без сохранения xattr** (важно при обновлении raxd — задача distribution) и не
  работают на ФС без поддержки xattr; (2) «executing a program that has any file capabilities set
  will clear the ambient set» (дословно из capabilities(7)); (3) известный нюанс: `NoNewPrivileges=yes`
  при execve блокирует получение **новых** привилегий от file-capabilities (setcap-капы не
  повышаются под NoNewPrivileges) — но дословной фразы про file-caps под NoNewPrivileges в
  capabilities(7) я не зафиксировал (см. Открытые вопросы). В отличие от file-caps, AmbientCapabilities
  проставляются менеджером systemd. → https://man7.org/linux/man-pages/man7/capabilities.7.html
- **macOS:** launchd-демон стартует от root и сбрасывает привилегии (`UserName=`); привязка к порту
  <1024 происходит до сброса (root биндит) либо через socket activation (`Sockets` ключ в plist,
  launchd открывает сокет и передаёт fd). Прямого «capability»-механизма как в Linux на macOS нет.
  Подтверждённого first-party-описания точной механики «root биндит <1024, затем UserName» в рамках
  этого research нет → Открытый вопрос (macOS вне Docker, AC13). → https://keith.github.io/xcode-man-pages/launchd.plist.5.html
- baseline §3: «при необходимости порта <1024 — Linux capabilities (CAP_NET_BIND_SERVICE), а не
  setuid root». → `.claude/reference/SECURITY-BASELINE.ru.md` §3

### Варианты (Linux)
- **A: systemd `AmbientCapabilities=CAP_NET_BIND_SERVICE` (+ опц. `SocketBindAllow=tcp:<порт>`)** —
  плюсы: capability выдаётся **менеджером** при старте, не зависит от xattr бинаря (переживает
  обновления raxd); штатный механизм дать cap непривилегированному `User=`/`DynamicUser` (auto
  `keep-caps`); декларативно в unit; можно сузить до конкретного порта. Минусы: только systemd (для
  других init нет; у нас целевой — systemd, AC1); явная совместимость с `NoNewPrivileges=yes` —
  Открытый вопрос (см. выше). → https://man7.org/linux/man-pages/man5/systemd.exec.5.html
- **B: `setcap cap_net_bind_service=+ep` на бинаре при install** — плюсы: работает вне systemd
  (любой запуск бинаря). Минусы: **теряется при замене бинаря** (обновление → надо повторять
  setcap), не работает на ФС без xattr, чистит ambient set («executing a program that has any file
  capabilities set will clear the ambient set»), вероятный конфликт с `NoNewPrivileges=yes` (новые
  привилегии при execve блокируются — нюанс к верификации); требует root при install для setcap.
  Хуже A для systemd-сервиса. → https://man7.org/linux/man-pages/man8/setcap.8.html ,
  https://man7.org/linux/man-pages/man7/capabilities.7.html
- **C: НЕ выдавать capability вовсе (только порты ≥1024)** — плюсы: ноль привилегий, максимально
  просто; покрывает дефолт 7822. Минусы: не выполняет AC7 (когда оператор хочет <1024). Подходит как
  поведение по умолчанию, но AC7 требует механизм для <1024. → spec AC7

### Рекомендация
**A: `AmbientCapabilities=CAP_NET_BIND_SERVICE` (systemd), применяемый ТОЛЬКО когда настроенный порт
<1024**; для дефолта 7822 и любых портов ≥1024 — никаких capability (вариант C как поведение по
умолчанию). A предпочтительнее B, потому что не зависит от xattr бинаря (переживает обновления),
является штатным systemd-способом дать cap непривилегированному пользователю и сужается через
`SocketBindAllow=`; file-caps (B) чистят ambient set и теряются при замене бинаря. Явную совместимость
ambient с `NoNewPrivileges=yes` верифицирует architect/security при утверждении unit (Открытый
вопрос). macOS-механика (root биндит / socket activation) — Открытый вопрос (вне Docker, AC13).
Развилка значима → **ADR-003**.

---

## Q5. Ротация/ограничение роста аудит-журнала → ADR-004

> Контекст: raxd сейчас пишет аудит в **stderr** строгим logfmt через `charmbracelet/log`
> (`internal/cli/serve.go`: `logger.SetFormatter(clog.LogfmtFormatter)`); STACK §Логи: «системный
> журнал (journald/syslog) + ротация при файловом выводе». → прочитан `internal/cli/serve.go`, STACK

### Найдено (факт → источник)
- **journald автоматически ограничивает рост (Linux):** при systemd-сервисе stderr демона попадает
  в journald (если `StandardError=journal`/дефолт). `SystemMaxUse=` / `RuntimeMaxUse=` — «how much
  disk space the journal may use up at most»; дефолты «10% и 15% размера ФС, capped to 4G»; единицы
  K/M/G/T. → https://man7.org/linux/man-pages/man5/journald.conf.5.html
- **journald: ротация синхронная, не нужен внешний триггер:** «size limits are enforced
  synchronously when journal files are extended, and no explicit rotation step triggered by time is
  needed»; vacuuming удаляет старейшие archived-файлы. `SystemMaxFileSize=` — размер отдельного
  файла (дефолт 1/8 от SystemMaxUse, capped 128M); `MaxRetentionSec=`/`MaxFileSec=` — по времени.
  → https://man7.org/linux/man-pages/man5/journald.conf.5.html
- **Storage=:** auto/persistent (`/var/log/journal`)/volatile (`/run/log/journal`)/none.
  → https://man7.org/linux/man-pages/man5/journald.conf.5.html
- **Нюанс: размерные лимиты journald — глобальные (per-host), НЕ per-unit:** per-unit в systemd.exec
  настраивается только rate-limit (`LogRateLimitIntervalSec=`/`LogRateLimitBurst=`), а не объём
  хранения. → https://man7.org/linux/man-pages/man5/journald.conf.5.html
- **logrotate (альтернатива для файлового вывода):** классический механизм ротации файловых логов
  по размеру/времени (`size`, `rotate`, `compress`, `maxsize`) — нужен, если raxd пишет аудит в
  собственный файл, а не в journald. → https://man7.org/linux/man-pages/man8/logrotate.8.html
- **macOS:** launchd `StandardErrorPath` пишет stderr демона в указанный файл; встроенной ротации
  per-file у launchd нет — для macOS-файлового вывода ротация делается через `newsyslog`
  (системный аналог logrotate в macOS) или unified logging. Подтверждённого first-party URL по
  newsyslog в рамках research не зафиксировал → Открытый вопрос (macOS вне Docker, AC13).
  → https://keith.github.io/xcode-man-pages/launchd.plist.5.html

### Варианты
- **A (Linux): оставить аудит в stderr → journald, ограничить `SystemMaxUse=`/`SystemMaxFileSize=`
  (или per-host политика)** — плюсы: **минимально-инвазивно** (raxd уже пишет в stderr; ничего не
  менять в коде); ротация/vacuum автоматические и синхронные; интеграция с `journalctl`. Минусы:
  размерные лимиты journald **глобальные** (влияют на весь хост, не только raxd) → для теста AC8
  «заниженный порог» придётся менять host-wide `SystemMaxUse` (в контейнере допустимо); если нужен
  именно per-raxd лимит — journald его не даёт. → https://man7.org/linux/man-pages/man5/journald.conf.5.html
- **B (Linux): аудит raxd в собственный файл (`LogsDirectory=raxd` → `/var/log/raxd`) + logrotate**
  — плюсы: per-raxd лимит (изолированная политика размера/срока), не трогает host journald. Минусы:
  требует, чтобы raxd писал в файл (сейчас stderr) — либо `StandardError=append:/var/log/raxd/...`
  в unit (перенаправление без правки кода), либо правка вывода; нужен файл конфигурации logrotate +
  его установка/дерегистрация (доп. артефакт, влияет на AC10 «без осиротевших артефактов»).
  → https://man7.org/linux/man-pages/man8/logrotate.8.html
- **C: гибрид — journald по умолчанию, но задокументировать logrotate как опцию для файлового
  вывода** — плюсы: дефолт минимально-инвазивен (A), а оператор может выбрать файловый вывод+ротацию
  (B). Минусы: две поддерживаемые конфигурации (больше документации/тестов). → обе ссылки выше

### Рекомендация
**A как минимально-инвазивный дефолт на Linux** (аудит в stderr → journald, ограничение через
`SystemMaxUse=`/`SystemMaxFileSize=`): raxd уже пишет в stderr, ротация/vacuum journald автоматические
— это закрывает AC8 наименьшими изменениями. Тест AC8 (заниженный порог + наполнение + проверка
ограниченности) воспроизводим в контейнере через занижение `SystemMaxUse=`/`SystemMaxFileSize=` и
`journalctl --vacuum-size`. Зафиксировать ограничение для architect: размерные лимиты journald —
глобальные (per-host, не per-unit) → если security захочет именно per-raxd лимит, это вариант B
(`LogsDirectory` + logrotate). macOS-ротация (newsyslog/unified logging) — Открытый вопрос (AC13).
Развилка journald vs logrotate значима → **ADR-004**.

---

## Q6. Формат/расположение unit/plist; минимально-безопасный набор директив

### Найдено (факт → источник)
- **systemd unit для системного сервиса** размещается в `/etc/systemd/system/<name>.service`
  (админ-уровень) — стандартное расположение для сервисов, устанавливаемых локально/пакетом.
  → https://man7.org/linux/man-pages/man5/systemd.service.5.html (и systemd.unit(5))
- **Ключевые директивы (факты по каждой):**
  - `Type=exec` — «the manager will consider the unit started immediately after the main service
    binary has been executed»; рекомендован для не-форкающего foreground-процесса (наш `raxd serve`),
    «ensures that process setup errors … are properly tracked». → https://man7.org/linux/man-pages/man5/systemd.service.5.html
  - `ExecStart=` — путь к бинарю + `serve` (раскладка бинаря — spec Q2, стык с distribution).
  - `Restart=on-failure` + `RestartSec=` — см. Q7. → https://man7.org/linux/man-pages/man5/systemd.service.5.html
  - `User=`/`DynamicUser=` — Q1; `StateDirectory=raxd` + `StateDirectoryMode=0700` — Q3.
  - `AmbientCapabilities=CAP_NET_BIND_SERVICE` — Q4 (только при порту <1024).
  - hardening: `NoNewPrivileges=yes` (implicit при DynamicUser); `ProtectSystem=strict`
    («the entire file system hierarchy is mounted read-only, except for /dev, /proc, /sys»);
    `ProtectHome=yes` («/home, /root, /run/user … inaccessible and empty»); `PrivateTmp=yes`.
    → https://man7.org/linux/man-pages/man5/systemd.exec.5.html
- **launchd plist для системного daemon** размещается в `/Library/LaunchDaemons/<label>.plist`.
  Ключи: `Label` («required key uniquely identifies the job»), `ProgramArguments` (argv: путь+`serve`),
  `RunAtLoad` (запуск при загрузке/load — AC3), `KeepAlive` (рестарт — Q7), `UserName` (не-root —
  Q1), `StandardErrorPath`/`StandardOutPath` (вывод — Q5). → https://keith.github.io/xcode-man-pages/launchd.plist.5.html
- **Права на файл описания:** unit/plist — текстовые файлы; чтобы непривилегированный пользователь не
  мог их подменить, владелец root и режим `0644` (читаемы, не записываемы пользователем) —
  общепринятая практика; точные права — деталь architect/security (нет отдельной обязывающей цитаты,
  фиксирую как практику, не как факт-цитату). Имя сервиса/label (`raxd` / `tech.oem.raxd` или
  подобный reverse-DNS для launchd) — деталь architect (spec Q6).

### Варианты
- **A: минимально-безопасный набор** — systemd: `Type=exec`, `ExecStart=…/raxd serve`,
  `Restart=on-failure`, `RestartSec=…`, `User=raxd` (или `DynamicUser=yes`), `StateDirectory=raxd`,
  `StateDirectoryMode=0700`, `NoNewPrivileges=yes`, `ProtectSystem=strict`, `ProtectHome=yes`,
  `PrivateTmp=yes`, (+`AmbientCapabilities=CAP_NET_BIND_SERVICE` только при <1024). launchd:
  `Label`, `ProgramArguments=[…/raxd, serve]`, `RunAtLoad=true`, `KeepAlive={SuccessfulExit=false}`
  (Q7), `UserName=raxd`, `StandardErrorPath=…`. → systemd.exec(5)/systemd.service(5),
  launchd.plist(5)
- **B: расширенный hardening** (доп. `ProtectKernelTunables=`, `ProtectControlGroups=`,
  `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX`, `RestrictNamespaces=`, `MemoryDenyWriteExecute=`,
  `SystemCallFilter=@system-service`) — плюсы: глубже сокращает поверхность атаки (raxd исполняет
  команды — оправдано). Минусы: `SystemCallFilter`/`RestrictAddressFamilies` могут сломать
  выполнение произвольных команд (exec) — нужна аккуратная настройка/тест; больше риска регрессий.
  → https://man7.org/linux/man-pages/man5/systemd.exec.5.html
- **C: минимум без hardening** (`Type=exec`/`Restart`/`User` и всё) — плюсы: проще. Минусы: не
  использует доступную «бесплатную» защиту (`ProtectSystem`/`ProtectHome`/`PrivateTmp`) → противоречит
  духу baseline. Хуже A. → `.claude/reference/SECURITY-BASELINE.ru.md`

### Рекомендация
**A (минимально-безопасный набор)** как база — даёт корректную регистрацию (AC2), не-root (AC6),
restart-семантику (Q7/AC4-AC5) и «дешёвый» hardening (`ProtectSystem`/`ProtectHome`/`PrivateTmp`/
`NoNewPrivileges`) без риска для exec-функциональности. **B (расширенный hardening)** — желательное
усиление, НО `SystemCallFilter`/`RestrictAddressFamilies` нужно проверять против exec-инструмента
(может ломать произвольные команды) → решение и набор за security/architect, с тестом. Расположение:
Linux `/etc/systemd/system/raxd.service`, macOS `/Library/LaunchDaemons/<label>.plist`; права файла
описания (владелец root, не записываем пользователем) и точный label — за architect/security (spec Q6).

---

## Q7. Restart-семантика: «перезапуск при сбое, НЕ при штатной остановке» (AC4/AC5)

### Найдено (факт → источник)
- **systemd `Restart=on-failure` (дословно):** «If set to on-failure, the service will be restarted
  when the process exits with a non-zero exit code, is terminated by a signal (including on core
  dump, but **excluding the aforementioned four signals**), when an operation (such as service
  reload) times out, and when the configured watchdog timeout is triggered.»
  → https://man7.org/linux/man-pages/man5/systemd.service.5.html
- **Определение «clean exit» (дословно из того же раздела systemd.service(5)):** «In this context, a
  clean exit means any of the following: • exit code of 0; • for types other than Type=oneshot, one
  of the signals **SIGHUP, SIGINT, SIGTERM, or SIGPIPE**; • exit statuses and signals specified in
  SuccessExitStatus=.» Эти четыре сигнала и есть «aforementioned four signals», исключаемые из
  триггеров рестарта при `Restart=on-failure`. → https://man7.org/linux/man-pages/man5/systemd.service.5.html
- **Следствие (вывод из цитат выше):** `systemctl stop` шлёт **SIGTERM** → raxd делает graceful
  shutdown (уже реализован: `signal.NotifyContext(…, os.Interrupt, syscall.SIGTERM)` в
  `internal/cli/serve.go`) → SIGTERM входит в clean-exit → systemd НЕ перезапускает (ровно AC5).
  А `kill -KILL` (SIGKILL — НЕ в списке clean-exit), паника или ненулевой код → рестарт (AC4).
  → https://man7.org/linux/man-pages/man5/systemd.service.5.html , прочитан `internal/cli/serve.go`
- **`RestartSec=`:** «Configures the time to sleep before restarting a service… Defaults to 100ms.»
  → https://man7.org/linux/man-pages/man5/systemd.service.5.html
- **launchd `KeepAlive` (булев):** «unconditionally keep the job alive» — рестартит ВСЕГДА (в т.ч.
  после штатной остановки) → НЕ подходит для AC5. → https://keith.github.io/xcode-man-pages/launchd.plist.5.html
- **launchd `KeepAlive`-словарь с `SuccessfulExit`:** «If true, the job will be restarted as long as
  the program exits and with an exit status of zero. **If false, the job will be restarted in the
  inverse condition**» (т.е. рестарт только при НЕнулевом коде = сбой). → https://keith.github.io/xcode-man-pages/launchd.plist.5.html
- Следствие macOS: `KeepAlive={SuccessfulExit=false}` → рестарт только при ненулевом выходе (сбой,
  AC4); при graceful shutdown с кодом 0 — НЕ рестартит (AC5). raxd `serve` завершается без ошибки
  (return nil) при сигнале → код 0 → не рестартит. → прочитан `internal/cli/serve.go`

### Варианты
- **A: systemd `Restart=on-failure`+`RestartSec=<N>`; launchd `KeepAlive={SuccessfulExit=false}`** —
  плюсы: ровно AC4 (рестарт при сбое: ненулевой код/kill -9/паника) + AC5 (нет рестарта при штатной
  остановке: SIGTERM-graceful → clean-exit). Минусы: нет (это и есть штатный способ). → обе ссылки выше
- **B: systemd `Restart=always`; launchd `KeepAlive=true`** — плюсы: проще. Минусы: рестартит и
  после штатной остановки → **нарушает AC5** («остановленный сервис НЕ перезапускается»). Отвергнут.
  → systemd.service(5), launchd.plist(5)

### Рекомендация
**A** — единственный вариант, удовлетворяющий и AC4, и AC5 на обеих платформах: systemd
`Restart=on-failure` (SIGTERM/SIGINT/SIGHUP/SIGPIPE входят в clean-exit и исключены из триггеров →
graceful stop по SIGTERM не рестартит) + `RestartSec=<подбирается architect>`; launchd
`KeepAlive={SuccessfulExit=false}` (рестарт только при ненулевом выходе). Согласуется с уже
реализованным graceful shutdown raxd (return nil → код 0 при сигнале). Это НЕ требует кода — только
корректные директивы. (Этот пункт встроен в ADR-001/ADR по шаблонам — отдельный ADR не обязателен,
факт однозначен и подтверждён дословной цитатой.)

---

## Q8. Кросс-сборка Go: матрица, CGO, критерий «рабочести» (AC14/AC15)

### Найдено (факт → источник)
- **GOOS/GOARCH:** «$GOARCH and $GOOS represent the target architecture and operating system»;
  допустимые включают `darwin`, `linux` (GOOS) и `amd64`, `arm64` (GOARCH). Кросс-сборка чистого Go
  с Go 1.5+ тривиальна: `GOOS=linux GOARCH=amd64 go build`, `GOOS=darwin GOARCH=arm64 go build`.
  → https://go.dev/doc/install/source
- **Список валидных пар:** `go tool dist list` «displays all the supported operating system and
  architecture combinations» в формате `GOOS/GOARCH` (включая darwin/linux × amd64/arm64).
  → https://pkg.go.dev/internal/platform , https://go.dev/doc/install/source
- **`CGO_ENABLED=0`:** сборка без cgo — «useful for pure Go cross-compilation without C
  dependencies»; «cgo is disabled when cross-compiling, so any file that mentions import \"C\" will
  be silently ignored»; для cgo при кросс-сборке нужен C-кросс-компилятор (`CC`/`CC_FOR_TARGET`).
  Раз raxd чисто Go и релиз `CGO_ENABLED=0` (STACK) → C-тулчейн не нужен, кросс-сборка под все 4 цели
  чистым `go build`. → https://pkg.go.dev/cmd/cgo , https://go.dev/wiki/WindowsCrossCompiling
- **Проверка GOOS/GOARCH собранного бинаря:** `go version <binary>` сообщает **только версию Go**
  («reports the Go version used to build each of the named files»), **НЕ** GOOS/GOARCH. → значит
  для проверки целевой архитектуры использовать системную утилиту `file` (ELF vs Mach-O, arch) или
  `go version -m` (модульная инфа, не таргет). → https://pkg.go.dev/cmd/go
- **`-race` требует CGO (дословно):** «The race detector requires cgo to be enabled, and on
  non-Darwin systems requires an installed C compiler.» → для релизных `CGO_ENABLED=0`-бинарей
  `-race` неприменим; race-тесты гоняются отдельно (нативно, с CGO). Не влияет на AC14 (релиз без
  cgo). → https://go.dev/doc/articles/race_detector
- baseline §6: запуск/тесты только в Docker → нативно исполняется лишь бинарь под архитектуру
  контейнера (linux/amd64 или linux/arm64); darwin-бинари и не-нативный linux-arch в Docker НЕ
  запускаются (эмуляция вне scope). → `.claude/reference/SECURITY-BASELINE.ru.md` §6, spec Q8/AC14

### Варианты (критерий «рабочести» cross-target бинаря — spec Q8)
- **A: компиляция под все 4 цели + запуск ТОЛЬКО нативного для контейнера (`raxd version`) +
  `file`/арх-проверка для остальных 3** — плюсы: ровно AC14 («собирается под цель; нативный
  исполняется и проходит базовую проверку»); не требует эмуляции; воспроизводимо в одном контейнере;
  `file` подтверждает корректный таргет (ELF/Mach-O + arch). Минусы: не-нативные бинари проверяются
  лишь по факту компиляции+формата (а не запуском) — но это и есть планка AC14. → spec AC14,
  https://pkg.go.dev/cmd/go
- **B: + QEMU-эмуляция для запуска не-нативных linux-бинарей** (`binfmt_misc`/`docker buildx
  --platform`) — плюсы: реальный запуск arm64-под-amd64. Минусы: darwin всё равно не запустить в
  Docker (AC13); эмуляция — доп. инфраструктура и флейки; AC14 этого НЕ требует («cross-target —
  проверка факта компиляции»). Избыточно для v1. → spec AC14
- **C: только компиляция, без запуска даже нативного** — плюсы: проще всего. Минусы: AC14 требует,
  чтобы нативный бинарь «запускался и отвечал на базовую проверку» → C не выполняет AC14. Отвергнут.
  → spec AC14

### Рекомендация
**A**: матрица `GOOS={linux,darwin}×GOARCH={amd64,arm64}` чистым `go build` с `CGO_ENABLED=0`
(C-тулчейн не нужен, raxd чисто Go); приёмка — все 4 артефакта компилируются без ошибок + нативный
для контейнера (`linux/<arch>`) исполняется и проходит `raxd version` (базовая неразрушающая
проверка), остальные 3 проверяются по факту успешной компиляции и формату через `file` (т.к.
`go version <bin>` НЕ даёт GOOS/GOARCH). Это ровно планка AC14 без эмуляции. Сборка офлайн из
`vendor/` (`-mod=vendor`, AC15). Эмуляция (B) избыточна. Отдельный ADR не обязателен (факт-критерий
однозначен); architect лишь утверждает «компиляция + `file`» как приёмку cross-target (spec Q8).

---

## Q9. Воспроизводимый рецепт systemd-в-Docker для сервисной интеграции (AC16, baseline §6)

### Найдено (факт → источник)
- **systemd в контейнере требует:** privileged-режим (или как минимум `CAP_SYS_ADMIN` для cgroup),
  монтирование cgroup (`-v /sys/fs/cgroup:/sys/fs/cgroup:ro`) и образ с `/sbin/init` (systemd как
  PID 1). Готовый образ-кандидат: `jrei/systemd-ubuntu` («can be used as a base container to run
  systemd services inside»); пример:
  `docker run -d --name systemd-test --privileged -v /sys/fs/cgroup:/sys/fs/cgroup:ro jrei/systemd-ubuntu:22.04`.
  → https://hub.docker.com/r/jrei/systemd-ubuntu
- Альтернативный поддерживаемый образ для тестов с systemd: `robertdebock/docker-ubuntu-systemd`
  («Container to test … including capabilities to use systemd facilities»).
  → https://github.com/robertdebock/docker-ubuntu-systemd
- **Безопасность/нюанс:** «Running privileged containers is not recommended for production»; как
  альтернатива — точечно `--cap-add` (минимум `CAP_SYS_ADMIN` для cgroup). На cgroup v2 (современные
  хосты) монтирование часто `:rw` или специфичное; точная команда зависит от версии cgroup на
  хост-CI. → https://hub.docker.com/r/jrei/systemd-ubuntu
- baseline §6: «тесты сервисной интеграции (systemd) — в контейнере с systemd; на хост-машину
  разработчика сервис не ставится». → `.claude/reference/SECURITY-BASELINE.ru.md` §6

### Рецепт-кандидат (для architect/qa/devops; финальный — за ними)
1. Базовый образ с systemd как PID 1 (`jrei/systemd-ubuntu:22.04` или собственный Dockerfile с
   установленным systemd и `ENTRYPOINT ["/sbin/init"]`).
2. Запуск: `--privileged` (или `--cap-add SYS_ADMIN`) + `-v /sys/fs/cgroup:/sys/fs/cgroup:ro`
   (для cgroup v2 на современном CI может потребоваться иная mount-опция — проверить на целевом CI).
3. Скопировать собранный `raxd` (нативный для арх. контейнера) внутрь; прогнать сценарий:
   `raxd <install>` → `systemctl status raxd` (AC3 — enabled/active) → проверить euid != 0 (AC6,
   напр. `systemctl show -p MainPID` + `cat /proc/<pid>/status | grep Uid`) → `kill -9 <pid>` →
   подождать `RestartSec` → снова `status`+смена PID (AC4) → `raxd <stop>` → graceful, остаётся
   stopped, не поднимается (AC5) → `raxd <uninstall>` → нет артефактов (AC10) → повторный install
   идемпотентен (AC9).
4. AC8 (ротация): занизить `SystemMaxUse=`/`SystemMaxFileSize=` в `journald.conf`, наполнить журнал
   синтетикой, проверить ограниченность роста (`journalctl --disk-usage` / `--vacuum-size`).

### Варианты
- **A: готовый `jrei/systemd-ubuntu` + privileged + cgroup-mount** — плюсы: быстрый старт,
  документирован, проверенный паттерн; минимум своей инфраструктуры. Минусы: privileged
  (приемлемо для CI-теста, не для прода — это лишь среда проверки baseline §6); внешний образ (но
  тянется один раз; для офлайн-CI его надо предзагрузить/закэшировать — стык с AC15/вендорингом
  образов). → https://hub.docker.com/r/jrei/systemd-ubuntu
- **B: собственный Dockerfile (debian/ubuntu + systemd + `/sbin/init`)** — плюсы: контроль над
  содержимым, легче офлайн/закэшировать, без зависимости от стороннего образа. Минусы: больше своей
  работы (настроить systemd как PID 1, маскировать ненужные юниты). → https://github.com/robertdebock/docker-ubuntu-systemd
  (как референс конфигурации)

### Рекомендация
Для скорости и воспроизводимости — **A (`jrei/systemd-ubuntu` или эквивалент) с `--privileged` +
cgroup-mount** как кандидат, ИЛИ **B (свой Dockerfile с systemd)** если важен офлайн-контроль образа
(стык с baseline §6/AC15: образ для CI желательно закэшировать). Точные mount-опции под cgroup v2 —
проверить на целевом CI (Открытый вопрос — зависит от хост-окружения). Сам сценарий
install→status→kill→restart→stop→uninstall + AC8-проверка журнала — выше; финальный рецепт и числа
порогов — за architect/qa/devops. macOS-интеграция в Docker НЕвозможна (AC13) → только unit-тесты
генератора plist + ручной/CI-прогон на реальном macOS.

---

## Сводка рекомендаций для architect

| # | Вопрос (AC) | Рекомендация research | Источник-ключ | Новая зависимость? |
|---|---|---|---|---|
| Q0 | Библиотека vs ручная генерация | **ADR-001**: kardianos/service жив (v1.2.4 2025), но его шаблоны НЕ покрывают наши hardening/cap/DynamicUser → текст unit/plist пишем сами в любом случае; склон к B (ручная генерация + native менеджеры) / C (гибрид). В go.mod его нет. | github.com/kardianos/service, pkg.go.dev | A/C — да (вендоринг); B — нет |
| Q1-lin | Сервис-пользователь Linux (AC6) | **ADR-002**: `User=raxd` (статический, sysusers.d/useradd) для симметрии с macOS и стабильного UID; DynamicUser — Linux-only альтернатива с лучшей изоляцией | systemd.exec(5), sysusers.d(5), useradd(8) | Нет |
| Q1-mac | launchd-пользователь (AC6) | **ADR-002**: системный daemon `/Library/LaunchDaemons` + `UserName=` (agent не даёт автозапуск при загрузке) | launchd.plist(5), Apple BPSystemStartup | Нет |
| Q3 | Пути не-root сервиса (AC6/AC8) | **ADR-002**: переиспользовать XDG-резолв, задав `XDG_CONFIG_HOME=/etc`/`XDG_STATE_HOME=/var/lib` через `Environment=` + `StateDirectory=raxd` `StateDirectoryMode=0700` (дефолт 0755 шире baseline!) | systemd.exec(5), paths.go (прочитан) | Нет |
| Q4 | Порт <1024 (AC7) | **ADR-003**: `AmbientCapabilities=CAP_NET_BIND_SERVICE` ТОЛЬКО при <1024 (переживает обновления, штатный systemd-механизм для непривил. User); setcap хуже (xattr теряется, чистит ambient set); дефолт 7822 → без cap | capabilities(7), systemd.exec(5), setcap(8) | Нет |
| Q5 | Ротация журнала (AC8) | **ADR-004**: дефолт — stderr→journald + `SystemMaxUse=`/`SystemMaxFileSize=` (минимально-инвазивно, raxd уже пишет в stderr); per-raxd лимит = logrotate (вариант B). Лимиты journald глобальные! | journald.conf(5), logrotate(8), serve.go (прочитан) | Нет |
| Q6 | Формат/директивы (AC2) | Минимально-безопасный набор: Type=exec, Restart=on-failure, User/StateDirectory, NoNewPrivileges/ProtectSystem/ProtectHome/PrivateTmp; расширенный hardening (SystemCallFilter) проверять против exec | systemd.service(5)/exec(5), launchd.plist(5) | Нет |
| Q7 | Restart-семантика (AC4/AC5) | systemd `Restart=on-failure` (SIGTERM/SIGINT/SIGHUP/SIGPIPE = clean-exit, исключены → graceful stop не рестартит) + `RestartSec`; launchd `KeepAlive={SuccessfulExit=false}` | systemd.service(5), launchd.plist(5), serve.go (прочитан) | Нет |
| Q8 | Кросс-сборка (AC14/AC15) | Матрица darwin/linux×amd64/arm64 `go build CGO_ENABLED=0` (C-тулчейн не нужен; `-race` требует cgo → только для тестов); приёмка = 4 компиляции + нативный `raxd version` + `file` (т.к. `go version <bin>` НЕ даёт GOOS/GOARCH); эмуляция избыточна | go.dev/install/source, cmd/go, race_detector | Нет |
| Q9 | systemd-в-Docker (AC16) | `jrei/systemd-ubuntu` (или свой Dockerfile) + `--privileged` + cgroup-mount; сценарий install→status→kill→restart→stop→uninstall + AC8-журнал; cgroup v2 mount уточнить на CI | hub.docker.com/r/jrei/systemd-ubuntu, baseline §6 | Образ — закэшировать офлайн |

**Подтверждение по зависимостям:** все рекомендации, КРОМЕ варианта A/C по Q0 (kardianos/service),
реализуемы на **stdlib Go 1.25** (`text/template` для unit/plist, `os/exec` для systemctl/launchctl,
`os` для путей/прав/Geteuid) + уже вендоренном `charmbracelet/log` (аудит в stderr → journald). Если
architect выберет A/C по Q0 — это **новая внешняя зависимость** `kardianos/service` (вендоринг +
коммит `vendor/` + правка STACK). Вариант B по Q0 — без новых зависимостей.

**Несоответствие STACK ↔ go.mod (требует решения):** STACK.ru.md называет `kardianos/service`
выбором, но его нет в `go.mod`/`vendor/` и нет использования в коде → ADR-001 должен явно зафиксировать
выбор (взять библиотеку и вендорить ИЛИ ручная генерация) и при необходимости поправить STACK.

**Кросс-платформенная заметка (Linux vs macOS):** systemd-механизмы (DynamicUser, StateDirectory,
AmbientCapabilities, journald, ProtectSystem) — **только Linux**; на macOS аналогов нет → каталоги/
права/ротация на macOS делаются вручную при install (нет StateDirectory) или через newsyslog/unified
logging; capability для <1024 на macOS — через root-бинд/socket activation, не «capability». macOS
интеграция НЕпроверяема в Docker (AC13) → только unit-тесты генератора plist + ручной/CI-прогон на
реальном macOS. Это усиливает аргумент за абстракцию «выделенный пользователь raxd» (статический), а
не Linux-специфичный DynamicUser (асимметрия платформ).

---

## Открытые вопросы

- [ ] **AmbientCapabilities + NoNewPrivileges — явная совместимость.** systemd.exec(5) дословно
  подтверждает, что AmbientCapabilities даёт capability процессу от непривилегированного `User=`
  (через auto `keep-caps`), а capabilities(7) — что ambient-капы переживают execve непривилегированного
  процесса. Но **дословной фразы «AmbientCapabilities работает совместно с `NoNewPrivileges=yes`»**
  в официальных man (systemd.exec(5)/capabilities(7)) в рамках этого research НЕ зафиксировано →
  верифицировать architect/security при утверждении unit. На рекомендацию (выбор ambient, а не
  file-caps) не влияет: file-caps чистят ambient set и теряются при замене бинаря — это уже основание
  предпочесть ambient. → https://man7.org/linux/man-pages/man5/systemd.exec.5.html ,
  https://man7.org/linux/man-pages/man7/capabilities.7.html
- [ ] **NoNewPrivileges vs file-capabilities (setcap)** — дословной фразы в capabilities(7), что
  `NoNewPrivileges=yes` блокирует получение file-caps при execve, не зафиксировал; вывод сделан из
  семантики ambient/file-caps. Не блокирует рекомендацию (выбран AmbientCapabilities, где конфликта
  нет), но при выборе setcap (вариант B) — верифицировать architect/security.
- [ ] **macOS: процедура создания/выбора сервис-пользователя** (`dscl` vs запуск под существующим
  непривилегированным аккаунтом) — first-party-источник по точной процедуре в рамках этого research
  НЕ зафиксирован; macOS-интеграция вне Docker (AC13). Для architect/system-dev (проверка на реальном
  macOS). Факт-граница подтверждена: `UserName=` в LaunchDaemon работает с любым существующим
  непривилегированным пользователем (launchd.plist(5)).
- [ ] **macOS: механика порта <1024** (root биндит до сброса привилегий vs socket activation через
  `Sockets`-ключ plist) — дословного first-party-описания не зафиксировал; для architect/system-dev
  (вне Docker, AC13). Дефолт raxd 7822 делает это нерелевантным по умолчанию.
- [ ] **macOS: ротация файлового лога** (newsyslog vs unified logging для `StandardErrorPath`) —
  first-party URL по newsyslog не зафиксирован; для architect (вне Docker, AC13).
- [ ] **cgroup v2 mount-опции для systemd-в-Docker** — точная команда зависит от версии cgroup на
  целевом CI-хосте; уточнить devops/qa при настройке (паттерн privileged+cgroup-mount подтверждён).
- Делегировано architect/security (НЕ research-вопросы — факт-механизмы подтверждены, открыты
  числа/политики из spec Open Questions):
  - [ ] Имя сервис-пользователя/группы и политика «уже существует» (spec Q1).
  - [ ] Путь установки бинаря (системный vs текущий) и стык с distribution (spec Q2/AC1-AC2).
  - [ ] Числовые дефолты путей macOS (`/usr/local/etc`/`/usr/local/var` vs `/Library/Application
    Support`) (spec Q3).
  - [ ] `RestartSec=` значение и интервал ожидания рестарта в тесте AC4 (spec Q4/AC4).
  - [ ] Пороги журнала (`SystemMaxUse=`/`SystemMaxFileSize=`) и политика срока хранения; per-host vs
    per-raxd (journald vs logrotate) (spec Q5/AC8).
  - [ ] Имя сервиса/label, расположение и точные права файла описания (spec Q6/AC2).
  - [ ] Семантика идемпотентной повторной установки (upgrade-in-place vs ошибка) и критерий «полного»
    удаления (spec Q7/AC9-AC11).
  - [ ] Критерий приёмки cross-target (компиляция+`file` vs эмуляция) — research рекомендует
    компиляцию+`file` (spec Q8/AC14).
  - [ ] Набор расширенного hardening (`SystemCallFilter`/`RestrictAddressFamilies`) и его совместимость
    с exec-инструментом — требует теста (spec, security/architect).
