# devops-guardian — задача `distribution`

**Verdict: pass.** Guardian (read-only). Дата: 2026-05-22. Сохранено дирижёром (red line 1).

Проверены install.sh, scripts/release.sh, scripts/test-install.sh, Makefile, Dockerfile.install,
Dockerfile (стадии), .github/workflows/{ci,release}.yml, impl-notes.md против SECURITY-BASELINE §5/§6,
контракта devops, AC1-16 и SR-97..SR-113. Учтён фикс бага D-1 (commit `1fde912`) и Docker-верификация
дирижёра (`docker-verification.md`).

## Подтверждено
- **Каркас install.sh (SR-97/98/99):** `#!/usr/bin/env bash`; `set -euo pipefail` первой исполняемой
  строкой (26); всё тело в `main()` (30–289); единственный вызов `main "$@"` последней строкой (293) —
  защита от обрыва `curl|sh`; `tmpdir=""` до `trap cleanup EXIT INT TERM` (46/52), `mktemp -d` (108);
  нет фиксированных temp-путей; дефолт `RAXD_BASE_URL` = `https://…`; скачивание `curl -fsSL`.
- **Целостность (SR-100/101/102):** проверка SHA256 (183–205) строго ДО `tar` (212) и `install`
  (241/253); несовпадение → `exit 3` + trap-cleanup, бинарь не ставится; имена
  `raxd_<v>_<os>_<arch>.tar.gz` идентичны install.sh(143)=release.sh(80); формат `<hash>␣␣<file>`
  (release.sh:130). Живой прогон дирижёра: подмена → код 3, бинарь не установлен.
- **Минимизация/детект (SR-103/104):** нет `eval` скачанного, нет запуска демона; строгий детект
  OS/arch с нормализацией; неподдерживаемое → код 2 без установки.
- **Привилегии (SR-106/107/108):** не-root по умолчанию (`/usr/local/bin` если writable иначе
  `~/.local/bin`); sudo только явно с сообщением; атомарный `install -m 0755` (нет chmod 777); PATH-hint.
- **macOS quarantine (SR-109/105):** идемпотентный `xattr -d … 2>/dev/null || true` + инструкция;
  нет ложного `gpg --verify` без ключа.
- **§6 — только Docker (SR-112/113) — критичная точка после D-1:** `DOCKER_GUARD` (`test -f
  /.dockerenv`) присутствует в build-linux-amd64/arm64 (91/100), build-darwin-amd64/arm64 (109/118) и
  `release` (219); `test-install` (251) без prereq, только проверка `dist/SHA256SUMS` (253); `release`
  (218) без prereq build-all; `ci-local` собирает артефакты ТОЛЬКО через `docker run raxd-build … make
  build-all release-all`. Guard не ломает in-Docker (`/.dockerenv` есть) и `docker build`-стадии (они
  зовут `go build` напрямую, не через make). Мок-сервер `--bind 127.0.0.1` + trap kill; python3 только
  в Dockerfile.install; CI `go vet/test` внутри `container: golang:1.25`. go.mod distribution не менял.
- **Секреты (SR-110/111):** ldflags только Version/Commit/Date; CI токены только через `secrets.*`;
  install.sh `error:`/`hint:` строчными, без сырых трасс.
- **Отклонения П-1..П-4 и ОР-1..ОР-5** зафиксированы (GPG/нотаризация/RAXD_BASE_URL/macOS-вне-Docker),
  компенсации адекватны, не молчаливый пропуск.
- Git-flow: ветка `feature/distribution` от develop, Conventional Commits; артефакты на русском.

## Наблюдения (не блокируют)
- **Н-1.** `golang:1.25` без pin `@sha256:` в Dockerfile/CI — воспроизводимость; отмечено самим devops
  (impl-notes п.7), к публичному релизу.
- **Н-2.** Реальный CI на runner не прогонялся (нет remote, ОР-5) — поведение `.dockerenv` в
  `container:`-job проверено логикой; локальный гейт `make ci-local` подтверждён дирижёром.
- **Н-3.** `shasum --quiet` может не поддерживаться на старых macOS; ошибка скрыта `2>/dev/null`,
  несовпадение всё равно даёт ненулевой код — функционально безопасно (проверяется на реальном macOS, ОР-4).
- **Н-4.** `.goreleaser.yaml` отсутствует — соответствует ADR-001 (вариант B, goreleaser неиспользуемый).
- **Н-5.** install.sh лишь hint `raxd service install` — регистрация сервиса в Out of Scope (AC1).

## Несоответствия
Не обнаружено.

## Итог
pass — переход к qa. Все 16 AC и SR-97..SR-113 закрыты; D-1 устранён корректно; целостность реально
отклоняет подмену (живой прогон). Артефакты devops править не требуется.
