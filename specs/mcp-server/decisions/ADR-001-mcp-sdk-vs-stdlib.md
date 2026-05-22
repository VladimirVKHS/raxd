# ADR-001 (mcp-server): официальный Go MCP SDK против минимальной stdlib-реализации

> Решение по развилке Q1 spec `mcp-server`. Статус — **proposed** (финальный выбор за architect).
> Нумерация локальна для задачи mcp-server. Глобальная политика вендоринга — отдельный
> `specs/key-management/decisions/ADR-002-vendoring-offline-builds.md`.

## Контекст
Spec `mcp-server` заменяет заглушку-диспетчер (501) реальным MCP-эндпоинтом Streamable HTTP поверх
готового HTTP/TLS-транспорта. Open Question Q1: реализовать MCP на официальном Go SDK
(`github.com/modelcontextprotocol/go-sdk`, экспортирует `StreamableHTTPHandler` как `http.Handler`)
или минимально на stdlib (`net/http`+`encoding/json`, JSON-RPC 2.0). Главное ограничение (ADR-002):
сборка/тесты — только в Docker без доступа к `proxy.golang.org`; проект вендорится (`vendor/` в git,
`-mod=vendor`), `go mod vendor` — на хосте. Поэтому реализуемость зависит от того, надёжно ли
вендорится SDK офлайн (размер дерева, лицензия, CGO, стабильность).

## Решение
Использовать **официальный Go MCP SDK** (`github.com/modelcontextprotocol/go-sdk/mcp`, v1.6.0),
монтируя `mcp.NewStreamableHTTPHandler(...)` (это `http.Handler`) как маршрут `/mcp` за уже готовой
middleware-цепочкой транспорта (Bearer-auth → Origin/Host → rate-limit → audit ДО MCP-обработки).
Инструменты `ping`/`server_info` регистрируются типизированно (`AddTool`). Офлайн-вендоринг
выполняется по ADR-002: `go mod vendor` на хосте → коммит `vendor/`+`go.sum` → сборка `-mod=vendor`,
`CGO_ENABLED=0`.

Решающее обоснование (офлайн-вендоринг РЕАЛИЗУЕМ):
- Пакет `mcp` (который импортируем) тянет в импорт-граф только `google/jsonschema-go` (zero внешних
  зависимостей) и `yosida95/uritemplate/v3` (+ internal-пакеты SDK). Тяжёлые `golang.org/x/tools`,
  `go-cmp`, `oauth2`, `jwt`, `segmentio/encoding` пакетом `mcp` НЕ импортируются (они для
  codegen/тестов/OAuth-подпакетов). → https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp?tab=imports
- `go mod vendor` копирует только пакеты, нужные для сборки/тестов main-модуля, а не всё go.mod-
  замыкание SDK → реальное вендорённое дерево малое. → https://go.dev/ref/mod
  - *Установленный факт* (по импорт-графу `mcp` на pkg.go.dev): внешние зависимости пакета `mcp` —
    `jsonschema-go` + `uritemplate/v3`. *Ожидаемое следствие*: ожидаемый состав `vendor/` по
    импорт-графу (pkg.go.dev/imports) — SDK + эти два модуля; точный список пакетов в `vendor/`
    подтверждается прогоном `go mod vendor` на хосте до commit (OQ-1 research). Рекомендация (SDK)
    от этого не зависит — вендоринг реализуем в любом случае.
- Все элементы pure Go, без CGO, amd64+arm64; лицензии permissive (Apache-2.0/MIT/BSD-3/MIT-0).
  → https://github.com/modelcontextprotocol/go-sdk/blob/v1.6.0/LICENSE ,
  https://github.com/yosida95/uritemplate , https://pkg.go.dev/github.com/google/jsonschema-go/jsonschema?tab=imports
- Требование SDK по версии Go удовлетворено: `go.mod` SDK на теге v1.6.0 объявляет `go 1.25.0`
  (SDK требует Go ≥1.25.0); проект raxd УЖЕ на Go 1.25 — `go.mod` raxd содержит `go 1.25.0`, а
  Dockerfile использует базовый образ `golang:1.25`. Это НЕ блокер сборки.
  → https://raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.6.0/go.mod ;
  `go.mod` (строка `go 1.25.0`) ; `Dockerfile` (`FROM golang:1.25`)
- SDK уже выбран `STACK.ru.md` («MCP-сервер → go-sdk/mcp, официальный v1.x») и учтён в ADR-002
  (вендоринг считал MCP SDK частью ~37 зависимостей). ADR-001 tls-transport спроектировал точку
  расширения именно под `StreamableHTTPHandler` (`http.Handler`).
  → `.claude/reference/STACK.ru.md` ; `specs/key-management/decisions/ADR-002-vendoring-offline-builds.md` ;
  `specs/tls-transport/decisions/ADR-001-http-tls-over-raw-tcp.md`

Версия и API SDK: v1.6.0 (30.04.2026), `NewServer`/`AddTool`/`Tool`/`NewStreamableHTTPHandler`.
Состав go.mod SDK на теге v1.6.0 (7 прямых + 2 indirect; цитата по тегу, не по ветке `main`) —
см. research.md раздел E. → https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp ,
https://raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.6.0/go.mod

## Альтернативы
- **Минимальная stdlib-реализация (`net/http`+`encoding/json`, JSON-RPC 2.0).** Рассматривали ради
  максимального минимализма зависимостей (в духе stdlib-транспорта и отказа от `adrg/xdg`/lipgloss):
  ноль новых вендорённых пакетов, минимальный `vendor/`-диф, нет завязки на релизы SDK. Спека
  достижима на stdlib (один handler `/mcp`, парс JSON-RPC, методы initialize/tools-list/tools-call,
  объявить `tools` capability + `protocolVersion 2025-11-25`). Хуже: ручное соответствие спеке и его
  поддержка при эволюции (06-18 → 11-25 — заметный темп изменений); совместимость с Claude/inspector
  надо валидировать руками (SDK даёт её как референс); ручная JSON Schema и обработка JSON-RPC-ошибок
  (риск багов в AC7); расширение под `command-exec`/`file-upload`/Resources — снова свой код; идёт
  ВРАЗРЕЗ с уже принятым STACK. Оправдан был бы, ТОЛЬКО если офлайн-вендоринг SDK ненадёжен — но он
  надёжен (см. Решение). Остаётся честным запасным вариантом. Источники: спека
  https://modelcontextprotocol.io/specification/2025-11-25/basic/transports ,
  https://modelcontextprotocol.io/specification/2025-11-25/server/tools ,
  changelog https://modelcontextprotocol.io/specification/2025-11-25/changelog
- **Community SDK `mark3labs/mcp-go`.** MCP-INTEGRATION прямо предписывает предпочесть официальный SDK
  community-варианту; не рассматривали как основной. → `.claude/reference/MCP-INTEGRATION.ru.md`

## Последствия
- **Плюсы:** spec-compliance и клиентская совместимость «из коробки»; `StreamableHTTPHandler` —
  `http.Handler`, монтируется в готовый mux без переписывания транспорта (ADR-001 tls-transport);
  типизированная регистрация инструментов и автогенерация JSON Schema снижают ручной код/ошибки
  (AC3/AC4/AC7); прямой путь к `command-exec`/`file-upload`/Resources без смены подхода (AC13).
- **Минусы / цена:** +ветка зависимостей в `vendor/` (малая: SDK + jsonschema-go + uritemplate/v3) →
  рост `vendor/`-дифа; завязка на темп релизов SDK (апдейт → `go mod vendor` на хосте + коммит);
  для минимального ping/server_info часть мощи SDK избыточна.
- **Влияние на стек (сверка STACK.ru.md):** новых ВНЕплановых зависимостей НЕ вводит — SDK уже в
  STACK и в ADR-002. Транзитивно в `vendor/` добавятся `google/jsonschema-go` и
  `yosida95/uritemplate/v3` (pure Go, permissive, без CGO). `CGO_ENABLED=0` сохраняется. Версия Go
  проекта (1.25) удовлетворяет требованию SDK (go 1.25.0) — менять go-версию не нужно. Хендофф
  devops/distribution: goreleaser/CI собирают из `vendor/` `-mod=vendor` (как уже зафиксировано ADR-002).
- **Рекомендация дирижёру (не блокер):** в `STACK.ru.md` ориентир «Go 1.22+» (строка про `crypto/tls`)
  стоит обновить до фактического минимума проекта «Go 1.25», совпадающего с требованием MCP SDK v1.6.0.
  → `.claude/reference/STACK.ru.md` ; https://raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.6.0/go.mod

## Статус (proposed|accepted)
proposed
