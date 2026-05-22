# file-upload — живая проверка в Docker (дирижёр)

Выполнена дирижёром после merge `feature/file-upload` в `develop` (мандат «проверять самому в
Docker», SECURITY-BASELINE §6). Дата: 2026-05-22.

## Среда

- Образ пересобран с develop-кодом: `docker build --target build -t raxd-build .` (go vet + build — ОК).
- Контейнер `raxd-fileupload-demo`: `bind_addr: 0.0.0.0`, порт 7822, публикация `127.0.0.1:8443->7822`.
- Конфиг для проверки: `upload.max_file_bytes: 64` (малый лимит, чтобы дёшево проверить too-large
  через handler, а не через транспортный 400). Остальное — дефолты: root `<StateDir>/uploads`
  (= `/root/.local/state/raxd/uploads`), `default_mode 0600`, `overwrite_default false`,
  `deny_root false`.
- Демон в контейнере работает от **root (euid==0)** — намеренно демонстрирует root-WARN (AC11/SR-77).
- Ключ создан: fingerprint `82b019057dfe`. Тело ключа в проверке не приводится (не утекает в логи).

## Результаты end-to-end (curl `https://127.0.0.1:8443/mcp`, Bearer, `-k`, stateless — без session)

| # | Проверка | Ожидание | Факт | Итог |
|---|---|---|---|---|
| 1 | `tools/list` | ping+server_info+execute_command+upload_file | все четыре присутствуют | ✅ |
| 2 | upload `notes/hello.txt` (`aGVsbG8K`=`hello\n`) | success, mode 0600, 4 поля, без isError | `path=notes/hello.txt size=6 overwritten=false mode=0600`; isError опущен | ✅ |
| 3 | traversal `../etc/passwd` | isError(deny), файла вне корня нет | `isError:true` «path is outside the upload root»; `/etc/passwd` нетронут (839B) | ✅ |
| 4 | повтор `notes/hello.txt` без overwrite | isError(deny), файл не изменён | `isError:true` «file already exists (set overwrite to replace)» | ✅ |
| 5 | mode `04000` (setuid) | isError(deny) | `isError:true` «invalid file mode» | ✅ |
| 6 | too-large (100B > max_file_bytes=64) | isError(deny), файла нет | `isError:true` «file too large: exceeds max_file_bytes»; `big.bin` отсутствует | ✅ |
| 6b | **F-1 live:** mode `010000` (бит вне 0o777) | isError(deny) | `isError:true` «invalid file mode»; `weird` отсутствует | ✅ |
| 6c | явный mode `0640`, авто-подкаталоги `sub/dir/` | success, mode 0640, каталоги 0700 | `path=sub/dir/app.conf size=8 mode=0640`; каталоги 700 | ✅ |
| 6d | лишнее поле `owner` (strict schema) | isError, ничего не создано | `isError:true` «unexpected additional properties ["owner"]» (SDK) | ✅ |

## Состояние ФС (docker exec, upload root `/root/.local/state/raxd/uploads`)

- Создано: `notes/hello.txt` (**600**, 6B), `sub/dir/app.conf` (**640**, 8B); авто-подкаталоги
  `notes`, `sub`, `sub/dir` — все **700**.
- Отвергнутые НЕ созданы: `tool`, `weird`, `big.bin`, `x.txt` — все absent.
- Traversal: `/etc/passwd` нетронут (839B, оригинальная первая строка root:x:0:0); каталога `/etc/etc` нет.
- Осиротевших temp-файлов (`.raxd-upload-*`) нет — атомарность temp→rename + defer cleanup подтверждена.

## Аудит (фактический рендер из stderr контейнера)

- Успех: `level=info msg=MCP fp=82b019057dfe remote=172.17.0.1:.. tool=upload_file result=ok
  path=notes/hello.txt size=6` (и `... path=sub/dir/app.conf size=8`). Содержимое НЕ логируется.
- Deny (по одному на кейс): `level=warn msg=DENY ... tool=upload_file reason=traversal path=../etc/passwd`;
  `reason="file already exists" path=notes/hello.txt`; `reason="invalid file mode" path=tool`;
  `reason="file too large: exceeds max_file_bytes" path=big.bin`; `reason="invalid file mode" path=weird`.
- root-WARN (на КАЖДЫЙ вызов, euid==0): `level=warn msg=WARN ... tool=upload_file
  reason="running-as-root: raxd writing files as root (euid==0); ensure raxd runs as non-root"` —
  **без поля `path=`** (root-проверка предшествует парсингу пути). Это вживую подтверждает doc-fix
  tech-writer-guardian Issue 1 (path= в WARN опциональный/отсутствует).

Разграничение поле→рендер подтверждено вживую: Result success→`msg=MCP ... result=ok`+path/size;
deny→`msg=DENY reason=`+path (без ключа result=); warn(root)→`msg=WARN reason=running-as-root` (без path).

## Безопасность (live)

- Тело ключа `rax_live_...` в логах: **0 совпадений**.
- Содержимое файлов (base64 `aGVsbG8K` / `a2V5PXZhbAo=` / строка `key=val`) в логах: **0 совпадений**.
- Запись строго внутри upload root (os.Root): traversal/абсолютный путь отвергнуты, файла вне корня нет.
- Strict-схема SDK отвергает лишнее поле (`owner`) ДО хендлера — файл не создаётся.

## Примечания

- root-WARN на каждый вызов — потому что демо-контейнер запускает raxd от root; это и есть живое
  подтверждение детекции euid==0 (ОР-U1). В проде раскладка не-root (будет в service-install); для
  жёсткого запрета — `upload.deny_root: true` (тогда WARN + отдельный DENY с `path=`).
- too-large проверен на пути handler (case 2 из size-limit note доки) при малом `max_file_bytes=64`;
  транспортный 400 при теле > `max_body_bytes` — отдельный случай (case 1, документирован в mcp.md).

**Вывод: upload_file работает end-to-end в Docker; безопасность (os.Root confinement, mode-политика
включая F-1, no-overwrite, лимит размера, атомарность без осиротевших temp, аудит с path/size и без
секретов/содержимого, root-WARN) подтверждена вживую. file-upload закрыт.**
