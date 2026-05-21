# План реализации: экспорт пользователей в CSV

## Chosen Approach

Streaming-ответ через `res.write()` в чанках по 500 строк, без буферизации всего результата в памяти. Используем библиотеку `csv-stringify` для корректного экранирования по RFC 4180. Запрос к БД выполняется keyset-пагинацией по `id` для стабильного порядка при concurrent updates. Альтернатива «загрузить весь результат в память и отдать одним блобом» отвергнута: при росте базы до 200k+ активных пользователей buffered-вариант даст OOM на пиках, а streaming держит память константной независимо от размера выборки. Дополнительная стоимость — аккуратное закрытие соединения при ошибке посередине стрима — оправдана.

## Modules

- `src/routes/users/export.ts` — новый route handler. Экспортирует одну функцию `exportActiveUsersCsv(req, res)`. Подключается в `src/routes/index.ts` через `router.get('/users/export.csv', requireRole('admin'), exportActiveUsersCsv)`.
- `src/services/users.service.ts` — добавить метод `streamActiveUsers(batchSize: number): AsyncIterable<UserRow>`. Внутри keyset-пагинация по `id ASC`, WHERE `status = 'active'`.
- `src/middleware/auth.ts` — переиспользуем существующий `requireRole('admin')`. Не модифицируем, только импортируем.
- `src/lib/csv.ts` — новый thin wrapper над `csv-stringify`. Дефолты: `delimiter: ','`, `quoted_string: true`, `record_delimiter: '\r\n'`, RFC 4180 strict mode.
- `src/routes/users/export.spec.ts` — integration-тесты с тестовой БД (фикстуры: 3 active, 1 suspended, 1 deleted, 1 user с запятой в name, 1 с кавычкой).

## Contracts

```
GET /api/users/export.csv

Request headers:
  Authorization: Bearer <token>
  Accept: text/csv

Response 200:
  Content-Type: text/csv; charset=utf-8
  Content-Disposition: attachment; filename="users-export-YYYY-MM-DD.csv"
  Transfer-Encoding: chunked
  Body: streaming CSV
    id,email,name,created_at,status
    1,a@example.com,Alice,2025-01-15T10:00:00Z,active
    ...

Response 403:
  Content-Type: application/json
  Body: { "error": "forbidden", "message": "admin role required" }

Response 500:
  Content-Type: application/json
  Body: { "error": "internal", "message": "export failed" }
  Side effect: server log с полным stack trace и request_id
```

## Trade-offs

- **Streaming vs buffered in memory**: выбран streaming. Дороже по сложности (нужно правильно закрывать соединение при ошибке после первого write), зато безопасно при росте таблицы и не блокирует event loop на больших выборках.
- **`csv-stringify` vs ручное экранирование**: библиотека. Дополнительная зависимость 12kb gzipped, зато RFC 4180 compliance из коробки — edge cases с кавычками в именах, переводами строк в адресах, unicode-символами уже покрыты maintainer'ами. Своё экранирование почти наверняка пропустило бы хотя бы один кейс.
- **Keyset pagination vs offset/limit**: keyset (`WHERE id > :last_id ORDER BY id LIMIT 500`). Стабильный порядок при concurrent INSERT/UPDATE во время длительного экспорта; offset мог бы пропустить или дублировать строки при сдвиге окна. Требует индекс по `id`, который уже есть как PRIMARY KEY.
