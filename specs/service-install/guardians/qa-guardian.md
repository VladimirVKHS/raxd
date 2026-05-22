# qa-guardian — задача `service-install` (раунд 1)

**Verdict: needs-changes.** Guardian (read-only). Дата: 2026-05-22. Сохранено дирижёром (red line 1).

Проверены `test-plan.md`, `scripts/integration-service.sh`, юнит-тесты service/cli, Makefile/Dockerfile.systemd
против контракта QA. Реальный прогон qa: 61 PASS/0 FAIL (LIVE euid=999, NRestarts=3, AC8 5.0M).

## Подтверждено (честно и реально)
- Матрица AC1-16 полна; ограничения AC7 (port<1024 нужен prod-config), AC11 (rollback code-review — root в
  privileged обходит chmod 000), AC13 (macOS только на реальном macOS) обоснованы и честны, не «снятие
  требования».
- Основные пути реальны: AC1 (5 подкоманд), AC2 (renderUnit/plist + daemon-reload), AC3 (is-enabled читается),
  AC4 основной (kill живого PID→смена PID/NRestarts>0), AC5 (stop→inactive 6s, различим от AC4), AC6 основной
  (LIVE euid=999 из /proc), AC9/10 идемпотентность, AC12 (su raxd, error:/hint:), AC14 (4 цели+file), SR-88/89/
  90/91/93/95 — реальные проверки, не хардкод. Нет t.Skip в service-тестах. Всё в Docker §6. Русский.

## Issues (needs-changes — риск ложного PASS в fallback-ветках)
**ISSUE-1 (AC8/SR-94) — тавтологичный assert ротации.** integration-service.sh STEP 10 (~573-583): `grep -qiE
"[0-9]"` и `[KM]?B` → PASS при ЛЮБОЙ цифре/любом размере (5.0M и 500M и 2G проходят). Не сравнивает с порогом.
spec AC8 требует «рост ограничен». Fix: извлечь числовой размер (awk/numfmt) и сравнить с порогом (напр. ≤10M);
если restart journald не удался — fail (не pass), чтобы было видно, что end-to-end не прошёл.
**ISSUE-2 (AC4) — fallback засчитывает PASS без наблюдения рестарта.** STEP 4 (~361-369): ветка
`nrestarts==0 && PID не пойман` делает `pass AC4` опираясь только на наличие Restart=on-failure в unit (уже
проверено в STEP 1). Ложный PASS. Fix: заменить на fail (или info без зачёта AC4) с пометкой «AC4 не
верифицирован: kill не достиг живого PID и NRestarts=0».
**ISSUE-3 (AC6/SR-83) — fallback засчитывает PASS без LIVE euid.** STEP 2 (~285-293): ветки «MainPID=0» и
«EUID не прочитан» делают `pass AC6` на основании unit-файла, без LIVE-наблюдения euid. Fix: эти fallback →
info (не pass); засчитывать AC6 PASS только если LIVE-чтение /proc/$PID/status с euid!=0 было в STEP 2 ИЛИ
STEP 4; иначе fail с объяснением.

## Итог
needs-changes (3 fallback-ветки дают ложный PASS — нарушение «ни одного ложного PASS»; основные пути честны).
В реальном прогоне сработали LIVE-пути, но fallback должны падать/info, а не зеленить. Возврат к qa; затем
перепрогон systemd-интеграции + qa-guardian раунд 2.

---

## Раунд 2 — подтверждение закрытия 3 фальш-PASS + BUG-1

**Verdict: pass.** Дата: 2026-05-22. Коммиты 1485f9d (фиксы fallback), 6816768 (перепрогон). Реальный
прогон: `make test-service` 62 PASS / 0 FAIL.

- **ISSUE-1 (AC8/SR-94) закрыт:** числовое сравнение размера журнала (regex+awk→байты) `<= 10485760`; fail
  при превышении и при нечитаемом размере. Тавтология устранена.
- **ISSUE-2 (AC4) закрыт:** трёхуровневый fallback (смена PID → NRestarts>0 → fail при NRestarts=0+PID=0,
  строка ~371). Лишних pass на пустых ветках нет.
- **ISSUE-3 (AC6/SR-83) закрыт:** финальный вердикт по флагу LIVE_AC6_VERIFIED (ставится только при реальном
  чтении /proc/$PID/status euid!=0); без LIVE → fail (~строка 379).
- **BUG-1 закрыт реально:** AC6→PASS подтверждён живым прогоном (MainPID=179, euid=999, PID 179→245,
  NRestarts=1, AC8 5.0M≤10M); root-cause (ConfigurationDirectory=raxd, 827d736/70ff715) задокументирован;
  test-plan итог 62 PASS/0 FAIL.
- Нет новых фальш-зелёных/скрытых скипов; AC4/AC5 различимы; всё в Docker. Наблюдение (не блок): промежуточные
  pass-строки STEP 2 (systemctl show User=raxd) учитываются в счётчике, но финал AC6 — по флагу (честно).

## Итог
pass — тест-стратегия и интеграция service-install приняты.
