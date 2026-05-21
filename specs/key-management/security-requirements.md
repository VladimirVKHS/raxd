# Security Requirements: Key Management — управление API-ключами raxd

> Каждое требование ПРОВЕРЯЕМО (ответ «выполнено/нет» одним вопросом: тест / grep / инспекция) и
> ссылается на пункт `SECURITY-BASELINE.ru.md` и на контракт `plan.md`. Эти требования ОБЯЗАНЫ
> выполнить `developer` (логика `internal/keystore`, CLI `internal/cli/key.go`) и `qa` (тесты).
> Соответствие проверяют `reviewer` и `security-guardian`.
>
> Scope: ЛОКАЛЬНАЯ key-логика + хранилище + `Verify` как контракт. Транспорт/TLS (§2), выполнение
> команд (§3) и СЕТЕВЫЕ контроли §4 (rate limiting, аудит сетевых запросов) — ВНЕ scope этой задачи
> (см. раздел «Вне scope» ниже и threat-model ОР-3). Способ проверки везде: тесты гоняются в Docker
> (baseline §6).

## Генерация ключей (baseline §1)

- [ ] **SR-1. Тело ключа генерируется только `crypto/rand`, ≥128 бит.** В `internal/keystore`
  (`crypto.go`, `generateBody`) тело — ≥16 байт из `crypto/rand` (план: 32 байта). Проверка: тест на
  длину/энтропию тела; grep по пакету `internal/keystore` НЕ находит импорт `math/rand`. (baseline §1
  «Генерация: crypto/rand, ≥128 бит; никогда math/rand»; plan: тело 32 байта crypto/rand → D1)

- [ ] **SR-2. `math/rand` отсутствует во всей key-логике.** Ни тело, ни salt, ни id не используют
  `math/rand`/`math/rand/v2`. Проверка: grep `math/rand` по `internal/keystore` и `internal/cli/key.go`
  = 0 совпадений. (baseline §1)

- [ ] **SR-3. Сбой источника энтропии не «глотается» и не подменяется.** Используется идиома Go 1.24
  `rand.Read` (ошибки нет; при сбое источника процесс аварийно завершается). Запрещено: добавлять
  fallback на `math/rand`, игнорировать/подменять контракт `rand.Read`, оборачивать его так, чтобы
  при недоступности энтропии выпускался слабый ключ. Проверка: инспекция `crypto.go` — нет
  fallback-ветки на не-CSPRNG при сбое генерации. (baseline §1; plan/research Q2: rand.Read, сбой=краш)

- [ ] **SR-4. per-key-salt из `crypto/rand`, ≥16 байт.** `generateSalt` берёт ≥16 байт (128 бит) из
  `crypto/rand` на КАЖДУЮ запись (уникальная соль на ключ). Проверка: тест на длину соли и на
  различие солей у двух разных ключей. (baseline §1 «+ сам salt»; plan: per-key-salt 16 байт crypto/rand)

- [ ] **SR-5. id записи генерируется `crypto/rand` и не производен от секрета.** `generateID` — 8 байт
  `crypto/rand` → hex/base32 (D5); id НЕ выводится из тела/хэша ключа; при коллизии id в хранилище —
  внутренняя перегенерация. Проверка: инспекция `crypto.go` (id не зависит от body/hash); тест на
  уникальность id и на перегенерацию при коллизии. (baseline §1; spec D5)

## Формат и хранение (baseline §1)

- [ ] **SR-6. Формат ключа `rax_live_<base64url>` без padding.** Тело кодируется
  `base64.RawURLEncoding`; полный ключ = `rax_live_` + base64url. Проверка: тест на префикс и на
  отсутствие padding `=`. (baseline §1 «префикс + base64url/hex тела»; spec D1)

- [ ] **SR-7. В `keys.db` НЕ хранится тело ключа — только `sha256(тело+per-key-salt)` и salt.**
  Сериализуемый `Database`/`Record` содержит `hash`=`sha256(тело+per-key-salt)` (конкатенация
  key+salt per baseline §1) и `salt`; тело/`PlainKey` НЕ сериализуется. Поля `hash`/`salt` —
  неэкспортируемые, в JSON только через явные теги; `PlainKey` не входит в `Record`/`Database`.
  Проверка: тест — подстрока тела ВЫПУЩЕННОГО ключа отсутствует в байтах файла `keys.db` (как в
  spec AC). (baseline §1 «хранить sha256(key+per-key-salt) + salt, не открытый ключ»; plan Contracts/Типы)

- [ ] **SR-8. Хэш считается по схеме baseline (key+salt), SHA-256.** `hashKey(body, salt)` =
  `sha256(тело ‖ salt)`; bcrypt/argon2 НЕ применяются (избыточны при ≥128 бит энтропии, research Q3).
  Проверка: инспекция `crypto.go` — конкатенация тела и соли перед `sha256`; тест воспроизводит
  хэш по сохранённым телу+соли. (baseline §1)

## Сравнение секретов (baseline §1)

- [ ] **SR-9. Сравнение хэшей/секретов ТОЛЬКО constant-time.** В `Verify` каждое сравнение —
  `crypto/subtle.ConstantTimeCompare` или `hmac.Equal` над хэшами фиксированной длины (32 байта, длина
  не «протекает»). Проверка: инспекция `Verify` использует `subtle.ConstantTimeCompare`/`hmac.Equal`.
  (baseline §1 «только constant-time»; plan Verify «constant-time на КАЖДОМ сравнении»)

- [ ] **SR-10. `==`/`EqualFold`/`bytes.Equal` по секретам/хэшам отсутствуют.** Запрет сравнения тела
  ключа, хэша или соли через `==`, `strings.EqualFold`, `bytes.Equal`. Проверка: grep по
  `internal/keystore` — нет сравнений секретных/хэш-значений небезопасными операторами; ревью
  подтверждает. (baseline §1 «никаких ==/EqualFold по секретам»)

## Одноразовый показ и неутечка секрета (baseline §1, §4)

- [ ] **SR-11. Полный ключ показывается РОВНО один раз при `key create`.** `Create` возвращает
  `PlainKey`; CLI печатает его в stdout один раз + предупреждение, что повторно получить нельзя.
  Проверка: тест/инспекция — `create` печатает полный ключ один раз; нет иного пути получить тело.
  (baseline §1 «показывается один раз при key create»; spec AC; plan CLI)

- [ ] **SR-12. `key list` и контракт `List` не раскрывают секрет.** `List` возвращает только
  id/label/created/last-used (revoked скрыты); ни тела, ни хэша, ни соли. Пустой/отсутствующий
  файл → пустой результат, «ключей нет», exit 0. Проверка: тест — вывод `list` не содержит
  тело/хэш/соль; тест на пустое хранилище. (baseline §1 «в key list — только id/label/метаданные»;
  plan List; spec AC)

- [ ] **SR-13. Тело/хэш/соль не попадают в логи и сообщения ошибок.** Sentinel-ошибки
  (`ErrNotFound`/`ErrAlreadyRevoked`/`ErrCorrupt`/`ErrLabelTooLong`) и любые сообщения оперируют
  id/label/fingerprint, но НЕ телом/хэшем/солью. Проверка: тест/grep — текст ошибок и аудит-вывод не
  содержат тело/хэш/соль. (baseline §4 «никаких секретов в логах/выводе CLI»; spec AC)

- [ ] **SR-14. Тело ключа не передаётся через аргументы процесса/окружение.** Ни одна команда
  key-логики не принимает тело ключа как позиционный аргумент или флаг и не пишет его в env (защита
  от утечки через `ps`/`/proc`). Проверка: инспекция `internal/cli/key.go` — `create` не принимает
  тело на вход, `delete` принимает `<id>` (не секрет). (baseline §1, §4; threat-model R6)

- [ ] **SR-15. fingerprint не раскрывает ключ.** `Fingerprint(presented)` = короткий префикс
  `sha256(тело)` в hex (8-12 симв., без соли) — для аудита/идентификации, не позволяет восстановить
  ключ при ≥128 бит энтропии. Проверка: тест — fingerprint детерминирован для одного тела, его длина
  ≤12 симв., он не равен телу/полному хэшу. (baseline §1/§4; plan Fingerprint)

## Отзыв (baseline §1)

- [ ] **SR-16. Отзыв мгновенный (soft-revoke); revoked немедленно неуспешен в `Verify`.** `Revoke(id)`
  ставит `Revoked=true`+`RevokedAt` (запись сохраняется для аудита) и атомарно фиксирует; `Verify`
  перебирает только активные → ранее предъявленное значение отозванного ключа сразу неуспешно.
  Проверка: тест — `Verify` успешен ДО `Revoke` и неуспешен СРАЗУ после. (baseline §1 «отзыв
  мгновенный, дальнейшие запросы → отказ»; spec D3/AC; plan Revoke/Verify)

- [ ] **SR-17. `FlushUsage` не «воскрешает» и не перезаписывает revoked.** `Verify` чисто читающий
  (`LastUsed` только буферизуется в памяти, файл не пишет, shared flock); `FlushUsage` под
  эксклюзивным flock перечитывает актуальный файл и мерджит `LastUsed` ПОВЕРХ, НЕ трогая `LastUsed`
  у revoked-записей → отзыв не теряется. Проверка: тест — `Revoke(id)` + буферизованный `LastUsed` по
  этому id + `FlushUsage` оставляет ключ revoked и `Verify` по-прежнему неуспешен. (baseline §1;
  plan Verify/FlushUsage/Trade-offs; threat-model R8)

- [ ] **SR-18. Повторный/несуществующий `delete` — понятная ошибка и ненулевой код.** Уже отозванный
  id → `ErrAlreadyRevoked`; неизвестный id → `ErrNotFound`; оба → exit≠0; запись не удаляется.
  Проверка: тест на оба случая (exit≠0, сообщение без секрета). (baseline §1; spec AC; plan Revoke)

## Хранилище на диске (baseline §1)

- [ ] **SR-19. Файл `keys.db` создаётся с правами `0600` по пути `KeysDB`.** Используется
  `internal/config.PathSet.KeysDB`; права файла `0600`; каталог `StateDir` `0700` (создаётся
  каркасом, не расширяется). Проверка: тест — `os.Stat(keys.db).Mode().Perm() == 0600`; каталог не
  становится шире `0700`. (baseline §1; spec AC; STACK «keys.db 0600»)

- [ ] **SR-20. Атомарная запись без окна расширенных прав.** Запись: temp В ТОМ ЖЕ каталоге →
  `Chmod 0600` ДО записи содержимого → `Sync` → `Close` → `os.Rename` → `fsync` родительского
  каталога. Нет окна, когда temp/целевой файл имеет права шире `0600`. Проверка: тест — права temp на
  момент записи `0600`; после успешной записи `keys.db` `0600`. (baseline §1; plan Chosen Approach/
  Trade-offs Durability)

- [ ] **SR-21. temp-файлы не «текут» с материалом ключа.** В temp пишется только `Database` (хэш+соль,
  без тела/`PlainKey`); при ошибке записи temp удаляется (не остаётся на диске). Проверка: тест —
  после смоделированной ошибки записи temp-файлы в каталоге отсутствуют; тело ключа в temp не
  встречается. (baseline §1; threat-model R11)

- [ ] **SR-22. Повреждённый/нечитаемый файл → `ErrCorrupt` без паники и без перезаписи.** Парсинг
  битого `keys.db` возвращает `ErrCorrupt`; исходный файл НЕ перезаписывается; нет паники.
  Отсутствующий файл = пустое хранилище (не ошибка) для `List`/`Verify`. Проверка: тест — подсунутый
  битый файл → `ErrCorrupt`, файл байт-в-байт не изменён; отсутствующий файл → пустой результат.
  (baseline §1; spec AC; plan Open Contract)

- [ ] **SR-23. flock корректен и не виснет/не теряется.** read-modify-write (`Create`/`Revoke`/
  `FlushUsage`) под ЭКСКЛЮЗИВНЫМ flock; чтение (`List`/`Verify`) под SHARED flock; lock держится
  только на время операции и освобождается всегда (в т.ч. при ошибке). Проверка: инспекция
  `lock.go` (acquire/release вокруг каждой операции, release в defer); тест — параллельные операции
  не повреждают файл. (baseline §1; plan lock.go/Trade-offs)

## Аудит (baseline §1, §4)

- [ ] **SR-24. `create` и `delete` порождают аудит-запись без тела ключа.** Структурная запись
  `timestamp, action(create|delete), id, fingerprint` через `charmbracelet/log` (slog-handler) на
  stderr демона/journald. В аудит НЕ пишется тело/хэш/соль. Проверка: тест — для create и delete
  присутствует запись с timestamp+id+fingerprint; grep по выводу аудита не находит тело/хэш/соль.
  (baseline §1 «аудит create/delete: timestamp+id+fingerprint, не тело»; §4 «структурно, без
  секретов»; spec AC; plan Аудит)

## Память (baseline §1, §4)

- [ ] **SR-25. Время жизни plaintext-ключа минимизировано (best-effort).** `PlainKey` генерируется,
  отдаётся для одноразового вывода и НЕ сохраняется в долгоживущих полях `Store`/`Record`/глобалах;
  не пишется в логи/файлы/temp/аргументы. Ограничение: детерминированное затирание в памяти Go
  невыполнимо (managed-память/GC, `CGO_ENABLED=0`) — фиксируется честно как best-effort, не как
  гарантия (см. threat-model ОР-1, эскалация). Проверка: инспекция — `PlainKey` не оседает в полях
  структур хранилища; SR-13/SR-14/SR-21 подтверждают, что тело не уходит в лог/arg/temp. (baseline
  §1, §4)

## Вне scope этой задачи (фиксация, не требование к key-management)

- TLS/сертификаты/`MinVersion`/bind-интерфейс (baseline §2) — задача `tls-transport`.
- `exec.Command` без shell, таймауты, allowlist, запуск не от root (baseline §3) — `command-exec`/
  `system-dev`.
- СЕТЕВЫЕ контроли §4: rate limiting per-key/per-IP + 429, аудит сетевых запросов (удалённый адрес,
  команда+аргументы, exit, длительность), обнаружение всплеска отказов аутентификации, graceful
  restart — задачи `command-exec`/`mcp-server`/`tls-transport`/`system-dev`. Передаётся как
  обязательное требование в их threat-model (см. threat-model ОР-3).
- Install-скрипт/`SHA256SUMS`/подпись-нотаризация (baseline §5) — задача `distribution`.
- Среда Docker для сборки/тестов/запуска (baseline §6) — операционное требование ко ВСЕМ
  builder-ролям; тесты этой задачи обязаны быть зелёными в Docker (spec AC).
