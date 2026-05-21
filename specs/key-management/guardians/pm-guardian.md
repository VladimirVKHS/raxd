# Guardian Report: pm-guardian — key-management

## Итог
Финальный verdict: **pass** (после одного раунда правок).

## История
- Раунд 1: needs-changes — (1) пробел безопасности: нет AC про аудит операций create/delete (timestamp+fingerprint, не тело ключа); (2) AC хранения хэша содержал неоднозначную схему — привязать к baseline §1.
- Раунд 2 (после правок + закрытия Open Questions): pass.

## Checklist (финал)
- [x] Все обязательные секции на месте и непустые.
- [x] Блоков кода нет; архитектурных решений (формат хранилища) нет — в Out of Scope.
- [x] Каждый AC проверяем.
- [x] Все 6 применимых пунктов SECURITY-BASELINE §1 покрыты: crypto/rand ≥128 бит; формат `rax_live_<base64url>`; sha256(key+per-key-salt)+salt (не открытый ключ); constant-time; одноразовый показ + list только метаданные; мгновенный отзыв; + аудит create/delete (§1/§4).
- [x] Out of Scope отрезает tls-transport/mcp-server/command-exec/rate-limiting/distribution/cli-ux/выбор формата хранилища.
- [x] Open Questions = None; Resolved Decisions D1-D5.
- [x] Длина 89 строк (чуть выше нормы из-за Resolved Decisions — оправдано). Русский.

## Resolved Decisions (зафиксированы дирижёром)
- D1 — тело ключа base64url без padding (RawURLEncoding), формат `rax_live_<base64url>`.
- D2 — label опционален, дубликаты разрешены (уникален id); пустой label → «-».
- D3 — delete = мягкий отзыв (revoked), верификация немедленно неуспешна, revoked скрыты в list, запись хранится для аудита.
- D4 — label ≤ 64 символов; жёсткого лимита на число ключей в v1 нет.
- D5 — отдельный короткий случайный id (8 байт crypto/rand → hex/base32, вид `abc123de`); тело/хэш как id не использовать.

## Verdict (финал)
pass
