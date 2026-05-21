# Development guide

This guide explains how to build and test the `raxd` skeleton, how the project is laid out, and how
build metadata is injected. It targets contributors working on the `bootstrap-cli` skeleton.

## Why Docker only

`raxd` is designed to execute commands over the network, which makes it a powerful and potentially
dangerous tool (like SSH). For that reason the security baseline (§6) requires that **all builds,
tests, and any execution of `raxd` happen inside a Docker container — never on the host machine.**

On the skeleton there is nothing dangerous to run yet (the feature commands are stubs), but the
Docker-only workflow is established from the start so it is already in place when real functionality
arrives.

## Build and test in Docker

The repository ships a `Dockerfile` with two named stages built on `golang:1.25` with
`CGO_ENABLED=0`. Module downloads are cached separately from the source, so they are re-fetched only
when `go.mod` / `go.sum` change.

### Run the test suite

This builds the project and runs `go vet` followed by the full test suite (`go test -v`):

```sh
docker build --target test -t raxd-test . && docker run --rm raxd-test
```

The container's exit code is propagated, so a failing test fails the command.

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
│   │   ├── key.go           "key" group: create / list / delete (stubs)
│   │   ├── config.go        "config" group: port (stub)
│   │   ├── serve.go         "serve" honest stub
│   │   ├── version.go       "version" command (working)
│   │   ├── status.go        "status" command (working)
│   │   └── stub.go          Shared not-implemented-yet helper for stub commands
│   ├── config/
│   │   ├── paths.go         XDG path resolution (PathSet, Paths, EnsureDirs)
│   │   └── config.go        config.yaml loading via viper (Config, Load)
│   ├── version/
│   │   └── version.go       Build metadata storage (Set, Info)
│   └── banner/
│       └── banner.go        Plain-text product banner with the author line
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
- Stub commands return a sentinel error from their `RunE`, which cobra turns into a non-zero exit.
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

## Dependencies

The skeleton keeps its dependency set small and limited to what the stack already approves:

| Dependency | Version | Purpose |
|------------|---------|---------|
| `github.com/spf13/cobra` | v1.10.2 | CLI and sub-commands |
| `github.com/spf13/viper` | v1.21.0 | `config.yaml` loading |

Two deliberate omissions:

- **`adrg/xdg` is not used.** XDG paths are resolved manually in the standard library
  (`os.Getenv` + `os.UserHomeDir`). The library's macOS default
  (`~/Library/Application Support`) would conflict with decision D3 (a single `~/.config/raxd` on
  both Linux and macOS), so the explicit resolution is more accurate for our contract.
- **`charmbracelet/lipgloss` (styling) is not used yet.** The banner is plain text. The
  `banner.Render() string` API is stable, so adding styling later is a local change to the
  `internal/banner` package only.

## Related documents

- [`commands.md`](commands.md) — what each command does and outputs.
- [`configuration.md`](configuration.md) — paths and the `config.yaml` format.
- The repository root [`README.md`](../README.md) — overview and quick start.
