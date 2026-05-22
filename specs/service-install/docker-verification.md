# service-install — живая проверка в Docker (дирижёр)

Выполнена дирижёром после merge `feature/service-install` в `develop` (мандат «проверять самому в
Docker», SECURITY-BASELINE §6). Дата: 2026-05-22.

## Среда

- Образ/бинарь пересобраны с develop-кодом: `make test-service` (зависимости `docker-systemd`,
  `build-linux-amd64`) — образ systemd (`Dockerfile.systemd`) + нативный linux/amd64 бинарь из vendor.
- Контейнер с systemd: `--privileged --cgroupns=host -v /sys/fs/cgroup:/sys/fs/cgroup:rw` (ТОЛЬКО для
  теста, не прод). Бинарь raxd скопирован в /usr/local/bin, прогон `scripts/integration-service.sh`.
- Юнит-тесты отдельно: `docker build --target test` → `go vet + go test ./... + -race` — все пакеты PASS,
  0 FAIL (включая internal/service, internal/cli).

## Результат интеграции (мой собственный прогон, не только qa)

**ИТОГ: 62 PASS / 0 FAIL** (`=== test-service PASSED ===`).

Ключевые живые наблюдения:
- STEP2: `raxd service start` → сервис **active**; `MainPID=179`; **LIVE euid=999** (не root) из
  `/proc/179/status` — AC6/SR-83 подтверждён вживую (не по unit-файлу). Демон под пользователем raxd.
- STEP4 (AC4 restart-on-failure): `kill -9 179` → автоперезапуск, новый `PID=246`, `NRestarts=1`; euid после
  рестарта снова 999. Restart=on-failure работает.
- STEP5 (AC5 graceful stop): `raxd service stop` → state **inactive**; через 6s по-прежнему inactive — НЕ
  перезапустился (SIGTERM = clean exit). Различимо от AC4.
- STEP10 (AC8 ротация): journald `--disk-usage` = `5.0M` (5242880 байт) ≤ порог 10M (10485760) при заниженном
  лимите — рост журнала ограничен системными средствами.
- Идемпотентность (AC9/10), безопасный откат (AC11, code-review для chmod-симуляции под root), понятные
  ошибки без секретов (AC12/SR-95), не-root install-требование (AC6/SR-84) — все PASS.

## BUG-1 (закрыт, подтверждено вживую)

До фикса демон под пользователем raxd падал при старте: `config.EnsureDirs`→`MkdirAll(/etc/raxd)` не мог
создать каталог (raxd не пишет в /etc; ProtectSystem=strict) → crash-loop → MainPID=0. Воспроизведено
дирижёром: без /etc/raxd `raxd serve` отдаёт «cannot create TLS directory: permission denied»; с
предсозданным /etc/raxd (owned raxd 0700) — стартует, cert в /var/lib/raxd/tls, listening. Фикс:
`ConfigurationDirectory=raxd`+`ConfigurationDirectoryMode=0700` в unit (systemd создаёт /etc/raxd до
ExecStart → EnsureDirs no-op). macOS-пути сделаны платформенно-корректными (/usr/local/etc/raxd,
/usr/local/var/raxd) — unit-тестируемо (AC13), интеграция launchd — на реальном macOS.

## Закрытые прежние остаточные риски

- command-exec ОР-1 / file-upload ОР-U1 (исполнение/запись от root) → закрыты не-root раскладкой raxd:raxd
  (euid=999 подтверждён LIVE).
- command-exec ОР-2 / file-upload ОР-U4 (ротация аудита) → закрыты journald-лимитами (5.0M≤10M LIVE);
  per-host граница остаётся как ОР-2 (П-3).

## Остаточные риски к эскалации (threat-model)

ОР-1 (NoNewPrivileges×ambient при порте<1024 — верифицировать перед прод-релизом с привилегированным портом),
ОР-3 (пользователь raxd сохраняется после uninstall — UID-reuse), ОР-4 (macOS/launchd вне Docker — прогон на
реальном macOS перед macOS-релизом).

**Вывод: service-install работает end-to-end в systemd-Docker; не-root исполнение (euid=999),
restart-on-failure, graceful stop, ротация журнала, идемпотентность, отсутствие секретов — подтверждены
вживую (62 PASS/0 FAIL). service-install закрыт.**
