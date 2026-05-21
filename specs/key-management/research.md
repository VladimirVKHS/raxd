# Research: Key Management — хранение, генерация и верификация API-ключей raxd

> Scope строго локальный: логика ключей + хранение/верификация. Сеть/TLS/MCP/exec — вне scope
> (задачи `tls-transport`, `mcp-server`, `command-exec`). Вход: `spec.md` (AC + D1-D5),
> `SECURITY-BASELINE.ru.md §1/§4`, `STACK.ru.md` (CGO_ENABLED=0, раскладка keys.db).
> Каждый факт — с URL. Версии проверены через WebFetch/WebSearch (май 2026), не по памяти.

## Вопросы
- Q1: Формат хранилища `keys.db` при `CGO_ENABLED=0`. Сравнить применимые pure-Go варианты:
  плоский JSON (atomic write), `go.etcd.io/bbolt`, `modernc.org/sqlite`. Критерии: отсутствие CGO,
  простота, атомарность/устойчивость к повреждению, конкурентный/многопроцессный доступ, размер
  зависимостей. Рекомендация для небольшого набора ключей.
- Q2: Генерация тела ключа: `crypto/rand`, сколько байт для ≥128 бит, `base64.RawURLEncoding` (D1).
  Идиомы и подводные камни (поведение ошибок в Go 1.24+).
- Q3: Хэш + соль: `sha256(key + per-key-salt)` (конкатенация, baseline §1), генерация per-key-salt,
  длина соли. Сравнение secret'ов — `crypto/subtle.ConstantTimeCompare`/`hmac.Equal`. Подтвердить,
  что SHA-256 (не bcrypt/argon2) уместен при высокой энтропии ключа.
- Q4: Идентификатор записи (D5): короткий случайный id (8 байт → hex/base32); коллизии; кодировка.
  Fingerprint ключа для аудита (что это, устоявшаяся практика).
- Q5: Аудит create/delete локально: `slog` (stdlib) vs `charmbracelet/log` (STACK); что и куда писать.
- Q6: Верификация по предъявленному значению без знания id (паттерн lookup) — контракт для будущих
  задач (`Authorization: Bearer rax_live_…`), без сетевой части.

## Найдено (факт → источник URL)

### Q1 — варианты хранилища (pure Go, CGO_ENABLED=0)

- **Плоский файл JSON + atomic write.** Идиома: писать во временный файл В ТОМ ЖЕ каталоге, сделать
  `Chmod 0600`, `Sync()`, закрыть, затем `os.Rename` — на POSIX `rename(2)` атомарно заменяет
  целевой файл, читатель видит либо старый, либо новый файл, но не частичный. Temp обязан быть в том
  же каталоге (иначе rename через FS становится copy и теряет атомарность). Кодек — stdlib
  `encoding/json`, без внешних зависимостей.
  → https://michael.stapelberg.ch/posts/2017-01-28-golang_atomically_writing/ (актуальная идиома,
  POSIX rename), https://pkg.go.dev/github.com/google/renameio (готовая обёртка, по умолчанию 0600).
  `encoding/json` — stdlib: https://pkg.go.dev/encoding/json

- **`go.etcd.io/bbolt`** — pure Go key/value (B+tree), без CGO. Текущая версия **v1.4.3**, стабильна,
  активно сопровождается etcd-io, лицензия MIT. ACID, crash-safe: незавершённые транзакции
  откатываются при сбое; two-phase commit + контрольные суммы на meta-страницах защищают от частичной
  записи. Модель параллелизма: одна read-write транзакция + сколько угодно read-only одновременно.
  → https://pkg.go.dev/go.etcd.io/bbolt , https://github.com/etcd-io/bbolt/blob/main/README.md
  - **КРИТИЧНО для нашего сценария:** bbolt берёт ЭКСКЛЮЗИВНУЮ блокировку файла — несколько
    процессов НЕ могут открыть одну БД одновременно. Открытие уже открытой БД ВИСИТ, пока другой
    процесс не закроет; по умолчанию ждёт бесконечно (можно ограничить `Options{Timeout: …}`).
    → https://github.com/etcd-io/bbolt/blob/main/README.md (раздел "Opening a database")
  - Следствие: `raxd serve` (daemon) и `raxd key create` (CLI) — РАЗНЫЕ процессы; если daemon держит
    keys.db открытой, CLI-команда повиснет/упадёт по таймауту. Это серьёзный минус для bbolt в нашей
    топологии (CLI + долгоживущий daemon на одном файле).

- **`modernc.org/sqlite`** — CGo-free порт SQLite3 (транспиляция C→Go через ccgo), CGO не нужен.
  Текущая версия **v1.50.1** (10 мая 2026), стабильна, production-ready, BSD-3-Clause, используется
  3500+ проектами (спонсор — Tailscale). Платформы покрывают linux/{amd64,arm64} и darwin/{amd64,arm64}.
  → https://pkg.go.dev/modernc.org/sqlite
  - Зависит от `modernc.org/libc` — требуется ТОЧНО та же версия libc, что в go.mod драйвера
    (issue cznic/sqlite#177). Импортирует ~23 пакета — самый «тяжёлый» по дереву зависимостей вариант.
    → https://pkg.go.dev/modernc.org/sqlite
  - Многопроцессность: SQLite штатно поддерживает несколько процессов через файловые блокировки
    pager'а; рекомендуется WAL-режим (читатели не блокируют писателя) и `PRAGMA busy_timeout` для
    смягчения `database is locked`. В драйвере прагмы задаются через DSN, напр.
    `file:./x.sqlite?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)`.
    → https://sqlite.org/wal.html , https://sqlite.org/lockingv3.html ,
    https://riverqueue.com/docs/sqlite (пример DSN для modernc.org/sqlite)

### Q2 — генерация ключа (`crypto/rand`)

- `crypto/rand` — CSPRNG (stdlib). ≥128 бит = ≥16 байт. baseline §1 и spec AC требуют ≥16 байт;
  для запаса допустимо 32 байта (256 бит). `math/rand` запрещён.
  → https://pkg.go.dev/crypto/rand , baseline §1.
- **Подводный камень (Go 1.24+):** `rand.Read(b)` теперь гарантированно НЕ возвращает ошибку — всегда
  отдаёт `nil` как error и полностью заполняет `b`; при невозможности получить энтропию (ошибка от
  `Reader`) программа НЕОБРАТИМО АВАРИЙНО ЗАВЕРШАЕТСЯ. Это подтверждено дословно: release notes Go 1.24
  — «The Read function is now guaranteed not to fail. It will always return nil as the error result.
  If Read were to encounter an error while reading from Reader, the program will irrecoverably crash.»
  И документация функции: «It never returns an error, and always fills b entirely. Read calls
  io.ReadFull on Reader and crashes the program irrecoverably if an error is returned.» То есть
  привычная идиома «проверь err от rand.Read» устарела: ошибки нет, но сбой = краш процесса.
  (Исключение по докам: legacy Linux < 3.17, где дефолтный Reader открывает `/dev/urandom`.)
  → https://go.dev/doc/go1.24 (раздел crypto/rand) , https://pkg.go.dev/crypto/rand#Read

- Кодирование тела (D1): `base64.RawURLEncoding` — base64url БЕЗ padding, URL-safe (подходит для
  заголовка `Authorization: Bearer`). stdlib `encoding/base64`.
  → https://pkg.go.dev/encoding/base64
- **Альтернатива/нюанс:** в Go 1.24 добавлен `rand.Text()` — возвращает криптослучайную строку в
  алфавите RFC 4648 base32 (26 символов, ≥128 бит). Это удобный готовый генератор токенов, НО его
  алфавит — base32, а D1 фиксирует base64url. Поэтому для ТЕЛА ключа `rand.Text` НЕ подходит без
  отступления от D1; зато он — идеальный кандидат для генерации `id` записи (см. Q4), если выбрать
  base32-вид id.
  → https://pkg.go.dev/crypto/rand (Text), https://go.dev/src/crypto/rand/text.go ,
  https://github.com/golang/go/issues/67057

### Q3 — хэш + соль + constant-time

- Схема baseline §1: хранить `sha256(key + per-key-salt)` и сам salt (конкатенация key+salt, НЕ
  открытый ключ). per-key-salt генерируется `crypto/rand`; общепринятая длина соли — 16 байт (128 бит),
  достаточно для уникальности на запись. → baseline §1; soль = просто случайные байты из crypto/rand:
  https://pkg.go.dev/crypto/rand
- **SHA-256 уместен (не bcrypt/argon2) при высокой энтропии ключа** — подтверждено независимым
  авторитетным источником: «While SHA-256 is unsuitable for user passwords, because the secret has
  120 bits of entropy and already unguessable as is, we can use a fast hashing algorithm here…
  an offline brute-force attack is impossible.» Безопасность даёт непредсказуемость токена, а не
  медленность хэша. Тело нашего ключа ≥128 бит → схема корректна.
  → https://lucia-auth.com/sessions/basic
  Дополнительно практики API-key подтверждают: хранить только SHA-256-хэш, bcrypt/argon2 избыточны
  для high-entropy ключей. → https://oneuptime.com/blog/post/2026-02-20-api-key-management-best-practices/view ,
  https://zuplo.com/learning-center/how-to-implement-api-key-authentication
  ВАЖНЫЙ нюанс: OWASP **Password Storage** Cheat Sheet прямо называет SHA-256 НЕпригодным — но это про
  ПАРОЛИ (низкая энтропия). Для high-entropy токенов он неприменим; не цитировать его как запрет на
  нашу схему. → https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html
- Сравнение секретов — ТОЛЬКО constant-time:
  - `subtle.ConstantTimeCompare(x, y []byte) int` → 1 при равенстве, 0 иначе; при РАЗНОЙ длине
    возвращает 0 СРАЗУ (длина может «протечь» по таймингу — но мы сравниваем хэши фиксированной
    длины 32 байта, поэтому длины всегда равны). → https://pkg.go.dev/crypto/subtle
  - `hmac.Equal(mac1, mac2 []byte) bool` — сравнивает без утечки тайминга; семантически
    предназначен для MAC/хэшей. → https://pkg.go.dev/crypto/hmac
  - Любой `==`/`strings.EqualFold` по секретам = timing-атака (атакующий восстанавливает значение
    побайтово). → https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html ,
    https://en.wikipedia.org/wiki/Timing_attack

### Q4 — id записи (D5) и fingerprint

- D5: отдельный короткий случайный id из `crypto/rand`, напр. 8 байт. Кодировки:
  - 8 байт → hex = 16 символов (вид `a1b2c3d4e5f60718`); алфавит `0-9a-f`, без коллизий по кодеку.
  - 8 байт → base32 (RFC4648, без padding) = 13 символов; компактнее, без спецсимволов.
  → https://pkg.go.dev/encoding/hex , https://pkg.go.dev/encoding/base32
  - Коллизии: 8 байт = 64 бита; для небольшого набора ключей (десятки-сотни) вероятность коллизии
    пренебрежимо мала (день рождения: ~50% при ~2^32 ≈ 4 млрд записей). Для надёжности — при create
    проверять отсутствие id в хранилище и при коллизии перегенерировать (дёшево).
  - `rand.Text()` (Go 1.24) — готовый base32-генератор ≥128 бит; можно взять префикс для id, но проще
    сгенерировать ровно нужные байты и закодировать. → https://pkg.go.dev/crypto/rand
  - Spec явно запрещает использовать тело/хэш ключа как id (id не должен быть производным от секрета).
- **Fingerprint ключа для аудита** — устоявшаяся практика: короткий необорачиваемый идентификатор
  ключа для логов, обычно ПРЕФИКС sha256-хэша в hex (или префикс самого ключа `myapi_k7Hj…`).
  Назначение — отличать ключи в журнале, не раскрывая секрет. В логи пишут id/префикс/fingerprint,
  НИКОГДА не сам ключ.
  → https://oneuptime.com/blog/post/2026-02-20-api-key-management-best-practices/view (key_prefix,
  «log the key prefix or a key ID instead»), https://zuplo.com/learning-center/how-to-implement-api-key-authentication
  Для raxd корректный fingerprint = короткий префикс `sha256(тело_ключа)` в hex (напр. 8-12 hex-символов).
  Замечание: это hash БЕЗ соли (для стабильной идентификации в разных журналах); сам по себе fingerprint
  не позволяет восстановить ключ при ≥128 бит энтропии.

### Q5 — аудит create/delete (локально)

- `log/slog` — stdlib с Go 1.21, штатное структурное логирование; `slog.NewJSONHandler(w, …)` даёт
  JSON; вывод в любой `io.Writer` (файл/stdout). → https://pkg.go.dev/log/slog
- `charmbracelet/log` (из STACK) — v2.0.0 (9 мар 2026), MIT; умеет работать КАК `slog.Handler`
  (передаёшь logger в `Slog`), поддерживает структурные key-value, JSON-форматтер, любой `io.Writer`.
  → https://github.com/charmbracelet/log
- Что писать (baseline §1/§4): timestamp, id, fingerprint ключа (НЕ тело), действие (create/delete),
  результат. Куда: для локальных операций — структурный JSON в системный журнал (journald/syslog
  через stderr демона) либо файл с ротацией; полноценный СЕТЕВОЙ аудит (удалённый адрес, команда,
  exit code, rate-limit) — вне scope, в `command-exec`/`mcp-server`. → baseline §4.

### Q6 — верификация по предъявленному значению (без знания id)

- Задача: дано `rax_live_<base64url>`, найти запись и проверить, не зная id заранее (для будущего
  `Authorization: Bearer`). Так как хранится `sha256(key+salt)` с РАЗНОЙ солью на запись, нельзя
  посчитать один хэш и сделать прямой lookup по индексу — соль у каждой записи своя.
- Каноничный паттерн server-side verification c per-key salt: перебрать активные (не revoked) записи,
  для каждой посчитать `sha256(предъявленное_тело + запись.salt)` и сравнить через
  `subtle.ConstantTimeCompare`/`hmac.Equal` с сохранённым хэшем; совпадение = успех. Перебор линейный
  по числу ключей — приемлемо для небольшого набора (spec: жёсткого лимита нет, но набор мал).
  Constant-time применяется к КАЖДОМУ сравнению (не `==`). → https://pkg.go.dev/crypto/subtle ,
  https://pkg.go.dev/crypto/hmac , https://lucia-auth.com/sessions/basic
- Оптимизация для будущего (НЕ обязательна сейчас): чтобы избежать линейного перебора, индустрия
  использует «искомый» компонент — встраивает в сам ключ публичный lookup-id (напр.
  `rax_live_<id>_<secret>`) ИЛИ хранит дополнительно бессолевой `sha256(тело)` как индекс, по нему
  делает прямой lookup, затем подтверждает солёным хэшем constant-time. Это меняет формат ключа/схему
  хранения и относится к решению architect.
  → https://oneuptime.com/blog/post/2026-02-20-api-key-management-best-practices/view (key_prefix как
  lookup), https://zuplo.com/learning-center/how-to-implement-api-key-authentication
- После `delete` (revoked) запись исключается из кандидатов перебора → верификация немедленно
  неуспешна (соответствует D3 и AC). → spec D3.

## Варианты (хранилище keys.db) — A/B/C

- **A: Плоский JSON + atomic write (temp+rename, 0600).**
  - Плюсы: ноль внешних зависимостей (stdlib `encoding/json` + `os`), минимум кода и дерева; полностью
    pure-Go при CGO_ENABLED=0; атомарная замена POSIX-rename даёт устойчивость к частичной записи;
    тривиально проверять права 0600 и edge-case «повреждённый файл» (parse error → понятная ошибка,
    исходный файл не перезаписывается); естественно подходит малому набору ключей; нет
    межпроцессных блокировок-сюрпризов.
  - Минусы: нет встроенных транзакций/индексов; конкурентная запись из двух процессов требует ручной
    координации (но для key-management запись делает только CLI, daemon в основном читает — конфликт
    маловероятен; при необходимости — file lock через `flock`); весь файл читается/пишется целиком.
  - Источники: https://michael.stapelberg.ch/posts/2017-01-28-golang_atomically_writing/ ,
    https://pkg.go.dev/github.com/google/renameio , https://pkg.go.dev/encoding/json

- **B: `go.etcd.io/bbolt` v1.4.3.**
  - Плюсы: pure-Go, ACID, crash-safe, зрелая (etcd), один файл 0600, хорош для большого числа записей.
  - Минусы: **эксклюзивная блокировка файла — два процесса не открывают БД одновременно**; daemon,
    держащий keys.db, заблокирует CLI-команды (вис/таймаут). Для топологии «CLI + долгоживущий daemon
    на одном файле» это архитектурный конфликт. Лишняя зависимость ради малого набора ключей —
    избыточно. Бинарный формат сложнее инспектировать/чинить вручную.
  - Источники: https://pkg.go.dev/go.etcd.io/bbolt ,
    https://github.com/etcd-io/bbolt/blob/main/README.md

- **C: `modernc.org/sqlite` v1.50.1 (CGo-free).**
  - Плюсы: pure-Go без CGO; SQL, индексы, транзакции; штатная многопроцессность (WAL + busy_timeout);
    легко делать прямой lookup по индексу (полезно для Q6-оптимизации).
  - Минусы: самое тяжёлое дерево зависимостей (~23 пакета + жёсткая привязка версии `modernc.org/libc`);
    избыточно для нескольких ключей; нужно настраивать WAL/busy_timeout и помнить про прагмы на каждое
    соединение; больше поверхности для ошибок ради простой задачи.
  - Источники: https://pkg.go.dev/modernc.org/sqlite , https://sqlite.org/wal.html ,
    https://riverqueue.com/docs/sqlite

## Рекомендация
Для key-management (малый набор ключей, простая схема записей, требования 0600 + атомарность +
устойчивость к повреждению) рекомендую **вариант A — плоский JSON с атомарной записью (temp+rename,
0600)**. Он минимизирует зависимости (важно при CGO_ENABLED=0 и простой дистрибуции из STACK),
тривиально удовлетворяет AC по правам/повреждённому файлу и не вносит межпроцессных блокировок,
которые у bbolt прямо конфликтуют с топологией «CLI + daemon». `modernc.org/sqlite` оставить как
запасной путь, если позже понадобятся индексы/большой объём/прямой lookup для верификации (Q6) —
тогда выигрыш SQL оправдает вес зависимостей. bbolt НЕ рекомендую из-за эксклюзивной файловой
блокировки. Это рекомендация для architect; финальный выбор формата сериализации и схемы — за ним
(spec явно относит выбор формата к architect/research).

Сопутствующие рекомендации:
- Тело ключа: 32 байта `crypto/rand` (запас над 16) → `base64.RawURLEncoding` → `rax_live_<…>` (D1).
  Учесть Go 1.24+ контракт `rand.Read` (нет err; при сбое процесс аварийно завершается).
- id записи: 8 байт `crypto/rand` → hex (16 симв.) либо base32; проверять коллизию при create.
  Не выводить id из секрета.
- Хэш: `sha256(тело + per-key-salt)`, salt = 16 байт crypto/rand; сравнение только constant-time
  (`subtle.ConstantTimeCompare`/`hmac.Equal`). SHA-256 здесь корректен (≥128 бит энтропии).
- Аудит: `slog` JSON (или charmbracelet/log как slog-handler из STACK) — timestamp/id/fingerprint,
  без тела ключа.
- Верификация без id: линейный перебор активных записей с per-key-salt и constant-time сравнением;
  оптимизацию через lookup-индекс оставить решению architect (меняет формат/схему).

## Открытые вопросы
- None — все факты подтверждены источниками. Решение по lookup-оптимизации верификации (Q6) и
  финальный выбор хранилища сознательно оставлены architect (это его зона по spec/Out of Scope).
