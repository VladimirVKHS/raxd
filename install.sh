#!/usr/bin/env bash
# install.sh — установочный скрипт raxd для curl | sh
#
# SECURITY-BASELINE §5: set -euo pipefail; всё тело в функции main();
# единственный вызов в конце файла (защита от обрыва закачки curl|sh).
# trap cleanup на EXIT/INT/TERM (SR-98).
#
# Использование:
#   curl -fsSL https://<base-url>/install.sh | bash
#   curl -fsSL https://<base-url>/install.sh | bash -s -- --prefix ~/.local/bin
#   RAXD_VERSION=v0.1.0 curl -fsSL … | bash
#
# Переменные окружения:
#   RAXD_BASE_URL  — база URL для скачивания артефактов (по умолчанию: боевой placeholder)
#   RAXD_VERSION   — тег версии (по умолчанию: latest)
#   RAXD_PREFIX    — каталог установки (override авто-детекта)
#
# Коды возврата:
#   0  — успех
#   1  — общая ошибка
#   2  — неподдерживаемая платформа (AC4, SR-104)
#   3  — несовпадение SHA256 (AC3, SR-100)
#   4  — нет прав на запись / нет sudo (AC9, SR-106)
#   5  — сбой скачивания (SR-99)

set -euo pipefail

# ── Точка входа (защита SR-97: ничего до вызова main) ────────────────────────

main() {
    # ── Параметры по умолчанию ────────────────────────────────────────────────

    # Дефолтный RAXD_BASE_URL: HTTPS-плейсхолдер боевого remote.
    # ВАЖНО: перед публичным релизом заменить на реальный URL
    # (например https://github.com/vladimirvkhs/raxd/releases/download/${RAXD_VERSION}).
    # Для теста install-flow переопределяется через env (ADR-002, SR-113).
    local base_url="${RAXD_BASE_URL:-https://releases.example.com/raxd}"
    local version="${RAXD_VERSION:-latest}"
    local prefix="${RAXD_PREFIX:-}"

    # ── Временный каталог + trap cleanup (SR-98) ──────────────────────────────
    # ВАЖНО: объявляем tmpdir и trap ДО разбора флагов, чтобы cleanup корректно
    # сработал при раннем exit (--help, ошибка параметра). Пустая строка "" —
    # rm -rf "" безвреден (проверяем -n перед rm).

    local tmpdir=""
    cleanup() {
        if [[ -n "${tmpdir:-}" ]]; then
            rm -rf "${tmpdir}"
        fi
    }
    trap cleanup EXIT INT TERM

    # ── Разбор флагов CLI ─────────────────────────────────────────────────────

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --prefix)
                shift
                prefix="${1:-}"
                if [[ -z "$prefix" ]]; then
                    echo "error: --prefix требует аргумент"
                    exit 1
                fi
                ;;
            --version)
                shift
                version="${1:-}"
                if [[ -z "$version" ]]; then
                    echo "error: --version требует аргумент"
                    exit 1
                fi
                ;;
            -h|--help)
                cat <<EOF
install.sh — установщик raxd

Использование:
  curl -fsSL <url>/install.sh | bash
  bash install.sh [--prefix <dir>] [--version <tag>]

Переменные окружения:
  RAXD_BASE_URL   база URL артефактов (по умолчанию: боевой HTTPS-плейсхолдер)
  RAXD_VERSION    тег версии (по умолчанию: latest)
  RAXD_PREFIX     каталог установки (override авто-детекта)

Коды выхода:
  0  успех
  1  общая ошибка
  2  неподдерживаемая платформа
  3  несовпадение SHA256
  4  нет прав на запись / нет sudo
  5  сбой скачивания
EOF
                exit 0
                ;;
            *)
                echo "error: неизвестный флаг: $1"
                echo "hint: используйте --help для справки"
                exit 1
                ;;
        esac
        shift
    done

    # ── mktemp для временного каталога (SR-98) ───────────────────────────────

    tmpdir="$(mktemp -d)"

    # ── Детект OS и архитектуры (AC4, SR-104) ────────────────────────────────

    local os arch

    local raw_os
    raw_os="$(uname -s)"
    case "$raw_os" in
        Linux)   os="linux" ;;
        Darwin)  os="darwin" ;;
        *)
            echo "error: платформа не поддерживается: $raw_os"
            echo "hint: поддерживаются только linux и darwin (macOS)"
            exit 2
            ;;
    esac

    local raw_arch
    raw_arch="$(uname -m)"
    case "$raw_arch" in
        x86_64)          arch="amd64" ;;
        aarch64|arm64)   arch="arm64" ;;
        *)
            echo "error: архитектура не поддерживается: $raw_arch"
            echo "hint: поддерживаются amd64 (x86_64) и arm64 (aarch64)"
            exit 2
            ;;
    esac

    echo "==> определена платформа: ${os}/${arch}"

    # ── Имя артефакта (AC16, SR-101) — единственный источник имён ─────────────
    # Согласовано с scripts/release.sh: raxd_<version>_<os>_<arch>.tar.gz

    local archive="raxd_${version}_${os}_${arch}.tar.gz"
    local archive_url="${base_url}/${archive}"
    local sums_url="${base_url}/SHA256SUMS"

    # ── Проверка наличия утилиты SHA256 (SR-100) ─────────────────────────────

    local sha256_cmd=""
    if command -v sha256sum >/dev/null 2>&1; then
        sha256_cmd="sha256sum"
    elif command -v shasum >/dev/null 2>&1; then
        sha256_cmd="shasum"
    else
        echo "error: не найдена утилита для проверки SHA256 (sha256sum или shasum)"
        echo "hint: установите coreutils (linux) или используйте macOS 10.13+"
        exit 1
    fi

    # ── Скачивание архива и SHA256SUMS (SR-99, SR-103) ───────────────────────

    echo "==> скачивание ${archive}..."
    if ! curl -fsSL "${archive_url}" -o "${tmpdir}/${archive}"; then
        echo "error: сбой скачивания архива: ${archive_url}"
        echo "hint: проверьте подключение к сети и корректность RAXD_BASE_URL/RAXD_VERSION"
        exit 5
    fi

    echo "==> скачивание SHA256SUMS..."
    if ! curl -fsSL "${sums_url}" -o "${tmpdir}/SHA256SUMS"; then
        echo "error: сбой скачивания SHA256SUMS: ${sums_url}"
        echo "hint: проверьте доступность ${base_url}/SHA256SUMS"
        exit 5
    fi

    # ── Проверка SHA256 ДО размещения (AC3, SR-100) ──────────────────────────

    echo "==> проверка целостности SHA256..."

    # Фильтруем SHA256SUMS: оставляем только строку для нашего архива.
    # Формат SHA256SUMS: '<hash>  <filename>' (два пробела, GNU sha256sum).
    local filtered_sums="${tmpdir}/SHA256SUMS.filtered"
    grep -F "  ${archive}" "${tmpdir}/SHA256SUMS" > "${filtered_sums}" || {
        echo "error: запись для '${archive}' не найдена в SHA256SUMS"
        echo "hint: убедитесь, что RAXD_VERSION='${version}' соответствует опубликованному релизу"
        exit 3
    }

    # Переходим во временный каталог для проверки (sha256sum -c ожидает файлы рядом).
    local check_ok=0
    (
        cd "${tmpdir}"
        if [[ "$sha256_cmd" == "sha256sum" ]]; then
            sha256sum -c "SHA256SUMS.filtered" --quiet 2>/dev/null
        else
            # shasum (macOS): shasum -a 256 -c
            shasum -a 256 -c "SHA256SUMS.filtered" --quiet 2>/dev/null
        fi
    ) || check_ok=1

    if [[ "$check_ok" -ne 0 ]]; then
        echo "error: несовпадение SHA256 — архив повреждён или подменён"
        echo "hint: попробуйте установку заново; если ошибка повторяется — сообщите разработчикам"
        exit 3
    fi

    echo "==> SHA256 совпадает — артефакт целостен"

    # ── Распаковка (SR-103) ───────────────────────────────────────────────────

    echo "==> распаковка..."
    tar -xzf "${tmpdir}/${archive}" -C "${tmpdir}"

    local bin_src="${tmpdir}/raxd"
    if [[ ! -f "$bin_src" ]]; then
        echo "error: бинарь 'raxd' не найден в архиве"
        exit 1
    fi

    # ── Определение каталога установки (AC9, ADR-003, SR-106) ─────────────────

    local install_dir
    if [[ -n "$prefix" ]]; then
        # Явный override через --prefix или RAXD_PREFIX
        install_dir="$prefix"
    elif [[ -w "/usr/local/bin" ]] || [[ "$(id -u)" -eq 0 ]]; then
        install_dir="/usr/local/bin"
    else
        install_dir="${HOME}/.local/bin"
    fi

    local dst="${install_dir}/raxd"

    # ── Установка с учётом привилегий (AC9, SR-106, SR-107) ──────────────────

    mkdir -p "${install_dir}" 2>/dev/null || true

    if [[ -w "${install_dir}" ]]; then
        # Установка без sudo (атомарная замена через install, SR-107)
        echo "==> установка в ${dst}..."
        install -m 0755 "${bin_src}" "${dst}"
    else
        # Каталог не writable — пробуем sudo
        if ! command -v sudo >/dev/null 2>&1; then
            echo "error: нет прав на запись в ${install_dir} и sudo недоступен"
            echo "hint: запустите от root или укажите другой каталог через --prefix ~/.local/bin"
            exit 4
        fi

        echo "==> для установки в ${install_dir} требуются права администратора..."
        echo "hint: sudo install -m 0755 raxd ${dst}"

        if ! sudo install -m 0755 "${bin_src}" "${dst}"; then
            echo "error: не удалось установить бинарь с sudo в ${install_dir}"
            echo "hint: используйте --prefix для выбора каталога без root-прав"
            exit 4
        fi
    fi

    echo "==> бинарь установлен: ${dst}"

    # ── macOS quarantine (AC11, ADR-005, SR-109) ──────────────────────────────

    if [[ "$os" == "darwin" ]]; then
        xattr -d com.apple.quarantine "${dst}" 2>/dev/null || true
        echo ""
        echo "hint: если macOS Gatekeeper блокирует запуск raxd, выполните:"
        echo "  xattr -d com.apple.quarantine ${dst}"
        echo "  или: System Settings → Privacy & Security → разрешить '${dst}'"
        echo "hint: для нотаризированной версии с Green Gatekeeper — запрашивайте нотаризацию через Apple Developer Program"
        echo ""
    fi

    # ── Проверка доступности в PATH (AC9, SR-108) ─────────────────────────────

    if ! command -v raxd >/dev/null 2>&1; then
        local path_hint="export PATH=\"${install_dir}:\$PATH\""
        echo "hint: raxd установлен, но каталог ${install_dir} не в \$PATH"
        echo "hint: добавьте в ~/.bashrc или ~/.zshrc:"
        echo "  ${path_hint}"
    fi

    # ── Опциональный hint про сервис (AC1, spec Out of Scope) ─────────────────

    echo ""
    echo "==> raxd успешно установлен (${version})"
    echo "hint: для регистрации системного сервиса выполните: raxd service install"
    echo ""
}

# ── Единственный вызов main в самом конце (SR-97) ─────────────────────────────
# Ничего после этой строки не исполняется при обрыве закачки curl|sh.
main "$@"
