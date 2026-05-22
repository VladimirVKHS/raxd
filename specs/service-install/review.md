# Review — задача `service-install`

**Verdict: accept.** Reviewer (read-only). Дата: 2026-05-22. Ветка feature/service-install поверх develop.
Сохранено дирижёром (reviewer не пишет сам — red line 1).

Вход: spec.md (AC1-16), plan.md, service-design.md, ux-spec.md, security-requirements.md (SR-83..96),
threat-model.md, ADR-001..004, impl-notes.md, test-plan.md, SECURITY-BASELINE. Сборку/тесты не запускал
(read-only) — доверие Docker-выводу дирижёра/qa (unit 0 FAIL; systemd-интеграция 62 PASS/0 FAIL), критичное
перепроверено чтением.

## Соответствие AC1-16 — все закрыты
| AC | Статус | Доказательство |
|---|---|---|
| AC1 CLI lifecycle | accept | cli/service.go группа+5 подкоманд; TestServiceCommandRegistered; integration STEP1-8 |
| AC2 валидное описание | accept | RenderUnit/RenderPlist; daemon-reload exit0 (STEP1) |
| AC3 автозапуск | accept | systemctl enable + WantedBy=multi-user.target; is-enabled=enabled |
| AC4 restart-on-failure | accept | Restart=on-failure/RestartSec; KeepAlive SuccessfulExit=false; LIVE PID 179→245 |
| AC5 graceful stop | accept | SIGTERM→graceful; inactive 6s без авторестарта |
| AC6 не-root euid≠0 | accept | User=raxd; LIVE euid=999 из /proc (STEP2/4) |
| AC7 capability порт<1024 | accept(unit)/огранич.live | условный AmbientCapabilities; TestRenderUnit_PrivilegedPort; live<1024 нужен prod-config |
| AC8 ротация | accept | journald drop-in; 5.0M≤10M при заниженном пороге (STEP10) |
| AC9 идемпот. install | accept | ErrAlreadyInstalled→exit0; integration STEP6 |
| AC10 идемпот. uninstall | accept | ErrNotInstalled→exit0; STEP7/8 |
| AC11 безопасный откат | accept(code-review)/огранич. | rollback(unit,dropIn); root обходит chmod 000 (честно) |
| AC12 понятные ошибки | accept | error:/hint: строчные; STEP9 |
| AC13 macOS-ограничение | accept | templates.go без build-тегов; TestPlist_DarwinXDGPaths; зафиксировано |
| AC14 кросс-сборка 4 цели | accept | Makefile -mod=vendor CGO=0; 4 артефакта+file |
| AC15 без новых deps | accept | go.mod stdlib+cobra/charm; нет kardianos |
| AC16 проверяемость Docker | accept | unit+интеграция офлайн vendor |

## Безопасность SR-83..96 — выполнены
Не-root euid LIVE-подтверждён (euid=999); capability условная+минимальная (AmbientCapabilities только при
NeedNetBindCap, CapabilityBoundingSet, NoNewPrivileges при ≥1024, опуск только при ambient — П-1); права 0644
unit/plist + StateDirectoryMode/ConfigurationDirectoryMode=0700 (SR-89); анти-инъекция SR-90 (ValidateTemplateData
до рендера, allowlist+control-char+IsAbs, 24+ вектора, InjectionRejectedBeforeRender); exec без shell (SR-91,
static-тест whitelist internal/service обоснован); uninstall полный, raxd сохранён осознанно (П-2); ротация
journald (SR-94); neutralizeStderr фикс. строка + реальный sentinel-тест (SR-95). Дыр не найдено.

## Закрытие прежних ОР
command-exec ОР-1/file-upload ОР-U1 (root-исполнение/запись) → не-root раскладка raxd:raxd euid!=0.
command-exec ОР-2/file-upload ОР-U4 (ротация аудита) → journald-лимиты. Зафиксировано.

## Остаточные риски (эскалация) — в threat-model
ОР-1 (NoNewPrivileges×ambient при <1024), ОР-2 (journald per-host), ОР-3 (raxd после uninstall), ОР-4
(macOS вне Docker), ОР-5 (privileged-контейнер для тестов).

## Качество
Идиоматичный Go, типизир. ошибки+ServiceError.Is(), чистые рендеры, инъекция менеджера для тестов, нет shell,
нет мёртвого кода, scope не превышен. Фальш-зелёные, что ловили guardians (тавтологичный инвариант, 3
fallback-PASS, фейковый SR-95 тест) — исправлены. Тесты содержательны (проверено на тавтологии).

## Findings (info, low — НЕ блокируют accept)
- **F-1 (info).** exec.go:87 mapExitCode — ветка `code==1 && detail=="manager command failed"` всегда истинна
  по detail, но обе ветки → ErrManagerUnavailable (эквивалентно). Не баг. Опц.: упростить.
- **F-2 (info).** integration STEP3 status через `2>&1` не валидирует разделение потоков; покрыто unit
  TestServiceStatus_OutputOnStdout. Опц.: захватывать stdout отдельно.
- **F-3 (info).** launchd.go:88-91 `launchctl enable` ошибка `_ = err` молча; путь macOS вне Docker (ОР-4),
  автозапуск через RunAtLoad=true независимо. Опц.: на реальном macOS проверить/логировать.

## Итог
accept. AC1-16 + SR-83..96 + baseline соблюдены, отклонения как утверждено security, дыр нет, scope не
превышен, без новых зависимостей. F-1..F-3 info — не гейт. Хэндофф tech-writer: документировать сохранение
raxd после uninstall (П-2), journald-ротацию+пороги (SR-94, fallback logrotate П-3), macOS-ограничение+прогон
на реальном macOS (AC13/ОР-4), capability для порта<1024 (ADR-003). Перед прод-релизом с <1024 или macOS —
эскалации ОР-1/ОР-4.
