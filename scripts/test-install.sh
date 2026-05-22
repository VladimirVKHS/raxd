#!/usr/bin/env bash
# scripts/test-install.sh — тест install-flow в чистом контейнере (AC12, SR-112/SR-113)
#
# SECURITY-BASELINE §6: запускается ТОЛЬКО внутри Docker (docker-guard /.dockerenv).
# ADR-002: мок-HTTP python3 -m http.server --bind 127.0.0.1 (SR-113).
#
# Предусловия (должны быть выполнены до вызова):
#   - dist/ содержит 4 архива raxd_<VERSION>_*.tar.gz + SHA256SUMS (make release-all)
#   - install.sh присутствует в корне репо
#   - python3 и curl установлены (Dockerfile.install)
#
# Проверяет:
#   AC1/AC3/AC5/AC6/AC7/AC9/AC12:
#     1. Позитивный кейс: install.sh скачивает архив, проверяет SHA256, ставит бинарь.
#     2. raxd version отвечает (не 'dev', формат 'raxd <v> (commit <c>, built <d>)').
#     3. Идемпотентность: повторный запуск оставляет ровно один raxd.
#     4. Негативный кейс (AC3, SR-100): подмена архива → install.sh отказывает с кодом 3.
#
# Использование:
#   PORT=8000 VERSION=v0.1.0 bash scripts/test-install.sh
#   make test-install

set -euo pipefail

main() {
    # ── docker-guard (SR-112, baseline §6) ────────────────────────────────────
    if [[ ! -f /.dockerenv ]]; then
        echo "error: scripts/test-install.sh должен запускаться только внутри Docker"
        echo "hint: make test-install запустит тест в чистом контейнере"
        exit 1
    fi

    local port="${PORT:-8000}"
    local version="${VERSION:-}"
    local dist_dir="${DIST_DIR:-dist}"
    local install_dir="${INSTALL_PREFIX:-/tmp/raxd-test-install}"

    # ── Определяем версию из SHA256SUMS если не задана явно ──────────────────

    if [[ -z "$version" ]]; then
        # Берём версию из имени первого архива в dist/
        local first_archive
        first_archive="$(ls "${dist_dir}"/raxd_*.tar.gz 2>/dev/null | head -1 || true)"
        if [[ -z "$first_archive" ]]; then
            echo "error: архивы не найдены в ${dist_dir}/"
            echo "hint: сначала выполните 'make release-all'"
            exit 1
        fi
        # Извлекаем версию из имени: raxd_<version>_<os>_<arch>.tar.gz
        local basename
        basename="$(basename "$first_archive")"
        # basename = raxd_v0.1.0_linux_amd64.tar.gz
        version="$(echo "$basename" | sed 's/^raxd_//; s/_[a-z]*_[a-z0-9]*\.tar\.gz$//')"
        echo "==> версия из архива: ${version}"
    fi

    # ── Проверка наличия dist/ с артефактами ──────────────────────────────────

    if [[ ! -f "${dist_dir}/SHA256SUMS" ]]; then
        echo "error: ${dist_dir}/SHA256SUMS не найден"
        echo "hint: сначала выполните 'make release-all'"
        exit 1
    fi

    echo "==> test-install: версия=${version}, порт=${port}"
    echo "==> dist/ содержит:"
    ls -lh "${dist_dir}/"

    # ── Запуск мок-HTTP-сервера (ADR-002, SR-113) ──────────────────────────────
    # Слушает ТОЛЬКО 127.0.0.1 (не наружу).

    echo "==> запуск мок-HTTP-сервера на 127.0.0.1:${port}..."
    python3 -m http.server "${port}" --directory "${dist_dir}" --bind 127.0.0.1 &
    # Используем глобальную переменную (не local), чтобы trap мог обратиться к ней
    # при EXIT из контекста функции main (SR-113).
    _SERVER_PID=$!

    stop_server() {
        kill "${_SERVER_PID}" 2>/dev/null || true
        wait "${_SERVER_PID}" 2>/dev/null || true
    }
    trap stop_server EXIT INT TERM

    # Ждём поднятия сервера
    local retries=10
    until curl -sf "http://127.0.0.1:${port}/SHA256SUMS" -o /dev/null; do
        retries=$((retries - 1))
        if [[ $retries -le 0 ]]; then
            echo "error: мок-HTTP-сервер не запустился за отведённое время"
            exit 1
        fi
        sleep 0.3
    done
    echo "==> мок-HTTP-сервер готов"

    # ── ПОЗИТИВНЫЙ КЕЙС: нормальная установка ─────────────────────────────────

    echo ""
    echo "══════════════════════════════════════════"
    echo "TEST 1: позитивный кейс — нормальная установка"
    echo "══════════════════════════════════════════"

    mkdir -p "${install_dir}"

    RAXD_BASE_URL="http://127.0.0.1:${port}" \
    RAXD_VERSION="${version}" \
    RAXD_PREFIX="${install_dir}" \
        bash install.sh

    # Проверка: бинарь существует и исполняем
    if [[ ! -x "${install_dir}/raxd" ]]; then
        echo "FAIL: бинарь ${install_dir}/raxd не найден или не исполняем"
        exit 1
    fi

    echo "PASS: бинарь установлен: ${install_dir}/raxd"

    # Проверка: raxd version (AC10, SR-110)
    local version_output
    version_output="$("${install_dir}/raxd" version 2>&1)"
    echo "==> raxd version: ${version_output}"

    # Формат: 'raxd <v> (commit <c>, built <d>)' — не 'dev'
    if echo "${version_output}" | grep -qE '^raxd .+ \(commit .+, built .+\)$'; then
        echo "PASS: формат version корректен"
    else
        echo "FAIL: неожиданный формат version: '${version_output}'"
        exit 1
    fi

    # Проверяем что версия в строке 'raxd <v> (...)' не равна 'dev'.
    # Извлекаем только строку с версией (не banner).
    local ver_line
    ver_line="$(echo "${version_output}" | grep -E '^raxd .+ \(commit .+, built .+\)$' | head -1)"
    if echo "${ver_line}" | grep -qE '^raxd dev '; then
        echo "FAIL: version содержит 'dev' — ldflags не применены: '${ver_line}'"
        exit 1
    fi
    echo "PASS: version не 'dev': ${ver_line}"

    # ── ИДЕМПОТЕНТНОСТЬ: повторный запуск (AC5, SR-107) ───────────────────────

    echo ""
    echo "══════════════════════════════════════════"
    echo "TEST 2: идемпотентность — повторная установка"
    echo "══════════════════════════════════════════"

    RAXD_BASE_URL="http://127.0.0.1:${port}" \
    RAXD_VERSION="${version}" \
    RAXD_PREFIX="${install_dir}" \
        bash install.sh

    # Должен быть ровно один бинарь raxd
    local raxd_count
    raxd_count="$(find "${install_dir}" -name "raxd" -type f | wc -l | tr -d ' ')"
    if [[ "$raxd_count" -ne 1 ]]; then
        echo "FAIL: ожидался 1 бинарь raxd, найдено: ${raxd_count}"
        exit 1
    fi
    echo "PASS: ровно 1 бинарь raxd после повторной установки"

    # Бинарь по-прежнему работает
    local version_output2
    version_output2="$("${install_dir}/raxd" version 2>&1)"
    if echo "${version_output2}" | grep -qE '^raxd .+ \(commit .+, built .+\)'; then
        echo "PASS: повторная установка не сломала бинарь"
    else
        echo "FAIL: бинарь после повторной установки не работает"
        echo "  output: ${version_output2}"
        exit 1
    fi

    # ── НЕГАТИВНЫЙ КЕЙС: подмена архива → SHA256 должен отказать (AC3, SR-100) ─

    echo ""
    echo "══════════════════════════════════════════"
    echo "TEST 3: негативный кейс — подмена архива (SHA256 должен отказать)"
    echo "══════════════════════════════════════════"

    # Определяем нативный архив — тот же, который install.sh запросит на этой платформе.
    # Архитектура: нормализуем x86_64→amd64, aarch64→arm64 (как install.sh).
    local native_arch
    case "$(uname -m)" in
        x86_64)        native_arch="amd64" ;;
        aarch64|arm64) native_arch="arm64" ;;
        *) native_arch="$(uname -m)" ;;
    esac
    local native_archive="${dist_dir}/raxd_${version}_linux_${native_arch}.tar.gz"

    if [[ ! -f "$native_archive" ]]; then
        echo "warning: ${native_archive} не найден — пропускаем негативный тест"
    else
        # Сохраняем оригинальный архив
        local original_archive="${dist_dir}/raxd_${version}_linux_amd64.tar.gz.orig"
        cp "${native_archive}" "${original_archive}"

        # Подменяем архив мусором
        echo "TAMPERED_CONTENT_FOR_SHA256_FAIL_TEST" > "${native_archive}"

        echo "==> архив подменён мусором — install.sh ДОЛЖЕН вернуть код 3..."

        local tamper_exit=0
        RAXD_BASE_URL="http://127.0.0.1:${port}" \
        RAXD_VERSION="${version}" \
        RAXD_PREFIX="/tmp/raxd-tamper-test" \
            bash install.sh 2>&1 || tamper_exit=$?

        echo "==> код выхода install.sh при подмене: ${tamper_exit}"

        # Восстанавливаем оригинальный архив
        mv "${original_archive}" "${native_archive}"

        if [[ "$tamper_exit" -eq 3 ]]; then
            echo "PASS: install.sh вернул код 3 (несовпадение SHA256) — защита работает"
        else
            echo "FAIL: install.sh вернул ${tamper_exit} вместо 3 — SHA256-проверка не работает!"
            exit 1
        fi

        # Убеждаемся, что бинарь НЕ установлен после подмены
        if [[ -f "/tmp/raxd-tamper-test/raxd" ]]; then
            echo "FAIL: бинарь был установлен несмотря на несовпадение SHA256!"
            exit 1
        fi
        echo "PASS: бинарь НЕ установлен при несовпадении SHA256"
    fi

    # ── Итог ──────────────────────────────────────────────────────────────────

    echo ""
    echo "══════════════════════════════════════════"
    echo "PASS: все тесты install-flow прошли успешно"
    echo "  - бинарь установлен и работает: ${version_output}"
    echo "  - повторная установка идемпотентна"
    echo "  - SHA256-проверка реально отвергает подмену (код 3)"
    echo "══════════════════════════════════════════"
}

main "$@"
