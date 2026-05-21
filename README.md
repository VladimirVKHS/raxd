# raxd

**raxd** is a remote access daemon for AI agents — a single cross-platform Go binary that is meant
to act as a system service, a CLI utility, a network server (TCP + TLS), and an MCP server for AI
agents, all at once.

> **Project status: early — this repository currently contains the project skeleton
> (`bootstrap-cli`).** A compilable command tree, configuration/path resolution, build metadata,
> a product banner, and a reproducible Docker dev/test environment are in place. The actual remote
> access functionality (API keys, TLS, command execution, MCP) is **not implemented yet** — see
> [Coming next](#coming-next).

Author: **Vladimir Kovalev, OEM TECH**.

---

## What is raxd

`raxd` (Remote Access Daemon) is designed to give AI agents secure, authenticated access to a
server. The end product is a single binary that is simultaneously:

- a system service (systemd on Linux, launchd on macOS);
- a CLI utility (`raxd <command>`);
- a network server (TCP + TLS);
- an MCP server for AI agents.

Target platforms: **macOS and Linux**, architectures **amd64 and arm64**. Windows is out of scope.

At this stage the binary is a **skeleton**: the command tree exists and two service commands
(`version`, `status`) are fully working, while the feature commands (`key`, `config`, `serve`) are
present as honest stubs that report `not implemented yet`. This lets the team build real logic on
top of a stable command tree.

## What works today

| Area | Status |
|------|--------|
| `raxd version` — print build metadata | **Working** |
| `raxd status` — show daemon state and config/state paths | **Working** |
| `raxd --help` and the full command tree | **Working** |
| Product banner with author (printed to stderr) | **Working** |
| XDG-based config/state path resolution (`~/.config/raxd`, `XDG_*` overrides) | **Working** |
| Directory creation with `0700` permissions | **Working** |
| `config.yaml` loading via viper (defaults, missing-file tolerance) | **Working (library; not yet wired into commands)** |
| `raxd key create` / `key list` / `key delete` | **Stub** (`not implemented yet`) |
| `raxd config port` | **Stub** (`not implemented yet`) |
| `raxd serve` | **Stub** (honest — prints and exits, does **not** open a port) |

Everything in the [Coming next](#coming-next) section is **not implemented yet**.

## Requirements

- [Go 1.25](https://go.dev/dl/) (module declares `go 1.25`).
- [Docker](https://www.docker.com/) — **all builds, tests, and any execution of `raxd` happen
  inside a container, never on the host.** `raxd` is designed to execute commands over the network,
  so its place is an isolated container (see [SECURITY-BASELINE §6] and `docs/development.md`).

## Quick start (Docker)

Clone the repository, then build and run the test suite inside Docker:

```sh
# Build the binary and run go vet + the full test suite in one step.
docker build --target test -t raxd-test . && docker run --rm raxd-test
```

To produce a build image only (compiles the binary, runs `go vet`):

```sh
docker build --target build -t raxd-build .
```

Build and test in a one-off container without keeping any image:

```sh
docker run --rm -v "$PWD":/src -w /src golang:1.25 \
  sh -c "CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./..."
```

See [`docs/development.md`](docs/development.md) for the project layout, how to inject build
metadata, and why the workflow is Docker-only.

## Commands

`raxd` exposes the following command tree. Service commands are working; feature commands are
stubs on this skeleton.

```
raxd
├── version            print version information           (working)
├── status             show daemon status and paths        (working)
├── key                manage API keys
│   ├── create         create a new API key                (stub)
│   ├── list           list all API keys                   (stub)
│   └── delete         delete an API key                   (stub)
├── config             manage configuration
│   └── port           set the listening port              (stub)
└── serve              start the raxd daemon                (stub)
```

A full reference with usage strings, exit codes, and output examples is in
[`docs/commands.md`](docs/commands.md).

### Example: `raxd version`

Prints a single line to **stdout** and exits with code `0`. On a build without ldflags
(the default development build), the values are `dev` / `none` / `unknown`:

```
raxd dev (commit none, built unknown)
```

A release build injects real values via ldflags, for example:

```
raxd 1.0.0 (commit abc1234, built 2025-06-01)
```

### Example: `raxd status`

Prints the daemon state and the resolved filesystem paths to **stdout** and exits with code `0`.
On this skeleton the state is always `not running`:

```
  state    not running
  config   /home/user/.config/raxd/config.yaml
  keys     /home/user/.local/state/raxd/keys.db
  tls      /home/user/.local/state/raxd/tls
```

If `config.yaml` does not exist yet, the path is still shown with an informational suffix (this is
not an error — defaults are applied):

```
  state    not running
  config   /home/user/.config/raxd/config.yaml  (not found, defaults applied)
  keys     /home/user/.local/state/raxd/keys.db
  tls      /home/user/.local/state/raxd/tls
```

For security, `status` never prints key material, TLS contents, or any secrets.

### The banner

Before every command, `raxd` prints a product banner to **stderr** (so it never pollutes the
machine-readable stdout — `raxd status | grep state` works cleanly). The banner is a plain-text
Unicode box and always contains the author line:

```
┌──────────────────────────────────────────┐
│  raxd  —  Remote Access Daemon            │
│  dev  ·  commit none  ·  built unknown    │
│  Vladimir Kovalev, OEM TECH               │
└──────────────────────────────────────────┘
```

The banner is **not** printed for `--help` (cobra prints help itself). On this skeleton the banner
always renders this single fixed (wide) layout — adaptive sizing and color/styling are planned (see
[Coming next](#coming-next)).

### Example: a stub command

Feature commands are honest stubs. They print an `error:` line to **stderr** and exit with a
non-zero code (`1`):

```
$ raxd key create --name laptop
error: key create: not implemented yet
```

## Configuration paths

`raxd` resolves its directories using the XDG Base Directory convention, with a single canonical
config path on both Linux and macOS:

| Path | Default | Override |
|------|---------|----------|
| Config directory | `~/.config/raxd` | `$XDG_CONFIG_HOME/raxd` |
| Config file | `~/.config/raxd/config.yaml` | follows config directory |
| State directory | `~/.local/state/raxd` | `$XDG_STATE_HOME/raxd` |
| Keys database (future) | `~/.local/state/raxd/keys.db` | follows state directory |
| TLS directory (future) | `~/.local/state/raxd/tls` | follows state directory |

Directories are created with `0700` permissions when `raxd` runs. `keys.db` and TLS files are
**not** created by the skeleton — only their paths are reserved. Full details are in
[`docs/configuration.md`](docs/configuration.md).

## Coming next

The following capabilities are **planned and not implemented yet**. They are listed so you know what
the skeleton is being built toward; do not treat them as available today.

- **API keys** — real `key create` / `key list` / `key delete` (key-management task).
- **TLS transport** — self-signed certificates and a TCP/TLS network server (tls-transport task).
- **Command execution** — running commands over the network with an allowlist, timeouts, and an
  audit log (command-exec task).
- **MCP server** — Model Context Protocol tools and transport for AI agents (mcp-server task).
- **Real `serve` and service registration** — running as a foreground daemon and registering a
  systemd/launchd service (service-install task).
- **Installation via `curl | sh`** — an `install.sh` script, goreleaser release matrix, SHA256
  verification, and macOS notarization (distribution task). *There is no installer yet — install
  by building from source in Docker as described above.*
- **`config port`** — actually writing the listening port to `config.yaml`.
- **Visual design** — lipgloss styling, adaptive banner width, and the `key list` table.

## Author

**Vladimir Kovalev, OEM TECH** — author of raxd. The author line is part of every CLI banner and
of this README.

## License

No license file is present in the repository yet; licensing terms are not defined at this stage.
