# Guardian Report: security-guardian — key-management

## Verdict: pass

## Summary
`threat-model.md` (6 активов, 13 рисков R1-R13, 3 остаточных) и `security-requirements.md` (25 требований SR-1..25) полностью покрывают SECURITY-BASELINE §1 по всем шести пунктам и относящуюся к локальной key-логике часть §4 (аудит create/delete без секретов). Каждый риск со смягчением, каждое требование проверяемо и привязано к baseline/plan. §2/§3/§5 и сетевые контроли §4 явно вынесены в «Вне scope»/ОР-3 (не тихий пропуск). Эскалация ОР-1 (best-effort memzero) оформлена согласно принятому дирижёром решению. Тихих ослаблений §1 нет.

## Checklist (§1 по пунктам)
- [x] п.1 генерация crypto/rand ≥128 бит, запрет math/rand, идиома rand.Read (R1; SR-1..5)
- [x] п.2 формат `rax_live_<base64url>` без padding (SR-6)
- [x] п.3 хранится только sha256(тело+per-key-salt)+salt (конкатенация), не открытый ключ (R2; SR-7,8)
- [x] п.4 сравнение только constant-time, запрет ==/EqualFold/bytes.Equal по секретам (R4; SR-9,10)
- [x] п.5 одноразовый показ; в list/логах/ошибках только id/label/fingerprint (R5; SR-11,12,13,15)
- [x] п.6 мгновенный отзыв; revoked немедленно неуспешен; FlushUsage не воскрешает revoked (R8,9; SR-16,17,18)
- [x] Хранилище: keys.db 0600, каталог 0700, atomic write (temp 0600 до rename), повреждён→ErrCorrupt без перезаписи, temp без утечки, flock (R10,11,12; SR-19..23)
- [x] Аудит §4: create/delete timestamp+id+fingerprint, не тело (R7; SR-24)
- [x] Каждое требование проверяемо; это требования, не код
- [x] memzero эскалирован (ОР-1, best-effort, принято дирижёром); сетевые контроли §4 → ОР-3 (передаваемое требование)
- [x] Русский язык

## Issues
- [ ] Nit (не блокирует): опечатка «corant-time» → «constant-time» в `threat-model.md` ОР-3. Косметика, §1 не ослабляет; поправить при следующем касании файла.

## Looks good
- Гонка отзыв↔flush проработана: Verify (read-only, shared flock, in-memory LastUsed) vs FlushUsage (read-merge-write, не трогает revoked) — закрывает «воскрешение» отозванного ключа.
- Хранение строго по baseline (конкатенация sha256(тело+salt)); запрет сериализации PlainKey; проверяемый тест «подстрока тела отсутствует в байтах keys.db».
- Честная эскалация ОР-1 (ограничение Go managed-memory/CGO_ENABLED=0; best-effort + компенсирующие контроли).

## Решение дирижёра по ОР-1
Best-effort затирание plaintext-ключа в памяти ПРИНЯТО для v1: детерминированный memzero в Go при CGO_ENABLED=0 невозможен; защита от swap делегируется ОС/контейнеру (baseline §6 обязывает Docker). Не блокер.

## Verdict
pass
