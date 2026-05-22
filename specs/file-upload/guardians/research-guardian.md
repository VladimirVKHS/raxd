# research-guardian — задача `file-upload`

**Итоговый verdict: pass** (раунд 2). Раунд 1 — needs-changes (#1 MAJOR os.Root Go 1.25 + #2/#3/#4
MINOR), все закрыты. Опасная фича. Сохранено дирижёром.

## Раунд 1 — needs-changes

Артефакты: research.md (Q1-Q10) + ADR-001 (os.Root traversal-safe), ADR-002 (atomic write + perms).

## Покрытие вопросов
Все 10 вопросов исследованы (path traversal, симлинки/TOCTOU, os.Root, атомарность, права/umask,
base64, MkdirAll, перезапись, root-детекция, аудит) с вариантами A/B/C и рекомендациями. AC1/17/18/20
корректно не дублированы (готовый код/транспорт).

## Достоверность os.Root в Go 1.25 — критический риск
os.Root/os.OpenRoot в Go 1.24 — подтверждено (go.dev/blog/osroot). НО конкретные методы Go 1.25
(Root.MkdirAll/Rename/WriteFile/Chmod/RemoveAll/Readlink) — приведены со ссылкой на go.dev/doc/go1.25
БЕЗ прямой цитаты release notes, URL на исходник (raw.githubusercontent .../go1.25.0/src/os/root.go)
сконструирован, не подтверждён. go.mod = go 1.25.0 (согласуется, но не доказывает наличие методов).
Вся безопасность (AC4/AC5b/AC9/AC10) и оба ADR стоят на этом факте.

## Findings
- **#1 (MAJOR).** Методы Root.MkdirAll/Rename/WriteFile/Chmod в Go 1.25 без прямой цитаты release notes;
  фундамент всего решения. Контракт research: каждый факт с URL, неподтверждённое → Открытые вопросы.
  Fix: верифицировать через WebFetch go.dev/doc/go1.25 (+ pkg.go.dev/os@go1.25.0#Root), вставить
  прямую цитату с перечнем методов; если не подтверждается — Открытый вопрос с fallback (os.Root базовый
  1.24 + ручной MkdirAll/Chmod).
- **#2 (MINOR).** Q10 ссылается на internal/server/audit.go и ADR-004 без URL — для фактов о коде
  репозитория явно указать «прочитан internal/server/audit.go».
- **#3 (MINOR).** «Открытые вопросы: None блокирующих» противоречит риску #1 — если неопределённость
  остаётся, вынести явно.
- **#4 (MINOR).** «Root.CreateTemp не существует» — утверждение об отсутствии тоже требует ссылки
  (pkg.go.dev/os@go1.25.0#Root перечень методов).

## ADR-оценка
ADR-001 и ADR-002 структурно полные (контекст→решение→альтернативы(4)→последствия→статус proposed),
нюансы (Root.Chmod-race обойдён chmod по fd; mount points вне гарантий; umask-срезание) проработаны.
Оба зависят от факта #1 — при подтверждении безупречны.

## Что хорошо
URL-дисциплина в большинстве фактов (CWE-22, filepath, osroot blog, man7); явная проработка нюансов
безопасности; граница research/architect соблюдена (proposed, числа делегированы); вывод «новых
зависимостей нет» обоснован (openat2/x-sys отклонён мотивированно); консистентность с keystore/
command-exec паттернами; кода нет; язык русский.

## Резюме для research-analyst
Закрыть #1 (MAJOR, верификация os.Root Go 1.25 методов через WebFetch + цитата/fallback) + #2/#3/#4.
После — повторный гейт.

## Раунд 2 — pass
- #1 закрыт: Q3 + ADR-001 содержат дословную цитату release notes go.dev/doc/go1.25 («The Root type
  supports the following additional methods:» + Chmod/Chown/Chtimes/Lchown/Link/MkdirAll/ReadFile/
  Readlink/RemoveAll/Rename/Symlink/WriteFile) + Index pkg.go.dev/os@go1.25.0#Root, «верифицировано
  WebFetch 2026-05-22». Нужные для ADR методы (OpenFile/Create/MkdirAll/Mkdir/Rename/Remove/Stat/Lstat/
  Chmod) подтверждены, fallback не нужен, сконструированный raw URL убран.
- #4 закрыт: Root.CreateTemp отсутствует — подтверждено Index («NO CreateTemp»).
- #2 закрыт: Q10 факты помечены «прочитан internal/server/audit.go».
- #3 закрыт: Открытые вопросы честны (#1 ЗАКРЫТ, остаются threat-model заметки + SDK additionalProperties).
Достоверность достаточна для architect (2 офиц. источника, дата, цитаты). ADR-001/002 proposed,
числа делегированы. Зависимости не требуются. Передаётся architect.
