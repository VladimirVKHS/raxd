# security-guardian — задача `file-upload`

**Итоговый verdict: pass** (раунд 1). Опасная фича (запись в ФС хоста) — планка максимальная.
Блокеров нет, 3 info-наблюдения. Сохранено дирижёром.

Артефакты: threat-model.md (R-U1..R-U13, П-U1/П-U2, ОР-U1..ОР-U6) + security-requirements.md
(SR-68..SR-82, 15 SR). Сверено с обязательным SECURITY-BASELINE §3/§4, spec, plan, ADR-001/002/003,
реальным кодом (audit.go, config.go, vendor logfmt/charmbracelet).

## Покрытие baseline — полное (применимое)
§3: traversal через os.Root + ранний IsLocal + тест-векторы (SR-69), границы os.Root (mount/chmod-race
обойдён chmod по fd, SR-69/70+ОР-U2), безопасный дефолт корня (SR-71), overwrite/каталог-цель (SR-72),
mode без опасных битов (SR-73), не от root (SR-77, П-U2), лимиты входа/DoS (SR-75/76), устойчивость
к битому входу (SR-75), атомарность (SR-74). §4: один аудит со всеми полями (SR-78), path/size (SR-79),
без секретов/содержимого/абс.пути (SR-80), rate-limit наследуется (SR-68), ротация системная (ОР-U4).
§6: Docker/vendor (SR-82). Транспорт §1/§2 наследуется, не переоткрыт. Дыр нет.

## Полнота модели угроз
Все классы покрыты: path traversal (R-U2 +векторы), границы os.Root (R-U3), overwrite/цель-каталог
(R-U4), mode (R-U7+ADR-003), DoS диск/размер (R-U5/6, ОР-U3 disk-quota), частичный файл (R-U6),
эскалация root (R-U9, ОР-U1), утечка содержимого/абс.пути/ключа (R-U12/13), symlink/спецфайлы вне
scope (R-U7), logfmt-инъекция через путь (R-U13). Упущенных нет, каждый риск со смягчением.

## 5 решений по зависимостям architect — обоснованы
1. os.Root границы — принять остаток (chmod по fd обязателен SR-69; mount→док+ОР-U2; openat2 вне
   stdlib). 2. mode ADR-003 достаточна (group-writable не запрещаем — корень 0700, world-writable
   закрыт). 3. root WARN-дефолт + отдельный upload.deny_root (запись≠запуск). 4. max_file_bytes=716800
   подтверждён (потолок ~785KiB, запас ~85KiB) + старт-валидация. 5. AuditRecord Path/Size + isUpload
   зеркально isExec (сверено audit.go:77). Все обоснованы.

## logfmt-инъекция через путь — закрыта (сверено с vendor end-to-end)
charmbracelet/log/logfmt.go → go-logfmt EncodeKeyval → needsQuotedValueRune квотирует <=0x20/=/"/0x7f
/RuneError (encode.go:235-249), writeQuotedString экранирует \n→\n, "→\" (jsonstring.go:37-92).
Путь со спецсимволами/переводом строки → path="..." с экранированием: нельзя подделать result=success
или новую строку лога. Заявление security ВЕРНО по факту кода. Условие: developer передаёт путь как
key/value-аргумент логгера (не в msg) — SR-79 предписывает + компенсирующий тест.

## Отклонения (red line 4) — оформлены
П-U1 (logfmt вместо JSON §4 — наследует command-exec П-1), П-U2 (root WARN-дефолт + опц. deny_root) —
оба с риск+почему+смягчение+остаток. Ротация §4 не снята (ОР-U4).

## Findings (INFO, не блокируют)
- INFO-1 (developer/reviewer): SR-79 закрывает logfmt-инъекцию ТОЛЬКО если путь — key/value-аргумент
  логгера, не интерполяция в msg. Проверить на code-review + тест на спецсимволы.
- INFO-2 (developer): ADR-003 запрещает world-writable но не group-writable — осознанно, не «дотягивать».
- INFO-3 (оркестратор/devops): ОР-U1 (root-запись)/ОР-U2 (mount points)/ОР-U3 (disk-quota)/ОР-U4
  (ротация) эскалировать пользователю ДО прод-релиза; tech-writer/devops документируют/logrotate.

## Что хорошо
Дисциплина наследования (SR-1..67 не переоткрыты, SR-68..82 без конфликтов); каждый риск со смягчением
и каждый SR проверяем; отклонения по контракту; проверка по реальному коду и vendor (не по памяти);
security не вышла за роль (кода нет, AC не тронуты); числовая согласованность лимита.

## Резюме
pass. Передаётся mcp-engineer → developer. Эскалации ОР-U1..ОР-U4 — пользователю до прод-релиза
(зафиксировать в финальном докладе/доке).
