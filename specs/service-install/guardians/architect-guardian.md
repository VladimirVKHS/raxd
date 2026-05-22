# architect-guardian — задача `service-install`

**Verdict: pass.** Guardian (read-only). Дата: 2026-05-22. Сохранено дирижёром (red line 1).

Проверены `specs/service-install/plan.md` + ADR-001..004 (accepted) против контракта architect и red lines.

## Подтверждено
- Выбран РОВНО ОДИН подход по каждой развилке (Q0-Q9): ручная генерация text/template (ADR-001),
  `ServiceManager`+systemd/launchd, статический пользователь `raxd`+XDG через `Environment=` (ADR-002),
  условная `AmbientCapabilities` при порте<1024 (ADR-003), journald+drop-in (ADR-004), CLI `raxd service`,
  кросс-сборка Makefile/Dockerfile.systemd. Никаких «вариантов на выбор developer».
- Модули с путями и контрактами: `internal/service/{service,systemd,launchd,templates,exec}.go`,
  `internal/cli/service.go`+root.go, Makefile/Dockerfile.systemd. Типизированные ошибки
  (ErrAlreadyInstalled/ErrNotInstalled/ErrManagerUnavailable/ErrPermission/ErrUnsupported);
  `ServiceManager` с сигнатурами; renderUnit/renderPlist чистые. Тел функций нет.
- AC1-16 не изменены, все покрыты (привязки в Modules/Contracts/Шаблонах/Плане тестирования).
- Вендоринг: новых зависимостей нет (stdlib + cobra). STACK↔go.mod конфликт kardianos/service разрешён в
  ADR-001 (помечен неиспользуемым, заменён ручной генерацией) — не проигнорирован.
- Безопасность спроектирована заранее: euid!=0 (AC6), StateDirectoryMode=0700 (не дефолт 0755), capability
  только при порте<1024 + осознанный опуск NoNewPrivileges (ADR-003) с явным хэндоффом security; валидация
  ExecPath/User/Port до рендера (анти-инъекция); uninstall снимает unit/drop-in. Restart AC4/AC5 опирается
  на реальный graceful shutdown serve.go (SIGTERM→exit 0→systemd не рестартит; launchd KeepAlive
  SuccessfulExit=false).
- ADR accepted, обоснованы, альтернативы+последствия. Scope-guard: distribution не затянут. Язык русский.

## Замечания (НЕ блокируют, для system-dev/developer)
- В unit-шаблоне нет явной `StandardError=journal` (для Type=exec это дефолт; AC8 завязан на journald —
  явная директива снимет неоднозначность).
- `config.Load(paths).Port` в Contracts/ADR-003 — shorthand; реальная сигнатура `Load(PathSet)(*Config,error)`,
  developer обязан обработать error (не игнорировать).
- Длина plan.md ~113 строк > ориентира 100 — оправдано 16 AC × кроссплатформенность (spec объявил задачу
  недробимой).

## Итог
pass — переход к роли security (хэндофф подготовлен в плане). Дирижёру: пометить kardianos/service в
STACK как неиспользуемый (поручено ADR-001).
