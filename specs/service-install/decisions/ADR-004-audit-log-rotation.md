# ADR-004: Ротация/ограничение роста аудит-журнала — journald (stderr), без правок кода

## Контекст
spec AC8 требует системного ограничения роста аудит-журнала сервиса (закрывает остаточный риск роста
лога из command-exec/file-upload). spec Q5 оставляет открытыми механизм и политику. raxd сейчас пишет
аудит в **stderr** строгим logfmt через `charmbracelet/log`
(`internal/cli/serve.go`: `logger.SetFormatter(clog.LogfmtFormatter)`).

## Решение
**Linux: оставить аудит в stderr → journald** (при systemd-сервисе с `Type=exec` и
`StandardError=journal` stderr демона попадает в journald) и ограничить рост через **drop-in
`/etc/systemd/journald.conf.d/raxd.conf`** с `SystemMaxUse=` / `SystemMaxFileSize=`. Минимально-
инвазивно: код raxd НЕ меняется (stderr уже пишется); ротация/vacuum journald автоматические и
синхронные. Drop-in устанавливается `service install`, удаляется `service uninstall` (учитывается в
AC10 «без осиротевших артефактов»). **macOS**: stderr → `StandardErrorPath=/var/log/raxd/raxd.log`;
ротация через `newsyslog`-конфиг — открытый вопрос для system-dev (вне Docker, AC13).

## Альтернативы
- Собственный файл (`LogsDirectory=raxd` → `/var/log/raxd`) + `logrotate`: даёт per-raxd политику
  размера/срока, НО требует файлового вывода (`StandardError=append:…` или правка кода) и доп.
  артефакта (logrotate-конфиг — установка/дерегистрация). Отклонено как более инвазивное; остаётся
  fallback, если security потребует именно per-raxd лимит. →
  https://man7.org/linux/man-pages/man8/logrotate.8.html

## Последствия
- Плюсы: ноль изменений кода (stderr уже пишется); автоматическая синхронная ротация; интеграция с
  `journalctl`; тест AC8 воспроизводим через занижение `SystemMaxUse=`/`SystemMaxFileSize=` в drop-in
  + наполнение + `journalctl --disk-usage`.
- Минусы (цена выбора): размерные лимиты journald **глобальные (per-host, не per-unit)** — занижение
  порога в тесте затрагивает весь хост контейнера (допустимо в Docker baseline §6); нет
  изолированного per-raxd лимита (приемлемо: контейнер выделенный). →
  https://man7.org/linux/man-pages/man5/journald.conf.5.html
- На стек: согласуется со STACK §Логи («системный журнал journald/syslog + ротация при файловом
  выводе»).

## Статус (proposed|accepted)
accepted
