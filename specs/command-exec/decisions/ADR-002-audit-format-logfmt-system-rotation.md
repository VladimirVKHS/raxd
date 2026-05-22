# ADR-002: Формат аудита команды — LogfmtFormatter (key=value), ротация — системная

## Контекст
Каждый вызов `execute_command` должен писать машиночитаемую структурированную аудит-запись через
существующий аудит-механизм (AC13/AC14 spec.md), содержащую timestamp(UTC), fingerprint ключа,
имя инструмента, команду+аргументы, exit code, длительность, remote, результат — **без секретов**
(AC15). Текущий аудит (`internal/server/audit.go`) пишет через `charmbracelet/log` парами key/value,
но логгер использует **дефолтный TextFormatter** (human-readable, styling при TTY), а структура
`AuditRecord` НЕ содержит полей команды/exit/duration. AC14 требует «парсится как структурированная
запись» и «не ломает формат не-`execute_command` записей». Транспорт (tls-transport) и mcp-server
**уже пишут аудит в формате key=value**. Ограничение: без новых внешних зависимостей (вендоринг,
offline Docker).

### Расхождение с буквой SECURITY-BASELINE §4 (red line 4)
Baseline §4 буквально требует: «Аудит-лог КАЖДОГО действия: timestamp, fingerprint ключа (не сам
ключ), команда+аргументы, exit code, длительность, удалённый адрес. **Структурно (JSON), с
ротацией.**» (→ `.claude/reference/SECURITY-BASELINE.ru.md` §4). Решение ниже выбирает **logfmt
(key=value), а НЕ JSON** — это **сознательное ОТКЛОНЕНИЕ от буквы baseline §4**. Дух требования
(«структурно», машиночитаемо, с ротацией) сохраняется: logfmt — стандартный структурный парсимый
формат; меняется лишь сериализация (logfmt вместо JSON). Согласно red line 4 («Безопасность не
опциональна … любое отступление — через эскалацию и фиксацию в `threat-model.md`») **окончательное
принятие этого отклонения — за ролью security (запись в `threat-model.md`: риск + почему +
смягчение) и architect.** Этот ADR лишь поднимает вопрос и рекомендует; решение принимает
security/architect, не research.

## Решение
1. **Формат:** рекомендуется переключить аудит-логгер на `log.LogfmtFormatter` (строгий,
   машиночитаемый, структурный logfmt key=value). Это тот же визуальный key=value-стиль, что уже в
   коде/ux-spec и в аудите транспорта/mcp-server, но строго парсимый. Форматтер встроен в уже
   вендоренный `charmbracelet/log` — новых зависимостей нет; вызовы `logger.Info(..., k, v)` менять
   не нужно. **Это отклонение от буквы baseline §4 «JSON» — см. раздел Контекст; принимает
   security/architect (red line 4).**
2. **Поля команды:** добавить в `AuditRecord` новые опциональные поля (команда+args, exit code,
   duration, timed_out/result), логируемые **только когда заполнены** — по аналогии с тем, как
   `tool=` пишется лишь при `Tool!=""`. Это сохраняет формат не-`execute_command` записей
   (auth/deny/rate) неизменным (AC14). Команда+args не содержат секретов; вместо ключа — fingerprint
   (AC15).
3. **Ротация:** в коде НЕ реализовывать. Вывод аудита — в stderr; ротацию обеспечивает система
   (journald для systemd / logrotate при файловом выводе), как предписывает STACK.ru.md («системный
   журнал + ротация при файловом выводе»). Это устраняет потребность в новой зависимости
   (`lumberjack`).

## Альтернативы
- **Остаться на TextFormatter (default).** Отвергнут: human-readable со styling при TTY → не
  гарантирует строгий машинный парсинг (AC14). Источник:
  https://pkg.go.dev/github.com/charmbracelet/log , https://github.com/charmbracelet/log/blob/main/README.md
- **JSONFormatter (буквально по baseline §4).** Плюс: **буквально соответствует baseline §4 «JSON»**,
  максимально машиночитаем. Минусы: смена форматтера **глобальна** и меняет формат ВСЕХ записей
  (auth/deny/rate), уже пишущихся в key=value → конфликт с AC14 «не ломать формат
  не-`execute_command` записей», **фрагментация исторического формата аудита** продукта и риск
  регрессий в уже зашлюзованном mcp-server/транспорте. logfmt ближе к текущему формату и менее
  разрушителен. **Если security настаивает на букве «JSON» — это валидный выбор ценой указанных
  минусов.** Источник: https://pkg.go.dev/github.com/charmbracelet/log
- **Ротация через `gopkg.in/natefinch/lumberjack.v2` (rolling io.Writer).** Отвергнут: **новая
  внешняя зависимость** → противоречит вендорингу/offline-Docker (STACK). Системная ротация
  (journald/logrotate) даёт тот же результат без зависимости. Источник:
  https://pkg.go.dev/gopkg.in/natefinch/lumberjack.v2 ,
  https://www.freedesktop.org/software/systemd/man/latest/journald.conf.html ,
  https://man7.org/linux/man-pages/man8/logrotate.8.html

## Последствия
- Плюсы: строго машиночитаемый структурный аудит (AC14) без новых зависимостей; формат не-MCP
  записей не ломается (опциональные поля); единый формат аудита продукта (key=value) сохраняется;
  ротация делегирована рантайму/системе (соответствует STACK и baseline §4 «ротация при файловом
  выводе»); для контейнерного демона (baseline §6) stderr — естественный канал.
- Минусы/цена: **отклонение от буквы baseline §4 «JSON»** — требует явного утверждения security
  (фиксация в `threat-model.md`) и architect (red line 4); цветной человекочитаемый вывод в консоли
  заменяется на logfmt (для аудита машиночитаемость важнее «красоты»); при чисто файловом выводе без
  journald нужен внешний конфиг `logrotate` (забота задачи `distribution`).
- Влияние на стек: смена форматтера затрагивает **инициализацию логгера в транспортном слое**
  (вне scope command-exec по коду) → **точка координации с architect**: возможно, провести как
  сквозное решение, а не локально в command-exec. Сверка со STACK.ru.md: `charmbracelet/log`
  остаётся, новых зависимостей нет.

## Статус (proposed|accepted)
accepted — ратифицирован гейтами security-guardian + architect-guardian (command-exec), 2026-05-22.
Финальное принятие отклонения П-1 (logfmt вместо JSON) зафиксировано security в
`specs/command-exec/threat-model.md` (раздел «Принятые отклонения»).
