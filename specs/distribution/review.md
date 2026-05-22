# Review: distribution — финальное ревью

**Verdict: accept.** Reviewer (read-only). Дата: 2026-05-22. Сохранено дирижёром.
Вход: spec.md (AC1-16), plan.md, security-requirements.md (SR-97..SR-113), threat-model.md,
ADR-001..005, реализация (install.sh, scripts/*, Makefile, Dockerfile.install, CI), docker-verification.md,
все guardian-отчёты.

## Сводка
Все 16 AC выполнены реализацией (не только заявлены), все 17 SR (SR-97..SR-113) выполнены либо являются
явными отклонениями (П-1..П-4) с адекватной компенсацией. Сквозной контракт артефактов (AC16: имена
install.sh = release.sh = SHA256SUMS) согласован и проверен живым прогоном; целостность по SHA256
проверяется ДО размещения с реальным hard-fail (код 3), подтверждено подменой архива в Docker, а не
статикой. Каркас `curl|sh` корректен (set -euo pipefail → тело в main → единственный вызов в конце →
trap cleanup), минимизация соблюдена, не-root по умолчанию, мок-сервер изолирован на 127.0.0.1.
Реализация не отклонилась от Chosen Approach (ручной релиз без goreleaser, мок-HTTP + RAXD_BASE_URL,
авто-детект пути, CI-артефакт + ci-local, idempotent quarantine).

Два реальных дефекта, найденных дирижёром при Docker-верификации, устранены и перепроверены:
**D-1** (host-build leak §6/SR-112) → DOCKER_GUARD + развязка prereq (`1fde912`); **D-2** (3 фальш-/
ложно-зелёных ассерта TEST8) → `72893ec`/`b04dbc2`. Похожих фальш-зелёных при ревью не обнаружено.
Блокеров merge в develop нет.

## AC-таблица
| AC | Статус | Где |
|----|--------|-----|
| AC1 | выполнен | install.sh ставит только бинарь + hint `raxd service install` (install.sh:287); нет генерации unit/plist — 6 assert_no_grep (TEST8) |
| AC2 | выполнен | set -euo pipefail (26), тело в main (30-289), вызов в конце (293), trap (52); TEST5 усечение |
| AC3 | выполнен | SHA до tar/install (176-205 vs 212/241); несовпадение→exit 3; TEST3 живой |
| AC4 | выполнен | детект+нормализация (110-136); неподдерж.→код 2; TEST4 (i686+MINGW) |
| AC5 | выполнен | install -m 0755 атомарно (241); ровно 1 бинарь — TEST2 |
| AC6 | выполнен | error:/hint: строчными, ненулевые коды, нет секретов — TEST3/4/7 |
| AC7 | выполнен | только перечисленные шаги; нет eval/демона — TEST8 |
| AC8 | выполнен | release.sh:78-131 → 4 tar.gz + SHA256SUMS; живой: хэши консистентны |
| AC9 | выполнен | авто-детект пути (222-230), sudo только явно (244-257); нет прав→код 4 — TEST7 |
| AC10 | выполнен | ldflags main.buildVersion/Commit/Date (Makefile:48-51); формат корректен; не dev |
| AC11 | выполнен | darwin: idempotent xattr -d + Gatekeeper-инструкция (264-272) — TEST6 |
| AC12 | выполнен | install-flow в чистом debian:stable-slim против мок-HTTP; raxd version OK |
| AC13 | зафиксировано | ограничение macOS-в-Docker задокументировано (test-plan/threat-model П-4); статика — TEST6 |
| AC14 | выполнен | CI YAML офлайн vendor в container golang:1.25; гейт v1 — make ci-local (живой exit 0) |
| AC15 | выполнен | -mod=vendor, CGO_ENABLED=0; нет новых зависимостей; linux — статич. ELF |
| AC16 | выполнен | имя идентично install.sh:143=release.sh:80; 4 цели в SHA256SUMS — TEST9 + sha256sum -c |

## SR / безопасность
- Каркас SR-97/98/99: тело в main+вызов в конце; mktemp -d + tmpdir объявлен ДО trap (защита set -u);
  дефолт RAXD_BASE_URL=https://; curl -fsSL.
- Целостность SR-100/101/102: проверка ДО размещения hard-fail код 3; фильтрация по архиву;
  детект sha256sum/shasum (резервный план Q8); формат `<hash>␣␣<file>`.
- Минимизация/детект SR-103/104, привилегии SR-106/107/108 (не-root, sudo явно, нет chmod 777,
  PATH-hint), секреты SR-110/111 (ldflags только Version/Commit/Date, CI только secrets.*),
  среда SR-112/113 (DOCKER_GUARD, мок 127.0.0.1+trap, python3 только в Dockerfile.install) — выполнены.
- Отклонения корректны и не молчаливы: П-1 (нет GPG — целостность на TLS+SHA, нет ложного gpg --verify),
  П-2 (нет нотаризации — quarantine+инструкция), П-3 (RAXD_BASE_URL доверенный вход, HTTPS-дефолт),
  П-4 (macOS вне Docker). Модель доверия v1 честна: SHA256SUMS с того же источника защищает от
  транзитной порчи/подмены одного файла, но не от согласованной подмены обоих → явно вынесено в ОР-1.

## Несоответствия / риски (НЕ блокируют merge)
- **Низкий.** `shasum --quiet` (install.sh:197) поддерживается не во всех версиях macOS; stderr скрыт
  `2>/dev/null`, несовпадение всё равно даёт ненулевой код → exit 3 (функц. безопасно). Проверить на
  реальном macOS (ОР-4); опц. убрать `--quiet`.
- **Низкий (косметика).** В TEST3 имя backup-переменной хардкодит `amd64` (test-install.sh:194) при
  arm64-хосте; логика восстановления корректна на любой arch — переименовать в `${native_archive}.orig`.
- **Низкий (зафиксировано devops Н-1).** golang:1.25 без pin @sha256 — к продакшн-пайплайну.

## Остаточные риски к прод-релизу (вынесены корректно, НЕ выданы за решённые)
ОР-1 GPG-подпись SHA256SUMS; ОР-2 Apple-нотаризация; ОР-3 боевой RAXD_BASE_URL (плейсхолдер
`https://releases.example.com/raxd`); ОР-4 macOS Gatekeeper на живом macOS; ОР-5 реальный remote-релиз/
CI на runner; LICENSE отсутствует (release.sh кладёт в архив только при наличии — добавить до публ. релиза).

## Итог
Готово к merge в develop. Эстафета — tech-writer. **Для tech-writer обязательно** (закрывает SR-105 по
способу «инспекция документации»): задокументировать модель доверия v1 (TLS + SHA256SUMS без GPG, ссылка
на П-1/ОР-1) и предупреждение про override RAXD_BASE_URL (П-3/ОР-3), плюс macOS quarantine-инструкцию.
