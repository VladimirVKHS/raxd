# Security Requirements: file-upload — MCP-инструмент `upload_file`

> Каждое требование ПРОВЕРЯЕМО (ответ «выполнено/нет» одним вопросом: тест / grep / инспекция) и
> ссылается на пункт `SECURITY-BASELINE.ru.md`, на AC `spec.md`, на контракт `plan.md`/ADR и на риск
> из `threat-model.md`. Эти требования ОБЯЗАНЫ выполнить `developer` (`internal/fileupload/*`,
> `internal/mcp/upload_tool.go`, расширения `internal/mcp/server.go`, `internal/server/audit.go`,
> `internal/config/config.go`, `internal/cli/serve.go`), `mcp-engineer` (контракт инструмента/схемы),
> `devops` (ротация в дистрибуции, CI в Docker), `tech-writer` (предупреждения в доке) и `qa`
> (тесты). Соответствие проверяют `reviewer` и `security-guardian`. Способ проверки везде: тесты
> гоняются в Docker из `vendor/` (`-mod=vendor`, baseline §6, AC20); запуск демона — только в
> контейнере.
>
> **Нумерация.** SR-1…SR-26 заняты `tls-transport`, SR-27…SR-39 — `mcp-server`, SR-40…SR-67 —
> `command-exec`. Они НАСЛЕДУЮТСЯ (см. раздел «Наследуемые требования») и здесь НЕ дублируются.
> Требования file-upload нумеруются СКВОЗНО с **SR-68** по **SR-82** (15 требований, без пропусков).
>
> **Терминология.** «Полный ключ» = `rax_live_<base64url>` целиком (заголовок `Authorization:
> Bearer`). «Fingerprint» = `keystore.Fingerprint(...)` (необратим) — в аудите/upload-слое ВМЕСТО
> ключа. upload-слой (`internal/fileupload`/`internal/mcp/upload_tool.go`) к телу ключа доступа НЕ
> имеет (только `server.FingerprintFromContext`/`RemoteAddrFromContext` из ctx). «Писатель» =
> `internal/fileupload` (чистая запись через `os.Root`); «handler» = `uploadHandler`
> (`internal/mcp/upload_tool.go`). «upload root» = разрешённый корень записи (дефолт
> `<StateDir>/uploads`, 0700). «Содержимое» = `content`/декодированные байты загружаемого файла.

## Поверхность и аутентификация записи (baseline §1/§3)

- [ ] **SR-68. Запись файла доступна ТОЛЬКО как MCP-инструмент `upload_file` за наследуемой цепочкой;
  отдельной поверхности записи НЕТ; аутентификация и rate-limit наследуются и выполняются ДО записи.**
  Инструмент регистрируется `sdkmcp.AddTool(s, uploadTool(), uploadHandler(...))` в
  `internal/mcp/server.go` (`NewHandler`), рядом с `ping`/`server_info`/`execute_command`, ВНУТРИ
  цепочки `bodyLimit→recover→Host/Origin→auth→rate-limit→authSuccessAudit→mux`. Отдельного не-`/mcp`
  сетевого эндпоинта и CLI-подкоманды `raxd upload` НЕТ; транспорт/маршрут `/mcp` НЕ меняется. Bearer-
  аутентификация (`keystore.Verify`) и rate-limit per-key/per-IP отрабатывают ДО handler: запрос без
  `Authorization: Bearer` / с неизвестным/отозванным ключом → 401, повреждение keystore → 403,
  превышение лимита → 429 — до писателя НЕ доходит, файл НЕ создаётся. upload-слой `keystore.Verify`
  НЕ вызывает и новых каналов auth НЕ вводит. Проверка: тест — после `serve` `upload_file` присутствует
  в `tools/list` рядом с `ping`/`server_info`/`execute_command`; неаутентифицированный `tools/call
  upload_file` → 401, файл не создан; превышение лимита → 429, файл не создан; grep — нет второго
  HTTP-маршрута записи и нет CLI-подкоманды `upload`; grep `internal/fileupload`/`internal/mcp` — нет
  вызова `keystore.Verify`. (baseline §1/§3; spec AC1/AC17/AC18; plan §Modules; threat-model R-U1;
  наследует SR-27/SR-28/SR-17/SR-18)

## Path traversal — главный контроль (baseline §3)

- [ ] **SR-69. Все ФС-операции записи идут ТОЛЬКО через `os.Root` по относительным путям; traversal
  (`..`/абсолютный/симлинк наружу/TOCTOU) отвергается ДО записи; права — chmod ПО fd, не по имени.**
  Писатель открывает upload root через `os.OpenRoot(uploadRoot)` и выполняет ВСЕ операции записи
  методами `os.Root` (`Root.MkdirAll`, `Root.OpenFile`, `Root.Rename`, `Root.Stat`, `Root.Remove`) по
  относительным путям из запроса; raw `os.OpenFile`/`os.MkdirAll`/`os.Rename` по объединённому с
  корнем пути в обход `Root` НЕ используется. Отвергаются (до записи, `isError:true`, deny): `path` с
  `..`-escape, абсолютный `path`, путь, итоговый резолв которого выходит за корень (в т.ч. через
  симлинк наружу), TOCTOU-подмена симлинка (снята `openat`-семантикой `os.Root`). Дополнительно —
  ранний лексический отказ `filepath.IsLocal` для явно абсолютных/`..`-путей. Права создаваемого файла
  выставляются `(*os.File).Chmod` ПО ДЕСКРИПТОРУ (fd), а НЕ через `Root.Chmod`-по-имени (обход race,
  R-U3). Проверка: тест — каждый вектор (`"../etc/passwd"`, `"/etc/passwd"`, `"a/../../b"`, путь
  через символьную ссылку наружу) → `isError:true` (deny), файл вне корня НЕ создан, существующий файл
  вне корня НЕ изменён; grep `internal/fileupload` — запись только через методы `os.Root`, права через
  `(*os.File).Chmod` (по fd), нет `Root.Chmod(name,…)`. (baseline §3 «валидация входа»; spec AC4;
  ADR-001/ADR-002; threat-model R-U2/R-U3; research Q1/Q2/Q3)

- [ ] **SR-70. Ограничение `os.Root` (mount points не блокируются) принято как остаточный риск со
  смягчением; bind-mount внутрь upload root запрещён документацией.** `os.Root` НЕ блокирует traversal
  через mount points внутри корня (документированное ограничение). Смягчение: upload root располагается
  в каталоге данных raxd (`<StateDir>/uploads`, права `0700`), не предназначенном для bind-mount;
  развёртывание — в контейнере (baseline §6); tech-writer ОБЯЗАН задокументировать «НЕ размещайте
  bind-mount/внешнюю ФС внутрь upload root — запись может выйти наружу». Проверка: инспекция доки —
  присутствует предупреждение о mount points в upload root; threat-model фиксирует ОР-U2 + эскалацию.
  (baseline §3/§6; spec AC4; ADR-001 §Последствия; threat-model R-U3/ОР-U2)

## Корень записи и подкаталоги (baseline §3)

- [ ] **SR-71. Безопасный дефолт upload root; создание подкаталогов ТОЛЬКО внутри корня.** При пустом/
  некорректном `upload.root` serve.go резолвит безопасный дефолт `<StateDir>/uploads` (НЕ `/`, НЕ
  корень ФС, НЕ `/root`, НЕ домашний каталог root), создаёт его с правами `0700` (как `EnsureDirs`).
  Недостающие промежуточные подкаталоги внутри корня создаются автоматически (`Root.MkdirAll(dir(rel),
  0700)`); создание каталогов ВНЕ корня невозможно (следствие SR-69). Проверка: тест — при
  отсутствии/невалидной настройке корня файл пишется в безопасный дефолтный каталог (не в `/`, не в
  `/root`), а не отказывает молча с записью в небезопасное место; валидная загрузка по пути с
  несуществующим промежуточным подкаталогом внутри корня создаёт подкаталог и файл по ожидаемому пути
  внутри корня. (baseline §3 «рабочая директория предсказуема, безопасные дефолты»; spec AC5a/AC5b;
  plan §Config Q1; threat-model R-U2)

## Перезапись, каталог-цель, права файла (baseline §3)

- [ ] **SR-72. Перезапись по умолчанию ЗАПРЕЩЕНА; цель-каталог отвергается; перезапись (если
  разрешена) атомарна.** При существующем целевом файле и `overwrite:false` (дефолт) → `isError:true`
  (deny), существующий файл НЕ изменён (`Root.Stat`). При `overwrite:true` файл заменяется АТОМАРНО
  (через temp→`Root.Rename` поверх, SR-74). Целевой путь, указывающий на существующий КАТАЛОГ →
  `isError:true` (deny, `Root.Stat`+`IsDir`), каталог не тронут. Проверка: тест №1 — повторная
  загрузка по тому же пути без `overwrite` → `isError`, содержимое прежнее; тест №2 — `overwrite:true`
  → файл заменён, `overwritten:true`; тест №3 — путь на каталог → `isError`, каталог не изменён.
  (baseline §3 «валидация входа, безопасные дефолты»; spec AC8/AC14; ADR-002; threat-model R-U4;
  research Q8)

- [ ] **SR-73. Права создаваемого файла предсказуемы и umask-независимы; setuid/setgid/sticky/
  world-writable ЗАПРЕЩЕНЫ; владелец — UID/GID демона, без эскалации.** Дефолт режима — `0600`
  (`upload.default_mode`); поле `mode` (восьмеричная строка) валидируется `fileupload.ParseMode`:
  допустимы ТОЛЬКО биты прав в маске **`0777`**; setuid (`04000`), setgid (`02000`), sticky (`01000`),
  world-writable (`0002`) и непарсимые значения → `ErrBadMode` (`isError:true`, deny). Режим
  применяется `(*os.File).Chmod` по fd ДО записи (umask-независимо). Файл создаётся под UID/GID
  процесса демона как есть; писатель НЕ делает chown/setuid/sudo, НЕ повышает привилегии; создание
  символических/жёстких ссылок и спецфайлов НЕ выполняется (только обычный файл). Проверка: тест —
  `mode="04755"`/`"02755"`/`"01777"`/world-writable/непарсимый → `isError` (deny), файл не создан;
  валидный `mode` в `0777` без `0002` применяется точно (фактический режим = заданный);
  при отсутствии `mode` — дефолт `0600`; владелец созданного файла = пользователь демона; grep
  `internal/fileupload` — нет chown/setuid/Credential/`Root.Symlink`/`Root.Link`. (baseline §3 «не
  повышать привилегии»; spec AC9/AC14, Out of Scope; ADR-002/ADR-003; threat-model R-U7/решение №2)

## Атомарность и отсутствие частичного/temp-файла (baseline §3)

- [ ] **SR-74. Запись атомарна (temp→Sync→Rename→fsync-dir); при ЛЮБОЙ ошибке/deny не остаётся ни
  частичного целевого, ни temp-файла.** Писатель создаёт temp-файл с уникальным `crypto/rand`-именем
  (`O_CREATE|O_EXCL`) в ТОМ ЖЕ подкаталоге, что цель → chmod по fd (SR-73) → запись декодированных
  байт → `(*os.File).Sync()` → `Root.Rename(tmp, target)` (атомарная фиксация/замена на одной ФС) →
  fsync каталога (best-effort). Целевой файл становится виден читателям только целиком, в финальном
  состоянии; при перезаписи (SR-72) старый файл сохраняется до фиксации. На ЛЮБОЙ ошибке/deny ДО
  фиксации temp удаляется (`Root.Remove`, defer). Проверка: тест — при искусственном прерывании до
  фиксации целевой файл отсутствует (новый путь) или прежний (перезапись); посторонних temp-файлов в
  каталоге назначения нет; превышение лимита/невалидный вход → temp на диске не остаётся. (baseline §3;
  spec AC7/AC10; ADR-002; threat-model R-U6; research Q4)

## base64-вход, лимит размера и согласование с телом транспорта (baseline §3, DoS)

- [ ] **SR-75. Содержимое декодируется как base64; невалидный base64 → deny без записи; лимит размера
  ДЕКОДИРОВАННОГО содержимого с ранним фильтром.** handler декодирует `content` как base64
  (`base64.StdEncoding`): `CorruptInputError` → `isError:true` (deny), файл НЕ создаётся. До
  декодирования — ранний фильтр `base64.DecodedLen(len(content)) > max_file_bytes` → deny без
  декодирования (защита памяти); после декодирования — точная проверка `len(decoded) ≤ max_file_bytes`,
  превышение → `isError:true` (deny), файл и temp НЕ остаются. Записанные на диск байты ТОЧНО равны
  исходным (включая бинарные). Проверка: тест — валидный base64 даёт на диске байты, равные исходным;
  невалидный base64 → `isError`, нет записи; декодированный размер > `max_file_bytes` → `isError`,
  файла и temp нет. (baseline §3 «лимиты входа»; spec AC6/AC7; plan §Contracts; threat-model R-U5/R-U8;
  research Q6)

- [ ] **SR-76. `max_file_bytes` согласован с наследуемым `max_body_bytes` и проверяется на старте;
  файлы у границы НЕ упираются в 413; большие файлы — Out of Scope.** Дефолт `max_file_bytes` =
  **716800** (700 KiB) — НИЖЕ выводимого из `max_body_bytes` (1 MiB) потолка одного файла
  ≈ `(max_body_bytes − reserve) × 3/4` ≈ 785 KiB (base64 ×4/3 + JSON-RPC overhead). На старте
  проверяется `0 < max_file_bytes ≤ floor((max_body_bytes − reserve) × 3/4)` — иначе ошибка загрузки
  (reserve покрывает base64-паддинг + overhead). Тело запроса проходит наследуемый `bodyLimitMiddleware`
  (`max_body_bytes`) ДО инструмента: тело > `max_body_bytes` → 413, файл НЕ создаётся. Загрузка файлов
  больше потолка — Out of Scope (chunked, отдельная задача); подъём `max_body_bytes` в рамках
  file-upload НЕ выполняется. Проверка: тест №1 — файл, чьё base64-тело ≤ `max_body_bytes` и
  декодированный размер ≤ `max_file_bytes`, загружается; тест №2 — тело > `max_body_bytes` → 413 ДО
  инструмента, файл не создан; тест №3 — конфиг с `max_file_bytes` выше потолка → ошибка загрузки на
  старте. (baseline §3/§4 «лимиты, безопасные дефолты»; spec AC7/AC15/AC16; plan §Config/§Trade-offs;
  threat-model R-U5/решение №4/ОР-U3/ОР-U6)

## Привилегии и политика root (baseline §3)

- [ ] **SR-77. Детекция root обязательна: при euid==0 — WARN-аудит на КАЖДЫЙ вызов; обязателен
  опциональный жёсткий отказ `upload.deny_root`.** handler проверяет `os.Geteuid()==0`; при истине
  пишет отдельную аудит-запись уровня WARN (`Result:"warn"`, reason «running-as-root») об операции
  записи файла от root при КАЖДОМ вызове `upload_file` (детекция обязательна, AC11). Конфиг-поле
  `upload.deny_root` (дефолт `false` — поведение WARN, П-U2); при `true` И `os.Geteuid()==0` файл НЕ
  создаётся → `isError:true` (причина «запись от root запрещена политикой»). Это ОТДЕЛЬНЫЙ флаг (НЕ
  переиспользование `exec.deny_root`). Проверка: тест №1 — при euid==0 вызов `upload_file` порождает
  WARN-запись (помимо основной upload-записи); тест №2 — `deny_root:true` + euid==0 → `isError`, файл
  не создан; `deny_root:false` + euid==0 → файл создан + WARN. (baseline §3 «демон НЕ от root»; spec
  AC9/AC11; ADR-003; threat-model R-U9/П-U2/решение №3; зеркало SR-55/SR-56)

## Аудит каждой загрузки без содержимого (baseline §4)

- [ ] **SR-78. Каждый вызов `upload_file` пишет РОВНО одну upload-аудит-запись во всех ветках;
  generic `withAudit` к нему НЕ применяется; поля по AC12, без содержимого.** Per ADR-004-стиль:
  `upload_file` НЕ оборачивается generic `withAudit`; `uploadHandler` сам пишет одну запись через
  `server.AuditFn`. Ветки: success → `Result:"success"`; deny (traversal/exists/isdir/too-large/
  bad-base64/bad-mode/deny_root) → `Result:"deny"` + reason; fail (I/O/диск полон) → `Result:"fail"`
  + reason. Двойной записи НЕТ; плюс отдельный root-WARN при euid==0 (SR-77). Поля success-записи:
  timestamp(UTC), fingerprint (НЕ ключ, из `server.FingerprintFromContext`), `tool=upload_file`,
  относительный путь назначения (внутри корня), размер записанных байт, удалённый адрес
  (`server.RemoteAddrFromContext`), результат. Поля deny/fail: fingerprint, путь (если известен),
  причина, remote, результат. СОДЕРЖИМОЕ файла (`content`/декодированные байты) в аудит НЕ пишется
  НИКОГДА. Проверка: тест — на один вызов в аудите РОВНО одна upload-запись с корректным результатом;
  success-запись содержит timestamp+fingerprint+tool+путь+размер+remote+результат; deny/fail-запись —
  fingerprint+путь+причину+remote+результат; deny/fail-вызов даёт запись, а не теряется. (baseline §4
  «аудит каждого действия»; spec AC12/AC19; plan §Contracts; ADR-004-стиль; threat-model R-U10)

- [ ] **SR-79. `AuditRecord`/`writeAudit` расширены полями path/size, логируемыми ТОЛЬКО для
  `upload_file`; формат прочих записей НЕ ломается; путь логируется безопасно (logfmt-инъекция
  закрыта).** В `internal/server/audit.go` к `AuditRecord` добавлены ОПЦИОНАЛЬНЫЕ поля `Path string`,
  `Size int64`; `writeAudit` выводит `path=`/`size=` ТОЛЬКО в ветке `isUpload := rec.Tool ==
  "upload_file"` (зеркало существующей `isExec`-ветки); не-upload записи (AUTH/FAIL/DENY/RATE/MCP-ping/
  exec) сохраняют прежний формат — наследуемые тесты tls-transport/mcp-server/command-exec НЕ ломаются.
  Путь логируется как ЗНАЧЕНИЕ key/value через `charmbracelet/log`→`go-logfmt/logfmt`, который
  АВТОМАТИЧЕСКИ квотирует/экранирует значения со спецсимволами (пробел, `=`, `"`, `\n`, `\r`,
  управляющие < 0x20, DEL): инъекция поддельной пары `result=`/новой строки лога через путь
  НЕВОЗМОЖНА. Проверка: тест — upload-запись содержит `path=`/`size=`; не-upload AUTH/exec-запись их
  НЕ содержит; существующие подстроки (`AUTH`, `MCP`, `fp=`, exec-поля) на месте; путь со спецсимволами
  (пробел/`=`/`"`/`\n`) в аудите квотирован/экранирован, запись остаётся одной парсимой logfmt-строкой,
  поддельной пары/новой строки нет; наследуемые тесты зелёные. (baseline §4 «структурно,
  машиночитаемо»; spec AC12; threat-model R-U11/R-U13/решение №5; зеркало SR-59/SR-60/П-1)

- [ ] **SR-80. Тело API-ключа / приватный TLS-ключ / содержимое файла / абсолютный путь хоста
  ОТСУТСТВУЮТ в результате, ошибках и upload-аудите.** Ни в `UploadOutput`/Content, ни в тексте
  `isError`/JSON-RPC-ошибки, ни в upload-аудит-записи НЕТ: полного ключа, его хэша, соли, raw
  `Authorization`, приватного TLS-ключа, ДЕКОДИРОВАННОГО содержимого файла, абсолютного пути хоста.
  Вместо ключа — fingerprint; в результате/аудите — только относительный путь (как принят сервером);
  `UploadOutput` = `path`/`size`/`overwritten`/`mode` (без содержимого, без абсолютного пути);
  сообщения об ошибках нейтральны (не раскрывают абсолютные пути ФС/раскладку). upload-слой к телу
  ключа доступа НЕ имеет; не читает keystore/TLS-файлы. Проверка: тест — предъявленный полный ключ как
  ПОДСТРОКА ОТСУТСТВУЕТ в захваченном upload-аудите И в теле MCP-ответа; декодированное содержимое как
  ПОДСТРОКА ОТСУТСТВУЕТ в логе; приватный TLS-ключ и абсолютный путь хоста не встречаются; grep
  `internal/fileupload`/`internal/mcp/upload_tool.go` — нет логирования/возврата `content`/decoded/
  raw `Authorization`/тела ключа/абсолютного пути. (baseline §4 «никаких секретов в логах», §1; spec
  AC3/AC12/AC13; threat-model R-U12/R-U13; наследует SR-21/SR-34/SR-62)

## Конфигурация безопасных дефолтов и среда (baseline §3/§4/§6)

- [ ] **SR-81. Параметры загрузки — конфиг-секция `upload` с безопасными дефолтами; без env-
  оверрайдов; невалидные значения отвергаются на старте.** В `internal/config/config.go` добавлена
  секция `upload` (viper, `v.SetDefault`): `root` (пусто → резолв `<StateDir>/uploads`, 0700),
  `max_file_bytes` (716800), `default_mode` (`"0600"`), `overwrite_default` (`false`), `deny_root`
  (`false`). Дефолты применяются при отсутствии config.yaml; значения переопределяемы; невалидные
  отвергаются на старте: `max_file_bytes` ≤ 0 ИЛИ > потолка из `max_body_bytes` (SR-76) → ошибка;
  `default_mode` непарсимый / с запрещёнными битами (SR-73) → ошибка; невалидный/небезопасный `root`
  (`/`, `/root`) → безопасный дефолт или ошибка. Проверка: тест — при отсутствии config.yaml
  применяются дефолты; переопределение работает; невалидный `max_file_bytes`/`default_mode`/`root`
  отвергается на старте. (baseline §3/§4 «безопасные дефолты»; spec AC15; plan §Config; threat-model
  R-U5/R-U7/R-U9)

- [ ] **SR-82. Все проверки file-upload прогоняются в Docker, офлайн из `vendor/`; запуск демона —
  только в контейнере; новых внешних зависимостей НЕТ.** Тесты этой задачи (traversal-векторы; mount-
  заметка в доке; дефолт корня + подкаталоги; overwrite/каталог-цель; права/запрет опасных битов;
  атомарность + отсутствие temp; base64 + лимит; согласование с `max_body_bytes`/413; root WARN/
  deny_root; аудит поля/формат/без содержимого/без секретов; logfmt-квотирование пути; isError на
  невалидном вводе) зелёные в Docker; сборка/тесты `-mod=vendor` без `go mod download`; на хосте
  `raxd` НЕ запускается. file-upload НОВЫХ внешних зависимостей НЕ вводит (всё на stdlib `os`+`os.Root`,
  `path/filepath`, `encoding/base64`, `crypto/rand`, `io/fs`, `strconv` + уже вендоренные
  `charmbracelet/log`/go-sdk). Проверка: CI/локальный прогон в контейнере проходит из `vendor/`; grep
  `go.mod` — нет новых внешних зависимостей от file-upload. (baseline §6; spec AC20; plan §Trade-offs;
  threat-model ОР-U4)

## Наследуемые требования (выполнены в `tls-transport`/`mcp-server`/`command-exec`, file-upload НЕ переопределяет)

> Полный текст и проверки — `specs/tls-transport/security-requirements.md`,
> `specs/mcp-server/security-requirements.md`, `specs/command-exec/security-requirements.md`.
> `upload_file` сидит за этим периметром; дублировать его как новые SR ЗАПРЕЩЕНО (CLAUDE.md: «не
> переписывать транспорт/MCP»). Перечислены ссылки, обязательные для понимания контекста.

- **SR-1/SR-2 (TLS 1.3)** — upload идёт ПОВЕРХ этого TLS.
- **SR-7 (bind `127.0.0.1` по умолчанию)** — тот же сокет обслуживает `upload_file`.
- **SR-8…SR-13 (auth ДО маршрутизации, Bearer→`keystore.Verify`, constant-time, мгновенный отзыв,
  `ErrCorrupt`→403)** — база для SR-68.
- **SR-14/SR-16 (Host/Origin валидация; Origin present&invalid→403)** — DNS-rebinding защита для
  `/mcp`, наследуется.
- **SR-17/SR-18 (rate-limit per-key/per-IP→429; TTL-GC)** — база для SR-68 (частота загрузок).
- **SR-19/SR-20/SR-21 (аудит каждого соединения/отказа; никаких секретов в логах)** — база для
  SR-78…SR-80; upload-аудит расширяет тот же канал.
- **SR-24/SR-25 (graceful shutdown; таймауты + лимит тела/заголовков)** — `MaxBodyBytes`-граница
  размера тела (база для SR-76) и Slowloris; не переопределяются.
- **SR-27/SR-28/SR-29 (MCP за единой цепочкой; нет второго auth-канала; тот же порт/TLS)** — база для
  SR-68.
- **SR-30 (некорректный JSON-RPC → ошибка без паники)** — база для устойчивости (R-U8).
- **SR-35/SR-36 (`withAudit` для ping/server_info; `AuditRecord.Tool`)** — НЕ применяется к
  `upload_file` (ADR-004-стиль/SR-78), но `Tool`-поле и канал переиспользуются.
- **SR-37/ОР-М2 (точка расширения `mcp.AddTool`; обязательство реализовать §3)** — ИСПОЛНЯЕТСЯ этой
  задачей (SR-68…SR-82) для записи файла.
- **SR-38/SR-39 (вендоринг офлайн, Docker)** — база для SR-82; file-upload новых зависимостей не
  добавляет.
- **SR-59/SR-60 (`AuditRecord`/`writeAudit` exec-поля только при `isExec`; LogfmtFormatter)** —
  ОБРАЗЕЦ для SR-79 (ветка `isUpload` зеркально `isExec`); LogfmtFormatter уже установлен
  command-exec в `serve.go`, upload пишет в тот же канал.
- **SR-61/ОР-2 (системная ротация аудит-лога)** — upload пишет в тот же канал; новой ротации не
  требует (см. ОР-U4); требование ротации НЕ снято.
- **SR-62 (тело ключа/TLS-ключ отсутствуют в результате/ошибке/аудите)** — расширено в SR-80
  (добавлены содержимое файла и абсолютный путь).

## Вне scope этой задачи (фиксация, не требование к file-upload)

- **Скачивание/чтение файлов с хоста (`download_file`)** — отдельная задача (spec Out of Scope).
- **Передача каталогов/рекурсия/архивы с авто-распаковкой** — не в v1.
- **Стриминговая/chunked-загрузка файлов больше потолка из `max_body_bytes`** — отдельная задача
  (threat-model ОР-U6; spec Out of Scope/AC16).
- **Создание символических/жёстких ссылок и спецфайлов (FIFO/устройства)** — запрещено; только
  обычный файл (SR-73; spec Out of Scope).
- **Смена владельца/группы (chown), setuid/sudo, эскалация привилегий** — вне scope (SR-73/AC9).
- **Антивирус/контент-фильтрация/проверка типа содержимого** — не в v1 (threat-model ОР-U5; spec Out
  of Scope).
- **Disk-quota на суммарный объём/число загрузок** — не в v1; постепенное заполнение — остаточный
  риск (threat-model ОР-U3).
- **Блокировка mount points внутри корня (openat2+RESOLVE_*)** — Linux-only/вне stdlib, отвергнуто
  ADR-001; остаточный риск + документация (SR-70; threat-model ОР-U2).
- **Запрет group-writable бита в `mode`** — НЕ требуется в v1 (threat-model решение №2); политика
  ADR-003 (запрет setuid/setgid/sticky/world-writable) признана достаточной.
- **Не-root раскладка сервиса, выбор системного пользователя, capabilities** — задачи
  `service-install`/`distribution`; здесь только «не повышать привилегии» + детекция root (SR-73/
  SR-77).
- **Управление ключами, TLS, rate-limit, протокол MCP, `ping`/`server_info`/`execute_command`** —
  готовы в смежных задачах; здесь только ПОТРЕБЛЯЮТСЯ.
