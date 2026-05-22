# Security Requirements: service-install — регистрация raxd как управляемого системного сервиса (Linux systemd + macOS launchd), не-root

> Каждое требование ПРОВЕРЯЕМО (ответ «выполнено/нет» одним вопросом: тест в Docker / grep / код-ревью /
> unit-тест генератора) и ссылается на пункт `SECURITY-BASELINE.ru.md`, на AC `spec.md`, на контракт
> `plan.md`/ADR и на риск из `threat-model.md`. Эти требования ОБЯЗАНЫ выполнить `system-dev`
> (`internal/service/*` — генератор unit/plist/drop-in, lifecycle, exec-обёртка, создание пользователя/
> каталогов; сервис-файлы), `developer` (`internal/cli/service.go`, интеграция `root.go`, при
> необходимости — валидация в `internal/service`), `devops` (Makefile/Dockerfile.systemd, кросс-сборка,
> CI/контейнер с systemd), `mcp-engineer` (НЕ затрагивается — демон/MCP как есть), `tech-writer`
> (документирование ротации, оставляемого пользователя, macOS-ограничения). Соответствие проверяют
> `reviewer` и `security-guardian`. Способ проверки везде: интеграция гоняется в Docker-контейнере с
> systemd, сборка/unit-тесты — офлайн из `vendor/` (`-mod=vendor`, baseline §6, AC16); запуск демона/
> сервиса — только в контейнере; macOS-интеграция — вне Docker на реальном macOS (AC13, см. SR-95).
>
> **Нумерация.** SR-1…SR-26 — `tls-transport`, SR-27…SR-39 — `mcp-server`, SR-40…SR-67 — `command-exec`,
> SR-68…SR-82 — `file-upload`. Они НАСЛЕДУЮТСЯ (см. «Наследуемые требования») и здесь НЕ дублируются.
> Требования service-install нумеруются СКВОЗНО с **SR-83** по **SR-96** (14 требований, без пропусков).
>
> **Терминология.** «unit» = `/etc/systemd/system/raxd.service` (Linux). «plist» =
> `/Library/LaunchDaemons/tech.oem.raxd.plist` (macOS). «drop-in» = `/etc/systemd/journald.conf.d/raxd.conf`.
> «генератор» = `renderUnit/renderPlist` (`internal/service/templates.go`, чистый рендер `text/template`).
> «менеджер» = `systemctl`/`launchctl`, вызываемый `runManager` (`internal/service/exec.go`). «сервис-
> пользователь» = `raxd:raxd` (статический системный, ADR-002). «состояние» = `StateDir` → `/var/lib/raxd`
> (`keys.db`, `tls/`, `uploads/`, аудит). «привилегированный порт» = TCP-порт <1024.

## Не-root исполнение (baseline §3)

- [ ] **SR-83. Демон по умолчанию исполняется под выделенным непривилегированным пользователем
  `raxd:raxd`; эффективный UID процесса демона != 0 (ЗАКРЫВАЕТ command-exec ОР-1 / file-upload ОР-U1).**
  Генератор всегда подставляет `User=raxd`/`Group=raxd` в unit (Linux) и `UserName=raxd` в plist (macOS);
  дефолтный порт **7822** непривилегированный → root для запуска НЕ требуется. Пользователь `raxd`
  создаётся при `service install` идемпотентно (`useradd --system --no-create-home --shell /usr/sbin/nologin
  raxd`, если ещё нет; существующий — переиспользуется без модификации); сервис-пользователь без login-shell
  и без обычного home. Проверка: тест в Linux-контейнере — после `service install`+start euid процесса
  демона **!= 0** (`systemctl show -p MainPID raxd` → `/proc/<pid>/status` поле `Uid:` != 0), сервис
  обслуживает запросы на дефолтном порту 7822 без root; grep шаблона — `User=raxd`/`Group=raxd` (unit) и
  `UserName=raxd` (plist) присутствуют; shell сервис-пользователя — `nologin`. (baseline §3 «демон НЕ от
  root: выделенный системный пользователь»; spec AC6; ADR-002; threat-model R-E1; ЗАКРЫВАЕТ command-exec
  ОР-1 / file-upload ОР-U1)

- [ ] **SR-84. Запуск менеджера сервиса под root для install/uninstall не превращается в запуск ДЕМОНА от
  root; при нехватке прав — нейтральная ошибка, а не молчаливый запуск от root.** Операции `install`/
  `uninstall` (запись в системные каталоги, создание пользователя, `systemctl`/`launchctl`) требуют прав
  администратора, НО результат — демон под `raxd` (SR-83), НЕ под root; на macOS launchd стартует от root и
  сбрасывает привилегии через `UserName=raxd`. Нехватка прав на регистрацию → типизированная `ErrPermission`
  с нейтральным сообщением + ненулевой код (НЕ тихий фолбэк к root-запуску). Проверка: тест — install без
  достаточных прав → ненулевой код + сообщение «недостаточно прав» (без сырой трассы); после успешного
  install euid демона != 0 (SR-83), независимо от того, что install выполнялся root. (baseline §3; spec
  AC6/AC12; plan §Contracts `ErrPermission`; threat-model R-E1/R-I1)

## Привилегия для порта <1024: строго условная и минимальная (baseline §3)

- [ ] **SR-85. При привилегированном порте (<1024) выдаётся ТОЛЬКО `AmbientCapabilities=CAP_NET_BIND_SERVICE`
  — НЕ полный root, НЕ setuid-root, НЕ иные capability; для порта ≥1024 capability НЕ выдаётся.** Генератор
  добавляет директиву `AmbientCapabilities=CAP_NET_BIND_SERVICE` в unit УСЛОВНО, ТОЛЬКО когда
  `NeedNetBindCap == true` (= `Port < 1024`, типизированное поле `TemplateData`, ADR-003); для дефолта 7822 и
  любых портов ≥1024 директива НЕ генерируется (ноль привилегий). НЕ используются `setcap`/setuid-root/
  `User=root`/иные capability. Проверка: тест генератора — `renderUnit` при `Port<1024` содержит ровно
  `AmbientCapabilities=CAP_NET_BIND_SERVICE` (и НИКАКИХ других `CAP_*`/setuid/`User=root`); при `Port≥1024`
  НЕ содержит `AmbientCapabilities`; интеграционный тест в контейнере — сервис на порту <1024 слушает порт
  при euid!=0 (SR-83); grep `internal/service` — нет вызова `setcap`/установки setuid-бита/`User=root`.
  (baseline §3 «порт <1024 — через capabilities, не setuid root»; spec AC7; ADR-003; threat-model R-E2;
  research Q4)

- [ ] **SR-86. `NoNewPrivileges=yes` ставится при порте ≥1024 (дефолт); опуск — ТОЛЬКО при ambient
  (Port<1024) — принятое отклонение П-1.** Генератор подставляет `NoNewPrivileges=yes` в unit для всех
  установок с `Port ≥ 1024` (включая дефолт 7822); опускает `NoNewPrivileges` ТОЛЬКО когда генерируется
  `AmbientCapabilities` (Port<1024, ADR-003 — исключение недоказанного конфликта ambient×NoNewPrivileges).
  Опуск НЕ происходит ни при каком Port≥1024. Проверка: тест генератора — `renderUnit` при `Port≥1024`
  содержит `NoNewPrivileges=yes` и НЕ содержит `AmbientCapabilities`; при `Port<1024` содержит
  `AmbientCapabilities` и НЕ содержит `NoNewPrivileges`. (baseline §3; spec AC7; ADR-003; threat-model
  R-E3/П-1; research Открытый вопрос «ambient×NoNewPrivileges»)

- [ ] **SR-87. Базовый hardening (`ProtectSystem=strict`/`ProtectHome=yes`/`PrivateTmp=yes`) присутствует в
  unit НЕЗАВИСИМО от порта.** Эти «дешёвые» директивы сужают видимую демону ФС и сохраняются в обоих случаях
  (Port<1024 и ≥1024), компенсируя опуск `NoNewPrivileges` при ambient (SR-86). Проверка: тест генератора —
  `renderUnit` при любом Port содержит `ProtectSystem=strict`, `ProtectHome=yes`, `PrivateTmp=yes`.
  (baseline §3 «окружение команд ограничено и предсказуемо»; spec AC7; plan §Шаблоны; threat-model
  R-E3/R-T2; research Q6)

## Права артефактов регистрации и состояния (baseline §2/§3)

- [ ] **SR-88. unit/plist/drop-in — владелец root:root, режим 0644 (не world/group-writable);
  непривилегированный пользователь не может подменить команду сервиса.** Файлы описания записываются с
  владельцем root:root и режимом 0644 (читаемы, НЕ записываемы группой/миром) в системные каталоги, куда
  пишет только root (`/etc/systemd/system/`, `/Library/LaunchDaemons/`, `/etc/systemd/journald.conf.d/`).
  Проверка: тест в контейнере — после install владелец unit и drop-in = root:root, режим = 0644 (нет битов
  `0022`); непривилегированный пользователь НЕ может перезаписать файл (`os.WriteFile` от не-root →
  permission denied); unit-тест генератора plist фиксирует ожидаемый режим/владельца как контракт (macOS
  проверяется вне Docker, SR-95). (baseline §3 «права»; spec AC2; plan §Хэндофф security «unit/plist/drop-in
  root:root 0644»; threat-model R-S1; research Q6)

- [ ] **SR-89. Каталог состояния — режим 0700 (явно, дефолт systemd 0755 ШИРЕ baseline); `keys.db`/приватный
  TLS-ключ — 0600; владелец — `raxd`.** Unit содержит `StateDirectory=raxd` + ЯВНО `StateDirectoryMode=0700`
  (НЕ полагаться на дефолт 0755); на macOS каталог состояния создаётся install явно `mkdir 0700`+`chown raxd`.
  `keys.db` и приватный TLS-ключ остаются 0600 (наследуется `EnsureDirs` 0700 / keystore, `internal/config/
  paths.go`). Владелец состояния = `raxd:raxd`. Проверка: тест в контейнере — после install режим каталога
  `/var/lib/raxd` = 0700, владелец `raxd`; (наследуемо) `keys.db`/TLS-ключ = 0600; grep unit — присутствует
  строка `StateDirectoryMode=0700`. (baseline §2 «приватный ключ 0600», §3 «права 0600/0700»; spec AC6;
  ADR-002; threat-model R-S2; research Q3)

## Анти-инъекция в шаблоны (baseline §3)

- [ ] **SR-90. Все значения, подставляемые в unit/plist (`ExecPath`/`User`/`Group`/`Label`/пути/`Port`),
  ВАЛИДИРУЮТСЯ ДО рендера; спецсимволы/переводы строк отвергаются; инъекция директив невозможна.** Перед
  рендером каждое поле `TemplateData` проверяется: `User`/`Group` — POSIX-имя (allowlist `[a-z_][a-z0-9_-]*`,
  без пробелов/`\n`/`\r`/управляющих/`=`/кавычек); `Label` — reverse-DNS-лейбл (allowlist, те же запреты);
  `ExecPath` — абсолютный нормализованный путь (`filepath.IsAbs`+`Clean`), без `\n`/`\r`/управляющих;
  `StateDir`/`ConfigDir`/`LogPath` — абсолютные нормализованные, без управляющих; `Port` — целое в `1..65535`.
  Условные директивы (`AmbientCapabilities`/`NoNewPrivileges`) деривируются из ТИПИЗИРОВАННОГО `NeedNetBindCap`
  (bool), а НЕ из сырой строки. Невалидное значение → ошибка ДО записи артефакта (не «тихий» рендер с
  инъекцией). Проверка: тест генератора — для каждого поля вход с `\n`/`\r`/пробелом/`=`/кавычкой/управляющим
  (например `User="raxd\nExecStart=/bin/sh"`, `Label="x\nUserName=root"`, `ExecPath="/x\nUser=root"`) →
  генератор/валидатор возвращает ошибку, поддельная директива в выводе НЕ появляется; валидный вход рендерится
  и сгенерированный unit принимается менеджером без ошибок разбора (AC2). (baseline §3 «валидация входа, без
  shell-инъекций»; spec AC2/AC12; plan §Хэндофф security «валидация ExecPath/User/Port до рендера»;
  threat-model R-T1; research Q6)

- [ ] **SR-91. Менеджер вызывается по известному имени через `exec.Command(name, args...)` БЕЗ shell-
  интерполяции; значения — отдельные аргументы; окружение сервиса задано явно и предсказуемо.** `runManager`
  запускает `systemctl`/`launchctl` через `exec.Command(name, args...)` (никогда `sh -c <строка>`,
  никогда конкатенация значений в одну строку-команду); `exec.ErrNotFound` → `ErrManagerUnavailable` (AC12).
  Окружение демона задаётся ЯВНО через `Environment=` (unit) / `EnvironmentVariables` (plist):
  `XDG_CONFIG_HOME=/etc`, `XDG_STATE_HOME=/var/lib`, `HOME=/var/lib/raxd` (ADR-002) — демон не наследует
  «грязное» окружение установщика. Проверка: grep `internal/service/exec.go` — вызов через
  `exec.Command(name, args...)`, отсутствуют `sh -c`/`exec.Command("sh", ...)` с подставленными значениями/
  строковая конкатенация аргументов; grep шаблонов — `Environment=`/`EnvironmentVariables` задают `XDG_*`/
  `HOME` явно; тест — `runManager` при отсутствующем менеджере возвращает `ErrManagerUnavailable`. (baseline
  §3 «exec без shell-интерполяции; окружение предсказуемо»; spec AC12; plan §Contracts `runManager`/§Шаблоны;
  threat-model R-T2; research Q6)

## Идемпотентность, откат, полнота uninstall (baseline §3)

- [ ] **SR-92. Установка идемпотентна и откатывается при сбое, не оставляя привилегий/полу-установленного
  сервиса.** Повторный `install` при уже установленном → `ErrAlreadyInstalled` без дубликата/повреждения
  существующей регистрации (AC9). Сбой на ЛЮБОМ шаге install → откат созданных артефактов (unit/plist/
  drop-in/созданные каталоги; пользователь НЕ откатывается — переиспользуем, ADR-002) + понятная ошибка,
  система в исходном (до установки) состоянии (AC11). Проверка: тест в контейнере — двойной install подряд →
  ровно одна корректная регистрация, вторая попытка не ломает первую; смоделированный сбой на шаге install →
  ошибка + отсутствие остаточных артефактов регистрации; повторный корректный install после сбоя проходит
  штатно. (baseline §3; spec AC9/AC11; plan §Contracts `Install`; threat-model R-D2)

- [ ] **SR-93. Удаление снимает автозапуск + capability + все артефакты регистрации; осознанно остаётся
  только непривилегированный пользователь `raxd` (П-2).** `Uninstall` выполняет stop+disable (снятие
  автозапуска) + удаление unit/plist + drop-in (с ним удаляются `AmbientCapabilities`/hardening-директивы,
  т.к. они в unit) + удаление созданных при install каталогов регистрации; после успеха артефактов
  регистрации НЕТ (`Status` → `Installed:false`); отсутствующий сервис → `ErrNotInstalled` без невнятного
  падения (AC10). ОСОЗНАННО ОСТАЁТСЯ: системный пользователь `raxd:raxd` БЕЗ login-shell и БЕЗ автозапуска
  (ADR-002/П-2). Проверка: тест в контейнере — после uninstall: `systemctl is-enabled raxd` → не enabled,
  unit и drop-in отсутствуют, `Status` → не установлен; пользователь `raxd` сохранён с shell `nologin` и без
  активного unit; удаление неустановленного сервиса → предсказуемый результат (`ErrNotInstalled`), без
  осиротевших артефактов. (baseline §3; spec AC10; ADR-002; threat-model R-E4/П-2/ОР-3)

## Системная ротация журнала (baseline §4)

- [ ] **SR-94. Рост аудит-журнала сервиса ограничен системно (journald drop-in `SystemMaxUse=`/
  `SystemMaxFileSize=`); механизм документирован и проверяем (ЗАКРЫВАЕТ command-exec ОР-2 / file-upload
  ОР-U4).** Аудит остаётся в stderr → journald (`Type=exec` + journald-дефолт `StandardError=journal`); рост
  ограничивается drop-in `/etc/systemd/journald.conf.d/raxd.conf` с `SystemMaxUse=`/`SystemMaxFileSize=`
  (ADR-004); drop-in устанавливается install и удаляется uninstall (SR-93). Механизм и пороги документирует
  tech-writer. На macOS — `StandardErrorPath=/var/log/raxd/raxd.log`, ротация через newsyslog (вне Docker,
  SR-95). Проверка (AC8): тест в контейнере — занизить `SystemMaxUse=`/`SystemMaxFileSize=` в drop-in,
  наполнить журнал синтетикой выше порога, `journalctl --disk-usage` показывает ОГРАНИЧЕННЫЙ рост (старые
  записи вытесняются, размер не растёт неограниченно); grep — drop-in присутствует после install; инспекция
  доки — механизм ротации описан. ГРАНИЦА (П-3/ОР-2): лимиты journald глобальны (per-host). (baseline §4
  «структурно, с ротацией»; spec AC8; ADR-004; threat-model R-D1/П-3; ЗАКРЫВАЕТ command-exec ОР-2 /
  file-upload ОР-U4)

## Среда, зависимости, отсутствие секретов в выводе (baseline §4/§6)

- [ ] **SR-95. Сообщения операций установки нейтральны и БЕЗ секретов/сырых трасс; macOS-ограничение
  проверки честно зафиксировано (unit-тесты генератора plist + ручной/CI-прогон вне Docker).** Вывод
  `service install/uninstall/start/stop/status` — осмысленные сообщения об успехе и нейтральные действенные
  ошибки (что произошло + подсказка) БЕЗ тела API-ключа, приватного TLS-ключа и сырых трасс менеджера
  (`runManager` захватывает stderr → нейтральная типизированная ошибка); типовые ошибки покрыты: нет прав
  (`ErrPermission`), уже установлен (`ErrAlreadyInstalled`), не установлен (`ErrNotInstalled`), менеджер
  недоступен/не поддержан (`ErrManagerUnavailable`/`ErrUnsupported`) — каждая → ненулевой код. macOS:
  интеграция в Docker НЕВОЗМОЖНА (Linux) → ОБЯЗАТЕЛЬНЫ (а) генерация ВАЛИДНОГО plist (включая `UserName=raxd`,
  права 0644, `KeepAlive={SuccessfulExit=false}`) и (б) unit-тесты генератора plist и выбора платформы по
  `runtime.GOOS`; в доке/плане зафиксировано, что macOS-интеграция (euid!=0, права, ротация) проверяется на
  РЕАЛЬНОМ macOS вне Docker. Проверка: тест — каждый типовой сценарий ошибки даёт ненулевой код + сообщение
  «ошибка + подсказка», grep вывода — нет `rax_live_`/PEM-маркеров/`panic:`/абсолютных raw-трасс; unit-тесты
  генератора plist зелёные; инспекция доки/плана — зафиксировано macOS-ограничение проверки (AC13). (baseline
  §4 «никаких секретов в логах/выводе CLI»; §6; spec AC12/AC13; plan §Contracts/§План тестирования;
  threat-model R-I1/ОР-4)

- [ ] **SR-96. Сборка/проверка — в Docker офлайн из `vendor/`; запуск сервиса — только в контейнере; новых
  внешних рантайм-зависимостей НЕТ.** Реализация ТОЛЬКО на stdlib (`text/template`/`os/exec`/`embed`/
  `runtime`/`os`/`context`) + уже вендоренный cobra (ADR-001); НОВЫХ внешних рантайм-зависимостей service-
  install НЕ вводит (НЕ `kardianos/service`); сборка/unit-тесты офлайн из `vendor/` (`-mod=vendor`, без
  `go mod download`); интеграция сервиса (install→status→kill→restart→stop→uninstall + AC8-журнал) и запуск
  демона — ТОЛЬКО в Docker-контейнере с systemd (baseline §6), не на хосте; кросс-сборка под darwin/linux ×
  amd64/arm64 (`CGO_ENABLED=0`, `-mod=vendor`) даёт 4 артефакта. Проверка: grep `go.mod` — нет новой внешней
  зависимости от service-install; контейнерный прогон интеграции + unit-тестов проходит из `vendor/`; запуск
  демона выполняется в systemd-контейнере, не на хосте; матрица сборки даёт 4 артефакта, нативный для
  контейнера `raxd version` исполняется. (baseline §6; spec AC14/AC15/AC16; ADR-001; plan §Trade-offs/§Modules;
  threat-model R-V1/ОР-5)

## Принятые отклонения (вход для reviewer / security-guardian)

> Полные формулировки с обоснованием и смягчением — в `threat-model.md` (раздел «Принятые отклонения от
> baseline»). Здесь — сводка для проверки.

- **П-1. `NoNewPrivileges` опускается при привилегированном порте (<1024) (ADR-003).** Обоснование:
  исключение недоказанного конфликта ambient×NoNewPrivileges (research Открытый вопрос). Компенсация:
  `NoNewPrivileges=yes` всегда при Port≥1024 (дефолт); опуск только при ambient; прочий hardening сохранён;
  демон непривилегированный, capability точечная. Проверяемо: SR-86, SR-87. Остаточный риск: ОР-1.
- **П-2. uninstall НЕ удаляет системного пользователя `raxd` (ADR-002).** Обоснование: удаление системного
  пользователя опаснее сохранения (UID-reuse, осиротевшие файлы); `raxd` без shell/home/unit не несёт
  автозапуска/привилегии. Компенсация: uninstall снимает всё, что несёт автозапуск/привилегию; факт
  документируется. Проверяемо: SR-93. Остаточный риск: ОР-3.
- **П-3. Лимиты роста journald глобальны (per-host), не per-raxd (ADR-004; наследует command-exec ОР-2).**
  Обоснование: размерные лимиты journald — per-host (research Q5); per-raxd требовал бы logrotate (более
  инвазивно). Компенсация: рост ограничен; выделенный контейнер/хост под raxd; fallback `LogsDirectory`+
  logrotate документируется. Проверяемо: SR-94. Остаточный риск: ОР-2.
- **macOS-ограничение проверки (AC13, ОР-4).** Интеграция launchd НЕпроверяема в Docker → обязательны
  генерация валидного plist + unit-тесты генератора + ручной/CI-прогон на реальном macOS вне Docker. Это
  НЕ снятие требования (AC1–AC12 применимы к обеим платформам). Проверяемо: SR-88 (контракт прав plist),
  SR-95.

## Закрытые остаточные риски command-exec / file-upload (явная фиксация для reviewer)

> Эти ОР были зафиксированы в предыдущих задачах как «закрываются не-root раскладкой + системной ротацией
> (задача service-install)». Здесь — что именно их закрывает.

- **command-exec ОР-1 / file-upload ОР-U1 — исполнение/запись от root-демона (WARN-дефолт).** ЗАКРЫВАЕТСЯ
  не-root раскладкой: демон по умолчанию исполняется под `raxd:raxd`, euid!=0 (SR-83/SR-84; threat-model
  R-E1). Это «основная защита», на которую те ОР ссылались. Понижает прежние ОР для штатной не-root
  установки до приемлемого уровня (опциональные `exec.deny_root`/`upload.deny_root` остаются доступными как
  дополнительный рычаг; per-инструментная детекция root + WARN сохраняется как наследуемая).
- **command-exec ОР-2 / file-upload ОР-U4 — ротация аудит-лога делегирована системе.** Ротационная часть
  ЗАКРЫВАЕТСЯ для systemd-установки: аудит stderr → journald + drop-in `SystemMaxUse=`/`SystemMaxFileSize=`
  ограничивает рост, проверяемо в контейнере (SR-94; threat-model R-D1/AC8). Граница: лимиты journald
  per-host (П-3/ОР-2) — для per-raxd лимита остаётся fallback logrotate.

## Наследуемые требования (выполнены в смежных задачах, service-install НЕ переопределяет)

> Полный текст и проверки — `specs/{tls-transport,mcp-server,command-exec,file-upload}/security-requirements.md`.
> Демон, оборачиваемый сервисом, сидит за этим периметром; service-install НЕ меняет функциональность демона
> (spec Out of Scope). Дублировать как новые SR ЗАПРЕЩЕНО.

- **SR-1/SR-2 (TLS 1.3), SR-7 (bind `127.0.0.1`)** — транспорт демона, как есть.
- **SR-8…SR-13 (auth ДО маршрутизации, Bearer→`keystore.Verify`, constant-time, мгновенный отзыв),
  SR-14/SR-16 (Host/Origin), SR-17/SR-18 (rate-limit→429)** — сетевой периметр, не меняется.
- **SR-19…SR-21 (аудит каждого действия; никаких секретов в логах)** — service-install обеспечивает
  СИСТЕМНУЮ РОТАЦИЮ этого канала (SR-94), но формат/содержание аудита не меняет.
- **SR-24/SR-25 (graceful shutdown; таймауты)** — graceful shutdown по SIGTERM НАСЛЕДУЕТСЯ как поведение
  остановки сервиса (AC5): `Restart=on-failure` (systemd) / `KeepAlive={SuccessfulExit=false}` (launchd) →
  штатная остановка не рестартит; service-install лишь подаёт SIGTERM через менеджер.
- **SR-54…SR-56 (command-exec) / SR-77 (file-upload) — детекция root + WARN + опциональный `deny_root`** —
  остаются на уровне инструментов как дополнительный рычаг; ОСНОВНУЮ защиту (не-root раскладка) даёт SR-83.
- **SR-61 (command-exec) / SR-82 (file-upload) — системная ротация / Docker-вендоринг** — service-install
  ИСПОЛНЯЕТ ротационную часть (SR-94) и не вводит новых зависимостей (SR-96).

## Вне scope этой задачи (фиксация, не требование к service-install)

- **Инсталлятор `curl | sh`, `.goreleaser`, CI-пайплайн релизов, подпись/нотаризация macOS, снятие
  quarantine, проверка `SHA256SUMS`** — задача `distribution` (baseline §5). Здесь только кросс-сборка как
  требование наличия рабочих бинарей (SR-96/AC14).
- **Изменение функциональности демона (serve/MCP/auth/TLS/rate-limit/формат аудит-записи)** — берётся как
  есть (spec Out of Scope); service-install лишь оборачивает запуск.
- **Расширенный hardening (`SystemCallFilter=@system-service`/`RestrictAddressFamilies`/
  `MemoryDenyWriteExecute`)** — НЕ в v1: для инструмента, исполняющего произвольные команды, эти фильтры
  могут ломать exec → требуют отдельной проверки против `execute_command` (research Q6 вариант B); базовый
  hardening (`ProtectSystem`/`ProtectHome`/`PrivateTmp`, SR-87) применён. При потребности — отдельная задача
  с тестом совместимости.
- **Создание macOS-пользователя (`dscl`), механика порта <1024 на macOS (root-bind/socket activation),
  ротация macOS-лога (newsyslog/unified logging)** — открытые вопросы для system-dev, проверяются на реальном
  macOS вне Docker (AC13, ОР-4).
- **Многоэкземплярный запуск, самообновление сервиса, контейнер/k8s как ПЕРВИЧНЫЙ режим** — вне scope v1
  (spec Out of Scope).
- **Per-инструментная политика root (`exec.deny_root`/`upload.deny_root`)** — реализована в
  command-exec/file-upload; здесь ОСНОВНАЯ защита — не-root раскладка (SR-83), per-инструментные флаги
  остаются как дополнительный рычаг.
