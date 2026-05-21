# Guardian Report: developer-guardian — bootstrap-cli

## Summary
Код каркаса добротный: структура соответствует plan.md, все AC из spec.md имеют тесты, заглушки реализованы честно, безопасность каркаса соблюдена (нет exec/net.Listen/секретов, 0700, serve — честная заглушка), git-flow и Conventional Commits соблюдены (9 атомарных коммитов в feature/bootstrap-cli). Два дефекта обязательны к исправлению + отклонение по adrg/xdg требует формального закрытия.

## Issues
### Issue 1 (MUST): имя публичной функции расходится с контрактом plan.md
- `internal/config/paths.go` — реализована `GetPaths()`, plan.md задаёт `Paths()`. Публичное имя — часть контракта.
- Исправить: переименовать `GetPaths`→`Paths`, обновить вызовы (root.go, status.go), тесты, impl-notes.

### Issue 2 (MUST): баг в EnsureDirs теряет первопричину ошибки
- `internal/config/paths.go:65` — `fmt.Errorf("...: %w", errors.Unwrap(err))` вместо `%w, err`. У `*os.PathError` Unwrap() отдаёт только syscall.Errno, теряя путь; при nil-Unwrap строка станет "...: <nil>".
- Исправить: оборачивать сам `err`. Проверить аналогичные места для state/tls каталогов.

### Issue 3 (ЭСКАЛАЦИЯ): отклонение от STACK по adrg/xdg не оформлено как принятое решение
- `go.mod` не содержит adrg/xdg; plan.md и spec.md его называют. Запись в impl-notes есть, но нужно формальное закрытие architect/PM.
- Вердикт по отклонению: (а) задокументировано в impl-notes — да; (б) контракт D3 (единый ~/.config/raxd + приоритет XDG_CONFIG_HOME) достигнут корректно, покрыт тестами, технически даже лучше adrg/xdg; (в) без оформленной эскалации/принятия — недопустимо как «молчаливое» отклонение. Требуется: architect фиксирует решение в plan.md.

### Issue 4 (INFO): формат version расходится plan (`raxd <version> ...`) vs ux-spec (`raxd v1.0.0 ...`)
- Код следует plan (без v-префикса), тест согласован. Выровнять контракты (cli-ux/architect).

### Issue 5 (INFO): t.Skip в TestGetPathsDefault
- Условный защитный skip (нет $HOME). Формально против red line developer. Сделать тест детерминированным (t.Setenv) и убрать Skip.

## Looks good
- Структура пакетов точно по plan; точки расширения (KeysDB, TLSDir) заложены.
- Безопасность каркаса подтверждена тестами (нет exec/net.Listen/секретов; 0700 явно; разделение stdout/stderr).
- Git-flow: feature/bootstrap-cli от develop, 9 атомарных Conventional Commits, без деструктивных операций.
- Сборка/тесты — docker-команды (baseline §6), 20 тестов pass.

## Verdict
needs-changes
(MUST: Issue 1, 2; ЭСКАЛАЦИЯ: Issue 3. INFO: 4, 5 — желательно закрыть.)
