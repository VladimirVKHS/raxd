# Service Design: <короткое название>

## Механизм per-OS
- **Linux (systemd)**: тип unit, расположение (`/etc/systemd/system/raxd.service`), ключевые
  директивы (`ExecStart`, `User=`, `Restart=`, capabilities).
- **macOS (launchd)**: plist (`~/Library/LaunchAgents/` или `/Library/LaunchDaemons/`), ключи
  (`ProgramArguments`, `KeepAlive`, `RunAtLoad`, `UserName`).
- Абстракция через `kardianos/service` + генерация unit/plist (из STACK).

## Lifecycle
- start / stop / restart — как выполняются на каждой ОС.
- Авто-рестарт при падении: systemd `Restart=on-failure`; launchd `KeepAlive`.
- Graceful shutdown (обработка сигналов, таймаут).

## Привилегии
- Демон работает **НЕ от root**: выделенный системный пользователь.
- Порт <1024 при необходимости — Linux **capabilities** (`CAP_NET_BIND_SERVICE`), **НЕ setuid root**.
- Рабочая директория/окружение ограничены и предсказуемы.

## Build-матрица
- Цели (4): `GOOS={linux,darwin} × GOARCH={amd64,arm64}` → `raxd_{linux,darwin}_{amd64,arm64}`.
- `CGO_ENABLED=0` (статическая сборка).
- Артефакты: архивы `.tar.gz` + `SHA256SUMS` (через goreleaser).

## Файлы
- Пути и назначение сгенерированных файлов: systemd unit, launchd plist, скрипты lifecycle,
  конфиг build-матрицы — где лежат в репозитории/на целевой системе.
