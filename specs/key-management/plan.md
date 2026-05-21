# Plan: Key Management — управление API-ключами raxd

## Chosen Approach
Новый пакет `internal/keystore` инкапсулирует всё: генерацию, хранение, верификацию. Хранилище —
**плоский JSON-файл `keys.db` с атомарной записью** (temp в том же каталоге → `Chmod 0600` →
`Sync` → `Close` → `os.Rename` → `fsync` родительского каталога), per ADR-001/research: ноль новых
зависимостей при `CGO_ENABLED=0`, тривиально проверять права 0600 и повреждённый файл. Конкурентность
между CLI и daemon защищена ОБЯЗАТЕЛЬНОЙ advisory-flock на `keys.db` (stdlib `syscall.Flock`,
macOS+Linux): read-modify-write — под эксклюзивным flock, чтение — под shared flock. `Verify` —
ТОЛЬКО читающий (constant-time перебор активных записей, без перезаписи файла); `LastUsed`
буферизуется в памяти daemon и сбрасывается отложенно через `FlushUsage` (read-merge-write под
эксклюзивным flock), что исключает затирание `Revoke`. Тело ключа — 32 байта `crypto/rand` →
`base64.RawURLEncoding` → `rax_live_<…>` (D1); хранится `sha256(тело+per-key-salt)`+salt (baseline §1).
CLI-заглушки в `internal/cli/key.go` наполняются вызовами keystore.

## Modules
- `internal/keystore/keystore.go` — тип `Store`; методы `Create`/`List`/`Revoke`/`Verify`/`FlushUsage`;
  загрузка/атомарное сохранение JSON по пути `KeysDB`, права 0600, edge-case повреждения.
- `internal/keystore/lock.go` — advisory-flock над `keys.db` (`syscall.Flock`, эксклюзивный/shared);
  обёртка acquire/release вокруг каждого read-modify-write и чтения; поведение при конфликте.
- `internal/keystore/record.go` — тип `Record` (запись ключа) и `Database` (обёртка `{version, keys}`
  для JSON); JSON-теги; сериализация без секретов наружу.
- `internal/keystore/crypto.go` — генерация тела ключа, per-key-salt, id (с проверкой коллизий),
  `sha256(body+salt)`, fingerprint; идиома Go 1.24 `rand.Read` (без проверки err — сбой = краш).
- `internal/keystore/errors.go` — sentinel-ошибки (`ErrNotFound`, `ErrAlreadyRevoked`, `ErrCorrupt`,
  `ErrLabelTooLong`) для маппинга в CLI exit-коды и понятные сообщения.
- `internal/cli/key.go` — `RunE` для `create`/`list`/`delete`: разбор флагов, вывод, exit-коды
  (наполняются вместо `newStub`); таблицу стилизует cli-ux отдельно.

## Contracts
- `Open(path string) (*Store, error)` — открывает/создаёт `Store` по пути `KeysDB`. Отсутствующий
  файл = пустое хранилище (НЕ ошибка). Повреждённый/нечитаемый файл → `ErrCorrupt` без перезаписи.
- `(*Store) Create(label string) (PlainKey, Record, error)` — под ЭКСКЛЮЗИВНЫМ flock: read-modify-write.
  Генерирует ключ+id+salt, добавляет запись, атомарно сохраняет. `label` опционален; `len(label) > 64`
  → `ErrLabelTooLong`. Возвращает `PlainKey` (полное `rax_live_…`, для одноразового вывода) и `Record`.
  Коллизия id → внутренняя перегенерация. Ошибка записи → запись не применяется.
- `(*Store) List() ([]Record, error)` — под SHARED flock: активные записи (revoked скрыты), без
  секретов. Пустое/отсутствующее хранилище → пустой срез, nil-ошибка.
- `(*Store) Revoke(id string) error` — под ЭКСКЛЮЗИВНЫМ flock: read-modify-write. Ставит
  `Revoked=true`+`RevokedAt`, атомарно сохраняет. Неизвестный id → `ErrNotFound`; уже отозванный →
  `ErrAlreadyRevoked` (оба → ненулевой exit). Запись не удаляется (аудит).
- `(*Store) Verify(presented string) (Record, bool, error)` — экспортируемый контракт для будущих
  задач; ЧИСТО ЧИТАЮЩИЙ (под SHARED flock, файл НЕ перезаписывается). Перебирает активные записи,
  для каждой `subtle.ConstantTimeCompare(sha256(body+salt), hash)`; совпадение → буферизует `LastUsed`
  в памяти (in-memory, без диска) и возвращает `(rec, true, nil)`. Несовпадение/нет кандидатов →
  `(_, false, nil)`. `ErrCorrupt` пробрасывается. Constant-time на КАЖДОМ сравнении.
- `(*Store) FlushUsage() error` — под ЭКСКЛЮЗИВНЫМ flock: перечитывает файл, мерджит буфер `LastUsed`
  ПОВЕРХ актуального состояния (revoked-записям `LastUsed` не трогает → отзыв не теряется), атомарно
  сохраняет, очищает буфер. Вызывается daemon периодически и при graceful stop. Нет буфера → no-op.
- `Fingerprint(presented string) string` — короткий префикс `sha256(тело)` в hex (8-12 симв.) для
  аудита; без соли, не раскрывает ключ. Используется в аудит-записях create/delete.
- Типы: `Record{ID, Label string; Created, LastUsed, RevokedAt time.Time; Revoked bool; hash, salt
  []byte}` (hash/salt — неэкспортируемые, в JSON через теги); `PlainKey string`.
- Внутренние (без экспорта): `generateBody() string`, `generateSalt() []byte`, `generateID() string`,
  `hashKey(body string, salt []byte) []byte`.
- Аудит: при `Create`/`Revoke` CLI пишет структурную запись через `charmbracelet/log` как
  `slog.Handler` (STACK) на stderr демона/journald: `timestamp, action, id, fingerprint` — НЕ тело.
- CLI: `key create [--name]` → `Create`, печатает полный ключ один раз + предупреждение, exit 0;
  `key list` → `List`, таблица id/label/created/last-used («-» при пустом label, «ключей нет» при
  пустом), exit 0; `key delete <id>` → `Revoke`, exit≠0 на `ErrNotFound`/`ErrAlreadyRevoked`.

## Trade-offs
- Выбрали **плоский JSON+atomic write+flock** вместо **bbolt** — bbolt берёт эксклюзивную блокировку
  всего файла на всё время открытия, daemon заблокировал бы CLI (вис/таймаут). Наш flock держится
  только на время одной операции read-modify-write. Цена: нет транзакций/индексов, файл пишется
  целиком; конкуренция координируется advisory-flock (обязателен в v1, не «задел»).
- `Verify` снят с роли писателя: `LastUsed` не пишется на горячем пути, а буферизуется и
  сбрасывается через `FlushUsage` (read-merge-write под эксклюзивным flock). Цена: `last-used` в
  `key list` может отставать до интервала flush/перезапуска daemon; зато daemon остаётся читателем,
  отзыв `Revoke` не затирается параллельным `LastUsed`-апдейтом, файл не переписывается на каждый
  запрос. Альтернатива (запись LastUsed в Verify) отвергнута: last-write-wins ломал бы корректность отзыва.
- Durability: после `os.Rename` делаем `fsync` родительского каталога — гарантирует, что переименование
  переживёт сбой питания (на части FS rename не durable без fsync каталога). Цена: один доп. syscall на
  запись — пренебрежимо при редких операциях управления ключами.
- Верификация без lookup-id в самом ключе (формат остаётся `rax_live_<base64url>` per D1). Цена: O(n)
  активных записей на проверку; lookup-оптимизация отложена, потребует смены схемы (точка расширения).
- Отвергли **modernc.org/sqlite** — ~23 пакета зависимостей + привязка к `modernc.org/libc`; избыточно.
- Новых зависимостей НЕ вводим: всё на stdlib (`crypto/rand`, `crypto/sha256`, `crypto/subtle`,
  `encoding/json`, `encoding/base64`, `os`, `syscall`) + уже принятый `charmbracelet/log` из STACK.
