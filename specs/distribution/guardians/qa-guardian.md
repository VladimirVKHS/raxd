# qa-guardian — задача `distribution`

**Verdict: pass** (после двух итераций needs-changes → fix). Guardian (read-only). Дата: 2026-05-22.
Сохранено дирижёром (red line 1).

Проверены `specs/distribution/test-plan.md`, `scripts/test-install.sh` (TEST1-3), `scripts/test-install-edge.sh`
(TEST4-9), `Makefile` (test-install/test-install-edge/test-install-all/ci-local), `Dockerfile.install`
против контракта QA, AC1-16, SR-97..SR-113, SECURITY-BASELINE §5/§6 и red lines. Учтены Docker-прогоны
дирижёра (`docker-verification.md` §4) и пойманный им баг D-2.

## История итераций
1. **needs-changes (Н-БЛОК-1):** матрица AC1 заявляла ассерт «grep: нет unit/plist генерации», которого
   в TEST8 фактически не было (проверялись только `systemctl/launchctl start`, не генерация unit/plist).
   Дыра: install.sh мог бы записать unit-файл без systemctl — тест не поймал бы. + Н-1: мёртвая `else`
   в TEST9. → возврат qa.
2. **pass (после commit `b04dbc2`):** добавлены 6 `assert_no_grep` (AC1): `/etc/systemd/system`;
   `/Library/Launch(Daemons|Agents)`; `\.service\b`; `\.plist\b`; `\[Unit\]|\[Service\]|ExecStart=`;
   `<key>Label</key>|RunAtLoad` — каждый с доказательством не-тавтологичности. Матрица AC1 приведена в
   точное соответствие коду. Мёртвая ветка TEST9 заменена на `fail`.

## Подтверждено
- **Покрытие AC1-16 полно:** каждый AC сопоставлен с тестом/способом проверки; зафиксированные
  ограничения (AC13/ОР-4 macOS вне Docker) честно оформлены как ограничение среды, не выданы за проверку.
- **Нет фальш-зелёного/тавтологии:** D-2 (3 дефекта TEST8: `curl|bash` ловил доку; `--bind` ломал grep;
  `chmod 777` с `\|`-литералом) исправлен в `72893ec`; новые AC1-ассерты не вырождены. Дирижёр прямым
  прогоном grep/perl подтвердил, что починенные и новые ассерты ПАДАЮТ на регрессии и ПРОХОДЯТ на чистом
  коде. `FAIL_COUNT>0 → exit 1` реально роняет прогон (именно это поймало D-2). Нет `|| true`-маскировки
  провалов (только в cleanup/сборе grep).
- **Security edge-кейсы — реальные прогоны в чистом контейнере:** SHA-fail → код 3 (TEST3); неподдерж.
  платформа i686/MINGW → код 2 (TEST4); усечённый скрипт → нет бинаря (TEST5); нет прав → код 4 +
  error:/hint: (TEST7). Не только статика.
- **§6:** docker-guard `/.dockerenv` в обоих тест-скриптах; Makefile-таргеты через docker build/run;
  мок-сервер `--bind 127.0.0.1` в обоих; python3 только в Dockerfile.install; нет go build/raxd на хосте.
- Edge-тесты дополняют TEST1-3, не дублируют слепо. Нет ссылок на несуществующие файлы. Артефакты
  на русском; reference не дублируется.
- **Docker-прогон дирижёра:** `make test-install-edge` → **42 PASS / 0 FAIL**; TEST1-3 (`make
  test-install`) — зелёные.

## Наблюдения (не блокируют)
- AC4: позитивные 4 пары OS×arch покрыты косвенно (TEST1 нативный + TEST9 имена в SHA256SUMS),
  отдельного шима нормализации нет — приемлемо для v1.
- AC6 «нет секретов в выводе» — допустимый gap v1 (install.sh секретами не оперирует).
- Унаследованы наблюдения devops-guardian Н-1 (golang:1.25 без @sha256) / Н-3 (shasum --quiet на старых
  macOS) — к прод-релизу.

## Итог
pass — переход к reviewer. Тесты честные, security-критичные AC покрыты реальными прогонами в Docker.
