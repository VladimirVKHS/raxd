# Docs Outline: raxd — TLS Transport (`raxd serve`)

Автор продукта: **Vladimir Kovalev, OEM TECH**.
Задача: `tls-transport`. Роль: tech-writer (Author). Вердикт reviewer: `accept`, guardian: `pass`.
Язык продуктовой документации — английский; язык этого плана — русский.

Цель документирования: перевести `raxd serve` из «honest stub» в рабочий foreground TLS-сервер во
всей документации, добавить новые поля конфигурации, и НЕ заявлять несуществующего (нет выполнения
команд, нет MCP, нет mTLS, нет установщика, serve не регистрируется как системный сервис).

## Структура docs/

Документируется ТОЛЬКО реально существующее в коде (`internal/server/*`, `internal/cli/serve.go`,
`internal/config/config.go`, `internal/config/paths.go`, `internal/keystore`):

- `README.md` (корень) — точка входа: что такое raxd, что работает сегодня (включая `serve`), что
  ещё планируется. Обновлён.
- `docs/commands.md` — command reference: добавлена полноценная секция `raxd serve` вместо stub,
  обновлены сводная таблица, дерево команд, exit-коды. Обновлён.
- `docs/configuration.md` — пути, `keys.db`, TLS-каталог, и новые сетевые поля `config.yaml`,
  которые читает `serve`. Обновлён.
- `docs/development.md` — раскладка проекта (добавлен пакет `internal/server`), тесты сервера в
  Docker с `-race`, новая зависимость `golang.org/x/time/rate`. Обновлён.
- `docs/troubleshooting.md` — типовые проблемы `serve` (порт, серт, ключи, 401/403/429/413/501, TLS),
  ключей и конфига. Создан (на него ссылаются README, commands, configuration, development).

### Документы, которые НЕ создаются в этой задаче (и почему)

- `docs/install.md` (`curl | sh`) — **не создаётся**: установщика (`install.sh`, goreleaser,
  SHA256SUMS) в репозитории нет, фича вне scope `tls-transport` (см. spec Out of Scope, README
  «Coming next»). Документировать несуществующий установщик — нарушение красной линии. Появится в
  задаче `distribution`.
- `docs/mcp.md` (MCP integration guide) — **не создаётся**: MCP-сервер не реализован. В коде есть
  только catch-all `dispatchHandler` → `501 not implemented` как точка расширения. Документировать
  MCP-tools/resources/транспорт сейчас означало бы выдумать поведение. Появится в задаче
  `mcp-server`; в README/commands явно указано, что MCP attach-ится к существующему серверу позже.
- man-страницы (`man/raxd.1` …) — **не выпускаются** в этой задаче: в репозитории нет man-каталога
  и инструмента генерации; контракт `tls-transport` их не требует. Точка для будущей задачи
  `distribution`.

## На каждый документ

### README.md
- **Цель:** дать обзор продукта и честный статус: `serve`/TLS теперь работают, остальное сетевое —
  нет.
- **Аудитория:** новый пользователь, оценивающий проект.
- **Ключевые секции (изменения):**
  - Блок статуса: добавлен TLS-сервер в список «in place and working».
  - `What works today`: новые строки — `serve` (foreground TLS 1.3), self-signed cert (auto-gen,
    reuse), key auth по сети (`Authorization: Bearer`), rate-limit per-key/per-IP, Host/Origin,
    audit-лог (только fingerprint), health-check `pong`. Явно отмечены как **Not implemented**:
    command-exec, MCP, file-upload, установщик, регистрация системного сервиса.
  - `Example: raxd serve`: стартовый блок (по ux-spec), пример audit-строки, вызов `/healthz` через
    `curl -k -H "Authorization: Bearer $KEY"`. Scope-предупреждение.
  - `Coming next`: TLS transport больше не «planned»; planned теперь — command-exec, MCP, upload,
    system-service, mTLS, установщик, `config port`.
  - `Documentation`: ссылки на все docs, включая troubleshooting.
  - Автор: **Vladimir Kovalev, OEM TECH** — в шапке и в разделе `Author` (контракт сохранён).

### docs/commands.md
- **Цель:** точный справочник по командам, как в коде.
- **Аудитория:** оператор/интегратор.
- **Ключевые секции (изменения):**
  - Шапка/дерево/таблица: `serve` помечен `working`, остался один stub — `config port`.
  - Глобальное поведение: уточнено, что `serve` пишет всё на stderr, stdout пуст.
  - Exit codes: `serve` → 0 при graceful shutdown, 1 при ошибках старта (включая невалидный
    `config.yaml`, не только плохой `bind_addr`).
  - Новая большая секция `raxd serve`: usage, help-текст, scope (что есть/чего нет), пайплайн
    запроса, таблица response-кодов (401/403/429/413/501/200), стартовый вывод (generated/loaded/
    no-keys warning), audit-поток (формат key=value, msg AUTH/FAIL/DENY/RATE), health-check,
    graceful shutdown, ошибки старта (со всеми текстами из serve.go/ux-spec), security summary.
  - Стартовый и shutdown-блок печатаются только при успешном bind (через хук `OnListen`); при любой
    ошибке старта — только `error:`/`hint:`, exit 1. Это явно описано в секциях Startup output,
    Graceful shutdown и Startup errors.
  - **Response-коды: `413` от body-limit НЕ аудируется** (D-6) — заметка под таблицей и в секции
    Audit stream: `bodyLimitMiddleware` (внешний слой) даёт `413` силами `http.MaxBytesReader` до
    audit-цепочки, без `FAIL`/`DENY`/`RATE`, в отличие от 401/403/429.
  - **Startup errors: единый config-load hint** (D-5) — секция Startup errors объясняет, что
    невалидный `bind_addr` И невалидный `config.yaml` идут одним блоком в `serve` и получают один
    обобщённый hint про `bind_addr`/`config.yaml`; для YAML-ошибки hint обобщённый, не специфичный —
    actionable часть в строке `error:`.
  - Заметки о связке с key-management: revoke действует немедленно по сети; ключ презентуется через
    `Authorization: Bearer`.

### docs/configuration.md
- **Цель:** где хранятся данные и какие поля `config.yaml` читает `serve`.
- **Аудитория:** оператор.
- **Ключевые секции (изменения):**
  - TLS-каталог: серт/ключ теперь создаются при первом `serve` (cert.pem 0644, key.pem 0600,
    атомарная запись, переиспользование, поведение при повреждении).
  - `keys.db` и `serve`: пустой/отсутствующий → старт + warning; повреждённый → ошибка старта.
  - Новая секция `Networking and serve fields`: полный список реальных ключей с дефолтами из
    `config.go` — `port`(7822), `bind_addr`(127.0.0.1), `rate_limit`(10), `rate_burst`(20),
    `host_allow`/`origin_allow`(localhost/127.0.0.1/::1), `read_timeout`(30s),
    `read_header_timeout`(10s), `write_timeout`(30s), `idle_timeout`(120s),
    `max_header_bytes`(1 MiB), `max_body_bytes`(1 MiB). Заметки: валидация IP, строгий Origin,
    фиксированный TTL лимитеров 10 мин.

### docs/development.md
- **Цель:** как собирать/тестировать, раскладка кода.
- **Аудитория:** контрибьютор.
- **Ключевые секции (изменения):**
  - «Why Docker only»: `serve` открывает listener → запускать только в контейнере.
  - Новый подраздел: тесты `internal/server` с `-race` (`CGO_ENABLED=1`).
  - Project layout: добавлен пакет `internal/server` с пофайловым описанием; `serve.go` помечен
    working; `config.go` — networking fields.
  - «How the pieces fit together»: формулировка про `serve.go` уточнена (ISSUE-2/D-1) — он не печатает
    стартовый блок напрямую, а **регистрирует хук `OnListen` (`srv.SetOnListen`), который печатает
    стартовый блок только после успешного bind** внутри `srv.Run`.
  - Dependencies: добавлен `golang.org/x/time/rate`; отмечено, что транспорт — только stdlib
    (`net/http`, `crypto/{tls,x509,ecdsa}`), без сторонних HTTP/TLS-фреймворков.

### docs/troubleshooting.md
- **Цель:** быстро решить типовую проблему `serve`/ключей/конфига.
- **Аудитория:** оператор.
- **Ключевые секции:** все 401-причины; `address already in use` (+ пояснение, что при ошибке старта
  печатается только `error:`/`hint:`, без ложного стартового/shutdown-блока); битый/неполный серт;
  нет прав на TLSDir; сбой генерации серта; битый `keys.db`; **единый config-load hint** (D-5):
  невалидный `bind_addr` И невалидный `config.yaml` дают один обобщённый hint про `bind_addr` —
  отдельная заметка под `config file is not valid YAML` объясняет, что упоминание `bind_addr` для
  YAML-ошибки несущественно, actionable — строка `error:`; **`413` не аудируется** (D-6) — отдельная
  запись «A request returns 413 (and nothing shows up in the audit stream)»; self-signed TLS-ошибка у
  клиента; `501`/`429`/`403` (разбор reason); «сервер висит» = норма; потеря тела ключа;
  `config.yaml` не применяется без рестарта; `$HOME` не задан.

## Примеры команд (корректные, проверены по коду/ux-spec)

- `raxd key create --name production-key` — выпуск ключа (показывается один раз; тело на stdout).
- `raxd key list` — таблица ключей (полный 16-hex id, label, created, last used).
- `raxd key delete d7bc3a34da19d94e` — soft-revoke по полному id.
- `raxd serve` — запуск foreground TLS-сервера (вывод на stderr, stdout пуст).
- `curl -k -H "Authorization: Bearer $KEY" https://127.0.0.1:7822/healthz` → `pong` (self-signed →
  `-k` для локального теста).
- `docker build --target test -t raxd-test . && docker run --rm raxd-test` — сборка + тесты.
- `docker run --rm raxd-test sh -c "CGO_ENABLED=1 go test -race -v -count=1 ./internal/server/..."`
  — тесты сервера с детектором гонок.

## Об авторе (OEM TECH)

Обязательный блок **Vladimir Kovalev, OEM TECH**:
- В шапке `README.md` (строка `Author:`) и в разделе `## Author`.
- В баннере CLI (контракт bootstrap-cli/ux-spec) — строка автора печатается перед каждой командой.
- В этом плане — поле «Автор продукта».
Контактов/лицензии в репозитории нет: в README честно указано, что файла лицензии пока нет.

## Решения по расхождениям код / ожидания (зафиксированы в доке, не сглажены)

- **D-1. Порядок вывода при ошибке старта — РЕШЁНО (исправлено в коммите `cefdee5`).** Ранее
  `internal/cli/serve.go` печатал стартовый блок ДО `srv.Run`, поэтому при `address already in use`
  пользователь видел ложный стартовый и shutdown-блок и лишь затем `error:`/`hint:`. Теперь стартовый
  блок печатается через хук `OnListen` в `internal/server` — **только после успешного bind** TCP-
  листенера. При любой ошибке старта (порт занят / нет прав на TLS-каталог / битый серт / повреждённый
  `keys.db`) `Run` возвращает ошибку, хук не вызывается → не печатается ни стартовый, ни shutdown-блок,
  только `error:`/`hint:` на stderr, exit 1. Это соответствует ux-spec §5. Подтверждено тестами
  `internal/cli/cli_gaps_test.go` (`TestServePortInUseNoStartupBlock`, `TestServePortInUseNoShutdownBlock`)
  и `internal/server/server_test.go` (`TestOnListenHookCalledOnSuccessfulBind`,
  `TestOnListenHookNotCalledOnPortInUse`). Документация обновлена: «output-ordering caveat» убран из
  `docs/commands.md` (раздел Startup errors + Startup output + Graceful shutdown) и
  `docs/troubleshooting.md` (раздел `address already in use`), заменён на корректное описание.
  Формулировка в `docs/development.md` («How the pieces fit together») приведена в соответствие:
  serve.go регистрирует хук `OnListen`, который печатает стартовый блок только после успешного bind
  (ISSUE-2 гардиана).
- **D-2. Пустой (zero-byte) серт не даёт ErrTLSCert.** `fileExists` в `tls.go` считает файл
  размером 0 «отсутствующим». Значит ДВА пустых файла → регенерация, а не ошибка «corrupted».
  Ошибка «corrupted» возникает при непарсящемся содержимом или при наличии только одного из двух
  файлов. Это уточнено в troubleshooting.md (рекомендация: удалить ОБА файла и перезапустить).
- **D-3. `last used` в `key list`.** Поле существует, но в типичном сценарии остаётся `never`:
  per-key usage пишется только `FlushUsage` при graceful shutdown, а единственный обработчик за
  auth — health-check. Формулировка в commands.md приведена в соответствие с этим (не обещаем, что
  каждый запрос сразу обновляет дату).
- **D-4. `raxd status` показывает `not running` даже при работающем `serve`.** `status` читает
  только пути на диске и не зондирует процесс. Уточнено в README и commands.md, чтобы не вводить в
  заблуждение.
- **D-5. Единый config-load hint для невалидного `bind_addr` и невалидного `config.yaml`.** В
  `serve.go` ошибки из `config.Load` (и невалидный bind-адрес, и невалидный YAML) обрабатываются
  ОДНИМ блоком (строки 60-65) с ОДНИМ общим hint про `bind_addr`/`config.yaml`. То есть при ошибке
  `config file is not valid YAML` (config.go:102) пользователь всё равно получает
  `hint: ... (field: bind_addr)` (config.go:112 — это сообщение только для невалидного IP). Документация
  НЕ изображает точный матчинг error→hint: в `docs/commands.md` (Startup errors) и
  `docs/troubleshooting.md` (`invalid bind address` + `config file is not valid YAML`) явно отмечено,
  что hint обобщённый для любого сбоя загрузки конфигурации, а actionable часть — строка `error:`. Для
  YAML-ошибки упоминание `bind_addr` несущественно. Никаких отдельных сообщений, которых нет в коде, не
  выдумано (ISSUE-1 гардиана).
- **D-6. `413` от body-limit не аудируется.** `bodyLimitMiddleware` (`middleware.go`, строки 74-81) —
  внешний слой цепочки (порядок: body-limit → recover → Host/Origin → auth → rate-limit). При
  превышении `max_body_bytes` `http.MaxBytesReader` отдаёт `413` ДО audit-цепочки и НЕ вызывает
  `auditFn`, поэтому DENY/FAIL/RATE-записи нет — в отличие от 401 (FAIL), 403 (DENY) и 429 (RATE),
  которые пишут аудит-строку. Зафиксировано в `docs/commands.md` (заметка под таблицей response-кодов
  и в Audit stream) и в `docs/troubleshooting.md` (запись «A request returns 413 …»). Сверено с
  `middleware.go` и порядком слоёв (ISSUE-3 гардиана).

## Открытые вопросы

- [x] Q1 (РЕШЁНО, коммит `cefdee5`): порядок вывода при ошибке старта (D-1). Реализован хук `OnListen`
  в `internal/server` — стартовый блок печатается только после успешного bind; при ошибке старта
  выводятся только `error:`/`hint:`, exit 1, без ложного стартового/shutdown-блока. Соответствует
  ux-spec §5; подтверждено тестами (см. D-1 выше). Доки приведены в соответствие.
- [x] G1 (ЗАКРЫТО): три находки точности tech-writer-guardian (needs-changes) исправлены —
  ISSUE-1/D-5 (единый config-load hint), ISSUE-2/D-1 (формулировка serve.go про `OnListen` в
  development.md), ISSUE-3/D-6 (`413` body-limit не аудируется). Все три сверены с кодом
  (`serve.go`, `middleware.go`, `config.go`).
- None по содержанию документации: все задокументированные команды/флаги/поля/коды сверены с кодом
  (`serve.go`, `server/*.go`, `config.go`, `paths.go`, `keystore`).
