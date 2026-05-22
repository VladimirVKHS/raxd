# tech-writer-guardian — задача `distribution`

**Verdict: pass.** Guardian (read-only). Дата: 2026-05-22. Сохранено дирижёром (red line 1).

Проверены `docs/installation.md` (новый), `README.md` (обновлён), `docs/troubleshooting.md` (секция
Installation), `specs/distribution/docs-outline.md` против контракта tech-writer, red lines и фактического
кода (`install.sh` после D-3/`f9143d7`, `scripts/release.sh`, `Makefile`, `Dockerfile.install`) + хендоффа
reviewer.

## Подтверждено
- **Точность против кода (installation.md):** env (RAXD_BASE_URL/VERSION/PREFIX + дефолты), флаги
  (--prefix/--version/-h|--help + ошибка пустого аргумента), все коды выхода 0-5 (с точными местами в
  install.sh), порядок шагов, авто-детект пути, формат SHA256SUMS (`<hash>␣␣<file>`), имена артефактов
  `raxd_<v>_<os>_<arch>.tar.gz`, содержимое архива (raxd + README всегда + LICENSE только если есть),
  команды `make ci-local/test-install` и `docker run raxd-build … make build-all release-all`, мок
  `python3 -m http.server --bind 127.0.0.1` / `debian:stable-slim` — всё сверено, расхождений нет.
- **Точность против кода (troubleshooting.md):** сообщения exit 1/2/3/4/5 (включая sudo-fail и
  no-SHA-utility) и PATH/Gatekeeper-hint дословно совпадают с english-выводом install.sh.
- **Честность ограничений:** placeholder `https://releases.example.com/raxd` (нет публичного хостинга),
  нет GPG (П-1/ОР-1, нет ложного `gpg --verify`), нет нотаризации (П-2/ОР-2), нет LICENSE, macOS вне
  Docker (AC13/ОР-4) — отражены честно, не выданы за готовое.
- **Покрытие хендоффа reviewer:** SR-105 trust model (TLS+SHA256SUMS без GPG + граница модели), SR-109/
  AC11 macOS quarantine (idempotent xattr -d + инструкция), П-3/ОР-3 (RAXD_BASE_URL/--prefix доверенный
  вход) — задокументированы.
- **Согласованность:** README больше НЕ заявляет «curl|sh not implemented yet»; якоря
  (`#installation-installsh`, `#error-unsupported-platform--unsupported-architecture-exit-2`) резолвятся;
  регистрация сервиса — ссылка на service-management.md, без дублирования.
- **Автор** Vladimir Kovalev, OEM TECH присутствует (README + installation.md). docs на английском;
  docs-outline на русском (допустимо).

## Наблюдения (не блокируют)
- **Н-1.** Косметика: пример версии в README (`raxd 1.0.0 (commit abc1234, built 2025-06-01)`) vs
  installation.md (`raxd v0.1.0 …`) — оба валидны (зависит от VERSION); гармонизировать в финальной
  задаче `docs`.
- **Н-2.** Шаг проверки SHA-утилиты не включён в нумерованный happy-path список (это негативный путь) —
  формально корректно.
- **Н-3.** Пред­существующий висячий тег `</content>` в 6 других docs-файлах (mcp/service-management/
  commands/configuration/file-upload-security/execute-command-security) — из прошлых задач, НЕ артефакт
  distribution; вычистить в финальной задаче `docs`.

## Несоответствия
Не обнаружено.

## Итог
pass — документация дистрибуции точна, честна и покрывает обязательное. Финальный гейт distribution
пройден. Можно закрывать задачу (коммиты + merge). Н-1/Н-3 — в backlog финальной задачи `docs`.
