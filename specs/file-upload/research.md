# Research: file-upload — безопасная запись файла на хост (MCP-инструмент `upload_file`)

> Автор research: research-analyst, команда raxd. Вход: `specs/file-upload/spec.md` (20 AC,
> зашлюзован pm-guardian), `specs/file-upload/context.md` (справка о готовом коде),
> `.claude/reference/{SECURITY-BASELINE,MCP-INTEGRATION,STACK}.ru.md`, образец
> `specs/command-exec/research.md`. Задача: собрать факты С URL и дать обоснованные варианты для
> **architect** (финальную архитектуру выбирает он). Код не пишется. Все факты проверены по
> первоисточникам.
>
> **Ревизия после research-guardian (needs-changes):** перечень методов `os.Root` в Go 1.25
> верифицирован фактически через WebFetch (2026-05-22) — go.dev/doc/go1.25 (дословный список) и
> pkg.go.dev/os@go1.25.0#Root (полный Index методов); подтверждено отсутствие `Root.CreateTemp`;
> факты о коде аудита подтверждены прямым чтением `internal/server/audit.go`.
>
> Ограничения, которые research НЕ нарушает (из задания, STACK и go.mod):
> - **Go 1.25.0** (`go.mod`: `go 1.25.0`), **stdlib предпочтительна**; проект **вендорится**,
>   `proxy.golang.org` недоступен в Docker → новые внешние зависимости крайне нежелательны.
> - Уже есть: MCP go-sdk **v1.6.0** (`mcp.AddTool` + jsonschema-генерация типов,
>   `additionalProperties:false` по дефолту), Bearer-auth + Origin/Host-валидация + rate-limit +
>   аудит (`AuditRecord` с fingerprint+remote из ctx, рендер через `charmbracelet/log`),
>   `bodyLimitMiddleware` (`max_body_bytes`, дефолт 1 MiB). Образец атомарной записи —
>   `internal/keystore/keystore.go` (`writeDB`: temp→chmod→write→sync→rename→fsync-dir).
> - Платформы: **Linux + darwin**, amd64/arm64. Windows вне scope.

---

## Вопросы (привязка к AC)

- Q1 (AC4): защита от path traversal — как безопасно резолвить путь назначения внутрь upload root; подводные камни голого `strings.HasPrefix`; CWE-22.
- Q2 (AC4): симлинки внутри корня, указывающие наружу (TOCTOU); `EvalSymlinks` vs `O_NOFOLLOW` vs openat2; различия Linux/darwin; что реалистично на stdlib.
- Q3 (AC4/AC5b — главный кандидат): **`os.Root` (Go 1.24/1.25)** — traversal-safe файловые операции в пределах каталога; наличие в Go 1.25, API, гарантии, платформы.
- Q4 (AC10): атомарная запись (temp в каталоге назначения → write → fsync → rename → fsync-dir); очистка temp при ошибке; как сочетается с `os.Root`.
- Q5 (AC9): права файла — `OpenFile` perm + влияние umask; нужен ли explicit `Chmod`; 0600 vs 0644; нюанс race у `Root.Chmod`.
- Q6 (AC6/AC7): base64-декодирование с лимитом — `encoding/base64`; раздувание 4/3; валидация корректности; защита от огромного входа.
- Q7 (AC5b): `MkdirAll` промежуточных каталогов внутри корня, права каталогов, безопасность относительно `os.Root`.
- Q8 (AC8): перезапись по умолчанию запрещена — `O_CREATE|O_EXCL` атомарный отказ при существующем файле; цель — существующий каталог.
- Q9 (AC11): детекция root-демона — `os.Geteuid()==0` → WARN (как в command-exec).
- Q10 (AC12/AC19): запись пути и размера в аудит — консистентно с command-exec; варианты для architect.

---

## Q1. Защита от path traversal (AC4)

### Найдено (факт → источник)
- **CWE-22 (Path Traversal):** «The product uses external input to construct a pathname … but the product does not properly neutralize special elements within the pathname that can cause the pathname to resolve to a location that is outside of the restricted directory». Рекомендация: «Use a built-in path canonicalization function … that produces the canonical version of the pathname, which effectively removes ".." sequences **and symbolic links**»; «Inputs should be decoded and canonicalized to the application's current internal representation **before being validated**». Блоклисты `../` недостаточны: «sequential removal of patterns like "../" can still leave dangerous sequences intact». → https://cwe.mitre.org/data/definitions/22.html
- `filepath.Clean` — «returns the shortest path name equivalent to path **by purely lexical processing**» (схлопывает `.`, внутренние `..`, и `/..` в начале rooted-пути заменяет на `/`). Это **только лексика**, не разыменовывает симлинки. → https://pkg.go.dev/path/filepath#Clean
- `filepath.Rel` — «Rel returns a relative path that is lexically equivalent to targPath … Rel calls Clean on the result. An error is returned if targPath can't be made relative to basePath». Если результат начинается с `..` — путь вне базы. → https://pkg.go.dev/path/filepath#Rel
- `filepath.IsLocal` (Go 1.20) — «reports whether path, **using lexical analysis only**, has all of these properties: is within the subtree rooted at the directory in which path is evaluated; is not an absolute path; is not empty; on Windows, is not a reserved name». ВАЖНО: «IsLocal is a purely lexical operation. **In particular, it does not account for the effect of any symbolic links** that may exist in the filesystem.» → https://pkg.go.dev/path/filepath#IsLocal
- Подводный камень голого `strings.HasPrefix(clean, root)`: префиксное сравнение строк ловит `/root2` как «внутри» `/root` (общий строковый префикс ≠ вложенность каталога) — отсюда необходимость сравнивать с `root + string(os.PathSeparator)` либо использовать `filepath.Rel` и проверять отсутствие ведущего `..`. (Это следствие семантики `strings.HasPrefix` — сравнение байтов строки, не компонентов пути.) → https://pkg.go.dev/strings#HasPrefix , https://pkg.go.dev/path/filepath#Rel

### Варианты (ручная лексическая валидация — без учёта симлинков, см. Q2/Q3)
- **A: `filepath.IsLocal(path)` → отвергнуть, если false; затем `filepath.Join(root, path)`** — плюсы: одна проверка отвергает абсолютные пути, `..`-escape и пустой путь; stdlib; ровно «is within the subtree». Минусы: **только лексика**, симлинки НЕ учитывает (нужна доп. защита, Q2/Q3); Windows-reserved не релевантен (Windows вне scope). → https://pkg.go.dev/path/filepath#IsLocal
- **B: `filepath.Rel(root, filepath.Join(root, path))` → отвергнуть, если результат начинается с `..` или `==`„..“** — плюсы: явная проверка вложенности после нормализации; stdlib. Минусы: больше шагов; всё ещё лексика (симлинки не учитываются). → https://pkg.go.dev/path/filepath#Rel
- **C: голый `strings.HasPrefix(filepath.Clean(abs), root)`** — плюсы: тривиально. Минусы: **баг `/root` vs `/root2`** (строковый префикс ≠ вложенность); требует trailing separator и всё равно хрупко; не покрывает симлинки. Отвергнут как самостоятельная мера. → https://pkg.go.dev/strings#HasPrefix , CWE-22

### Рекомендация
Лексическая часть: **A** (`filepath.IsLocal`) как самый прямой и наименее ошибко-опасный способ отвергнуть абсолютные/`..`-пути; **C (голый HasPrefix) НЕ использовать**. Но лексика НЕ закрывает симлинки (AC4 явно требует отвергать симлинк наружу) → см. Q2 и **главный кандидат Q3 (`os.Root`)**, который снимает и лексику, и симлинки, и TOCTOU одним механизмом. Развилка «`os.Root` vs ручная валидация» значима → **ADR-001**.

---

## Q2. Симлинки внутри корня, указывающие наружу + TOCTOU (AC4)

### Найдено (факт → источник)
- Класс атаки (Go blog «Traversal-resistant file APIs»): три вектора — (1) relative-path escape (`..`); (2) **symlink attack** «the attacker controls part of the local filesystem … use symbolic links to cause a program to access the wrong file»; (3) **TOCTOU race** — даже с проверкой через `filepath.EvalSymlinks` «the attacker creates a symlink **after** the program's check» и «The Open call follows the symlink». → https://go.dev/blog/osroot
- `filepath.EvalSymlinks` — разыменовывает симлинки в путь, после чего можно проверить, что результат внутри корня. НО: это **проверка отдельно от открытия** → классический TOCTOU (симлинк подменяют между check и open). → https://go.dev/blog/osroot , https://pkg.go.dev/path/filepath#EvalSymlinks
- `O_NOFOLLOW` доступен как `syscall.O_NOFOLLOW` (POSIX-флаг, есть на Linux и Darwin). НО он запрещает следование симлинку **только в последнем компоненте** пути — родительские компоненты-симлинки он не контролирует. → https://pkg.go.dev/syscall#Openat (constants), https://man7.org/linux/man-pages/man2/open.2.html (O_NOFOLLOW)
- `openat2(2)` с `RESOLVE_BENEATH`/`RESOLVE_NO_SYMLINKS` (Linux 5.6+) — атомарно запрещает выход за каталог-корень. НО: **в stdlib `syscall` НЕ экспонирован** (`Openat` есть, `Openat2`/`RESOLVE_*` нет); «Developers needing openat2 or RESOLVE flags must use `golang.org/x/sys/unix`» — это **новая внешняя зависимость**, нежелательна. И это **Linux-специфично** (на darwin openat2 нет вовсе). → https://pkg.go.dev/syscall#Openat
- `golang.org/x/sys` уже присутствует транзитивно (`golang.org/x/sys v0.41.0 // indirect` в go.mod) — но прямое использование openat2 всё равно Linux-only и не кросс-платформенно. → `go.mod`

### Варианты
- **A: `filepath.EvalSymlinks` (проверка) + затем open** — плюсы: stdlib; разыменовывает все компоненты. Минусы: **TOCTOU-race** (проверка отдельно от открытия) — Go blog прямо называет это дырой; не атомарно. → https://go.dev/blog/osroot
- **B: `O_NOFOLLOW` при open** — плюсы: stdlib; запрещает симлинк в финальном компоненте. Минусы: **не контролирует родительские симлинки**; неполная защита для путей с подкаталогами (а AC5b требует промежуточные подкаталоги). → https://man7.org/linux/man-pages/man2/open.2.html
- **C: openat2 + RESOLVE_BENEATH (Linux)** — плюсы: атомарная, без TOCTOU. Минусы: **не в stdlib** (нужен `golang.org/x/sys/unix` напрямую = новая зависимость в графе использования), **Linux-only** (нет на darwin) → нарушает кросс-платформенность и предпочтение stdlib. Отвергнут. → https://pkg.go.dev/syscall#Openat
- **D: `os.Root` (Go 1.24/1.25)** — реализован поверх семейства `openat`, **атомарно** защищает от `..`, симлинков наружу И TOCTOU, кросс-платформенно (Unix), без новых зависимостей. См. Q3 — **главный кандидат**. → https://go.dev/blog/osroot

### Рекомендация
**D (`os.Root`)** — единственный stdlib-вариант, закрывающий все три вектора (`..`, симлинк наружу, TOCTOU) кросс-платформенно. A (EvalSymlinks) оставляет TOCTOU; B (O_NOFOLLOW) неполон для подкаталогов; C (openat2) — внешняя зависимость и Linux-only. Деталь → **ADR-001**.

---

## Q3. `os.Root` (Go 1.24/1.25) — traversal-safe запись (AC4/AC5b) — ГЛАВНЫЙ КАНДИДАТ → ADR-001

### Найдено (факт → источник; перечень методов верифицирован WebFetch 2026-05-22)
- **Наличие в Go 1.25 — ДА.** `os.Root`/`os.OpenRoot` добавлены в **Go 1.24**: «The new `os.Root` type provides the ability to perform filesystem operations within a specific directory.» «`os.OpenRoot` … Methods on `os.Root` operate within the directory and **do not permit paths that refer to locations outside the directory, including ones that follow symbolic links out of the directory**.» Базовые методы 1.24: `Open`, `Create`, `Mkdir`, `Stat`. → https://go.dev/doc/go1.24
- **Go 1.25 расширил набор методов — ДОСЛОВНАЯ цитата из release notes** (введение + полный bullet-список; верифицировано WebFetch 2026-05-22). Текст release notes: «The `Root` type supports the following additional methods:» — далее списком: `Root.Chmod`, `Root.Chown`, `Root.Chtimes`, `Root.Lchown`, `Root.Link`, `Root.MkdirAll`, `Root.ReadFile`, `Root.Readlink`, `Root.RemoveAll`, `Root.Rename`, `Root.Symlink`, `Root.WriteFile`. → https://go.dev/doc/go1.25
- **Перечень ВСЕХ методов `*Root` в Go 1.25 (из Index pkg.go.dev/os@go1.25.0#Root; верифицировано WebFetch 2026-05-22).** Присутствуют, среди прочего: `OpenFile(name, flag, perm)`, `Create(name)`, `Open(name)`, `Mkdir(name, perm)`, **`MkdirAll(name, perm)`**, **`Rename(oldname, newname)`**, `Remove(name)`, `RemoveAll(name)`, `Stat(name)`, `Lstat(name)`, `Readlink(name)`, `Chmod(name, mode)`, `Chown`, `Chtimes`, `Lchown`, `Link`, `Symlink`, `ReadFile`, `WriteFile`, `Name()`, `Close()`, `FS()`, `OpenRoot(name)`. **Все методы, на которых стоят рекомендации (OpenFile/Create, MkdirAll, Mkdir, Rename, Remove, Stat, Lstat, Chmod), присутствуют в Go 1.25 — fallback НЕ требуется.** → https://pkg.go.dev/os@go1.25.0#Root
- **`Root.CreateTemp` ОТСУТСТВУЕТ** (Finding #4): метода `CreateTemp` нет в полном перечне методов `*Root` на pkg.go.dev/os@go1.25.0#Root (выведено из отсутствия в Index методов; верифицировано WebFetch 2026-05-22 — явный ответ «NO, there is no CreateTemp method on *Root»). Следствие: уникальное имя temp-файла генерируется кодом самостоятельно (см. Q4/ADR-002), а не через несуществующий `Root.CreateTemp`. → https://pkg.go.dev/os@go1.25.0#Root
- **Гарантия (package doc, go1.25.0; верифицировано WebFetch):** «Methods on Root can **only access files and directories beneath a root directory**. **If any component of a file name passed to a method of Root references a location outside the root, the method returns an error.**» «Methods on Root **will follow symbolic links, but symbolic links may not reference a location outside the root**.» → https://pkg.go.dev/os@go1.25.0#Root
- **TOCTOU/симлинк-гонки:** Go blog — `os.Root` методы «**disallow any operations that would escape from the root either using relative path components ("..") or symlinks**»; реализация на Unix через семейство `openat`, что устраняет TOCTOU класса «подмена симлинка после проверки» (нет отдельной фазы check). Разрешено `root.Open("a/../b")` (внутренние `..`, не выходящие за корень). → https://go.dev/blog/osroot
- **Платформы:** «On Unix systems, Root is implemented using the openat family of system calls» (Linux **и** darwin — оба Unix); на Windows — handle на каталог; на `GOOS=js` уязвим к TOCTOU (не наш scope). → https://go.dev/blog/osroot
- **Известные ограничения / нюансы (важно для architect):**
  - «Root defends against symlink traversal **but does not limit traversal of mount points**» — bind-mount внутри корня, ведущий наружу, не блокируется (низкий риск в нашем контейнерном демоне, baseline §6; зафиксировать). → https://go.dev/blog/osroot
  - **На Unix `Root.Chmod`, `Root.Chown`, `Root.Chtimes` уязвимы к race condition** (package doc прямо отмечает) — влияет на стратегию выставления прав файла (Q5/ADR-002). → https://pkg.go.dev/os@go1.25.0#Root
  - Производительность: «current implementation prioritizes correctness and safety over performance» — для записи одного файла в пределах `max_body_bytes` несущественно. → https://go.dev/blog/osroot
- **API, релевантный записи файла (все методы подтверждены в перечне выше):** `OpenRoot(root)` → `(*Root)`; `Root.OpenFile(name, flag, perm)` (с `O_CREATE|O_EXCL|O_WRONLY` — атомарный эксклюзивный create, Q8); `Root.Create`; `Root.MkdirAll(name, perm)` (Q7); `Root.Stat`/`Root.Lstat`; `Root.Remove` (очистка temp); `Root.Rename(old,new)` (атомарный rename внутри корня, Q4); `Root.Chmod` (Q5, с оговоркой про race); `Root.Close`. → https://pkg.go.dev/os@go1.25.0#Root

### Варианты
- **A: `os.Root` для всех ФС-операций записи** (открыть upload root через `OpenRoot` один раз/на вызов; все операции — `Root.MkdirAll`/`Root.OpenFile`/`Root.Rename`/`Root.Remove` по относительным путям из запроса) — плюсы: **закрывает AC4 целиком** (`..`, абсолютный путь, симлинк наружу, TOCTOU) одним механизмом stdlib; кросс-платформенно (Unix); промежуточные каталоги (AC5b) — `Root.MkdirAll`; **новых зависимостей нет**; радикально проще и безопаснее ручной валидации. Минусы: не ограничивает mount points (низкий риск, зафиксировать); `Root.Chmod` race на Unix (обход — Q5/ADR-002); требует Go 1.25 для `MkdirAll` (есть). → https://go.dev/doc/go1.24 , https://go.dev/doc/go1.25 , https://pkg.go.dev/os@go1.25.0#Root
- **B: ручная валидация (`filepath.IsLocal`/`Rel`) + обычные `os.OpenFile`/`os.MkdirAll`** — плюсы: знакомый код. Минусы: лексика **не закрывает симлинки** → нужен ещё `EvalSymlinks` (TOCTOU) или `O_NOFOLLOW` (неполон); больше ручного кода = больше шансов на ошибку безопасности (CWE-22 предупреждает о хрупкости ручных проверок); по сути воспроизводит то, что `os.Root` даёт из коробки и хуже. → https://pkg.go.dev/path/filepath#IsLocal , https://go.dev/blog/osroot , CWE-22
- **C: openat2 + RESOLVE_BENEATH напрямую** — плюсы: атомарно. Минусы: внешняя зависимость (`x/sys`), Linux-only — отвергнут (см. Q2). → https://pkg.go.dev/syscall#Openat

### Рекомендация
**A (`os.Root`)** — **вероятная рекомендация** для architect: это самый сильный и при этом stdlib-вариант, закрывающий AC4 (traversal + симлинк наружу + TOCTOU) и AC5b (`MkdirAll` внутри корня) без новых зависимостей, кросс-платформенно на Linux+darwin (оба — Unix/openat). Доступен в Go 1.25 проекта (базовый тип — 1.24, нужные `MkdirAll`/`Rename`/`Chmod` — 1.25; перечень методов верифицирован WebFetch, fallback не нужен). Зафиксировать как ограничение: не покрывает mount points (низкий риск в контейнере baseline §6) и `Root.Chmod`-race (стратегия прав — Q5/ADR-002). Финальный выбор и обработка нюансов — за architect/security. → **ADR-001**.

---

## Q4. Атомарная запись (AC10) — temp→write→fsync→rename→fsync-dir

### Найдено (факт → источник / код)
- Образец проекта (прочитан `internal/keystore/keystore.go`, метод `writeDB`): `os.CreateTemp(dir, ...)` (temp в **том же каталоге**) → `tmp.Chmod(0600)` → `tmp.Write` → `tmp.Sync()` → `tmp.Close()` → `os.Rename(tmp, target)` → `os.Open(dir)`+`dirF.Sync()`; **очистка temp `os.Remove` при любой ошибке до rename** (на каждой ветке). → прочитан `internal/keystore/keystore.go`
- `os.Rename` на одной ФС — атомарная замена цели; temp **обязан быть в каталоге назначения** (иначе rename через границу устройств = `EXDEV`, не атомарен). → https://pkg.go.dev/os#Rename , https://man7.org/linux/man-pages/man2/rename.2.html (EXDEV)
- `File.Sync()` — «commits the current contents of the file to stable storage»; fsync каталога делает durable сам факт rename (запись dir-entry). → https://pkg.go.dev/os#File.Sync
- В терминах `os.Root` (методы подтверждены в Q3): `Root.OpenFile`/`Root.Create` для temp в подкаталоге назначения (внутри корня), `Root.Rename(tmpRel, targetRel)` для атомарной фиксации внутри корня, `Root.Remove(tmpRel)` для очистки. **`Root.CreateTemp` отсутствует** (Q3) → имя temp генерируется кодом. Все пути относительны корню → traversal-safe. → https://pkg.go.dev/os@go1.25.0#Root
- AC10/AC7 требуют: при ошибке/превышении лимита **не остаётся ни частичного целевого, ни temp-файла**. Образец keystore уже даёт паттерн `defer/Remove` на каждой ветке ошибки. → spec AC10/AC7 ; прочитан `internal/keystore/keystore.go`

### Варианты
- **A: схема keystore, но через `os.Root`** (temp в каталоге назначения через `Root`, write→sync→`Root.Rename`→fsync-dir, `Root.Remove` temp при ошибке) — плюсы: атомарность (AC10) + traversal-safe (AC4) совмещены; консистентно с проектом; stdlib. Минусы: temp-имя должно быть внутри корня и в том же подкаталоге, что цель (для атомарного rename) — `os.Root` НЕ имеет `CreateTemp` (подтверждено Q3), имя temp придётся генерировать самим (`crypto/rand`-суффикс) и создавать через `Root.OpenFile(O_CREATE|O_EXCL)`. → https://pkg.go.dev/os@go1.25.0#Root , прочитан `internal/keystore/keystore.go`
- **B: писать прямо в целевой файл (без temp)** — плюсы: проще. Минусы: **нарушает AC10** (виден частичный файл при обрыве; перезапись теряет старое при сбое). Отвергнут. → spec AC10
- **C: `Root.WriteFile`** (Go 1.25) — плюсы: одна вызов-операция, traversal-safe. Минусы: `WriteFile` **не атомарен** (truncate+write напрямую в цель) → не даёт AC10 (частичный файл при обрыве); не годится для overwrite-семантики AC8/AC10. Отвергнут как механизм атомарности (можно лишь как образец API, не для финальной записи). → https://pkg.go.dev/os@go1.25.0#Root , spec AC10

### Рекомендация
**A**: повторить проверенную схему keystore (temp в каталоге назначения → write → `Sync` → `Rename` → fsync-dir, очистка temp на каждой ошибке), но через методы `os.Root`, чтобы атомарность и traversal-safety были одним согласованным механизмом. Генерация уникального имени temp (нет `Root.CreateTemp`, подтверждено Q3) — деталь реализации (суффикс из `crypto/rand`, создание через `O_CREATE|O_EXCL`), решает architect/developer; факт-механизм (rename в пределах одного каталога/ФС атомарен, temp обязателен в каталоге цели) подтверждён.

---

## Q5. Права создаваемого файла (AC9) — perm + umask + Chmod → ADR-002

### Найдено (факт → источник)
- `os.OpenFile(name, flag, perm)`: «If the file does not exist, and the O_CREATE flag is passed, it is created with mode perm **(before umask)**». То есть фактический режим = `perm &^ umask` — umask может **срезать** биты (например, при umask 022 запрошенный 0666 станет 0644; для 0600 umask 022 не меняет, но umask 077 у 0644 даст 0600). → https://pkg.go.dev/os#OpenFile
- `File.Chmod(mode)` / `os.Chmod` — «changes the mode of the file to mode»; **umask НЕ применяется** к Chmod → explicit Chmod после создания гарантирует точные биты независимо от umask демона. → https://pkg.go.dev/os#OpenFile (раздел Chmod) , https://pkg.go.dev/os#Chmod
- `Root.Chmod` существует (Go 1.25, подтверждено перечнем Q3), НО package doc: «On Unix, `Root.Chmod`, `Root.Chown`, and `Root.Chtimes` are **vulnerable to a race condition**» — между открытием по имени и chmod возможна подмена. → https://pkg.go.dev/os@go1.25.0#Root
- Обход race: выставлять права на **уже открытый дескриптор** (`(*os.File).Chmod` на fd, полученном из `Root.OpenFile`), а не по имени через `Root.Chmod` — chmod по fd не подвержен симлинк-подмене имени. (Следствие семантики fchmod по дескриптору.) → https://pkg.go.dev/os#File.Chmod
- baseline / spec: дефолт `0600` рекомендован (AC9), наследование UID/GID демона, без chown/setuid. → spec AC9 ; STACK «Состояние/ключи … права 0600». → `.claude/reference/STACK.ru.md`
- Образец keystore (прочитан): `tmp.Chmod(0o600)` **до записи** содержимого (нет окна с более широкими правами). → прочитан `internal/keystore/keystore.go`

### Варианты
- **A: создать temp с perm + `(*os.File).Chmod(mode)` на дескрипторе ДО записи** (как keystore) — плюсы: точные биты независимо от umask; нет окна с широкими правами; chmod по fd обходит race `Root.Chmod`-по-имени; stdlib. Минусы: нужно аккуратно применить желаемый `mode` (из поля запроса или дефолт) до записи. → https://pkg.go.dev/os#File.Chmod , прочитан `internal/keystore/keystore.go`
- **B: только `OpenFile(..., perm)` без Chmod** — плюсы: проще. Минусы: **umask может срезать биты** → фактический режим непредсказуем (нарушает AC9 «umask-независимый режим»); AC9 прямо требует umask-независимости. Отвергнут. → https://pkg.go.dev/os#OpenFile
- **C: `Root.Chmod(name, mode)` по имени** — плюсы: одна строка. Минусы: package doc — **race на Unix**; противоречит цели безопасной записи. Отвергнут в пользу chmod по fd. → https://pkg.go.dev/os@go1.25.0#Root

### Рекомендация
**A**: создать temp-файл, затем `(*os.File).Chmod(желаемый_mode)` на полученном дескрипторе **до записи** содержимого (паттерн keystore), что даёт umask-независимый точный режим (AC9) и обходит race метода `Root.Chmod`-по-имени. Дефолт `0600`, поле `mode` валидируется в разрешённом диапазоне (политику диапазона/запрет setuid·setgid·sticky·world-writable задаёт architect/security — spec Q2). Развилка «как именно выставлять права в связке с `os.Root`» значима → **ADR-002** (совместно с атомарностью). → spec AC9

---

## Q6. base64-декодирование с лимитом (AC6/AC7)

### Найдено (факт → источник)
- `base64.StdEncoding.DecodeString(s)` — «If the input is malformed, it returns the partially decoded data and **`CorruptInputError`**» → невалидный base64 даёт типизированную ошибку (закрывает AC6 «невалидный base64 → isError»). `StdEncoding` = RFC 4648 (`+/`, с паддингом `=`). → https://pkg.go.dev/encoding/base64#Encoding.DecodeString , https://pkg.go.dev/encoding/base64#CorruptInputError
- `DecodedLen(n)` — «maximum length in bytes of the decoded data corresponding to n bytes of base64-encoded data»; соотношение 4 закодированных байта → 3 декодированных, т.е. `DecodedLen ≈ n*3/4`. Обратно: base64 раздувает на ~33% (4/3). → https://pkg.go.dev/encoding/base64#Encoding.DecodedLen
- `base64.NewDecoder(enc, r)` — «Constructs a new base64 stream decoder», возвращает `io.Reader` → можно обернуть `io.LimitReader` для **потокового** декодирования с жёстким потолком памяти (декодировать не более N байт). → https://pkg.go.dev/encoding/base64#NewDecoder , https://pkg.go.dev/io#LimitReader
- Контекст AC16: тело запроса уже ограничено `bodyLimitMiddleware` (`max_body_bytes`, дефолт 1 MiB) **до** инструмента; декодированный размер ограничивается `max_file_bytes` (AC7). Из-за раздувания 4/3 потолок одного файла ≈ `(max_body_bytes − overhead) × 3/4`. → `internal/server/middleware.go` (по context.md) ; spec AC16

### Варианты
- **A: `DecodeString` целиком, затем проверка `len(decoded) ≤ max_file_bytes`** — плюсы: просто; `CorruptInputError` ловит невалидный вход (AC6). Минусы: декодирует всё в память до проверки размера — но тело уже ограничено `max_body_bytes` (1 MiB), поэтому пик памяти ограничен транспортом; для v1-потолка приемлемо. → https://pkg.go.dev/encoding/base64#Encoding.DecodeString
- **B: предварительная проверка `DecodedLen(len(content)) ≤ max_file_bytes` ДО декодирования + затем `DecodeString`** — плюсы: ранний отказ по размеру без декодирования (дешевле при превышении). Минусы: `DecodedLen` — **максимум** (паддинг даёт небольшую погрешность) → как ранний фильтр годится, точную проверку всё равно по факту `len(decoded)`. → https://pkg.go.dev/encoding/base64#Encoding.DecodedLen
- **C: `NewDecoder`+`io.LimitReader(N+1)` потоково** — плюсы: жёсткий потолок памяти даже без доверия к `max_body_bytes`; чтение N+1 байт → если прочитано >N, превышение. Минусы: чуть больше кода; для v1 (тело ≤1 MiB) выигрыш невелик. → https://pkg.go.dev/encoding/base64#NewDecoder , https://pkg.go.dev/io#LimitReader

### Рекомендация
**B затем A**: сначала ранний фильтр по `DecodedLen(len(content)) > max_file_bytes` → `isError` без декодирования; затем `DecodeString` (ловит `CorruptInputError` для AC6) и точная проверка `len(decoded) ≤ max_file_bytes` (AC7). Поскольку тело уже ограничено `max_body_bytes` (AC16), пик памяти ограничен транспортом — потоковый C избыточен для v1, но остаётся опцией усиления (architect). Числа `max_file_bytes`/`max_body_bytes` (с учётом 4/3 + overhead) — за architect/security (spec Q4). → https://pkg.go.dev/encoding/base64

---

## Q7. MkdirAll промежуточных каталогов внутри корня (AC5b)

### Найдено (факт → источник)
- `os.MkdirAll(path, perm)` — «creates a directory named path, along with any necessary parents … The permission bits perm **(before umask)** are used for all directories that MkdirAll creates. **If path is already a directory, MkdirAll does nothing and returns nil.**» → https://pkg.go.dev/os#MkdirAll
- `Root.MkdirAll` (Go 1.25, присутствие подтверждено перечнем Q3) — тот же смысл, но **в пределах корня** (traversal-safe): не создаст каталог вне корня; компонент, выводящий наружу, → ошибка. → https://go.dev/doc/go1.25 , https://pkg.go.dev/os@go1.25.0#Root
- Права каталогов: perm до umask → для предсказуемых прав каталога (напр. `0700`) тот же нюанс umask, что и у файла (Q5); explicit поправка прав каталога — если нужна гарантия. → https://pkg.go.dev/os#MkdirAll

### Варианты
- **A: `Root.MkdirAll(filepath.Dir(relPath), 0700)`** — плюсы: создаёт промежуточные подкаталоги **только внутри корня** (AC5b); создание вне корня невозможно (следствие AC4); stdlib, без зависимостей. Минусы: права каталога зависят от umask (как файл) — если нужны строго `0700`, держать в уме umask демона. → https://pkg.go.dev/os@go1.25.0#Root
- **B: ручной `os.MkdirAll(filepath.Join(root, dir))`** — плюсы: знакомо. Минусы: вне `os.Root` теряется traversal-гарантия для промежуточных компонентов (симлинк-подкаталог) → надо снова валидировать вручную. Хуже A. → https://pkg.go.dev/os#MkdirAll

### Рекомендация
**A**: `Root.MkdirAll` для каталога назначения относительно корня — создаёт недостающие подкаталоги внутри корня (AC5b) и физически не может создать их снаружи (AC4), без новых зависимостей. Права каталогов (рекоменд. `0700`) и поведение umask — деталь, согласуемая architect/security (как и дефолт upload root, spec Q1). → spec AC5b/AC4

---

## Q8. Перезапись по умолчанию запрещена; цель — каталог (AC8/AC14)

### Найдено (факт → источник)
- `O_EXCL` с `O_CREATE`: «used with O_CREATE, **file must not exist**» → `OpenFile(name, O_CREATE|O_EXCL|O_WRONLY, perm)` **атомарно** падает, если файл существует. Это естественный механизм для `overwrite:false` (AC8): отказ без гонки. → https://pkg.go.dev/os#OpenFile
- Для `overwrite:true`: писать temp + `Rename` поверх (rename атомарно заменяет существующий целевой, AC8/AC10), а не открывать цель с `O_TRUNC` напрямую (это нарушило бы AC10 при обрыве). → https://pkg.go.dev/os#Rename
- Цель — существующий **каталог** (AC14): `Root.Stat(target)` + `FileInfo.IsDir()` → deny; либо `Rename` поверх каталога вернёт ошибку (rename файла на непустой каталог не разрешён) — но явная проверка `IsDir` даёт чистый `isError` без частичных эффектов. → https://pkg.go.dev/os#Stat , https://pkg.go.dev/io/fs#FileInfo
- Семантика для temp-схемы (Q4): existence-проверку цели делать **до** rename — `overwrite:false` + цель существует → deny (не писать temp зря или удалить temp); `overwrite:true` → rename заменяет. → spec AC8/AC10

### Варианты
- **A: проверка существования цели через `Root.Stat` (и `IsDir`) + при `overwrite:false` deny; при `overwrite:true` — temp+`Rename` поверх** — плюсы: явный контроль AC8 (overwrite-политика) и AC14 (цель-каталог → deny); атомарность через rename; traversal-safe. Минусы: `Stat`+`Rename` — два шага (узкое окно между ними; для v1 однопоточной семантики приемлемо; rename поверх каталога всё равно вернёт ошибку как страховка). → https://pkg.go.dev/os@go1.25.0#Root
- **B: полагаться только на `O_CREATE|O_EXCL` для прямой записи в цель** — плюсы: атомарный отказ при существовании. Минусы: для `overwrite:true` всё равно нужен путь через temp+rename (иначе нет AC10); прямая запись в цель нарушает атомарность. Подходит лишь как часть схемы, не целиком. → https://pkg.go.dev/os#OpenFile

### Рекомендация
**A**: temp-файл всегда создавать с `O_CREATE|O_EXCL` (уникальное имя — не конфликтует), а политику overwrite решать проверкой существования цели (`Root.Stat`/`IsDir`) перед `Rename`: `overwrite:false`+цель есть → deny (AC8); цель — каталог → deny (AC14); иначе `Rename` атомарно фиксирует/заменяет (AC8/AC10). Очистить temp при любом deny/ошибке (AC10). Точные детали (порядок проверок) — за architect; факт-механизмы (O_EXCL атомарен, rename заменяет, Stat/IsDir) подтверждены.

---

## Q9. Детекция root-демона (AC11)

### Найдено (факт → источник)
- `os.Geteuid()` «returns the numeric effective user id of the caller. On Windows, it returns -1.» Доступна на Linux и darwin (POSIX), без ошибки. → https://pkg.go.dev/os#Geteuid
- AC11 требует: при euid==0 на **каждый** вызов `upload_file` — WARN-аудит об операции записи от root (детекция обязательна; hard-fail — опциональное политическое решение, как `exec.deny_root`). → spec AC11
- Образец: command-exec уже делает `os.Geteuid()==0` → WARN через существующий аудит (ADR-003 command-exec). → `specs/command-exec/research.md` Q6 ; baseline §3 «Демон работает НЕ от root». → `.claude/reference/SECURITY-BASELINE.ru.md` §3
- Подтверждение из кода: `internal/server/audit.go` поддерживает `Result:"warn"` (отдельный уровень WARN, семантически отличный от deny) — комментарий «"warn" используется для предупреждений (напр. root-WARN SR-55), команда при этом может продолжиться». → прочитан `internal/server/audit.go`

### Варианты
- **A: `os.Geteuid()==0` → WARN-запись через существующий аудит при каждом вызове** (как command-exec) — плюсы: кросс-платформенно, stdlib, ровно по AC11; консистентно с `execute_command`; уровень `Result:"warn"` уже есть в `writeAudit`. Минусы: только детекция/предупреждение (не запрет). → https://pkg.go.dev/os#Geteuid , прочитан `internal/server/audit.go`
- **B: + опциональный hard-fail при euid==0** (`upload.deny_root`, по аналогии с `exec.deny_root`) — плюсы: строже. Минусы: AC11 делает hard-fail опциональным (политическое решение architect/security). → spec AC11

### Рекомендация
**A** как обязательный минимум (детекция `os.Geteuid()==0` + WARN при каждом `upload_file`), консистентно с command-exec; уровень `Result:"warn"` уже поддержан `writeAudit`. Опциональный `deny_root` для загрузки — политическое решение architect/security (spec Q3). Согласуется с baseline §3 «не от root». → spec AC11

---

## Q10. Запись пути и размера в аудит (AC12/AC19)

### Найдено (факт → источник / прочитанный код)
- **Структура аудита (прочитан `internal/server/audit.go`):** `AuditRecord` имеет поля `TS time.Time`, `Fingerprint string`, `RemoteAddr string`, `Result string` (принимает `"success"`/`"fail"`/`"deny"`/`"warn"`/`"rate-limited"`), `Reason string`, `Tool string`, и блок exec-специфичных полей `Command string`, `Args []string`, `ExitCode *int`, `Duration time.Duration`, `TimedOut bool`. → прочитан `internal/server/audit.go`
- **Паттерн «поля только для своего инструмента» (прочитан `internal/server/audit.go`):** функция `writeAudit` вычисляет `isExec := rec.Tool == "execute_command"` и логирует exec-поля (`command`/`args`/`exit_code`/`duration`/`timed_out`) **только при `isExec`**; не-exec и не-MCP записи (AUTH/RATE и т.п.) пишутся прежним набором ключей. Рендер — через `charmbracelet/log` методами `logger.Info("MCP", "fp", …, "tool", …, "result", "ok", …)` / `logger.Warn("DENY"/"FAIL"/"WARN", …)` с key/value-парами; ветвление по `rec.Result` (switch). → прочитан `internal/server/audit.go`
- **Контракт «без секретов» уже в коде (прочитан `internal/server/audit.go`):** комментарии SR-21/SR-36 — `writeAudit` НЕ должен логировать тело ключа, raw Authorization, hash, salt, приватный TLS-ключ; `tool=` пишется только при `rec.Tool != ""`. Это совпадает с требованием AC12/AC13 (содержимое файла НЕ логируется НИКОГДА). → прочитан `internal/server/audit.go`
- Образец command-exec: собственный аудит **в handler** (без generic `withAudit`), ровно одна основная запись на вызов с результатом ok/deny/fail (ADR-004 command-exec). file-upload по AC19 наследует ту же планку. → `specs/command-exec/decisions/ADR-004-exec-audit-in-handler.md` (существует) ; spec AC19
- AC12 требует поля: timestamp(UTC), fingerprint (НЕ ключ), tool (`upload_file`), относительный путь, размер записанных байт, result (ok/deny/fail), remote. **Содержимое НИКОГДА** не пишется (AC12/AC13). → spec AC12/AC13

### Варианты (способ представления полей пути/размера — spec Q5, делегировано architect)
- **A: расширить `AuditRecord` опциональными полями (напр. `Path string` и `Size int64`), логировать только при `Tool=="upload_file"`** — точное зеркало того, как `writeAudit` уже логирует exec-поля только при `Tool=="execute_command"` (`isExec`-ветвление). Плюсы: единый формат аудита, не ломает существующие записи (AC12 «не менять формат прочих», как уже сделано для exec-полей); консистентно с прочитанным паттерном `writeAudit`. Минусы: рост структуры `AuditRecord` (приемлемо; поля опциональны). → прочитан `internal/server/audit.go` ; spec AC12
- **B: передавать путь/размер как доп. key/value прямо в вызове логгера** (без полей структуры) — плюсы: без изменения `AuditRecord`. Минусы: расходится с паттерном exec-полей (там — поля структуры + `isExec`-ветка в `writeAudit`); риск рассинхрона набора полей. → прочитан `internal/server/audit.go`

### Рекомендация
**A**: добавить в `AuditRecord` опциональные поля пути (относительного, внутри корня) и размера, логируемые только для `upload_file` (новая ветка `isUpload := rec.Tool == "upload_file"` в `writeAudit`, зеркально существующей `isExec`-ветке) — единый формат, без слома существующих записей (AC12), и с собственным аудитом в handler (AC19, как command-exec ADR-004). Содержимое не логируется никогда (AC12/AC13; уже закреплено комментариями SR-21/SR-36 в коде). Окончательный способ представления — за architect/security (spec Q5); research фиксирует совместимый с прочитанным кодом паттерн. → прочитан `internal/server/audit.go` ; spec AC12/AC19

---

## Сводка рекомендаций для architect

| # | Вопрос (AC) | Рекомендация research | Источник-ключ | Новая зависимость? |
|---|---|---|---|---|
| Q1 | Path traversal лексически (AC4) | `filepath.IsLocal` отвергает абс./`..`; **голый `HasPrefix` НЕ использовать**; но лексика не закрывает симлинки → см. Q3 | filepath#IsLocal, CWE-22 | Нет (stdlib) |
| Q2 | Симлинки + TOCTOU (AC4) | EvalSymlinks=TOCTOU, O_NOFOLLOW неполон, openat2=внешн.+Linux-only → `os.Root` | go.dev/blog/osroot, syscall#Openat | Нет (stdlib) |
| Q3 | **`os.Root`** traversal-safe (AC4/AC5b) | **ADR-001**: `os.Root` (Go 1.24/1.25) — закрывает `..`+симлинк наружу+TOCTOU+MkdirAll внутри корня; **главный кандидат**; методы верифицированы WebFetch | go1.24/go1.25 notes, os@go1.25#Root, blog/osroot | Нет (stdlib) |
| Q4 | Атомарная запись (AC10) | схема keystore (temp в каталоге цели→write→Sync→Rename→fsync-dir, очистка temp) через методы `os.Root`; `Root.WriteFile` НЕ атомарен; `Root.CreateTemp` отсутствует → имя temp генерируем сами | os#Rename, os@go1.25#Root, keystore.go (прочитан) | Нет (stdlib) |
| Q5 | Права файла (AC9) | **ADR-002**: `(*os.File).Chmod` по fd ДО записи (umask-независимо; обходит race `Root.Chmod`-по-имени); дефолт 0600 | os#OpenFile/Chmod, os@go1.25#Root | Нет (stdlib) |
| Q6 | base64 + лимит (AC6/AC7) | ранний `DecodedLen` фильтр → `DecodeString` (CorruptInputError) → точный `len`; тело уже ≤ max_body_bytes | encoding/base64, io#LimitReader | Нет (stdlib) |
| Q7 | MkdirAll внутри корня (AC5b) | `Root.MkdirAll(dir, 0700)` — подкаталоги только внутри корня; вне корня невозможно (AC4) | os@go1.25#Root, os#MkdirAll | Нет (stdlib) |
| Q8 | overwrite/цель-каталог (AC8/AC14) | temp `O_CREATE\|O_EXCL`; overwrite через `Root.Stat`/`IsDir`+`Rename`; цель-каталог→deny | os#OpenFile, os#Rename, os#Stat | Нет (stdlib) |
| Q9 | Детекция root (AC11) | `os.Geteuid()==0`→WARN каждый вызов (как command-exec); уровень `Result:"warn"` уже есть в `writeAudit`; hard-fail опционален | os#Geteuid, audit.go (прочитан) | Нет (stdlib) |
| Q10 | Аудит пути/размера (AC12/AC19) | опц. поля `AuditRecord` (path/size), логируемые только для `upload_file` (зеркало `isExec`-ветки `writeAudit`); аудит в handler | audit.go (прочитан), ADR-004 command-exec | Нет (уже вендорено) |

**Подтверждение по зависимостям:** все рекомендации реализуемы на **stdlib Go 1.25** (`os` с `os.Root`/`os.OpenRoot`/`Root.*`, `path/filepath`, `encoding/base64`, `io`, `crypto/rand` для temp-имени) + **уже вендоренных** `charmbracelet/log` (аудит) и go-sdk v1.6.0 (MCP-инструмент, типизир. вход/выход с `additionalProperties:false`). **Новые внешние зависимости НЕ требуются.** Единственный кандидат, который потребовал бы зависимости — openat2/RESOLVE_BENEATH через `golang.org/x/sys/unix` — **сознательно отклонён** в пользу `os.Root` (stdlib, кросс-платформенно), что устраняет потребность в новой зависимости.

**`os.Root` доступен в Go 1.25 — ДА (верифицировано WebFetch 2026-05-22).** Базовый тип `os.Root`/`os.OpenRoot` и методы `Open`/`Create`/`Mkdir`/`Stat` — с **Go 1.24** (https://go.dev/doc/go1.24). Нужные file-upload методы — `Root.MkdirAll`, `Root.Rename`, `Root.RemoveAll`, `Root.Chmod`, `Root.Readlink`, `Root.WriteFile` и др. — добавлены в **Go 1.25** (дословный список в release notes: https://go.dev/doc/go1.25) и присутствуют в Index методов `*Root` (https://pkg.go.dev/os@go1.25.0#Root). Проект на `go 1.25.0` → весь набор доступен. **Все методы, на которых стоят ADR-001/ADR-002 (OpenFile/Create, MkdirAll, Mkdir, Rename, Remove, Stat, Lstat, Chmod), подтверждены в Go 1.25 — fallback не требуется.** **`Root.CreateTemp` ОТСУТСТВУЕТ** → имя temp генерируется кодом (учтено в Q4/ADR-002).

**Кросс-платформенная заметка (Linux vs darwin):** `os.Root` на обеих платформах реализован через семейство `openat` (Unix) → защита от `..`/симлинков наружу/TOCTOU работает одинаково на Linux и darwin. `os.Geteuid()`, `encoding/base64`, `filepath.IsLocal`, `os.Rename`, umask-семантика `OpenFile`/`MkdirAll` — кросс-платформенны на Unix. openat2/RESOLVE_BENEATH — Linux-only и не в stdlib (не используем). Ограничение `os.Root`: не блокирует traversal через **mount points** (не симлинки) — низкий риск в контейнерном демоне (baseline §6), зафиксировать в threat-model.

---

## Открытые вопросы

- **#1 (наличие методов `os.Root` в Go 1.25) — ЗАКРЫТ.** Перечень методов верифицирован фактически через WebFetch (2026-05-22): дословный список из release notes (https://go.dev/doc/go1.25) и полный Index методов `*Root` (https://pkg.go.dev/os@go1.25.0#Root). Все нужные методы (OpenFile/Create, MkdirAll, Mkdir, Rename, Remove, Stat, Lstat, Chmod) присутствуют; `Root.CreateTemp` отсутствует (имя temp генерируем сами, Q4/ADR-002). Fallback не требуется — ADR-001/ADR-002 не нуждаются в коррекции по доступности API.
- Делегировано architect/security (НЕ research-вопросы — факт-механизмы подтверждены, открыты числа/политики, прямо отмеченные в spec Open Questions Q1–Q5):
  - [ ] Числовое значение безопасного дефолта upload root и его права (рекоменд. `<data-dir>/uploads`, `0700`) — spec Q1/AC5a/AC15.
  - [ ] Политика допустимых значений `mode` (диапазон/маска; запрет setuid·setgid·sticky·world-writable) — spec Q2/AC9.
  - [ ] Политика при root-демоне (WARN vs опциональный hard-fail) — spec Q3/AC11.
  - [ ] Числа `max_file_bytes` и согласованного `max_body_bytes` (учёт base64 +33% + overhead) — spec Q4/AC7/AC16.
  - [ ] Способ представления полей пути/размера в аудите (рекоменд. опц. поля `AuditRecord`) — spec Q5/AC12.
- [ ] **Заметка для architect/security (фиксация в threat-model, требует решения следующего слоя):** ограничения `os.Root` подтверждены источником, но требуют явного принятия риска — (а) `os.Root` **не блокирует mount points** (только симлинки/`..`/TOCTOU); (б) `Root.Chmod`/`Chown`/`Chtimes` **по имени** уязвимы к race на Unix → рекомендация chmod по fd (Q5/ADR-002). Низкий риск в контейнерном демоне (baseline §6), но это решение/фиксация за security/architect, не research. → https://go.dev/blog/osroot , https://pkg.go.dev/os@go1.25.0#Root
- [ ] **Требует верификации architect/developer перед реализацией:** дефолт `additionalProperties:false` SDK go-sdk v1.6.0 для входной схемы `upload_file` (как и в command-exec) — закрепить тестом «вход с лишним полем → `isError`» (AC2). Это унаследованный от command-exec пункт (там подтверждён источниками и Issue #892); для file-upload — то же поведение, проверить при реализации. Не блокирует research.
