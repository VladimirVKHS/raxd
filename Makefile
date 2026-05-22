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
GO        := go
GOFLAGS   := -mod=vendor
CGO_OFF   := CGO_ENABLED=0
LDFLAGS   := -ldflags="-s -w"

# Output directory for compiled binaries.
DIST_DIR  := dist

# Main package path.
CMD       := ./cmd/raxd

# Docker image name for systemd integration tests.
SYSTEMD_IMAGE := raxd-systemd-test
SYSTEMD_CTR   := raxd-svc-test

# Detect native arch for verify-cross (nativee binary to run).
NATIVE_GOOS   := linux
NATIVE_GOARCH := $(shell uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

# ── Phony targets ────────────────────────────────────────────────────────────

.PHONY: all build-all \
        build-linux-amd64 build-linux-arm64 \
        build-darwin-amd64 build-darwin-arm64 \
        verify-cross \
        docker-systemd test-service test-unit \
        clean

all: build-all

# ── Cross-compilation (AC14, AC15, SR-96) ────────────────────────────────────

## build-all: компилировать под все 4 цели (darwin/linux × amd64/arm64)
build-all: build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64
	@echo "build-all: 4 artifacts in $(DIST_DIR)/"
	@ls -lh $(DIST_DIR)/

## build-linux-amd64: linux/amd64
build-linux-amd64: $(DIST_DIR)
	$(CGO_OFF) GOOS=linux GOARCH=amd64 $(GO) build \
		$(GOFLAGS) $(LDFLAGS) \
		-o $(DIST_DIR)/raxd_linux_amd64 \
		$(CMD)
	@echo "OK: $(DIST_DIR)/raxd_linux_amd64"

## build-linux-arm64: linux/arm64
build-linux-arm64: $(DIST_DIR)
	$(CGO_OFF) GOOS=linux GOARCH=arm64 $(GO) build \
		$(GOFLAGS) $(LDFLAGS) \
		-o $(DIST_DIR)/raxd_linux_arm64 \
		$(CMD)
	@echo "OK: $(DIST_DIR)/raxd_linux_arm64"

## build-darwin-amd64: darwin/amd64
build-darwin-amd64: $(DIST_DIR)
	$(CGO_OFF) GOOS=darwin GOARCH=amd64 $(GO) build \
		$(GOFLAGS) $(LDFLAGS) \
		-o $(DIST_DIR)/raxd_darwin_amd64 \
		$(CMD)
	@echo "OK: $(DIST_DIR)/raxd_darwin_amd64"

## build-darwin-arm64: darwin/arm64 (Apple Silicon)
build-darwin-arm64: $(DIST_DIR)
	$(CGO_OFF) GOOS=darwin GOARCH=arm64 $(GO) build \
		$(GOFLAGS) $(LDFLAGS) \
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
