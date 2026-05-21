# ADR-001 (tls-transport): HTTP/TLS как сетевой протокол вместо сырого TCP

> Решение по развилке Q1 spec `tls-transport`. Статус — **proposed** (финальный выбор за architect).
> Нумерация локальна для задачи tls-transport; глобальная политика вендоринга — отдельный
> `specs/key-management/decisions/ADR-002-vendoring-offline-builds.md`.

## Контекст
Spec `tls-transport` требует сетевой фундамент: TCP-listener в TLS 1.3, аутентификация по API-ключу
до обработки, rate-limit, аудит, health-обработчик (ping→pong). Open Question Q1: какой протокол
поверх TLS — сырой TCP с кастомным фреймингом или HTTP/TLS. Дефолт PM — HTTP/TLS. Выбор влияет на
форму AC4 (как передаётся ключ), на Q2 (Origin) и на совместимость с будущими `mcp-server`,
`command-exec`, `file-upload`.

## Решение
Использовать **HTTP/1.1+ поверх TLS** (`net/http` `http.Server` над `crypto/tls`-конфигом). API-ключ
передаётся заголовком `Authorization: Bearer rax_live_…` (НЕ argv/env, AC4). Аутентификация —
HTTP-middleware ДО маршрутизации к health-обработчику. Это обеспечивает прямую совместимость с
будущим MCP Streamable HTTP: официальный Go SDK экспортирует `StreamableHTTPHandler` как
`http.Handler` (`NewStreamableHTTPHandler(...)`/`ServeHTTP`), подключаемый как маршрут того же mux.
Источники:
- MCP Streamable HTTP — строго HTTP (single endpoint, POST+GET): https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
- SDK как http.Handler, v1.6.0 (30.04.2026): https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp
- TLS-конфиг сервера/MinVersion: https://pkg.go.dev/crypto/tls
- Graceful shutdown: https://pkg.go.dev/net/http#Server.Shutdown

## Альтернативы
- **Сырой TCP + кастомный фрейминг поверх `tls.Listen`.** Рассматривали ради полного контроля и
  минимального оверхеда. Хуже: переизобретает фрейминг/таймауты/keep-alive, которые даёт `net/http`;
  нет нативных Origin/Host-заголовков (DNS-rebinding-защиту и передачу ключа надо делать кастомным
  handshake); прямой конфликт с целью «совместимость с MCP Streamable HTTP» (MCP только HTTP →
  понадобится отдельный листенер/мост); сложнее тестировать (нет `httptest`); graceful shutdown руками.
  Спека жёсткого требования минимального бинарного протокола НЕ ставит. Источник по MCP-HTTP:
  https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
- **mTLS как транспортная аутентификация вместо ключа в заголовке.** Вне scope (Q3, отложено
  дирижёром); см. baseline §2. Не отменяет выбор HTTP/TLS.

## Последствия
- **Плюсы:** AC1 (TLS1.3), AC4/AC5 (ключ заголовком + middleware), AC8/AC9 (аудит с Fingerprint),
  AC12 (`Server.Shutdown(ctx)`), AC14 (`httptest`) закрываются зрелым stdlib без ручного фрейминга;
  будущий MCP/command-exec/file-upload встраиваются как маршруты без смены транспорта.
- **Минусы / цена:** HTTP-оверхед на кадр (для health/команд пренебрежимо); долгоживущие SSE-стримы
  будущего MCP требуют аккуратной связки с `Shutdown` (отмена контекста) — вне scope этой задачи.
- **Влияние на стек (сверка STACK.ru.md):** транспорт = stdlib (`net/http`+`crypto/tls`), новых
  runtime-зависимостей под сам транспорт НЕ вводит. MCP SDK (уже в STACK) подключится позже без
  смены транспорта. Появляется только `golang.org/x/time/rate` для rate-limit — см. ADR-002 этой задачи.

## Статус (proposed|accepted)
proposed
