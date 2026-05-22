# ADR-001: Способ регистрации сервиса — ручная генерация unit/plist (без `kardianos/service`)

## Контекст
spec service-install (AC1/AC2/AC9-AC12) требует кросс-платформенную регистрацию сервиса
(install/uninstall/start/stop/status) с генерацией валидного unit (Linux) и plist (macOS).
STACK.ru.md называет `kardianos/service` выбором, НО в `go.mod`/`vendor/` его нет и в коде он не
используется (research Q0). Обязательные для нас безопасные директивы (`StateDirectory`/
`StateDirectoryMode`/`AmbientCapabilities`/hardening на Linux; `KeepAlive={SuccessfulExit=false}` на
macOS) встроенными шаблонами библиотеки НЕ покрываются → текст unit/plist всё равно пишется нами,
ценность библиотеки (генерация описания) обнуляется требованиями безопасности.

## Решение
**Вариант B (research-рекомендация): ручная генерация unit/plist через stdlib `text/template` + вызов
нативных менеджеров (`systemctl` / `launchctl`) через `os/exec`.** Без новой зависимости. Абстракция
платформы — интерфейс `ServiceManager` (`Install/Uninstall/Start/Stop/Status`) + реализации
`systemdManager` (Linux) и `launchdManager` (macOS), выбор по `runtime.GOOS`. Текст описания —
встроенные шаблоны (`embed` или строковые константы), параметризуемые `TemplateData`.

## Альтернативы
- **A/C — `kardianos/service` (vendored)**: даёт готовый lifecycle, но требует `go get` + `go mod
  tidy` + `go mod vendor` + коммит `vendor/` на хосте (offline-Docker недоступен для `go mod
  download`); шаблоны всё равно подменяются через `SystemdScript`/`LaunchdConfig` → выигрыша нет, а
  цена — новая внешняя зависимость v1.2.4 и правка STACK. Отклонено. →
  https://pkg.go.dev/github.com/kardianos/service
- **A (дефолтные шаблоны библиотеки)**: не покрывают наши security-директивы. Отклонено.

## Последствия
- Плюсы: ноль новых зависимостей (совместимо с offline-вендорингом, baseline §6/AC15); полный
  контроль над безопасным текстом сервиса; реализуемо без правок хост-окружения.
- Минусы (цена выбора): идемпотентность (AC9/AC10), безопасный откат (AC11), детект менеджера/
  обработка ошибок (AC12) реализуются нами — больше платформенного кода и тестов.
- **Разрешение конфликта STACK ↔ go.mod**: `kardianos/service` помечается в STACK.ru.md как НЕ
  используемый (заменён ручной генерацией unit/plist на stdlib); правку STACK выполняет дирижёр/
  tech-writer после accept этого ADR. go.mod/vendor не меняются.

## Статус (proposed|accepted)
accepted
