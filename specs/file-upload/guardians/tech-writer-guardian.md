# tech-writer-guardian — задача `file-upload` (раунд 1)

**Verdict: needs-changes.** Guardian (read-only). Дата: 2026-05-22. Ветка feature/file-upload.
Сохранено дирижёром (guardian не пишет сам — red line 1).

Проверены docs `upload_file`: `docs/mcp.md` (раздел upload_file + интеграция), `docs/configuration.md`
(секция upload), `docs/file-upload-security.md` (новый), `docs/troubleshooting.md`, `docs/commands.md`,
`README.md`, `specs/file-upload/docs-outline.md`. Сверка с кодом upload_tool.go / fileupload / config.go /
audit.go / auth.go.

## Подтверждено (pass-пункты)
- UploadInput 4 поля / UploadOutput РОВНО 4 поля (path/size/overwritten/mode), без абс.пути/содержимого —
  `upload_tool.go:59-68`. ✔
- mode-политика пост-F-1: `mode&^0o777 != 0` + world-writable `0o002` → ErrBadMode — `mode.go:51-58`,
  `config.go:283-289`. ✔
- Дефолты config: root=`<StateDir>/uploads`, max_file_bytes=716800, default_mode="0600",
  overwrite_default=false, deny_root=false; стартовая валидация — `config.go:171-175,219-233`. ✔
- Mandatory: 4xx/400 (не 413) — честно, в mcp.md/configuration.md/troubleshooting.md, расхождение со
  spec/mcp-spec отмечено, «файл не создан» указано, два случая (тело>max_body_bytes→400; decoded>
  max_file_bytes→deny) различены. ✔ Mount points/bind-mount (SR-70/ОР-U2), секреты в путях (путь
  логируется, содержимое нет), root-WARN/deny_root, нет disk-quota (ОР-U3) — присутствуют. ✔
- **Fingerprint:** docs корректно описывают `server.FingerprintFromContext(ctx)` (fingerprint КЛЮЧА из
  ctx, установлен authMiddleware), НЕ `sha256(decoded)`. Ошибочный текст impl-notes §15 зафиксирован в
  docs-outline как замечание. `upload_tool.go:102`, `auth.go:30-38`. ✔
- Язык: docs/** английский, docs-outline русский. Автор Vladimir Kovalev OEM TECH сохранён. ✔
- Curl-примеры корректны (/mcp, Bearer, tools/call). Выдуманного содержания/неверных примеров НЕ найдено.

## Issues (minor, needs-changes)

**Issue 1 (minor): таблица аудита — `path=` в WARN-строке условный, подан как всегда присутствующий.**
- `docs/mcp.md` (таблица формата аудита) и `docs/file-upload-security.md:148`: строка WARN показывает
  `... tool=upload_file reason=running-as-root… path=<rel>` — подразумевает, что `path=` всегда есть.
- Код: `audit.go:157-175` ветка `case "warn"/isUpload` добавляет `path=` только при `rec.Path != ""`;
  `upload_tool.go:109-133` эмитит root-WARN с `Path:""` (путь ещё не валидирован) → реальная WARN-строка
  **без** `path=`. Inline-пример в mcp.md (строка ~896) корректен (без path=), но таблица противоречит.
- Fix: в таблицах формата (mcp.md + file-upload-security.md) пометить `path=` в WARN как опциональный
  (`[path=<rel>]`) + примечание, что root-WARN предшествует парсингу пути и поля path= не содержит.

**Issue 2 (minor): `additionalProperties:false` описан как явное свойство схемы.**
- `docs/mcp.md:419-423` (и аналоги): «input schema is strict (`additionalProperties: false`)».
- В коде нет ручной JSON Schema с этим полем — строгость даёт официальный go-sdk при typed handler
  `ToolHandlerFor[UploadInput, UploadOutput]`, инференцируя схему из Go-структуры. (Та же формулировка
  принята для execute_command — проектная конвенция, но факт гарантируется поведением SDK, не явным полем.)
- Fix: уточнить, что строгость инференцируется SDK из typed handler (напр. «the SDK derives a strict
  schema from the typed handler — unknown input fields are rejected»), не утверждать явное поле схемы.
  Не расширять scope на переписывание раздела execute_command.

## Итог
needs-changes (2 minor про точность аудит-таблиц и формулировку валидации). Блокирующих/выдуманных
расхождений нет; основная проверка (fingerprint ключа, а не хэш содержимого) — пройдена. Возврат к
tech-writer на точечные правки.

---

## Раунд 2 — подтверждение закрытия 2 minor-issue

**Verdict: pass.** Дата: 2026-05-22. Сохранено дирижёром.

- **Issue 1 (закрыт):** `docs/mcp.md:866,870-873` и `docs/file-upload-security.md:148,150-154` — строка WARN
  помечает `path=` опциональным (`[path=<rel>]`) + примечание: root-WARN предшествует парсингу пути → без
  `path=`; при `deny_root=true` отдельная DENY-запись несёт `path=<rel>`. Inline-примеры WARN корректны
  (без path=). Сверено с audit.go:157-175 + upload_tool.go:109-133.
- **Issue 2 (закрыт):** `docs/mcp.md:420-425` — строгость инференцируется go-sdk из typed handler
  `ToolHandlerFor[UploadInput,UploadOutput]`, не заявляется явное поле схемы; смысл (лишние поля → isError,
  файл не создан) сохранён.
- Новых неточностей/противоречий нет; cross-ref/якоря (`mcp.md#upload_file-audit-records`) резолвятся;
  `docs-outline.md:85-101` фиксирует закрытие обоих issue.

Документация upload_file принята. file-upload готов к закрытию (коммиты дирижёра + merge в develop).
