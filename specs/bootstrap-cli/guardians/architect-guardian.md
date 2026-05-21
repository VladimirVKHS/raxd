# Guardian Report: architect-guardian — bootstrap-cli

## Summary
План `specs/bootstrap-cli/plan.md` качественный и зрелый: выбран ровно один подход, модули заданы конкретными путями с зонами ответственности, контракты команд однозначны (вывод, exit-коды, флаги), все Resolved Decisions реализованы, находка по lipgloss зафиксирована корректно как отложенная с правильным путём `charm.land/lipgloss/v2` и эскалацией STACK. Зависимости — только из STACK, новых нет. Блокирующих нарушений не найдено. Verdict — pass; два минорных info-наблюдения, возврата не требуют.

## Checklist
- [x] Выбран РОВНО ОДИН подход (раскладка ADR-001), без «вариантов на выбор».
- [x] Modules указывают конкретные пути файлов/пакетов и ответственность.
- [x] Contracts содержат сигнатуры с типами и обработкой ошибок.
- [x] В плане НЕТ тел функций.
- [x] Acceptance criteria НЕ изменены.
- [x] Новых зависимостей нет; используемые сверены со STACK (cobra v1.10.2 + pflag ≥1.0.9, viper v1.21.0, adrg/xdg v0.5.3).
- [x] Trade-offs называют цену (plain-баннер vs lipgloss, cmd/internal vs плоский, ldflags vs ReadBuildInfo, single- vs multi-stage Docker).
- [x] Длина 55 строк (коридор 30-100). Русский язык.
- [x] Каждый AC покрыт элементом плана; scope не расширен.
- [x] Заглушки → ненулевой код; version/status → exit 0.
- [x] Resolved Decisions реализованы (D1-D4).
- [x] Безопасность каркаса: 0700/0600 в EnsureDirs, без секретов, Dockerfile §6, баннер на stderr.
- [x] Находка lipgloss зафиксирована (отложен; путь charm.land/lipgloss/v2; STACK к синхронизации).

## Покрытие AC (сводка)
AC1→cmd/raxd+Dockerfile; AC2→internal/*; AC3→internal/cli/{root,key,config,serve,version,status}.go; AC4→stub.go+serve.go; AC5→version.Set/Info; AC6→status; AC7→config.Paths/Load; AC8→config.EnsureDirs; AC9→banner.Render; AC10→только TODO-границы; AC11→Dockerfile golang:1.25; AC12→тестируемость заложена.

## Issues
Блокирующих и needs-changes нет. Info-наблюдения:
- Наблюдение 1 (info): AC12 (юнит-тесты) — пишет developer/qa; план закладывает тестируемость (чистые сигнатуры Info(), Paths(), newStub, Execute() error). Действие architect не требуется.
- Наблюдение 2 (info): SECURITY-BASELINE §3 «демон не от root» в плане не отражён даже как будущий TODO. Корректно, т.к. реальный serve/сервис — Out of Scope (service-install). Возврат не нужен; опционально пометить «непривилегированный запуск — граница service-install».

## Looks good
- config.Paths() точно реализует D3: явное построение $HOME/.config/raxd с приоритетом XDG_CONFIG_HOME, без macOS-дефолта adrg/xdg.
- Находка lipgloss отработана образцово (API banner.Render() сохранён; путь charm.land/lipgloss/v2; рассинхрон STACK эскалирован).
- Разделение stdout/stderr (баннер на stderr, машиночитаемый status на stdout) — продуманное решение.

## Verdict
pass
