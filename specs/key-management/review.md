# Review: key-management

Verdict (финал, после фиксов): **accept**.
Verdict (раунд 1): **needs-discussion** → дирижёр принял решение ИСПРАВИТЬ (см. ниже «Решение дирижёра»). Все замечания закрыты.

## Финальный verdict: accept (раунд 2)
Все три замечания устранены корректно (проверено по коду):
- Issue 1 (data race): `mu sync.Mutex` защищает usageBuf; Verify пишет под mu (LastUsed из локальной ts); FlushUsage snapshot-паттерн (короткие критические секции, file I/O вне mu под exclusive flock); deadlock невозможен (однонаправленный порядок mu/flock); restore не перетирает свежие concurrent-значения и не трогает revoked; §1 цел. Покрыт TestConcurrentVerifyMixWithFlush + -race в Docker test-target (prod build CGO_ENABLED=0). Контракт «safe to use concurrently» теперь истинен.
- Issue 2: utf8.RuneCountInString>64 + multibyte-тесты (AC/D4 «≤64 символов»).
- Issue 3: friendlyErr удалён, единый printStoreError.
- Мелочи: Open→json.Unmarshal (trailing garbage); ux-тексты ErrNotFound/ErrAlreadyRevoked с id+hint по ux-spec; 0 skip.
Регрессий ранее подтверждённого нет. Готовность как фундамента: Verify/Fingerprint — потокобезопасные локальные контракты для tls/mcp.
Остаточные необязательные: go.mod держит lipgloss v1.1.0 (indirect; на key-management не влияет — отметить для cli-ux/tls).

---


Реализация по существу соответствует spec, plan, security-requirements и SECURITY-BASELINE §1/§4. Все 12 AC и SR-1..25 закрыты фактическим кодом (проверено чтением). Crypto-инварианты выдержаны строго. Verdict `needs-discussion` из-за одного содержательного расхождения между кодом и его контрактом потокобезопасности (Issue 1), влияющего на готовность `Verify` как фундамента для daemon (tls/mcp).

## Подтверждено по коду
- §1 генерация: generateBody 32 байта crypto/rand, salt 16, id 8 с проверкой коллизий; math/rand отсутствует (grep + TestStaticNoMathRand); идиома rand.Read без err-check, без fallback.
- Формат rax_live_<base64url> (RawURLEncoding); keyPrefix склеен (легитимно).
- Хранение: сериализуется только dbRecord (Hash=sha256(presented‖salt)+Salt); PlainKey не оседает; hash/salt неэкспортируемы. Тесты NoPlaintextInDB/HashSchemeDirectVerification/HashSizeInDB/ListRecordHasNoHashOrSalt.
- Сравнение: только subtle.ConstantTimeCompare, перебор всех активных без раннего break; ==/EqualFold/bytes.Equal по секретам нет.
- Отзыв: soft-revoke, запись хранится; Verify исключает revoked → немедленная неуспешность; FlushUsage пропускает revoked (не воскрешает).
- Хранилище: keys.db 0600; atomic write temp→chmod 0600→write→sync→close→rename→fsync каталога; temp чистится на error-путях; corrupt→ErrCorrupt без перезаписи байт-в-байт; flock exclusive(Create/Revoke/FlushUsage)/shared(List/Verify), release в defer.
- Вывод: тело ключа только на stdout при create; list/ошибки/аудит без тела; revoked скрыты; delete→revoked; error:/hint: строчные; exit-коды.
- Аудит: charmbracelet/log через cmd.ErrOrStderr() с action/id/fingerprint без тела; delete-fingerprint из персистентного rec.Fingerprint (12 hex sha256(body), необратим, не ослабляет §1).
- errors.Is для обёрнутых sentinel; зависимости из STACK; Dockerfile target test (go vet+test, golang:1.25, CGO_ENABLED=0).

## Issues
### Issue 1 (MAJOR / needs-discussion → ИСПРАВИТЬ): data race на usageBuf в Verify
- Где: `internal/keystore/keystore.go:15-25` (док-комментарий о потокобезопасности) и `:218-220` (`s.usageBuf[matched.ID]=...` в Verify).
- Почему: Verify под shared flock пишет в общий map без синхронизации (в пакете нет Mutex/atomic). Конкурентные Verify (горячий путь daemon tls/mcp) → concurrent map write (паника/повреждение). Противоречит контракту типа Store («safe to use concurrently») и plan (Verify — переиспользуемый контракт). Тест TestConcurrentCreateAndList не покрывает конкурентный Verify; Docker-target без -race.
- Что делать: защитить usageBuf мьютексом в Verify/FlushUsage (файловый flock оставить), либо явно убрать обещание потокобезопасности и переложить на сетевую задачу; в любом случае добавить тест конкурентного Verify под -race.

### Issue 2 (MINOR): валидация label в байтах, а не символах
- Где: `internal/keystore/keystore.go:59` (`len(label) > 64`).
- Почему: AC/D4/ux говорят «64 символа»; len() — байты; multibyte (кириллица/эмодзи) отвергаются раньше. Тесты только ASCII.
- Что делать: `utf8.RuneCountInString(label) > 64`; тест с multibyte меткой 64 символа (>64 байт) должен проходить.

### Issue 3 (MINOR): мёртвый код printStoreError, friendlyErr
- Где: `internal/cli/key.go:91-112`, `:378-384` — объявлены, не вызываются.
- Что делать: удалить или перевести обработчики на единый printStoreError (уберёт дублирование 4 блоков ErrCorrupt).

## Остаточные мелочи (необязательные)
- Open (json.NewDecoder.Decode) vs readDB (json.Unmarshal): «валидный JSON + хвост» пройдёт Open, упадёт ErrCorrupt на операции; инвариант SR-22 сохраняется. Желательно унифицировать на json.Unmarshal.
- TestSaltUniqueness слабее имени (реальную уникальность закрывает TestSaltLengthAndUniqueness). Доусилить/переименовать.
- go.mod: lipgloss v1.1.0 (indirect) — на key-management не влияет (lipgloss не подключается); отметить для tls/cli-ux (STACK = v2 charm.land).

## Готовность как фундамента
Verify/Fingerprint — экспортируемые локальные контракты без сетевой части, пригодны для tls/mcp. Оговорка — Issue 1 (потокобезопасность Verify).

## Решение дирижёра
needs-discussion разрешён: Issue 1 ИСПРАВЛЯЕТСЯ сейчас (мьютекс на usageBuf + -race тест), т.к. док-контракт обещает потокобезопасность и сетевые задачи зависят от конкурентного Verify. Issue 2, 3 и унификация corruption — также чинятся. После фиксов — повторный reviewer → accept.
