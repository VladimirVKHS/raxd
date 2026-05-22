# ADR-001: os.Root (Go 1.24/1.25) для traversal-safe записи файла вместо ручной filepath-валидации

## Контекст
`upload_file` записывает файл на хост по сети по относительному пути из запроса клиента (`path`).
Главный контроль безопасности (AC4 spec.md) — запись ТОЛЬКО внутрь разрешённого upload root: отвергать
абсолютные пути, `..`-escape и симлинки внутри корня, указывающие наружу, в т.ч. защититься от TOCTOU
(подмена симлинка между проверкой и открытием). Дополнительно нужны промежуточные подкаталоги внутри
корня (AC5b). Ограничения: только stdlib (новые зависимости крайне нежелательны — вендоринг, offline
Docker), кросс-платформенно Linux + darwin (Windows вне scope), проект на `go 1.25.0`.

Ручная лексическая валидация (`filepath.IsLocal`/`filepath.Clean`/`filepath.Rel`) — «purely lexical
operation … does not account for the effect of any symbolic links» (https://pkg.go.dev/path/filepath#IsLocal),
поэтому НЕ закрывает симлинки. Добавление `filepath.EvalSymlinks` оставляет окно TOCTOU
(https://go.dev/blog/osroot), `O_NOFOLLOW` контролирует лишь последний компонент пути, а openat2 +
RESOLVE_BENEATH — не в stdlib (нужен `golang.org/x/sys/unix` напрямую) и только Linux.

## Решение
Использовать **`os.Root`** (открыть upload root через `os.OpenRoot`) и выполнять ВСЕ файловые операции
записи через методы `os.Root` по относительным путям из запроса: `Root.MkdirAll` (промежуточные
подкаталоги внутри корня), `Root.OpenFile`/`Root.Create` (temp-файл), `Root.Rename` (атомарная фиксация,
см. ADR-002), `Root.Stat`/`Root.Lstat` (проверки overwrite/каталог), `Root.Remove` (очистка temp).

Гарантия из package doc (go1.25.0, верифицировано WebFetch 2026-05-22): «Methods on Root can only access
files and directories beneath a root directory. If any component of a file name passed to a method of Root
references a location outside the root, the method returns an error.» «Methods on Root will follow
symbolic links, but symbolic links may not reference a location outside the root.»
(https://pkg.go.dev/os@go1.25.0#Root). Go blog: методы Root «disallow any operations that would escape from
the root either using relative path components ("..") or symlinks»; на Unix реализованы через семейство
`openat`, что снимает TOCTOU класса «подмена симлинка после проверки» (https://go.dev/blog/osroot). Это
закрывает AC4 (абсолютный путь, `..`, симлинк наружу, TOCTOU) и AC5b (`MkdirAll` внутри корня) одним
stdlib-механизмом без новых зависимостей.

**Доступность в Go 1.25 — верифицировано фактически (WebFetch 2026-05-22).** Базовый `os.Root`/`os.OpenRoot`
+ `Open`/`Create`/`Mkdir`/`Stat` — с Go 1.24 (https://go.dev/doc/go1.24). Расширенные методы добавлены в
Go 1.25; дословная цитата release notes: «The `Root` type supports the following additional methods:» — далее
списком: `Root.Chmod`, `Root.Chown`, `Root.Chtimes`, `Root.Lchown`, `Root.Link`, `Root.MkdirAll`,
`Root.ReadFile`, `Root.Readlink`, `Root.RemoveAll`, `Root.Rename`, `Root.Symlink`, `Root.WriteFile`
(https://go.dev/doc/go1.25). Полный Index методов `*Root` на pkg.go.dev подтверждает присутствие всех
методов, на которых стоит это решение: `OpenFile`, `Create`, `Open`, `Mkdir`, `MkdirAll`, `Rename`,
`Remove`, `RemoveAll`, `Stat`, `Lstat`, `Readlink`, `Chmod` (https://pkg.go.dev/os@go1.25.0#Root). Проект на
`go 1.25.0` → весь необходимый набор доступен; **fallback на ручную валидацию не требуется**. Отдельно
подтверждено ОТСУТСТВИЕ `Root.CreateTemp` в перечне методов — имя temp генерируется кодом (см. ADR-002),
это не влияет на выбор `os.Root`.

## Альтернативы
- **Ручная лексическая валидация (`filepath.IsLocal`/`Rel`) + `os.OpenFile`/`os.MkdirAll`.** Отвергнута
  как самостоятельная: лексика не учитывает симлинки (https://pkg.go.dev/path/filepath#IsLocal), требует
  доп. `EvalSymlinks` (TOCTOU) или `O_NOFOLLOW` (контролирует лишь последний компонент) — больше ручного
  кода и шире поверхность ошибок (CWE-22 предупреждает о хрупкости ручных проверок:
  https://cwe.mitre.org/data/definitions/22.html). По сути воспроизводит то, что `os.Root` даёт из
  коробки, и хуже. (`filepath.IsLocal` остаётся полезен как дешёвый ранний отказ для явно абсолютных/
  `..`-путей до открытия корня — но НЕ как единственная защита.) ПРИМЕЧАНИЕ: технически это резервный путь,
  ЕСЛИ бы методов `os.Root` не хватало — но проверка перечня методов (см. Решение) показала, что все
  нужные методы присутствуют в Go 1.25, поэтому fallback НЕ активируется.
- **Голый `strings.HasPrefix(filepath.Clean(abs), root)`.** Отвергнут: строковый префикс ≠ вложенность
  каталога (`/root` ловит `/root2`), требует trailing separator, не покрывает симлинки. CWE-22.
- **openat2 + RESOLVE_BENEATH/RESOLVE_NO_SYMLINKS (Linux 5.6+).** Отвергнут: НЕ в stdlib `syscall`
  (`Openat` есть, `Openat2`/`RESOLVE_*` нет → нужен `golang.org/x/sys/unix` = новая зависимость в графе
  использования) и **Linux-only** (на darwin нет) → нарушает кросс-платформенность и предпочтение stdlib.
  https://pkg.go.dev/syscall#Openat
- **`EvalSymlinks`-проверка + обычный open.** Отвергнута: проверка отдельно от открытия = TOCTOU
  (https://go.dev/blog/osroot).

## Последствия
- Плюсы: AC4 (traversal + симлинк наружу + TOCTOU) и AC5b закрыты одним механизмом stdlib;
  кросс-платформенно (Linux + darwin — оба Unix/openat); промежуточные каталоги — `Root.MkdirAll`;
  меньше ручного кода безопасности → меньше шансов на ошибку; **новых зависимостей нет**; наличие всех
  нужных методов в Go 1.25 верифицировано (WebFetch).
- Минусы / цена: `os.Root` «defends against symlink traversal but **does not limit traversal of mount
  points**» (https://go.dev/blog/osroot) — bind-mount внутри корня, ведущий наружу, не блокируется (низкий
  риск в контейнерном демоне baseline §6; зафиксировать в threat-model); `Root.Chmod`/`Chown`/`Chtimes`
  **по имени** уязвимы к race на Unix (https://pkg.go.dev/os@go1.25.0#Root) → права выставлять по fd
  (см. ADR-002), а не через `Root.Chmod`-по-имени; `os.Root` не имеет `CreateTemp` → имя temp генерируем
  кодом (ADR-002); «implementation prioritizes correctness and safety over performance» (несущественно
  для одного файла ≤ `max_body_bytes`).
- Влияние на стек: согласуется со STACK.ru.md (stdlib предпочтительна, новых зависимостей нет; вендоринг
  не затрагивается). Требует Go ≥1.25 для `Root.MkdirAll`/`Root.Rename`/`Root.Chmod` — проект уже на
  `go 1.25.0`.

## Статус (proposed|accepted)
accepted — Ратифицирован гейтами security-guardian + architect-guardian (file-upload), 2026-05-22.
Границы os.Root (mount points вне гарантий; обход race `Root.Chmod` через chmod по fd, ADR-002) приняты
security в specs/file-upload/threat-model.md (раздел «Принятые отклонения»).
