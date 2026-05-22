# command-exec — живая проверка в Docker (дирижёр)

Выполнена дирижёром после merge `feature/command-exec` в `develop` (мандат «проверять самому в
Docker», SECURITY-BASELINE §6). Дата: 2026-05-22.

## Среда

- Образ пересобран с develop-кодом: `docker build --target build -t raxd-build .` (go vet + build — ОК).
- Контейнер `raxd-mcp-demo`: `bind_addr: 0.0.0.0`, порт 7822, публикация `127.0.0.1:8443->7822`.
- Демон в контейнере работает от **root (euid==0)** — это намеренно демонстрирует root-WARN (AC9/SR-55).
- Ключ создан: fingerprint `435018a5e8d3`.

## Результаты end-to-end (curl `https://127.0.0.1:8443/mcp`, Bearer, `-k`)

| Проверка | Ожидание | Факт | Итог |
|---|---|---|---|
| `tools/list` | execute_command + ping + server_info | все три присутствуют | ✅ |
| `execute_command echo hello world` | exit_code 0, stdout, не isError, 7 полей | `exit=0 ... stdout="hello world\n"`, structuredContent 7 полей, isError опущен | ✅ |
| `execute_command sh -c 'exit 42'` | ненулевой exit НЕ ошибка | `exit_code=42`, isError опущен (нормальный результат) | ✅ |
| `execute_command nosuchbinary12345` | isError:true | `{"content":[{"text":"command not found"}],"isError":true}` | ✅ |
| `execute_command sleep 10, timeout_ms=300` | timed_out:true, процесс убит | `timed_out:true exit_code=-1 duration_ms=305` (убит по таймауту) | ✅ |
| `execute_command ./evil` (относит. путь) | isError (ErrDot) | `command not found`, isError:true | ✅ |
| тело ключа в логах | отсутствует | grep `rax_live_...` = 0 | ✅ |

## Аудит (фактический рендер из stderr контейнера)

- Успех: `level=info msg=MCP fp=435018a5e8d3 remote=172.17.0.1:.. tool=execute_command result=ok
  command=echo args=[hello,world] exit_code=0 duration=1.27ms timed_out=false`
- Ненулевой exit: `... result=ok command=sh args="[-c,exit 42]" exit_code=42 timed_out=false`
- Таймаут: `... result=ok command=sleep args=[10] exit_code=-1 duration=305ms timed_out=true`
- Не найден / ErrDot: `level=warn msg=FAIL ... reason=not-found command=nosuchbinary12345 args=[]`
  и `... reason=not-found command=./evil args=[]`
- root-WARN (на каждый вызов, т.к. euid==0): `level=warn msg=WARN ... reason="running-as-root: raxd
  executing commands as root (euid==0); ensure raxd runs as non-root" command=.. args=..`

Разграничение поле→рендер подтверждено вживую: Result success→`msg=MCP result=ok`+поля; fail→
`msg=FAIL reason=`; warn(root)→`msg=WARN reason=running-as-root`. Тела ключа нет нигде.

## Примечания

- root-WARN появляется на КАЖДЫЙ вызов, потому что демо-контейнер запускает raxd от root — это и есть
  живое подтверждение детекции euid==0 (AC9/SR-55). В проде раскладка не-root + контейнер; для жёсткого
  запрета — `exec.deny_root: true`.
- Относительный путь `./evil` отвергнут (ErrDot) с клиентским текстом `command not found` и аудитом
  `reason=not-found` — соответствует docs (один текст для not-found и ErrDot/bad-cwd, различие в аудите).

**Вывод: execute_command работает end-to-end в Docker; безопасность (no-shell, таймаут+kill, ErrDot,
аудит с полями и без секретов, root-WARN) подтверждена вживую. command-exec закрыт.**
