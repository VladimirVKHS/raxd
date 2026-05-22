# Research: command-exec — безопасное выполнение команд на хосте (MCP-инструмент `execute_command`)

> Автор research: research-analyst, команда raxd. Вход: `specs/command-exec/spec.md` (18 AC,
> зашлюзован pm-guardian), `.claude/reference/{SECURITY-BASELINE,MCP-INTEGRATION,STACK}.ru.md`.
> Задача: собрать факты С URL и дать обоснованные варианты для **architect** (финальную
> архитектуру выбирает он). Код не пишется. Все факты проверены по первоисточникам.
>
> Ограничения, которые research НЕ нарушает (из задания и STACK):
> - Go 1.25, **stdlib предпочтительна**; проект **вендорится**, `proxy.golang.org` недоступен в
>   Docker → новые внешние зависимости крайне нежелательны.
> - Уже есть: MCP go-sdk **v1.6.0** (`mcp.AddTool` + jsonschema-генерация типов), Bearer-auth +
>   rate-limit + аудит (`charmbracelet/log`, `AuditRecord` с fp+remote из ctx).
> - Платформы: **Linux + darwin**, amd64/arm64. Windows вне scope (учтено для syscall-специфики).

---

## Вопросы (привязка к AC)

- Q1 (AC2): безопасный запуск без shell — `exec.CommandContext(ctx, bin, args...)`; почему нельзя `sh -c`; PATH-резолвинг и риск относительного пути.
- Q2 (AC5/AC6): таймаут и **гарантированное убийство процесса И его потомков**; почему `CommandContext` сам убивает только голову; process group; `Cmd.Cancel`/`Cmd.WaitDelay`; различия Linux vs darwin.
- Q3 (AC11): лимит размера stdout/stderr против OOM; фиксация факта обрезки; как избежать дедлока пайпов.
- Q4 (AC10): ограничение окружения явным whitelist; что обязательно (PATH); риски наследования (LD_PRELOAD, IFS).
- Q5 (AC10): рабочая директория `Cmd.Dir`, валидация, безопасный дефолт.
- Q6 (AC9): детекция root — `os.Geteuid()==0`; кросс-платформенно; согласование с baseline «не от root».
- Q7 (AC7): allowlist — строгое сопоставление; где сопоставлять (до/после `LookPath`).
- Q8 (AC13/AC14): структурированный аудит — поля команды/exit/duration поверх `charmbracelet/log`; key=value vs JSON; ротация.
- Q9 (AC1/AC3/AC4): MCP-инструмент с богатым выводом — `structuredContent` + `content`; ограничения схемы (`additionalProperties:false`) в go-sdk v1.6.0.
- Q10 (AC11, edge): аргументная DoS — отсутствие встроенного лимита на число/размер аргументов в `exec.Command`.

---

## Q1. Безопасный запуск без shell (AC2)

### Найдено (факт → источник)
- `exec.Command`/`exec.CommandContext` запускают **именованную программу с явным списком аргументов**, а НЕ строку, интерпретируемую оболочкой. Аргументы передаются процессу как есть, без shell-парсинга. → https://pkg.go.dev/os/exec#Command , https://pkg.go.dev/os/exec#CommandContext
- Каноническая мера против OS Command Injection (CWE-78): «identify any function that invokes a command shell using a single string, and replace it with a function that requires individual arguments» — то есть **не запускать через shell**, передавать аргументы массивом. → https://cwe.mitre.org/data/definitions/78.html
- PATH-резолвинг: «LookPath searches for an executable named file in the current path … If file contains a slash, it is tried directly and the default path is not consulted. Otherwise, on success the result is an absolute path.» `exec.Command` сам зовёт `LookPath`, если в имени нет слэша. → https://pkg.go.dev/os/exec#LookPath
- Защита от относительного пути (Go 1.19+): «as of Go 1.19, this package will not resolve a program using an implicit or explicit path entry relative to the current directory … these functions return an error err satisfying `errors.Is(err, exec.ErrDot)`.» → https://pkg.go.dev/os/exec#hdr-Executables_in_the_current_directory

### Варианты
- **A: `exec.CommandContext(ctx, bin, args...)` без shell** — плюсы: метасимволы (`;`, `|`, `$()`, `&&`, `` ` ``) попадают в процесс как литеральные аргументы, shell-инъекция невозможна (закрывает AC2); stdlib. Минусы: нет «удобства» shell-пайпов (но это и не нужно по spec). → https://pkg.go.dev/os/exec#CommandContext
- **B: `sh -c <строка>`** — плюсы: гибкость shell. Минусы: **прямое нарушение AC2 и baseline §3** — любой метасимвол = инъекция (CWE-78). Отвергнут. → https://cwe.mitre.org/data/definitions/78.html

### Рекомендация
Вариант **A**. Дополнительно: явно отвергать имя бинаря, дающее `exec.ErrDot` (относительный путь), либо требовать абсолютный путь/чистое имя — это снимает риск «подмены бинаря из cwd». Деталь (запрещать ли вообще относительные имена) — решение architect; факт-механизм (`ErrDot`) подтверждён.

---

## Q2. Таймаут и убийство процесса + потомков (AC5/AC6) — РАЗВИЛКА → ADR-001

### Найдено (факт → источник)
- `CommandContext` ставит Cancel = Kill **только головного процесса**: «CommandContext sets the command's Cancel function to invoke the Kill method on its Process, and leaves its WaitDelay unset.» — про потомков ничего, `Process.Kill()` шлёт сигнал одному PID. → https://pkg.go.dev/os/exec#CommandContext
- Следствие: потомки осиротевают (PPID=1). Подтверждение из практики (несколько независимых разборов): «Once you kill the parent process those child processes become orphan and get a PPID=1 … Go is using kill(2) to send a KILL signal to the PID of the sh process, but not the watch process, turning it into an orphan.» → https://medium.com/@felixge/killing-a-child-process-and-all-of-its-children-in-go-54079af94773 , https://sigmoid.at/post/2023/08/kill_process_descendants_golang/
- Решение — process group: `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` создаёт **новую группу процессов**; затем `syscall.Kill(-cmd.Process.Pid, SIGKILL)` (отрицательный PID = группа) убивает всё дерево. `kill(2)` поддерживает рассылку сигнала группе через отрицательный PGID. → https://medium.com/@felixge/killing-a-child-process-and-all-of-its-children-in-go-54079af94773 , https://pkg.go.dev/syscall#SysProcAttr
- `Setpgid`/`Pgid` есть **и на Linux, и на Darwin** (portable Unix-механизм). `Pdeathsig` (сигнал ребёнку при смерти родителя) — **только Linux**, на Darwin отсутствует. → https://pkg.go.dev/syscall?GOOS=linux#SysProcAttr , https://go.dev/src/syscall/exec_linux.go , https://groups.google.com/g/golang-codereviews/c/jSRC-H5nZ5c
- `Cmd.Cancel` (Go 1.20+) можно переопределить: «If Cancel is non-nil, the command must have been created with CommandContext and Cancel will be called when the command's Context is done … Typically a custom Cancel will send a signal to the command's Process». → https://pkg.go.dev/os/exec#Cmd.Cancel
- `Cmd.WaitDelay` (Go 1.20+): «If WaitDelay is non-zero, it bounds the time spent waiting on two sources of unexpected delay in Wait: a child process that fails to exit after the associated Context is canceled, and a child process that exits but leaves its I/O pipes unclosed … If the child process has failed to exit … it will be terminated using os.Process.Kill. Then, if the I/O pipes … are still open, those pipes are closed». При WaitDelay=0 «I/O pipes will be read until EOF, which might not occur until orphaned subprocesses … have also closed their descriptors» — то есть **долгоживущий потомок может подвесить Wait** даже после kill головы. → https://pkg.go.dev/os/exec#Cmd.WaitDelay
- `Cancel`/`WaitDelay` добавлены в **Go 1.20**: «The new Cmd fields Cancel and WaitDelay specify the behavior of the Cmd when its associated Context is canceled or its process exits with I/O pipes still held open by a child process.» (доступны в Go 1.25 проекта). → https://go.dev/doc/go1.20

### Варианты
- **A: только `exec.CommandContext`** — плюсы: ноль кода. Минусы: убивает лишь голову → осиротевшие потомки, **AC6 не выполняется**; долгий потомок с открытым пайпом подвешивает Wait (WaitDelay=0). Отвергнут. → https://pkg.go.dev/os/exec#CommandContext
- **B: `Setpgid:true` + custom `Cmd.Cancel` шлёт `syscall.Kill(-pgid, SIGKILL)` + ненулевой `Cmd.WaitDelay`** — плюсы: убивает **всю группу** (закрывает AC6); WaitDelay страхует от зависшего пайпа/потомка; всё на stdlib (`os/exec`,`syscall`); работает на Linux и Darwin (`Setpgid` portable). Минусы: SIGKILL без graceful — процесс не успеет завершиться чисто (для НЕинтерактивных команд приемлемо); нужен build-tag/абстракция, т.к. `syscall.SysProcAttr` платформенно-зависим (Linux+darwin поля совпадают, но Windows — другой пакет; Windows вне scope, поэтому достаточно `//go:build unix`). → https://pkg.go.dev/os/exec#Cmd.Cancel , https://pkg.go.dev/os/exec#Cmd.WaitDelay , https://pkg.go.dev/syscall#SysProcAttr
- **C: `Setpgid:true` + Cancel шлёт сначала `SIGTERM` группе, затем `SIGKILL` по `WaitDelay`** — плюсы: даёт процессу шанс на чистое завершение перед kill; всё stdlib. Минусы: сложнее (graceful-период), а выигрыш для НЕинтерактивных stateless-команд (интерактив/PTY в Out of Scope) невелик; больше edge-case в тестах. → https://pkg.go.dev/os/exec#Cmd.Cancel

### Рекомендация
Базово **B** (минимум, закрывает AC6 и страхует пайпы), с возможностью эволюции в **C**, если security/architect захотят graceful-период. Развилка значима → **ADR-001**. На darwin отсутствие `Pdeathsig` НЕ критично: основная гарантия — kill группы по `-pgid` при отмене/таймауте, а `Pdeathsig` был бы лишь подстраховкой при внезапной смерти демона (демон в контейнере, baseline §6).

---

## Q3. Лимит размера вывода + дедлок пайпов (AC11)

### Найдено (факт → источник)
- `io.LimitReader(r, n)` «returns a Reader that reads from r but stops with EOF after n bytes»; реализация `*io.LimitedReader` с полем `N` (остаток). → https://pkg.go.dev/io#LimitReader , https://pkg.go.dev/io#LimitedReader
- Дедлок пайпов: для `StdoutPipe`/`StderrPipe` — «Wait will close the pipe after seeing the command exit … It is thus incorrect to call Wait before all reads from the pipe have completed. For the same reason, it is incorrect to call Cmd.Run when using StdoutPipe.» То есть читать пайпы надо **конкурентно** (в горутинах) до `Wait`. → https://pkg.go.dev/os/exec#Cmd.StdoutPipe , https://pkg.go.dev/os/exec#Cmd.StderrPipe
- Альтернатива пайпам: присвоить `cmd.Stdout`/`cmd.Stderr` своему `io.Writer` (например, ограниченному буферу). `Output`/`CombinedOutput` буферизуют внутри, но **не дают лимита** и собирают всё в память → не годятся против OOM «болтливой» команды. → https://pkg.go.dev/os/exec#Cmd.Output

### Варианты
- **A: `cmd.Stdout = &cappedWriter{limit:N}`** (свой `io.Writer`, который пишет до N байт, дальше отбрасывает и взводит флаг truncated) — плюсы: жёсткий потолок памяти; SDK/exec сам пишет в Writer, конкурентность обеспечивается рантаймом exec (отдельные горутины копирования внутри `Cmd`); фиксация `*_truncated` тривиальна; stdlib. Минусы: нужен небольшой тип-обёртка (не зависимость, ~20 строк). → https://pkg.go.dev/os/exec#Cmd (поля Stdout/Stderr)
- **B: `StdoutPipe`+`io.LimitReader`+горутины** — плюсы: явный контроль; `LimitReader` останавливает на N. Минусы: после достижения лимита надо **продолжать дренировать** пайп (иначе процесс заблокируется на write) — `LimitReader` сам EOF-нет, придётся отдельно `io.Copy(io.Discard, ...)`; больше ручной синхронизации и риск дедлока при неверном порядке Wait/read. → https://pkg.go.dev/io#LimitReader , https://pkg.go.dev/os/exec#Cmd.StdoutPipe
- **C: `CombinedOutput`/`Output`** — плюсы: просто. Минусы: **нет лимита**, всё в память → нарушает AC11 (OOM). Отвергнут. → https://pkg.go.dev/os/exec#Cmd.Output

### Рекомендация
Вариант **A** (capped `io.Writer` на `cmd.Stdout`/`cmd.Stderr`): даёт жёсткий лимит памяти, простую фиксацию `stdout_truncated`/`stderr_truncated`, и снимает класс дедлоков пайпов (exec сам копирует в Writer в своих горутинах). Важно: **обёртка должна дренировать остаток** (отбрасывать сверх лимита, не возвращая ошибку Write), чтобы не подвесить процесс. Деталь реализации обёртки — за architect/developer; механизм подтверждён.

---

## Q4. Ограничение окружения (AC10)

### Найдено (факт → источник)
- `Cmd.Env`: «Env specifies the environment of the process. Each entry is of the form "key=value". **If Env is nil, the new process uses the current process's environment.**» → https://pkg.go.dev/os/exec#Cmd
- Следствие: оставить `Env=nil` = слепое наследование всего окружения демона (нарушает принцип AC10). Нужен **явный whitelist**: `cmd.Env = []string{"PATH=...", ...}`.
- `LookPath`/`exec.Command` используют PATH для резолва имени бинаря → PATH в whitelist **обязателен**, иначе чистое имя команды не разрешится. → https://pkg.go.dev/os/exec#LookPath
- Риск наследования опасных переменных (обоснование whitelist): динамический загрузчик и shell реагируют на `LD_PRELOAD`/`LD_LIBRARY_PATH` (Linux), `DYLD_INSERT_LIBRARIES` (macOS), `IFS` — классические векторы перехвата исполнения/инъекции. → https://man7.org/linux/man-pages/man8/ld.so.8.html (раздел про `LD_PRELOAD`/secure-execution) ; https://cwe.mitre.org/data/definitions/426.html (Untrusted Search Path) ; https://cwe.mitre.org/data/definitions/427.html (Uncontrolled Search Path Element)

### Варианты
- **A: явный whitelist `cmd.Env`** (минимум: `PATH`; опц. `HOME`, `LANG`, `TZ`) — плюсы: предсказуемость, не наследуются `LD_PRELOAD`/`DYLD_*`/`IFS`; stdlib. Минусы: некоторые команды могут требовать спец-переменных → состав whitelist надо документировать (но это и есть требование AC10). → https://pkg.go.dev/os/exec#Cmd
- **B: `Env=nil` (наследовать всё)** — плюсы: ноль кода. Минусы: нарушает AC10; тянет потенциально опасные `LD_*`/`DYLD_*`. Отвергнут. → https://pkg.go.dev/os/exec#Cmd

### Рекомендация
Вариант **A**: `cmd.Env` = явный whitelist, **PATH обязателен** (для `LookPath`). Точный состав (`HOME`/`LANG`/`TZ`?) — политическое решение architect/security (spec прямо оставляет это им); research фиксирует: PATH обязателен, `LD_PRELOAD`/`LD_LIBRARY_PATH`/`DYLD_INSERT_LIBRARIES`/`IFS` **не должны** попадать в окружение.

---

## Q5. Рабочая директория (AC10)

### Найдено (факт → источник)
- `Cmd.Dir`: «Dir specifies the working directory of the command. **If Dir is the empty string, Run runs the command in the calling process's current directory.**» На Unix Dir также определяет `PWD` ребёнка. → https://pkg.go.dev/os/exec#Cmd
- Следствие: пустой `Dir` = «случайный» cwd процесса демона → нарушает требование «предсказуемого дефолта» (AC10). Нужен явный безопасный дефолт из конфига.
- Валидация каталога: `os.Stat` + `FileInfo.IsDir()` — проверить, что путь существует и является директорией. → https://pkg.go.dev/os#Stat , https://pkg.go.dev/io/fs#FileInfo

### Варианты
- **A: `cmd.Dir = cfg.WorkDir` (дефолт из конфига), при заданном `cwd` — валидировать `os.Stat`+`IsDir` до запуска** — плюсы: предсказуемость; невалидный `cwd` → `isError` без запуска (AC10); stdlib. Минусы: нужно решить безопасный дефолт (напр. выделенный рабочий каталог, не `/`). → https://pkg.go.dev/os/exec#Cmd , https://pkg.go.dev/os#Stat
- **B: оставить `Dir=""`** — плюсы: просто. Минусы: непредсказуемый cwd, нарушает AC10. Отвергнут. → https://pkg.go.dev/os/exec#Cmd

### Рекомендация
Вариант **A**. Безопасный дефолт `cwd` — конфиг-поле (architect задаёт значение; spec оставляет дефолт ему). Опционально architect может добавить ограничение `cwd` поддеревом (защита от выхода вверх) — это усиление, не требуется AC напрямую.

---

## Q6. Детекция root (AC9)

### Найдено (факт → источник)
- `os.Geteuid()` «returns the numeric effective user id of the caller. On Windows, it returns -1.» Доступна на Linux и Darwin (POSIX), без ошибки. → https://pkg.go.dev/os#Geteuid
- `os.Getuid()` — real UID, аналогично; «On Windows, it returns -1.» → https://pkg.go.dev/os#Getuid
- baseline §3: «Демон работает НЕ от root … при необходимости порта <1024 — Linux capabilities (`CAP_NET_BIND_SERVICE`), а не setuid root.» → `.claude/reference/SECURITY-BASELINE.ru.md` §3

### Варианты
- **A: `os.Geteuid()==0` → WARN-аудит при каждом вызове** (как требует AC9) — плюсы: кросс-платформенно (Linux+darwin), stdlib, без зависимостей; ровно то, что предписывает AC9. Минусы: только детекция/предупреждение, не запрет (запрет — отдельное политическое решение). → https://pkg.go.dev/os#Geteuid
- **B: жёсткий отказ исполнять при euid==0** — плюсы: строже. Минусы: AC9 фиксирует обязательным лишь факт детекции+WARN; полный отказ — политическое решение architect/security (spec явно оставляет открытым). → spec AC9

### Рекомендация
Вариант **A** как обязательный минимум (детекция `os.Geteuid()==0` + WARN-запись через существующий аудит при каждом `execute_command`). Политику «WARN vs полный отказ» оставить architect/security (зафиксировано в spec Open Questions). Согласуется с baseline §3 «не от root» — детекция дополняет, не заменяет правильную раскладку сервиса (это `service-install`/`distribution`).

---

## Q7. Allowlist команд (AC7)

### Найдено (факт → источник)
- baseline §3: «Опциональный allowlist команд: строгое сопоставление (не regex), по умолчанию выключен». → `.claude/reference/SECURITY-BASELINE.ru.md` §3
- AC7: точное совпадение, НЕ regex и НЕ префикс; выкл → любая команда, вкл → только из списка. → spec AC7
- `exec.LookPath` превращает чистое имя в **абсолютный путь** (резолв по PATH); имя со слэшем используется напрямую. → https://pkg.go.dev/os/exec#LookPath . Это создаёт развилку «сопоставлять до или после резолва».

### Варианты
- **A: сопоставлять по исходному `command` (до `LookPath`), точное строковое равенство** — плюсы: предсказуемо для админа (в списке ровно то, что прислал клиент); просто. Минусы: `ls` и `/bin/ls` — разные строки; клиент может обойти список, прислав другой алиас/путь к тому же бинарю (но «строгое точное» — это и есть контракт AC7). → spec AC7
- **B: резолвить `LookPath` сначала, сопоставлять абсолютный путь** — плюсы: список привязан к реальному бинарю, устойчив к алиасам имени. Минусы: символьные ссылки/несколько путей к одному бинарю всё равно могут расходиться; админу труднее предсказать (надо знать резолв PATH); резолв до проверки = чуть больше работы до отказа. → https://pkg.go.dev/os/exec#LookPath
- **C: гибрид — допускать в списке и чистые имена, и абсолютные пути; матчить строго по тому, что прислано** — плюсы: гибкость. Минусы: неоднозначность семантики «строгого» сравнения, сложнее тестировать; риск разночтений с AC7 «точное совпадение». → spec AC7

### Рекомендация
Вариант **A** как дефолт (строгое точное равенство по присланному `command`, до `LookPath`) — буквально соответствует AC7 «точное совпадение, не regex, не префикс» и предсказуем для администратора. **Развилку «до/после резолва» стоит зафиксировать явно** — это значимое решение безопасности. Можно вынести в ADR при желании architect; research рекомендует A и помечает, что B усиливает защиту от алиасов ценой предсказуемости. (Не оформляю отдельным ADR, т.к. AC7 уже однозначно предписывает строгое точное сравнение; место сопоставления — деталь, оставленная architect.)

---

## Q8. Структурированный аудит-формат (AC13/AC14) — РАЗВИЛКА → ADR-002

### Найдено (факт → источник / код)
- Существующий аудит: `internal/server/audit.go` — `AuditRecord{TS,Fingerprint,RemoteAddr,Result,Reason,Tool}`, запись через `charmbracelet/log` методами `logger.Info("MCP", "fp",..., "remote",..., "tool",..., "result","ok")` и т.п. (key/value пары). Транспорт (tls-transport) и mcp-server **уже пишут аудит в формате key=value** через дефолтный TextFormatter. → `internal/server/audit.go` (читано), `internal/mcp/audit.go` (читано)
- `charmbracelet/log` имеет 3 форматтера: «TextFormatter (default), JSONFormatter, LogfmtFormatter». TextFormatter — «leveled structured human readable logger», styling **только** в TextFormatter и **отключается, если вывод не TTY**. → https://pkg.go.dev/github.com/charmbracelet/log , https://github.com/charmbracelet/log/blob/main/README.md
- Все уровни принимают key/value пары (structured logging) **во всех форматтерах** — то есть смена форматтера НЕ меняет код вызовов `logger.Info(..., k, v)`. → https://github.com/charmbracelet/log/blob/main/README.md
- Ротация: «charmbracelet/log does NOT support log rotation natively. It writes to an io.Writer» (`New(w io.Writer)`). → https://pkg.go.dev/github.com/charmbracelet/log
- **baseline §4 (точная буква):** «Аудит-лог КАЖДОГО действия: timestamp, fingerprint ключа (не сам ключ), команда+аргументы, exit code, длительность, удалённый адрес. **Структурно (JSON), с ротацией.**» → `.claude/reference/SECURITY-BASELINE.ru.md` §4 ; STACK: «Логи: системный журнал (journald/syslog) + ротация при файловом выводе». → `.claude/reference/STACK.ru.md`

> **ОТКЛОНЕНИЕ ОТ БУКВЫ baseline §4 (важно, требует утверждения security/architect).** Baseline §4
> буквально предписывает «Структурно (JSON), с ротацией». Рекомендация research ниже —
> `LogfmtFormatter` (структурный key=value), а **не** JSON. Это **сознательное отклонение от буквы**
> baseline ради консистентности с уже существующим аудитом продукта: транспорт (tls-transport) и
> mcp-server уже пишут аудит в key=value через `charmbracelet/log`; переход на JSON **фрагментирует
> формат аудита** (часть записей key=value, часть JSON) и нарушает AC14 «не ломать формат
> не-`execute_command` записей». logfmt при этом **тоже структурный и машиночитаемый** (logfmt —
> стандартный парсимый формат), т.е. дух требования baseline §4 «структурно» сохраняется, меняется
> лишь конкретная сериализация (logfmt вместо JSON). Согласно red line 4 (безопасность не
> опциональна, отступления — через эскалацию и фиксацию в `threat-model.md`): **окончательное
> принятие этого отклонения — за ролью security (запись в `threat-model.md` с риском/обоснованием/
> смягчением) и architect.** Research лишь поднимает вопрос и рекомендует; решение принимает
> security/architect, не research. → `.claude/reference/SECURITY-BASELINE.ru.md` §4 + §Эскалация ;
> https://pkg.go.dev/github.com/charmbracelet/log

### Подвопрос 8a: формат — добавить поля команды/exit/duration, не сломав парсинг

`AuditRecord` сейчас имеет фиксированный набор полей; команда/args/exit/duration в нём ОТСУТСТВУЮТ. Их надо куда-то поместить. Развилка:

- **A: остаться на дефолтном TextFormatter** — плюсы: ноль изменений в инициализации логгера. Минусы: TextFormatter human-readable со styling при TTY → НЕ гарантирует строгий машинный парсинг (AC14 «парсится как структурированная запись»); хрупко. → https://pkg.go.dev/github.com/charmbracelet/log
- **B: переключить логгер на `LogfmtFormatter` (строгий key=value)** — плюсы: тот же key=value-стиль, что уже в коде/транспорте и в ux-spec; **строго парсимый logfmt** (структурный, машиночитаемый); без новых зависимостей (форматтер встроен в уже вендоренный `charmbracelet/log`); вызовы `logger.Info(..., k, v)` не меняются; сохраняет единый формат аудита продукта. Минусы: **отклонение от буквы baseline §4 «JSON»** (см. блок ОТКЛОНЕНИЕ выше — требует утверждения security/architect); визуально менее «красиво» для человека в консоли, чем цветной TextFormatter (но для аудита машиночитаемость важнее). → https://pkg.go.dev/github.com/charmbracelet/log
- **C: `JSONFormatter`** — плюсы: **буквально соответствует baseline §4 «JSON»**; максимально машиночитаемо. Минусы: меняет формат **ВСЕХ** записей (auth/deny/rate), а AC14 требует «не ломать формат не-`execute_command` записей» и единый канал — глобальная смена на JSON затрагивает уже зашлюзованный mcp-server/транспорт и фрагментирует исторический формат; больше риск регрессий в существующих тестах формата. → https://pkg.go.dev/github.com/charmbracelet/log , spec AC14

Поля команды/exit/duration: их надо добавить в `AuditRecord` (новые опциональные поля, логируемые только когда заполнены — по аналогии с тем, как `tool=` пишется только при `Tool!=""`), ЛИБО передавать как доп. key/value прямо в вызове. Любой подход совместим с logfmt/JSON. AC13 требует: timestamp(UTC), fingerprint, имя инструмента, команда+аргументы, exit code, duration, remote, result. **Без секретов** (AC15) — команда+args НЕ содержат ключ, fingerprint вместо ключа. → spec AC13/AC15 ; `internal/server/audit.go`

### Подвопрос 8b: ротация

- Факт: `charmbracelet/log` не умеет ротацию сам (пишет в `io.Writer`). → https://pkg.go.dev/github.com/charmbracelet/log
- Популярный Go-способ ротации в коде — `gopkg.in/natefinch/lumberjack.v2` (rolling `io.Writer`), НО это **новая внешняя зависимость** → нежелательна (вендоринг, offline Docker). → https://pkg.go.dev/gopkg.in/natefinch/lumberjack.v2
- Альтернатива без зависимостей: писать аудит в **stderr** (как сейчас) и отдать ротацию **системе** — journald (systemd) или logrotate. STACK прямо называет дефолт «системный журнал (journald/syslog) + ротация при файловом выводе». → `.claude/reference/STACK.ru.md`

### Варианты (сводно по Q8)
- **Формат: B (LogfmtFormatter)** — рекомендован (см. выше); это **отклонение от буквы baseline §4 «JSON»**, требующее утверждения security/architect (см. блок ОТКЛОНЕНИЕ). Если security настаивает на букве «JSON» — вариант C, ценой фрагментации формата аудита.
- **Ротация: системная (journald/logrotate), вывод в stderr** — плюсы: ноль новых зависимостей, соответствует STACK и baseline §4 «ротация при файловом выводе»; для контейнерного демона (baseline §6) stdout/stderr — естественный канал, ротация — забота рантайма/journald. Минусы: при чисто файловом выводе без journald нужен внешний `logrotate` (конфиг дистрибуции, задача `distribution`). → https://www.freedesktop.org/software/systemd/man/latest/journald.conf.html , https://man7.org/linux/man-pages/man8/logrotate.8.html

### Рекомендация
- **Формат:** рекомендуется `LogfmtFormatter` (строгий, структурный key=value) ради консистентности с уже существующим key=value аудитом транспорта/mcp-server; добавить поля команды/exit/duration как опциональные поля `AuditRecord`, логируемые только для `execute_command` (не ломая не-MCP записи). Закрывает AC14 «машиночитаемость» без новых зависимостей. **ВНИМАНИЕ:** это отклонение от буквы baseline §4 «JSON» — финальное решение и фиксация в `threat-model.md` за security/architect (red line 4). Если security выбирает строгий «JSON» — вариант C. → **ADR-002**.
- **Ротация:** оставить системной (journald/logrotate), вывод аудита в stderr; в коде ротацию НЕ реализовывать (избегаем `lumberjack`-зависимости). Требование ротации baseline §4 выполняется на уровне дистрибуции/рантайма (задача `distribution`). → **ADR-002** (раздел ротации).

> Примечание: смена форматтера затрагивает инициализацию логгера в транспортном слое (вне scope command-exec по коду). Это **точка координации с architect** — возможно, решение о форматтере правильнее провести как сквозное (а не локально в command-exec). research фиксирует развилку и рекомендацию; окончательно — architect/security.

---

## Q9. MCP-инструмент с богатым выводом (AC1/AC3/AC4) — go-sdk v1.6.0

### Найдено (факт → источник / код)
- `AddTool[In, Out any](s *Server, t *Tool, h ToolHandlerFor[In, Out])`. Если `InputSchema` nil — выводится из `In`; если `OutputSchema` nil и `Out != any` — выводится из `Out`. «The In type argument must be a map or a struct, so that its inferred JSON Schema has type "object"». Описания свойств — из тега `jsonschema`. Внутри — `github.com/google/jsonschema-go` (вендорится транзитивно SDK). → https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp#AddTool (v1.6.0)
- `additionalProperties:false` входит в выведенную схему **по умолчанию для struct-типов**. Прямое подтверждение из документации движка инференса схем, который использует SDK (`.../go-sdk/jsonschema`, раздел Inference): «Structs have schema type "object", and **disallow additionalProperties**. Their properties are derived from exported struct fields, using the struct field JSON name.» В выведенной схеме это представлено как `"additionalProperties": {"not": {}}` (эквивалент `false` — отвергает любые поля сверх объявленных; см. пример с `type Player struct{…}` в документации). → https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/jsonschema (раздел Inference + пример)
- Подтверждение, что это поведение действует именно через `mcp.AddTool` (а не только в low-level API): «mcp.AddTool generates JSON schemas from Go structs via jsonschema-go, **which sets additionalProperties:false on all object types**» — из-за чего вход с лишними полями отвергается на валидации схемы ещё до хэндлера. → https://github.com/modelcontextprotocol/go-sdk/issues/892 (Issue фиксирует это как текущее дефолтное поведение `mcp.AddTool`)
- **ВАЖНО для architect/developer при реализации:** автоматическое закрытие AC3 (лишнее поле → ошибка валидации без ручного патча схемы) держится именно на этом дефолте. Источники выше подтверждают дефолт `additionalProperties:false`; при реализации это **обязательно закрепить тестом** (вход с лишним полем → `isError`), т.к. дефолт настраиваемый и может меняться в будущих версиях SDK (Issue #892 — открытый запрос на конфигурируемость `additionalProperties`). → https://github.com/modelcontextprotocol/go-sdk/issues/892
- `ToolHandlerFor[In,Out] = func(context.Context, *CallToolRequest, In) (*CallToolResult, Out, error)`; «Inputs are automatically validated according to the inferred schema. Outputs are automatically marshaled to the output schema.» → https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp#ToolHandlerFor
- `CallToolResult{ Content []Content; StructuredContent any; IsError bool }` — `Content` (например `&TextContent{Text:...}`) для текстового блока + `StructuredContent`/возврат `Out` для структурированного результата. → https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp#CallToolResult
- Подтверждение паттерна в нашем коде: `pingHandler`/`serverInfoHandler` возвращают `(*sdkmcp.CallToolResult{Content:[...TextContent]}, OutStruct, nil)`; точка расширения — `sdkmcp.AddTool(s, tool, withAudit(name, handler, audit))` в `internal/mcp/server.go`. → `internal/mcp/tools.go`, `internal/mcp/server.go` (читано)
- Ошибка валидации входа должна быть `isError:true` (НЕ protocol error) — соответствует AC17 и тому, как SDK различает ошибку инструмента (`CallToolResult.IsError`) и протокольную ошибку (возврат `error`). → https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp#CallToolResult ; spec AC17

### Варианты
- **A: типизированные `In`/`Out` struct + `AddTool` (schema авто-выводится)** — плюсы: `additionalProperties:false` и валидация входа из коробки (AC3); `Out`-struct → `structuredContent` (AC4); единообразно с `ping`/`server_info`; точка расширения уже есть. Минусы: нужно аккуратно описать `Out` (stdout/stderr/exit_code/duration_ms/timed_out/*_truncated) тегами `json`/`jsonschema`; зависимость закрытия AC3 от дефолта SDK → закрепить тестом (см. выше). → https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp#AddTool
- **B: ручной `InputSchema`/`OutputSchema`** — плюсы: полный контроль над схемой; явная гарантия `additionalProperties:false` независимо от дефолта SDK. Минусы: дублирование, риск рассинхрона с Go-типом; в общем случае не нужно — авто-вывод покрывает требования. → https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp

### Рекомендация
Вариант **A**: `ExecInput{Command string; Args []string; TimeoutMs int; Cwd string}` (+ `jsonschema`-описания, `omitempty` для опц.) и `ExecOutput{Stdout, Stderr string; ExitCode, DurationMs int; TimedOut, StdoutTruncated, StderrTruncated bool}`; зарегистрировать через `AddTool(s, execTool(), withAudit("execute_command", execHandler, audit))` в существующей точке расширения. Богатый вывод: `Content` = текстовый блок (например, краткий stdout/итог) + `Out` = `ExecOutput` для `structuredContent`. Закрывает AC1/AC3/AC4 на уже имеющемся SDK v1.6.0, без новых зависимостей. Закрытие AC3 (отвержение лишнего поля) **закрепить тестом** — оно опирается на дефолт `additionalProperties:false`, который SDK может сделать конфигурируемым (Issue #892).

---

## Q10. Аргументная DoS — отсутствие встроенного лимита на args (AC11, edge)

### Найдено (факт → источник)
- `exec.Command(name string, arg ...string)` принимает `[]string` аргументов **без встроенного прикладного лимита** на их число или суммарный размер — пакет не ограничивает argv. → https://pkg.go.dev/os/exec#Command
- Ядро имеет жёсткий потолок `ARG_MAX` на суммарный размер argv+envp; при превышении `execve(2)` возвращает ошибку **`E2BIG`**: «E2BIG — The total number of bytes in the environment (envp) and argument list (argv) is too large». Это **защита уровня exec**, срабатывает лишь в момент запуска и НЕ является прикладной защитой от исчерпания памяти процесса демона до exec. → https://man7.org/linux/man-pages/man2/execve.2.html (RETURN VALUE/ERRORS: `E2BIG`, ARG_MAX)
- Следствие: до вызова `execve` строки аргументов уже находятся в памяти процесса демона (приняты из JSON-RPC запроса, разложены в `[]string`). Клиент может прислать тысячи аргументов или мегабайтные строки → рост памяти/argv на стороне демона ещё до того, как ядро отвергнет запуск (или запуск пройдёт, но с непредсказуемым ресурсопотреблением).

### Класс угрозы
**Аргументная DoS:** аутентифицированный (или, при дыре в auth — любой) клиент шлёт `execute_command` с очень большим числом аргументов и/или очень длинными строками аргументов. Без прикладного лимита это ведёт к исчерпанию памяти/argv процесса демона (или к непредсказуемому поведению на границе ARG_MAX). `E2BIG` от ядра — поздняя и грубая защита (только при exec, не покрывает память до exec, сообщение не прикладное). Класс соседствует с AC11 (лимиты вывода против OOM), но касается **входа**, а не вывода.

### Рекомендация
Рекомендовать architect ввести **конфигурируемые прикладные лимиты на вход**: `max_args` (максимум число элементов `args`) и `max_arg_len` (максимум длина одного аргумента и/или суммарная длина argv), проверяемые **до** запуска команды → при превышении `isError:true` без запуска (по аналогии с отказом при превышении жёсткого максимума таймаута, AC5). Конкретные числа НЕ задаю — это решение architect/security (как и другие числовые пороги: таймаут, лимиты вывода). Реализуемо на stdlib (проверка `len(args)`/`len(s)`), без новых зависимостей. → https://man7.org/linux/man-pages/man2/execve.2.html , https://pkg.go.dev/os/exec#Command

---

## Сводка рекомендаций для architect

| # | Вопрос (AC) | Рекомендация research | Источник-ключ | Новая зависимость? |
|---|---|---|---|---|
| Q1 | Запуск без shell (AC2) | `exec.CommandContext(ctx,bin,args...)`; отвергать `ErrDot`/относит. путь | pkg.go.dev/os/exec, CWE-78 | Нет (stdlib) |
| Q2 | Таймаут+kill потомков (AC5/AC6) | **ADR-001**: `Setpgid:true` + custom `Cmd.Cancel`→`Kill(-pgid,SIGKILL)` + ненулевой `WaitDelay` | os/exec#Cmd.Cancel/WaitDelay, syscall#SysProcAttr | Нет (os/exec, syscall) |
| Q3 | Лимит вывода + дедлок (AC11) | capped `io.Writer` на `cmd.Stdout/Stderr`, дренировать сверх лимита, флаги `*_truncated` | pkg.go.dev/io, os/exec#Cmd.StdoutPipe | Нет (stdlib) |
| Q4 | Окружение (AC10) | явный whitelist `cmd.Env`, **PATH обязателен**; не пускать `LD_PRELOAD/DYLD_*/IFS` | os/exec#Cmd, CWE-426/427 | Нет (stdlib) |
| Q5 | Рабочая директория (AC10) | `cmd.Dir`=безопасный дефолт из конфига; валидация `os.Stat`+`IsDir` при `cwd` | os/exec#Cmd, os#Stat | Нет (stdlib) |
| Q6 | Детекция root (AC9) | `os.Geteuid()==0`→WARN-аудит каждый вызов; «WARN vs отказ» — за architect | os#Geteuid | Нет (stdlib) |
| Q7 | Allowlist (AC7) | строгое точное равенство по присланному `command` (до `LookPath`); место резолва — за architect | spec AC7, os/exec#LookPath | Нет (stdlib) |
| Q8 | Аудит-формат+ротация (AC13/14) | **ADR-002**: `LogfmtFormatter` (key=value) — ОТКЛОНЕНИЕ от буквы baseline §4 «JSON», утверждает security/architect; новые опц. поля `AuditRecord`; ротация — системная (journald/logrotate), вывод в stderr | charmbracelet/log, baseline §4, STACK §Логи | Нет (форматтер встроен; БЕЗ lumberjack) |
| Q9 | MCP богатый вывод (AC1/3/4) | типизир. `ExecInput`/`ExecOutput` + `AddTool` (авто-схема, `additionalProperties:false`), `Content`+`structuredContent`; закрытие AC3 закрепить тестом | go-sdk v1.6.0#AddTool, go-sdk/jsonschema, Issue #892 | Нет (SDK уже есть) |
| Q10 | Аргументная DoS (AC11, edge) | конфигурируемые `max_args`/`max_arg_len`, проверка до запуска → `isError`; числа за architect | execve(2) E2BIG/ARG_MAX, os/exec#Command | Нет (stdlib) |

**Подтверждение по зависимостям:** все рекомендации реализуемы на **stdlib** (`os/exec`, `context`, `syscall`, `io`, `os`) + **уже вендоренных** `charmbracelet/log` и go-sdk v1.6.0 (с транзитивным `jsonschema-go`). **Новые внешние зависимости НЕ требуются.** Единственный кандидат, который мог бы появиться — `lumberjack` для ротации в коде — **сознательно отклонён** в пользу системной ротации (journald/logrotate), что устраняет потребность в новой зависимости и соответствует STACK.

**Кросс-платформенная заметка (Linux vs darwin):** `Setpgid`/`Pgid` и `syscall.Kill(-pgid,...)` доступны на обеих платформах; `Pdeathsig` — только Linux (на darwin отсутствует, но не критичен — основная гарантия идёт через kill группы при отмене). `os.Geteuid()` работает на обеих. Платформенный код `syscall.SysProcAttr` — под `//go:build unix` (Windows вне scope).

---

## Открытые вопросы

- [ ] **Q8-deviation (red line 4):** рекомендация формата аудита `LogfmtFormatter` (key=value) — **сознательное отклонение от буквы baseline §4 «Структурно (JSON), с ротацией»**. Обоснование (консистентность с уже существующим key=value аудитом транспорта/mcp-server; logfmt структурен и машиночитаем; JSON фрагментирует формат аудита и нарушает AC14) изложено в Q8 и ADR-002. Согласно red line 4 окончательное **принятие отклонения — за security (запись в `threat-model.md`: риск + почему + смягчение) и architect**. Research лишь поднимает вопрос и рекомендует; не блокирует контракт, но требует явного решения следующего слоя.
- [ ] **Q8-coord:** смена форматтера логгера затрагивает транспортный слой (инициализацию логгера вне scope command-exec по коду). Требует согласования с architect: проводить как глобальное изменение или искать локальный путь (доп. поля только в записях `execute_command`). Зафиксировано в ADR-002 как точка координации. Не блокирует контракт.
- [ ] **Q9-impl-check:** автоматическое закрытие AC3 (лишнее поле → ошибка валидации) опирается на дефолт SDK `additionalProperties:false` (подтверждён источниками: go-sdk/jsonschema + Issue #892). Поскольку дефолт — предмет открытого запроса на конфигурируемость (Issue #892), **требует проверки при реализации (architect/developer)**: закрепить тестом «вход с лишним полем → `isError`», либо при необходимости задать схему явно (вариант B Q9). Не блокирует контракт.
- Остальные «политические» развилки (политика при root: WARN vs отказ; точный состав env-whitelist; числовой жёсткий максимум таймаута; числовые `max_args`/`max_arg_len` из Q10) уже относятся к проектным решениям следующего слоя (architect/security). Часть прямо зафиксирована в `spec.md`; research подтверждает факт-механизмы (детекция euid, whitelist `cmd.Env`, отказ при превышении максимума, проверка длины/числа args), числовые/политические значения — за architect/security. Это НЕ открытые research-вопросы (источники подтверждены).
