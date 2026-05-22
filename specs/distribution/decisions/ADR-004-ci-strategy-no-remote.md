# ADR-004: CI-стратегия без публичного remote

## Контекст
spec distribution Q5 / AC14, baseline §6: «CI прогоняет сборку и тесты в контейнере». Но remote
отсутствует (нет публичного repo/runner), значит AC14 «процесс работает» надо сделать проверяемым
локально, при этом задокументировав автоматизированный процесс на будущее.

## Решение
**Принят вариант A — оба пути**: (1) `.github/workflows/ci.yml` + `.github/workflows/release.yml`
(build-матрица + тесты в контейнере, сборка из `vendor/` офлайн, кэш модулей `setup-go` отключён —
не нужен при `-mod=vendor`) как АРТЕФАКТ на будущее (не запускается без remote); (2) **локально-
прогоняемый docker-CI** — Make-таргет `ci-local` (вызывает `release-all` = `build-all`+архивы+
`SHA256SUMS` + `test-unit` из `vendor/`, под docker-guard `/.dockerenv`) как фактический гейт v1.
Оба пути зовут ОДНИ И ТЕ ЖЕ Make-таргеты, чтобы не рассинхронизироваться.

## Альтернативы
- **B: только GitHub Actions YAML** — одна точка, но без remote НЕ запускается → AC14 непроверяем
  сейчас. → spec AC14
- **C: только локальный docker-CI без YAML** — проверяемо сейчас, но AC14 просит задокументированный
  автоматизированный процесс; при появлении remote CI придётся писать заново. → spec AC14
- **A: оба** (ПРИНЯТО) — проверяемость сегодня + готовый процесс на будущее. → spec AC14,
  https://github.com/actions/setup-go

## Последствия
- Плюсы: AC14/AC15 проверяемы локально в Docker уже сейчас (офлайн из `vendor/`); YAML готов к моменту
  появления remote (тогда `actions/setup-go` и предустановленный Go доступны,
  https://docs.github.com/actions/automating-builds-and-tests/building-and-testing-go).
- Минусы: два описания CI → риск рассинхрона (смягчается общими Make-таргетами `release-all`/`test-unit`).
- Стек: кэш модулей в `setup-go` НЕ нужен (сборка из `vendor/`, без `go mod download`) — в YAML
  явно отключить. Переиспользует существующие `Dockerfile`(target test)/`Makefile`.

## Статус (proposed|accepted)
accepted
