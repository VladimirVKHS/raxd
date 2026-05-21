# Guardian Report: qa-guardian — key-management

## Summary
Test-plan и тесты добросовестны: матрицы AC (12) и SR (25) заполнены, ключевые security-инварианты покрыты по существу (независимое воспроизведение sha256(key‖salt), байт-в-байт corrupt, fingerprint без тела), нет t.Skip/закомментированных. Три проблемы блокируют pass + один INFO.

## Issues
- [ ] Issue 1 (MAJOR): SR-2 ссылается на `TestStaticNoHardcodedSecrets`, который НЕ ищет `math/rand` (только rax_live_/PRIVATE KEY и т.п.). Реального теста на отсутствие math/rand в `internal/keystore`/`internal/cli/key.go` нет — SR-2 числится покрытым ложно.
  - Что делать: добавить паттерн `"math/rand"` в forbidden (или отдельный TestStaticNoMathRand) по key-логике; исправить ссылку в матрице SR-2.
- [ ] Issue 2 (MAJOR): `TestNoTempFileAfterError` (keystore_qa_test.go ~220-240) обещает «read-only dir → ошибка до rename», но фактически делает УСПЕШНУЮ запись и проверяет отсутствие .tmp — дублирует TestAtomicWritePermissions, сценарий ошибки не реализован (SR-21).
  - Что делать: реализовать реальный путь ошибки. ВНИМАНИЕ: тесты идут в Docker как root → `chmod 0555` на каталог НЕ запретит запись root'у. Используй root-устойчивый приём: сделать родителя пути обычным файлом (создать файл `X`, путь стора `X/keys.db` → создание temp в `X/` падает даже под root), вызвать Create, проверить ошибку И отсутствие .tmp. Либо мок writeDB. Не оставлять «зелёный» тест, проверяющий успешный путь под видом ошибочного.
- [ ] Issue 3 (MINOR): счётчик «116» не сходится с фактом (top-level ~106; `internal/cli` заявлено 55, найдено 45).
  - Что делать: посчитать в Docker (`go test -v -count=1 ./... | grep -c '^--- PASS'`), выставить точное число и исправить разбивку по пакетам (top-level + суб-тесты явно, если считаешь t.Run).
- [ ] Issue 4 (INFO): `TestEmptyListReturnsNil` (keystore_test.go) не включён ни в одну матрицу. Добавить в AC-5/AC-12 или Edge Cases.

## Looks good
- TestHashSchemeDirectVerification независимо считает sha256(key‖salt) из raw JSON — реальная верификация схемы, не «не паникнул».
- SR-матрица с уровнями проверки (unit/integration/static), in/out-of-scope разграничены со ссылками на другие задачи.
- Docker-команды конкретны (keystore/cli/static/race по отдельности).

## Verdict (раунд 1)
needs-changes
(MAJOR: Issue 1, 2; MINOR: Issue 3; INFO: Issue 4.)

---

## Повторная проверка (раунд 2)
Все правки выполнены корректно:
- Issue 1: добавлен `TestStaticNoMathRand` (сканирует internal/keystore + cli/key.go на math/rand-импорты и прямые вызовы); матрица SR-2 переключена на него.
- Issue 2: `TestNoTempFileAfterError` переписан на реальный error-путь через ENOTDIR (родитель = файл, CreateTemp падает даже под root); проверяет ошибку + отсутствие .tmp.
- Issue 3: счётчик 117 top-level + 3 sub = 120, разбивка сходится арифметически, добавлена команда подсчёта.
- Issue 4: `TestEmptyListReturnsNil` в матрице AC-5 и Edge Cases.
Нет t.Skip/ослабленных ассертов. Замечаний нет.

## Verdict (раунд 2)
pass

---

## Раунд 3 — тесты по reviewer (data race) + multibyte
- `TestConcurrentVerifyMixWithFlush` (8 Verify-valid + 8 Verify-invalid + 4 FlushUsage, 10 итераций) реально провоцирует гонку на usageBuf и проверяет корректность (ok=true/false); `TestConcurrentVerifyNoRace`/`TestConcurrentVerifyAndFlush` значимы под -race.
- `-race` включён в Dockerfile (`CGO_ENABLED=1 go test -race ./internal/keystore/...`); продакшен-сборка остаётся CGO_ENABLED=0 (корректное разделение, задокументировано).
- multibyte-label тесты (TestLabelMultibyteExact64/TooLong) в матрице AC-10 (формулировка уточнена «≤64 рун»); concurrent — в SR-23.
- Счётчик 122 top-level + 3 sub = 125; разбивка сходится (keystore 47+5=52). Нет t.Skip/ослабленных ассертов.

## Verdict (финал)
pass


