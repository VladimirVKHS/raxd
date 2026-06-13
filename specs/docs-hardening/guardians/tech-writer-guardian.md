# tech-writer-guardian — docs-hardening

**Verdict: pass** (после доправки installation.md/troubleshooting.md)

Проверены: docs/{production-readiness,configuration,file-upload-security,service-management,commands,installation,troubleshooting}.md, README.md против кода и install.sh.

Подтверждено:
- upload-quota (`upload.max_total_bytes`, 0=выкл, deny, нейтральное сообщение) и service-purge (`--purge`/`--yes`, барьер, идемпотентность, guardrails) задокументированы строго по коду; тексты вывода совпадают до символа с `internal/cli/service.go` и ux-spec.
- production-readiness: §1 host closed (GitHub Releases реальны), §5 LICENSE closed (MIT присутствует), §7 quota closed на уровне приложения, §6 uninstall mitigated через --purge (by-design дефолт сохранён), §2 GPG deferred-by-decision (честно), §3/§4 notarization/Gatekeeper open.
- installation.md/troubleshooting.md исправлены: устаревшее «нет хоста / плейсхолдер example.com / exit 5» убрано; `RAXD_BASE_URL` дефолт = пусто→GitHub Releases; таблица переменных корректна; рабочий canonical one-liner. Сверено с install.sh.
- Единственное упоминание releases.example.com — в production-readiness.md как «удалён» (допустимо).
- Конкурентная правка двух tech-writer не повредила файлы: дублей/обрывов/битых якорей/stray-тегов нет.
- Все 7 docs внутренне согласованы (host/LICENSE/quota/purge/GPG/Gatekeeper). Автор Vladimir Kovalev, OEM TECH сохранён. tech-writer код не менял.
