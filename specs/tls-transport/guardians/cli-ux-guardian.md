# Guardian Report: cli-ux-guardian — tls-transport

**Дата:** 2026-05-21
**Артефакт:** `specs/tls-transport/ux-spec.md` (вывод команды `raxd serve`)

## Раунд 1 — needs-changes

- ISSUE-1 (critical): макеты аудит-строк показывали авторский plain-формат (`AUTH fp:...`),
  которого `charmbracelet/log` не выдаёт — риск ввести developer в заблуждение.
- ISSUE-2 (major): sentinel-имена `ErrCorrupt`/`ErrTLSCert`/`ErrPortInUse` в заголовках/тексте —
  нарушение принципа 5 самой спеки.
- ISSUE-3 (minor): пустой keystore не связан с аудит FAIL/401.
- ISSUE-4 (minor): нет «None+обоснование» для install/status/key list (требование шаблона).

## Раунд 2 — pass

После правок cli-ux:
- Все 8 аудит-макетов раздела 3 переписаны в реальный формат `charmbracelet/log` key=value
  (`time=... level=INFO/WARN msg=AUTH/FAIL/DENY/RATE fp=... remote=... reason="..."`); `fp=-` для
  неаутентифицированных; таблица меток получила колонку level (INFO ok / WARN denied|rate). OQ-2
  закрыт `[РЕШЕНО]`.
- Grep `ErrCorrupt`/`ErrTLSCert`/`ErrPortInUse` → 0; описательные формулировки на месте.
- Пустой keystore (п.5.7) связан с аудит FAIL/401; добавлена заметка про install/status/key list
  (закрыты прежними спеками).
- Регрессий нет: каналы (stderr/stdout чист), маппинг 401/403/429/501 (SR-13), отсутствие секретов
  (fp 12 hex, не тело), тексты error:/hint:, автор — на месте; Go-кода нет.

Issues: нет.

## Verdict (раунд 2)
pass
