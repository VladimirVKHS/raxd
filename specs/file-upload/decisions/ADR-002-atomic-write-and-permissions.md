# ADR-002: Атомарная запись через temp+rename в корне и umask-независимые права по fd

## Контекст
`upload_file` должен записывать файл атомарно (AC10): целевой файл виден только целиком; при ошибке/
обрыве до фиксации не остаётся ни частичного целевого, ни временного файла; при перезаписи (AC8) старый
файл сохраняется до фиксации. Одновременно файл должен получать **предсказуемый, umask-независимый**
POSIX-режим (AC9, дефолт `0600`, переопределяемый полем `mode`), под UID/GID демона, без повышения
привилегий. Это нужно совместить с записью через `os.Root` (ADR-001), у которого `Root.Chmod`-по-имени
уязвим к race на Unix. Ограничения: stdlib, Linux + darwin, без новых зависимостей. В проекте уже есть
проверенный образец — `internal/keystore/keystore.go` `writeDB` (прочитан research-analyst).

Перечень методов `os.Root`, используемых ниже (`OpenFile`, `Rename`, `Remove`, `Stat`, `MkdirAll`),
а также ОТСУТСТВИЕ `Root.CreateTemp` (отсюда генерация имени temp кодом) — верифицированы фактически через
WebFetch (2026-05-22) по go.dev/doc/go1.25 и pkg.go.dev/os@go1.25.0#Root (см. ADR-001). Fallback по
доступности API не требуется.

## Решение
**Атомарность.** Повторить схему keystore через методы `os.Root` (ADR-001), все пути относительны корню:
1. `Root.MkdirAll(dir(rel), perm)` — недостающие подкаталоги внутри корня (AC5b).
2. Создать **temp-файл в том же подкаталоге, что и цель** (обязательно для атомарного rename на одной
   ФС — rename через границу устройств = `EXDEV`, не атомарен:
   https://man7.org/linux/man-pages/man2/rename.2.html) через `Root.OpenFile(tmpRel,
   O_CREATE|O_EXCL|O_WRONLY, perm)`. Уникальное имя temp генерируется самим кодом (`Root.CreateTemp`
   ОТСУТСТВУЕТ — подтверждено WebFetch, см. ADR-001) суффиксом из `crypto/rand`; `O_EXCL` гарантирует
   отсутствие коллизии (https://pkg.go.dev/os#OpenFile).
3. `(*os.File).Chmod(желаемый_mode)` **на полученном дескрипторе ДО записи содержимого** (см. «Права»).
4. Записать декодированные байты → `(*os.File).Sync()` (durability:
   https://pkg.go.dev/os#File.Sync) → `Close`.
5. Политика overwrite (AC8): `Root.Stat(target)` — если цель существует и `overwrite:false` → deny; если
   цель — каталог → deny (AC14); иначе `Root.Rename(tmpRel, targetRel)` атомарно фиксирует/заменяет
   (https://pkg.go.dev/os#Rename).
6. fsync каталога назначения (durable rename), best-effort (как в keystore).
7. **На любой ошибке/deny до фиксации** — `Root.Remove(tmpRel)` (не остаётся temp/частичного файла,
   AC7/AC10).

**Права (umask-независимо, обход race).** `os.OpenFile` создаёт файл «with mode perm **(before umask)**»
(https://pkg.go.dev/os#OpenFile) — umask демона может срезать биты, поэтому одного `perm` недостаточно для
AC9 «umask-независимый режим». Решение: после создания temp вызвать `(*os.File).Chmod(mode)` **на
дескрипторе** (chmod по fd umask не применяет: https://pkg.go.dev/os#Chmod) ДО записи содержимого (нет
окна с более широкими правами — как `tmp.Chmod(0o600)` в keystore). Это также **обходит race**
`Root.Chmod`-по-имени, который package doc отмечает как уязвимый на Unix
(https://pkg.go.dev/os@go1.25.0#Root). Дефолт `0600`; поле `mode` валидируется в разрешённом диапазоне
(диапазон/маска и запрет setuid·setgid·sticky·world-writable — политика architect/security, spec Q2).
Владелец — UID/GID демона как есть (никакого chown/setuid, AC9).

## Альтернативы
- **`Root.WriteFile` (Go 1.25) напрямую в цель.** Отвергнута: `WriteFile` truncate+write в цель **не
  атомарен** → частичный целевой файл при обрыве, потеря старого при перезаписи (нарушает AC10).
  https://pkg.go.dev/os@go1.25.0#Root
- **Запись прямо в цель с `O_CREATE|O_EXCL`/`O_TRUNC` без temp.** Отвергнута: `O_EXCL` даёт атомарный
  отказ при существовании (полезно как часть проверки), но прямая запись/`O_TRUNC` в цель нарушает AC10
  (виден частичный файл; перезапись теряет старое при сбое). https://pkg.go.dev/os#OpenFile
- **`Root.Chmod(name, mode)` по имени для прав.** Отвергнута: package doc — race на Unix
  (https://pkg.go.dev/os@go1.25.0#Root); chmod по fd безопаснее и umask-независим.
- **Только `OpenFile(..., perm)` без Chmod.** Отвергнута: umask срезает биты → режим непредсказуем,
  нарушает AC9. https://pkg.go.dev/os#OpenFile

## Последствия
- Плюсы: атомарность (AC10) и umask-независимые точные права (AC9) совмещены с traversal-safety
  (ADR-001); консистентно с проверенным образцом keystore; перезапись/цель-каталог обработаны (AC8/AC14);
  temp всегда очищается (AC7/AC10); chmod по fd обходит race `Root.Chmod`; всё на stdlib, без новых
  зависимостей.
- Минусы / цена: нужно самим генерировать уникальное имя temp (нет `Root.CreateTemp`) — несколько строк
  с `crypto/rand` + `O_EXCL`; `Stat`+`Rename` — два шага (узкое окно; для v1 однопоточной семантики
  приемлемо, rename поверх каталога вернёт ошибку как страховка); fsync-dir best-effort (как keystore).
- Влияние на стек: согласуется со STACK.ru.md (stdlib, права `0600` для чувствительных файлов; вендоринг
  не затрагивается). Совместимо с Go 1.25 (`os.Root` методы, `(*os.File).Chmod`/`Sync`).

## Статус (proposed|accepted)
accepted — Ратифицирован гейтами security-guardian + architect-guardian (file-upload), 2026-05-22.
Обход race `Root.Chmod` через chmod по fd принят security в specs/file-upload/threat-model.md (раздел
«Принятые отклонения»); число max_file_bytes подтверждено security там же.
