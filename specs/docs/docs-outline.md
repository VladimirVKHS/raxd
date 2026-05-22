# docs-outline.md — финальная сверка и полировка документации raxd

Роль: tech-writer (финальная задача `docs`). Ветка: `feature/docs`. Язык docs/README: **английский**.
Дата: 2026-05-22. Автор продукта: **Vladimir Kovalev, OEM TECH**.

Это итоговый отчёт по последней сверке ВСЕЙ продуктовой документации raxd по факту реально
реализованного кода. Карта для **tech-writer-guardian**: что проверено, какие неточности docs↔код
найдены и исправлены, что согласовано, где размещены остаточные риски, какие ограничения остаются.

> Метод: каждая команда/флаг/вывод/код/схема MCP-инструмента/путь/дефолт сверены с исходным кодом
> (`cmd/raxd/main.go`, `internal/cli/*`, `internal/version/version.go`, `internal/mcp/*`,
> `internal/server/*`, `internal/service/*`, `internal/cmdexec/*`, `internal/fileupload/*`,
> `internal/config/*`, `install.sh`, `Makefile`, `scripts/*`) и со спеками остаточных рисков
> (`specs/distribution/threat-model.md` ОР-1..ОР-5 / П-1..П-4). Документировано ТОЛЬКО реальное;
> плейсхолдеры/ограничения зафиксированы честно. Код НЕ менялся (Author tier, только Write).

## 1. Карта документации `docs/`

| Файл | Назначение | Статус сверки |
|------|------------|----------------|
| `README.md` | Точка входа: обзор, статус, установка, быстрый старт, команды, ссылки, автор | **Изменён** (статус-блок, version-пример, status-пример, ссылки на production-readiness) |
| `docs/installation.md` | Гайд установки `curl\|sh`, источник артефактов, trust-модель, manual, macOS quarantine, сборка из исходников, коды выхода | Сверен — уже точен (version-пример `v0.1.0` был корректен) |
| `docs/commands.md` | Полный command reference (version/status/key/service/serve/config port) | **Изменён** (version-пример → `v0.1.0`, добавлена секция Author) |
| `docs/configuration.md` | Пути, service layout, keys.db, TLS, поля config.yaml (networking/exec/upload) | **Изменён** (добавлены ссылка на production-readiness и секция Author) |
| `docs/mcp.md` | MCP integration guide: /mcp, connection, ping/server_info/execute_command/upload_file, auth, audit | **Изменён** (version в JSON-примерах `1.0.0` → `v0.1.0`, 5 мест) |
| `docs/service-management.md` | Security/ops модель сервиса: non-root, CAP_NET_BIND_SERVICE, что хранит uninstall, ротация, restart, macOS | **Изменён** (ссылки на production-readiness + секция Author) |
| `docs/execute-command-security.md` | Обязательные security-предупреждения execute_command | Сверен — точен, Author уже был |
| `docs/file-upload-security.md` | Обязательные security-предупреждения upload_file | Сверен — точен, Author уже был |
| `docs/troubleshooting.md` | Диагностика: install/serve/service/TLS/keys/config/exec/upload | **Изменён** (ссылка на production-readiness + секция Author) |
| `docs/development.md` | Сборка/тесты в Docker, раскладка проекта, build metadata | **Изменён существенно** (устаревшая раскладка/контракты — см. §2) |
| `docs/production-readiness.md` | **НОВЫЙ** — сводная карта остаточных рисков/ограничений к прод-релизу | **Создан** (см. §4) |

## 2. Найденные и исправленные неточности docs↔код (конкретно)

### Критично: `docs/development.md` была СУЩЕСТВЕННО устаревшей (до реализации exec/upload)

- **«command execution and file upload … do not exist yet»** — было ЛОЖНО: `execute_command` и
  `upload_file` реализованы (`internal/mcp/exec_tool.go`, `internal/mcp/upload_tool.go`). Исправлено:
  раздел «Why Docker only» теперь честно говорит, что инструменты реализованы и Docker-only правило —
  load-bearing.
- **Неверная сигнатура** `internalmcp.NewHandler(version.Version, auditFn)` — реальная сигнатура
  `NewHandler(ver, audit, execCfg, uplCfg)` (`internal/mcp/server.go:42`). Исправлено.
- **«registers exactly ping and server_info»** — реально регистрируются ЧЕТЫРЕ инструмента
  (`server.go:55-58`: ping, server_info, execute_command, upload_file). Исправлено.
- **Раскладка `internal/mcp/`** не содержала `exec_tool.go`/`upload_tool.go`; отсутствовали пакеты
  `internal/cmdexec`, `internal/fileupload`, `internal/service`, верхнеуровневые `install.sh`,
  `scripts/`, `Makefile`. Все добавлены (существование проверено через Glob).
- **«How the pieces fit together»** дополнено: `internal/cli/service.go`, резолв порта из config для
  CAP_NET_BIND_SERVICE, делегирование exec→cmdexec, upload→fileupload, генерация unit/plist в service.
- Версия-пример `1.0.0 (… 2025-06-01)` → `v0.1.0 (… 2026-05-22)`; добавлено пояснение, что ldflags
  целятся в `main.buildVersion/Commit/Date` (Makefile), а пример с `internal/version.*` — ручная форма.
- Тест-команда дополнена пакетами `cmdexec`/`fileupload`; добавлен раздел про `make ci-local`.
- Зависимости: charmbracelet/log теперь и для service-аудита; уточнено, что exec/upload/service
  используют только stdlib (`os/exec`, `os.Root`, `text/template`, `os/user`).

### Согласование версии-примера (расхождение между файлами)

- **Было расхождение**: README/commands.md/mcp.md/development.md использовали `1.0.0` (без `v`),
  installation.md и `Makefile` — `v0.1.0` (с `v`). Реальный формат из кода
  (`internal/version/version.go` `Info()`): `raxd <version> (commit <commit>, built <date>)`; raxd
  НЕ добавляет и НЕ срезает `v` (default dev-сборки = `dev`, не `vdev`). Конвенция релиза в
  `Makefile` — git-теги (`git describe --tags` → `v0.1.0`), пример `make release VERSION=v0.1.0`.
- **Решение**: согласовано на ЕДИНЫЙ канонический пример **`raxd v0.1.0 (commit abc1234, built
  2026-05-22)`** (с `v`-префиксом — отражает реальную релизную конвенцию). Применено в README,
  commands.md, mcp.md (5 JSON-мест: server_info structuredContent ×2, текст-строка ×2, initialize
  serverInfo), development.md. installation.md уже был на `v0.1.0` — не трогал.
- Проверка: после правок в `docs/` НЕТ вхождений `1.0.0` или `2025-06-01` (Grep — 0 совпадений).

### README.md — статус и точность

- **Заменён хронологический «Project status: early»** (читался как build-log: «the first MCP
  server», «the latest piece», «now in place») на честный **«feature-complete for v1»** с явным
  списком ЧТО ещё pending к публичному релизу (публичный хостинг, GPG-подпись, нотаризация, LICENSE)
  и ссылкой на `docs/production-readiness.md`. Устаревших «not implemented yet» по реализованным
  фичам не осталось; раздел «Coming next» оставлен только для реально НЕ реализованного.
- **status-пример исправлен**: показывал `config /…/config.yaml` без суффикса; реальный код
  (`internal/cli/status.go:34-39`) добавляет `(not found, defaults applied)` когда файла нет (частый
  кейс свежей установки). Пример приведён к реальному поведению + пояснение.
- Раздел статуса честен: публичный хостинг/GPG/нотаризация/LICENSE — pending; ссылки добавлены.

### Сверка остального (расхождений docs↔код НЕ найдено — подтверждено точным)

- **commands.md**: все подкоманды (version/status/key create|list|delete/service install|uninstall|
  start|stop|status/serve/config port-stub), `--json` у `service status`, коды выхода, форматы
  вывода (stdout/stderr split), success-блоки service, audit-формат, response-коды (401/403/429/413/
  400/405/501), startup/shutdown — сверено с `internal/cli/*` и `internal/server/*`, точно.
- **mcp.md**: схемы инструментов сверены с `internal/mcp/{tools,exec_tool,upload_tool}.go` —
  ExecInput {command,args,timeout_ms,cwd}, ExecOutput (7 полей), UploadInput {path,content,overwrite,
  mode}, UploadOutput (4 поля), `additionalProperties:false` (инференция SDK), protocol `2025-11-25`,
  stateless, GET→405, server_info {name,version,protocolVersion}, isError omitempty — точно.
- **configuration.md**: пути (XDG, `~/.config/raxd`), service layout (Linux/macOS), keys.db (0600),
  TLS (cert 0644/key 0600), поля config.yaml (networking/exec/upload + дефолты: port 7822, rate 10/20,
  max_body 1MiB, exec.default_timeout 30000/max 300000/default_cwd /tmp/env_whitelist/max_args 256/
  max_arg_len 131072/max_output 1MiB/deny_root false, upload.max_file_bytes 716800/default_mode 0600/
  deny_root false) — сверено с `internal/config/*`, `internal/cmdexec/config.go`,
  `internal/fileupload/config.go`, точно.
- **installation.md / troubleshooting.md**: коды выхода install.sh (0-5), сообщения, RAXD_BASE_URL
  плейсхолдер, SHA256-до-размещения, macOS quarantine, edge-cases — сверено с `install.sh`, точно.
- **service-management.md**: non-root (`User=raxd`), CAP_NET_BIND_SERVICE при port<1024,
  NoNewPrivileges trade-off, uninstall сохраняет user+StateDir, journald drop-in, restart-on-failure,
  unit path `/etc/systemd/system/raxd.service`, plist `/Library/LaunchDaemons/tech.oem.raxd.plist` —
  сверено с `internal/service/*` и `internal/cli/service.go`, точно.

## 3. Согласованность между docs

- **Версия-пример**: единый `v0.1.0` (см. §2). Формат `raxd <version> (commit <commit>, built <date>)`
  везде согласован с `internal/version.Info()`.
- **Термины**: «upload root», «non-root», «stateless», «self-signed», «`os.Root`-confined»,
  «allowlist off by default», «soft revoke» — единообразны.
- **Перекрёстные ссылки**: все docs ссылаются друг на друга и на новый `production-readiness.md`;
  якоря/пути проверены (относительные пути внутри `docs/`, `../specs/...` для спеков и `../README.md`).
- **Автор**: `## Author — Vladimir Kovalev, OEM TECH` теперь во ВСЕХ 10 файлах `docs/` + в README
  (строка автора в шапке, раздел Author, упоминание в баннере). Ранее отсутствовал в commands.md,
  configuration.md, development.md, service-management.md, troubleshooting.md — добавлен.

## 4. Где размещены остаточные риски (требование RESUME)

Создан **`docs/production-readiness.md`** — единое заметное место «Production readiness / Known
limitations», на которое ссылаются README (шапка статуса + Coming next + Documentation + License),
service-management.md, configuration.md, troubleshooting.md, development.md. Содержит сводную таблицу
«At a glance» + детальные разделы:

1. Нет публичного релиз-хостинга; `RAXD_BASE_URL` — плейсхолдер (ОР-3/ОР-5).
2. Нет GPG/minisign-подписи `SHA256SUMS`; v1-доверие = TLS + SHA256 (ОР-1/П-1).
3. Нет Apple-нотаризации; install.sh только снимает quarantine (ОР-2/П-2).
4. macOS Gatekeeper/launchd проверяется ВНЕ Docker (ОР-4/П-4).
5. Нет `LICENSE` в репозитории (подтверждено: только vendored-лицензии, корневого нет).
6. `service uninstall` сохраняет inert-пользователя `raxd` и StateDir (UID-reuse; П-2 service-install).
7. Нет disk-quota на суммарный объём upload (только per-file лимит).
8. `execute_command` args и `upload_file` path логируются дословно (не передавать секреты).
9. Демон от root → WARN-дефолт (`deny_root: false`); запускать non-root (раскладка `raxd service`).
10. Нет sandboxing (cgroups/rlimits/seccomp/namespaces); нет mTLS.

Каждый пункт — честно, с пометкой «known limitation v1» и эскалацией. ОР-нумерация согласована с
`specs/distribution/threat-model.md` и smежными threat-моделями.

## 5. Известные ограничения, которые ОСТАЮТСЯ (не «чинятся» докой)

- Все 10 пунктов §4 — это реальные границы v1, зафиксированы честно, НЕ выдуманы и НЕ замаскированы.
- `config port` — честный stub (`error: config port: not implemented yet`, exit 1); документирован как
  stub везде, обходной путь (ручная правка config.yaml) описан.
- Документация описывает плейсхолдер `RAXD_BASE_URL` как плейсхолдер; реальный URL НЕ выдуман.
- LICENSE отсутствует — указано честно (README §License → ссылка на production-readiness).

## 6. Что НЕ делалось (границы роли)

- Код/спеки НЕ менялись (Author tier: только Read/Grep/Glob/Write). Багов в коде при сверке не
  выявлено — расхождения были только в доке (development.md устарела), исправлены в доке.
- Коммит НЕ делался (это задача дирижёра).

## 7. Что проверить tech-writer-guardian

1. **docs↔код**: что development.md теперь соответствует реальной раскладке (`internal/cmdexec`,
   `internal/fileupload`, `internal/service`, `exec_tool.go`, `upload_tool.go`) и сигнатуре
   `NewHandler(ver, audit, execCfg, uplCfg)` — сверить с `internal/mcp/server.go`.
2. **Версия-пример**: единый `v0.1.0` во всех docs; формат совпадает с `internal/version.Info()`;
   отсутствуют остаточные `1.0.0`/`2025-06-01` (Grep даёт 0).
3. **README status-пример**: суффикс `(not found, defaults applied)` соответствует
   `internal/cli/status.go`.
4. **production-readiness.md**: полнота списка (10 пунктов), честность, корректность ОР-ссылок на
   `specs/distribution/threat-model.md`, наличие LICENSE-пункта.
5. **Автор**: `Vladimir Kovalev, OEM TECH` во всех 10 docs + README; консистентность написания.
6. **Перекрёстные ссылки/якоря**: резолвятся (особенно новые ссылки на production-readiness и его
   внутренние якоря вида `#6-service-uninstall-keeps-the-raxd-user-and-data-uid-reuse`).
7. **Полнота пути пользователя**: установка → команды → MCP → troubleshooting → production-readiness
   — без пустых разделов; всё либо раскрыто, либо честно помечено как pending/None с причиной.
8. **MCP-схемы**: имена полей вход/выход и `additionalProperties:false` в mcp.md ↔
   `internal/mcp/{exec_tool,upload_tool,tools}.go`.
