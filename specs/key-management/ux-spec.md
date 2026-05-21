# UX Spec: Key Management — управление API-ключами raxd

> Контракт для: `developer` (реализует вывод `internal/cli/key.go`).
> Проверяет: `cli-ux-guardian`.
> Все тексты CLI — английские. Язык артефакта — русский.
> Согласован с: `specs/bootstrap-cli/ux-spec.md` (источник единого стиля).

---

## Принципы

1. **Иерархия по срочности.** При `key create` предупреждение о единственном показе идёт первым — до
   самого ключа. Пользователь должен прочитать предупреждение прежде, чем увидит значение.
2. **Каналы по назначению.** Декор, предупреждения и метаданные — stderr. Машиночитаемые значения —
   stdout. Правило: `raxd key create > key.txt` кладёт в файл ТОЛЬКО строку ключа, ничего лишнего.
3. **Ключ изолирован визуально.** В выводе `key create` ключ — единственный элемент с box-рамкой на
   stdout. Whitespace до и после создаёт визуальную зону «это то, что нужно скопировать».
4. **Секрет — только один раз.** Полное тело ключа появляется ровно один раз: в выводе `key create`.
   В `key list`, ошибках, логах, аудите — никогда, даже частично. Допустимы только id, label,
   fingerprint.
5. **Plain-текст как база.** Весь вывод — чистый текст с Unicode-рамками и выравниванием пробелами.
   Стилизация через `charmbracelet/lipgloss` — точка расширения (NO_COLOR-гейт обязателен).
6. **Единый тон с bootstrap.** Ошибки: `error:` + `hint:`. Таблицы: dash-разделители, левое
   выравнивание, двухпробельный отступ слева. Всё в одном стиле с `raxd status` и `raxd version`.
7. **Дружелюбность без жаргона.** Ошибки объясняют, что случилось, и говорят, что делать. Никаких
   Go-стек-трейсов, sentinel-имён или кодов возврата в тексте.

---

## Раскладка каналов: обоснование решения

### Принципиальное правило

| Тип содержимого | Канал | Обоснование |
|---|---|---|
| Баннер автора | stderr | Идентичен bootstrap: не засоряет stdout |
| Предупреждение «save it now» | stderr | Декор/предупреждение для человека |
| Само тело ключа (`rax_live_…`) | **stdout** | Единственное машиночитаемое значение; нужно для `> file` / `$(...)` |
| Метаданные (id, label, created) | stderr | Человеческое подтверждение, не данные |
| Ошибки (`error:` / `hint:`) | stderr | Стандарт bootstrap |
| Таблица `key list` | **stdout** | Машиночитаемый результат; `key list | grep …` работает |
| Подтверждение `key delete` | stderr | Статусное сообщение, не данные |

### Эффект перенаправления

```
raxd key create --name prod > key.txt
```

- `key.txt` содержит: `rax_live_XXXXX…` (одна строка, без пробелов/переносов)
- Терминал видит: баннер + предупреждение + ключ (из stdout) + метаданные — всё вместе

```
raxd key create --name prod 2>/dev/null
```

- На экране: только `rax_live_XXXXX…` (чисто для скрипта без декора)

```
raxd key list > keys.tsv
```

- `keys.tsv` содержит: таблицу (заголовок + строки), баннер — нет

---

## Состояния вывода

### `raxd key create [--name <label>]` — успех

**Каналы:** stderr (предупреждение + метаданные), stdout (тело ключа). **Exit:** 0.

**Порядок вывода на экране** (stderr и stdout интерлив в терминале):

1. Предупреждение (stderr) — первым, до ключа
2. Тело ключа (stdout) — в box-рамке, изолировано пустыми строками
3. Метаданные (stderr) — после ключа, подтверждение

**Макет (то, что видит пользователь в терминале):**

```
  ! WARNING: This key will NOT be shown again. Save it now.           <- stderr
                                                                       <- stderr (пустая строка)
┌──────────────────────────────────────────────────────────────────┐  <- stdout
│  rax_live_dGhpcyBpcyBhIHRlc3Qga2V5IGZvciBkb2N1bWVudGF0aW9u   │  <- stdout
└──────────────────────────────────────────────────────────────────┘  <- stdout
                                                                       <- stdout (пустая строка)
  id        abc123de                                                   <- stderr
  label     production-key                                             <- stderr
  created   2025-05-21                                                 <- stderr
                                                                       <- stderr (пустая строка)
```

**Что идёт на stdout (то, что попадает в файл при перенаправлении):**

```
┌──────────────────────────────────────────────────────────────────┐
│  rax_live_dGhpcyBpcyBhIHRlc3Qga2V5IGZvciBkb2N1bWVudGF0aW9u   │
└──────────────────────────────────────────────────────────────────┘

```

Ключ обёрнут в box-рамку на stdout. Box — единственный в этом выводе (баннер на stderr).
Рамка адаптивна по ширине ключа. Ключ — `rax_live_` + 43 символа base64url = ~53 символа итого;
рамка: `│  ` + ключ + `  │` + отступы.

**Что идёт на stderr (декор + метаданные):**

```
  ! WARNING: This key will NOT be shown again. Save it now.

  id        abc123de
  label     production-key
  created   2025-05-21

```

> **Требование безопасности (SR-11):** тело ключа (`rax_live_…`) печатается ровно один раз, только
> на stdout. На stderr — ни символа из тела ключа. Метаданные содержат только id/label/created.

**Вариант без label (`--name` не передан):**

```
  id        abc123de
  label     -
  created   2025-05-21
```

Значение `label` = `-` по аналогии с `key list` (D2).

**Формат ключа:** `rax_live_` + base64url без padding. Пример длины: `rax_live_` (9) + 43 = 52 символа.

**Формат метаданных:** выравнивание по образцу `raxd status` — метка 9 символов, значение с позиции
11, двухпробельный отступ слева.

---

### `raxd key create [--name <label>]` — режим чистого захвата (скрипт)

Если нужна только строка ключа без декора (скрипт, CI):

```bash
KEY=$(raxd key create 2>/dev/null)
# $KEY = "rax_live_dGhpcyBpcyBhIHRlc3Qga2V5IGZvciBkb2N1bWVudGF0aW9u\n"
# (содержит box-рамку — нужна обрезка)
```

> **Примечание по box на stdout:** box-рамка помогает человеку, но усложняет парсинг. Это принятый
> trade-off: автоматизация получает только ключ через `2>/dev/null`, но всё равно видит рамку.
> Точка расширения: флаг `--raw` (не реализовывать в `key-management`, только зафиксировать как
> будущую возможность) вывел бы голое значение без рамки на stdout.

---

### `raxd key list` — таблица ключей

**Канал:** stdout. **Exit:** 0.

**Рекомендуемый стиль:** dash-separator без внешней рамки (идентично bootstrap ux-spec).

**Стандартный макет (есть ключи):**

```
  ID            LABEL                CREATED      LAST USED
  ──────────    ────────────────     ──────────   ──────────
  abc123de      production-key       2025-05-10   2025-05-20
  f9e21b00      staging              2025-05-15   never
  c0ffee01      -                    2025-05-01   2025-05-21
  9a7b3e12      ci-runner            2025-04-28   2025-05-18
```

**Ширина колонок:**

| Колонка   | Ширина  | Правило усечения                 |
|-----------|---------|----------------------------------|
| ID        | 12 сим. | Обрезать до 12, без `…`          |
| LABEL     | 20 сим. | Обрезать с суффиксом `…`         |
| CREATED   | 10 сим. | Формат `YYYY-MM-DD`              |
| LAST USED | 10 сим. | `YYYY-MM-DD` или `never`         |

Итого: ~56 символов — укладывается в 60-колоночный терминал.

**Дополнительные правила таблицы:**

- Значение id — первые 12 символов hex-id (D5: 8 байт crypto/rand → hex = 16 символов; усечение без `…`)
- `label = -` для ключей без label (D2)
- `last-used = never` для ключей, которые ни разу не использовались
- `last-used` может отставать от реального времени последнего использования (план: `FlushUsage`
  сбрасывает буфер периодически, не на горячем пути). Значение в таблице — «не ранее чем», не «точно».
  Это поведение не отражается в выводе (не добавлять суффикс `~` или `approx`).
- Отозванные (`revoked`) ключи не отображаются (D3). Нет флага `--all` в v1.

**Что НЕ показывает `key list` (SR-12):**

- Тело ключа (`rax_live_…`)
- Хэш ключа или salt
- fingerprint
- статус `revoked`
- любые другие внутренние поля записи

**Пустое хранилище:**

```
  No API keys found.
  hint: create your first key with "raxd key create --name <label>"
```

`hint:` всегда строчными — единое правило проекта. Иллюстративный пустой список в
`specs/bootstrap-cli/ux-spec.md` (раздел заглушки `key list`) использовал `Hint:` с заглавной —
данный контракт заменяет его и устанавливает строчный `hint:` как норму для всего дерева `raxd key`.

**Реализуется через `olekukonko/tablewriter`** с отключёнными внешними рамками, выравниванием по
левому краю, разделителем `──` под заголовком. Двухпробельный отступ слева через `SetColMinWidth`
или добавлением пустой первой колонки.

---

### `raxd key delete <id>` — успех (мягкий отзыв)

**Канал:** stderr. **Exit:** 0.

```
  key abc123de revoked
  hint: the key can no longer be used for authentication
```

Формулировка `revoked`, не `deleted`: запись сохраняется для аудита (D3, plan). Точное слово важно:
пользователь должен понимать, что запись не уничтожена, а деактивирована.

Двухпробельный отступ слева, как везде.

---

### `raxd key delete <id>` — ошибки

**Канал:** stderr. **Exit:** 1 (ненулевой).

**Несуществующий id:**

```
error: key "abc123de" not found
  hint: run "raxd key list" to see available key IDs
```

ID отражается в сообщении: пользователь видит, что система прочитала именно то, что он ввёл.
ID — не секрет (D5: отдельный случайный id, не производный от тела ключа).

**Уже отозванный id:**

```
error: key "abc123de" is already revoked
  hint: run "raxd key list" to see active keys
```

**Пропущен обязательный аргумент `<id>`:**

```
error: key delete requires an id argument
  hint: run "raxd key list" to find the key ID, then use "raxd key delete <id>"
```

---

## Тексты ошибок и edge-cases

### Невалидный/слишком длинный label (>64 символов)

**Канал:** stderr. **Exit:** 1.

```
error: label is too long (max 64 characters)
  hint: choose a shorter label and try again
```

Без указания введённой длины или самого значения (значение может быть чувствительным для
пользователя; кроме того, длинный label в ошибке засоряет вывод).

### Повреждённый `keys.db` (`ErrCorrupt`)

**Канал:** stderr. **Exit:** 1.

```
error: key store is corrupted or unreadable
  hint: check file permissions on keys.db (must be readable by current user)
  hint: do not attempt to repair the file manually — contact support if data recovery is needed
```

Два `hint:`-a: первый — самостоятельное действие, второй — предупреждение не делать деструктивных
действий. Файл не перезаписывается автоматически (SR-22).

### Нет прав на чтение/запись `keys.db`

**Канал:** stderr. **Exit:** 1.

```
error: cannot open key store: permission denied
  hint: check that the current user owns the file at the keys path shown in "raxd status"
```

Путь к keys.db не включается напрямую в ошибку (виден через `raxd status`) — избегаем дублирования
пути в выводе команды.

### `keys.db` заблокирован другим процессом (flock-таймаут)

**Канал:** stderr. **Exit:** 1.

```
error: key store is locked by another process
  hint: wait a moment and try again; if the problem persists, restart raxd
```

### Ошибка записи при `key create` (диск полон, etc.)

**Канал:** stderr. **Exit:** 1.

Если ключ уже сгенерирован и выведен на stdout, но запись в `keys.db` провалилась — ключ бесполезен
(нельзя верифицировать), но пользователь его уже увидел. Это критичная ситуация:

```
error: key was generated but could not be saved to key store
  hint: do not use the key shown above — it cannot be verified
  hint: free up disk space or check write permissions, then run "raxd key create" again
```

> **Важно:** вывод ключа на stdout УЖЕ произошёл до ошибки. Это поведение обусловлено тем, что
> ключ генерируется до записи. Hint явно предупреждает не использовать показанный ключ.

---

## Безопасность вывода — явные требования

Раздел фиксирует требования к выводу, производные от SR-11…SR-15.

1. **Тело ключа — только при `key create`, только на stdout, только один раз.**
   Никакая другая команда, флаг, режим или внутренний путь не выводит тело ключа.

2. **`key list` не раскрывает секрет ни при каких условиях.**
   Вывод содержит только: id, label (или `-`), created (`YYYY-MM-DD`), last-used (`YYYY-MM-DD`
   или `never`). Хэш, salt, fingerprint, статус `revoked` — не выводятся.

3. **Ошибки оперируют id, label, fingerprint — не телом ключа.**
   Текст ошибки может включать id (короткий случайный идентификатор). Тело `rax_live_…` —
   никогда.

4. **Аудит-записи (stderr daemon/journald) не раскрывают секрет.**
   `charmbracelet/log` пишет структурную запись: `timestamp`, `action`, `id`, `fingerprint` (8-12
   hex-символов `sha256(тело)`, SR-15). Тело, хэш, salt — не попадают в лог.

5. **fingerprint не является телом ключа и не позволяет его восстановить.**
   Fingerprint — короткий префикс (8-12 символов) `sha256(тело)` без соли. Используется только в
   аудите, не в пользовательском выводе команд.

6. **ID в выводе — безопасен.** ID — случайный идентификатор записи, не производный от секрета
   (D5, SR-5). Его показ в ошибках, подтверждениях и таблице не раскрывает ключ.

---

## Баннер автора

Баннер наследуется без изменений из `specs/bootstrap-cli/ux-spec.md`. Команды `key create`,
`key list`, `key delete` — подкоманды дерева `raxd`; баннер печатается через `PersistentPreRun`
корневой команды на **stderr**.

**Wide-макет (терминал >= 52 символов):**

```
┌──────────────────────────────────────────────────┐
│  raxd  —  Remote Access Daemon                   │
│  v1.0.0  ·  commit abc1234  ·  built 2025-06-01  │
│  Vladimir Kovalev, OEM TECH                      │
└──────────────────────────────────────────────────┘
```

Строка автора `Vladimir Kovalev, OEM TECH` обязательна. Полный контракт баннера (narrow-макет,
fallback без рамки, dev-дефолты) — в `specs/bootstrap-cli/ux-spec.md` (не дублируется).

---

## Цвета и стиль (lipgloss)

### Текущее состояние: plain-текст

На стадии `key-management` стилизация через `charmbracelet/lipgloss` **не подключается** —
идентично bootstrap-cli (решение architect). Весь вывод — plain Unicode с пробельным выравниванием.

Палитра наследуется из bootstrap-cli без изменений:

| Роль          | Цвет (ориентир) | Применение в key-управлении                |
|---------------|------------------|--------------------------------------------|
| Primary       | `#5FD7FF` (cyan) | Заголовки таблицы `key list`               |
| Muted         | `#767676` (gray) | `never`, `-`, `hint:`, даты               |
| Success       | `#5FFF87` (green)| Подтверждение `key revoked`                |
| Warning       | `#FFD75F` (yellow)| Строка `WARNING` при `key create`         |
| Error         | `#FF5F5F` (red)  | Префикс `error:`                           |
| Author/Accent | `#D7AF87` (warm) | Строка автора в баннере                    |
| Border        | `#3A3A3A` (dark) | Рамки баннера и box вокруг ключа           |

### Точки расширения для `key-management`

Специфичные для этой задачи — в дополнение к точкам bootstrap-cli:

| Элемент вывода | Будущий стиль (lipgloss) |
|---|---|
| Слово `NOT` в строке WARNING | Bold |
| Box-рамка вокруг ключа на stdout | `lipgloss.Border`, цвет Warning |
| Тело ключа внутри box | Bold |
| Значение `never` в таблице | Цвет Muted |
| Значение `-` (нет label) | Цвет Muted |
| Подтверждение `key … revoked` | Цвет Success |
| `hint:` | Цвет Muted |
| `error:` | Цвет Error + Bold |

### Путь подключения

При подключении использовать путь `charm.land/lipgloss/v2` (стабильный v2.0.x, STACK.ru.md).
Гейт NO_COLOR проверяется до применения стилей (см. раздел «Доступность»).

При наличии `NO_COLOR` (любое значение) или флага `--no-color` строка WARNING выводится без
lipgloss-стилей (plain); иначе — со стилем. Unicode-рамка вокруг ключа и текст сохраняются в
обоих режимах.

---

## Тексты команд и ошибок

### `raxd key create`

**Use:** `create [--name <label>]`

**Short:**
```
Create a new API key
```

**Long:**
```
Generate a new API key for remote access authentication.
The key is displayed once and cannot be retrieved afterwards.

Store the key securely immediately after creation.

Flags:
  --name string   optional human-readable label for the key (max 64 characters)
```

Изменение относительно bootstrap: добавлено `Store the key securely immediately after creation.`
и уточнение `(max 64 characters)` в описании флага.

### `raxd key list`

**Use:** `list`

**Short:** (без изменений из bootstrap)
```
List all API keys
```

**Long:**
```
Display a table of active API keys with their ID, label, creation date,
and last-used date. Revoked keys are not shown.
```

Добавлено: `Revoked keys are not shown.` — явно, чтобы пользователь не искал потерявшиеся ключи.

### `raxd key delete`

**Use:** `delete <id>`

**Short:** (уточнение: «revoke» точнее «delete», но команда называется `delete` по контракту spec)
```
Revoke an API key
```

**Long:**
```
Revoke the API key with the given ID. The key is immediately invalidated
and can no longer be used for authentication. This action cannot be undone.

The key record is retained for audit purposes and will not appear in "key list".

Example:
  raxd key delete abc123de
```

> **Примечание по Short:** spec называет команду `key delete`, но содержательно это `revoke`.
> Short = `Revoke an API key` честнее описывает действие. Имя команды (`delete`) не меняется.

---

## Сводка контрактов вывода (key-management)

| Команда         | stdout                        | stderr                            | Exit 0 | Exit ≠0 |
|-----------------|-------------------------------|-----------------------------------|--------|---------|
| `key create`    | box с телом ключа + пустая строка | баннер + WARNING + метаданные | успех  | ошибка валидации, ошибка записи |
| `key list`      | таблица (или «No keys found») | баннер                            | всегда | нет     |
| `key delete`    | —                             | баннер + подтверждение / ошибка   | успех  | not found, already revoked |

Сравнение с bootstrap-ux сводкой (исправление заглушек):

| Команда         | bootstrap (заглушка) | key-management (реализация)          |
|-----------------|----------------------|--------------------------------------|
| `key create`    | stderr + exit 1      | stdout (ключ) + stderr (декор) + exit 0 |
| `key list`      | stderr + exit 1      | stdout (таблица) + stderr (баннер) + exit 0 |
| `key delete`    | stderr + exit 1      | stderr (подтверждение) + exit 0/1   |

---

## Доступность (NO_COLOR, узкий терминал)

### NO_COLOR и --no-color

**Текущее поведение (plain-текст):** ANSI отсутствует. Никаких изменений не требуется.

Unicode box-drawing (`┌─┐│└─┘`) и разделители таблицы (`──`) — не ANSI, не отключаются при `NO_COLOR`.

**При добавлении lipgloss (точка расширения):**

При наличии `NO_COLOR` (любое значение) или флага `--no-color`:
- Lipgloss-стили не применяются
- Box-рамка вокруг ключа сохраняется (Unicode, не ANSI)
- `──` разделители таблицы сохраняются
- Вывод остаётся читаемым и корректным

### Узкий терминал

**`raxd key create`:**

Box-рамка вокруг ключа адаптируется по ширине ключа (~54 символа total с отступами).
При терминале < 54 символов рамка может выходить за правый край — это ожидаемо, ключ читаем.
Усечение ключа запрещено: это секрет, он должен быть полным.

**`raxd key list`:**

При ширине < 56 символов (как в bootstrap):
- Сначала усекается `LABEL` (до 12 символов с `…`)
- При < 46 символах — минимальная таблица: ID + LABEL, даты опускаются

`olekukonko/tablewriter` управляет шириной через `SetColWidth`. Заголовки не усекаются.

**Предупреждение (`key create`):**

Строка `! WARNING: This key will NOT be shown again. Save it now.` = 57 символов.
При терминале < 57 символов терминал переносит строку естественно.
Текст предупреждения не усекается и не сокращается.

### Пайп и перенаправление

- `raxd key create > key.txt` — в файл попадает box с ключом + пустая строка; баннер и
  предупреждение не попадают.
- `raxd key list | grep abc` — работает корректно (stdout = таблица).
- `raxd key list | less` — корректно, таблица читаема постранично.
- `raxd key delete abc123de 2>/dev/null` — на stdout ничего нет; выход по exit-коду.

---

*Артефакт задачи: `key-management`. Контракт для: developer. Проверяет: cli-ux-guardian.*
*Автор продукта: Vladimir Kovalev, OEM TECH.*
