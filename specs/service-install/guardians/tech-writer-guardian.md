# tech-writer-guardian — задача `service-install`

**Verdict: pass.** Guardian (read-only). Дата: 2026-05-22. Сохранено дирижёром (red line 1).

Проверены docs `raxd service`: commands.md, configuration.md, service-management.md (новый),
troubleshooting.md, README.md, docs-outline.md против контракта tech-writer и кода.

## Подтверждено (сверено с кодом)
- journald-пороги SystemMaxUse=200M/SystemMaxFileSize=50M (templates.go:348); StateDirectory/
  ConfigurationDirectory=raxd + оба Mode=0700 (templates.go:193-200); Restart=on-failure/RestartSec=2s;
  KeepAlive SuccessfulExit=false; условный AmbientCapabilities=CAP_NET_BIND_SERVICE (NeedNetBindCap),
  NoNewPrivileges только при !NeedNetBindCap; ProtectSystem=strict/ProtectHome/PrivateTmp в обоих вариантах.
- Пути linux (/etc/raxd,/var/lib/raxd,unit,drop-in) и macOS (/usr/local/etc/raxd,/usr/local/var/raxd,
  /usr/local/var/log/raxd,/Library/LaunchDaemons/tech.oem.raxd.plist) — DefaultConfigForGOOS, systemd.go/
  launchd.go константы. useradd --system --no-create-home --shell nologin.
- Exit-коды (ErrAlreadyInstalled@install/ErrNotInstalled@uninstall→0; ErrNotInstalled@start/stop→1; status→0)
  и потоки (status→stdout, мутации→stderr) — service.go. euid не печатается на macOS (by design, OQ-2).
- **OQ-1 закрыт:** uninstall-hint берёт путь из DefaultConfigForGOOS(runtime.GOOS).StateDir (service.go:210);
  docs показывают платформенный путь, прежняя оговорка про Linux-хардкод убрана.
- Хэндофф reviewer покрыт (6 пунктов): не-root §1; capability<1024+ОР-1 §2; raxd после uninstall+ручное
  удаление §3; journald-ротация+пороги+per-host/logrotate §4; macOS-ограничение §5; restart vs graceful §6.
- macOS честно «не тестируется в Docker, нужен реальный macOS». Автор Vladimir Kovalev OEM TECH в README.
  Примеры команд корректны; cross-ref/якоря рабочие; docs английский, outline русский. Выдумок нет.

## Замечание (не блокирует)
- commands.md install-hint в мокапе показан без выравнивания `%-14s` — косметика отображения примера.

## Итог
pass — документация service-install принята. Задача готова к закрытию (коммиты дирижёра + merge).
