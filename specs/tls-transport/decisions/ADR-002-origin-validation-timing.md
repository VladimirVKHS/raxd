# ADR-002 (tls-transport): тайминг и реализация Origin/Host-валидации

> Решение по развилке Q2 spec `tls-transport`. Статус — **proposed** (финальный выбор за architect).
> Зависит от ADR-001 (HTTP/TLS). Нумерация локальна для задачи tls-transport.

## Контекст
baseline §2 требует валидацию `Origin` для HTTP/MCP-эндпоинта (защита от DNS-rebinding). MCP-спека
(2025-11-25) предписывает это как MUST для браузерных клиентов. Но raxd обслуживает преимущественно
НЕ-браузерные клиенты (ИИ-агенты: curl/SDK), которые заголовок `Origin` обычно не отправляют. Open
Question Q2: как именно валидировать и когда включать (сразу или под `mcp-server`).

## Решение
Защиту от DNS-rebinding строить как **многослойную связку**, а Origin-валидацию закладывать
middleware сразу:
1. **Bind по умолчанию `127.0.0.1`** (baseline §2; MCP «SHOULD bind only to localhost») — AC7.
2. **Аутентификация по API-ключу — основной гейт** (AC4): запрос без валидного ключа отсекается до
   обработки, что нейтрализует rebinding-эксплойт независимо от заголовков.
3. **Origin-middleware:** если `Origin` ПРИСУТСТВУЕТ и не входит в allowlist → **403** (MCP MUST);
   если `Origin` ОТСУТСТВУЕТ → НЕ отклонять только по этому признаку (иначе ломаются легитимные
   не-браузерные клиенты), полагаться на bind + ключ. Дополнительно — **Host-allowlist**
   (`localhost`/`127.0.0.1`/`::1`) как defense-in-depth (паттерн из фикса rmcp). Каркас middleware
   ставится уже в tls-transport (точка расширения); полная браузер-ориентированная Origin-политика
   обязательна с включением `mcp-server`.

Источники:
- MCP MUST по Origin + 403 при present&invalid, SHOULD bind localhost:
  https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
- Host-allowlist как фикс DNS-rebinding (rmcp, дефолт localhost/127.0.0.1/::1, 403 вне списка):
  https://github.com/modelcontextprotocol/rust-sdk/security/advisories/GHSA-89vp-x53w-74fx
- MCP best practices (verify all inbound requests; require auth token для HTTP-транспорта):
  https://modelcontextprotocol.io/specification/2025-11-25/basic/security_best_practices

## Альтернативы
- **Строгий Origin-обязателен (отклонять и при отсутствии заголовка).** Соответствовало бы «жёсткому»
  чтению, но сломало бы не-браузерные raxd-клиенты, которые Origin не шлют; спека требует 403 только
  при present&invalid. Отвергнут.
- **Полностью отложить Origin до `mcp-server`, в tls-transport ничего не закладывать.** Минус: при
  включении MCP пришлось бы переделывать middleware-цепочку; дешевле заложить точку расширения сразу.
  Частично принят как fallback (полная политика — в mcp-server), но каркас ставим уже сейчас.
- **Полагаться только на TLS без Origin/Host-проверок.** Минус: TLS не защищает от DNS-rebinding
  (атака идёт через легитимный браузер жертвы); baseline §2 явно требует Origin-валидацию. Отвергнут.

## Последствия
- **Плюсы:** соответствие baseline §2 и MCP MUST; не ломает не-браузерные клиенты; bind+ключ дают
  реальную защиту уже в v1; путь к mcp-server без переделки.
- **Минусы / цена:** Origin/Host allowlist надо держать конфигурируемым (значения хостов/портов);
  в tls-transport middleware пока «лёгкий» (без полной браузерной политики) — это осознанная отсрочка.
- **Влияние на стек:** реализуется stdlib (`net/http` заголовки), новых зависимостей не вводит.

## Статус (proposed|accepted)
proposed
