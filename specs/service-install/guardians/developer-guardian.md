# developer-guardian — задача `service-install` (раунд 1)

**Verdict: needs-changes.** Guardian (read-only). Дата: 2026-05-22. Сохранено дирижёром (red line 1).

Проверены коммиты 0e7b32f/9f4a62b/fb919e2/83a0b44/b40d677/6b4c663 против plan.md, security-requirements
(SR-83..96), ux-spec, контракта developer. Docker-верификация дирижёра: 479 PASS, 0 FAIL.

## Подтверждено
- Модули по plan.md (ServiceManager 5 методов, 5 типизир. ошибок, New() по GOOS, render*/runManager).
- SR-90 анти-инъекция реальна (11 user/5 ExecPath/4 Label векторов отвергаются ДО рендера; тесты проверяют
  отсутствие инъекции в выводе — не тавтология); templates.go без build-тегов (AC13 ок).
- SR-85/86/87 (NoNewPrivileges/AmbientCapabilities/hardening по NeedNetBindCap bool), SR-89
  (StateDirectoryMode=0700), SR-88 (0644), SR-91 (exec.CommandContext без sh -c, фикс. пути), SR-83 (User=raxd),
  SR-92 (rollback), SR-93/94 (uninstall+journald drop-in). Whitelist internal/service обоснован. Без новых deps.
- Exit-коды/потоки CLI базово по ux-spec (status→stdout, ErrAlreadyInstalled→exit0 и т.д.).

## Issues (needs-changes)
**ISSUE-1 (важно, plan+SR-85/ADR-003).** `internal/cli/service.go:58` resolveManager берёт Port из
`service.DefaultConfig()` (хардкод 7822), НЕ из `config.Load(paths).Port`. При порте<1024 unit генерируется
БЕЗ AmbientCapabilities → демон не сможет слушать привилегированный порт. plan §Contracts/§Интеграция требует
Port из config.Load. Fix: читать config.Load(paths).Port в RunE install (и где нужен порт).
**ISSUE-2 (ux-spec).** `service.go:427-454` jsonStatus без полей port/autostart/unit_path (ux-spec §status --json
требует). Fix: добавить и заполнить.
**ISSUE-3 (ux-spec).** `service.go:388-424` printStatusHuman без строк port/autostart. Fix: добавить.
**ISSUE-4 (ФАЛЬШ-ЗЕЛЁНЫЙ).** `internal/service/exec_test.go:48-67` TestRunManager_RawStderrNotPropagated
заканчивается `_ = errStr` — ничего не проверяет, всегда зелёный. Fix: реальная проверка (команда с известным
stderr, напр. echo RAW_SECRET >&2; убедиться, что RAW_SECRET нет в err.Error()).
**ISSUE-6 (ux-spec).** `service.go:104-121` success-блок install без port/autostart. Fix: добавить (зависит от ISSUE-1).

## Info (не блокируют, желательно заодно)
- ISSUE-5: neutralizeStderr (templates.go:353-369) вычисляет raw, но всегда возвращает фикс. строку — raw не
  используется в return (SR-95 случайно соблюдён). Задокументировать/упростить.
- ISSUE-7: launchd.go:108 прямой exec.Command для chown вместо runCommandRaw — консистентность.

## Итог
needs-changes (ISSUE-1 — реальный баг capability; ISSUE-4 — фальш-зелёный тест; ISSUE-2/3/6 — ux-spec поля).
Возврат к developer. После правок — перепрогон Docker дирижёром + повторный developer-guardian.

---

## Раунд 2 — подтверждение закрытия 5 блокирующих + 2 info

**Verdict: pass.** Дата: 2026-05-22. Коммиты 4876696 (код+тесты), 5a1f6cf/66ed17e (impl-notes). Docker
дирижёра: 11 пакетов PASS, 0 FAIL.

- **ISSUE-1 (закрыт):** resolveManagerWithPort (service.go:64-91) читает config.Load(Paths()).Port, error
  обработан, порт прокинут в svcCfg.Port до New(); fallback к DefaultConfig при недоступном HOME. Тесты
  service_whitebox_test.go (port:443→443; нет конфига→7822) реальны, не тавтология.
- **ISSUE-4 (закрыт):** TestRunManager_RawStderrNotPropagated (exec_test.go:54-72) запускает /bin/ls
  /nonexistent... и ассертит отсутствие sentinel в err.Error() — реальная проверка SR-95, упадёт при поломке
  neutralize. t.Fatal при err==nil. Не тавтология.
- **ISSUE-2/3/6 (закрыты):** jsonStatus с port/autostart/unit_path; printStatusHuman со строками port/autostart;
  install success block с port/autostart — сверено с ux-spec.
- **ISSUE-5 (закрыт):** neutralizeStderr всегда фикс. строка, мёртвый код убран, импорт strings убран. SR-95 ок.
- **ISSUE-7 (закрыт):** launchd.go chown через runCommandRaw, импорт os/exec убран. SR-91 ок.
- Регрессий/новых фальш-зелёных нет; plan/security-requirements соблюдены; static-тест не ослаблен; go.mod
  не изменён; коммиты атомарны.

## Итог
pass — реализация service-install принята. Переход к qa (systemd-Docker интеграция).

---

## Раунд 3 — гейт BUG-1 фикса

**Verdict: needs-changes.** Дата: 2026-05-22. Коммиты 827d736 (ConfigurationDirectory+launchd ConfigDir),
70ff715 (macOS-пути: DefaultConfigForGOOS, ConfigHome/StateHome, инвариант E). Docker дирижёра: unit PASS;
qa systemd-интеграция 62 PASS/0 FAIL (LIVE euid=999, рестарт PID 179→245, AC8 5.0M≤10M).

## Подтверждено
- ConfigurationDirectory=raxd + ConfigurationDirectoryMode=0700 в ОБОИХ вариантах unit (templates.go ~194-200,
  фикс. часть до условного блока; тесты DefaultPort/PrivilegedPort). SR-89 ок.
- macOS консистентность: DefaultConfigForGOOS(darwin)→/usr/local/etc/raxd,/usr/local/var/raxd,/usr/local/var/
  log/raxd; ConfigHome/StateHome=filepath.Dir; plist XDG из переменных; createDirs создаёт реальные raxd-каталоги
  (launchd.go:102 включает ConfigDir). SR-90 распространён на ConfigHome/StateHome (ValidateTemplateData).
- Linux не сломан (DefaultConfigForGOOS(linux)→/etc/raxd,/var/lib/raxd; systemd hardcoded XDG /etc /var/lib;
  TestPlist_LinuxXDGPathsRegress). Без новых зависимостей; static-тест не тронут; scope только BUG-1.

## Issues (needs-changes)
**ISSUE-1 — тавтологичный тест инварианта.** `templates_test.go` TestTemplateDataFromConfig_InvariantE проверяет
`filepath.Dir(ConfigDir)+"/raxd"==ConfigDir` — истинно для ЛЮБОГО пути вида `*/raxd`, не ловит неверные-но-
консистентные значения. Конкретные значения проверяет лишь TestDefaultConfigForGOOS_Paths (отдельно). Fix:
добавить в InvariantE явные проверки значений: linux ConfigHome=="/etc",StateHome=="/var/lib"; darwin
ConfigHome=="/usr/local/etc",StateHome=="/usr/local/var".
**ISSUE-2 — impl-notes не обновлён под BUG-1.** Нет раздела Round 3, описания ConfigurationDirectory/
DefaultConfigForGOOS, 4 новых тестов в таблице, обновлённого Docker-вывода. Fix: добавить раздел Round 3
(причина бага, изменения, 4 теста с привязкой SR/AC, свежий Docker-вывод 62 PASS/0 FAIL).

## Итог
needs-changes (тавтологичный тест + неактуальный impl-notes). Блокеров нет — функционал BUG-1 корректен.
После 2 правок → pass.

### Раунд 3 — закрытие 2 issue (инлайн-подтверждение дирижёра)
**Verdict: pass.** Коммит bbb3442. Guardian раунда 3 дал pass предусловно «после 2 правок».
- ISSUE-1 закрыт: TestTemplateDataFromConfig_InvariantE теперь таблица с ЯВНЫМИ значениями (linux ConfigHome
  "/etc"/StateHome "/var/lib"; darwin "/usr/local/etc"/"/usr/local/var") + инвариант — не тавтология; падает
  при неверных-но-консистентных путях. PASS в Docker.
- ISSUE-2 закрыт: impl-notes раздел Round 3 (причина бага, 2 коммита, таблица 5 тестов SR-89/AC2/AC13,
  инвариант E, qa systemd 62 PASS/0 FAIL).
- Docker дирижёра: 0 FAIL; TestPlist_DarwinXDGPaths/TestDefaultConfigForGOOS_Paths/InvariantE — PASS.
Реализация service-install (вкл. BUG-1) принята. Переход к reviewer.
