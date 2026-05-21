# Guardian Report: reviewer-guardian — tls-transport

**Дата:** 2026-05-21
**Артефакт:** `specs/tls-transport/review.md` (раунд 1 needs-changes + раунд 2 accept)

## Итог

Ревью охватывает все 14 AC (spec.md), значимые SR (security-requirements.md) и контракты plan.md.
Находки конкретны (файл:строка, ссылка на SR/AC/baseline, «что делать»). Verdict честен:
- Раунд 1 `needs-changes` — по двум РЕАЛЬНЫМ дефектам безопасности с воспроизводимыми примерами:
  ISSUE-1 (SR-16, Origin subdomain-bypass через `HasPrefix`), ISSUE-2 (SR-25, нет `MaxBytesReader`).
- Раунд 2 `accept` — выдан ПОСЛЕ фактического закрытия блокеров, не «проштампован».

Выборочно сверено guardian'ом по коду (accept не фальшивый):
- ISSUE-1: `middleware.go:158-170` — `url.Parse`+`u.Hostname()`+точное case-insensitive `contains`; HasPrefix убран.
- ISSUE-2: `config.go:61,91` (`MaxBodyBytes`, дефолт 1 MiB) + `server.go:101` + `middleware.go:74-81` (`http.MaxBytesReader`).
- ISSUE-3: `auth.go:103-108` (AUTH не пишется в authMiddleware) + `auth.go:126-140` (`authSuccessAuditMiddleware`) + `server.go:84` (innermost).

Reviewer не правил код (read-only); не блокировал на стиле; уважает Out of Scope (command-exec/mcp/
file-upload/mTLS/systemd не вменены как дефекты). Минор (TestMaxBodyBytesDefault проверяет хелпер, а
не config.Load; ADR-002 формально proposed) честно зафиксирован без раздувания в блокер.

Issues: нет.

## Verdict
pass
