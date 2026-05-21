# raxd

**raxd** is a remote access daemon for AI agents — a single cross-platform Go binary that is meant
to act as a system service, a CLI utility, a network server (TCP + TLS), and an MCP server for AI
agents, all at once.

> **Project status: early.** The command tree, configuration/path resolution, build metadata, a
> product banner, a reproducible Docker dev/test environment, and **API key management**
> (`key create` / `key list` / `key delete`) are in place and working. The networked parts of the
> product — TLS transport, command execution, the MCP server, the `serve` daemon, and `curl | sh`
> installation — are **not implemented yet**; see [Coming next](#coming-next).

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

At this stage the binary already provides a stable command tree, three working service commands
(`version`, `status`, and the `key` group), and the local foundation that the networked features
will build on: API keys are issued and stored securely so that later tasks (TLS transport, the MCP
server, command execution) can authenticate against them.

## What works today

| Area | Status |
|------|--------|
| `raxd version` — print build metadata | **Working** |
| `raxd status` — show daemon state and config/state paths | **Working** |
| `raxd key create` — issue an API key (shown once) | **Working** |
| `raxd key list` — list active API keys (no secrets) | **Working** |
| `raxd key delete` — revoke an API key | **Working** |
| Secure key storage in `keys.db` (salted SHA-256 hash, `0600` file) | **Working** |
| `raxd --help` and the full command tree | **Working** |
| Product banner with author (printed to stderr) | **Working** |
| XDG-based config/state path resolution (`~/.config/raxd`, `XDG_*` overrides) | **Working** |
| Directory creation with `0700` permissions | **Working** |
| `config.yaml` loading via viper (defaults, missing-file tolerance) | **Working (library; not yet wired into commands)** |
| `raxd config port` | **Stub** (`not implemented yet`) |
| `raxd serve` | **Stub** (honest — prints and exits, does **not** open a port) |

Everything in the [Coming next](#coming-next) section is **not implemented yet**. In particular,
there is no network layer: the keys you create are stored and revocable, but nothing yet presents
them over a connection.

## Requirements

- [Go 1.25](https://go.dev/dl/) (module declares `go 1.25`).
- [Docker](https://www.docker.com/) — **all builds, tests, and any execution of `raxd` happen
  inside a container, never on the host.** `raxd` is designed to execute commands over the network,
  so its place is an isolated container (see the security baseline §6 and `docs/development.md`).

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

> There is **no installer yet**. Installation via `curl | sh` is planned (see
> [Coming next](#coming-next)) but does not exist — build from source in Docker as shown above.

## Commands

`raxd` exposes the following command tree. The service commands and the `key` group are working;
`config port` and `serve` are honest stubs.

```
raxd
├── version            print version information           (working)
├── status             show daemon status and paths        (working)
├── key                manage API keys
│   ├── create         create a new API key                (working)
│   ├── list           list all API keys                   (working)
│   └── delete         revoke an API key                   (working)
├── config             manage configuration
│   └── port           set the listening port              (stub)
└── serve              start the raxd daemon                (stub)
```

A full reference with usage strings, exit codes, and output examples is in
[`docs/commands.md`](docs/commands.md).

### Example: API keys

Issue a key (the full key is printed **once** to stdout, inside a box; the warning and metadata go
to stderr):

```
$ raxd key create --name production-key
  ! WARNING: This key will NOT be shown again. Save it now.

┌──────────────────────────────────────────────────────────────────┐
│  rax_live_dGhpcyBpcyBhIHRlc3Qga2V5IGZvciBkb2N1bWVudGF0aW9u   │
└──────────────────────────────────────────────────────────────────┘

  id        d7bc3a34da19d94e
  label     production-key
  created   2026-05-21
```

List active keys (the key body is **never** shown here — only metadata):

```
$ raxd key list
┌──────────────────┬────────────────┬────────────┬───────────┐
│ ID               │ LABEL          │ CREATED    │ LAST USED │
├──────────────────┼────────────────┼────────────┼───────────┤
│ d7bc3a34da19d94e │ production-key │ 2026-05-21 │ never     │
│ e4b550b565a232b6 │ staging        │ 2026-05-21 │ never     │
└──────────────────┴────────────────┴────────────┴───────────┘
```

The `ID` column shows the full id, which you pass directly to `key delete` (it matches the id from
`key create`). Revoke a key by its id (soft revoke — the record is kept for audit, but the key stops
working):

```
$ raxd key delete d7bc3a34da19d94e
  key d7bc3a34da19d94e revoked
  hint: the key can no longer be used for authentication
```

The full key is shown only at creation and cannot be retrieved again. `keys.db` stores only a
salted SHA-256 hash and the salt, never the plaintext key. See
[`docs/commands.md`](docs/commands.md#api-keys-raxd-key) for the complete reference and
[`docs/configuration.md`](docs/configuration.md#the-keysdb-key-database) for the storage details.

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
The state is always `not running` on the current build:

```
  state    not running
  config   /home/user/.config/raxd/config.yaml
  keys     /home/user/.local/state/raxd/keys.db
  tls      /home/user/.local/state/raxd/tls
```

For security, `status` never prints key material, TLS contents, or any secrets — only the state
string and the resolved paths.

### The banner

Before every command, `raxd` prints a product banner to **stderr** (so it never pollutes the
machine-readable stdout — `raxd status | grep state` and `raxd key create > key.txt` work cleanly).
The banner is a plain-text Unicode box and always contains the author line:

```
┌──────────────────────────────────────────┐
│  raxd  —  Remote Access Daemon            │
│  dev  ·  commit none  ·  built unknown    │
│  Vladimir Kovalev, OEM TECH               │
└──────────────────────────────────────────┘
```

The banner is **not** printed for `--help` (cobra prints help itself). The binary always renders
this single fixed (wide) layout — adaptive sizing and color/styling are planned (see
[Coming next](#coming-next)).

## Configuration paths

`raxd` resolves its directories using the XDG Base Directory convention, with a single canonical
config path on both Linux and macOS:

| Path | Default | Override |
|------|---------|----------|
| Config directory | `~/.config/raxd` | `$XDG_CONFIG_HOME/raxd` |
| Config file | `~/.config/raxd/config.yaml` | follows config directory |
| State directory | `~/.local/state/raxd` | `$XDG_STATE_HOME/raxd` |
| Keys database | `~/.local/state/raxd/keys.db` | follows state directory |
| TLS directory (future) | `~/.local/state/raxd/tls` | follows state directory |

Directories are created with `0700` permissions when `raxd` runs. The `keys.db` file is created with
`0600` permissions the first time you run `key create`. TLS files are **not** created yet — only
their path is reserved. Full details are in [`docs/configuration.md`](docs/configuration.md).

## Coming next

The following capabilities are **planned and not implemented yet**. They are listed so you know what
the binary is being built toward; do not treat them as available today.

- **TLS transport** — self-signed certificates and a TCP/TLS network server (tls-transport task).
  This is also what would present an API key over a connection; today keys exist locally only.
- **Command execution** — running commands over the network with an allowlist, timeouts, and an
  audit log (command-exec task).
- **MCP server** — Model Context Protocol tools and transport for AI agents (mcp-server task).
- **Real `serve` and service registration** — running as a foreground daemon and registering a
  systemd/launchd service (service-install task).
- **Installation via `curl | sh`** — an `install.sh` script, goreleaser release matrix, SHA256
  verification, and macOS notarization (distribution task). *There is no installer yet — install
  by building from source in Docker as described above.*
- **`config port`** — actually writing the listening port to `config.yaml`.
- **Visual design** — lipgloss styling, adaptive banner width, and colored `key list` output.

## Author

**Vladimir Kovalev, OEM TECH** — author of raxd. The author line is part of every CLI banner and
of this README.

## License

No license file is present in the repository yet; licensing terms are not defined at this stage.
