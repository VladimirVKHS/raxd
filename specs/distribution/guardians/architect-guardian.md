# architect-guardian — задача `distribution`

**Verdict: pass.** Guardian (read-only). Дата: 2026-05-22. Сохранено дирижёром (red line 1).

Проверены `specs/distribution/plan.md` + ADR-001..005 (accepted) против контракта architect и red lines.

## Подтверждено
- РОВНО ОДИН подход по каждой развилке (Q1-Q8): ручной релизный скрипт B (без goreleaser, ADR-001);
  install.sh bash+set -euo pipefail+тело в main+trap+SHA-проверка до установки+RAXD_BASE_URL+детект пути;
  мок-HTTP тест (python3 http.server, ADR-002); CI yaml-артефакт+локальный docker-CI (ADR-004); macOS
  идемпотентный xattr -d (ADR-005). Меню для devops не оставлено.
- Файлы с путями+контрактами: install.sh, Makefile (release/checksums/release-all/ci-local/test-install),
  scripts/release.sh, scripts/test-install.sh, Dockerfile.install, .github/workflows/{ci,release}.yml,
  LICENSE/README в архивах. Контракт install.sh детален: env RAXD_BASE_URL/RAXD_VERSION/RAXD_PREFIX, коды
  возврата 0-5 с привязкой к AC, ветки, сообщения error:/hint:. Тел скриптов нет.
- AC1-16 не изменены; AC10-формат версии дословно совпадает с internal/version.Info(). baseline §5/§6
  соблюдён (bash не dash обоснован, trap, SHA до размещения с abort; чистый debian-контейнер мок-сервер
  127.0.0.1; ci-local/test-install под docker-guard /.dockerenv; 4 цели+SHA256SUMS; ldflags).
- Вендоринг: без новой рантайм-зависимости (python3 только в тест-образе); goreleaser офлайн неустановим
  (research+ADR-001 обоснованно); STACK↔goreleaser mismatch разрешён (опциональный/неиспользуемый, как
  kardianos). Безопасность спроектирована (хэндофф security 7 пунктов, риск единого источника URL+SHA
  зафиксирован). Scope-guard корректен (Windows/пакеты/нотаризация/remote вне; сервис — service-install).
  ADR accepted, русский.

## Замечания (не блокируют)
- Формат SHA256SUMS: Chosen Approach/ADR-001 «<hash>␣␣<file>» vs Contracts `sha256sum *.tar.gz` — по сути
  сходится (GNU текстовый режим в Linux-контейнере); стоит явно зафиксировать «генерация всегда GNU
  sha256sum, проверка macOS через shasum -a 256 -c».
- RAXD_BASE_URL дефолт — плейсхолдер будущего боевого URL (следствие отсутствия remote, ADR-002) — зона
  devops при появлении remote.
- Длина plan ~95 содержательных строк — в норме.

## Итог
pass — переход к security. Дирижёру: правка STACK (goreleaser→опциональный; install.sh не регистрирует
сервис; пути установки/--prefix) — вне компетенции architect.
