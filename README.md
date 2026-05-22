# raxd

**raxd** is a remote access daemon for AI agents — a single cross-platform Go binary that is meant
to act as a system service, a CLI utility, a network server (TCP + TLS), and an MCP server for AI
agents, all at once.

> **Project status: early.** The command tree, configuration/path resolution, build metadata, a
> product banner, a reproducible Docker dev/test environment, **API key management**
> (`key create` / `key list` / `key delete`), the **TLS network server** (`raxd serve`), the first
> **MCP server** (the `ping` and `server_info` tools), **command execution over MCP** (the
> `execute_command` tool), and **file upload over MCP** (the `upload_file` tool) — all on the `/mcp`
> route — are in place and working. The server provides a TLS 1.3 transport with per-connection
> API-key authentication, rate limiting, and an audit log; the MCP endpoint runs behind that same
> transport. The features that still run *behind* authentication — registering `raxd` as a system
> service, and `curl | sh` installation — are **not implemented yet**; see [Coming next](#coming-next).

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

At this stage the binary provides a stable command tree, the local foundation (API keys stored
securely on disk), and the networked core: `raxd serve` opens a TLS 1.3 listener, authenticates every
connection against those keys, and serves an MCP endpoint on `/mcp` with two read-only tools (`ping`,
`server_info`), the security-critical `execute_command` tool (runs a command on the host), and the
`upload_file` tool (writes a file on the host). Both `execute_command` and `upload_file` are
registered at the same extension point, behind the same authentication, rate limiting, and audit.

## What works today

| Area | Status |
|------|--------|
| `raxd version` — print build metadata | **Working** |
| `raxd status` — show daemon state and config/state paths | **Working** |
| `raxd key create` — issue an API key (shown once) | **Working** |
| `raxd key list` — list active API keys (no secrets) | **Working** |
| `raxd key delete` — revoke an API key | **Working** |
| Secure key storage in `keys.db` (salted SHA-256 hash, `0600` file) | **Working** |
| `raxd serve` — foreground TLS 1.3 server | **Working** |
| TLS 1.3 transport with a self-signed ECDSA P-256 certificate (auto-generated, reused) | **Working** |
| Per-connection API-key authentication over the network (`Authorization: Bearer`) | **Working** |
| Rate limiting (per key and per IP) and DNS-rebinding `Host`/`Origin` checks | **Working** |
| Structured audit log of every connection (fingerprint only, never the key) | **Working** |
| Authenticated health check (`GET /healthz` → `pong`) | **Working** |
| **MCP server** on `/mcp` (Streamable HTTP, protocol `2025-11-25`) with `ping` + `server_info` tools | **Working** |
| **MCP `execute_command`** — run a command on the host (no shell, timeout, optional allowlist, limits, audit) | **Working** |
| **MCP `upload_file`** — write a file on the host (`os.Root`-confined, size limit, controlled mode, no-overwrite default, atomic, audit) | **Working** |
| MCP audit (one line per tool call; for `execute_command`: command, args, exit code, duration; for `upload_file`: path, size) | **Working** |
| `raxd --help` and the full command tree | **Working** |
| Product banner with author (printed to stderr) | **Working** |
| XDG-based config/state path resolution (`~/.config/raxd`, `XDG_*` overrides) | **Working** |
| Directory creation with `0700` permissions | **Working** |
| `config.yaml` loading via viper (networking, `exec`, and `upload` fields read by `serve`) | **Working** |
| File **download** / host filesystem read / file deletion over MCP | **Not implemented** |
| MCP tools beyond `ping` / `server_info` / `execute_command` / `upload_file`; MCP Resources / Prompts | **Not implemented** |
| Interactive / PTY command sessions and real-time output streaming | **Not implemented** |
| Chunked / streaming / resumable upload of large files | **Not implemented** (`upload_file` ships one whole file per request) |
| Command sandboxing (cgroups/rlimits/seccomp/namespaces) | **Not implemented** (isolation via non-root + container) |
| mTLS / client certificates | **Not implemented** (API-key auth only) |
| `raxd config port` | **Stub** (`not implemented yet`) |
| `curl \| sh` installer | **Not implemented** |
| Running as a registered system service (systemd/launchd) | **Not implemented** (`serve` is foreground only) |

Behind authentication, the working routes today are the health check (`GET /healthz`) and the MCP
server (`/mcp`, with `ping`, `server_info`, `execute_command`, and `upload_file`). Every other route
returns `501 Not Implemented`. Everything in the [Coming next](#coming-next) section is **not
implemented yet**.

> **`execute_command` and `upload_file` are dangerous.** `execute_command` runs an arbitrary binary
> on the host on behalf of an authenticated client — remote code execution of the SSH class.
> `upload_file` writes a file into the host's filesystem. Read the
> [`execute_command` security guide](docs/execute-command-security.md) and the
> [`upload_file` security guide](docs/file-upload-security.md) before enabling either against a real
> host. The allowlist is **off by default** (any command is allowed), command arguments and the upload
> destination path are **logged verbatim** (do not pass secrets in `args` or in `path`), and you
> should run `raxd` as a **non-root** user inside a container.

## Requirements

- [Go 1.25](https://go.dev/dl/) (module declares `go 1.25`).
- [Docker](https://www.docker.com/) — **all builds, tests, and any execution of `raxd` happen
  inside a container, never on the host.** `raxd` is designed to execute commands over the network
  and `raxd serve` opens a TLS listener, so its place is an isolated container (see the security
  baseline §6 and `docs/development.md`).

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

`raxd` exposes the following command tree. The service commands, the `key` group, and `serve` are
working; `config port` is an honest stub.

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
└── serve              start the raxd TLS server           (working)
```

A full reference with usage strings, exit codes, and output examples is in
[`docs/commands.md`](docs/commands.md). The MCP server is not a CLI command — it is hosted by
`raxd serve` on the `/mcp` route; see [`docs/mcp.md`](docs/mcp.md). Command execution and file upload
are **not** CLI sub-commands either — they are the MCP `execute_command` and `upload_file` tools.

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

### Example: `raxd serve`

Start the TLS server. It runs in the foreground and writes everything to **stderr** (its stdout is
empty). On the first run it generates the self-signed certificate; on later runs it reuses it:

```
$ raxd serve
  cert      generated  /home/user/.local/state/raxd/tls/cert.pem
  key       generated  /home/user/.local/state/raxd/tls/key.pem  (0600)
  tls       TLS 1.3 only
  listening https://127.0.0.1:7822
  press Ctrl+C to stop

```

After this block the server blocks and writes one structured audit line per connection. A successful
authenticated request looks like this (only the key fingerprint is logged, never the key):

```
time=2026-05-21T14:32:01Z level=INFO msg=AUTH fp=a3f9c1d2e847 remote=127.0.0.1:54312
```

Call the health check with a created key (the certificate is self-signed, so a local client must
trust it or skip verification — `curl -k` here is a controlled local test):

```sh
curl -k -H "Authorization: Bearer $KEY" https://127.0.0.1:7822/healthz
# → pong
```

Press Ctrl+C for a graceful shutdown (exit code `0`). For the full startup/audit/shutdown reference,
response codes, and configuration fields, see [`docs/commands.md`](docs/commands.md#raxd-serve) and
[`docs/configuration.md`](docs/configuration.md#networking-and-serve-fields).

> **Scope:** `serve` today provides the secure transport, authentication, the health check, and the
> MCP server (`ping` / `server_info` / `execute_command` / `upload_file`). On startup it also creates
> the upload root (default `~/.local/state/raxd/uploads`, `0700`) for `upload_file`. Every route
> other than `/healthz` and `/mcp` returns `501`. `serve` is foreground only and does **not** register
> itself as a system service.

### Example: connecting to the MCP server

`raxd serve` also serves an MCP endpoint on `/mcp`, behind the same TLS, authentication, and rate
limiting. Connect an MCP client to `https://127.0.0.1:<port>/mcp` (default port `7822`) with the same
`Authorization: Bearer rax_live_…` key. A quick `ping` over `curl`:

```sh
curl -k https://127.0.0.1:7822/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ping","arguments":{}}}'
# → {"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"pong"}]}}
```

> On a successful tool result the `isError` field is **omitted** (the SDK serializes it with
> `omitempty` and the server does not set it on success), so it does **not** appear in the response
> above. It shows up, set to `true`, only when a tool reports its own error. See
> [`docs/mcp.md`](docs/mcp.md#behaviour-and-error-handling).

The server speaks Streamable HTTP, MCP protocol `2025-11-25`, and exposes four tools: `ping` (returns
`pong`), `server_info` (returns `{name, version, protocolVersion}`, no secrets), `execute_command`
(runs a command on the host), and `upload_file` (writes a file on the host). Use the official Go MCP
SDK on the server side. For connection parameters, the full smoke-test, MCP client config, the
self-signed-TLS caveat, the audit format, and the `execute_command` / `upload_file` contracts, see
[`docs/mcp.md`](docs/mcp.md).

### Example: running a command over MCP

`execute_command` runs a non-interactive command on the host as a binary plus an argument list,
**without a shell**, and returns the captured output, exit code, duration, and timeout/truncation
flags. Call it as a JSON-RPC `tools/call` on `/mcp`:

```sh
curl -k https://127.0.0.1:7822/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "MCP-Protocol-Version: 2025-11-25" \
  -d '{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"execute_command","arguments":{"command":"ls","args":["-la"],"timeout_ms":5000}}}'
```

A non-zero exit code and a timeout are **normal results**, not errors (the result has `isError`
omitted, exactly as on a successful `ping`); a rejected or unstartable command (allowlist deny,
missing binary, limits, `deny_root`) comes back with `isError: true`. For the full response shapes,
see [`docs/mcp.md`](docs/mcp.md#execute_command).

> **Before you enable this against a real host:** read the
> [`execute_command` security guide](docs/execute-command-security.md). Key points: the allowlist is
> **off by default** (any command runs); command arguments are **logged verbatim** (do not pass
> secrets in `args`); the allowlist match is **exact** (`ls` ≠ `/bin/ls`); and you should run `raxd`
> as a **non-root** user in a container. The `exec.*` settings are documented in
> [`docs/configuration.md`](docs/configuration.md#command-execution-exec-fields).

### Example: uploading a file over MCP

`upload_file` writes one regular file into the upload root, given a relative `path` and base64
`content`. The write is confined to the upload root (no `..`-escape, no absolute path, no out-of-root
symlink), size-limited, with a controlled file mode (default `0600`), atomic, and not overwriting an
existing file unless `overwrite: true`. Call it as a JSON-RPC `tools/call` on `/mcp`:

```sh
curl -k https://127.0.0.1:7822/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "MCP-Protocol-Version: 2025-11-25" \
  -d '{"jsonrpc":"2.0","id":20,"method":"tools/call","params":{"name":"upload_file","arguments":{"path":"notes/hello.txt","content":"aGVsbG8K"}}}'
```

A successful write returns `path`, `size`, `overwritten`, and `mode` (no absolute path, no content);
a rejected write (traversal, an existing file without `overwrite`, too-large, bad base64, a forbidden
mode) comes back with `isError: true`. For the full response shapes and error mapping, see
[`docs/mcp.md`](docs/mcp.md#upload_file).

> **Before you enable this against a real host:** read the
> [`upload_file` security guide](docs/file-upload-security.md). Key points: keep the upload root a
> dedicated directory **free of bind-mounts** (`os.Root` does not block mount points); the destination
> **path is logged** (do not put secrets in `path` — the content is never logged); setuid/setgid/sticky
> and world-writable file modes are **forbidden**; and you should run `raxd` as a **non-root** user.
> The `upload.*` settings are documented in
> [`docs/configuration.md`](docs/configuration.md#file-upload-upload-fields).

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
`status` reports on-disk paths only and does not probe a running `serve` process, so `state` is
shown as `not running` even while a server is listening:

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
| TLS directory | `~/.local/state/raxd/tls` | follows state directory |
| Upload root (default) | `~/.local/state/raxd/uploads` | follows state directory (or `upload.root`) |

Directories are created with `0700` permissions when `raxd` runs. The `keys.db` file is created with
`0600` permissions the first time you run `key create`. The TLS certificate (`cert.pem`, `0644`) and
private key (`key.pem`, `0600`) are created in the TLS directory the first time you run `raxd serve`,
and reused afterward. The upload root is created with `0700` on `raxd serve`. Full details, including
the networking fields and the `exec` / `upload` fields that `serve` reads from `config.yaml`, are in
[`docs/configuration.md`](docs/configuration.md).

## Coming next

The following capabilities are **planned and not implemented yet**. They are listed so you know what
the binary is being built toward; do not treat them as available today.

- **File download / read / delete** — `upload_file` is upload-only; reading or deleting host files
  over MCP is a separate task.
- **More MCP tools and resources** — the MCP server today exposes `ping`, `server_info`,
  `execute_command`, and `upload_file`; further tools and MCP Resources / Prompts may follow.
  `initialize` currently advertises the `tools` capability only.
- **Chunked / streaming upload** — `upload_file` ships one whole file per request, bounded by
  `max_body_bytes`; uploading larger files would need a chunked/streaming channel.
- **Command sandboxing** — cgroups/rlimits/seccomp/namespaces for `execute_command`. Today isolation
  relies on running `raxd` as a non-root user inside a container; the tool already kills the whole
  process tree on timeout, caps output, and limits argument count/length.
- **System-service registration** — running `raxd` as a systemd/launchd service (`serve` is
  foreground only today and does not install or manage a service).
- **mTLS / client certificates** — currently out of scope; authentication is by API key only.
- **Installation via `curl | sh`** — an `install.sh` script, goreleaser release matrix, SHA256
  verification, and macOS notarization (distribution task). *There is no installer yet — install
  by building from source in Docker as described above.*
- **`config port`** — actually writing the listening port to `config.yaml` (edit the file by hand
  for now).
- **Visual design** — lipgloss styling, adaptive banner width, and colored output.

## Documentation

- [`docs/commands.md`](docs/commands.md) — full command reference (`version`, `status`, the `key`
  group, `serve`, and the `config port` stub).
- [`docs/mcp.md`](docs/mcp.md) — MCP integration guide: the `/mcp` endpoint, connection parameters,
  the `ping` / `server_info` / `execute_command` / `upload_file` tools, authentication, the curl
  smoke-test, MCP client config, and audit.
- [`docs/execute-command-security.md`](docs/execute-command-security.md) — mandatory security warnings
  for `execute_command` (secrets in arguments, allowlist semantics, `deny_root`/root, isolation,
  residual risks).
- [`docs/file-upload-security.md`](docs/file-upload-security.md) — mandatory security warnings for
  `upload_file` (mount points in the upload root, secrets in the path, `deny_root`/root, the mode
  policy, no disk quota, residual risks).
- [`docs/configuration.md`](docs/configuration.md) — paths, `keys.db`, the TLS directory, and the
  `config.yaml` networking, `exec`, and `upload` fields.
- [`docs/troubleshooting.md`](docs/troubleshooting.md) — common problems with `serve`, the TLS
  certificate, keys, the config file, `execute_command`, and `upload_file`.
- [`docs/development.md`](docs/development.md) — building and testing in Docker, project layout, and
  build metadata.

## Author

**Vladimir Kovalev, OEM TECH** — author of raxd. The author line is part of every CLI banner and
of this README.

## License

No license file is present in the repository yet; licensing terms are not defined at this stage.
</content>
