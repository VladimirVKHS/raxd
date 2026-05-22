# tech-writer-guardian — задача `docs` (финальная сверка всей документации продукта)

**Verdict: pass.** Guardian (read-only). Дата: 2026-05-22. Сохранено дирижёром (red line 1).
ЗАВЕРШАЮЩИЙ гейт дорожной карты raxd.

Проверены README.md + docs/{installation,commands,configuration,mcp,service-management,development,
troubleshooting,execute-command-security,file-upload-security,production-readiness}.md + docs-outline.md
против контракта tech-writer, red lines и фактического кода (cmd/raxd, internal/*, install.sh, Makefile).

## Подтверждено
- **Точность docs↔код:**
  - development.md: сигнатура `NewHandler(ver string, audit server.AuditFn, execCfg cmdexec.Config,
    uplCfg fileupload.Config)` == server.go:42; 4 инструмента (ping/server_info/execute_command/
    upload_file) == server.go:55-58; раскладка проекта реальна (cmdexec/fileupload/service/exec_tool/
    upload_tool/audit + install.sh/scripts/Makefile); serve.go:137 вызов совпадает. Устаревшее
    «tools do not exist yet» устранено.
  - mcp.md: ExecInput{command,args,timeout_ms,cwd} (нет env), ExecOutput 7 полей, UploadInput
    {path,content,overwrite,mode}, UploadOutput 4 поля, протокол 2025-11-25, GET→405, server_info
    {name,version,protocolVersion}, ветки аудита DENY/FAIL/WARN — всё совпадает с
    internal/mcp/{tools,exec_tool,upload_tool}.go.
  - version.Info() = `raxd %s (commit %s, built %s)` (не вставляет `v`); пример `raxd v0.1.0 …` корректен
    (`v` из git-тега). status.go суффикс `(not found, defaults applied)` — README/commands верны.
    `config port` — stub, помечен везде.
- **Честность:** production-readiness.md — 10 пунктов (нет хостинга ОР-3/5, нет GPG ОР-1, нет нотаризации
  ОР-2, macOS вне Docker ОР-4, нет LICENSE, UID-reuse uninstall, нет disk-quota upload, args/path
  логируются дословно, root-WARN, нет sandboxing/mTLS) со ссылками на specs/distribution/threat-model.md;
  RAXD_BASE_URL — placeholder; LICENSE — явно отсутствует.
- **Согласованность:** нет остаточных `1.0.0`/`2025-06-01` (grep 0); единый `v0.1.0`; golang:1.25 ==
  go.mod go 1.25.0. Якоря перекрёстных ссылок на production-readiness.md резолвятся (#6-…-uid-reuse,
  #4-…-outside-docker-ор-4 и др.).
- **Полнота пути:** установка → команды → MCP → конфигурация → сервис → troubleshooting →
  production-readiness; нет пустых/оборванных разделов; нет битых ссылок.
- **Автор** Vladimir Kovalev, OEM TECH — в 11 артефактах (10 docs + README).
- **Red lines:** docs на английском; docs-outline (внутренний) на русском (допустимо); reference не
  дублируется; код не менялся; stray `</content>` отсутствуют.

## Наблюдения (не блокируют)
- **А.** Возможная неэффективность `tablewriter.WithBorders` (deprecated API читает Config() как копию)
  в `internal/cli/key.go:208-217` — комментарий `"no outer border"` может расходиться с фактическим
  рендером полной рамки. Это вопрос КОДА (key-management, известная косметика), не документации; docs
  совпадают с фактическим выводом. Без запуска однозначно не разрешить.
- **Б.** docs-outline упоминает ссылки на production-readiness.md из execute/file-upload-security.md,
  которых фактически нет; но оба файла имеют собственные разделы Residual risks — пробела для понимания
  нет, контракт не нарушен.

## Несоответствия
Критических расхождений docs↔код не обнаружено.

## Итог
pass — вся документация продукта финально сверена с кодом, честна, согласована, с автором и видимой
картой остаточных рисков. Документация в итоговом состоянии. Дорожная карта raxd завершена.
