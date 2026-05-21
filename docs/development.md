# Development guide

This guide explains how to build and test `raxd`, how the project is laid out, and how
build metadata is injected. It targets contributors working on the codebase.

## Why Docker only

`raxd` is designed to execute commands over the network, which makes it a powerful and potentially
dangerous tool (like SSH). For that reason the security baseline (§6) requires that **all builds,
tests, and any execution of `raxd` happen inside a Docker container — never on the host machine.**

Most of the dangerous functionality (TLS transport, command execution, the `serve` daemon) does not
exist yet, but the Docker-only workflow is established from the start so it is already in place when
those features arrive. The key-management code that exists today writes a `keys.db` file under the
state directory — running it in a container keeps that file inside the container, off the host.

## Build and test in Docker

The repository ships a `Dockerfile` with two named stages built on `golang:1.25` with
`CGO_ENABLED=0`. Dependencies are vendored (`vendor/` is committed to git), so builds and tests run
fully offline from the source tree — no network access and no `go mod download` are needed. See
[Vendoring and offline builds](#vendoring-and-offline-builds) below.

### Run the test suite

This builds the project and runs `go vet` followed by the full test suite (`go test -v`):

```sh
docker build --target test -t raxd-test . && docker run --rm raxd-test
```

The container's exit code is propagated, so a failing test fails the command.

To run a single package — for example the keystore tests — mount the source and target it directly:

```sh
docker run --rm -v "$PWD":/src -w /src golang:1.25 \
  sh -c "CGO_ENABLED=0 go test -v -count=1 ./internal/keystore/..."
```

The keystore tests include data-race checks; run them with the race detector:

```sh
docker run --rm -v "$PWD":/src -w /src golang:1.25 \
  sh -c "go test -race ./internal/keystore/..."
```

### Build only

This compiles the binary and runs `go vet` (the `build` stage produces `/bin/raxd` inside the
image):

```sh
docker build --target build -t raxd-build .
```

### One-off build + test without keeping an image

Mount the working directory into a stock `golang:1.25` container and run both steps directly:

```sh
docker run --rm -v "$PWD":/src -w /src golang:1.25 \
  sh -c "CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./..."
```

To run `go vet` and the verbose test suite the same way:

```sh
docker run --rm -v "$PWD":/src -w /src golang:1.25 \
  sh -c "CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test -v -count=1 ./..."
```

## Project layout

The project follows a single entry point plus internal packages. Putting the implementation under
`internal/` means those packages cannot be imported from outside the module, which keeps the public
surface intentionally empty at this stage.

```
.
├── cmd/
│   └── raxd/
│       └── main.go          Entry point: sets build metadata, runs the CLI, maps errors to exit codes
├── internal/
│   ├── cli/                 Cobra command tree
│   │   ├── root.go          Root command, sub-command registration, banner via PersistentPreRun
│   │   ├── key.go           "key" group: create / list / delete (working)
│   │   ├── config.go        "config" group: port (stub)
│   │   ├── serve.go         "serve" honest stub
│   │   ├── version.go       "version" command (working)
│   │   ├── status.go        "status" command (working)
│   │   └── stub.go          Shared not-implemented-yet helper for stub commands
│   ├── keystore/            API key generation, storage, verification, revocation
│   │   ├── keystore.go      Store: Open / Create / List / Revoke / Verify / FlushUsage
│   │   ├── crypto.go        Key body / salt / id generation, hashing, fingerprint (crypto/rand)
│   │   ├── record.go        Record / dbRecord / Database / PlainKey types
│   │   ├── lock.go          Advisory flock (shared for reads, exclusive for writes)
│   │   └── errors.go        Sentinel errors (ErrNotFound, ErrAlreadyRevoked, ErrCorrupt, ErrLabelTooLong)
│   ├── config/
│   │   ├── paths.go         XDG path resolution (PathSet, Paths, EnsureDirs)
│   │   └── config.go        config.yaml loading via viper (Config, Load)
│   ├── version/
│   │   └── version.go       Build metadata storage (Set, Info)
│   └── banner/
│       └── banner.go        Plain-text product banner with the author line
├── vendor/                  Committed dependency tree (offline / hermetic builds, ADR-002)
├── Dockerfile               Two-stage (build / test) dev/test environment
├── go.mod                   Module github.com/vladimirvkhs/raxd, go 1.25
└── go.sum
```

### How the pieces fit together

- `cmd/raxd/main.go` declares the package-level `buildVersion` / `buildCommit` / `buildDate`
  variables (the ldflags targets), passes them into `version.Set(...)`, then calls `cli.Execute()`
  and maps a non-nil error to `os.Exit(1)`.
- `internal/cli/root.go` builds the command tree, sets `SilenceUsage` and `SilenceErrors` so that
  `main` controls error printing and exit codes, and prints the banner (and ensures the XDG
  directories exist) in `PersistentPreRun`.
- `internal/cli/key.go` implements the `key` group on top of `internal/keystore`: it opens the
  store at the `KeysDB` path, calls `Create` / `List` / `Revoke`, and renders output per the UX
  contract (key body on stdout, decoration on stderr).
- `internal/keystore` owns all secret handling: `crypto/rand` generation, `sha256(key‖salt)`
  hashing, constant-time `Verify`, atomic `0600` writes, and advisory flock. The plaintext key
  never leaves the caller's stack (`PlainKey`); it is not stored in any `Store` field.
- Stub commands (`config port`, `serve`) return a sentinel error from their `RunE`, which cobra
  turns into a non-zero exit.
- `version`, `status` print to stdout and return `nil` (exit `0`).

## Build metadata via ldflags

Version information is injected at build time using `-ldflags -X`. When the binary is built without
these flags (the normal development build), the defaults are `version=dev`, `commit=none`,
`date=unknown`.

To produce a build with real metadata (run this inside Docker):

```sh
go build -ldflags "\
  -X github.com/vladimirvkhs/raxd/internal/version.Version=$(git describe --tags --always) \
  -X github.com/vladimirvkhs/raxd/internal/version.Commit=$(git rev-parse --short HEAD) \
  -X github.com/vladimirvkhs/raxd/internal/version.Date=$(date -u +%Y-%m-%d)" \
  ./cmd/raxd
```

`raxd version` then prints the injected values, for example:

```
raxd 1.0.0 (commit abc1234, built 2025-06-01)
```

## Vendoring and offline builds

All dependencies are vendored: the `vendor/` directory is committed to git, and the Go toolchain
builds with `-mod=vendor` automatically when `vendor/` is present. The Dockerfile copies the source
together with `vendor/` and compiles **without** any networked `go mod download`, so a cold
`docker build` and CI both work offline. Module integrity is guaranteed by `go.sum` (checked by
`go mod verify`). The rationale and rejected alternatives are recorded in
[`specs/key-management/decisions/ADR-002-vendoring-offline-builds.md`](../specs/key-management/decisions/ADR-002-vendoring-offline-builds.md).

If you change dependencies (`go get` or an edit to `go.mod`), you **must** run `go mod vendor` and
commit the updated `vendor/` and `go.sum`; otherwise the offline build will break.

## Dependencies

The dependency set is kept small and limited to what the stack already approves. After `go mod tidy`
the direct dependencies (the `require` block in `go.mod`) are:

| Dependency | Purpose |
|------------|---------|
| `github.com/spf13/cobra` | CLI and sub-commands |
| `github.com/spf13/viper` | `config.yaml` loading |
| `github.com/olekukonko/tablewriter` | `key list` table rendering |
| `github.com/charmbracelet/log` | structured audit logging for `key create` / `key delete` |

Two notes on libraries that are present but not used directly:

- **`adrg/xdg` is not used at all.** XDG paths are resolved manually in the standard library
  (`os.Getenv` + `os.UserHomeDir`). The library's macOS default
  (`~/Library/Application Support`) would conflict with decision D3 (a single `~/.config/raxd` on
  both Linux and macOS), so the explicit resolution is more accurate for our contract.
- **`charmbracelet/lipgloss` (styling) is not imported directly.** It is present in the dependency
  tree only as a **transitive** dependency (pulled in via `charmbracelet/log`); no `raxd` package
  imports it. The banner and all command output are plain text. Adding styling later — using
  lipgloss directly — is an extension point; the `banner.Render() string` API is stable, so it would
  be a local change.

## Related documents

- [`commands.md`](commands.md) — what each command does and outputs.
- [`configuration.md`](configuration.md) — paths, the `keys.db` database, and the `config.yaml` format.
- The repository root [`README.md`](../README.md) — overview and quick start.
