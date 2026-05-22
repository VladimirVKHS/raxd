# mcp-server — живая проверка в Docker (дирижёр)

Выполнена дирижёром после merge `feature/mcp-server` в `develop` (мандат «проверять самому
в Docker», SECURITY-BASELINE §6). Дата: 2026-05-22.

## Среда

- Образ пересобран с MCP-кодом: `docker build --target build -t raxd-build .`
  (`go vet ./...` + `go build` — без ошибок).
- Контейнер `raxd-mcp-demo`: `config.yaml` с `bind_addr: 0.0.0.0`, порт 7822, публикация
  `127.0.0.1:8443->7822`. Старый `raxd-demo` (tls-сборка) снесён.
- Ключ создан в контейнере: `raxd key create --name demo` →
  id `9272d7b86fc397ff`, fingerprint `be7acedf0c13` (ключ — одноразово в выводе, тело не сохраняем).
- Запуск `raxd serve`: баннер с автором (Vladimir Kovalev, OEM TECH), TLS 1.3,
  cert/key сгенерированы (`key.pem` 0600), `listening https://0.0.0.0:7822`.

## Результаты end-to-end (curl с хоста на `https://127.0.0.1:8443/mcp`, `-k`)

| Проверка | Ожидание | Факт | Итог |
|---|---|---|---|
| `initialize` (Bearer валидный) | 200 + capabilities.tools + protocolVersion 2025-11-25 + serverInfo | 200, `{"capabilities":{"logging":{},"tools":{"listChanged":true}},"protocolVersion":"2025-11-25","serverInfo":{"name":"raxd","version":"dev"}}` | ✅ |
| `tools/call ping` | 200, text `pong`, без `isError` | 200, `{"content":[{"type":"text","text":"pong"}],"structuredContent":{}}` | ✅ |
| `tools/call server_info` | 200, `{name,version,protocolVersion}` + text, без секретов | 200, text `raxd dev (MCP 2025-11-25)`, structuredContent `{name:raxd, protocolVersion:2025-11-25, version:dev}` | ✅ |
| `tools/call execute_command` (неизвестный) | JSON-RPC error, НЕ исполнен | `error{code:-32602, message:"unknown tool \"execute_command\""}`, команда не запущена | ✅ |
| `initialize` без Bearer | 401 | 401 | ✅ |
| `initialize` с неверным ключом | 401 (Unauthorized; 403 — для Origin-reject/corrupt-store) | 401 | ✅ |
| GET `/mcp` (Bearer валидный) | 405 | 405 | ✅ |
| Не-`/mcp` путь (`/`) | 501 (заглушка диспетчера) | 501 | ✅ |
| Bad `Origin: https://evil.example.com` | 403 (защита от DNS-rebinding) | 403 | ✅ |
| Allowed `Origin: http://localhost` | 200 | 200 | ✅ |

## Аудит и отсутствие утечек

- Аудит-строки в stderr контейнера:
  - `INFO AUTH fp=be7acedf0c13 remote=172.17.0.1:<port>` на каждый аутентифицированный запрос.
  - `INFO MCP fp=be7acedf0c13 remote=172.17.0.1:63828 tool=ping result=ok`
  - `INFO MCP fp=be7acedf0c13 remote=172.17.0.1:63844 tool=server_info result=ok`
  - `remote` в MCP-строке совпадает с `remote` соответствующей AUTH-строки того же запроса.
- Тело ключа (`rax_live_…`) в логах НЕ встречается (grep = 0).
- Неизвестный инструмент порождает AUTH-строку, но не `tool=`-строку: handler не вызывается
  (отказ на уровне диспетчера SDK до `withAudit`) — команда не исполняется. Транспортный AUTH
  фиксирует факт запроса.

## Примечания

- `version=dev` ожидаемо: сборка `--target build` без ldflags (документировано в docs/mcp.md).
- `capabilities.tools.listChanged=true` и наличие `logging:{}` — производятся SDK; docs/mcp.md
  явно оговаривает «exact JSON shape … is produced by the SDK».
- Неизвестный инструмент в этой версии SDK вернул `-32602`; docs корректно допускают
  `-32601 | -32602` в зависимости от версии SDK.

**Вывод: MCP-сервер работает end-to-end в Docker, безопасность (auth/Origin/method/audit/
no-secrets) подтверждена вживую. mcp-server закрыт.**
