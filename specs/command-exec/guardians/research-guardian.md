# research-guardian — задача `command-exec`

**Итоговый verdict: pass** (раунд 2). Раунд 1 — needs-changes (2 MEDIUM + 1 LOW), все закрыты.
Сохранено дирижёром (verifier не пишет сам).

## Раунд 1 — needs-changes

Артефакты: `research.md` + ADR-001 (process-group-kill), ADR-002 (audit logfmt + системная ротация).
Проверено против контракта research-analyst, SECURITY-BASELINE §3/§4, ограничений (Go 1.25 stdlib,
вендоринг, без новых зависимостей).

### Покрытие вопросов
Все 9 вопросов spec/baseline освещены с URL (pkg.go.dev, CWE, man7.org). Сравнение вариантов
A/B/C + рекомендация по каждому. Архитектура за architect не выбрана, политические развилки
делегированы. ADR валидны (контекст→решение→альтернативы→последствия, статус proposed).

### Достоверность ключевых фактов (сверено, верно)
`exec.CommandContext` убивает только головной процесс; `Cmd.Cancel`/`WaitDelay` (Go 1.20);
`exec.ErrDot` (Go 1.19); `Setpgid` Linux+darwin, `Pdeathsig` только Linux; `Kill(-pgid)` бьёт
группу; charmbracelet/log имеет Logfmt/JSON/Text форматтеры, ротации нет; `os.Geteuid()` кросс;
`Cmd.Env=nil` наследует окружение. Новые зависимости не требуются — обосновано.

### Findings
- **MEDIUM.** Факт «`additionalProperties:false` выводится из struct в go-sdk v1.6.0 по умолчанию»
  (Q9, ~стр.193) дан без точного URL примера/исходника. Если неверно — AC3 (лишнее поле→ошибка)
  не закрыт автоматически. Добавить прямую ссылку (пример/строка кода SDK) ИЛИ перенести в
  Открытые вопросы «требует проверки при реализации».
- **MEDIUM.** baseline §4 буквально требует аудит «Структурно (JSON), с ротацией». ADR-002 выбирает
  logfmt (key=value) — это отклонение от буквы baseline, не оформленное явно. По red line 4
  отклонения от SECURITY-BASELINE фиксируются осознанно (в идеале → threat-model security). В
  ADR-002 (и Q8 research) добавить ЦИТАТУ §4 и явно зафиксировать logfmt как сознательное
  отклонение с обоснованием (консистентность с уже существующим key=value аудитом tls/mcp;
  структурно и машиночитаемо; финальное принятие — за security/architect в threat-model).
- **LOW.** Не описан класс «аргументной DoS»: `exec.Command` принимает `[]string args` без лимита;
  ARG_MAX ядра возвращает E2BIG только при exec, не как прикладная защита. Зафиксировать класс
  угрозы и рекомендовать architect ввести конфигурируемые `max_args`/`max_arg_len`
  (man7 execve.2).

### Что хорошо
Дисциплина источников (каждый факт с URL); чёткое разграничение research/architect (ADR proposed,
политические решения делегированы); детальный разбор взаимодействия Setpgid+Cancel+WaitDelay.

### Резюме для research-analyst
Закрыть 2 MEDIUM + 1 LOW. Особенно — явная фиксация отклонения от baseline §4 (red line 4).
После — повторный гейт.

## Раунд 2 — pass
- MEDIUM-1 — закрыт: Q9 — два URL (jsonschema-go Inference «disallow additionalProperties» +
  Issue #892 «AddTool sets additionalProperties:false»), оговорка о настраиваемости дефолта +
  открытый вопрос Q9-impl-check (закрепить AC3 тестом).
- MEDIUM-2 — закрыт: Q8 + ADR-002 — точная цитата baseline §4 «Структурно (JSON), с ротацией»,
  явный статус «ОТКЛОНЕНИЕ», обоснование (консистентность key=value, машиночитаемость, JSON
  фрагментирует/нарушает AC14), принятие делегировано security (threat-model) + architect.
  Red line 4 соблюдена — research не принял отклонение сам.
- LOW-3 — закрыт: новый Q10 — класс аргументной DoS, источники os/exec + man7 execve.2 (E2BIG/
  ARG_MAX), рекомендация конфигурируемых max_args/max_arg_len без чисел.
Новых findings нет, противоречий между research.md и ADR-002 нет, новые зависимости не требуются.
Передаётся architect.
