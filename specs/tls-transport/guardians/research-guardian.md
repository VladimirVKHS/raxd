# Guardian Report: research-guardian — tls-transport

**Дата:** 2026-05-21
**Артефакты:** `specs/tls-transport/research.md`, `decisions/ADR-001-http-tls-over-raw-tcp.md`,
`decisions/ADR-002-origin-validation-timing.md`

## Раунд 1 — needs-changes

- Issue 1 (medium): тезис про ECDSA P-256 подкреплён маркетинговыми ссылками SSL-ресейлеров
  (ssl.com, namesilo) — не авторитетный источник.
- Issue 2 (low): паттерн rate-limit `map[key]*rate.Limiter` без URL + висящая отсылка
  «см. варианты ниже» (раздела нет).
- Issue 3 (low): ложное «Открытые вопросы: None» при наличии неподтверждённого факта.

Блокирующих нарушений нет.

## Раунд 2 — pass

После правок research-analyst:
- Маркетинговые домены удалены; заменены на RFC 8446 (§4.2.7/§4.2.3), NIST SP 800-186 (final, 2023),
  Go docs (`crypto/ecdsa`, `crypto/x509`) — релевантны. Количественная часть «≈RSA-3072, быстрее
  handshake» честно вынесена в OQ-1 (не блокирует выбор кривой).
- Rate-limit: добавлен URL `pkg.go.dev/golang.org/x/time/rate`; висящая отсылка убрана; TTL-очистка
  карты лимитеров → детали реализации / OQ-2.
- «Открытые вопросы» содержат OQ-1, OQ-2 (нет ложного «None»).
- Регрессий нет: выводы Q1 (HTTP/TLS), Q2 (Origin-middleware сразу), Q3 (mTLS отложен) на месте;
  ADR полны (Контекст/Решение/Альтернативы/Последствия/Статус), статус `proposed` (финал за architect);
  контракт research-analyst соблюдён (факты с источниками, не выбирает архитектуру, нет кода).

Issues: нет.

## Verdict (раунд 2)
pass
