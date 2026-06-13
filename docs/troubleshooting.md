# Troubleshooting

Common problems and how to resolve them. Every behaviour here is taken from the current code; nothing
is hypothetical. Run `raxd` inside Docker (security baseline §6) — see
[`development.md`](development.md).

All error messages follow the project format: a lowercase `error:` line describing what happened,
followed by one or more indented `hint:` lines describing what to do.

## Installation (`install.sh`)

Problems running the `curl | sh` installer. The full installation guide is in
[`installation.md`](installation.md); this section covers the failure modes and their exit codes. The
installer prints lowercase `error:` / `hint:` lines and exits with a specific code per failure class.

> **Note: artifacts are on GitHub Releases and the default installer resolves them automatically.**
> The release archives and `SHA256SUMS` are published on GitHub Releases for `vladimirvkhs/raxd`, and
> the canonical one-liner works as-is:
> `curl -fsSL https://github.com/vladimirvkhs/raxd/releases/latest/download/install.sh | bash`.
> You only need `RAXD_BASE_URL` (an alternative artifact source you control) or `RAXD_VERSION` (to
> pin an exact tag instead of `latest`) for the non-default cases — see
> [`installation.md`](installation.md#quick-install-curl--sh) and the version policy
> (`latest` vs a pinned tag) in [`production-readiness.md`](production-readiness.md#1-public-release-host--closed-github-releases-ор-3--ор-5).

### `error: unsupported platform` / `unsupported architecture` (exit `2`)

The installer detected an OS or architecture it does not build for. It supports `linux` / `darwin` and
`amd64` / `arm64` only; Windows, other OSes, and 32-bit architectures (for example `i686`) are rejected,
and no binary is installed.

```
error: unsupported platform: <uname -s>
  hint: only linux and darwin (macOS) are supported
```

```
error: unsupported architecture: <uname -m>
  hint: only amd64 (x86_64) and arm64 (aarch64) are supported
```

There is nothing to fix on a genuinely unsupported platform — `raxd` is macOS/Linux, amd64/arm64 only.
If you are on a supported platform but `uname` reports an unexpected value, install
[manually](installation.md#manual-installation) and pick the archive for your real platform.

### `error: SHA256 mismatch` (exit `3`)

The downloaded archive failed its integrity check against `SHA256SUMS`. The check runs **before** the
binary is placed, so on this error **no binary is installed** and the temporary files are removed.

```
error: SHA256 mismatch — archive is corrupted or tampered
  hint: try reinstalling; if the error persists, report it to the maintainers
```

Two distinct causes share this exit code:

- **The archive is corrupted or was substituted** (the hash does not match). Re-run the install; if it
  keeps failing, the source is unreliable — do not force it past the check. Recall the
  [trust model](installation.md#trust-model-v1): the hash protects against a corrupted or substituted
  *single* file, not a coordinated tampered source.
- **No matching `SHA256SUMS` entry** for the requested archive (a different message, same exit `3`):

  ```
  error: no entry for '<archive>' in SHA256SUMS
    hint: make sure RAXD_VERSION='<version>' matches a published release
  ```

  The `RAXD_VERSION` you asked for is not present in that `SHA256SUMS`. Confirm `RAXD_VERSION` matches a
  published release on that source.

Never bypass this check. If you are verifying a download by hand, see
[Integrity verification](installation.md#integrity-verification-sha256).

### `error: no write permission …` / no `sudo` (exit `4`)

The chosen install directory is not writable and `sudo` is unavailable, so the binary could not be
placed.

```
error: no write permission to /usr/local/bin and sudo is unavailable
  hint: run as root or specify a different directory with --prefix ~/.local/bin
```

If `sudo` is present but the privileged install still fails, you get the related message:

```
error: failed to install binary with sudo to /usr/local/bin
  hint: use --prefix to choose a directory that does not require root
```

Fix it one of these ways:

- Install into a user directory you can write to: add `--prefix ~/.local/bin` (or set
  `RAXD_PREFIX=~/.local/bin`).
- Run as root, or on a host with `sudo` the installer will use it for the system directory and tell you
  so explicitly.

After a successful install into `~/.local/bin`, if that directory is not on `PATH` the installer prints
a `PATH` hint rather than failing silently:

```
hint: raxd is installed but /home/user/.local/bin is not in $PATH
hint: add to ~/.bashrc or ~/.zshrc:
  export PATH="/home/user/.local/bin:$PATH"
```

Add that line to your shell profile and reopen the shell (or `source` it) so `raxd` resolves on `PATH`.

### `error: download failed …` (exit `5`)

The installer could not download the archive or `SHA256SUMS`.

```
error: download failed: <url>
  hint: check your network connection and verify RAXD_BASE_URL/RAXD_VERSION
```

```
error: download failed: <sums_url>
  hint: check availability of <base-url>/SHA256SUMS
```

Most common causes:

- **Network / host unreachable** — check connectivity and that the release artifacts are actually
  reachable over HTTPS. The default source is GitHub Releases (`vladimirvkhs/raxd`); a transient GitHub
  outage or a blocked network can cause this.
- **Wrong `RAXD_VERSION`** — the archive name built from the version does not exist on the source (no
  release published under that tag).
- **A custom `RAXD_BASE_URL` that does not serve the artifacts** — if you overrode `RAXD_BASE_URL` to
  your own host, make sure `<base-url>/SHA256SUMS` and the archive are actually served there (see
  [`installation.md`](installation.md#pointing-the-installer-at-an-artifact-source)).

### `error: no SHA256 utility found` (exit `1`)

Neither `sha256sum` nor `shasum` is on the host, so integrity cannot be verified.

```
error: no SHA256 utility found (sha256sum or shasum)
  hint: install coreutils (linux) or use macOS 10.13+
```

Install GNU coreutils (Linux — provides `sha256sum`) or use a macOS version that ships `shasum`, then
re-run.

### macOS: "raxd is damaged" / Gatekeeper blocks it

Without Apple notarization (out of scope in v1), Gatekeeper may block the binary on first run. The
installer already strips the quarantine attribute on macOS and prints a hint:

```
hint: if macOS Gatekeeper blocks raxd, run:
  xattr -d com.apple.quarantine /usr/local/bin/raxd
  or: System Settings → Privacy & Security → allow '/usr/local/bin/raxd'
hint: for a notarized build with full Gatekeeper approval, request notarization via Apple Developer Program
```

If you obtained the binary another way (browser/AirDrop, or a manual install), clear it yourself:

```sh
xattr -d com.apple.quarantine /usr/local/bin/raxd
```

Or approve it under **System Settings → Privacy & Security**. See
[macOS Gatekeeper / quarantine](installation.md#macos-gatekeeper--quarantine) for the full context (and
why the real Gatekeeper flow is verified on a live macOS host, not in Docker).

## `raxd serve`

### The server starts but every connection is rejected with 401

You have no active API keys, or you are not sending the key correctly.

- On startup with an empty `keys.db`, `serve` prints a warning:

  ```
  warning   no API keys found — all connections will be rejected
  hint      create a key with "raxd key create --name <label>"
  ```

  Create a key with `raxd key create --name <label>` and retry.

- If you do have keys, check the request header. The key must be sent as
  `Authorization: Bearer rax_live_…` (the full key body printed once by `key create`). A missing
  header, a non-`Bearer` scheme, or an empty token all produce `401` and a `FAIL` audit line:

  ```
  time=… level=WARN msg=FAIL fp=- remote=… reason="no authorization header"
  ```

- An unknown or **revoked** key also produces `401`, with a fingerprint in the audit line:

  ```
  time=… level=WARN msg=FAIL fp=b7d2a0c19f3e remote=… reason="authentication failed"
  ```

  Revocation takes effect immediately — a key you delete with `raxd key delete` stops working on its
  next request. Confirm the key is still active with `raxd key list`.

This applies to the MCP endpoint too: a `401` on `/mcp` means the same thing, and no tool runs
(including `execute_command` — no command is executed — and `upload_file` — no file is written). See
[MCP server (`/mcp`)](#mcp-server-mcp) below.

Note: response bodies for rejected requests are intentionally empty; the reason is only in the
server's audit stream. That is by design (it avoids telling a caller whether a key is unknown or
revoked).

### `error: cannot bind to 127.0.0.1:7822: address already in use`

The configured port is taken by another process (often a previous `raxd serve` you forgot to stop).

```
error: cannot bind to 127.0.0.1:7822: address already in use
  hint: check what is using port 7822 with "lsof -i :7822" and stop it, or change the port with "raxd config port <PORT>"
```

- Find and stop the other process (`lsof -i :7822`), or
- Change the port. `raxd config port` is still a stub, so edit `config.yaml` directly: set the
  `port:` key (see [`configuration.md`](configuration.md#networking-and-serve-fields)) and start
  `serve` again. The MCP endpoint follows the same port.

> If `raxd` is running as a registered service, a stale instance is most often the cause — stop it
> with `raxd service stop` (or check `raxd service status`) rather than killing it by hand. See
> [the service section](#raxd-service) below.

> **What you will (and will not) see.** When the port is in use, the bind fails before the server
> ever starts, so `serve` prints **only** the `error:` / `hint:` lines above and exits `1`. The
> startup block (`cert` / `key` / `tls` / `listening …` / `press Ctrl+C`) and the shutdown block do
> **not** appear — there is no misleading `listening …` line for a server that did not start. The
> startup block is emitted only after the listener is successfully bound (via the `OnListen` hook in
> `internal/server`), which matches how the certificate and permission errors below behave.

### `error: TLS certificate or key is corrupted or unreadable`

The files in the TLS directory exist but cannot be loaded as a valid pair — for example one of
`cert.pem` / `key.pem` is missing, truncated, or does not match the other.

```
error: TLS certificate or key is corrupted or unreadable
  hint: remove the files in /home/user/.local/state/raxd/tls/ and run "raxd serve" again to regenerate
```

`serve` never overwrites an existing certificate automatically. To recover, delete `cert.pem` **and**
`key.pem` from the TLS directory (default `~/.local/state/raxd/tls/`, shown by `raxd status` as the
`tls` line) and start `serve` again — it will generate a fresh self-signed pair.

> Implementation detail worth knowing: a completely **empty** (zero-byte) `cert.pem` or `key.pem` is
> treated as "not present", so if *both* files are zero-byte the server regenerates them rather than
> reporting corruption. The corruption error appears when the files have content that cannot be
> parsed, or when only one of the two exists. Either way, removing both files and restarting is the
> reliable fix.

### `error: cannot create TLS directory: permission denied`

`raxd` could not create the state/TLS directory.

```
error: cannot create TLS directory: permission denied
  hint: check that the current user has write access to ~/.local/state/raxd/
```

Make sure the current user can write under `~/.local/state/raxd/` (or wherever `XDG_STATE_HOME`
points). In a container, ensure the mounted path is writable by the container user.

### `error: cannot create upload root directory: …`

`raxd serve` could not create the upload root (default `<state directory>/uploads`). It is created
with `0700` permissions on startup, before the listener binds, so this error is a startup failure
(exit `1`) and the server does not start.

```
error: cannot create upload root directory: permission denied
```

Make sure the current user can write under the state directory (or wherever `upload.root` points if
you set it), and that any custom `upload.root` is a writable path. See
[`configuration.md`](configuration.md#file-upload-upload-fields).

### `error: failed to generate TLS certificate`

Certificate generation failed while writing to disk — typically no free space or no write permission
in the TLS directory.

```
error: failed to generate TLS certificate
  hint: check available disk space and write permissions for /home/user/.local/state/raxd/tls/
```

Free up space or fix permissions on the TLS directory and retry.

### `error: key store is corrupted or unreadable` (at startup)

`keys.db` exists but cannot be parsed. `serve` refuses to start and does not modify the file.

```
error: key store is corrupted or unreadable
  hint: check file permissions on the keys.db path shown in "raxd status"
  hint: do not attempt to repair the file manually — contact support if data recovery is needed
```

Check that the file is readable by the current user (`raxd status` shows the path). Do not hand-edit
`keys.db`. A *missing* `keys.db` is **not** an error — it is treated as an empty store (the server
starts and warns).

### Configuration load errors share one generic hint

Both config-load failures below — an **invalid bind address** and an **invalid `config.yaml`** — come
out of the same code path in `serve` (the `config.Load` step). That single path prints **one generic
hint** that always references `bind_addr` / `config.yaml`, regardless of which of the two actually
failed. The `error:` line always tells you the real cause; the `hint:` line is **not** specialised
per cause. So:

- **Trust the `error:` line** — it carries the underlying message from `config.Load` and names the
  actual problem.
- **Read the `hint:` line as "fix your `config.yaml`"** rather than literally "the problem is
  `bind_addr`". For a YAML-syntax error the `bind_addr` mention is incidental.

The two concrete cases follow. (Note: an invalid `upload.max_file_bytes`, a negative
`upload.max_total_bytes`, or an invalid `upload.default_mode` is also a `config.Load` failure and
surfaces through this same path — the `error:` line names the upload field; see
[the `upload_file` section](#the-upload_file-tool) below.)

### `error: invalid bind address "…": not a valid IP address`

`bind_addr` in `config.yaml` is not a valid IP. Here the cause and the generic hint line up:

```
error: invalid bind address "0.0.0.256": not a valid IP address
  hint: set a valid address in config.yaml (field: bind_addr), for example "127.0.0.1" or "0.0.0.0"
```

Set `bind_addr` to a valid IP literal. The default `127.0.0.1` binds to loopback only; `0.0.0.0`
exposes the server beyond the host (your responsibility to secure).

### `error: config file is not valid YAML`

The `config.yaml` file exists but is not valid YAML. Fix the syntax. A *missing* file is not an
error — the defaults are applied (and `raxd status` shows `(not found, defaults applied)`).

> **Heads-up: the hint here mentions `bind_addr`, but the real problem is YAML syntax.** Because the
> invalid-YAML case and the invalid-bind-address case are handled by the **same** `config.Load`
> error path in `serve`, a malformed `config.yaml` produces the **same generic hint** as a bad bind
> address:
>
> ```
> error: config file is not valid YAML: <parser detail>
>   hint: set a valid address in config.yaml (field: bind_addr), for example "127.0.0.1" or "0.0.0.0"
> ```
>
> Ignore the `bind_addr` part in this situation. The actionable line is the `error:` one
> (`config file is not valid YAML`): correct the YAML syntax in `config.yaml` (indentation, quoting,
> a stray tab, an unterminated string, etc.) and start `serve` again. This is the same `error:` you
> would see from any tool that fails to parse the file.

### `curl` to `/healthz` or `/mcp` fails with a TLS / certificate error

The server uses a **self-signed** certificate, so a client that verifies certificates will reject it
by default. There is no built-in trust anchor and no mTLS in this build.

- For a controlled local test, skip verification: `curl -k -H "Authorization: Bearer $KEY"
  https://127.0.0.1:7822/healthz`.
- Otherwise, add the generated `cert.pem` to your client's trust store. The certificate's SAN covers
  `127.0.0.1` and `localhost`, so connect using one of those names/addresses.
- For Node-based MCP clients (MCP Inspector and similar) in development, set
  `NODE_TLS_REJECT_UNAUTHORIZED=0` in the client's environment (insecure — dev only). See
  [`mcp.md`](mcp.md#self-signed-tls).

### A request returns `501 Not Implemented`

This is expected for an **unimplemented** route. After authentication, the routes that do real work
are `GET /healthz` (returns `pong`) and `/mcp` (the MCP server). Every other path (for example
`/exec`) returns `501` with the body `not implemented`. Note that command execution and file upload
are **not** separate routes: they are the MCP `execute_command` and `upload_file` tools on `/mcp`, not
`/exec` or `/upload` endpoints. There is nothing to fix about the `501` on other paths — those routes
are simply unimplemented.

> **`/mcp` no longer returns `501`.** If you used an older build, the MCP route was a `501` stub. In
> the current build, `POST /mcp` with a valid key returns a real JSON-RPC response. If you still get
> `501` on `/mcp`, you are running an old binary — rebuild. (A `GET /mcp` returns `405`, not `501` —
> see the MCP section below.)

### A request returns `413` or `400` for an oversized body (and nothing shows up in the audit stream)

The request body exceeded `max_body_bytes` (default 1 MiB). The rejection is produced by the
outermost body-size limit (`http.MaxBytesReader`), which sits **before** the auth and audit
middlewares — so, unlike the `401` / `403` / `429` cases, it **does not** write any audit line
(`FAIL` / `DENY` / `RATE`). If you are debugging an oversized request, do not look for it in the audit
stream: confirm it from the client side (the response code) instead. Reduce the request body or raise
`max_body_bytes` in `config.yaml` (see
[`configuration.md`](configuration.md#networking-and-serve-fields)).

> **`413` vs `400` — which one you see depends on the route.**
> - For a plain non-MCP route, the body limit surfaces as **`413`**.
> - For an **`upload_file`** request on `/mcp`, an oversized body surfaces (in this build) as an HTTP
>   **`400`** with "failed to read body" — this comes from the Go MCP SDK reading the truncated
>   `MaxBytesReader`, **not** a `413`. (The project spec/mcp-spec mention `413`; the SDK returns `400`
>   here. This is a documentation caveat, **not** a defect — the body is rejected before the handler
>   and **no file is written**.) See [`mcp.md`](mcp.md#upload_file) for the full caveat.
>
> Either way, the rejection is **silent in the audit stream** — confirm an oversized request by the
> response code, not by grepping the audit log.

> Note: large `execute_command` arguments are bounded twice — first by `max_body_bytes` (the whole
> JSON-RPC body), then by `exec.max_args` / `exec.max_arg_len`. An oversized body means the **whole
> request** was too big; an `isError: true` with a "too many arguments" / "argument too long" message
> means the request was fine but the per-argument limits were exceeded (see
> [the `execute_command` section](#the-execute_command-tool) below). The analogous distinction for
> `upload_file` (body limit vs `upload.max_file_bytes`) is in
> [the `upload_file` section](#the-upload_file-tool).

### A request returns `429 Too Many Requests`

You exceeded the rate limit. Limiting is a token bucket applied **per key and per client IP** with a
sustained `rate_limit` (default 10 req/s) and a `rate_burst` (default 20). The audit line shows which
limit was hit:

```
time=… level=WARN msg=RATE fp=a3f9c1d2e847 remote=… reason="rate limit exceeded (key)"
time=… level=WARN msg=RATE fp=- remote=… reason="rate limit exceeded (ip)"
```

Slow down, or raise `rate_limit` / `rate_burst` in `config.yaml` (see
[`configuration.md`](configuration.md#networking-and-serve-fields)). This rate limit also applies to
MCP calls on `/mcp`, including `execute_command` and `upload_file` — when the limit is hit the call is
rejected with `429` **before** any command runs or any file is written.

### A request returns `403 Forbidden`

Three different conditions map to `403`; the audit line's `reason` tells them apart:

- `reason="invalid host header"` — the request's `Host` is not in `host_allow`. By default only
  `localhost`, `127.0.0.1`, and `::1` are allowed.
- `reason="invalid origin header"` — an `Origin` header was present and its hostname is not in
  `origin_allow`. (A request with no `Origin` is allowed — that is the normal case for non-browser
  clients.) Matching is strict on the hostname, so `https://localhost.evil.com` does not pass
  `localhost`.
- `reason="key store unavailable"` — the key store became unreadable while the server was running.

Adjust `host_allow` / `origin_allow` in `config.yaml` if you are connecting under a different name,
and connect using a host the certificate covers (`127.0.0.1` or `localhost`).

### The server seems to hang and prints nothing

That is the normal, healthy state. `raxd serve` is a long-running process: after the startup block it
blocks and prints **only** an audit line per connection. Silence means no connections are arriving;
there are no heartbeat messages. Press Ctrl+C to stop it (graceful shutdown, exit `0`).

## `raxd service`

Problems registering or running `raxd` as a system service. The full command reference is in
[`commands.md`](commands.md#raxd-service); the security and operations model is in
[`service-management.md`](service-management.md). Remember the systemd integration is exercised in a
privileged systemd-in-Docker container, while the macOS launchd path is verified on a real macOS host
(see [`service-management.md`](service-management.md#5-the-macos-path-is-not-tested-in-docker)).

### `error: insufficient privileges to install the service`

`install`, `uninstall`, `start`, and `stop` write to system directories and call the service manager,
so they require root.

```
error: insufficient privileges to install the service
  hint: run as root or with sudo: sudo raxd service install
  hint: installation requires root to write system service files
```

Run the command with `sudo`. `raxd` does **not** silently fall back to anything when it lacks
privileges — there is no risk of a partially-installed or root-running daemon from a non-root attempt.

> Installing needs root, but the **daemon** does not run as root: the generated unit/plist set
> `User=raxd` / `UserName=raxd`, so the running service is the unprivileged `raxd` user. The
> `insufficient privileges` error is only about the installer's write access, not the daemon. See
> [`service-management.md`](service-management.md#1-non-root-execution).

`raxd service status` does **not** require root — you can always inspect the state without `sudo`.

### `error: service manager is not available`

`raxd` could not reach the OS service manager (`systemctl` on Linux, `launchctl` on macOS) — it is
not present, or the init system is not the expected one.

```
error: service manager is not available
  hint: ensure systemd (Linux) or launchd (macOS) is running
```

- On Linux, this means `systemd` is not the init system (or `systemctl` is missing). A plain
  container without systemd as PID 1 cannot host the service — the systemd integration is tested in a
  dedicated privileged systemd container (see
  [`service-management.md`](service-management.md#where-this-runs)). On a normal Linux host with
  systemd, confirm `systemctl` is on `PATH`.
- On a non-Linux, non-macOS platform `raxd` cannot manage a service at all and reports
  `error: this platform is not supported` with the hint that service management is available on Linux
  and macOS only.

### The service installed but will not start (crash-loop)

If `raxd service start` succeeds but `raxd service status` keeps showing the daemon stopped or with a
changing PID, the daemon is failing to start under the service. Inspect the logs:

- **Linux:** `journalctl -u raxd -e` (the audit stream and any startup error go to journald). Look
  for the same `serve` startup errors documented under [`raxd serve`](#raxd-serve) above (port in
  use, TLS directory permission, corrupt `keys.db`, bad `config.yaml`, upload-root permission).
- **macOS:** check the log file `/usr/local/var/log/raxd/raxd.log` (the plist's `StandardErrorPath`).

Common service-specific causes:

- **Directory ownership/permissions.** The service runs as `raxd`, so its state directory
  (`/var/lib/raxd` on Linux, `/usr/local/var/raxd` on macOS) and config directory (`/etc/raxd` /
  `/usr/local/etc/raxd`) must exist and be owned by `raxd` with mode `0700`. On Linux systemd creates
  `/var/lib/raxd` and `/etc/raxd` for you (via `StateDirectory` / `ConfigurationDirectory`) before
  the daemon starts; on macOS `install` creates them. If you changed ownership by hand, restore
  `raxd:raxd` ownership and `0700`.
- **A privileged port without the capability.** If you set `port:` to a value `< 1024` after the
  service was installed for the default port, the running unit may not have `CAP_NET_BIND_SERVICE` and
  the bind will fail with a permission error. Re-run `sudo raxd service install` (after `uninstall`)
  so the regenerated unit gains the capability — see the next entry.

### A privileged port (< 1024) fails to bind

By default `raxd` listens on `7822`, which is unprivileged and binds fine as the `raxd` user. If you
set `port:` in `config.yaml` to a value below `1024` (for example `443`), the daemon needs the
`CAP_NET_BIND_SERVICE` capability to bind it without being root.

`raxd service install` reads the port from `config.yaml` at install time and adds the capability to
the generated unit **only** when the port is `< 1024`. So:

1. Set the privileged `port:` in `config.yaml` first.
2. Then run `sudo raxd service install` (uninstall first if it is already installed) so the
   regenerated unit includes `CAP_NET_BIND_SERVICE`.

If you changed the port without re-installing, the old unit lacks the capability and the bind fails.
The capability granted is only `CAP_NET_BIND_SERVICE` — never full root or setuid-root. See
[`service-management.md`](service-management.md#2-privileged-ports--1024-and-the-network-capability).
On macOS the privileged-port mechanics are an open question verified on a real macOS host; keeping the
default `7822` avoids the issue entirely.

### `error: raxd service is not installed`

You ran `raxd service start` or `raxd service stop` before installing the service.

```
error: raxd service is not installed
  hint: install it first with "raxd service install"
```

Install it first with `sudo raxd service install`. Note the asymmetry: `start` and `stop` on an
absent service are **errors** (exit `1`), but `uninstall` on an absent service is a **no-op success**
(exit `0`, with a `not installed` message). A `raxd service status` on an absent service is also a
clean exit `0` showing `installed no`.

### `install` says "already installed"

Re-running `install` on an installed service is safe — it does **not** create a duplicate and is
**not** an error:

```
  already installed   raxd service
  hint: use "raxd service status" to check the current state
```

The command exits `0`. To change the configuration of an installed service (for example after editing
`port:`), `uninstall` first, then `install` again.

### After `uninstall`, the `raxd` user is still there

That is deliberate. `uninstall` removes the unit/plist, disables autostart, and removes the journald
drop-in, but it **intentionally keeps** the system user `raxd` (no login shell, no home, no longer
running) — removing a system user is riskier than keeping an inert one (UID-reuse). The success block
says so:

```
  kept          system user "raxd" (no shell, no home, not running)
  hint: to also remove the user: sudo userdel raxd
```

If you need a zero-footprint removal, delete the user yourself **after** confirming it owns nothing
you still need:

- **Linux:** `sudo userdel raxd`
- **macOS:** `sudo dscl . -delete /Users/raxd`

The state directory is **not** removed by `uninstall` either, so `keys.db` and any data survive.
(The uninstall hint names the real, platform-specific state directory: `/var/lib/raxd` on Linux,
`/usr/local/var/raxd` on macOS.) Alternatively, `raxd service uninstall --purge --yes` removes the
user **and** the state/config directories in one irreversible step — see
[`service-management.md`](service-management.md#3-the-raxd-user-is-kept-after-uninstall) and
[`commands.md`](commands.md#raxd-service-uninstall). This
UID-reuse residual risk is also summarised in
[`production-readiness.md`](production-readiness.md#6-service-uninstall-keeps-the-raxd-user-and-data-uid-reuse).

### The journal fills up despite the size cap

`install` writes a journald drop-in (`/etc/systemd/journald.conf.d/raxd.conf`) with
`SystemMaxUse=200M` and `SystemMaxFileSize=50M`. These limits are **per-host** (they apply to the
whole journal, not just the `raxd` unit). On a host shared with other heavily-logging services the cap
is shared among all of them.

- Confirm the drop-in is present and applied: `systemctl restart systemd-journald` after install, then
  `journalctl --disk-usage`.
- For a limit isolated to `raxd`, switch the daemon to file output and add a `logrotate` config — the
  documented fallback (see
  [`service-management.md`](service-management.md#4-audit-log-rotation)).

On macOS there is no journald; rotation of `/usr/local/var/log/raxd/raxd.log` is done with `newsyslog`
and is verified on a real macOS host.

### The service stopped and did not come back

That is correct for a **graceful stop**. `raxd service stop` sends `SIGTERM`, the daemon exits cleanly
(code `0`), and the restart policy (`Restart=on-failure` on Linux, `KeepAlive.SuccessfulExit=false` on
macOS) does **not** restart a clean exit. The service stays stopped until you `raxd service start` it.
The manager only restarts the service after a **crash** (a non-zero exit or `kill -9`). See
[`service-management.md`](service-management.md#6-restart-on-failure-vs-graceful-stop).

## MCP server (`/mcp`)

The MCP server runs inside `raxd serve` on the `/mcp` route. Most MCP problems are the transport
problems above (auth, TLS, Origin, rate limit), because MCP sits behind the same chain. The MCP-only
cases:

### `GET /mcp` returns `405 Method Not Allowed`

Expected. The server is **stateless** and does not offer a server→client SSE stream, so it answers
`GET /mcp` with `405`. MCP requests use `POST`. Send a JSON-RPC request with `POST`:

```sh
curl -k https://127.0.0.1:7822/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ping","arguments":{}}}'
```

### A `tools/call` returns a JSON-RPC error instead of running

- **Unknown tool name** (for example a typo like `exec` instead of `execute_command`, or `upload`
  instead of `upload_file`) → error code `-32602` (`Invalid params`). Only `ping`, `server_info`,
  `execute_command`, and `upload_file` are registered. No command runs and no file is written. See
  [`mcp.md`](mcp.md#behaviour-and-error-handling).
- **Unknown method** → `-32601`. **Malformed JSON** → `-32700`. **Not a valid JSON-RPC request** →
  `-32600`.

These are JSON-RPC errors returned with HTTP `200`, not transport `4xx`/`501`. After such an error
the server stays up, and a valid `ping` still returns `pong`.

> Do not confuse a JSON-RPC `-32602` ("unknown tool name") with a **tool result** that has
> `isError: true`. The former means the tool name was wrong and nothing was dispatched; the latter
> means the tool *was* called but the operation was rejected or could not complete (see the
> [`execute_command`](#the-execute_command-tool) and [`upload_file`](#the-upload_file-tool) sections
> below).

### An MCP client cannot connect even though `curl` works

Some MCP clients/versions do not forward custom headers (such as `Authorization`) during their
initial health/`initialize` step. If `curl` (which sends the header reliably) succeeds but a client
does not, the issue is likely on the client side. Verify the channel with the `curl` smoke-test in
[`mcp.md`](mcp.md#curl-smoke-test) first. Also confirm the client is configured as a
**streamable-http** (remote) server with the `url` and `Authorization` header, **not** as a stdio
command, and that it trusts the self-signed certificate (or has verification disabled for dev).

## The `execute_command` tool

`execute_command` runs a command on the host. Unlike `ping`/`server_info`, it can fail in
command-specific ways. The full contract is in [`mcp.md`](mcp.md#execute_command); the security
warnings are in [`execute-command-security.md`](execute-command-security.md). This section is for
diagnosing a call.

### A command returns `isError: true`

`isError: true` means the command was **rejected or could not start** — it does **not** mean a
command ran and failed. (A command that ran and failed has a non-zero `exit_code` and `isError`
absent/false — see [the next entry](#a-command-that-exits-non-zero-or-times-out-is-not-an-error).)
The cases, and the matching audit line:

| Symptom (text content) | Cause | Audit |
|------------------------|-------|-------|
| `command not found` | the binary does not exist, is a relative path, or `cwd` is invalid | `msg=FAIL … reason=not-found` / `reason=bad-cwd` |
| `command not allowed` | the allowlist is on and the command is not in it | `msg=DENY … reason=not-allowed` |
| `too many arguments: N > M` | more than `exec.max_args` arguments | `msg=DENY … reason=…` |
| `argument too long: N > M` | a single argument longer than `exec.max_arg_len` | `msg=DENY … reason=…` |
| `timeout_ms N exceeds max M` | the requested `timeout_ms` is above `exec.max_timeout_ms` | `msg=DENY … reason=…` |
| `execution as root is forbidden by policy` | `deny_root: true` and the daemon is root | `msg=WARN reason=running-as-root` then `msg=DENY` |
| `validating "arguments": additional properties not allowed: …` | an unknown input field (for example `env`, `shell`) | none (rejected by the SDK before the handler) |

How to act on each:

- **`command not found`.** This **same** client text covers two distinct causes: (1) the binary does
  not exist (or is a relative path), and (2) an **invalid `cwd`** — a working directory you passed
  that does not exist or is not a directory. The response body is identical in both cases, so the
  client alone cannot tell them apart; **use the audit line's `reason=` to distinguish them**:
  `reason=not-found` is the binary case, `reason=bad-cwd` is the invalid-`cwd` case. To fix the binary
  case, check the binary is installed in the container and on the daemon's `PATH`, and use a bare name
  resolved on `PATH` (for example `ls`) or an **absolute** path (for example `/bin/ls`) — a
  **relative** path (for example `./tool`) is always rejected. To fix the `cwd` case, pass a `cwd`
  that exists and is a directory (or omit `cwd` to use `exec.default_cwd`, default `/tmp`).
- **`command not allowed`.** The allowlist (`exec.allowlist`) is on and your `command` string is not
  an exact match. Remember `ls` ≠ `/bin/ls`: list the command **exactly** the way you call it. See
  [the allowlist note](configuration.md#command-execution-exec-fields).
- **Argument / timeout limits.** Reduce the number/length of `args`, or lower `timeout_ms` below
  `exec.max_timeout_ms`. These limits are configurable in `config.yaml` (`exec.max_args`,
  `exec.max_arg_len`, `exec.max_timeout_ms`).
- **`execution as root is forbidden by policy`.** `deny_root: true` is set and the daemon is running
  as root. Run `raxd` as a non-root user, or set `deny_root: false` (which downgrades the refusal to a
  per-call `WARN` — the command then runs as root, which is itself risky). The simplest way to run
  non-root is to register `raxd` as a service, which runs the daemon as the `raxd` user — see
  [`service-management.md`](service-management.md#1-non-root-execution).
- **`additional properties not allowed`.** Remove the unknown field. The only accepted fields are
  `command`, `args`, `timeout_ms`, `cwd`. There is no `env` field.

### A command that exits non-zero or times out is **not** an error

This trips people up. Two normal outcomes are **not** reported as `isError`:

- **Non-zero exit code.** If the command runs and exits with a non-zero code, the result is a normal
  success: `isError` is absent/false and `exit_code` carries the value. The audit line is
  `msg=MCP … result=ok …`. Inspect `exit_code` / `stderr` in `structuredContent` to decide what to do.
- **Timeout (`timed_out: true`).** If the command runs longer than its timeout, it is killed (the
  whole process tree), and the result is again **not** an error: `isError` is absent/false,
  `timed_out: true`, with whatever partial output was captured. `exit_code` is reported as `-1` in
  this case — treat `timed_out` as the authoritative field. The audit line is
  `msg=MCP … result=ok … timed_out=true`.

So if you expected an error and got a "successful" result with a non-zero `exit_code` or
`timed_out: true`, that is by design: the command **did** run.

### Output looks cut off (`stdout_truncated` / `stderr_truncated`)

Each output stream is capped at `exec.max_output_bytes` (default 1 MiB). When a stream reaches the
cap, the captured output is truncated and the matching flag (`stdout_truncated` or
`stderr_truncated`) is `true`. This is not an error — it protects the daemon from a runaway,
high-output command. If you genuinely need more, raise `exec.max_output_bytes` in `config.yaml` (see
[`configuration.md`](configuration.md#command-execution-exec-fields)), bearing in mind the memory
cost.

### Reading the `execute_command` audit lines

Every call writes its own audit record to the `stderr` audit stream. To watch only command execution:

```sh
raxd serve 2>&1 | grep "tool=execute_command"
```

When `raxd` runs as a service on Linux, the audit stream is in journald — use
`journalctl -u raxd -f | grep "tool=execute_command"` instead.

What to look for:

- `msg=MCP … result=ok` — the command ran. Read `exit_code=`, `duration=`, `timed_out=`.
- `msg=DENY … reason=…` — the command was rejected (allowlist, limits, `deny_root`); it did not run.
- `msg=FAIL … reason=…` — the command could not start; it did not run. The `reason=` tells you which
  case: `reason=not-found` (the binary does not exist or is a relative path) or `reason=bad-cwd` (an
  invalid working directory) — note that both surface the same `command not found` text to the client.
- `msg=WARN … reason=running-as-root` — an extra record written on **every** call when the daemon is
  root. If you see this, the daemon is running as root and every command runs as root. Fix the
  deployment (run non-root — for example via [`raxd service`](service-management.md#1-non-root-execution))
  or set `exec.deny_root: true`.

> **Arguments are logged verbatim.** The `args=[…]` field shows exactly what the client sent, with no
> masking. If you find a secret in the audit log, the client put it in `args` — do not do that. See
> [the security guide](execute-command-security.md#1-do-not-pass-secrets-in-command-arguments-argv).

## The `upload_file` tool

`upload_file` writes a file to the host inside the upload root. Like `execute_command`, it can fail in
tool-specific ways. The full contract is in [`mcp.md`](mcp.md#upload_file); the security warnings are
in [`file-upload-security.md`](file-upload-security.md). This section is for diagnosing a call.

### An upload returns `isError: true`

`isError: true` means the write was **rejected by a control** or **failed mid-write** — no successful
upload reports `isError`. The cases, the client text, and the matching audit line:

| Symptom (text content) | Cause | Audit |
|------------------------|-------|-------|
| `path is outside the upload root` | the `path` escapes the root: a `..`-segment, an absolute path, or an out-of-root symlink | `msg=DENY … reason=traversal` |
| `file already exists (set overwrite to replace)` | the target exists and `overwrite` is `false` (the default) | `msg=DENY … reason="file already exists"` |
| `target path is a directory` | the `path` points at an existing directory, not a file | `msg=DENY … reason="target is a directory"` |
| `file too large: exceeds max_file_bytes` | the **decoded** content is larger than `upload.max_file_bytes` (default 700 KiB) | `msg=DENY … reason="file too large"` |
| `upload denied: total upload quota exceeded` | the write would push the upload root over `upload.max_total_bytes` (when set `> 0`) | `msg=DENY … reason="total upload quota exceeded"` |
| `invalid base64 content` | `content` is not valid standard base64 | `msg=DENY … reason="invalid base64 content"` |
| `invalid file mode` | `mode` is unparseable, has a bit outside `0777` (setuid/setgid/sticky/higher), or is world-writable | `msg=DENY … reason="invalid file mode"` |
| `upload as root is forbidden by policy` | `upload.deny_root: true` and the daemon is root | `msg=WARN reason=running-as-root` then `msg=DENY` |
| `write failed` | an I/O error during the write (for example a full disk) | `msg=FAIL … reason="write failed"` |
| `validating "arguments": …` | an unknown input field, a wrong type, or a missing required `path`/`content` | none (rejected by the SDK before the handler) |

How to act on each:

- **`path is outside the upload root`.** The `path` must be **relative** and stay inside the root.
  Drop any leading `/`, remove `..` segments, and do not target a symlink that points outside the
  root. Missing intermediate sub-directories inside the root are created for you, so a path like
  `scripts/sub/deploy.sh` is fine even if `scripts/sub` does not exist yet.
- **`file already exists (set overwrite to replace)`.** A file already exists at that path and
  `overwrite` defaulted to `false`. To replace it, send `overwrite: true` (the replace is atomic). To
  keep both, choose a different `path`.
- **`target path is a directory`.** You pointed `path` at an existing directory. Choose a file path
  (for example `dir/file.txt`, not `dir`).
- **`file too large: exceeds max_file_bytes`.** The decoded content is over `upload.max_file_bytes`.
  This is the **per-file** limit (default 700 KiB). It is distinct from the **transport body limit**:
  if the **whole request body** is too big you get an HTTP `400`/`413` *before* the tool (no audit
  line, see [the oversized-body entry](#a-request-returns-413-or-400-for-an-oversized-body-and-nothing-shows-up-in-the-audit-stream))
  — whereas this `isError: true` deny means the body was fine but the decoded file exceeded the
  per-file cap. Send a smaller file, or raise `upload.max_file_bytes` (it must stay below the ceiling
  derived from `max_body_bytes`; see [`configuration.md`](configuration.md#file-upload-upload-fields)).
- **`upload denied: total upload quota exceeded`.** A **total-size** cap (`upload.max_total_bytes`, set
  `> 0`) is configured and this write would push the combined size of all files in the upload root
  over it. Nothing is written. Free space by removing files from the upload root, or raise
  `upload.max_total_bytes` in `config.yaml` (`0` disables the cap). This is independent of the per-file
  limit above. See [`configuration.md`](configuration.md#file-upload-upload-fields) and
  [the security guide](file-upload-security.md#7-residual-risks-out-of-scope-for-this-version).
- **`invalid base64 content`.** Encode the file as **standard** base64 with padding. A common mistake
  is sending URL-safe base64 (`-`/`_` instead of `+`/`/`) or stripping the `=` padding.
- **`invalid file mode`.** Use an octal string with only `0777` permission bits, for example `"0600"`,
  `"0644"`, `"0700"`, `"0755"`. setuid (`"04000"`), setgid (`"02000"`), sticky (`"01000"`), higher
  bits (`"010000"`), and world-writable values (`"0666"`, `"0002"`) are rejected. See
  [the mode policy](file-upload-security.md#4-file-mode-policy--only-0777-permission-bits-default-0600).
- **`upload as root is forbidden by policy`.** `upload.deny_root: true` is set and the daemon is root.
  Run `raxd` as a non-root user, or set `upload.deny_root: false` (which downgrades the refusal to a
  per-call `WARN` — the file is then written as root, which is itself risky). Registering `raxd` as a
  service runs the daemon non-root for you — see
  [`service-management.md`](service-management.md#1-non-root-execution).
- **`write failed`.** A genuine I/O error (often a full disk, or a permission problem on the upload
  root). Check free space and that the upload root is writable (`0700`, owned by the daemon user). No
  partial or temp file is left behind. See the next entry.
- **`validating "arguments": …`.** Remove the unknown field, fix the type, or supply the missing
  required field. The only accepted fields are `path`, `content`, `overwrite`, `mode`; `path` and
  `content` are required.

### `error: cannot create upload root directory` / permission denied on the upload root

If `raxd serve` cannot create the upload root at startup, see
[the startup entry above](#error-cannot-create-upload-root-directory-). If the root exists but a write
fails at runtime with `write failed`, the upload root is probably not writable by the daemon user.
Check:

- the upload root exists and is owned by the user running `raxd` (default `<state directory>/uploads`,
  permissions `0700`);
- there is free disk space — and, if you have set `upload.max_total_bytes`, that the upload root is
  below that total-size cap (otherwise the write is denied with `total upload quota exceeded`; see
  [the security guide](file-upload-security.md#7-residual-risks-out-of-scope-for-this-version));
- any custom `upload.root` you set is a writable absolute path.

### Reading the `upload_file` audit lines

Every call writes its own audit record to the `stderr` audit stream. To watch only file uploads:

```sh
raxd serve 2>&1 | grep "tool=upload_file"
```

When `raxd` runs as a service on Linux, use `journalctl -u raxd -f | grep "tool=upload_file"`.

What to look for:

- `msg=MCP … result=ok path=<rel> size=<N>` — the file was written. `path=` is the relative path and
  `size=` is the byte count (plain integer).
- `msg=DENY … reason=…` — the write was rejected by a control (traversal, exists, is-a-directory,
  too-large, total quota exceeded, bad base64, bad mode, `deny_root`); nothing was written.
- `msg=FAIL … reason="write failed"` — an I/O error during the write; nothing usable was written and
  the temp file was cleaned up.
- `msg=WARN … reason=running-as-root` — an extra record written on **every** call when the daemon is
  root. If you see this, the daemon is running as root and every file is written as root. Fix the
  deployment (run non-root) or set `upload.deny_root: true`.

> **The destination path is logged; the content is never logged.** The `path=` field shows exactly
> what the client sent (auto-quoted as a logfmt value). If you find a secret in the audit log, the
> client put it in `path` — do not do that. The file content never appears in the log. See
> [the security guide](file-upload-security.md#2-do-not-put-secrets-in-the-destination-path).

## Key management

### `error: key store is corrupted or unreadable` (from `key create` / `key delete`)

Same `keys.db` corruption as above, surfaced by a key command. The file is never overwritten.

```
error: key store is corrupted or unreadable
  hint: check file permissions on keys.db (must be readable by current user)
  hint: do not attempt to repair the file manually — contact support if data recovery is needed
```

### `error: key "…" not found` / `is already revoked`

The id passed to `raxd key delete` does not match an active key.

```
error: key "d7bc3a34da19d94e" not found
  hint: run "raxd key list" to see available key IDs
```

Run `raxd key list` to see active ids and pass the full 16-character id. A revoked key reports
`is already revoked` and never reappears in `key list`.

### I lost the key body

It cannot be recovered. The full `rax_live_…` key is shown **only once**, by `key create`. `keys.db`
stores only a salted hash, so the key cannot be read back from disk and no command reprints it.
Revoke the lost key (`raxd key delete <id>`) and create a new one.

## Configuration

### `config.yaml` changes have no effect

Only `raxd serve` reads `config.yaml` today, and it reads the file **at startup**. Restart `serve`
after editing the file. This includes the `exec.*` and `upload.*` keys — changing the allowlist,
timeouts, limits, the upload root, the size limit, the total-size cap, the default mode, or either
`deny_root` requires a `serve` restart to take effect. When `raxd` runs as a service, restart it with
`raxd service stop` then `raxd service start`. The other commands (`version`, `status`, the `key`
group) do not act on the config values, except that `raxd service install` reads `port:` to decide on
the privileged-port capability. `raxd config port` is a stub and does not write the file — edit
`config.yaml` by hand.

> Cross-reference: the dedicated entries for the two config-load failures live under
> [`raxd serve`](#configuration-load-errors-share-one-generic-hint) above, including the note that an
> invalid `config.yaml` and an invalid `bind_addr` share one generic hint. An invalid
> `upload.max_file_bytes`, a negative `upload.max_total_bytes`, or an invalid `upload.default_mode` is
> also a startup failure — its `error:` line names the upload field.

## Paths and `$HOME`

### `error: cannot determine config directory: $HOME is not set`

`raxd` could not resolve your home directory.

```
error: cannot determine config directory: $HOME is not set
  hint: set the HOME environment variable and try again
```

Set `HOME` (or the relevant `XDG_CONFIG_HOME` / `XDG_STATE_HOME`) and retry. In a container, make
sure the user running `raxd` has a home directory set.

> The system service avoids this by setting `HOME` (and `XDG_CONFIG_HOME` / `XDG_STATE_HOME`)
> explicitly in the unit/plist, so the unprivileged `raxd` user — which has no normal home — still
> resolves system paths. See
> [`configuration.md`](configuration.md#service-layout-system-service).

## Related documents

- [`installation.md`](installation.md) — installation guide (`curl | sh`, manual install, integrity
  verification, the trust model, macOS quarantine, exit codes).
- [`commands.md`](commands.md) — full command reference, including all `serve` and `service` error
  cases.
- [`service-management.md`](service-management.md) — the system-service security and operations guide
  (non-root execution, privileged-port capability, what `uninstall` keeps, audit-log rotation, the
  macOS verification limitation).
- [`mcp.md`](mcp.md) — MCP integration guide: the `/mcp` endpoint, connection parameters, the
  `ping` / `server_info` / `execute_command` / `upload_file` tools, and audit.
- [`execute-command-security.md`](execute-command-security.md) — mandatory security warnings for
  `execute_command`.
- [`file-upload-security.md`](file-upload-security.md) — mandatory security warnings for `upload_file`.
- [`configuration.md`](configuration.md) — paths, the service layout, `keys.db`, the TLS directory,
  and the `config.yaml` networking, `exec`, and `upload` fields.
- [`production-readiness.md`](production-readiness.md) — known limitations and pending pre-release items.
- [`development.md`](development.md) — building and testing in Docker.

## Author

**Vladimir Kovalev, OEM TECH** — author of raxd.
