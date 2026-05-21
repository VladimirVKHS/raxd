# Security Requirements: <короткое название>

> Каждое требование проверяемо (можно ответить «выполнено/нет») и ссылается на пункт
> `SECURITY-BASELINE.ru.md`. Эти требования обязаны выполнить developer/system-dev/devops/mcp-engineer.

## Аутентификация ключей (baseline §1)
- [ ] Ключи генерируются `crypto/rand`, ≥ 128 бит энтропии; `math/rand` отсутствует (baseline §1).
- [ ] Хранение — `sha256(key + per-key-salt)` + salt, не в открытом виде (baseline §1).
- [ ] Сравнение секретов только constant-time (`crypto/subtle`/`hmac.Equal`); `==` по секретам нет (baseline §1).
- [ ] Ключ показывается один раз при `key create`; `key list` — без секрета; `key delete` отзывает мгновенно (baseline §1).

## Транспорт / TLS (baseline §2)
- [ ] TLS обязателен, `MinVersion` ≥ TLS 1.2 (цель 1.3); приватный ключ `0600` (baseline §2).
- [ ] Для HTTP/MCP — валидация заголовка `Origin` (защита от DNS-rebinding) (baseline §2).
- [ ] Осознанный bind-интерфейс; локально — `127.0.0.1` (baseline §2).

## Выполнение команд (baseline §3)
- [ ] `exec.Command(bin, args...)` без shell-интерполяции; `sh -c <user-input>` отсутствует (baseline §3).
- [ ] Таймаут на каждую команду через `context` (baseline §3).
- [ ] Демон работает не от root; порт <1024 — через capabilities, не setuid root (baseline §3).

## Аудит / устойчивость (baseline §4)
- [ ] Аудит-лог каждого действия (timestamp, fingerprint ключа, команда+аргументы, exit, длительность, адрес), JSON + ротация (baseline §4).
- [ ] Rate limiting per-key и per-IP, 429 при превышении (baseline §4).
- [ ] Секреты в логах/выводе CLI/commit-истории отсутствуют (baseline §4).

## Дистрибуция (baseline §5)
- [ ] Install-скрипт: `set -euo pipefail`, тело в функции, `trap` на очистку (baseline §5).
- [ ] Проверка `SHA256SUMS` скачанного бинаря (baseline §5).
- [ ] macOS: подпись/нотаризация (минимум — снятие quarantine + инструкция) (baseline §5).
