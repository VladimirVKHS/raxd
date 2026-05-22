# reviewer-guardian — задача `service-install`

**Verdict: pass.** Guardian (read-only). Дата: 2026-05-22. Сохранено дирижёром (red line 1).

Проверен `specs/service-install/review.md` (verdict accept) против контракта Reviewer и red lines.

## Подтверждено
- Все 16 AC в таблице review с конкретными доказательствами (file/тест/integration-шаг), не голословно;
  контракты plan.md обойдены. Пропусков нет.
- SR-83..96 покрыты; отклонения П-1/П-2/П-3 со ссылками на ADR; закрытие прежних ОР (command-exec/file-upload)
  зафиксировано; остаточные ОР-1..5 выписаны. Дыр не пропущено.
- **Выдуманных доказательств нет** — выборочно сверено 4 утверждения с кодом:
  (1) ConfigurationDirectory=raxd+Mode=0700 вне условного блока → в обоих вариантах (templates.go:196-200,
      TestRenderUnit_PrivilegedPort);
  (2) LIVE euid из /proc/<pid>/status (systemd.go readProcEUID:319-335, заполнение при pid>0&&active);
  (3) анти-инъекция: RenderUnit/RenderPlist первым делом ValidateTemplateData (templates.go:237/326),
      InjectionRejectedBeforeRender; векторов фактически 32 (review «24+» — консервативно, не дезинформация);
  (4) neutralizeStderr фикс. строка (templates.go:391-394) + реальный sentinel-тест (exec_test.go:54-72).
- Findings F-1..F-3 (info) реальны, severity корректна (не блокируют, не занижены): F-1 эквивалентные ветки
  mapExitCode; F-2 STEP3 2>&1 покрыт unit TestServiceStatus_OutputOnStdout; F-3 launchd enable `_ = err` —
  macOS вне Docker (ОР-4), RunAtLoad=true обеспечивает автозапуск.
- Хэндофф tech-writer конкретен (raxd после uninstall П-2, journald-ротация+пороги SR-94/П-3, macOS-ограничение
  AC13/ОР-4, capability ADR-003). Read-only соблюдён (review создан дирижёром). Язык русский.

## Незначительные неточности (не блокируют)
- review «24+ вектора» SR-90 — фактически 32 (в пользу консерватизма).
- F-3 ссылка launchd.go:88-91 — фактически 88-92; суть верна.

## Итог
pass — ревью корректно/полно/честно, verdict accept обоснован. Переход к tech-writer.
