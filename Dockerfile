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

# Cache module downloads separately from source — rebuild only when go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download

# Copy source.
COPY . .

# ── build stage ──────────────────────────────────────────────────────────────
FROM base AS build
RUN go vet ./... && go build -o /bin/raxd ./cmd/raxd

# ── test stage ───────────────────────────────────────────────────────────────
FROM base AS test
# Run go vet + go test. Exit code is propagated to the caller.
CMD ["sh", "-c", "go vet ./... && go test -v -count=1 ./..."]
