# Guardian Report: architect-guardian — tls-transport

**Дата:** 2026-05-21
**Артефакт:** `specs/tls-transport/plan.md` (сверка со spec.md, research.md, ADR-001/002, SECURITY-BASELINE §1/§2/§4/§6)

## Итог

Выбран РОВНО ОДИН подход — **HTTP/TLS** (`net/http` + `crypto/tls`, bind 127.0.0.1, TLS 1.3,
self-signed ECDSA P-256, фиксированная цепочка middleware до `http.ServeMux`); сырой TCP явно
отклонён в Trade-offs с ценой. Модули с путями и непересекающимися границами
(`internal/server/{server,tls,middleware,auth,ratelimit,audit,handlers}.go`, `internal/cli/serve.go`,
расширение `internal/config`). Контракты содержат сигнатуры с типами и маппинг ошибок на
401/403/429; rate-лимитеры — мьютекс + TTL-GC + остановка горутины по контексту. Поток запроса
end-to-end описан (ключ через Bearer, не argv/env; `keystore.Verify` ДО обработки; аудит по
`Fingerprint`, не телу; `FlushUsage` при shutdown). Все AC1–AC14 привязаны к модулям. Сигнатуры
сверены с реальным кодом keystore/config (`Verify(...) (Record, bool, error)`, `FlushUsage() error`,
`Fingerprint`, `ErrCorrupt`, `PathSet.TLSDir`). Тел функций нет; AC не изменены; scope не расширен
(command-exec/mcp/file-upload — только точки расширения). Новая зависимость `golang.org/x/time/rate`
обоснована с требованием `go mod vendor`.

## Находки (не блокеры)

| # | Severity | Описание |
|---|----------|----------|
| 1 | info | План ~105 строк — на ~5% выше ориентира 30-100; оправдано объёмом (cert+auth+rate-limit+audit+lifecycle+serve). Опц.: сжать раздел «Тестируемость в Docker» (частично дублирует qa). |
| 2 | low | Маппинг `ErrCorrupt → 403`: штатно `Verify` после успешного `Open` отдаёт `(_,false,nil)`; `ErrCorrupt` от `Verify` — только при повреждении keys.db в рантайме после `Open`. Стоит развести в прозе два пути отказа. **Дирижёр: передано security для формализации маппинга 401/403 в security-requirements.** |
| 3 | info | `server.New` принимает `cfg *config.Config` и `paths config.PathSet` — developer не должен дублировать `TLSDir` внутри `Config`. Без действия. |

## Verdict
pass
