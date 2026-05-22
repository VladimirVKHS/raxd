# security-guardian — задача `command-exec`

**Итоговый verdict: pass** (раунд 1). Самая опасная фича — планка максимальная. Сохранено
дирижёром (verifier не пишет сам).

Артефакты: `threat-model.md` (R-1..R-15, П-1..П-3, ОР-1..ОР-6) + `security-requirements.md`
(28 SR: SR-40..SR-67). Сверено с обязательным SECURITY-BASELINE §3/§4, spec (18 AC), plan,
ADR-001..004, реальным кодом.

## Покрытие baseline §3 — полное
exec.Command без shell (SR-43, тест-вектор `echo "a; touch /tmp/pwned"`), таймаут+max через context
(SR-46/66), allowlist строгий/выкл/документирован (SR-48), не-root + Credential не задаётся + детекция
euid==0 (SR-54/55/56), env-whitelist без LD_*/DYLD_*/IFS (SR-49), cwd валидация+дефолт /tmp (SR-50).
Доп.: ErrDot/PATH (SR-44/45), process-group kill (SR-47), DoS-лимиты (SR-51/52/53). Пробелов нет.

## Покрытие baseline §4 — полное
Аудит ровно одна запись/вызов со всеми полями (SR-57/58), машиночитаемость (SR-59/60), ротация
системная (SR-61, ОР-2), auth-fail/rate наследуются (SR-42), без секретов ключа/TLS (SR-62). Пробелов нет.

## 3 отклонения (red line 4) — обоснованы и смягчены
- **П-1 logfmt вместо JSON §4:** консистентность с key=value tls/mcp; условие — строгий LogfmtFormatter
  (не TextFormatter), ротация системная. Реализуемо (charmbracelet/log имеет LogfmtFormatter).
- **П-2 root WARN-дефолт + обязательный `exec.deny_root` (hard-fail, дефолт false):** УСИЛЕНИЕ
  baseline сверх ADR-003 (architect рекомендовал только WARN). Образцовое применение red line 4.
- **П-3 args дословно без маскирования:** надёжно отличить секрет в argv нельзя; маскирование =
  ложная безопасность; полнота args нужна для расследования RCE. Граница: секрет клиента в argv
  (ОР-4) ≠ ключ raxd/TLS (жёсткий инвариант SR-62). Компенсация: предупреждение в доке (tech-writer),
  ограниченный доступ к логу.

## Полнота модели угроз
Все классы покрыты: shell-инъекция, ErrDot/PATH, осиротевшие процессы, env-инъекция, allowlist+TOCTOU
(честно разобран), DoS вывод/argv/таймаут/форк-бомба (ОР-3 cgroups вне scope v1, честно), path traversal
cwd, эскалация root, утечка секретов. Упущенных классов нет. SR проверяемы, нумерация SR-40..67 без
конфликта с наследуемыми SR-1..39.

## Findings (НЕ блокирующие — для нижних ролей)
- **F-1 (Low) → DEVELOPER:** SR-60 требует LogfmtFormatter, но текущий `internal/cli/serve.go:81`
  `clog.New(stderr)` использует дефолтный **TextFormatter** (human-readable, не строго парсимый).
  Developer ДОЛЖЕН добавить `logger.SetFormatter(clog.LogfmtFormatter)` и перепрогнать наследуемые
  тесты (грепают `fp=`/`AUTH`/`MCP` — в logfmt сохраняются как подстроки, не сломаются).
- **F-2 (Info) → QA:** при LogfmtFormatter метка идёт как `msg=MCP`/`msg=AUTH`. Тест парсинга
  exec-записи (SR-60) должен это учитывать.
- **F-3 (Info) → TECH-WRITER:** при документировании allowlist (ОР-5) предупредить оператора об
  отсутствии нормализации путей/алиасов (`ls` ≠ `/bin/ls`).

## Что хорошо
Образцовая фиксация отклонений (раздел «Принятые отклонения» + зеркальные ОР с триггерами эскалации);
П-2 усиливает baseline; граница секретов R-12 vs R-13 точна; периметр tls/mcp не переоткрыт; все SR
сверены с реальным кодом (auth.go:36/56, audit.go, config.go viper, server.go AddTool, LogfmtFormatter
в vendor). security не написал код и не менял AC.

## Резюме
pass. Передаётся mcp-engineer → developer. Developer обязан учесть F-1 (LogfmtFormatter) и новое
требование SR-56 (`exec.deny_root` конфиг-поле).
