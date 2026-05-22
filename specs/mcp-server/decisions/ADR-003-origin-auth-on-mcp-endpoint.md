# ADR-003 (mcp-server): Origin-валидация и Bearer-auth на MCP-эндпоинте

> Решение по развилке Q5 spec `mcp-server`. Статус — **proposed** (финальный выбор за architect/security).
> Опирается на ADR-002 (Origin/Host) и ADR-001 (HTTP/TLS) задачи `tls-transport`. Нумерация локальна.

## Контекст
MCP-эндпоинт `/mcp` монтируется за уже готовой middleware-цепочкой транспорта. Спека MCP (2025-11-25)
требует Origin-валидацию как MUST (защита от DNS-rebinding) и аутентификацию для всех соединений; при
этом raxd обслуживает преимущественно НЕ-браузерные клиенты (ИИ-агенты: curl/SDK), которые `Origin`
обычно не шлют. Open Question Q5: достаточно ли готового Origin/Host-middleware транспорта для MCP,
нужен ли явный allowlist Origin для браузерных MCP-клиентов, и как Bearer-auth уживается с
MCP-рекомендациями по auth.

## Решение
**Переиспользовать готовый Origin/Host- и Bearer-middleware транспорта без переписывания**, так как он
уже соответствует требованиям MCP:
1. **Origin (MUST):** спека требует «Servers MUST validate the `Origin` header … If the `Origin`
   header is present and invalid, servers MUST respond with HTTP 403 Forbidden». Готовый middleware
   (ADR-002 tls-transport) делает ровно это: 403 при present&invalid, пропуск при отсутствии Origin
   (не-браузерные клиенты), + Host-allowlist `localhost`/`127.0.0.1`/`::1` как defense-in-depth.
   → https://modelcontextprotocol.io/specification/2025-11-25/basic/transports ;
   `specs/tls-transport/decisions/ADR-002-origin-validation-timing.md`
2. **Bind localhost (SHOULD) + bind 127.0.0.1 по умолчанию** — уже в транспорте (AC7 tls-transport),
   совпадает с MCP «SHOULD bind only to localhost». → transports (URL выше)
3. **Bearer-auth ДО MCP-обработки:** ключ из `Authorization: Bearer rax_live_…` проверяется
   middleware транспорта (`keystore.Verify`) ДО любой MCP-логики (AC2 mcp-server). Это согласуется с
   MCP best-practice «MCP servers that implement authorization MUST verify all inbound requests» и
   «MCP Servers MUST NOT use sessions for authentication» — auth у нас отдельный транспортный слой,
   НЕ MCP-сессия. → https://modelcontextprotocol.io/specification/2025-11-25/basic/security_best_practices

Явный allowlist Origin для конкретных браузерных MCP-клиентов — НЕ вводить в v1 по умолчанию
(основные клиенты raxd не браузерные; базовый MUST уже покрыт), но оставить конфигурируемым через тот
же `OriginAllow` транспорта, если появится браузерный клиент (architect/security включает при
необходимости).

## Альтернативы
- **Своя Origin/auth-логика на уровне MCP (внутри SDK-handler).** Хуже: дублирует готовый middleware,
  нарушает контракт «транспорт не переписывается», создаёт второй путь auth. Отвергнут.
- **Строгий Origin (403 и при отсутствии заголовка).** Сломал бы не-браузерные MCP-клиенты (curl/SDK
  Origin не шлют); спека требует 403 только при present&invalid. Отвергнут (как в ADR-002 tls-transport).
- **Обязательный явный Origin-allowlist уже в v1.** Избыточно: целевые клиенты не браузерные; добавит
  конфигурацию без выгоды. Отложено как опция. → transports (URL выше)

## Последствия
- **Плюсы:** соответствие MCP MUST/SHOULD и baseline §2 без нового кода; auth — единый транспортный
  гейт (AC2/AC8/AC9); не ломает не-браузерные клиенты; путь к браузерным клиентам через существующий
  `OriginAllow` без переделки.
- **Минусы / цена:** при появлении браузерного MCP-клиента нужно вручную наполнить `OriginAllow`
  доверенными origin; «лёгкая» Origin-политика (без полной браузерной матрицы) — осознанная отсрочка.
- **Влияние на стек:** новых зависимостей не вводит; реализуется существующим stdlib-middleware
  транспорта. Известные ограничения КЛИЕНТОВ (проброс кастомных заголовков на initialize, self-signed
  TLS) документируются tech-writer (AC15), на серверную логику не влияют. → inspector#584/#879,
  claude-code#29562 (см. research.md раздел G).

## Статус (proposed|accepted)
proposed
