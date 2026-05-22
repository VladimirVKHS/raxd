# mcp-guardian — задача `file-upload`

**Итоговый verdict: pass** (раунд 2). Раунд 1 — needs-changes (F-2 HIGH + F-1 MEDIUM + F-3/F-4 LOW),
все закрыты. Опасная фича. Сохранено дирижёром.

## Раунд 1 — needs-changes

Артефакт: mcp-spec.md (инструмент upload_file). Сверено со spec (20 AC), plan, ADR-001/002/003
(accepted), security-requirements (SR-68..82), реальным кодом и vendored go-sdk. Образец — command-exec.

## Схемы вход/выход — корректны
UploadInput (path+content required без omitempty; overwrite/mode optional+omitempty;
additionalProperties:false из struct). UploadOutput (path/size(int64→integer)/overwritten/mode, без
абс.пути/содержимого, SR-80). outputSchema с required всех 4 полей.

## Error-mapping — верен (1 MEDIUM-оговорка)
Транспорт 413(тело>max_body_bytes ДО инструмента)/401/403/429; протокол -32700/-32600/-32601/-32602;
deny (traversal/exists/isdir/too-large/bad-base64/bad-mode/deny_root) vs fail (диск/IO) разнесены;
двойная запись warn+deny при deny_root=true&euid==0. Консистентно с command-exec.

## Поток и аудит — структура точна, 1 HIGH по рендеру
Цепочка bodyLimit(первый)→recover→Host/Origin→auth→rate-limit→authSuccessAudit→SDK→uploadHandler→
fileupload.Write→аудит — совпадает с кодом. fingerprint/remote из ctx. Содержимое не логируется (SR-80).
Разграничение поле Result vs рендер (§2.3.1) корректно.

## Findings
- **F-2 (HIGH).** Пример warn-записи (§2.3.1/§6.6) обещает `tool=upload_file` в warn-строке, но реальный
  writeAudit (audit.go:117-136) в `case "warn"` логирует `tool=` ТОЛЬКО в ветке `if isExec`; `else`-ветка
  его НЕ пишет. Developer, добавив isUpload зеркально isExec в success/deny/fail, может не перестроить
  warn-ветку → warn-записи upload_file без tool=, qa-тесты по §6.6 упадут. Fix: в §8 явно указать —
  в case "warn" добавить отдельный `else if isUpload` блок с tool=/reason=/path= (как isExec логирует
  tool=/reason=/command=), НЕ оставлять в общем else.
- **F-1 (MEDIUM).** Таблица §4 строка #10 (SDK-валидация входа): текст «...validating arguments...» без
  пометки, что генерируется SDK (qa не проверяет дословно). Оговорка есть в §6.8, но не в таблице.
  Fix: добавить в колонку пометку «(текст от SDK; qa: только isError:true + файл не создан)».
- **F-3 (LOW).** §6.1 пример содержит "isError":false — образец command-exec его опускает. Косметика.
- **F-4 (LOW).** size= с суффиксом B в text-блоке (§5.2) vs без суффикса в логе (§6.1) — разграничить:
  B только в content-тексте, logfmt size= числовое.

## Что хорошо
Детальная диаграмма потока (точнее образца); безупречное разграничение deny/fail (3 места); граница
413/max_body_bytes на всех уровнях; ранний base64.DecodedLen фильтр; logfmt-инъекция через путь
объяснена+закрыта; ADR-004-стиль аудита консистентен; Q-UPL-1..3 неблокирующие; outputSchema строгая.

## Резюме для mcp-engineer
F-2 (HIGH, обязательно — указать перестройку warn-ветки) + F-1 (MEDIUM). F-3/F-4 — косметика.
После — повторный гейт.

## Раунд 2 — pass
- F-2 закрыт: §8 «КРИТИЧНО — рендер upload-веток» предписывает структуру if isExec/else if isUpload/
  else в КАЖДОМ case; для case "warn" расписаны tool=upload_file/reason=/path=; запрещено оставлять
  upload в общем else. §2.3.1 (колонки tool=/прочие), §2.3.2, §6.6 согласованы (tool=upload_file во
  всех ветках через else if isUpload).
- F-1 закрыт: §4 строка #10 — пометка про текст SDK (qa: isError:true+файл не создан).
- F-3 закрыт: §6.1 без "isError":false.
- F-4 закрыт: суффикс B только в content text; structuredContent.size и логи size= числовые.
Новых findings нет, схемы/error-mapping/поток верны. Передаётся developer.
