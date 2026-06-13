# docs-outline.md — обновление документации raxd под две смерженные фичи

Роль: tech-writer. Ветка: `feature/docs-hardening` (от `main`). Язык docs/README: **английский**;
служебные пояснения этого плана — русские (конвенция docs-outline). Дата: 2026-06-13.
Автор продукта: **Vladimir Kovalev, OEM TECH**.

Задача: актуализировать продуктовую документацию `docs/**` (+ README) под две уже смерженные в `main`
фичи — **upload disk-quota** (`upload.max_total_bytes`) и **service uninstall --purge --yes** —
строго по факту реализованного кода. Не выдумывать; сверено с исходниками и спеками.

## Источники истины (сверено)

| Артефакт | Что подтверждает |
|----------|------------------|
| `internal/config/config.go` | поле `UploadConfig.MaxTotalBytes`; default `upload.max_total_bytes=0`; валидация `< 0` отвергается на старте; `0` = отключён; не связан с `max_file_bytes` |
| `internal/fileupload/quota.go` | `ErrQuotaExceeded` («upload denied: total upload quota exceeded»); учёт всех regular-файлов в upload root через `WalkDir`; симлинки не учитываются; fail-closed при ошибке обхода; per-root мьютекс; при `0` обход не задействуется |
| `internal/mcp/upload_tool.go` | маппинг `ErrQuotaExceeded` → `Result:"deny"`, reason «total upload quota exceeded»; ровно одна deny-запись |
| `internal/cli/service.go` | флаги `--purge` / `--yes`; барьер `--purge` без `--yes` (warning + ненулевой exit, ничего не удаляется); вывод purge success/idempotent; маппинг sentinel-ошибок; `uninstall` без `--purge` byte-for-byte неизменён |
| `internal/service/purge.go` | `validatePurgePath` — защита путей (не `/`, не `$HOME`, не blocked-roots, EvalSymlinks, allowedRoots-prefix) |
| `specs/upload-quota/spec.md` | AC1–AC12; out-of-scope: per-key квоты, content-inspection, FS/OS-квоты, TTL |
| `specs/service-purge/spec.md` + `ux-spec.md` | AC1–AC10; тексты вывода (английские); аудит-ключи; out-of-scope |
| `install.sh` | дефолт = GitHub Releases (`vladimirvkhs/raxd`), плейсхолдер example.com убран; канонический one-liner работает; политика latest vs pinned |
| `LICENSE` (root) | MIT, Copyright 2026 Vladimir Kovalev, OEM TECH — присутствует |

## Карта изменений `docs/`

| Файл | Что обновлено |
|------|----------------|
| `docs/production-readiness.md` | Ключевое. §1 закрыт (GitHub Releases), §5 закрыт (LICENSE), §7 закрыт на уровне приложения (`max_total_bytes`; остаточное — нет per-key/content-inspection), §6 смягчён (`--purge`), §2 GPG = deferred-by-decision, §3/§4 macOS остаются; таблица At a glance актуализирована. Якоря §3/§6/§7 сохранены прежними ради входящих ссылок из troubleshooting |
| `docs/configuration.md` | Добавлен `upload.max_total_bytes` в YAML-пример секции `upload`, в таблицу ключей, в startup-validation (отрицательное → ошибка на старте), в notes; обновлён пункт disk-quota; заметка про `--purge` в service-layout |
| `docs/file-upload-security.md` | §7 (заголовок сохранён): общий лимит как смягчение disk-DoS; аудит-таблица DENY включает total-quota; остаточное — per-key/content-inspection; ссылки |
| `docs/service-management.md` | §3 (заголовок сохранён) расширен: добавлен подраздел `uninstall --purge --yes` (что удаляет, барьер, идемпотентность, guardrails, остаток вручную без --purge); §5/§7/security summary |
| `docs/commands.md` | В `raxd service uninstall`: usage с `--purge`/`--yes`, флаги, барьер, purge-вывод (success/idempotent/refusals), exit-коды; summary-таблица; serve-замечания про total-quota |
| `README.md` | Статус-блок (GitHub Releases, LICENSE сделан, GPG = deferred); таблица What works (purge, disk quota, host, LICENSE); Installation (one-liner + latest/pinned); Coming next (LICENSE/host убраны из pending, GPG — deferred); License-секция → MIT |

## Принципы и цель аудитории

- Аудитория: оператор/админ, разворачивающий raxd на реальном хосте; ИИ-агент-интегратор.
- Цель: дать точную картину «что закрыто / что осталось» и научить настраивать новые рычаги
  (`upload.max_total_bytes`, `service uninstall --purge --yes`).
- Стиль: сохранён язык и тон существующих файлов (английский, «as implemented in the code today»,
  честные пометки об ограничениях). Тексты команд/вывода — английские, дословно из кода/ux-spec.
- Красная линия: документировано ТОЛЬКО реальное. Открытых вопросов нет — обе фичи смержены,
  поведение подтверждено кодом.

## Открытые вопросы

None. Поведение обеих фич полностью подтверждено исходным кодом и спеками; расхождений docs↔код,
требующих эскалации, при сверке не выявлено.

## Что проверить tech-writer-guardian

1. `upload.max_total_bytes`: default `0`=disabled, `< 0` отвергается на старте, не связан с
   `max_file_bytes`, учёт всех существующих regular-файлов, deny с нейтральным сообщением + один
   deny-аудит — сверить с `config.go`/`quota.go`/`upload_tool.go`.
2. `service uninstall --purge --yes`: барьер без `--yes` (ненулевой exit, ничего не удалено),
   идемпотентность, что удаляет (user + state/config dirs), `uninstall` без `--purge` неизменён —
   сверить с `internal/cli/service.go`.
3. production-readiness: §1/§5/§7 помечены closed корректно; §6 смягчён; §2 GPG = deferred-by-decision
   (не «pending» как обязательство); §3/§4 macOS остаются; таблица At a glance согласована с разделами.
4. install.sh: канонический URL `https://github.com/vladimirvkhs/raxd/releases/latest/download/install.sh`
   и политика latest vs pinned — сверить с `install.sh`.
5. LICENSE: MIT, Vladimir Kovalev/OEM TECH — README/installation/production-readiness отражают наличие.
6. Автор `Vladimir Kovalev, OEM TECH` сохранён во всех затронутых файлах.
7. Перекрёстные ссылки/якоря резолвятся: §3/§6/§7-якоря в service-management/production-readiness/
   file-upload-security сохранены прежними; внутренние ссылки на новые подразделы
   (`#full-cleanup-in-one-command-uninstall---purge---yes`,
   `#full-cleanup-raxd-service-uninstall---purge---yes`) корректны.
