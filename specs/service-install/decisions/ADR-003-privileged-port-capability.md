# ADR-003: Выдача сетевой привилегии для порта <1024 — `AmbientCapabilities` (условно)

## Контекст
spec AC7: если оператор настраивает привилегированный порт (<1024), сервису выдаётся ТОЛЬКО точечная
сетевая привилегия, не root и не setuid-root. Дефолт raxd — порт 7822 (НЕ привилегированный) → по
умолчанию capability НЕ нужна; этот выбор релевантен лишь при ручной смене на <1024. baseline §3
прямо предписывает `CAP_NET_BIND_SERVICE` вместо setuid root.

## Решение
- **Linux: `AmbientCapabilities=CAP_NET_BIND_SERVICE`** в unit, добавляется генератором **УСЛОВНО —
  только когда `cfg.Port < 1024`**. Для дефолта 7822 и портов ≥1024 директива НЕ генерируется (ноль
  привилегий). Генератор узнаёт порт из `config.Load(paths)` на момент `service install` (тот же код,
  что читает serve). Опционально сужение `SocketBindAllow=tcp:<порт>`.
- **NoNewPrivileges**: при порте ≥1024 (включая дефолт) — `NoNewPrivileges=yes` (полный hardening).
  При порте <1024 — `NoNewPrivileges` НЕ ставится (ambient-капы выдаёт менеджер systemd через auto
  `keep-caps`; чтобы исключить недоказанный конфликт ambient×NoNewPrivileges, при наличии
  AmbientCapabilities NoNewPrivileges опускается). Совместимость подтверждает security при утверждении
  unit (открытый вопрос research). →
  https://man7.org/linux/man-pages/man5/systemd.exec.5.html
- **macOS**: launchd-daemon стартует от root и сбрасывает привилегии через `UserName=`; для <1024 —
  механика root-bind/socket activation, открытый вопрос для system-dev (вне Docker, AC13). Дефолт
  7822 делает macOS-случай нерелевантным по умолчанию.

## Альтернативы
- `setcap cap_net_bind_service=+ep` на бинаре: capability в xattr `security.capability` → **теряется
  при замене/перекомпиляции бинаря** (обновление raxd), не работает на ФС без xattr, чистит ambient
  set, вероятный конфликт с `NoNewPrivileges=yes`. Хуже ambient для systemd-сервиса. Отклонено. →
  https://man7.org/linux/man-pages/man8/setcap.8.html
- Не выдавать capability вовсе: не выполняет AC7 для <1024 (годится лишь как поведение по умолчанию,
  и оно у нас и есть для ≥1024).

## Последствия
- Плюсы: capability выдаётся менеджером при старте, не зависит от xattr (переживает обновления);
  декларативно; сужаемо до порта; уживается с `User=raxd`; для дефолта — ноль привилегий.
- Минусы (цена выбора): механизм systemd-специфичный (Linux); при <1024 опускается `NoNewPrivileges`
  (узкое окно ослабления hardening только для редкого случая привилегированного порта).
- Безопасность: соответствует baseline §3 (capability вместо setuid root); условная генерация
  директивы + связка с NoNewPrivileges фиксируется в threat-model (security).

## Статус (proposed|accepted)
accepted
