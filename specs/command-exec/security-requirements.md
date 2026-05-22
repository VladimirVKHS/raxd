# Security Requirements: command-exec — MCP-инструмент `execute_command`

> Каждое требование ПРОВЕРЯЕМО (ответ «выполнено/нет» одним вопросом: тест / grep / инспекция) и
> ссылается на пункт `SECURITY-BASELINE.ru.md`, на AC `spec.md`, на контракт `plan.md`/ADR и на риск
> из `threat-model.md`. Эти требования ОБЯЗАНЫ выполнить `developer` (`internal/cmdexec/*`,
> `internal/mcp/exec_tool.go`, расширения `internal/mcp/server.go`, `internal/server/audit.go`,
> `internal/config/config.go`, `internal/cli/serve.go`), `mcp-engineer` (контракт инструмента/схемы),
> `devops` (логирование/ротация в дистрибуции, CI в Docker), `tech-writer` (предупреждения в доке) и
> `qa` (тесты). Соответствие проверяют `reviewer` и `security-guardian`. Способ проверки везде: тесты
> гоняются в Docker из `vendor/` (`-mod=vendor`, baseline §6, AC18); запуск демона/команд — только в
> контейнере.
>
> **Нумерация.** SR-1…SR-39 заняты `tls-transport` (SR-1…SR-26) и `mcp-server` (SR-27…SR-39) — они
> НАСЛЕДУЮТСЯ (см. раздел «Наследуемые требования») и здесь НЕ дублируются. Требования command-exec
> нумеруются СКВОЗНО с **SR-40** по **SR-67** (без пропусков).
>
> **Терминология.** «Полный ключ» = `rax_live_<base64url>` целиком (заголовок `Authorization: Bearer`).
> «Fingerprint» = `keystore.Fingerprint(...)` (12 hex sha256 ключа, необратим) — в аудите/exec-слое
> используется ВМЕСТО ключа. exec-слой (`internal/cmdexec`/`internal/mcp/exec_tool.go`) к телу ключа
> доступа НЕ имеет (только `server.FingerprintFromContext`/`RemoteAddrFromContext` из ctx).
> «Раннер» = `internal/cmdexec` (чистый запуск); «handler» = `execHandler` (`internal/mcp/exec_tool.go`).

## Поверхность и аутентификация исполнения (baseline §1/§3)

- [ ] **SR-40. Выполнение команд доступно ТОЛЬКО как MCP-инструмент `execute_command` за наследуемой
  цепочкой; отдельной поверхности исполнения НЕТ.** Инструмент регистрируется
  `sdkmcp.AddTool(s, execTool(), execHandler(...))` в `internal/mcp/server.go` (NewHandler), рядом с
  `ping`/`server_info`, ВНУТРИ цепочки `bodyLimit→recover→Host/Origin→auth→rate-limit→authSuccessAudit
  →mux`. Отдельного не-`/mcp` сетевого эндпоинта и CLI-подкоманды `raxd exec` НЕТ; транспорт/маршрут
  `/mcp` НЕ меняется. Проверка: тест — после `serve` `execute_command` присутствует в `tools/list`
  рядом с `ping`/`server_info`; grep — нет второго HTTP-маршрута исполнения и нет CLI-подкоманды
  `exec`; регистрация не добавляет слушающих сокетов. (baseline §3; spec AC1; plan §Chosen Approach;
  threat-model R-1)

- [ ] **SR-41. Аутентификация наследуется и выполняется ДО исполнения команды; неаутентифицированный
  вызов НЕ запускает команду.** `execute_command` проходит ту же Bearer-аутентификацию транспорта
  (`keystore.Verify`) ДО handler: запрос без `Authorization: Bearer` / с неизвестным/отозванным
  ключом → 401, повреждение keystore (`ErrCorrupt`) → 403, до раннера не доходит. Новых каналов
  аутентификации exec-слой НЕ вводит (не вызывает `keystore.Verify`). Проверка: тест —
  неаутентифицированный `tools/call execute_command` → 401, никакая команда НЕ запущена (нет
  дочернего процесса, нет exec-аудита success); grep по `internal/cmdexec`/`internal/mcp` — нет
  вызова `keystore.Verify`. (baseline §1; spec AC12; threat-model R-1; наследует SR-27/SR-28)

- [ ] **SR-42. Rate-limit наследуется: превышение per-key/per-IP → 429 ДО исполнения.** Вызовы
  `execute_command` подчиняются наследуемому `rateLimitMiddleware` (per-key И per-IP, token bucket);
  при превышении → 429 ДО handler, команда НЕ запускается. exec-слой rate-limit НЕ переопределяет и
  НЕ обходит. Проверка: тест — превышение лимита для `execute_command` → 429, команда не запущена.
  (baseline §4 «rate limiting»; spec AC16; threat-model R7/R8 наследуемые; наследует SR-17/SR-18)

## Запуск без shell и без подмены бинаря (baseline §3)

- [ ] **SR-43. Запуск ТОЛЬКО формой «бинарь + список аргументов» без shell-интерполяции.** Раннер
  использует `exec.CommandContext(ctx, bin, args...)`; shell (`sh -c`/`bash -c`) НЕ привлекается ни
  при каких входных данных; shell-метасимволы (`;`, `|`, `$()`, `&&`, `>`, `` ` ``) попадают в процесс
  как ЛИТЕРАЛЬНЫЕ аргументы. Проверка: тест-вектор — `command="echo"`, `args=["a; touch /tmp/pwned"]`
  → аргумент трактуется литерально, `/tmp/pwned` НЕ создаётся (нет побочного эффекта, который дал бы
  только shell); grep по `internal/cmdexec` — отсутствуют `"sh"`/`"bash"`/`-c`/`exec.Command("sh"`.
  (baseline §3 «без shell-интерполяции, никогда sh -c»; spec AC2; plan AC2; threat-model R-2;
  research Q1)

- [ ] **SR-44. Относительный путь бинаря из cwd отвергается (`exec.ErrDot`); резолв по
  контролируемому PATH.** Имя бинаря, дающее `exec.ErrDot` (неявный/явный относительный путь из
  текущего каталога, Go 1.19+), отвергается → `isError:true`, команда НЕ запускается; чистое имя
  резолвится `LookPath` по серверному PATH (из env-whitelist, SR-49), НЕ по клиентскому вводу.
  Проверка: тест — `command="./x"` (или относительное имя) → `isError` (`ErrDot`/`ErrNotFound`), не
  запущено, сервер жив; PATH дочернего процесса = whitelist-значение, не из ввода. (baseline §3; spec
  AC2/AC8; plan AC2/AC8; threat-model R-4; research Q1/Q4)

- [ ] **SR-45. Несуществующий/недоступный бинарь → нейтральная ошибка, без паники/5xx, сервер жив.**
  Запуск отсутствующего бинаря (`exec.ErrNotFound`) → `isError:true` с НЕЙТРАЛЬНЫМ сообщением (без
  раскрытия путей/секретов/внутренних деталей), без паники и без 5xx; следующий валидный вызов
  отрабатывает штатно. Проверка: тест — заведомо отсутствующий бинарь → `isError`, сервер жив
  (последующий валидный вызов успешен). (baseline §3/§4; spec AC8/AC17; plan AC8; threat-model R-4/R-12)

## Таймаут, отмена и завершение дерева процессов (baseline §3)

- [ ] **SR-46. Каждая команда ограничена таймаутом через `context`; запрос сверх жёсткого максимума
  отклоняется ДО запуска; по истечении — kill всего дерева.** handler ставит `context.WithTimeout`:
  эффективный таймаут = `timeout_ms` или `default_timeout_ms` (дефолт 30000); `timeout_ms` >
  `max_timeout_ms` (дефолт 300000 = 5 мин) → `isError:true`, команда НЕ запускается. По истечении
  таймаута команда прерывается, процесс И его потомки убиваются (см. SR-47), результат содержит
  `timed_out:true` + частичный вывод. Проверка: тест №1 — команда дольше таймаута → прервана,
  `timed_out:true`, дочерних процессов группы не осталось; тест №2 — `timeout_ms` > max → `isError`,
  не запущено. (baseline §3 «таймаут на каждую команду»; spec AC5; ADR-003; threat-model R-6/R-8)

- [ ] **SR-47. process-group kill: при таймауте/отмене завершается всё дерево, осиротевших процессов
  не остаётся.** Перед стартом — `cmd.SysProcAttr={Setpgid:true}` (новая группа процессов);
  `cmd.Cancel` шлёт `syscall.Kill(-pgid, SIGKILL)` (kill всей группы); `cmd.WaitDelay` ненулевой
  (страховка от зависших пайпов/потомков). При отмене контекста запроса (обрыв соединения клиента)
  — то же завершение. Платформенный код под `//go:build unix` (Linux+darwin; Windows вне scope).
  Проверка: тест — отмена/таймаут долгой команды, порождающей потомков, → ни одного живого процесса
  группы после возврата (проверка отсутствием живого дочернего PID). (baseline §3; spec AC5/AC6;
  ADR-001; threat-model R-8/R-9; plan §Contracts `applyProcessGroup`/`killGroup`)

## Allowlist (baseline §3)

- [ ] **SR-48. Опциональный allowlist со СТРОГИМ точным сопоставлением; по умолчанию выключен.**
  `exec.allowlist` (дефолт `[]` = выключен = разрешена любая команда); включён → разрешены ТОЛЬКО
  команды, точно совпадающие с записью списка (НЕ regex, НЕ префикс, без нормализации регистра/
  пробелов), сопоставление по присланному `command` ДО `LookPath`; всё остальное → `isError:true`
  (причина «не разрешена»), команда НЕ запускается. Проверка: тест — allowlist включён: команда из
  списка выполняется, команда вне списка → `isError`(deny), не запущена; allowlist выключен: обе
  выполняются; вход с иным регистром/пробелами/префиксом записи списка → НЕ совпадает (deny).
  (baseline §3 «опциональный allowlist, строгое сопоставление, по умолчанию выключен»; spec AC7;
  plan AC7; threat-model ОР-5; research Q7)

## Окружение и рабочая директория (baseline §3)

- [ ] **SR-49. Окружение дочернего процесса — явный whitelist; опасные переменные НЕ наследуются.**
  `cmd.Env` устанавливается ЯВНО из `env_whitelist` (дефолт `["PATH","HOME","LANG","TERM"]`, значения
  берутся из окружения демона), БЕЗ слепого наследования (`cmd.Env != nil`); `PATH` присутствует
  (обязателен для `LookPath`); `LD_PRELOAD`/`LD_LIBRARY_PATH`/`DYLD_INSERT_LIBRARIES`/`IFS` в whitelist
  ОТСУТСТВУЮТ и в дочерний процесс НЕ попадают даже если заданы у демона; поле `env` во входе
  инструмента НЕ принимается (строгая схема, SR-51). Проверка: тест — дочерний процесс видит ТОЛЬКО
  whitelist-переменные; при заданных у демона `LD_PRELOAD`/`DYLD_INSERT_LIBRARIES`/`IFS` они
  отсутствуют в окружении ребёнка. (baseline §3 «окружение ограничено»; spec AC10; ADR-003;
  threat-model R-7; research Q4)

- [ ] **SR-50. Рабочая директория предсказуема и валидируется; небезопасный/невалидный `cwd`
  отклоняется до запуска.** При отсутствии `cwd` используется `default_cwd` (дефолт `/tmp` —
  предсказуемый, не `/`, не cwd демона); `cmd.Dir` задаётся ЯВНО (не пустой). При заданном `cwd` он
  валидируется до запуска (`os.Stat`+`IsDir`: существует И каталог) — невалидный (несуществующий /
  файл) → `isError:true`, команда НЕ запускается. Проверка: тест — команда видит ожидаемый cwd
  (дефолт при пустом вводе); невалидный `cwd` → `isError`, не запущено. (baseline §3 «рабочая
  директория ограничена»; spec AC10; threat-model R-15; research Q5)

## Лимиты входа и вывода против DoS/OOM (baseline §3)

- [ ] **SR-51. Строгая входная схема: лишние поля отвергаются; поле `env` отсутствует.** `ExecInput`
  — struct `{command, args, timeout_ms, cwd}`; схема выводится `AddTool` с
  `additionalProperties:false` (отвержение лишних полей); поля `env` НЕТ (v1). Запрос с лишним/
  неизвестным полем → ошибка валидации входа (`isError:true`), команда НЕ запускается. ЗАКРЕПИТЬ
  тестом (дефолт `additionalProperties:false` настраиваем в SDK — issue #892). Проверка: тест — вход
  с лишним полем → `isError`, не запущено; вход с `env` → отвергается. (baseline §3; spec AC3; plan
  §Contracts; threat-model R-7; research Q9)

- [ ] **SR-52. Прикладные лимиты входа (argv-DoS) проверяются ДО запуска → `isError`.** handler до
  `cmdexec.Run` проверяет: `len(args) > max_args` (дефолт 256) ИЛИ длина любого аргумента >
  `max_arg_len` (дефолт 131072 = 128 KiB) → `isError:true`(deny), команда НЕ запускается. Защищает
  память демона раньше ядерного `E2BIG`. Проверка: тест — `args` свыше `max_args` или аргумент свыше
  `max_arg_len` → `isError`, не запущено. (baseline §3; spec AC11; ADR-003; threat-model R-5; research
  Q10)

- [ ] **SR-53. Лимит вывода (OOM-защита): stdout/stderr обрезаются до конфигурируемого максимума с
  флагом усечения.** `cmd.Stdout`/`cmd.Stderr` = capped-writer с потолком `max_output_bytes` (дефолт
  1048576 = 1 MiB на КАЖДЫЙ поток); при превышении вывод обрезается до лимита, `stdout_truncated`/
  `stderr_truncated` = `true`, остаток ДРЕНИРУЕТСЯ без ошибки Write (чтобы не подвесить процесс);
  память на вывод ограничена сверху. Проверка: тест — команда, печатающая больше лимита, → обрезанный
  вывод + `*_truncated:true`, потребление памяти ограничено. (baseline §3 «вывод ограничен»; spec
  AC11; ADR-003; threat-model R-3; research Q3)

## Привилегии и политика root (baseline §3)

- [ ] **SR-54. Инструмент НЕ повышает привилегии; дочерний процесс наследует uid/gid демона как
  есть.** Раннер НЕ устанавливает `SysProcAttr.Credential`, не использует setuid/sudo, не пытается
  стать root; эффективный UID процесса демона при исполнении не меняется. Проверка: тест №1 —
  дочерний процесс выполняется под тем же UID, что демон; grep по `internal/cmdexec` — нет
  `Credential`/`setuid`/`sudo`. (baseline §3 «не повышать привилегии, демон не от root»; spec AC9;
  plan AC9; threat-model R-14)

- [ ] **SR-55. Детекция root обязательна: при euid==0 — WARN-аудит на КАЖДЫЙ вызов.** handler
  проверяет `os.Geteuid()==0`; при истине пишет отдельную аудит-запись уровня WARN об исполнении
  команд от root при КАЖДОМ вызове `execute_command` (детекция обязательна, AC9). Проверка: тест №2 —
  при euid==0 вызов `execute_command` порождает WARN-запись в аудите (помимо основной exec-записи).
  (baseline §3; spec AC9; ADR-003; threat-model R-14/П-2)

- [ ] **SR-56. Обязателен опциональный жёсткий отказ исполнять от root (`exec.deny_root`).**
  Конфиг-поле `exec.deny_root` (дефолт `false` — поведение WARN, см. П-2); при `true` И `os.Geteuid()
  ==0` команда НЕ запускается → `isError:true` (причина «исполнение от root запрещено политикой»).
  Это требование security ПОВЕРХ ADR-003 (architect рекомендовал только WARN): WARN остаётся
  дефолтом, но оператор ОБЯЗАН иметь рычаг hard-fail. Проверка: тест — `deny_root:true` + euid==0 →
  `isError`, команда не запущена; `deny_root:false` + euid==0 → команда выполняется + WARN (SR-55).
  (baseline §3 «демон НЕ от root»; spec AC9; threat-model П-2/ОР-1; ADR-003 §Альтернативы
  `exec.deny_root`)

## Аудит каждого вызова без секретов (baseline §4)

- [ ] **SR-57. Каждый вызов `execute_command` пишет РОВНО одну exec-аудит-запись во всех ветках;
  generic `withAudit` к нему НЕ применяется.** Per ADR-004: `execute_command` НЕ оборачивается generic
  `withAudit`; `execHandler` сам пишет одну запись через `server.AuditFn`. Ветки: success/таймаут →
  `Result:"success"` (+ `TimedOut`); deny (allowlist / превышение входных лимитов / `deny_root`) →
  `Result:"deny"`; fail (несуществующий бинарь / невалидный cwd) → `Result:"fail"`. Двойной записи
  НЕТ. Проверка: тест — на один вызов в аудите РОВНО одна exec-запись (плюс отдельный root-WARN при
  euid==0); deny-вызов даёт запись с `Result=deny`. (baseline §4 «аудит каждого действия»; spec AC13;
  ADR-004; threat-model R-10)

- [ ] **SR-58. Поля exec-аудит-записи соответствуют AC13.** Запись success содержит: timestamp(UTC),
  fingerprint ключа (НЕ ключ, из `server.FingerprintFromContext`), имя инструмента
  (`tool=execute_command`), выполненную команду + аргументы, exit code, длительность (duration),
  удалённый адрес (`server.RemoteAddrFromContext`), результат. Запись deny/fail содержит: fingerprint,
  команду+аргументы, причину, удалённый адрес, результат(deny/fail). Проверка: тест — success-запись
  содержит timestamp+fingerprint+команда+args+exit_code+duration+remote+result; deny-запись содержит
  fingerprint+команда+args+remote+result(deny). (baseline §4; spec AC13; plan §Contracts/ADR-004;
  threat-model R-10)

- [ ] **SR-59. `AuditRecord`/`writeAudit` расширены exec-полями, логируемыми ТОЛЬКО для
  `execute_command`; формат не-exec записей НЕ ломается.** В `internal/server/audit.go` к
  `AuditRecord` добавлены ОПЦИОНАЛЬНЫЕ поля (команда, args, exit_code, duration, timed_out); `writeAudit`
  выводит `command=`/`args=`/`exit_code=`/`duration=`/`timed_out=` ТОЛЬКО при `Tool=="execute_command"`
  (по аналогии с `tool=` при `Tool!=""`); не-exec записи (AUTH/FAIL/DENY/RATE/MCP-ping) сохраняют
  прежний формат — наследуемые тесты `tls-transport`/`mcp-server` не ломаются. Проверка: тест —
  exec-запись содержит `command=`/`exit_code=`/`duration=`; не-exec AUTH-запись их НЕ содержит;
  существующие подстроки (`AUTH`, `MCP`, `fp=`) на месте. (baseline §4; spec AC14; ADR-002;
  threat-model R-11)

- [ ] **SR-60. exec-аудит машиночитаем (строгий logfmt); единый канал/схема с остальным аудитом.**
  Аудит-логгер использует `LogfmtFormatter` (строгий, парсимый key=value), НЕ human-readable
  TextFormatter; exec-запись парсится как структурная logfmt-запись существующего формата через тот
  же канал (`charmbracelet/log`, `writeAudit`), что и записи транспорта/MCP. Это ПРИНЯТОЕ отклонение
  от буквы baseline §4 «JSON» (см. threat-model П-1) с условием строгой парсимости. Проверка: тест —
  exec-запись успешно парсится как logfmt (ключи/значения извлекаются); формат не-exec записей не
  ломается. (baseline §4 «структурно, машиночитаемо»; spec AC14; ADR-002; threat-model П-1/R-11)

- [ ] **SR-61. Ротация аудит-лога обеспечивается системно (journald/logrotate); требование baseline
  §4 не снято.** В коде ротация НЕ реализуется (нет новой зависимости `lumberjack`); вывод аудита —
  в stderr; ротацию обеспечивает journald (systemd) или logrotate (файловый вывод). devops/distribution
  обязаны поставить ротацию для файлового вывода; tech-writer документирует. Проверка: инспекция —
  в коде нет реализации ротации/`lumberjack`; в дистрибуции/доке зафиксирована системная ротация
  аудит-лога. (baseline §4 «с ротацией»; spec AC14; ADR-002; threat-model П-1/ОР-2; наследует
  tls-transport ОР-4)

- [ ] **SR-62. Тело API-ключа / приватный TLS-ключ ОТСУТСТВУЮТ в результате, ошибках и exec-аудите.**
  Ни в `ExecOutput`/`Content` (`stdout`/`stderr`/итог), ни в тексте `isError`/JSON-RPC-ошибки, ни в
  exec-аудит-записи НЕТ полного ключа, его хэша, соли, raw `Authorization`, приватного TLS-ключа;
  вместо ключа — fingerprint; сообщения об ошибках нейтральны. exec-слой к телу ключа доступа НЕ
  имеет. Проверка: тест — предъявленный полный ключ как ПОДСТРОКА ОТСУТСТВУЕТ в захваченном
  exec-аудите И в теле MCP-ответа; приватный TLS-ключ не встречается; grep по `internal/cmdexec`/
  `internal/mcp/exec_tool.go` — нет логирования/возврата raw `Authorization`/тела ключа. (baseline §4
  «никаких секретов в логах», §1; spec AC15; threat-model R-12; наследует SR-21/SR-34)

- [ ] **SR-63. Аргументы команды логируются ДОСЛОВНО; предупреждение оператору о секретах в argv
  обязательно в доке.** Per принятое отклонение П-3: args в exec-аудите соответствуют присланным (без
  маскирования — надёжное определение секрета в произвольном argv невозможно, полнота аудита
  критична). Компенсирующий контроль ОБЯЗАТЕЛЕН: tech-writer документирует явное предупреждение
  «аргументы команд логируются дословно; НЕ передавайте секреты в argv — используйте механизмы целевой
  утилиты (файл/переменная окружения команды)»; аудит-лог — ограниченного доступа. Это НЕ противоречит
  SR-62 (там — секреты raxd: ключ/TLS; здесь — секрет КЛИЕНТА в argv). Проверка: тест — args в аудите
  равны присланным; инспекция доки — предупреждение о логировании argv присутствует. (baseline §4
  «команда+аргументы» ↔ «никаких секретов»; spec AC13/AC15; threat-model П-3/R-13/ОР-4)

## Устойчивость протокола (baseline §4)

- [ ] **SR-64. Некорректный ввод инструмента → `isError`/корректная JSON-RPC-ошибка, без паники/501;
  сервер жив.** Ошибка валидации ВВОДА (лишнее поле, неверный тип, превышение лимитов) → `isError:true`
  (Tool Execution Error, НЕ protocol error); некорректный JSON-RPC / неверные параметры протокола →
  корректная JSON-RPC-ошибка; ненулевой exit code КОМАНДЫ — НЕ ошибка раннера, а `exit_code` в
  результате; раннер НИКОГДА не паникует; наследуемый `recoverMiddleware` — страховка. После любого
  некорректного запроса валидный вызов отрабатывает штатно. Проверка: тест — невалидные параметры →
  `isError`/JSON-RPC error (не паника/501); ненулевой exit → `isError:false` + `exit_code != 0`;
  после некорректного запроса валидный вызов успешен. (baseline §4 «устойчивость»; spec AC17; plan
  §Chosen Approach «граница ошибок»; threat-model R-10; наследует SR-30)

- [ ] **SR-65. Формат выхода соответствует AC4 (структурированный результат).** Успешный вызов
  возвращает `ExecOutput{stdout, stderr, exit_code, duration_ms, timed_out, stdout_truncated,
  stderr_truncated}` (structuredContent) + текст-блок Content; форма соответствует объявленной
  выходной схеме. Проверка: тест — запуск команды с известным выводом/кодом → ожидаемые значения семи
  полей; форма соответствует output-схеме. (baseline §4; spec AC4; plan §Contracts `ExecOutput`;
  threat-model R-3 косвенно)

## Конфигурация безопасных дефолтов (baseline §3/§4)

- [ ] **SR-66. Все параметры безопасности — конфиг-поля секции `exec` с безопасными дефолтами; без
  env-оверрайдов.** В `internal/config/config.go` добавлена секция `exec` (viper, `v.SetDefault`):
  `allowlist` (`[]` = выкл), `default_timeout_ms` (30000), `max_timeout_ms` (300000), `default_cwd`
  (`/tmp`), `env_whitelist` (`["PATH","HOME","LANG","TERM"]`), `max_args` (256), `max_arg_len`
  (131072), `max_output_bytes` (1048576), `deny_root` (`false`). Дефолты — безопасные (allowlist выкл
  по контракту AC7, но остальная защита §3 действует независимо; env-whitelist без опасных
  переменных). Числовые значения и состав env-whitelist ПОДТВЕРЖДЕНЫ security как достаточные против
  DoS/эскалации (threat-model R-3/R-5/R-6/R-7; ADR-003 §Зависимость от security). Проверка: тест —
  дефолты применяются при отсутствии config.yaml; значения переопределяемы; невалидные числовые
  значения отвергаются на загрузке. (baseline §3/§4 «безопасные дефолты»; spec AC5/AC7/AC10/AC11;
  plan §Config; ADR-003; threat-model R-3/R-5/R-6/R-7/R-14)

## Среда сборки/тестов/запуска (baseline §6)

- [ ] **SR-67. Все проверки command-exec прогоняются в Docker, офлайн из `vendor/`; запуск
  демона/команд — только в контейнере.** Тесты этой задачи (без shell; ErrDot; таймаут+kill дерева;
  отмена; allowlist; env-whitelist; cwd; лимиты вход/вывод; root WARN/deny_root; uid наследуется;
  аудит поля/формат/без секретов/args дословно; isError на невалидном вводе) зелёные в Docker; сборка/
  тесты `-mod=vendor` без `go mod download`; `-race`-прогон; на хосте `raxd` НЕ запускается (исполняет
  произвольные команды — место в изолированном контейнере). НОВЫХ внешних зависимостей command-exec
  НЕ вводит (всё на stdlib + уже вендоренные). Проверка: CI/локальный прогон в контейнере проходит из
  `vendor/`; grep `go.mod` — нет новых внешних зависимостей от command-exec. (baseline §6; spec AC18;
  plan §Trade-offs «без новых зависимостей»; threat-model R-9/ОР-3)

## Наследуемые требования (выполнены в `tls-transport`/`mcp-server`, command-exec НЕ переопределяет)

> Полный текст и проверки — `specs/tls-transport/security-requirements.md` и
> `specs/mcp-server/security-requirements.md`. `execute_command` сидит за этим периметром; дублировать
> его как новые SR ЗАПРЕЩЕНО (CLAUDE.md: «не переписывать транспорт/MCP»). Перечислены ссылки,
> обязательные для понимания контекста.

- **SR-1/SR-2 (TLS 1.3)** — exec идёт ПОВЕРХ этого TLS.
- **SR-7 (bind `127.0.0.1` по умолчанию)** — тот же сокет обслуживает `execute_command`.
- **SR-8…SR-13 (auth ДО маршрутизации, Bearer→`keystore.Verify`, constant-time, мгновенный отзыв,
  `ErrCorrupt`→403)** — база для SR-41.
- **SR-14/SR-16 (Host/Origin валидация; Origin present&invalid→403)** — DNS-rebinding защита для
  `/mcp`, наследуется (threat-model R-M-Origin).
- **SR-17/SR-18 (rate-limit per-key/per-IP→429; TTL-GC)** — база для SR-42.
- **SR-19/SR-20/SR-21 (аудит каждого соединения/отказа; никаких секретов в логах)** — база для
  SR-57…SR-63; exec-аудит расширяет тот же канал.
- **SR-24/SR-25 (graceful shutdown; таймауты + лимит тела/заголовков)** — внешняя граница argv-DoS
  (SR-52) и Slowloris; не переопределяются.
- **SR-27/SR-28/SR-29 (MCP за единой цепочкой; нет второго auth-канала; тот же порт/TLS)** — база для
  SR-40/SR-41.
- **SR-30 (некорректный JSON-RPC → ошибка без паники)** — база для SR-64.
- **SR-35/SR-36 (`withAudit` для ping/server_info; `AuditRecord.Tool`)** — НЕ применяется к
  `execute_command` (ADR-004/SR-57), но `Tool`-поле и канал переиспользуются.
- **SR-37/ОР-М2 (точка расширения `mcp.AddTool`; обязательство реализовать §3)** — ИСПОЛНЯЕТСЯ этой
  задачей (SR-40…SR-67).
- **SR-38/SR-39 (вендоринг офлайн, Docker)** — база для SR-67; command-exec новых зависимостей не
  добавляет.

## Вне scope этой задачи (фиксация, не требование к command-exec)

- **Приём `env` от клиента** — отложено (threat-model ОР-6); окружение только серверным whitelist
  (SR-49).
- **Sandboxing/cgroups/rlimits/seccomp/namespaces** — не в v1 (threat-model ОР-3); изоляция —
  контейнер (baseline §6) + не-root (service-install).
- **Интерактив/PTY/stdin-стриминг, стриминг вывода** — отдельные задачи (spec Out of Scope).
- **Не-root раскладка сервиса, выбор системного пользователя, capabilities** — задачи
  `service-install`/`distribution`; здесь только «не повышать привилегии» + детекция root (SR-54…SR-56).
- **`upload_file` / передача файлов** — задача `file-upload`.
- **Управление ключами, TLS, rate-limit, протокол MCP, `ping`/`server_info`** — готовы в смежных
  задачах; здесь только ПОТРЕБЛЯЮТСЯ.
- **Allowlist-матч по абсолютному резолву (устойчивость к алиасам)** — возможное усиление (research
  Q7 вариант B); v1 — строгое точное равенство по присланному `command` (SR-48, threat-model ОР-5).
