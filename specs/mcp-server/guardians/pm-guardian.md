# Guardian Report: pm-guardian — mcp-server

**Дата:** 2026-05-21
**Артефакт:** `specs/mcp-server/spec.md`

## Раунд 1 — needs-changes

- Issue 1: реализационные детали в Goal и разделе инструментов (имена `dispatchHandler`,
  `handlers.go`, точная цепочка `body-limit → recover → Host/Origin → auth → rate-limit → audit`) —
  нарушение «pm не принимает архитектурные решения».
- Issue 2: длина 117 строк > ориентира 80.

## Раунд 2 — pass

После правок pm:
- Имён функций/файлов и точной middleware-цепочки в spec нет; формулировки на уровне требований
  («за той же аутентификацией, Origin/Host-проверкой, rate-limit и аудитом — контракт tls-transport»).
  Раздел переименован в «Требуемые инструменты», обоснование инструментов вынесено в User Story.
- 15 AC по сути сохранены, измеримы (POST→не-501, без Bearer→401, ping→pong, аудит с fingerprint+
  имя+результат, и т.д.), покрывают error/edge cases и безопасность; Out of Scope (execute_command/
  upload_file/Resources/Prompts/mTLS/key-mgmt/service-install) явный; Q1-Q5 с дефолтами; AC15 —
  параметры подключения в docs; автор на месте.
- Длина 101 строка > 80, но обосновано явной фразой о неделимости задачи в Goal (прецедент
  tls-transport принят на 96); превышение — от полноты (15 AC + 5 OQ + детальный Out of Scope), не воды.

Issues: нет.

## Verdict (раунд 2)
pass
