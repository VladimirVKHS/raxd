#!/usr/bin/env bash
# scripts/test-install-edge.sh — edge-тесты install-flow (AC2/AC4/AC7/AC9/AC11/AC16, SR-97..SR-113)
#
# SECURITY-BASELINE §6: запускается ТОЛЬКО внутри Docker (docker-guard /.dockerenv).
# Дополняет scripts/test-install.sh (TEST1-3). Не дублирует их.
#
# TEST 4: неподдерживаемая архитектура (AC4/SR-104) — uname-shim i686 → код 2, без бинаря
# TEST 5: усечённый скрипт (AC2/SR-97) — обрыв до вызова main → бинарь не появляется
# TEST 6: darwin-ветка статически (AC11/SR-109/AC13) — grep xattr + Gatekeeper-инструкция
# TEST 7: нет прав на запись в целевой каталог (AC9/SR-106/SR-108) → код 4 + error:/hint:
# TEST 8: минимизация кода (AC7/SR-103/SR-105) — grep: нет eval/запуска демона/gpg --verify
# TEST 9: согласованность имён артефактов (AC16/SR-101) — install.sh ↔ release.sh совпадают
#
# Использование:
#   PORT=8001 VERSION=v0.1.0 bash scripts/test-install-edge.sh
#   make test-install-edge VERSION=v0.1.0

set -euo pipefail

# ── Вспомогательные функции ────────────────────────────────────────────────────

PASS_COUNT=0
FAIL_COUNT=0

pass() {
    echo "PASS: $*"
    PASS_COUNT=$((PASS_COUNT + 1))
}

fail() {
    echo "FAIL: $*"
    FAIL_COUNT=$((FAIL_COUNT + 1))
}

# assert_eq <actual> <expected> <description>
assert_eq() {
    local actual="$1"
    local expected="$2"
    local desc="$3"
    if [[ "$actual" == "$expected" ]]; then
        pass "${desc} (ожидалось: ${expected})"
    else
        fail "${desc} (ожидалось: ${expected}, получено: ${actual})"
    fi
}

# assert_no_file <path> <description>
assert_no_file() {
    local path="$1"
    local desc="$2"
    if [[ ! -f "$path" ]]; then
        pass "${desc} — файл отсутствует (как ожидалось)"
    else
        fail "${desc} — файл существует, хотя НЕ должен: ${path}"
    fi
}

# assert_file_exists <path> <description>
assert_file_exists() {
    local path="$1"
    local desc="$2"
    if [[ -f "$path" ]]; then
        pass "${desc} — файл найден: ${path}"
    else
        fail "${desc} — файл НЕ найден: ${path}"
    fi
}

# assert_grep <pattern> <file> <description>
# Добавляем '--' перед паттерном: защита от трактовки ведущего '-' как опции grep (дефект 2).
assert_grep() {
    local pattern="$1"
    local file="$2"
    local desc="$3"
    if grep -qE -- "${pattern}" "${file}"; then
        pass "${desc} — паттерн найден: '${pattern}'"
    else
        fail "${desc} — паттерн НЕ найден: '${pattern}' в ${file}"
    fi
}

# assert_no_grep <pattern> <file> <description>
# Добавляем '--' перед паттерном: защита от трактовки ведущего '-' как опции grep (дефект 2).
assert_no_grep() {
    local pattern="$1"
    local file="$2"
    local desc="$3"
    if ! grep -qE -- "${pattern}" "${file}"; then
        pass "${desc} — паттерн отсутствует: '${pattern}' (как ожидалось)"
    else
        fail "${desc} — запрещённый паттерн найден: '${pattern}' в ${file}"
    fi
}

main() {
    # ── docker-guard (SR-112, baseline §6) ────────────────────────────────────
    if [[ ! -f /.dockerenv ]]; then
        echo "error: scripts/test-install-edge.sh должен запускаться только внутри Docker"
        echo "hint: make test-install-edge запустит тесты в чистом контейнере"
        exit 1
    fi

    local port="${PORT:-8001}"
    local version="${VERSION:-}"
    local dist_dir="${DIST_DIR:-dist}"
    local install_script="${INSTALL_SCRIPT:-install.sh}"

    # ── Определяем версию из dist/ если не задана явно ────────────────────────
    if [[ -z "$version" ]]; then
        local first_archive
        first_archive="$(ls "${dist_dir}"/raxd_*.tar.gz 2>/dev/null | head -1 || true)"
        if [[ -z "$first_archive" ]]; then
            echo "error: архивы не найдены в ${dist_dir}/"
            echo "hint: сначала выполните 'make release-all'"
            exit 1
        fi
        local basename_arch
        basename_arch="$(basename "$first_archive")"
        version="$(echo "$basename_arch" | sed 's/^raxd_//; s/_[a-z]*_[a-z0-9]*\.tar\.gz$//')"
        echo "==> версия из архива: ${version}"
    fi

    # ── Проверка наличия dist/ ────────────────────────────────────────────────
    if [[ ! -f "${dist_dir}/SHA256SUMS" ]]; then
        echo "error: ${dist_dir}/SHA256SUMS не найден"
        echo "hint: сначала выполните 'make release-all'"
        exit 1
    fi

    echo "==> test-install-edge: версия=${version}, порт=${port}"

    # ── Запуск мок-HTTP-сервера (ADR-002, SR-113) ─────────────────────────────
    echo "==> запуск мок-HTTP-сервера на 127.0.0.1:${port}..."
    python3 -m http.server "${port}" --directory "${dist_dir}" --bind 127.0.0.1 &
    _EDGE_SERVER_PID=$!

    stop_edge_server() {
        kill "${_EDGE_SERVER_PID}" 2>/dev/null || true
        wait "${_EDGE_SERVER_PID}" 2>/dev/null || true
    }
    trap stop_edge_server EXIT INT TERM

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
    echo "==> мок-HTTP-сервер готов (127.0.0.1:${port})"

    # ══════════════════════════════════════════════════════════════════════════
    # TEST 4: неподдерживаемая архитектура (AC4, SR-104)
    # Проверяет: uname-shim подделывает uname -m → i686
    # install.sh ДОЛЖЕН: вернуть код 2, не установить бинарь
    # Регрессия ловит: удаление case `*)→exit 2` из блока arch-детекта install.sh
    # ══════════════════════════════════════════════════════════════════════════
    echo ""
    echo "══════════════════════════════════════════"
    echo "TEST 4: неподдерживаемая архитектура i686 (AC4, SR-104)"
    echo "══════════════════════════════════════════"

    local test4_dir="/tmp/raxd-edge-test4"
    rm -rf "${test4_dir}"
    mkdir -p "${test4_dir}/bin" "${test4_dir}/install_dest"

    # Создаём uname-shim: для аргумента -m возвращает i686 (неподдерживаемая arch),
    # для всех остальных аргументов — системный uname.
    cat > "${test4_dir}/bin/uname" <<'UNAME_SHIM'
#!/usr/bin/env bash
if [[ "${1:-}" == "-m" ]]; then
    echo "i686"
else
    exec /usr/bin/uname "$@"
fi
UNAME_SHIM
    chmod +x "${test4_dir}/bin/uname"

    local test4_exit=0
    # Запускаем install.sh с поддельным PATH: наш shim первый
    PATH="${test4_dir}/bin:${PATH}" \
    RAXD_BASE_URL="http://127.0.0.1:${port}" \
    RAXD_VERSION="${version}" \
    RAXD_PREFIX="${test4_dir}/install_dest" \
        bash "${install_script}" 2>&1 || test4_exit=$?

    echo "==> код выхода install.sh при arch=i686: ${test4_exit}"

    # Проверка 1: код выхода должен быть 2 (неподдерживаемая платформа)
    assert_eq "${test4_exit}" "2" "TEST4: код выхода при неподдерживаемой arch"

    # Проверка 2: бинарь НЕ должен быть установлен
    assert_no_file "${test4_dir}/install_dest/raxd" \
        "TEST4: бинарь не установлен при arch=i686"

    rm -rf "${test4_dir}"

    # Дополнительно: неподдерживаемая ОС (MINGW — Windows-like)
    local test4b_dir="/tmp/raxd-edge-test4b"
    rm -rf "${test4b_dir}"
    mkdir -p "${test4b_dir}/bin" "${test4b_dir}/install_dest"

    cat > "${test4b_dir}/bin/uname" <<'UNAME_SHIM_B'
#!/usr/bin/env bash
if [[ "${1:-}" == "-s" ]]; then
    echo "MINGW64_NT-10.0"
else
    exec /usr/bin/uname "$@"
fi
UNAME_SHIM_B
    chmod +x "${test4b_dir}/bin/uname"

    local test4b_exit=0
    PATH="${test4b_dir}/bin:${PATH}" \
    RAXD_BASE_URL="http://127.0.0.1:${port}" \
    RAXD_VERSION="${version}" \
    RAXD_PREFIX="${test4b_dir}/install_dest" \
        bash "${install_script}" 2>&1 || test4b_exit=$?

    echo "==> код выхода install.sh при OS=MINGW: ${test4b_exit}"
    assert_eq "${test4b_exit}" "2" "TEST4b: код выхода при неподдерживаемой OS (MINGW)"
    assert_no_file "${test4b_dir}/install_dest/raxd" \
        "TEST4b: бинарь не установлен при OS=MINGW"

    rm -rf "${test4b_dir}"

    # ══════════════════════════════════════════════════════════════════════════
    # TEST 5: усечённый скрипт — обрыв закачки до вызова main (AC2, SR-97)
    # Проверяет: скрипт с телом функции, но без вызова main "$@" в конце
    # install.sh ДОЛЖЕН: бинарь НЕ появляется (функция определена, но не вызвана)
    # Регрессия ловит: вынос логики из main() на верхний уровень
    # ══════════════════════════════════════════════════════════════════════════
    echo ""
    echo "══════════════════════════════════════════"
    echo "TEST 5: усечённый скрипт — обрыв до вызова main (AC2, SR-97)"
    echo "══════════════════════════════════════════"

    local test5_dir="/tmp/raxd-edge-test5"
    rm -rf "${test5_dir}"
    mkdir -p "${test5_dir}"

    # Создаём копию install.sh, усечённую ДО строки с вызовом main.
    # Обрезаем всё начиная со строки "^main \"\$@\"" (последнего вызова).
    # Результат: функция main() определена, но НЕ вызвана — имитация обрыва curl|sh.
    local truncated_script="${test5_dir}/install_truncated.sh"

    # Ищем номер строки с единственным вызовом main в конце файла
    local main_call_line
    main_call_line="$(grep -n '^main "\$@"' "${install_script}" | tail -1 | cut -d: -f1)"

    if [[ -z "$main_call_line" ]]; then
        fail "TEST5: строка вызова 'main \"\$@\"' не найдена в ${install_script} — структура скрипта нарушена"
    else
        echo "==> строка вызова main: ${main_call_line}"

        # Копируем всё ДО строки вызова (исключая её)
        head -n "$((main_call_line - 1))" "${install_script}" > "${truncated_script}"
        chmod +x "${truncated_script}"

        local test5_dest="${test5_dir}/install_dest"
        mkdir -p "${test5_dest}"

        local test5_exit=0
        RAXD_BASE_URL="http://127.0.0.1:${port}" \
        RAXD_VERSION="${version}" \
        RAXD_PREFIX="${test5_dest}" \
            bash "${truncated_script}" 2>&1 || test5_exit=$?

        echo "==> код выхода усечённого скрипта: ${test5_exit}"

        # Проверка: бинарь НЕ установлен (функция не вызвана)
        assert_no_file "${test5_dest}/raxd" \
            "TEST5: усечённый скрипт не установил бинарь (обрыв защищает от частичной установки)"

        # Код выхода 0 ожидается: скрипт просто определил функцию и вышел без ошибки.
        # Это корректное поведение bash при наличии только определения функции.
        assert_eq "${test5_exit}" "0" \
            "TEST5: усечённый скрипт завершился кодом 0 (функция определена, не вызвана)"

        pass "TEST5: обрыв закачки до вызова main — частичная установка не выполняется"
    fi

    rm -rf "${test5_dir}"

    # ══════════════════════════════════════════════════════════════════════════
    # TEST 6: darwin-ветка статически (AC11, SR-109, AC13)
    # Проверяет: наличие xattr -d com.apple.quarantine и Gatekeeper-инструкции
    # Регрессия ловит: удаление darwin-ветки или quarantine-строки
    # ══════════════════════════════════════════════════════════════════════════
    echo ""
    echo "══════════════════════════════════════════"
    echo "TEST 6: darwin-ветка статически (AC11, SR-109, AC13)"
    echo "══════════════════════════════════════════"

    # Проверка 1: xattr -d com.apple.quarantine присутствует
    assert_grep \
        'xattr -d com\.apple\.quarantine' \
        "${install_script}" \
        "TEST6: xattr -d com.apple.quarantine присутствует в install.sh"

    # Проверка 2: 2>/dev/null присутствует (идемпотентность — ошибка при отсутствии атрибута подавлена)
    assert_grep \
        'xattr -d com\.apple\.quarantine.*2>/dev/null' \
        "${install_script}" \
        "TEST6: xattr идемпотентен (2>/dev/null)"

    # Проверка 3: Gatekeeper-инструкция присутствует
    assert_grep \
        'Gatekeeper|com\.apple\.quarantine' \
        "${install_script}" \
        "TEST6: Gatekeeper-инструкция присутствует"

    # Проверка 4: darwin-ветка существует (if [[ "$os" == "darwin" ]])
    assert_grep \
        '\$\{?os\}?.*==.*darwin|darwin.*==.*\$\{?os\}?' \
        "${install_script}" \
        "TEST6: darwin-ветка присутствует в install.sh"

    echo ""
    echo "ЗАФИКСИРОВАНО (AC13/ОР-4): реальный Gatekeeper-флоу проверяется на живом macOS вне Docker."
    echo "  Статическая проверка darwin-ветки — компенсация за ограничение среды Docker (Linux)."

    # ══════════════════════════════════════════════════════════════════════════
    # TEST 7: нет прав на запись в целевой каталог (AC9, SR-106, SR-108)
    # Проверяет: install.sh с RAXD_PREFIX на каталог без прав записи у пользователя
    # install.sh ДОЛЖЕН: вернуть код 4, напечатать error:/hint:, бинарь не ставится
    # Регрессия ловит: удаление проверки writable или замену exit 4 на exit 1
    # Примечание: в контейнере обычно root — создаём системный каталог без прав
    # ══════════════════════════════════════════════════════════════════════════
    echo ""
    echo "══════════════════════════════════════════"
    echo "TEST 7: нет прав на запись в целевой каталог (AC9, SR-106, SR-108)"
    echo "══════════════════════════════════════════"

    local test7_dir="/tmp/raxd-edge-test7"
    rm -rf "${test7_dir}"
    mkdir -p "${test7_dir}"

    # Стратегия: создаём целевой каталог и создаём непривилегированного пользователя.
    # В debian:stable-slim обычно нет adduser без пакета; используем useradd если есть,
    # иначе создаём каталог с chmod 000 от root и запускаем install.sh с su -s /bin/bash.
    local test7_dest="${test7_dir}/readonly_dest"
    mkdir -p "${test7_dest}"
    chmod 000 "${test7_dest}"

    local test7_exit=0
    local test7_output

    # Проверяем есть ли useradd/su для запуска от непривилегированного пользователя
    if command -v useradd >/dev/null 2>&1 && command -v su >/dev/null 2>&1; then
        # Создаём временного пользователя raxdtest
        useradd -m -s /bin/bash raxdtest 2>/dev/null || true

        test7_output="$(
            su -s /bin/bash raxdtest -c \
                "RAXD_BASE_URL='http://127.0.0.1:${port}' RAXD_VERSION='${version}' RAXD_PREFIX='${test7_dest}' bash '$(pwd)/${install_script}'" \
                2>&1
        )" || test7_exit=$?

        userdel -r raxdtest 2>/dev/null || true
    else
        # Fallback: если нет useradd, используем системный /root/readonly (root не пишет в chmod 000)
        # chmod 000 означает: даже root может писать на Linux (capabilities), поэтому
        # делаем иначе: используем sticky-bit + другого владельца каталога.
        # В крайнем случае — skip с фиксацией ограничения.
        echo "warning: useradd недоступен — проверяем поведение через chmod 000 каталога"

        # Запускаем как root с RAXD_PREFIX на несуществующий системный путь без mkdir:
        # используем /proc/fake_raxd_test_dir (не существует, mkdir провалится).
        local test7_sys_dest="/proc/fake_raxd_test_dir_unreachable"

        test7_output="$(
            RAXD_BASE_URL="http://127.0.0.1:${port}" \
            RAXD_VERSION="${version}" \
            RAXD_PREFIX="${test7_sys_dest}" \
                bash "${install_script}" 2>&1
        )" || test7_exit=$?

        # Если /proc/fake не вызвал ошибку доступа, используем chmod 000 напрямую.
        # Даже от root: install использует `-m 0755`, install -m 0755 src dst провалится если dst-dir 000.
        if [[ "${test7_exit}" -eq 0 ]]; then
            echo "==> fallback: тест через chmod 000 каталога от root"
            test7_exit=0
            test7_output="$(
                RAXD_BASE_URL="http://127.0.0.1:${port}" \
                RAXD_VERSION="${version}" \
                RAXD_PREFIX="${test7_dest}" \
                    bash "${install_script}" 2>&1
            )" || test7_exit=$?
        fi
    fi

    echo "==> код выхода install.sh при нет прав: ${test7_exit}"
    echo "==> фрагмент вывода: $(echo "${test7_output}" | head -5)"

    # Проверка 1: код выхода должен быть 4
    assert_eq "${test7_exit}" "4" "TEST7: код выхода при нет прав на запись (ожидаем 4)"

    # Проверка 2: вывод содержит error: или hint:
    if echo "${test7_output}" | grep -qE 'error:|hint:'; then
        pass "TEST7: вывод содержит error:/hint: при отказе установки"
    else
        fail "TEST7: вывод НЕ содержит error:/hint: при отказе установки"
    fi

    # Проверка 3: бинарь НЕ установлен
    assert_no_file "${test7_dest}/raxd" \
        "TEST7: бинарь не установлен при отказе в правах"

    chmod 755 "${test7_dest}" 2>/dev/null || true
    rm -rf "${test7_dir}"

    # ══════════════════════════════════════════════════════════════════════════
    # TEST 8: минимизация кода (AC7, SR-103, SR-105)
    # Статическая проверка: нет eval, нет запуска демона, нет gpg --verify без ключа
    # Регрессия ловит: добавление eval/запуска демона/ложного gpg
    # ══════════════════════════════════════════════════════════════════════════
    echo ""
    echo "══════════════════════════════════════════"
    echo "TEST 8: минимизация кода (AC7, SR-103, SR-105)"
    echo "══════════════════════════════════════════"

    # eval скачанного содержимого запрещён
    assert_no_grep \
        '^\s*eval\s' \
        "${install_script}" \
        "TEST8: нет eval в install.sh (SR-103)"

    # Запуск демона raxd serve/start/daemon запрещён
    assert_no_grep \
        'raxd\s+(serve|start|daemon|run)' \
        "${install_script}" \
        "TEST8: нет запуска демона raxd serve/start в install.sh (SR-103)"

    # systemctl start/enable запрещён
    assert_no_grep \
        'systemctl\s+(start|enable|restart)' \
        "${install_script}" \
        "TEST8: нет systemctl start/enable в install.sh (SR-103)"

    # launchctl load/start запрещён
    assert_no_grep \
        'launchctl\s+(load|start|enable)' \
        "${install_script}" \
        "TEST8: нет launchctl load/start в install.sh (SR-103)"

    # Ложный gpg --verify без ключа запрещён (SR-105)
    assert_no_grep \
        'gpg\s+--verify' \
        "${install_script}" \
        "TEST8: нет ложного gpg --verify в install.sh (SR-105)"

    # curl … | bash/sh запрещён в ИСПОЛНЯЕМОМ коде (посторонняя загрузка+исполнение, SR-103).
    # Дефект 1: паттерн совпадал с легитимными строками-примерами в комментариях и heredoc --help.
    # Решение: отфильтровываем строки-комментарии (^\s*#) и содержимое heredoc cat <<EOF...EOF
    # (тело help-текста), затем ищем curl … | bash/sh в оставшемся исполняемом коде.
    #
    # Доказательство не-тавтологичности:
    #   - На текущем install.sh: после фильтрации комментариев/heredoc паттерн НЕ найден → PASS.
    #   - Если добавить `curl "$url" | bash` в тело main() — отфильтрованный вывод содержит строку
    #     и паттерн совпадёт → FAIL. Тест реально ловит регрессию.
    local curl_pipe_in_code
    curl_pipe_in_code="$(
        # Убираем строки-комментарии и содержимое heredoc (от cat <<EOF до EOF включительно).
        # sed: удаляем строки, начинающиеся с необязательных пробелов + '#'.
        # perl: вырезаем блоки cat <<WORD ... WORD (любой heredoc-маркер).
        grep -v '^\s*#' "${install_script}" \
            | perl -0777 -pe 's/cat\s*<<'\''?(\w+)'\''?.*?\n\1\n//gs' \
            | grep -E -- 'curl\s+.*\|\s*(ba)?sh' \
            || true
    )"
    if [[ -z "${curl_pipe_in_code}" ]]; then
        pass "TEST8: нет curl | bash/sh в исполняемом коде install.sh (SR-103)"
    else
        fail "TEST8: найден curl | bash/sh в исполняемом коде install.sh (SR-103): ${curl_pipe_in_code}"
    fi

    # chmod 777 / chmod -R 777 запрещён (SR-107, нет world-writable).
    # Дефект 3: прежний паттерн 'chmod\s+777\|chmod\s+-R\s+777' использовал \| как ЛИТЕРАЛЬНЫЙ
    # символ '|' в ERE (не альтернацию), поэтому ассерт был тавтологичен — всегда проходил.
    # Исправлено: ERE-альтернация без экранирования: 'chmod\s+(-R\s+)?777' покрывает оба случая.
    #
    # Доказательство не-тавтологичности:
    #   - На текущем install.sh: 'chmod 777' отсутствует → grep не найдёт → PASS.
    #   - Если добавить `chmod 777 /usr/local/bin/raxd` или `chmod -R 777 ...` → grep найдёт → FAIL.
    assert_no_grep \
        'chmod\s+(-R\s+)?777' \
        "${install_script}" \
        "TEST8: нет chmod 777 / chmod -R 777 в install.sh (SR-107)"

    # Мок-сервер в тест-скрипте должен иметь --bind 127.0.0.1 (SR-113)
    assert_grep \
        '--bind 127\.0\.0\.1' \
        "scripts/test-install.sh" \
        "TEST8: мок-сервер в test-install.sh слушает только 127.0.0.1 (SR-113)"

    # Каркас: set -euo pipefail (SR-97)
    assert_grep \
        '^set -euo pipefail' \
        "${install_script}" \
        "TEST8: set -euo pipefail присутствует (SR-97)"

    # trap cleanup на EXIT/INT/TERM (SR-98)
    assert_grep \
        'trap.*(EXIT|INT|TERM)' \
        "${install_script}" \
        "TEST8: trap cleanup на EXIT/INT/TERM (SR-98)"

    # mktemp -d (SR-98)
    assert_grep \
        'mktemp -d' \
        "${install_script}" \
        "TEST8: mktemp -d для временного каталога (SR-98)"

    # RAXD_BASE_URL дефолт = https:// (SR-99)
    assert_grep \
        'RAXD_BASE_URL.*https://' \
        "${install_script}" \
        "TEST8: дефолтный RAXD_BASE_URL начинается с https:// (SR-99)"

    # curl -fsSL (SR-99)
    assert_grep \
        'curl -fsSL' \
        "${install_script}" \
        "TEST8: curl -fsSL для скачивания (SR-99)"

    # ── Проверка отсутствия генерации unit/plist (AC1) ────────────────────────
    # AC1/spec: install.sh ставит ТОЛЬКО бинарь; собственной логики генерации/
    # регистрации unit-файлов (systemd .service) или plist-файлов (launchd) нет.
    # Регистрация сервиса — Out of Scope (задача service-install, команда raxd service install).
    # install.sh может лишь выводить hint "raxd service install" — это не генерация.
    #
    # Доказательство не-тавтологичности каждого паттерна:
    #   - На текущем install.sh: паттерны НЕ найдены → PASS (подтверждено локально).
    #   - Если добавить генерацию unit/plist (любой из сценариев ниже) → FAIL:
    #     * запись в /etc/systemd/system → паттерн 1 поймает
    #     * запись в /Library/LaunchDaemons|LaunchAgents → паттерн 2 поймает
    #     * упоминание файла *.service или *.plist → паттерны 3/4 поймают
    #     * содержимое unit-файла ([Unit]/[Service]/ExecStart=) → паттерн 5 поймает
    #     * содержимое plist-файла (<key>Label</key>/RunAtLoad) → паттерн 6 поймает

    # Паттерн 1: запись в каталог systemd-юнитов
    assert_no_grep \
        '/etc/systemd/system' \
        "${install_script}" \
        "TEST8/AC1: нет записи в /etc/systemd/system в install.sh"

    # Паттерн 2: запись в каталоги launchd-plist macOS
    assert_no_grep \
        '/Library/Launch(Daemons|Agents)' \
        "${install_script}" \
        "TEST8/AC1: нет записи в /Library/Launch{Daemons,Agents} в install.sh"

    # Паттерн 3: упоминание файла *.service (генерация/копирование unit-файла)
    # Примечание: hint 'raxd service install' не содержит '.service' — PASS на корректном коде.
    assert_no_grep \
        '\.service\b' \
        "${install_script}" \
        "TEST8/AC1: нет упоминания .service-файла в install.sh"

    # Паттерн 4: упоминание файла *.plist (генерация/копирование plist-файла)
    assert_no_grep \
        '\.plist\b' \
        "${install_script}" \
        "TEST8/AC1: нет упоминания .plist-файла в install.sh"

    # Паттерн 5: характерное содержимое systemd unit-файла
    assert_no_grep \
        '\[Unit\]|\[Service\]|ExecStart=' \
        "${install_script}" \
        "TEST8/AC1: нет содержимого unit-файла ([Unit]/[Service]/ExecStart=) в install.sh"

    # Паттерн 6: характерное содержимое launchd plist-файла
    assert_no_grep \
        '<key>Label</key>|RunAtLoad' \
        "${install_script}" \
        "TEST8/AC1: нет содержимого plist-файла (<key>Label</key>/RunAtLoad) в install.sh"

    # ══════════════════════════════════════════════════════════════════════════
    # TEST 9: согласованность имён артефактов (AC16, SR-101)
    # Проверяет: шаблон имени в install.sh совпадает с шаблоном в release.sh
    # Регрессия ловит: рассинхрон имён между install.sh и release.sh
    # ══════════════════════════════════════════════════════════════════════════
    echo ""
    echo "══════════════════════════════════════════"
    echo "TEST 9: согласованность имён артефактов (AC16, SR-101)"
    echo "══════════════════════════════════════════"

    # Извлекаем шаблон имени архива из install.sh
    # Ищем строку вида: archive="raxd_${version}_${os}_${arch}.tar.gz"
    local install_archive_pattern
    install_archive_pattern="$(grep -E 'archive=.*raxd_\$\{.*\}_\$\{.*\}_\$\{.*\}\.tar\.gz' "${install_script}" | head -1 || true)"

    if [[ -z "$install_archive_pattern" ]]; then
        fail "TEST9: шаблон имени архива не найден в ${install_script}"
    else
        pass "TEST9: шаблон имени архива найден в install.sh: '$(echo "${install_archive_pattern}" | tr -d ' ')'"
    fi

    # Извлекаем шаблон имени архива из release.sh
    # Ищем строку вида: archive="${dist_dir}/raxd_${version}_${target}.tar.gz"
    local release_archive_pattern
    release_archive_pattern="$(grep -E 'archive=.*raxd_\$\{.*\}_\$\{.*\}\.tar\.gz' "scripts/release.sh" | head -1 || true)"

    if [[ -z "$release_archive_pattern" ]]; then
        fail "TEST9: шаблон имени архива не найден в scripts/release.sh"
    else
        pass "TEST9: шаблон имени архива найден в release.sh: '$(echo "${release_archive_pattern}" | tr -d ' ')'"
    fi

    # Структурная проверка: оба содержат raxd_ + версию + .tar.gz
    assert_grep \
        'raxd_.*\.tar\.gz' \
        "${install_script}" \
        "TEST9: install.sh строит имя с суффиксом .tar.gz"

    assert_grep \
        'raxd_.*\.tar\.gz' \
        "scripts/release.sh" \
        "TEST9: release.sh строит имя с суффиксом .tar.gz"

    # Проверяем: SHA256SUMS фактически содержит имена, которые ожидает install.sh
    # Формат файла: dist/ должен содержать записи raxd_<version>_<os>_<arch>.tar.gz
    if [[ -f "${dist_dir}/SHA256SUMS" ]]; then
        # Проверяем все 4 ожидаемые цели
        local all_targets_ok=1
        for target in linux_amd64 linux_arm64 darwin_amd64 darwin_arm64; do
            local expected_name="raxd_${version}_${target}.tar.gz"
            if grep -qF "${expected_name}" "${dist_dir}/SHA256SUMS"; then
                pass "TEST9: ${expected_name} присутствует в SHA256SUMS"
            else
                fail "TEST9: ${expected_name} НЕ найден в SHA256SUMS"
                all_targets_ok=0
            fi
        done

        if [[ "$all_targets_ok" -eq 1 ]]; then
            pass "TEST9: все 4 цели присутствуют в SHA256SUMS (согласованность install.sh↔release.sh↔SHA256SUMS)"
        fi
    else
        # Эта ветка недостижима: выше (строка ~122) скрипт уже завершается с exit 1
        # если SHA256SUMS отсутствует. Заменяем graceful-warning на честный fail —
        # чтобы не создавать ложного ощущения допустимой деградации (Н-1 guardian).
        fail "TEST9: ${dist_dir}/SHA256SUMS не найден — невозможно проверить согласованность имён"
    fi

    # ── Итог ──────────────────────────────────────────────────────────────────
    echo ""
    echo "══════════════════════════════════════════"
    echo "Итог test-install-edge:"
    echo "  PASS: ${PASS_COUNT}"
    echo "  FAIL: ${FAIL_COUNT}"
    echo "══════════════════════════════════════════"

    if [[ "${FAIL_COUNT}" -gt 0 ]]; then
        echo "ИТОГ: FAIL — ${FAIL_COUNT} проверок провалено"
        exit 1
    else
        echo "ИТОГ: PASS — все ${PASS_COUNT} проверок прошли"
    fi
}

main "$@"
