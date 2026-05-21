# Review: Bootstrap CLI — каркас проекта raxd

## Summary

Реализация `bootstrap-cli` в ветке `feature/bootstrap-cli` соответствует всем 12 acceptance criteria из `spec.md`, контрактам `plan.md` (модули, сигнатуры, каналы вывода, поведение заглушек) и всем активным требованиям `security-requirements.md`. Код идиоматичный, без мусора и мёртвого кода, зависимости строго из STACK (cobra v1.10.2, viper v1.21.0). Принятое отклонение от STACK (ручной XDG-резолв вместо `adrg/xdg`) обосновано, зафиксировано в plan/impl-notes/threat-model и достигает D3, подтверждено тестами. Security-инварианты (нет exec/net.Listen/секретов, директории 0700, честная заглушка serve) выполнены и проверены независимым grep + тестами без признаков «зелёных за счёт ослабления». Обязательных issues нет. **Verdict: accept.**

## Покрытие AC (проверено по коду)
- AC-1 (go.mod модуль + go 1.25, собирается) — `go.mod:1,3`, Dockerfile build stage. OK.
- AC-2 (cmd/+internal/, изоляция) — раскладка верна. OK.
- AC-3 (--help: все подкоманды) — `root.go:39-45` + `key.go`/`config.go`. OK.
- AC-4 (заглушки: ненулевой exit + сообщение; serve честная) — `stub.go:18-23`, `serve.go`. OK.
- AC-5 (version: версия/commit/дата, ldflags, дефолты) — `version.go`, `main.go`. OK.
- AC-6 (status: not running + пути, exit 0) — `status.go`. OK.
- AC-7 (XDG, единый ~/.config/raxd, отсутствие config.yaml не ошибка) — `paths.go`, `config.go`. OK.
- AC-8 (директории 0700) — `paths.go:66`. OK.
- AC-9 (баннер с авторством) — `banner.go:25`. OK.
- AC-10 (нет секретов) — подтверждено grep. OK.
- AC-11 (Dockerfile build+test) — `Dockerfile`. OK.
- AC-12 (юнит-тесты) — покрытие полное (50 тестов). OK.

## Issues
Обязательных, блокирующих merge, нет.

### Необязательные (nice-to-have, не блокеры)
- [ ] Баннер: ширина рамки считается в байтах (`len()`), а не в видимой ширине рун — `internal/banner/banner.go:28-36`. Артефакт для строк с `—`/`·`. AC-9 не нарушен (визуальный дизайн вынесен в cli-ux). Действие: при подключении lipgloss считать `utf8.RuneCountInString`/`lipgloss.Width`. В bootstrap-cli действий не требуется.
- [ ] `TestStubsErrorPrefix` хрупко индексирует `strings.Split(stderr,"\n")[1]` — `internal/cli/security_test.go:33`. Спасает `|| Contains(stderr,"error:")`; паники нет. Косметика, не блокер.
- [ ] Рассинхрон счётчика тестов: `impl-notes.md` (20) vs `test-plan.md` (50). Факт = 50 (test-plan актуален). Синхронизировать impl-notes.

## Looks good
- Каналы вывода строго по контракту: баннер/ошибки → `cmd.ErrOrStderr()` (`root.go:28`, `stub.go:20`), version/status → `cmd.OutOrStdout()` (`version.go:18`, `status.go:29`). BUG-001 закрыт правильно. `TestBannerChannelSplit`/`TestStatusOnStdout` проверяют обе стороны.
- Безопасность по построению: `EnsureDirs` явный `0o700` (umask-независимо); нет `exec.*`/`net.Listen`/`math/rand`/секретов (grep); `serve` — честная заглушка без блокировки/порта.
- Обработка ошибок чистая: корректный `%w` в EnsureDirs; `config.Load` различает «нет файла → дефолты» и «битый YAML → ошибка» через errors.As/Is.
- Точки расширения заложены без расширения scope: `KeysDB`/`TLSDir` в PathSet, `Port` в Config, `banner.Render()`, `version.Set/Info`.
- Git-дисциплина: атомарные Conventional Commits в feature-ветке.
- STACK соблюдён: только cobra+viper прямыми зависимостями; Dockerfile single-stage golang:1.25, CGO_ENABLED=0, stage build/test (baseline §6).

## Security checks (дополнительно по требованию reviewer-guardian)

### Security check 1 — документированная docker-команда запуска (подтверждено)
`security-requirements.md:58-60` (baseline §6, AC-11). Явные docker-команды присутствуют:
- `Dockerfile:7` — `docker build --target test ... && docker run --rm raxd-test`.
- `Dockerfile:10` — `docker build --target build`.
- `Dockerfile:13` — `docker run --rm -v "$PWD":/src -w /src golang:1.25 sh -c "CGO_ENABLED=0 go build ./... && go test ./..."`.
- `test-plan.md:121-141` — раздел «Как запускать» (docker-команды + пометка «на хосте go test не запускается»).
- `impl-notes.md:59-68` — те же docker-команды.
Хостовых go-инструкций нет. Отдельный README — за tech-writer (не требуется для AC-11). Чисто.

### Security check 2 — отсутствие повышения привилегий / запуска от root (подтверждено)
`security-requirements.md:78-80` (baseline §3). Grep по всему `*.go`:
- Нет `Setuid/Setgid/Seteuid/Setreuid`, `Chown/Lchown`, `Setcap/CAP_NET_BIND_SERVICE`, `prctl`, `Geteuid`, `SysProcAttr`, `Credential`, `sudo` — 0 совпадений.
- Нет жёстко зашитых системных путей (`/etc`, `/usr`, `/var`, `/opt`, `/root`).
- Единственный `syscall` во всём репозитории — `syscall.Umask(0o022)` в `internal/config/security_test.go:22-23` (только тест umask-независимости); в `cmd/`/`internal/` продакшен-коде `syscall` не используется.
- Весь I/O в пользовательском пространстве: пути через `os.UserHomeDir()`/`XDG_*` (`paths.go:30-47`), каталоги `0o700` (`paths.go:66`). `serve` процесс не запускает. Чисто.

## Verdict
accept

Эстафету принимает tech-writer. Рекомендация: синхронизировать счётчик тестов в impl-notes (→50).
