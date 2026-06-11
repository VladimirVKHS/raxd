# Impl Notes: fix/cmdexec-killgroup-linux

## Что реализовано

- `internal/cmdexec/sysproc_linux.go` — новый файл: Linux-специфичная логика reap orphan-зомби
  для обеспечения SR-47 (kill всего дерева, осиротевших не остаётся).
  Функции: `setSelfSubreaper()`, `reapGroupOrphans(pgid int)`.
- `internal/cmdexec/sysproc_unix_other.go` — новый файл: no-op заглушки `setSelfSubreaper` и
  `reapGroupOrphans` для Unix-систем кроме Linux (macOS, *BSD).
- `internal/cmdexec/sysproc_unix.go` — рефакторинг: `applyProcessGroup` теперь вызывает
  `setSelfSubreaper()` (платформо-специфичная реализация), `killGroup` и `waitDelay` без изменений.
- `internal/cmdexec/sysproc_stub.go` — новый файл: заглушки для `!unix` (Windows, вне scope).
- `internal/cmdexec/exec.go` — минимальная правка: после `cmd.Wait()` добавлен вызов
  `reapGroupOrphans(cmd.Process.Pid)` для reap orphan-зомби группы.

## Корневая причина

На Linux (CI golang:1.25) после `killGroup(-pgid, SIGKILL)`:
1. sh (головной процесс) получает SIGKILL и умирает → `cmd.Process.Wait()` (waitpid) возвращает.
2. Дочерний `sleep` (запущен через `sh -c "sleep 120 &"`) тоже получает SIGKILL, но при смерти sh
   становится **orphan** — усыновляется глобальным init (PID 1).
3. Init делает `waitpid(sleep)` асинхронно. Пока это не произошло, `sleep` — **зомби**.
4. Go 1.23+ использует `pidfd_open` в `os.FindProcess`. Для зомби-процессов `pidfd_open` возвращает
   **SUCCESS** (не ESRCH).
5. `proc.Signal(syscall.Signal(0))` через pidfd → SUCCESS для зомби → тест считает sleep живым.
6. На нагруженном CI init медленнее reap-ает orphan-ов → тест стабильно падает в течение 500ms.

На macOS (Docker Desktop) проблема не воспроизводится: Docker Desktop использует быстрый init
(tini/docker-init), который reap-ает orphan-ов за микросекунды, и Go не использует pidfd.

Подтверждение диагнозом через C-программу: `pidfd_open(zombie_pid)` возвращает SUCCESS на Linux,
только после `waitpid(zombie_pid)` возвращает ESRCH.

## Фикс (два шага)

**Шаг 1. `prctl(PR_SET_CHILD_SUBREAPER, 1)` в `applyProcessGroup`** (до `cmd.Start()`):
- Делает текущий процесс (raxd / go test) sub-reaper.
- При смерти sh, orphan-потомки группы усыновляются нашим процессом, а не init.
- Реализовано в `sysproc_linux.go: setSelfSubreaper()`.

**Шаг 2. `waitpid(-pgid, WNOHANG)` в цикле после `cmd.Wait()`**:
- `reapGroupOrphans(pgid)` reap-ает усыновлённых orphan-ов группы.
- После reap `pidfd_open(pid)` возвращает ESRCH → `Signal(0)` возвращает `ErrProcessDone`.
- Таймаут 200ms (SIGKILL доставляется за < 1ms, 200ms — запас для CI).
- Реализовано в `sysproc_linux.go: reapGroupOrphans()`, вызов в `exec.go`.

## Отклонения/эскалации

Нет. Фикс строго соответствует SR-47 (kill всего дерева, осиротевших не остаётся).

Не нарушены:
- SR-54: `SysProcAttr.Credential` не добавлялся — наследование uid/gid сохранено.
- SR-43: exec.Command без shell — не затронуто.
- Кроссплатформенность darwin+linux: Linux-специфичный код отделён в `sysproc_linux.go` (build tag
  `linux`); для darwin — no-op заглушки в `sysproc_unix_other.go`.

## Тесты

Затронутый тест: `TestContextCancelKillsChildren` в `internal/cmdexec/exec_qa_test.go`.
Тест НЕ изменялся (QA-артефакт). Исправлен продуктовый код.

Команда запуска:
```
docker build --target test -t raxd-test . && docker run --rm raxd-test
```

Или только cmdexec:
```
docker run --rm raxd-test sh -c "go test -v -count=1 ./internal/cmdexec/..."
```

Стабильность: `TestContextCancelKillsChildren` прошёл 20/20 раз подряд.
Race-детектор: `CGO_ENABLED=1 go test -race -count=1 ./internal/cmdexec/...` — чист.
Полный suite: все 11 пакетов `ok`.

## Безопасность

- Команды: `exec.Command(bin, args...)` без shell — не изменено, SR-43 соблюдён.
- `killGroup`: `syscall.Kill(-pgid, SIGKILL)` — не изменено, SR-47 усилен (добавлен reap).
- `setSelfSubreaper`: `prctl(PR_SET_CHILD_SUBREAPER, 1)` — не повышает привилегий, не меняет
  uid/gid (SR-54 соблюдён). Стандартная практика для демонов (systemd, tini).
- `reapGroupOrphans`: `waitpid(-pgid, WNOHANG)` — только reap потомков нашей группы, не влияет
  на чужие процессы. Таймаут предотвращает бесконечный loop (SR-64: no panic).
- Новые зависимости не введены: только стандартный `syscall`.

## Примечание дирижёра: статус hotfix (git-flow)

Эта правка — регрессия релизной линии: тег `v0.1.0` (на истории `main`) не собрался из-за
падения security-теста SR-47 на Linux CI; релиз заблокирован. По `guides/GIT-FLOW-GUIDE.ru.md`
§2.2 правка релизной линии — это `hotfix/*`, ответвляемый от `main`/`master` и мержимый в `main`.
Поэтому ветка переименована `fix/* → hotfix/cmdexec-killgroup-linux`. На remote опубликован только
`main` (trunk дистрибуции); ответвление от `develop` для разблокировки релиза неприменимо.
Замечание developer-guardian (verdict needs-changes по origin ветки) этим закрыто.
