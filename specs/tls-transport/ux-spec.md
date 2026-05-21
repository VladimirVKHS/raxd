# UX Spec: TLS Transport — консольный вывод `raxd serve`

> Контракт для: `developer` (реализует вывод `internal/cli/serve.go`, `internal/server/*`).
> Проверяет: `cli-ux-guardian`.
> Все тексты CLI — английские. Язык артефакта — русский.
> Согласован с: `specs/bootstrap-cli/ux-spec.md`, `specs/key-management/ux-spec.md` (единый стиль).
> Маппинг кодов отказа (401/403/429/501) — из `security-requirements.md` (источник истины).

---

## Принципы

1. **Долгоживущий процесс — не команда.** `raxd serve` не завершается после первого вывода. Стартовый
   блок — единственный «спроектированный» момент; после него каждая строка — структурированная
   аудит-запись. Тишина означает здоровье: никаких heartbeat-строк, никаких «still running».

2. **Каналы строго по назначению.** Все стартовые, статусные, аудит-сообщения и сообщения об ошибках —
   **stderr**. Stdout остаётся пустым: `raxd serve` не пишет ничего на stdout. Это сохраняет stdout
   чистым для возможных будущих машиночитаемых расширений и не ломает пайпы на уровне процесса.

3. **Никаких секретов в выводе (SR-21).** Ни тело API-ключа, ни raw-заголовок `Authorization`, ни
   приватный TLS-ключ не появляются ни в одной строке вывода — ни при старте, ни в аудит-записях,
   ни в ошибках. Идентификация ключа — только по fingerprint (`fp=` + 12 hex-символов).

4. **Иерархия через плотность, не через декор.** В monospace-сетке первая строка блока — самый важный
   факт. Отступ (2 пробела) — подчинённая информация. Пустая строка — граница секции. Метки
   фиксированной ширины создают читаемый столбец фактов при беглом просмотре.

5. **Ошибки: `error:` + `hint:` строчными, двухпробельный отступ у hint.** Единое правило проекта.
   Текст ошибки описывает, что случилось, `hint:` говорит, что сделать. Никаких Go-стек-трейсов,
   sentinel-имён пакетов, raw-путей в тексте.

6. **Единый тон с bootstrap и key-management.** Выравнивание меток: 8 символов + значение с позиции
   12, двухпробельный отступ слева — как в `raxd status`. Ошибки — по шаблону `error:`/`hint:`.

7. **Plain-текст как база; lipgloss — точка расширения.** На стадии `tls-transport` стилизация
   через `charmbracelet/lipgloss` не подключается. Весь вывод — plain Unicode. Цвет и стиль
   зафиксированы как намерение (см. раздел «Цвета и стиль»).

---

## Язык интерфейса

**Все тексты CLI — английские.** Язык артефакта (этот файл) — русский. Тексты, показанные в
ASCII-макетах ниже, — итоговые строки на английском, копируемые в реализацию.

---

## Баннер автора

Баннер наследуется без изменений из `specs/bootstrap-cli/ux-spec.md`. `raxd serve` — подкоманда
дерева `raxd`; баннер печатается через `PersistentPreRun` корневой команды на **stderr** до старта
сервера.

**Wide-макет (терминал >= 52 символов):**

```
┌──────────────────────────────────────────────────┐
│  raxd  —  Remote Access Daemon                   │
│  v1.0.0  ·  commit abc1234  ·  built 2025-06-01  │
│  Vladimir Kovalev, OEM TECH                      │
└──────────────────────────────────────────────────┘
```

Строка автора `Vladimir Kovalev, OEM TECH` обязательна. Полный контракт баннера (narrow-макет,
fallback без рамки, dev-дефолты) — в `specs/bootstrap-cli/ux-spec.md`.

---

## Состояния вывода

> **Примечание:** состояния `install`, `status` и `key list` в данной спеке **не рассматриваются** —
> они закрыты `specs/bootstrap-cli/ux-spec.md` и `specs/key-management/ux-spec.md` соответственно.
> Данная спека покрывает **только** вывод команды `raxd serve`: стартовый блок, аудит-поток,
> graceful shutdown и ошибки старта.

### 1. Успешный старт `raxd serve` — первый запуск (генерация сертификата)

**Когда:** сертификат и ключ в `TLSDir` (`~/.local/state/raxd/tls/`) отсутствуют.
**Канал:** stderr. **Поведение:** блокируется до получения SIGINT/SIGTERM. **Exit:** 0 при
штатном завершении по сигналу.

**Порядок вывода:**

1. Баннер (PersistentPreRun, stderr)
2. Стартовый блок сервера (stderr)
3. Пустая строка — сигнал готовности
4. Аудит-поток (stderr, по мере соединений)

**Макет стартового блока:**

```
  cert      generated  ~/.local/state/raxd/tls/cert.pem
  key       generated  ~/.local/state/raxd/tls/key.pem  (0600)
  tls       TLS 1.3 only
  listening https://127.0.0.1:7822
  press Ctrl+C to stop

```

**Правила макета:**

- Метки: 9 символов, выравнивание по левому краю, значения с позиции 12 (2 пробела отступ + метка + пробелы).
- Строка `cert`: `generated` — явное событие (сертификат только что создан); путь — для проверки.
- Строка `key`: `generated (0600)` — явный сигнал о правах приватного ключа; путь виден.
- Строка `tls`: фиксирует минимальную версию протокола; оператор видит её без дополнительных команд.
- Строка `listening`: URL-форма `https://` — явно, что TLS включён; не `tcp://`.
- Строка `press Ctrl+C`: ориентир для foreground-запуска; убирает вопрос «как остановить».
- Пустая строка после блока: визуальный разделитель «сервер готов».

---

### 2. Успешный старт `raxd serve` — последующий запуск (переиспользование сертификата)

**Когда:** сертификат и ключ в `TLSDir` уже существуют и валидны (AC3, SR-5).
**Канал:** stderr.

**Макет стартового блока:**

```
  cert      loaded  ~/.local/state/raxd/tls/cert.pem
  key       loaded  ~/.local/state/raxd/tls/key.pem
  tls       TLS 1.3 only
  listening https://127.0.0.1:7822
  press Ctrl+C to stop

```

**Отличие от первого запуска:** `generated` заменяется на `loaded`. Это разграничение важно:
оператор сразу видит, создавался ли новый сертификат или используется существующий, не заглядывая
в файловую систему. Строка `(0600)` для ключа отсутствует — права уже выставлены ранее.

---

### 3. Аудит-строки соединений

**Канал:** stderr. **Формат:** структурированный однострочный лог через `charmbracelet/log`
в формате `key=value`.

**Общие правила аудит-строк:**

- Одна строка на соединение/событие — без переносов.
- Формат `charmbracelet/log`: `time=<timestamp> level=<LEVEL> msg=<LABEL> fp=<fingerprint> remote=<IP:port> [reason=<text>]`.
- `time` — UTC, ISO 8601 компактный: `2026-05-21T14:32:01Z`.
- `fp` — 12 hex-символов `keystore.Fingerprint`; для неаутентифицированных запросов (нет ключа) — `fp=-`.
- `remote` — `IP:port` без резолвинга DNS (избегаем задержек и утечки через DNS).
- `reason` — присутствует только на non-success строках.
- `msg` выполняет роль метки события: `AUTH`, `FAIL`, `DENY`, `RATE`.
- Тело ключа, raw Authorization, соль, хэш — **никогда** не попадают в аудит (SR-21).

**Уровни (level):**

| Метка (msg) | level  | Значение                                           |
|-------------|--------|----------------------------------------------------|
| `AUTH`      | `INFO` | Успешная аутентификация (до маршрутизации)          |
| `FAIL`      | `WARN` | Нет ключа / неизвестный / отозванный ключ           |
| `DENY`      | `WARN` | Повреждённый keys.db / неверный Host / неверный Origin |
| `RATE`      | `WARN` | Превышение rate-limit (per-key или per-IP)          |

**Примеры строк:**

#### 3.1 Успешная аутентификация (AUTH → 200 /healthz)

```
time=2026-05-21T14:32:01Z level=INFO msg=AUTH fp=a3f9c1d2e847 remote=127.0.0.1:54312
```

Нет поля `reason`: успех не требует пояснений. Строка короче — визуально тише в потоке.

#### 3.2 Отказ 401 — нет заголовка Authorization

```
time=2026-05-21T14:32:05Z level=WARN msg=FAIL fp=- remote=127.0.0.1:54401 reason="no authorization header"
```

`fp=-` — нет ключа, нечего идентифицировать. `reason` — не раскрывает внутренние детали,
только класс проблемы.

#### 3.3 Отказ 401 — неизвестный или отозванный ключ

```
time=2026-05-21T14:32:07Z level=WARN msg=FAIL fp=b7d2a0c19f3e remote=127.0.0.1:54402 reason="authentication failed"
```

`fp` присутствует (fingerprint вычислен из предъявленного ключа), но `reason` не указывает,
отозван ли ключ или просто неизвестен — это соответствует SR-13 (не раскрывать причину клиенту;
аудит-запись используется оператором, однако даже здесь детализация не нужна: «authentication
failed» достаточно для мониторинга аномалий).

#### 3.4 Отказ 403 — повреждённый keys.db

```
time=2026-05-21T14:32:09Z level=WARN msg=DENY fp=- remote=127.0.0.1:54403 reason="key store unavailable"
```

`fp=-` — при повреждённом хранилище ключей fingerprint не вычислен. `reason` — без упоминания
имени Go-ошибки или пути файла.

#### 3.5 Отказ 403 — Host вне allowlist

```
time=2026-05-21T14:32:11Z level=WARN msg=DENY fp=- remote=127.0.0.1:54404 reason="invalid host header"
```

Проверка Host происходит до auth (SR-14), поэтому fingerprint недоступен.

#### 3.6 Отказ 403 — Origin present и вне allowlist

```
time=2026-05-21T14:32:13Z level=WARN msg=DENY fp=- remote=127.0.0.1:54405 reason="invalid origin header"
```

#### 3.7 Отказ 429 — превышение rate-limit (per-key)

```
time=2026-05-21T14:32:15Z level=WARN msg=RATE fp=a3f9c1d2e847 remote=127.0.0.1:54312 reason="rate limit exceeded (key)"
```

`fp` присутствует — rate-limit по ключу проверяется после auth; fingerprint уже в контексте.
Поле `reason` указывает тип лимита: `(key)` vs `(ip)`.

#### 3.8 Отказ 429 — превышение rate-limit (per-IP)

```
time=2026-05-21T14:32:16Z level=WARN msg=RATE fp=- remote=127.0.0.1:54500 reason="rate limit exceeded (ip)"
```

Если per-IP лимит срабатывает до извлечения ключа — `fp=-`.

---

### 4. Graceful shutdown (SIGINT / SIGTERM)

**Когда:** получен сигнал завершения. **Канал:** stderr.
**Порядок:** `http.Server.Shutdown` → `store.FlushUsage` → exit 0.

**Макет:**

```
^C
  shutting down  signal received
  draining       waiting for active connections to finish
  flushing       usage data flushed
  stopped

```

**Правила:**

- `^C` — стандартный отпечаток терминала при Ctrl+C; сервер не подавляет его (выводится самим
  терминалом, не кодом). При SIGTERM — `^C` отсутствует, блок начинается с `shutting down`.
- Метки: 12 символов (немного шире стартового блока — shutdown менее частый, можно чуть
  подробнее). Значения выровнены по единому столбцу.
- Строка `draining`: сообщает, что процесс ждёт соединений, а не завис. Оператор видит, почему
  завершение не мгновенное.
- Строка `flushing`: фиксирует вызов `FlushUsage` — данные об использовании ключей сброшены.
- Строка `stopped`: финальный маркер. После неё — пустая строка и возврат в shell.
- Пустая строка после `stopped`: визуальный разделитель, сигнализирующий о возврате управления.

**Макет без ^C (SIGTERM, не Ctrl+C):**

```
  shutting down  signal received
  draining       waiting for active connections to finish
  flushing       usage data flushed
  stopped

```

---

### 5. Ошибки старта (exit 1)

**Канал:** stderr. **Код возврата:** 1. **Структура:** `error:` + `hint:` строчными,
двухпробельный отступ у `hint:`.

Стартовый блок НЕ печатается при ошибке старта — ошибка возникает до того, как сервер готов
к прослушиванию. Баннер уже напечатан (PersistentPreRun). Ошибка идёт напрямую.

#### 5.1 Порт занят

```
error: cannot bind to 127.0.0.1:7822: address already in use
  hint: check what is using port 7822 with "lsof -i :7822" and stop it, or change the port with "raxd config port <PORT>"
```

#### 5.2 Нет прав на каталог TLS (создание TLSDir)

```
error: cannot create TLS directory: permission denied
  hint: check that the current user has write access to ~/.local/state/raxd/
```

#### 5.3 Сбой генерации сертификата

```
error: failed to generate TLS certificate
  hint: check available disk space and write permissions for ~/.local/state/raxd/tls/
```

#### 5.4 Повреждённый или нечитаемый сертификат

```
error: TLS certificate or key is corrupted or unreadable
  hint: remove the files in ~/.local/state/raxd/tls/ and run "raxd serve" again to regenerate
```

**Важно:** сертификат не перезаписывается автоматически (SR-6). `hint:` явно предлагает удалить
файлы вручную.

#### 5.5 Повреждённый или недоступный keys.db при старте

```
error: key store is corrupted or unreadable
  hint: check file permissions on the keys.db path shown in "raxd status"
  hint: do not attempt to repair the file manually — contact support if data recovery is needed
```

Два `hint:`: первый — самостоятельное действие, второй — предупреждение не делать деструктивных
действий (согласован с аналогичной ошибкой в `specs/key-management/ux-spec.md`).

#### 5.6 Неверный bind-адрес (невалидный IP)

```
error: invalid bind address "0.0.0.256": not a valid IP address
  hint: set a valid address in config.yaml (field: bind_addr), for example "127.0.0.1" or "0.0.0.0"
```

Введённое значение отражается в ошибке: пользователь видит, что именно система прочитала.
Адрес bind — не секрет.

#### 5.7 Отсутствие созданных ключей (пустой keys.db при старте)

Это не ошибка старта — сервер запускается, но все подключения будут отклоняться с 401 (Verify
всегда вернёт false). Сервер выводит предупреждение в стартовом блоке:

```
  cert      loaded  ~/.local/state/raxd/tls/cert.pem
  key       loaded  ~/.local/state/raxd/tls/key.pem
  tls       TLS 1.3 only
  listening https://127.0.0.1:7822
  warning   no API keys found — all connections will be rejected
  hint      create a key with "raxd key create --name <label>"
  press Ctrl+C to stop

```

**Обоснование:** пустой keys.db не является причиной отказа старта — это валидное состояние
(например, после установки до создания первого ключа). Сервер стартует, но предупреждает.
Каждый последующий запрос без ключа будет давать аудит-строку `FAIL/401` (см. п. 3.2 — «нет
заголовка Authorization»; или п. 3.3 — «неизвестный ключ»), что позволяет developer отследить
связь между предупреждением при старте и аудит-потоком.
Это аналог `(not found, defaults applied)` в `raxd status`.

---

### 6. Health-эндпоинт (ping → pong) — ответ сервера

Это не CLI-вывод, а HTTP-ответ сервера аутентифицированному клиенту (AC10, SR-22).

**Маршрут:** `GET /healthz`
**Требования:** только аутентифицированный запрос достигает обработчика.

**HTTP-ответ:**

```
HTTP/1.1 200 OK
Content-Type: text/plain

pong
```

**Тело:** строка `pong` (строчными, без переноса строки, либо с `\n` — на усмотрение developer).

**Аудит-строка на stderr** (сервер) при успешном ping:

```
time=2026-05-21T14:32:01Z level=INFO msg=AUTH fp=a3f9c1d2e847 remote=127.0.0.1:54312
```

**Catch-all (501) — ответ на нереализованные маршруты:**

```
HTTP/1.1 501 Not Implemented
Content-Type: text/plain

not implemented
```

Аудит-строка для 501 не требуется отдельно — AUTH-строка уже зафиксировала успешную
аутентификацию. Маршрутизация после auth — деталь реализации, не событие безопасности.

> **Открытый вопрос OQ-1:** нужно ли логировать отдельной аудит-строкой факт обращения к
> конкретному маршруту (path, method) после успешной auth? Рекомендуемый дефолт: нет — это
> детализация уровня application-лога, не аудита безопасности. Если developer добавит поле
> `path` в `AuditRecord` — не противоречит spec; если нет — тоже корректно. Решение —
> developer по согласованию с architect.

---

### 7. Флаги и конфигурация

По spec (AC7) и plan (`config.go`), параметры берутся из `config.Config`:
- `BindAddr` (дефолт `127.0.0.1`)
- `Port` (дефолт `7822`)
- `RateLimit`, `RateBurst`
- `OriginAllow`, `HostAllow []string`

CLI-флаги для `raxd serve` в текущем scope **не вводятся** — конфигурация только через
`config.yaml`. Если в будущем флаги появятся (`--bind`, `--port`), их описание в `--help`:

```
Usage:
  raxd serve [flags]

Flags:
  -h, --help   help for serve

  (configuration is read from ~/.config/raxd/config.yaml)
```

**Long-описание команды (обновлённое, заменяет заглушку bootstrap-cli):**

```
Start raxd as a foreground TLS server.

The server listens on the configured address (default: 127.0.0.1:7822)
with TLS 1.3. Every connection is authenticated with an API key before
any request is processed.

Configuration is read from ~/.config/raxd/config.yaml.
For production use, register raxd as a system service instead.
```

**Short:**

```
Start the raxd TLS server
```

---

## Цвета и стиль (lipgloss)

### Текущее состояние: plain-текст

На стадии `tls-transport` стилизация через `charmbracelet/lipgloss` **не подключается**.
Весь вывод — plain Unicode с пробельным выравниванием.

Аудит-записи используют `charmbracelet/log` (уже в стеке): структурированный вывод с
timestamp и полями в формате `key=value` — без кастомного lipgloss-стилизования.

### Палитра — наследуется из bootstrap-cli

Палитра без изменений из `specs/bootstrap-cli/ux-spec.md`. Специфичные для `tls-transport`
точки расширения:

| Элемент вывода                    | Будущий стиль (lipgloss)                |
|-----------------------------------|-----------------------------------------|
| Метка `AUTH` в аудит-строке       | Цвет Success (`#5FFF87`)                |
| Метка `FAIL` в аудит-строке       | Цвет Error (`#FF5F5F`) + Bold           |
| Метка `DENY` в аудит-строке       | Цвет Error (`#FF5F5F`)                  |
| Метка `RATE` в аудит-строке       | Цвет Warning (`#FFD75F`)                |
| `generated` в стартовом блоке    | Цвет Success (`#5FFF87`)                |
| `loaded` в стартовом блоке       | Цвет Muted (`#767676`)                  |
| `listening` — значение адреса     | Цвет Primary (`#5FD7FF`) + Bold         |
| `warning` строка (нет ключей)    | Цвет Warning (`#FFD75F`)                |
| `stopped` при shutdown            | Цвет Muted (`#767676`)                  |
| `error:` префикс                  | Цвет Error (`#FF5F5F`) + Bold           |
| `hint:` / `hint ` префикс        | Цвет Muted (`#767676`)                  |

Гейт NO_COLOR проверяется до применения стилей (см. раздел «Доступность»).

---

## Тексты команд и ошибок

### Полный каталог текстов

#### Стартовый блок — метки и значения (английский, plain)

| Метка      | Значение (первый запуск)                              | Значение (последующий)             |
|------------|-------------------------------------------------------|-------------------------------------|
| `cert`     | `generated  <путь к cert.pem>`                       | `loaded  <путь к cert.pem>`         |
| `key`      | `generated  <путь к key.pem>  (0600)`                | `loaded  <путь к key.pem>`          |
| `tls`      | `TLS 1.3 only`                                       | `TLS 1.3 only`                      |
| `listening`| `https://<addr>:<port>`                              | `https://<addr>:<port>`             |
| `press…`   | `press Ctrl+C to stop`                               | `press Ctrl+C to stop`              |
| `warning`  | *(нет ключей)* `no API keys found — all connections will be rejected` | *(отсутствует)* |
| `hint`     | *(нет ключей)* `create a key with "raxd key create --name <label>"` | *(отсутствует)* |

#### Shutdown-блок

| Метка           | Текст                                          |
|-----------------|------------------------------------------------|
| `shutting down` | `signal received`                              |
| `draining`      | `waiting for active connections to finish`     |
| `flushing`      | `usage data flushed`                           |
| `stopped`       | *(пусто)*                                      |

#### Аудит-поля и reason-строки (формат charmbracelet/log key=value)

| msg (метка) | level  | reason (если есть)                         |
|-------------|--------|--------------------------------------------|
| `AUTH`      | `INFO` | *(отсутствует)*                            |
| `FAIL`      | `WARN` | `no authorization header`                  |
| `FAIL`      | `WARN` | `authentication failed`                    |
| `DENY`      | `WARN` | `key store unavailable`                    |
| `DENY`      | `WARN` | `invalid host header`                      |
| `DENY`      | `WARN` | `invalid origin header`                    |
| `RATE`      | `WARN` | `rate limit exceeded (key)`                |
| `RATE`      | `WARN` | `rate limit exceeded (ip)`                 |

#### Ошибки старта

Полные тексты — в разделе «Состояния вывода / 5. Ошибки старта». Краткая сводка:

| Условие                               | error: (начало)                                  |
|---------------------------------------|--------------------------------------------------|
| Порт занят                            | `cannot bind to <addr>:<port>: address already in use` |
| Нет прав на TLSDir                    | `cannot create TLS directory: permission denied` |
| Сбой генерации серта                  | `failed to generate TLS certificate`             |
| Повреждённый или нечитаемый сертификат | `TLS certificate or key is corrupted or unreadable` |
| Повреждённый keys.db                  | `key store is corrupted or unreadable`           |
| Неверный bind-адрес                   | `invalid bind address "<значение>": not a valid IP address` |

---

## Доступность (NO_COLOR, узкий терминал)

### NO_COLOR и --no-color

**Текущее поведение (plain-текст):** ANSI отсутствует. Никаких изменений не требуется.
Unicode-символы (`─`, `│`, `┌`, `┘`) — не ANSI, не отключаются при `NO_COLOR`.

**При добавлении lipgloss (точка расширения):** при наличии `NO_COLOR` (любое значение) или
флага `--no-color` lipgloss-стили не применяются. Вывод остаётся читаемым plain-текстом.
Аудит-строки через `charmbracelet/log` — при `NO_COLOR` log автоматически отключает ANSI.

### Узкий терминал

**Стартовый и shutdown-блок:** key-value строки переносятся естественно. Длинный путь к cert/key
может уходить за правую границу — ожидаемо, путь не усекается (оператор должен видеть полный путь).

Минимальная ширина для нормального отображения стартового блока: ~70 символов
(`  listening  https://127.0.0.1:7822` = 36 + рамка баннера ~52). При уже 40 символах
рамка баннера опускается (правило bootstrap-cli), стартовый блок не меняется.

**Аудит-строки:** каждая строка в формате `charmbracelet/log` в типовом случае:
```
time=2026-05-21T14:32:01Z level=INFO msg=AUTH fp=a3f9c1d2e847 remote=127.0.0.1:54312
```
(~80 символов). При узком терминале строка переносится терминалом — это ожидаемо и не ломает
смысл. Усечение аудит-строк запрещено: все поля обязательны для аудита (SR-19).

**Самая длинная аудит-строка** — с reason:
```
time=2026-05-21T14:32:15Z level=WARN msg=RATE fp=a3f9c1d2e847 remote=127.0.0.1:54312 reason="rate limit exceeded (key)"
```
~110 символов. Перенос при ширине < 110 — ожидаемо, содержание не теряется.

### Пайп и перенаправление

- `raxd serve 2>server.log` — все сообщения (стартовый блок, аудит, shutdown) пишутся в файл;
  stdout пуст. Корректно.
- `raxd serve 2>&1 | grep FAIL` — фильтрация аудита по типу отказа работает корректно.
- `raxd serve > /dev/null` — stdout и так пуст; не влияет на поведение.

---

## Сводка контрактов вывода

| Ситуация                     | stdout | stderr                                      | Exit |
|------------------------------|--------|---------------------------------------------|------|
| Нормальный старт (1-й раз)   | —      | баннер + стартовый блок (cert: generated)   | —    |
| Нормальный старт (повт.)     | —      | баннер + стартовый блок (cert: loaded)      | —    |
| Нет ключей при старте        | —      | баннер + стартовый блок + warning + hint    | —    |
| Аудит: AUTH                  | —      | одна строка `msg=AUTH fp=…`                 | —    |
| Аудит: FAIL (401)            | —      | одна строка `msg=FAIL fp=… reason=…`        | —    |
| Аудит: DENY (403)            | —      | одна строка `msg=DENY fp=- reason=…`        | —    |
| Аудит: RATE (429)            | —      | одна строка `msg=RATE fp=… reason=…`        | —    |
| Graceful shutdown            | —      | shutdown-блок (4 строки) + пустая строка    | 0    |
| Ошибка старта (bind/cert/db) | —      | баннер + `error:` + `hint:`                 | 1    |

---

## Открытые вопросы

**OQ-1 (маршрут в аудит-записи):** нужно ли добавлять поле `path`/`method` в `AuditRecord`
(`GET /healthz`, `POST /dispatch`) после успешной аутентификации? Это детализация application-лога,
не требование security-baseline. **Рекомендуемый дефолт:** не добавлять в v1 — упрощает структуру
и соответствует SR-19 (поля: timestamp, fingerprint, remote, result, reason).
Если qa или architect сочтёт нужным — добавить в `AuditRecord` без изменений ux-spec.

**OQ-2: РЕШЕНО** — принят формат `key=value` `charmbracelet/log`. Все макеты аудит-строк
переведены в реальный формат библиотеки: `time=... level=INFO/WARN msg=AUTH/FAIL/DENY/RATE
fp=... remote=... [reason=...]`. Developer реализует через `log.With(...)` поля `fp`, `remote`,
`reason`; `msg` выполняет роль метки события.

---

*Артефакт задачи: `tls-transport`. Контракт для: developer. Проверяет: cli-ux-guardian.*
*Автор продукта: Vladimir Kovalev, OEM TECH.*
