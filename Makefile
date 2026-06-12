# Makefile — raxd build infrastructure
#
# SECURITY-BASELINE §6: все сборки и тесты — только в Docker, не на хосте.
# AC14/AC15/AC16: кросс-сборка под 4 цели + systemd-интеграция в контейнере.
#
# Использование:
#   make build-all         — собрать 4 бинаря в dist/
#   make verify-cross      — проверить форматы (file + нативный version)
#   make docker-systemd    — собрать образ для systemd-тестов
#   make test-service      — собрать образ + запустить сценарий интеграции
#   make test-unit         — unit-тесты (офлайн, vendor)
#   make clean             — удалить dist/

# ── Variables ────────────────────────────────────────────────────────────────

# Go flags: offline vendor, CGO disabled for cross-compilation.
# -buildvcs=false: версия/коммит вшиваются явно через VERSION_LDFLAGS (buildCommit=none),
#   авто-VCS-штамп Go не нужен и ломает сборку в Docker-CI ("dubious ownership", exit 128).
GO        := go
GOFLAGS   := -mod=vendor -buildvcs=false
CGO_OFF   := CGO_ENABLED=0
LDFLAGS   := -ldflags="-s -w"

# Output directory for compiled binaries.
DIST_DIR  := dist

# Main package path.
CMD       := ./cmd/raxd

# Docker image name for systemd integration tests.
SYSTEMD_IMAGE := raxd-systemd-test
SYSTEMD_CTR   := raxd-svc-test

# Docker image name for install-flow tests (AC12, Dockerfile.install).
INSTALL_TEST_IMAGE := raxd-install-test

# Detect native arch for verify-cross (nativee binary to run).
NATIVE_GOOS   := linux
NATIVE_GOARCH := $(shell uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

# Release version: from git tag or override.
# Usage: make release VERSION=v0.1.0
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo "dev")

# ldflags with version metadata (AC10, SR-110).
# Целевые переменные — в cmd/raxd/main.go (package main).
# Для package main ldflags путь = "main.<var>" (не полный import path).
VERSION_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
VERSION_DATE   := $(shell date -u +%Y-%m-%d 2>/dev/null || echo "unknown")
VERSION_LDFLAGS := -ldflags="-s -w \
	-X main.buildVersion=$(VERSION) \
	-X main.buildCommit=$(VERSION_COMMIT) \
	-X main.buildDate=$(VERSION_DATE)"

# ── Phony targets ────────────────────────────────────────────────────────────

.PHONY: all build-all \
        build-linux-amd64 build-linux-arm64 \
        build-darwin-amd64 build-darwin-arm64 \
        verify-cross \
        docker-systemd test-service test-unit \
        release checksums release-all \
        test-install test-install-edge test-install-all ci-local \
        clean

all: build-all

# ── Cross-compilation (AC14, AC15, SR-96) ────────────────────────────────────
#
# SECURITY-BASELINE §6 docker-guard: go build выполняется ТОЛЬКО внутри Docker.
# На хосте — fail-fast с понятной ошибкой. Вызов: docker run raxd-build make build-all.
# Dockerfile-стадия build (RUN go build) не использует эти таргеты — она вызывает
# go build напрямую, поэтому docker-guard здесь не мешает сборке образа.

# Внутренний макрос docker-guard: абортируем если /.dockerenv отсутствует.
define DOCKER_GUARD
	@test -f /.dockerenv || { \
		echo "ERROR: 'make $@' нельзя запускать на хосте (SECURITY-BASELINE §6)."; \
		echo "  Запустите сборку внутри Docker:"; \
		echo "    docker run --rm -v \"\$$(pwd)/dist:/src/dist\" -e VERSION=\$(VERSION) -w /src raxd-build make build-all release-all VERSION=\$(VERSION)"; \
		echo "  Или используйте: make ci-local VERSION=\$(VERSION)"; \
		exit 1; \
	}
endef

## build-all: компилировать под все 4 цели (darwin/linux × amd64/arm64) — ТОЛЬКО в Docker
build-all: build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64
	@echo "build-all: 4 artifacts in $(DIST_DIR)/"
	@ls -lh $(DIST_DIR)/

## build-linux-amd64: linux/amd64 — ТОЛЬКО в Docker (§6)
build-linux-amd64: $(DIST_DIR)
	$(DOCKER_GUARD)
	$(CGO_OFF) GOOS=linux GOARCH=amd64 $(GO) build \
		$(GOFLAGS) $(VERSION_LDFLAGS) \
		-o $(DIST_DIR)/raxd_linux_amd64 \
		$(CMD)
	@echo "OK: $(DIST_DIR)/raxd_linux_amd64"

## build-linux-arm64: linux/arm64 — ТОЛЬКО в Docker (§6)
build-linux-arm64: $(DIST_DIR)
	$(DOCKER_GUARD)
	$(CGO_OFF) GOOS=linux GOARCH=arm64 $(GO) build \
		$(GOFLAGS) $(VERSION_LDFLAGS) \
		-o $(DIST_DIR)/raxd_linux_arm64 \
		$(CMD)
	@echo "OK: $(DIST_DIR)/raxd_linux_arm64"

## build-darwin-amd64: darwin/amd64 — ТОЛЬКО в Docker (§6)
build-darwin-amd64: $(DIST_DIR)
	$(DOCKER_GUARD)
	$(CGO_OFF) GOOS=darwin GOARCH=amd64 $(GO) build \
		$(GOFLAGS) $(VERSION_LDFLAGS) \
		-o $(DIST_DIR)/raxd_darwin_amd64 \
		$(CMD)
	@echo "OK: $(DIST_DIR)/raxd_darwin_amd64"

## build-darwin-arm64: darwin/arm64 (Apple Silicon) — ТОЛЬКО в Docker (§6)
build-darwin-arm64: $(DIST_DIR)
	$(DOCKER_GUARD)
	$(CGO_OFF) GOOS=darwin GOARCH=arm64 $(GO) build \
		$(GOFLAGS) $(VERSION_LDFLAGS) \
		-o $(DIST_DIR)/raxd_darwin_arm64 \
		$(CMD)
	@echo "OK: $(DIST_DIR)/raxd_darwin_arm64"

$(DIST_DIR):
	mkdir -p $(DIST_DIR)

# ── Cross-build verification (AC14) ─────────────────────────────────────────

## verify-cross: проверить все 4 артефакта (file + нативный version)
## Требует: build-all выполнен и нативный бинарь совпадает с NATIVE_GOARCH.
verify-cross: build-all
	@echo "=== verify-cross: checking binary formats ==="
	@echo "--- raxd_linux_amd64 ---"
	@file $(DIST_DIR)/raxd_linux_amd64
	@echo "--- raxd_linux_arm64 ---"
	@file $(DIST_DIR)/raxd_linux_arm64
	@echo "--- raxd_darwin_amd64 ---"
	@file $(DIST_DIR)/raxd_darwin_amd64
	@echo "--- raxd_darwin_arm64 ---"
	@file $(DIST_DIR)/raxd_darwin_arm64
	@echo ""
	@echo "=== verify-cross: running native binary ($(NATIVE_GOOS)/$(NATIVE_GOARCH)) ==="
	@# SECURITY-BASELINE §6: raxd must only run inside a container, never on the host.
	@# Guard: abort if /.dockerenv is absent (i.e. we are not inside a container).
	@test -f /.dockerenv || { \
		echo "ERROR: 'make verify-cross' must be run inside Docker (SECURITY-BASELINE §6)."; \
		echo "  hint: docker run --rm -v \$$(pwd)/$(DIST_DIR):/dist raxd-build /dist/raxd_linux_$(NATIVE_GOARCH) version"; \
		exit 1; \
	}
	@./$(DIST_DIR)/raxd_$(NATIVE_GOOS)_$(NATIVE_GOARCH) version && \
		echo "PASS: native binary executes and returns version" || \
		(echo "FAIL: native binary did not execute successfully" && exit 1)
	@echo ""
	@echo "=== verify-cross: PASSED ==="

# ── Unit tests (offline, vendor) ─────────────────────────────────────────────

## test-unit: go test (офлайн из vendor, без Docker)
## Для запуска в Docker: docker build --target test -t raxd-test . && docker run --rm raxd-test
test-unit:
	$(GO) vet $(GOFLAGS) ./...
	$(GO) test $(GOFLAGS) -v -count=1 ./...

# ── systemd Docker integration (AC16, baseline §6) ───────────────────────────

## docker-systemd: собрать Docker-образ для systemd-интеграционных тестов
docker-systemd:
	docker build -f Dockerfile.systemd -t $(SYSTEMD_IMAGE) .
	@echo "OK: image $(SYSTEMD_IMAGE) built"

## test-service: собрать образ + нативный бинарь + запустить сценарий интеграции
## SECURITY-BASELINE §6: сервисные тесты — ТОЛЬКО в контейнере, не на хосте.
## QA: запускает scripts/integration-service.sh — автоматизированный сценарий (AC1-AC12, AC16).
test-service: docker-systemd build-linux-amd64
	@echo "=== test-service: starting systemd container ==="
	@# Остановить и удалить предыдущий контейнер если был
	docker rm -f $(SYSTEMD_CTR) 2>/dev/null || true
	@# Запустить контейнер с systemd
	docker run -d \
		--name $(SYSTEMD_CTR) \
		--privileged \
		--cgroupns=host \
		-v /sys/fs/cgroup:/sys/fs/cgroup:rw \
		$(SYSTEMD_IMAGE)
	@echo "Waiting for systemd to start..."
	@sleep 4
	@# Скопировать нативный бинарь и интеграционный скрипт
	docker cp $(DIST_DIR)/raxd_linux_amd64 $(SYSTEMD_CTR):/usr/local/bin/raxd
	docker exec $(SYSTEMD_CTR) chmod 0755 /usr/local/bin/raxd
	docker cp scripts/integration-service.sh $(SYSTEMD_CTR):/integration-service.sh
	docker exec $(SYSTEMD_CTR) chmod +x /integration-service.sh
	@echo "=== Запуск интеграционного сценария (AC1-AC12, AC16)... ==="
	@echo ""
	docker exec $(SYSTEMD_CTR) /integration-service.sh
	@echo ""
	@echo "=== test-service PASSED ==="
	@echo "Для очистки: make stop-service-test"

## stop-service-test: остановить и удалить контейнер интеграционных тестов
stop-service-test:
	docker stop $(SYSTEMD_CTR) 2>/dev/null || true
	docker rm $(SYSTEMD_CTR) 2>/dev/null || true
	@echo "OK: $(SYSTEMD_CTR) stopped and removed"

# ── Release: архивы + SHA256SUMS (AC8/AC10/AC15/AC16, ADR-001) ───────────────
#
# Таргеты release/checksums/release-all вызывают scripts/release.sh.
# Единственный источник имён артефактов: scripts/release.sh (SR-101).
# Версия: VERSION (из git describe или override, AC10, SR-110).
#
# Использование:
#   make release-all VERSION=v0.1.0

## release: собрать 4 архива tar.gz в dist/ (требует build-all, ТОЛЬКО в Docker §6)
## Предусловие: build-all должен быть выполнен (бинари уже в dist/).
## Не вызывает build-all как Make-prereq — чтобы не допустить хостовой сборки.
release:
	$(DOCKER_GUARD)
	@echo "=== release: VERSION=$(VERSION) ==="
	VERSION=$(VERSION) DIST_DIR=$(DIST_DIR) bash scripts/release.sh

## checksums: сгенерировать SHA256SUMS из существующих архивов (без пересборки)
checksums:
	@echo "=== checksums: VERSION=$(VERSION) ==="
	VERSION=$(VERSION) DIST_DIR=$(DIST_DIR) bash scripts/release.sh --checksums-only

## release-all: release + checksums (полный релизный цикл)
release-all: release
	@echo "=== release-all: ГОТОВО ==="
	@echo "    артефакты: $(DIST_DIR)/raxd_$(VERSION)_*.tar.gz"
	@echo "    контрольные суммы: $(DIST_DIR)/SHA256SUMS"

# ── test-install: AC12 install-flow в чистом Docker-контейнере ───────────────
#
# SECURITY-BASELINE §6: test-install гоняется в чистом debian-контейнере.
# docker-guard /.dockerenv защищает от случайного запуска на хосте.
# ADR-002: мок-HTTP python3 -m http.server --bind 127.0.0.1 (SR-113).
#
# Предусловие: make release-all VERSION=<v> должен быть выполнен.

## test-install: прогнать install-flow в чистом контейнере (AC12, baseline §6)
##
## SECURITY-BASELINE §6: test-install НЕ пересобирает бинари — он ПОТРЕБЛЯЕТ
## готовый dist/ (собранный в Docker через make ci-local или docker run raxd-build).
## Prereq release-all УДАЛЁН намеренно: он тянул build-all → go build на хосте (баг D-1).
##
## Предусловие: dist/ должен содержать SHA256SUMS (выполните make ci-local или
##   docker run --rm -v "$(pwd)/dist:/src/dist" -e VERSION=<v> -w /src raxd-build \
##     sh -c "make build-all release-all VERSION=<v>").
test-install:
	@# Проверяем наличие готового dist/ — go build на хосте ЗАПРЕЩЁН (§6/SR-112, D-1).
	@test -f $(DIST_DIR)/SHA256SUMS || { \
		echo "ERROR: $(DIST_DIR)/SHA256SUMS не найден."; \
		echo "  Сначала соберите артефакты в Docker (go build на хосте запрещён — §6):"; \
		echo "    make ci-local VERSION=$(VERSION)"; \
		echo "  Или вручную:"; \
		echo "    docker run --rm -v \"\$$(pwd)/dist:/src/dist\" -e VERSION=$(VERSION) -w /src raxd-build sh -c \"make build-all release-all VERSION=$(VERSION)\""; \
		exit 1; \
	}
	@echo "=== test-install: сборка образа $(INSTALL_TEST_IMAGE)... ==="
	docker build -f Dockerfile.install -t $(INSTALL_TEST_IMAGE) \
		--build-arg VERSION=$(VERSION) .
	@echo "=== test-install: запуск в контейнере... ==="
	docker run --rm \
		-e VERSION=$(VERSION) \
		-e PORT=8000 \
		$(INSTALL_TEST_IMAGE)
	@echo "=== test-install: ПРОШЁЛ ==="

# ── test-install-edge: edge-тесты install-flow (AC2/AC4/AC7/AC9/AC11/AC16) ───
#
# SECURITY-BASELINE §6: гоняется в том же Dockerfile.install-контейнере.
# docker-guard /.dockerenv защищает от случайного запуска на хосте.
# Покрывает: AC2(усечённый скрипт), AC4(неподдерж. платформа), AC7(минимизация),
#            AC9(нет прав→код 4), AC11(darwin-ветка), AC16(согласованность имён).
#
# Предусловие: dist/ должен содержать SHA256SUMS.

# Docker-образ для edge-тестов (тот же Dockerfile.install, другая CMD).
INSTALL_EDGE_IMAGE := raxd-install-edge-test

## test-install-edge: edge-тесты install.sh в чистом контейнере (AC2/AC4/AC7/AC9/AC11/AC16)
test-install-edge:
	@# Проверяем наличие готового dist/ — go build на хосте ЗАПРЕЩЁН (§6/SR-112, D-1).
	@test -f $(DIST_DIR)/SHA256SUMS || { \
		echo "ERROR: $(DIST_DIR)/SHA256SUMS не найден."; \
		echo "  Сначала соберите артефакты в Docker (go build на хосте запрещён — §6):"; \
		echo "    make ci-local VERSION=$(VERSION)"; \
		exit 1; \
	}
	@echo "=== test-install-edge: сборка образа $(INSTALL_EDGE_IMAGE)... ==="
	docker build -f Dockerfile.install -t $(INSTALL_EDGE_IMAGE) \
		--build-arg VERSION=$(VERSION) .
	@echo "=== test-install-edge: запуск edge-тестов в контейнере... ==="
	docker run --rm \
		-e VERSION=$(VERSION) \
		-e PORT=8001 \
		--entrypoint bash \
		$(INSTALL_EDGE_IMAGE) \
		scripts/test-install-edge.sh
	@echo "=== test-install-edge: ПРОШЁЛ ==="

## test-install-all: TEST1-3 + edge-тесты TEST4-9 (полное покрытие install.sh)
test-install-all: test-install test-install-edge
	@echo ""
	@echo "=== test-install-all: ВСЕ ТЕСТЫ (TEST1-9) ПРОШЛИ ==="

# ── ci-local: полный локальный CI в Docker (AC14, baseline §6) ───────────────
#
# SECURITY-BASELINE §6: ни один шаг не выполняет go build на хосте.
# Фикс D-1: test-install НЕ имеет prereq release-all — он потребляет dist/,
# заполненный шагом "docker run raxd-build make build-all release-all" выше.
#
# Порядок:
#   1. docker build + docker run raxd-test  — go vet + unit-тесты в Docker
#   2. docker build raxd-build              — образ для кросс-компиляции
#   3. docker run raxd-build make build-all release-all  — 4 бинари + архивы в dist/ (В DOCKER)
#   4. make test-install                    — потребляет готовый dist/, go build НЕ вызывается

## ci-local: локальный CI-гейт (go vet+test + build-all + test-install) — всё в Docker (§6)
ci-local:
	@echo "=== ci-local: сборка образа для unit-тестов... ==="
	docker build --target test -t raxd-test .
	@echo "=== ci-local: go vet + unit-тесты... ==="
	docker run --rm raxd-test
	@echo "=== ci-local: сборка образа для кросс-компиляции... ==="
	docker build --target build -t raxd-build .
	@echo "=== ci-local: build-all (4 цели) + release-all — в Docker (§6)... ==="
	docker run --rm \
		-v "$(PWD)/dist:/src/dist" \
		-e VERSION=$(VERSION) \
		-w /src \
		raxd-build \
		sh -c "make build-all release-all VERSION=$(VERSION)"
	@echo "=== ci-local: test-install (потребляет dist/ из Docker, go build не вызывается)... ==="
	$(MAKE) test-install VERSION=$(VERSION)
	@echo ""
	@echo "=== ci-local: ВСЕ ПРОВЕРКИ ПРОШЛИ ==="
	@echo "    VERSION=$(VERSION)"
	@echo "    Артефакты: dist/raxd_$(VERSION)_*.tar.gz"
	@echo "    SHA256SUMS: dist/SHA256SUMS"

# ── Cleanup ──────────────────────────────────────────────────────────────────

## clean: удалить скомпилированные артефакты
clean:
	rm -rf $(DIST_DIR)
	@echo "OK: dist/ removed"

# ── Help ─────────────────────────────────────────────────────────────────────

## help: показать список таргетов
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'

.PHONY: help stop-service-test
