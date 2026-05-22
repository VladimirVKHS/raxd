# ADR-001: Process group + kill для гарантированного завершения процесса и его потомков

## Контекст
`execute_command` запускает на хосте дочерний процесс с таймаутом и должен гарантированно
завершить его (и всех его потомков) при таймауте/отмене контекста (AC5/AC6 spec.md). Проблема:
`exec.CommandContext` по умолчанию посылает Kill **только головному процессу**, а его дети
осиротевают (PPID=1) и продолжают жить — это прямое нарушение AC6 «не остаётся осиротевших
процессов». Дополнительно: при WaitDelay=0 долгоживущий потомок с открытым I/O-пайпом может
подвесить `Wait` даже после убийства головы. Требование: только stdlib (без новых зависимостей),
Linux + darwin (Windows вне scope).

## Решение
Перед стартом команды задать `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` — это
помещает дочерний процесс в **новую группу процессов** (PGID = PID ребёнка). Переопределить
`cmd.Cancel` так, чтобы при отмене/таймауте слать сигнал **всей группе**:
`syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)` (отрицательный PID = группа). Дополнительно
выставить **ненулевой `cmd.WaitDelay`** как подстраховку: если процесс не вышел после Cancel или
оставил пайпы открытыми, рантайм exec сам добьёт процесс через `Process.Kill()` и закроет пайпы,
разблокировав чтение. Платформенный код (`syscall.SysProcAttr`) — под `//go:build unix`.

Базовый вариант: сразу SIGKILL группе (минимум кода, закрывает AC6). Возможная эволюция —
SIGTERM группе с graceful-периодом, затем SIGKILL через WaitDelay (см. Альтернативы).

## Альтернативы
- **Только `exec.CommandContext` (default Kill головы).** Отвергнут: убивает лишь головной
  процесс, потомки осиротевают (PPID=1) → AC6 не выполняется; долгий потомок с открытым пайпом
  подвешивает Wait при WaitDelay=0. Источник: https://pkg.go.dev/os/exec#CommandContext ,
  https://pkg.go.dev/os/exec#Cmd.WaitDelay , практика:
  https://medium.com/@felixge/killing-a-child-process-and-all-of-its-children-in-go-54079af94773 ,
  https://sigmoid.at/post/2023/08/kill_process_descendants_golang/
- **`Setpgid` + SIGTERM→(grace)→SIGKILL группе.** Корректен и «вежливее» (даёт чистое завершение),
  но добавляет graceful-период и edge-case в тестах; выигрыш для НЕинтерактивных stateless-команд
  (интерактив/PTY в Out of Scope) невелик. Хороший путь эволюции, если security/architect захотят
  graceful. Источник: https://pkg.go.dev/os/exec#Cmd.Cancel
- **`Pdeathsig` для авто-смерти детей при гибели родителя.** Отвергнут как основной механизм:
  поле есть **только на Linux**, на darwin отсутствует → не кросс-платформенно; годится лишь как
  Linux-specific подстраховка, не как гарантия. Источник:
  https://pkg.go.dev/syscall?GOOS=linux#SysProcAttr , https://go.dev/src/syscall/exec_linux.go ,
  https://groups.google.com/g/golang-codereviews/c/jSRC-H5nZ5c

## Последствия
- Плюсы: убивается **всё дерево процессов** (закрывает AC6); WaitDelay страхует от зависших
  пайпов/потомков; полностью на stdlib (`os/exec`, `syscall`), новых зависимостей нет; `Setpgid`
  portable на Linux и darwin.
- Минусы/цена: SIGKILL без graceful — процесс не успевает завершиться чисто (для НЕинтерактивных
  команд приемлемо); нужен платформенный файл под `//go:build unix` для `SysProcAttr`; на darwin
  нет `Pdeathsig` (подстраховка при внезапной смерти демона слабее, но демон в контейнере —
  baseline §6).
- Влияние на стек: согласуется со STACK.ru.md (stdlib предпочтительна, новых зависимостей нет).
  Совместимо с Go 1.25 (`Cmd.Cancel`/`Cmd.WaitDelay` доступны с Go 1.20:
  https://go.dev/doc/go1.20).

## Статус (proposed|accepted)
accepted — ратифицирован гейтами security-guardian + architect-guardian (command-exec), 2026-05-22.
