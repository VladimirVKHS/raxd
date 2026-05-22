# Dockerfile for raxd — dev/test environment.
#
# SECURITY-BASELINE §6: All builds, tests, and raxd execution must happen
# inside this container — never on the host machine.
#
# Offline/hermetic build via vendor/ (ADR-002): no network access required.
# Update vendor: run "go mod vendor" on the host, then commit vendor/.
#
# Run tests:
#   docker build --target test -t raxd-test . && docker run --rm raxd-test
#
# Run build only:
#   docker build --target build -t raxd-build .

FROM golang:1.25 AS base

ENV CGO_ENABLED=0
# Use vendored dependencies — no network required (ADR-002).
ENV GOFLAGS="-mod=vendor"
WORKDIR /src

# Copy module metadata and vendor tree.
# Vendor layer is cached separately: rebuilds only when go.mod/go.sum/vendor change.
COPY go.mod go.sum ./
COPY vendor/ vendor/

# Copy source.
COPY . .

# ── build stage ──────────────────────────────────────────────────────────────
FROM base AS build
RUN go vet ./... && go build -o /bin/raxd ./cmd/raxd

# ── test stage ───────────────────────────────────────────────────────────────
FROM base AS test
# Run go vet + go test (all packages) + go test -race (keystore).
# -race requires CGO; the keystore step overrides CGO_ENABLED=0.
# All module resolution is from vendor/ — no outbound network calls.
CMD ["sh", "-c", "go vet ./... && go test -v -count=1 ./... && CGO_ENABLED=1 go test -race -count=1 ./internal/cmdexec/... ./internal/keystore/... ./internal/server/... ./internal/mcp/..."]
