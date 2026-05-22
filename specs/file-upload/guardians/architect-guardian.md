# architect-guardian — задача `file-upload`

**Итоговый verdict: pass** (раунд 1). Опасная фича. 4 finding info-уровня (не блокируют).
Сохранено дирижёром.

Артефакты: plan.md (~147 стр.) + ADR-003 (mode-политика). Сверено с реальным кодом (cmdexec/,
exec_tool.go, audit.go, config.go, paths.go, serve.go, server.go, keystore.go).

## Покрытие AC — все 20 покрыты
AC1 (AddTool, без CLI), AC2 (additionalProperties:false), AC3 (UploadOutput без abs-пути), AC4
(os.Root traversal+симлинк+TOCTOU→ErrTraversal), AC5a (StateDir/uploads 0700), AC5b (Root.MkdirAll),
AC6 (base64 CorruptInputError), AC7 (DecodedLen+точная len, temp cleanup), AC8 (Root.Stat+overwrite),
AC9 (chmod по fd, 0600, UID демона), AC10 (temp O_EXCL+Sync+Rename+fsync-dir+defer), AC11 (euid==0
WARN + deny_root), AC12/19 (собственный аудит, одна запись), AC13 (content не логируется), AC14
(ErrIsDir/диск/ErrBadMode, не паникует), AC15 (секция upload+валидация на старте), AC16 (лимит под
bodyLimit), AC17/18 (auth/rate-limit транспорта), AC20 (stdlib, офлайн-юниты). Потерянных нет.

## Контракты против кода — реальны
NewHandler +uplCfg (server.go:40 реален); AuditRecord isUpload-ветка зеркально isExec (audit.go:77);
config.go секция upload зеркально Exec; FingerprintFromContext/RemoteAddrFromContext реальны; os.Root
методы Go 1.25 (research verified), temp через crypto/rand+O_EXCL (Root.CreateTemp нет); chmod по fd
(обход Root.Chmod-race, образец keystore); имя internal/fileupload свободно.

## Безопасность + расчёт лимита — дыр нет, арифметика верна
traversal/симлинк/TOCTOU (os.Root+filepath.IsLocal), атомарность (нет частичного/temp файла),
права (chmod по fd umask-независимо + ADR-003 запрет setuid/setgid/sticky/world-writable), content не
логируется, наследование auth/rate-limit. Расчёт: max_file_bytes=716800 → base64 ceil(716800/3)*4=
955736 + JSON overhead ~250 ≈ 955986 < 1048576 (1 MiB), запас ~92 KiB. AC16 выдержан. ВЕРНО.

## Решения по развилкам — конкретны
Q1 root=<StateDir>/uploads 0700; Q2 ADR-003 mode-маска 0777 без опасных битов, дефолт 0600;
Q3 отдельный upload.deny_root (дефолт false/WARN); Q4 max_file_bytes=716800; Q5 AuditRecord.Path/Size
+ isUpload-ветка. Развилок «на выбор» нет.

## Findings (все INFO — для developer, не блокируют)
- **#1.** Смена сигнатуры NewHandler ломает 14 тестовых вызовов (4 файла) + serve.go:104 — developer/qa
  обновят (видно компилятором). План пишет «serve.go + mcp-тесты» без числа.
- **#2.** Валидация max_file_bytes/default_mode в buildConfig — НОВАЯ (exec-поля сейчас не валидируются);
  формулировка «зеркало exec» слегка вводит в заблуждение, но поведение верное (AC15).
- **#3.** «EnsureDirs-аналог» для upload-корня — это новый код os.MkdirAll(uploadRoot,0700) в serve.go,
  не вызов существующего EnsureDirs (фиксированный список).
- **#4.** plan ~147 стр. (норма 30-100) — обосновано плотностью 20 security-AC, неделимость в spec.

## ADR-003 — валиден
Контекст→решение (ParseMode маска 0777, запрет setuid/setgid/sticky/world-writable→ErrBadMode, дефолт
0600)→альтернативы(3)→последствия→зависимость security→статус proposed. Согласован с ADR-002 (chmod
по fd) и AC9. Max допустимый mode = 0775.

## Зависимости от security (перечислены в плане)
mount points вне гарантий os.Root; Root.Chmod-race обойдён chmod по fd; mode-политика ADR-003;
root-политика (отдельный deny_root); число max_file_bytes; представление полей аудита path/size.

## Что хорошо
Один подход, точное следование образцу command-exec (аудит без withAudit, isUpload зеркально isExec,
секция конфига, root-WARN), контракты реалистичны с типизированными ошибками, расчёт лимита прозрачен
и верен, безопасность до уровня fd-chmod/O_EXCL/defer-cleanup/content-never-logged, AC не тронуты,
новых зависимостей нет.

## Резюме
pass. info-findings — справочно developer. Передаётся security.
