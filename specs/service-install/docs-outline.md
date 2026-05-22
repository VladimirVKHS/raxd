# Docs Outline: service-install — управление системным сервисом `raxd service`

> План документации для задачи `service-install`. Источник истины — КОД
> (`internal/cli/service.go`, `internal/service/{service,templates,systemd,launchd,exec}.go`,
> `internal/config/paths.go`) и артефакты `specs/service-install/*`. Документируется ТОЛЬКО то,
> что реально есть в коде. Язык docs/** — английский (как все docs/), этот outline — русский.
> Автор продукта: **Vladimir Kovalev, OEM TECH**.

## Затронутые файлы docs/

| Файл | Тип изменения | Назначение |
|------|---------------|-----------|
| `docs/commands.md` | дополнение | Новый раздел `raxd service` (5 подкоманд) + правки командного дерева, summary-таблицы, "Out of scope for serve" |
| `docs/configuration.md` | дополнение | Новый раздел "Service layout (system service)": пути сервиса, не-root пользователь, порт/capability |
| `docs/service-management.md` | НОВЫЙ файл | Операционный + security-гайд по сервису (по образцу execute-command-security.md / file-upload-security.md) |
| `docs/troubleshooting.md` | дополнение | Новый раздел "`raxd service`": типовые проблемы установки/запуска |
| `README.md` | точечная правка | `raxd service` в обзоре команд; статус "Working"; ссылки на новые доки; автор сохранён |

## На каждый документ

### `docs/commands.md` — раздел `raxd service`
- **Цель**: точный command reference для группы `raxd service` и её 5 подкоманд.
- **Аудитория**: оператор хоста, ставящий и обслуживающий сервис.
- **Ключевые секции**:
  - Командное дерево (добавить `service` с 5 подкомандами).
  - Что делает `install` (создаёт пользователя raxd, раскладывает каталоги, генерирует unit/plist,
    включает автозапуск); требование root ДЛЯ install, но демон работает НЕ от root.
  - 5 подкоманд: назначение, usage, output, exit-коды, тексты ошибок.
  - Потоки вывода: `status` → stdout (и `--json`); мутации (install/uninstall/start/stop) → stderr.
  - Exit-коды (ровно по коду): ErrAlreadyInstalled@install → 0; ErrNotInstalled@uninstall → 0;
    ErrNotInstalled@start/stop → 1; status всегда 0.
  - Тексты ошибок (`error:` / `hint:` строчными). Hint `data in … is preserved` — платформенный
    путь (Linux `/var/lib/raxd`, macOS `/usr/local/var/raxd`), см. OQ-1 ниже (закрыт).
  - Обновить "Out of scope for serve" (сервис теперь есть) и summary-таблицу.

### `docs/configuration.md` — раздел "Service layout"
- **Цель**: где сервис хранит конфиг/состояние/логи и под каким пользователем работает.
- **Аудитория**: оператор, настраивающий сервис на хосте.
- **Ключевые секции**:
  - Таблица путей: Linux (`/etc/raxd`, `/var/lib/raxd`, journald) и macOS
    (`/usr/local/etc/raxd`, `/usr/local/var/raxd`, `/usr/local/var/log/raxd`) — сверено с
    `DefaultConfigForGOOS`.
  - Не-root пользователь `raxd` (euid != 0); как сервис разрешает XDG-пути через `Environment=`/plist.
  - Порт по умолчанию 7822 (root не нужен); порт <1024 → нужна capability (ссылка на service-management.md).
  - Права каталогов (0700) и файлов регистрации (0644), keys.db 0600.

### `docs/service-management.md` — НОВЫЙ операционный + security гайд
- **Цель**: объяснить безопасную модель сервиса и операционные нюансы (по образцу security-гайдов).
- **Аудитория**: оператор + специалист по безопасности.
- **Ключевые секции (хэндофф reviewer)**:
  1. Non-root execution: демон под raxd, euid != 0; закрывает прежние root-риски command-exec/file-upload.
  2. Capability для порта <1024 (ADR-003): дефолт 7822 → не нужно; <1024 →
     `AmbientCapabilities=CAP_NET_BIND_SERVICE` (только эта); оговорка про NoNewPrivileges + эскалация ОР-1.
  3. Пользователь raxd сохраняется после uninstall (П-2): что снимается, что остаётся, как удалить вручную.
  4. Ротация журнала (SR-94, AC8): journald drop-in SystemMaxUse=200M/SystemMaxFileSize=50M;
     per-host граница (П-3) + fallback logrotate; macOS — newsyslog/StandardErrorPath.
  5. macOS-ограничение проверки (AC13/ОР-4): launchd не тестируется в Docker; прогон на реальном macOS.
  6. Restart-on-failure vs graceful stop (AC4/AC5): рестарт при сбое, не при штатной остановке.

### `docs/troubleshooting.md` — раздел `raxd service`
- **Цель**: диагностика типовых проблем сервиса.
- **Аудитория**: оператор.
- **Ключевые секции**:
  - Нет прав на install (нужен root для install, демон — не-root).
  - Менеджер недоступен (нет systemd/launchd).
  - Сервис не стартует (journalctl/логи; права каталогов; BUG-1 ConfigDir).
  - Порт занят.
  - Порт <1024 без capability.
  - Уже установлен / не установлен.

### `README.md`
- **Цель**: упомянуть рабочий `raxd service` в обзоре.
- Правки: командное дерево + `service` группа; статус "system-service registration" → Working;
  ссылка на `docs/service-management.md`; строка автора **Vladimir Kovalev, OEM TECH** сохранена.

## Примеры команд (проверены по коду/CLI)
- `sudo raxd service install` — регистрация сервиса + автозапуск (требует root).
- `raxd service status` — состояние сервиса в stdout.
- `raxd service status --json` — машиночитаемый статус.
- `sudo raxd service start` / `sudo raxd service stop` — запуск/остановка.
- `sudo raxd service uninstall` — снятие регистрации (пользователь raxd остаётся).
- `sudo userdel raxd` — ручное удаление пользователя на Linux (по hint из uninstall).
- `sudo dscl . -delete /Users/raxd` — ручное удаление пользователя на macOS (по hint из uninstall).

## Об авторе (OEM TECH)
- **Vladimir Kovalev, OEM TECH** — сохранён в README ("Author" + раздел "## Author") и в баннере
  CLI (третья строка, через `banner.Render()`). Новые документы ссылаются на продукт raxd; авторская
  строка не дублируется в каждом файле, но и не удаляется из README.

## Чек-лист покрытия хэндофф-пунктов reviewer
- [ ] Сохранение пользователя raxd после uninstall (П-2) — service-management.md §3 + commands.md (uninstall) + configuration.md.
- [ ] Ротация journald + пороги (SR-94) + fallback logrotate (П-3) — service-management.md §4.
- [ ] macOS-ограничение проверки + прогон на реальном macOS (AC13/ОР-4) — service-management.md §5.
- [ ] Capability для порта <1024 (ADR-003) + оговорка NoNewPrivileges/ОР-1 — service-management.md §2 + configuration.md.
- [ ] Не-root исполнение (euid != 0) — service-management.md §1 + commands.md + configuration.md.
- [ ] Restart-on-failure vs graceful stop (AC4/AC5) — service-management.md §6.
- [ ] 5 подкоманд + exit-коды + тексты ошибок — commands.md.
- [ ] Пути сервиса (Linux/macOS) — configuration.md.
- [ ] Troubleshooting сервиса — troubleshooting.md.

## Открытые вопросы / честные оговорки (расхождение «вывод ≠ платформа», не выдумка)
- [x] **OQ-1 — ЗАКРЫТ (исправлено в коде, developer-коммит 2075d2b; Docker-верифицирован
      `TestServiceUninstall_Success_HintContainsPlatformStateDir` PASS, 0 FAIL).** Ранее блок успеха
      `raxd service uninstall` печатал hint с захардкоженным linux-путём `/var/lib/raxd` на ОБЕИХ
      платформах. Теперь `internal/cli/service.go` берёт путь из
      `service.DefaultConfigForGOOS(runtime.GOOS).StateDir` → Linux `/var/lib/raxd`, macOS
      `/usr/local/var/raxd`. Поведение корректно — путь платформенный. Doc обновлены: в
      `docs/commands.md`, `docs/service-management.md`, `docs/troubleshooting.md` macOS-блок/нота
      показывают реальный платформенный state-каталог (а не «hint всегда печатает Linux-путь»). Прежняя
      оговорка про захардкоженный путь удалена из всех трёх docs.
- [ ] **OQ-2 (остаётся, by design).** `euid` в `raxd service status` читается из `/proc/<pid>/status`
      только на Linux; на macOS `EUID` всегда `0` в структуре `Status` (нет `/proc`) и строка `euid`
      в выводе НЕ печатается (печатается только при `EUID > 0`). Документируем это поведение честно (на
      macOS гарантия не-root проверяется через `UserName=raxd` в plist, не через строку euid).
</content>
