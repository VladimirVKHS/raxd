#!/usr/bin/env bash
# scripts/release.sh — релизная сборка: архивация + SHA256SUMS
#
# SECURITY-BASELINE §6: запускается из Makefile (таргет release/release-all)
# под docker-guard — только внутри Docker-контейнера.
#
# Единственный источник имён артефактов (AC16, SR-101):
#   dist/raxd_<VERSION>_<os>_<arch>.tar.gz
#
# Использование:
#   VERSION=v0.1.0 bash scripts/release.sh [--checksums-only]
#   make release VERSION=v0.1.0
#
# Предусловие: make build-all уже выполнен (dist/ содержит 4 бинаря).
#
# Выход:
#   dist/raxd_<VERSION>_linux_amd64.tar.gz
#   dist/raxd_<VERSION>_linux_arm64.tar.gz
#   dist/raxd_<VERSION>_darwin_amd64.tar.gz
#   dist/raxd_<VERSION>_darwin_arm64.tar.gz
#   dist/SHA256SUMS

set -euo pipefail

main() {
    local checksums_only=0

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --checksums-only) checksums_only=1 ;;
            *) echo "error: неизвестный флаг: $1"; exit 1 ;;
        esac
        shift
    done

    # ── Версия ────────────────────────────────────────────────────────────────
    # VERSION берётся из env (из Makefile: VERSION := $(shell git describe --tags --always))
    local version="${VERSION:-}"
    if [[ -z "$version" ]]; then
        echo "error: переменная VERSION не задана"
        echo "hint: запустите через 'make release VERSION=v0.1.0' или 'VERSION=v0.1.0 bash scripts/release.sh'"
        exit 1
    fi

    local dist_dir="${DIST_DIR:-dist}"

    # ── Матрица целей (AC8, SR-112) ───────────────────────────────────────────
    # Имена согласованы с install.sh (AC16, SR-101).
    local -a targets=(
        "linux_amd64"
        "linux_arm64"
        "darwin_amd64"
        "darwin_arm64"
    )

    if [[ "$checksums_only" -eq 0 ]]; then
        # ── Проверка наличия всех 4 бинарей ──────────────────────────────────

        echo "==> release ${version}: проверка бинарей в ${dist_dir}/..."

        for target in "${targets[@]}"; do
            local bin="${dist_dir}/raxd_${target}"
            if [[ ! -f "$bin" ]]; then
                echo "error: бинарь не найден: ${bin}"
                echo "hint: сначала выполните 'make build-all'"
                exit 1
            fi
        done

        echo "==> все 4 бинаря присутствуют"

        # ── Архивация (AC8, SR-102) ───────────────────────────────────────────
        # Формат: raxd_<version>_<os>_<arch>.tar.gz
        # Внутри архива: бинарь raxd + README.md (всегда) + LICENSE (если есть).

        echo "==> создание архивов tar.gz..."

        for target in "${targets[@]}"; do
            local bin="${dist_dir}/raxd_${target}"
            local archive="${dist_dir}/raxd_${version}_${target}.tar.gz"

            # Собираем список файлов для архива
            local -a files_to_archive=()

            # Бинарь переименовываем в 'raxd' внутри архива (стандарт goreleaser)
            # Делаем через временный каталог для корректного именования внутри архива.
            local pkg_dir
            pkg_dir="$(mktemp -d)"

            cleanup_pkg() { rm -rf "$pkg_dir"; }
            # shellcheck disable=SC2064
            trap "cleanup_pkg" RETURN

            cp "${bin}" "${pkg_dir}/raxd"
            chmod 0755 "${pkg_dir}/raxd"

            # README всегда включаем
            if [[ -f "README.md" ]]; then
                cp "README.md" "${pkg_dir}/README.md"
            else
                echo "warning: README.md не найден — пропускаем"
            fi

            # LICENSE включаем только если файл существует (AC8, задача: добавить LICENSE)
            if [[ -f "LICENSE" ]]; then
                cp "LICENSE" "${pkg_dir}/LICENSE"
            else
                echo "warning: LICENSE не найден — пропускаем (добавьте LICENSE перед публичным релизом)"
            fi

            echo "  -> ${archive}"
            # tar с относительными путями из pkg_dir
            tar -czf "${archive}" -C "${pkg_dir}" .

            # Сброс trap RETURN после создания архива
            trap - RETURN
            cleanup_pkg
        done

        echo "==> архивы созданы"
    fi

    # ── Генерация SHA256SUMS (AC8, SR-102) ────────────────────────────────────
    # Формат: '<hash>  <filename>' (два пробела — GNU sha256sum стандарт)
    # Совместим с sha256sum -c и shasum -a 256 -c (SR-100, SR-102).

    echo "==> генерация SHA256SUMS..."
    (
        cd "${dist_dir}"
        sha256sum raxd_"${version}"_*.tar.gz > SHA256SUMS
    )

    echo "==> SHA256SUMS:"
    cat "${dist_dir}/SHA256SUMS"
    echo "==> release ${version}: ГОТОВО"
    echo "    артефакты в ${dist_dir}/:"
    ls -lh "${dist_dir}/raxd_${version}"_*.tar.gz "${dist_dir}/SHA256SUMS"
}

main "$@"
