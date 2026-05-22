# security-guardian — задача `service-install`

**Verdict: pass.** Guardian (read-only). Дата: 2026-05-22. Сохранено дирижёром (red line 1).

Проверены `threat-model.md` + `security-requirements.md` (SR-83..SR-96, без пропусков) против контракта
security, red line 4 и обязательного SECURITY-BASELINE §1-6.

## Подтверждено
- **Baseline покрыт, не ослаблен:** не-root euid!=0 (R-E1/SR-83/SR-84, проверка /proc/<pid>/status);
  порт<1024 → ровно CAP_NET_BIND_SERVICE, не setuid/root (SR-85); **StateDirectoryMode=0700 явно, не дефолт
  systemd 0755** (R-S2/SR-89) — главная ловушка systemd поймана; unit/plist root:root 0644 не world-writable
  (R-S1/SR-88); аудит journald drop-in с лимитами + тест journalctl --disk-usage (§4/SR-94); сборка/тест в
  Docker офлайн vendor (§6/SR-96); секреты не в выводе (SR-95).
- **Анти-инъекция в шаблоны конкретна** (SR-90): allowlist символов, запрет \n\r/управляющих/=, числовой
  Port, абсолютный ExecPath, дерив директив из типизированного NeedNetBindCap, отказ ДО записи + тест-векторы
  (User="raxd\nExecStart=/bin/sh" и т.п.) — контракт, не «будьте осторожны».
- **Модель угроз полна:** эскалация (R-E1..E4), инъекция unit/plist (R-T1), world-writable→подмена команды
  (R-S1), неполный uninstall (R-E4/SR-93), полу-установка (R-D2/SR-92), утечка секретов (R-I1), окружение
  PATH/HOME (R-T2/SR-91), запуск менеджера без shell (R-T2). Зияющих пропусков нет.
- **Отклонения обоснованы+компенсированы:** П-1 (NoNewPrivileges опущен только при порте<1024; ProtectSystem/
  Home/PrivateTmp сохранены; дефолт 7822 полностью hardened; обязательство верифицировать перед релизом — ОР-1),
  П-2 (uninstall не удаляет raxd — UID-reuse; снимается всё привилегиеносное — ОР-3), П-3 (journald-лимиты
  per-host — ОР-2; цель AC8 достигнута + fallback logrotate). macOS-непроверяемость — честно как ограничение
  среды (AC13/ОР-4), не снятие требования.
- **Закрытие прежних ОР механизмом, не декларацией:** command-exec ОР-1/file-upload ОР-U1 (root) → не-root
  раскладка raxd:raxd euid!=0; command-exec ОР-2/file-upload ОР-U4 (ротация) → journald drop-in (per-host
  граница честно вынесена в П-3).
- Каждое SR проверяемо с привязкой AC/угроза/baseline; reference не дублируется; кода нет; русский.

## Незначительные nit (НЕ блокируют)
1. Стилистическое расхождение нумерации R-T1↔SR-90 / R-T2↔SR-91 (связи целостны).
2. SR-88: «нет 0022» vs активы «нет 0002/0020» — эквивалентно (group/other write запрещены).
3. macOS StandardErrorPath=/var/log/raxd/raxd.log — режим лог-файла на macOS явно не зафиксирован в SR
   (на Linux защищён journald); покрыто оговоркой ОР-4 + newsyslog в открытых вопросах system-dev. Держать в
   поле зрения при macOS-релизе.

## Итог
pass — переход к (cli-ux ‖ system-dev). Security-артефакты править не требуется.
