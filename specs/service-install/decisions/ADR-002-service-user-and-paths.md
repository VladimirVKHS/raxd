# ADR-002: Сервис-пользователь и раскладка путей для не-root сервиса (Linux + macOS)

## Контекст
spec AC6 требует не-root исполнение под выделенным пользователем; AC8 — ограничение роста журнала;
spec Q1/Q3 оставляют открытыми имя/способ создания пользователя и раскладку путей. Текущий резолв
путей raxd — XDG через env (`internal/config/paths.go`): `ConfigDir`/`StateDir` управляются
`XDG_CONFIG_HOME`/`XDG_STATE_HOME`, при отсутствии `$HOME` — ошибка. У системного пользователя нет
обычного `$HOME` → домашние пути не годятся.

## Решение
- **Сервис-пользователь и группа: статический системный `raxd:raxd`** (симметрия Linux/macOS,
  стабильный UID для владения персистентным состоянием). На Linux создаётся при `service install`
  идемпотентно через `useradd --system --no-create-home --shell /usr/sbin/nologin raxd` (если ещё
  нет); если пользователь существует — переиспользуется (НЕ модифицируется, НЕ удаляется при
  uninstall). На macOS `UserName=raxd` в plist; создание macOS-пользователя (`dscl`/готовый аккаунт)
  — открытый вопрос для system-dev (вне Docker, AC13), launchd работает с любым существующим
  непривилегированным пользователем.
- **Платформа сервиса**: Linux — `User=raxd` в unit `/etc/systemd/system/raxd.service`; macOS —
  системный daemon `/Library/LaunchDaemons/tech.oem.raxd.plist` + `UserName=raxd` (per-user agent
  отклонён — не даёт автозапуск до логина, нарушает AC3).
- **Пути: переиспользовать существующий XDG-резолв БЕЗ правки кода**, задав сервису через
  `Environment=` (unit) / `EnvironmentVariables` (plist): `XDG_CONFIG_HOME=/etc`,
  `XDG_STATE_HOME=/var/lib`, `HOME=/var/lib/raxd` → raxd сам разрешит `/etc/raxd`, `/var/lib/raxd`.
  На Linux каталог состояния создаёт systemd через `StateDirectory=raxd` с
  **`StateDirectoryMode=0700`** (дефолт 0755 ШИРЕ baseline — задаётся явно). На macOS каталоги/права
  создаёт `service install` явно (`mkdir 0700` + `chown raxd`), т.к. StateDirectory-аналога нет.

## Альтернативы
- `DynamicUser=yes` + `StateDirectory` (через `/var/lib/private/raxd`): лучшая изоляция (UID-recycle),
  но **Linux-only** (асимметрия с macOS) и нестабильный UID для персистентного состояния. Отклонено
  ради единого ментального контракта «пользователь raxd» на обеих платформах. →
  https://man7.org/linux/man-pages/man5/systemd.exec.5.html
- Захардкодить системные пути в коде raxd: дублирует уже работающий XDG-механизм. Отклонено.
- Оставить домашние XDG (`~/.config`): у системного пользователя нет `$HOME` → ошибка. Отклонено.

## Последствия
- Плюсы: симметрия платформ; стабильный UID упрощает владение состоянием (keys.db/tls/uploads/аудит);
  системные пути по FHS; код резолва путей НЕ меняется.
- Минусы (цена выбора): шаг создания пользователя при install + политика «уже существует»; на macOS
  каталоги/права делаем вручную (нет StateDirectory).
- Безопасность: `StateDirectoryMode=0700` явно (баз. 0700); keys.db остаётся 0600 (наследуется из
  существующего `EnsureDirs`/keystore).

## Статус (proposed|accepted)
accepted
