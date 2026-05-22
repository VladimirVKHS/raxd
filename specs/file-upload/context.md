# Context: file-upload — справочно для architect (НЕ контракт)

> Этот файл — НЕ часть контракта `spec.md`. Это справка о готовом коде и паттернах репозитория,
> собранная pm для ускорения работы architect/research-analyst. Контракт (что и как проверяется) —
> только в `spec.md`. Конкретные сигнатуры, имена пакетов и структуры здесь приведены как ориентир,
> архитектурные решения принимает architect.

## Готовый код, на который опирается file-upload

- **MCP-сервер (`mcp-server`):** MCP-сервер на официальном Go SDK с точкой расширения регистрации
  инструментов (`internal/mcp/server.go`, `NewHandler` + `sdkmcp.AddTool`). `ping`/`server_info`/
  `execute_command` уже зарегистрированы здесь. Сигнатура `NewHandler` уже расширялась под
  command-exec параметром `execCfg cmdexec.Config` — для file-upload возможно аналогичное расширение
  (решает architect).

- **Транспорт (`tls-transport`):** цепочка middleware в `internal/server/middleware.go`:
  `bodyLimitMiddleware` → `recoverMiddleware` → `hostOriginMiddleware` → auth → `rateLimitMiddleware`
  → authSuccessAudit → mux. Контекст несёт fingerprint и remote address через
  `server.FingerprintFromContext` / `server.RemoteAddrFromContext`. НЕ переписывается — потребляется.

- **Лимит тела запроса:** `bodyLimitMiddleware` (`internal/server/middleware.go`) оборачивает тело
  каждого запроса в `http.MaxBytesReader` как ВНЕШНИЙ слой цепочки, ДО `/mcp`-handler. Лимит —
  `MaxBodyBytes` из конфига (`internal/config/config.go`, ключ `max_body_bytes`, дефолт 1 MiB,
  `1<<20`). Это внешняя граница размера тела (см. AC15 в spec).

- **Аудит-инфраструктура:** `internal/server/audit.go` — `AuditRecord`, `AuditFn`, `writeAudit`.
  `AuditRecord.Result` уже принимает значения `success`/`fail`/`deny`/`warn`/`rate-limited`.
  `writeAudit` рендерит logfmt и логирует exec-специфичные поля (`Command`/`Args`/`ExitCode`/
  `Duration`/`TimedOut`) ТОЛЬКО при `Tool=="execute_command"`, не ломая формат прочих записей.
  Для file-upload нужны поля «относительный путь» и «размер»; как их представить (расширять ли
  `AuditRecord` или иначе) — решает architect/security (см. Open Question в spec, делегировано).

- **Атомарная запись (образец):** `internal/keystore/keystore.go`, метод `writeDB` — атомарная запись
  по схеме `temp → chmod → write → sync → rename → fsync-dir` с очисткой temp при любой ошибке до
  rename. Это пример того, как в проекте уже делается атомарная запись на диск; применять ли именно
  эту схему для file-upload — решает architect (spec фиксирует только поведение: атомарность +
  отсутствие частичного/temp-файла при сбое).

- **Конфигурация:** `internal/config/config.go` (viper, безопасные дефолты). Секция `exec`
  (`ExecConfig`) — образец того, как добавляется секция конфига с безопасными дефолтами через
  `v.SetDefault` и валидацией в `buildConfig`. Новая секция загрузки добавляется по аналогии.

- **Паттерн command-exec (планка качества):** `execute_command` ведёт собственный аудит в handler
  (без generic `withAudit`, ADR-004), типизированные вход/выход через struct с
  `additionalProperties:false`, проверяет входные лимиты ДО действия, детектирует root + пишет WARN,
  имеет опциональный `deny_root`. file-upload следует той же планке.

## Артефакты command-exec как образец структуры/планки
- `specs/command-exec/spec.md` — структура и плотность AC.
- `specs/command-exec/mcp-spec.md` — дизайн инструмента (схемы, error mapping).
- `specs/command-exec/security-requirements.md` — формат проверяемых SR.
- `specs/command-exec/plan.md` — модули, контракты, trade-offs.
