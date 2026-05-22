# pm-guardian — задача `file-upload`

**Итоговый verdict: pass** (раунд 2). Раунд 1 — needs-changes (Issue 1/2 MEDIUM + 3/4 LOW), все
закрыты. Опасная фича (запись в ФС хоста). Сохранено дирижёром.

## Раунд 1 — needs-changes

Артефакт: spec.md (19 AC). Сверено с контрактом pm, SECURITY-BASELINE §4, реальным кодом
(middleware.go bodyLimit, config.go, audit.go, keystore.go, mcp/).

## Покрытие безопасности — полное
path traversal (..//абсолютные/симлинки наружу) AC4; запись только в upload root AC4/5; лимит
размера AC7/15; overwrite дефолт false AC8; права mode 0600+наследование UID AC9; атомарность
temp→rename AC10; root-WARN AC11; аудит без содержимого AC12/13; наследование auth/rate-limit AC16/17;
невалидный base64 AC6; edge-cases AC18. Угрозы §3/§4 покрыты.

## Развилка base64/bodyLimit — корректна, сверена с кодом
max_body_bytes=1MiB (config.go:126), bodyLimitMiddleware первым в цепочке (server.go:117,
middleware.go:74-80). AC15 верно: base64 +33%, потолок (max_body_bytes−overhead)×3/4, 413 ДО
инструмента; большие файлы в Out of Scope; числа в Q4 для architect. Реалистично.

## Findings
- **Issue 1 (MEDIUM).** Реализационные детали в AC — нарушение red line pm. AC1 называет
  sdkmcp.AddTool + internal/mcp/server.go; AC10 называет keystore.writeDB как обязательную модель;
  секция «Зависимости от готового кода» описывает сигнатуры/пакеты/FingerprintFromContext. Это уровень
  plan.md. Fix: AC1 — поведенчески («только через MCP, без эндпоинтов/CLI»); AC10 — поведенчески
  («при ошибке до фиксации файл не появляется, temp удаляется»); секцию «Зависимости» убрать или
  вынести в context.md «справочно для architect».
- **Issue 2 (MEDIUM).** AC12 требует поля аудита path/size, которых нет в AuditRecord (audit.go:13-49 —
  только Tool/Command/Args/ExitCode/Duration/TimedOut). Неоднозначность для architect/developer. Fix:
  либо явно «поля path/size добавляются в AuditRecord как часть задачи (не заполняются для не-upload
  записей, по аналогии с exec)», либо убрать перечисление и сформулировать поведенчески («запись
  содержит достаточно для расследования: операция, путь, размер, результат, remote»), отдав формат
  architect/security.
- **Issue 3 (LOW).** AC5 содержит два требования (безопасный дефолт корня + создание промежуточных
  каталогов). Разбить на AC5a/AC5b.
- **Issue 4 (LOW).** AC11 тест euid==0 неоднозначен в Docker (AC19). Уточнить: тест в контейнере с
  демоном от root без --user.

## Что хорошо
base64/bodyLimit сверена с кодом и с формулой; политические вопросы делегированы architect/security
через Open Questions (не додуманы); AC18 edge-cases исчерпывающий; Out of Scope точечно отрезает
download/каталоги/стриминг/спецфайлы/chown.

## Резюме для pm
Issue 1 + Issue 2 (MEDIUM, обязательно) + Issue 3/4 (LOW). После — повторный гейт.

## Раунд 2 — pass
spec.md = 20 AC (AC1-AC20 с AC5a/AC5b).
- Issue 1 закрыт: AC1/AC10 поведенческие (нет sdkmcp.AddTool/keystore.writeDB); секция «Зависимости»
  очищена, конкретика в specs/file-upload/context.md «НЕ контракт». Grep sdkmcp/AddTool/writeDB/
  keystore/internal/ в spec.md — 0.
- Issue 2 закрыт: AC12 поведенческий (timestamp/fingerprint/путь/размер/результат/remote без
  содержимого); способ представления → Open Questions Q5 (не блокирует).
- Issue 3 закрыт: AC5→AC5a (безопасный дефолт корня) + AC5b (автосоздание подкаталогов).
- Issue 4 закрыт: AC11 уточнён (демон от root euid==0 в Docker без --user).
Перенумерация AC1-AC20 согласована, перекрёстные ссылки целы, покрытие безопасности сохранено
(§1/§3/§4/§6), язык русский. Новых нарушений нет. Передаётся research-analyst → architect.
