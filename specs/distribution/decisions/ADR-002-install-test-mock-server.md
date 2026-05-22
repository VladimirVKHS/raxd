# ADR-002: Тестирование `curl | sh` install-flow без публичного remote

## Контекст
spec distribution Q3 / AC12, baseline §6: remote отсутствует (push/PR не делаем), но install-flow
`curl | sh` надо проверить в чистом Linux-контейнере. Нужен источник артефактов внутри контейнера и
способ направить install.sh на него вместо несуществующего боевого хоста.

## Решение
**Принят вариант A**: тестировать install-flow против **локального мок-HTTP-сервера артефактов внутри
чистого debian-контейнера** (`python3 -m http.server <port> -d dist --bind 127.0.0.1`), а install.sh
**спроектирован с обязательной env-параметризацией базового URL** `RAXD_BASE_URL` (дефолт — будущий
боевой URL-плейсхолдер, в тесте — `http://127.0.0.1:8000`) + `RAXD_VERSION`. Рецепт реализуется как
Make-таргет `test-install` + `scripts/test-install.sh` + `Dockerfile.install` (чистый
`debian:stable-slim` с `python3`/`curl`, без Go/raxd): положить `dist/` (4 архива + `SHA256SUMS`) в
каталог раздачи, поднять сервер, выполнить `RAXD_BASE_URL=http://127.0.0.1:8000 bash install.sh`,
проверить `raxd version`, идемпотентность и хэш-fail. Таргет под docker-guard (`/.dockerenv`, §6).

## Альтернативы
- **B: `file://`-источник** (`RAXD_BASE_URL=file:///artifacts`, curl читает локальные файлы) — проще
  (без сервера/порта), но не проверяет реальный сетевой путь (HTTP-коды, редиректы, обрыв) → слабее
  как тест install-flow. → https://man7.org/linux/man-pages/man1/curl.1.html
- **C: docker-compose из двух сервисов** (artifact-server + чистый client) — честнее изоляция
  «сервер ≠ клиент», но больше инфраструктуры; выигрыш для v1 невелик. → baseline §6

## Последствия
- Плюсы: ближе всего к боевому `curl|sh` (реальное HTTP-скачивание+проверка хэша) в одном
  воспроизводимом контейнере; env-override URL — стандартный приём реальных инсталляторов (deno
  `DENO_INSTALL`). → https://docs.deno.com/runtime/getting_started/installation/ ,
  https://docs.python.org/3/library/http.server.html
- Минусы: нужен python3 (или busybox/nginx) в тест-образе; это **накладывает требование на ДИЗАЙН
  install.sh** — обязательная env-параметризация URL/версии (без хардкода единственного боевого хоста),
  иначе AC12 непроверяем без remote. Зафиксировано в plan.md (install.sh контракт: `RAXD_BASE_URL`).
- Стек: новых рантайм-зависимостей продукта не вводит (сервер `python3` — только для теста, bind 127.0.0.1).

## Статус (proposed|accepted)
accepted
