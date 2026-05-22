# system-dev-guardian — задача `service-install` (раунд 1)

**Verdict: needs-changes.** Guardian (read-only). Дата: 2026-05-22. Сохранено дирижёром (red line 1).

Проверены `specs/service-install/service-design.md` + `Makefile` + `Dockerfile.systemd` против контракта
system-dev, red lines и SECURITY-BASELINE.

## Подтверждено
- systemd unit полон+безопасен: Type=exec, ExecStart=raxd serve, Restart=on-failure+RestartSec=2s, User/Group
  raxd, StateDirectory=raxd + **StateDirectoryMode=0700 явно**, StandardOutput/Error=journal явно,
  NoNewPrivileges=yes (порт≥1024) / AmbientCapabilities=CAP_NET_BIND_SERVICE+CapabilityBoundingSet (порт<1024),
  ProtectSystem=strict/ProtectHome/PrivateTmp, WantedBy=multi-user.target. NeedNetBindCap типизирован. SR-85/86/87/89.
- launchd plist полон: KeepAlive{SuccessfulExit=false} (restart-on-failure, не при graceful stop, AC4/AC5),
  UserName/GroupName, EnvironmentVariables XDG_*+HOME, StandardOut/ErrorPath, RunAtLoad, Label reverse-DNS
  tech.oem.raxd, каталоги install -d -m 0700 -o raxd.
- Lifecycle детален (точки отката A/B, идемпотентность, осознанное сохранение raxd — П-2); AC8-тест
  воспроизводим (заниженный порог + наполнение + journalctl --disk-usage). Кросс-сборка 4 цели CGO_ENABLED=0.
  Scope: Go app-код (internal/service/*, cli/service.go) НЕ написан — делегирован developer. Русский.

## Issues (needs-changes)
**Issue 1 — НАРУШЕНИЕ baseline §6: `Makefile` verify-cross запускает бинарь на ХОСТЕ.** Строки ~105-107:
`@./$(DIST_DIR)/raxd_$(NATIVE...) version` — нет docker-обёртки/guard; `make verify-cross` на хосте запустит
raxd вне контейнера. baseline §6: raxd запускается только в изолированном контейнере. Fix: обернуть нативный
запуск в `docker run --rm`/`docker exec` ИЛИ guard на `/.dockerenv` с прерыванием, ИЛИ отдельный
docker-таргет. (service-design.md §10.3 говорит «в Docker», но Makefile это не обеспечивает.)

**Issue 2 — контракт роли `.claude/agents/system-dev/system-dev.md` устарел (kardianos).** Red line роли всё
ещё предписывает `kardianos/service`, а ADR-001 (accepted) выбрал ручную генерацию (STACK уже обновлён
дирижёром). Следующий system-dev прочитает противоречие. Fix (дирижёр): обновить system-dev.md. (system-dev
сам контракт роли не правит.)

**Issue 3 — service-design.md §10.1 недостоверный пакет logger.** Указан `util-linux (logger)`, но в
Ubuntu/Debian `logger` в пакете `bsdutils` (Dockerfile.systemd корректно ставит bsdutils). Fix: в §10.1
исправить на `bsdutils (logger, Ubuntu/Debian; на RHEL — util-linux)`.

## Итог
needs-changes (Issue 1 — реальное нарушение §6 в Makefile, не игнорируется; Issue 3 — недостоверная дока).
Issue 2 закрывает дирижёр (контракт роли). Возврат к system-dev по Issue 1+3.
