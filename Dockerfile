# Dockerfile for raxd — dev/test environment.
#
# SECURITY-BASELINE §6: All builds, tests, and raxd execution must happen
# inside this container — never on the host machine.
#
# Run tests:
#   docker build --target test -t raxd-test . && docker run --rm raxd-test
#
# Run build only:
#   docker build --target build -t raxd-build .
#
# One-liner (build + test without keeping an image):
#   docker run --rm -v "$PWD":/src -w /src golang:1.25 sh -c "CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./..."

FROM golang:1.25 AS base

ENV CGO_ENABLED=0
WORKDIR /src

# Copy module metadata and vendor directory first — rebuilds only when these change.
# Vendor mode (-mod=vendor) avoids network access: all dependencies are in ./vendor.
COPY go.mod go.sum ./
COPY vendor/ vendor/

# Copy source.
COPY . .

# ── build stage ──────────────────────────────────────────────────────────────
FROM base AS build
RUN go vet ./... && go build -o /bin/raxd ./cmd/raxd

# ── test stage ───────────────────────────────────────────────────────────────
FROM base AS test
# Run go vet + go test (all packages) + go test -race (keystore, where concurrent
# Verify/FlushUsage races on usageBuf are the primary risk).
# Race detector requires CGO; keystore race step overrides CGO_ENABLED=0.
# Exit code of the last failing command propagates to the caller.
CMD ["sh", "-c", "go vet ./... && go test -v -count=1 ./... && CGO_ENABLED=1 go test -race -count=1 ./internal/keystore/..."]
