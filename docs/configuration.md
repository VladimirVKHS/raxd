# Configuration and paths

This document describes where `raxd` stores its configuration and state, how to override those
locations, the `keys.db` key database, the TLS directory, the `config.yaml` format, and — when `raxd`
runs as a registered **system service** — the system paths it uses instead. Everything is **as
implemented in the code today**.

## Path resolution

`raxd` resolves all of its filesystem locations using the XDG Base Directory convention, with one
important decision (D3): **the canonical config path is `~/.config/raxd` on both Linux and macOS.**
macOS's `~/Library/Application Support` is intentionally **not** used, so the two platforms behave
identically.

| Location | Default | How it is derived |
|----------|---------|-------------------|
| Config directory | `~/.config/raxd` | `$XDG_CONFIG_HOME/raxd` if `XDG_CONFIG_HOME` is set, otherwise `$HOME/.config/raxd` |
| Config file | `~/.config/raxd/config.yaml` | `<config directory>/config.yaml` |
| State directory | `~/.local/state/raxd` | `$XDG_STATE_HOME/raxd` if `XDG_STATE_HOME` is set, otherwise `$HOME/.local/state/raxd` |
| Keys database | `~/.local/state/raxd/keys.db` | `<state directory>/keys.db` |
| TLS directory | `~/.local/state/raxd/tls` | `<state directory>/tls` |
| Upload root (default) | `~/.local/state/raxd/uploads` | `<state directory>/uploads` (used when `upload.root` is empty — see [the `upload` fields](#file-upload-upload-fields)) |

These paths come from `internal/config.PathSet`, resolved by `config.Paths()`. You can see the
resolved values at any time with [`raxd status`](commands.md#raxd-status). (`raxd status` shows the
config, keys, and TLS paths; the upload root is created by `raxd serve` from the state directory and is
not listed by `status`.)

> The table above is the **interactive** layout — what an ordinary user gets from `$HOME`. When `raxd`
> runs as a registered **system service**, the daemon resolves these same paths against **system**
> XDG values set in the unit/plist, so it lands in `/etc/raxd` and `/var/lib/raxd` (Linux) instead.
> See [Service layout (system service)](#service-layout-system-service) below.

> Path resolution is implemented explicitly in the standard library (reading `XDG_CONFIG_HOME` /
> `XDG_STATE_HOME` and falling back to `$HOME`). The `adrg/xdg` library is intentionally **not**
> used, because its macOS default would point at `~/Library/Application Support` and conflict with
> decision D3. See [`development.md`](development.md#dependencies) for the rationale.

## XDG overrides

Set `XDG_CONFIG_HOME` and/or `XDG_STATE_HOME` to relocate the directories. The `raxd` sub-directory
is always appended.

```sh
# Config goes to /custom/config/raxd/config.yaml
export XDG_CONFIG_HOME=/custom/config

# State (keys.db, tls/, uploads/) goes under /custom/state/raxd/
export XDG_STATE_HOME=/custom/state
```

When a variable is empty or unset, the corresponding default (`$HOME/.config` or
`$HOME/.local/state`) is used.

This is exactly the mechanism the system service uses: the generated unit/plist set
`XDG_CONFIG_HOME` / `XDG_STATE_HOME` / `HOME` so the daemon resolves system paths without any change
to `raxd`'s code (see [Service layout](#service-layout-system-service)).

## Directory creation and permissions

When `raxd` runs, it ensures the config directory, the state directory, and the TLS directory exist.
They are created with **`0700`** permissions (owner-only access). The permissions are set
explicitly and do not depend on the process `umask`. Creating directories that already exist is
safe and does not widen their permissions (the operation is idempotent). `raxd serve` additionally
creates the **upload root** (default `<state directory>/uploads`) with `0700` permissions on startup
(see [the `upload` fields](#file-upload-upload-fields)).

The only condition under which path resolution fails is when the home directory cannot be
determined (`$HOME` is not set). In that case commands that need the paths (such as `status`)
report an error and exit with a non-zero code:

```
error: cannot determine config directory: $HOME is not set
  hint: set the HOME environment variable and try again
```

## Service layout (system service)

When you register `raxd` as a service with [`raxd service install`](commands.md#raxd-service), the
daemon does **not** use the interactive `~/.config` / `~/.local/state` paths. Instead the generated
unit (Linux) or plist (macOS) sets system XDG values so `raxd`'s ordinary path resolution lands in
system directories — no change to `raxd`'s code is needed (ADR-002). The service runs under the
unprivileged user `raxd`, which has no normal `$HOME`, so the system paths are the correct home for
its state.

The paths come from the service defaults in the code (`DefaultConfigForGOOS`):

| Location | Linux | macOS |
|----------|-------|-------|
| Config directory | `/etc/raxd` | `/usr/local/etc/raxd` |
| State directory | `/var/lib/raxd` | `/usr/local/var/raxd` |
| Log directory | n/a — audit goes to **journald** | `/usr/local/var/log/raxd` |
| Unit / plist | `/etc/systemd/system/raxd.service` | `/Library/LaunchDaemons/tech.oem.raxd.plist` |
| Journal drop-in | `/etc/systemd/journald.conf.d/raxd.conf` | n/a (no journald) |

How the daemon is pointed at these directories:

- **Linux.** The unit sets `Environment=XDG_CONFIG_HOME=/etc`, `Environment=XDG_STATE_HOME=/var/lib`,
  and `Environment=HOME=/var/lib/raxd`. With `$XDG_CONFIG_HOME=/etc`, `raxd`'s resolver appends
  `/raxd`, giving `/etc/raxd`; likewise `$XDG_STATE_HOME=/var/lib` gives `/var/lib/raxd`. systemd
  creates `/var/lib/raxd` (`StateDirectory=raxd`) and `/etc/raxd` (`ConfigurationDirectory=raxd`)
  owned by `raxd` **before** the daemon starts, both with mode `0700` (explicit
  `StateDirectoryMode=0700` / `ConfigurationDirectoryMode=0700`, not the systemd default of `0755`).
- **macOS.** The plist sets `XDG_CONFIG_HOME=/usr/local/etc`, `XDG_STATE_HOME=/usr/local/var`, and
  `HOME=/usr/local/var/raxd`. launchd has no `StateDirectory` equivalent, so `install` creates
  `/usr/local/var/raxd`, `/usr/local/etc/raxd`, and `/usr/local/var/log/raxd` itself, each `0700`
  and owned by `raxd`.

> **Why `/usr/local` on macOS and not `/etc` / `/var/lib`.** On macOS, `/etc` and `/var` are
> system-managed (under SIP); third-party daemons use the `/usr/local` prefix. The plist's
> `XDG_CONFIG_HOME` / `XDG_STATE_HOME` are derived from those paths so the directory the daemon
> resolves is exactly the one `install` created.

Permissions of the service artifacts:

| Artifact | Owner | Mode |
|----------|-------|------|
| Unit (`raxd.service`) | `root:root` | `0644` |
| Journal drop-in (`raxd.conf`) | `root:root` | `0644` |
| plist (`tech.oem.raxd.plist`) | `root:wheel` | `0644` |
| State directory (`/var/lib/raxd`, `/usr/local/var/raxd`) | `raxd:raxd` | `0700` |
| Config directory (`/etc/raxd`, `/usr/local/etc/raxd`) | `raxd:raxd` | `0700` |
| Log directory (macOS, `/usr/local/var/log/raxd`) | `raxd:raxd` | `0700` |
| `keys.db`, TLS private key (inside the state dir) | `raxd:raxd` | `0600` |

The registration files are owned by root and not group/world-writable, so the unprivileged `raxd`
user **cannot** rewrite the definition of its own service. The state directory is `0700` (not the
wider systemd default), and `keys.db` and the TLS private key keep their `0600` — exactly as for the
interactive layout above.

> **Removing the service layout entirely.** `raxd service uninstall` removes the registration but, by
> design, **keeps** the `raxd` user and the state/config directories. To erase them too, use
> `raxd service uninstall --purge --yes` — see
> [`service-management.md`](service-management.md#3-the-raxd-user-is-kept-after-uninstall).

### The listening port and privileged ports

The service listens on the port from `config.yaml` (`port:`, default `7822`), the same value
[`raxd serve`](commands.md#raxd-serve) would bind. `raxd service install` reads it at install time.

- **Default `7822` (≥ 1024).** Not a privileged port, so the daemon needs no special privilege to
  bind it. This is why the service runs fully as the unprivileged `raxd` user with no extra
  capability.
- **A privileged port (< 1024).** On Linux the generated unit gains exactly one capability —
  `CAP_NET_BIND_SERVICE` — and nothing more (no full root, no setuid-root). For that case
  `NoNewPrivileges` is omitted while the other hardening is kept; this is a deliberate, narrow
  trade-off documented in
  [`service-management.md`](service-management.md#2-privileged-ports--1024-and-the-network-capability).

Because the daemon runs as `raxd` (not root), `config.yaml`'s `exec.deny_root` / `upload.deny_root`
levers are a secondary defence — the non-root service layout is the primary one. See
[`service-management.md`](service-management.md#1-non-root-execution).

For the full service security and operations model — non-root execution, the privileged-port
capability, what `uninstall` keeps, audit-log rotation, and the macOS verification limitation — see
[`service-management.md`](service-management.md).

## The `keys.db` key database

`keys.db` is the file where `raxd` stores API keys created with
[`raxd key create`](commands.md#raxd-key-create). It lives at the `KeysDB` path resolved above
(`~/.local/state/raxd/keys.db` by default) and is created the first time you run `key create`.

### Permissions

`keys.db` is created with **`0600`** permissions (read/write for the owner only). The file is
written atomically: `raxd` writes to a temporary file in the same directory, sets `0600` on it
**before** writing any content, syncs it, and renames it into place. There is no moment at which the
file is readable with wider permissions. Its parent (the state directory) is `0700`, and that
permission is not widened.

### What is stored — and what is not

For each key, `keys.db` holds:

- a random id (the identifier shown in `key list` and used by `key delete`);
- the optional label;
- the creation time, the last-used time, and the revoked flag;
- a per-key random **salt**;
- the **SHA-256 hash** of the key combined with that salt;
- a short, non-reversible **fingerprint** (a 12-character hex prefix of `sha256(key)`), used only
  for audit logging.

The **plaintext key is never stored**. `keys.db` contains only the salted hash and the salt, so the
key cannot be reconstructed from the file. This follows the project's security baseline (§1). The
key is shown to you once, at creation time, and never again.

The file is JSON with a small versioned envelope (`{"version": 1, "keys": [...]}`). Treat it as
internal: do not edit it by hand.

### How `serve` uses `keys.db`

`raxd serve` opens this same `keys.db` and authenticates every connection against it (via the
keystore's constant-time `Verify`). Two consequences worth noting:

- An **empty or missing** `keys.db` is a valid state: `serve` starts but warns that every connection
  will be rejected with `401`. Create a key first (`raxd key create`).
- A **corrupt** `keys.db` is a startup error: `serve` reports `key store is corrupted or unreadable`
  and exits with code `1`, without modifying the file. (If the store becomes unreadable while the
  server is running, individual requests are answered with `403` and a `DENY` audit line.)

> **Under the service, `keys.db` lives in the system state directory.** A daemon registered with
> `raxd service` reads `keys.db` from `/var/lib/raxd/keys.db` (Linux) or `/usr/local/var/raxd/keys.db`
> (macOS), owned by `raxd`, `0600`. Create keys as the operator with `raxd key create` against that
> same state directory before starting the service. A full `raxd service uninstall --purge --yes`
> **deletes** this `keys.db` along with the state directory (irreversible).

### Behaviour and edge cases

- **Missing file is not an error.** Before you create your first key, `keys.db` does not exist.
  `key list` (and the internal verification path) treat a missing file as an empty store and exit
  with code `0`.
- **Corrupt file is reported, never overwritten.** If `keys.db` exists but cannot be parsed,
  `raxd` reports `key store is corrupted or unreadable` and leaves the file untouched, so no data is
  lost.
- **Revoked keys are retained.** `key delete` performs a soft revoke: the record is marked revoked
  and kept in `keys.db` for audit purposes, but it no longer appears in `key list` and can no longer
  authenticate.

See [`commands.md`](commands.md#api-keys-raxd-key) for the full `key` command reference.

## The TLS directory (`tls/`)

The TLS directory (`~/.local/state/raxd/tls` by default) holds the certificate and private key that
`raxd serve` uses for the TLS 1.3 listener. The directory itself is created with `0700` permissions
the first time `raxd` runs.

**The files are created on the first `raxd serve`, not before.** Until you start the server for the
first time, the directory exists (or is reserved) but contains no certificate.

| File | Permissions | Created by |
|------|-------------|------------|
| `cert.pem` | `0644` | `raxd serve` (first run) — self-signed ECDSA P-256 certificate, SAN `127.0.0.1` + `localhost` |
| `key.pem` | `0600` | `raxd serve` (first run) — the matching private key |

Behaviour:

- **First run:** `serve` generates the pair (atomic write: temp file → `chmod` → rename, so the key
  is never momentarily world-readable).
- **Later runs:** the existing pair is **reused** and never regenerated.
- **Corrupt or partial state:** if the files exist but cannot be loaded — for example only one of the
  two is present — `serve` reports `TLS certificate or key is corrupted or unreadable` and exits
  `1`. It does **not** overwrite anything; you must remove the files manually to regenerate.

> The certificate is self-signed and there is no built-in trust anchor. Clients must trust it
> explicitly or skip verification in a controlled local setup (mTLS / client certificates are out of
> scope for this build). See [`commands.md`](commands.md#raxd-serve) for the `serve` reference and
> [`troubleshooting.md`](troubleshooting.md#raxd-serve) for certificate problems.

## The `config.yaml` file

Configuration is read from the config file shown above (`~/.config/raxd/config.yaml` by default)
using [viper](https://github.com/spf13/viper). The file is YAML. It is read by `raxd serve` at
startup; the other commands do not consume it yet — with one exception:
[`raxd service install`](commands.md#raxd-service-install) reads the `port:` value (only) to decide
whether the service needs the privileged-port capability.

### Networking and `serve` fields

`raxd serve` reads the following keys from `config.yaml`. Every key has a built-in default, so a
missing file (or a file that sets only some keys) is fine — the defaults below apply. A full example
with the default values:

```yaml
# ~/.config/raxd/config.yaml

# Listener
port: 7822
bind_addr: "127.0.0.1"

# Rate limiting (token bucket, applied per key AND per client IP)
rate_limit: 10        # sustained requests per second
rate_burst: 20        # maximum burst

# DNS-rebinding protection (host part only; ports are ignored for Host)
host_allow:   ["localhost", "127.0.0.1", "::1"]
origin_allow: ["localhost", "127.0.0.1", "::1"]

# Connection timeouts (Go duration strings)
read_timeout:        "30s"
read_header_timeout: "10s"
write_timeout:       "30s"
idle_timeout:        "120s"

# Size limits
max_header_bytes: 1048576   # 1 MiB
max_body_bytes:   1048576   # 1 MiB
```

| Key | Type | Default | Meaning |
|-----|------|---------|---------|
| `port` | integer | `7822` | TCP port the listener binds to. Also read by `raxd service install` to decide on the privileged-port capability (a value `< 1024` triggers `CAP_NET_BIND_SERVICE`) |
| `bind_addr` | string | `127.0.0.1` | Local address to bind to. Must be a valid IP. Default is loopback only |
| `rate_limit` | number | `10` | Sustained request rate (events/second) per key and per IP |
| `rate_burst` | integer | `20` | Maximum burst size for the token bucket |
| `host_allow` | list of strings | `["localhost", "127.0.0.1", "::1"]` | Allowed `Host` header values (host part only). A request whose `Host` is not in this list gets `403` |
| `origin_allow` | list of strings | `["localhost", "127.0.0.1", "::1"]` | Allowed `Origin` hostnames. If `Origin` is present and its hostname is not in this list, the request gets `403`. A request with **no** `Origin` is allowed (typical for non-browser clients) |
| `read_timeout` | duration | `30s` | Maximum time to read the full request (incl. body) — Slowloris protection |
| `read_header_timeout` | duration | `10s` | Maximum time to read request headers |
| `write_timeout` | duration | `30s` | Maximum time to write the response |
| `idle_timeout` | duration | `120s` | Maximum idle time for a keep-alive connection |
| `max_header_bytes` | integer | `1048576` (1 MiB) | Maximum size of request headers |
| `max_body_bytes` | integer | `1048576` (1 MiB) | Maximum size of the request body (enforced via `http.MaxBytesReader`; exceeding it rejects the request before any tool runs — see the note below). Also caps the `upload_file` body and constrains `upload.max_file_bytes` |

Notes on the values:

- **`bind_addr` is validated as an IP.** An invalid value (for example `0.0.0.256`) makes `serve`
  fail at startup with `invalid bind address "…": not a valid IP address` and exit `1`. The default
  binds to loopback only; binding to a non-loopback address (such as `0.0.0.0`) exposes the server
  beyond the host and is the operator's responsibility.
- **Durations** are Go duration strings (`"30s"`, `"2m"`, `"500ms"`). Plain numbers are interpreted
  as nanoseconds, so prefer the string form.
- **Origin matching is strict.** Only the hostname part of the `Origin` URL is compared, and it must
  match an allowlist entry exactly (case-insensitive). Prefix tricks such as
  `https://localhost.evil.com` do **not** match `localhost`.
- **`host_allow` / `origin_allow` lists** are compared case-insensitively; the `Host` comparison
  uses only the host part (any port is stripped).
- **Rate-limiter cleanup** is not configurable: idle per-key/per-IP limiters are garbage-collected
  after a fixed 10-minute TTL.
- **`max_body_bytes` and the body-limit rejection.** A request body larger than `max_body_bytes` is
  rejected by the outermost limiter (`http.MaxBytesReader`) **before** auth and audit, so it writes
  **no** audit line. For an oversized `upload_file` body this surfaces (in this build) as an HTTP
  `400` from the MCP SDK ("failed to read body"), **not** a `413` — see
  [`mcp.md`](mcp.md#upload_file) for that caveat and [`troubleshooting.md`](troubleshooting.md#a-request-returns-413-and-nothing-shows-up-in-the-audit-stream).
- **Changing `port` for the service.** If you set a port `< 1024`, run `raxd service install` (or
  re-install) so the generated unit picks up the capability for the new port. See
  [`service-management.md`](service-management.md#2-privileged-ports--1024-and-the-network-capability).

### Command execution (`exec`) fields

The `exec` section configures the MCP **`execute_command`** tool — the security-critical capability
that runs commands on the host. These keys are read by `raxd serve` at startup and supplied to the
tool. Every key has a safe built-in default, so an absent `config.yaml` (or one that omits the `exec`
section) runs with the defaults below.

> **Read the [`execute_command` security guide](execute-command-security.md) before changing these.**
> The defaults are deliberately conservative on everything **except** the allowlist, which is **off by
> default** (any command is allowed). For production, turn the allowlist on and run `raxd` as a
> non-root user.

A full `exec` section with the default values:

```yaml
# ~/.config/raxd/config.yaml (exec section)

exec:
  # Command allowlist. Empty = DISABLED = any command may run.
  # When non-empty, only commands whose `command` string matches an entry
  # EXACTLY (no regex, no prefix, case-sensitive) may run.
  allowlist: []

  # Timeouts (milliseconds)
  default_timeout_ms: 30000     # 30s — used when the client omits timeout_ms
  max_timeout_ms:     300000    # 5m  — a requested timeout_ms above this is rejected

  # Working directory used when the client omits cwd (must exist; a client cwd is validated)
  default_cwd: "/tmp"

  # Child-process environment: an explicit whitelist of variables copied from the daemon.
  # Dangerous loader/shell variables (LD_PRELOAD, LD_LIBRARY_PATH, DYLD_INSERT_LIBRARIES, IFS)
  # are intentionally absent and are NOT passed to the child.
  env_whitelist: ["PATH", "HOME", "LANG", "TERM"]

  # Input limits checked before the command starts (argv-DoS protection)
  max_args:    256              # maximum number of arguments
  max_arg_len: 131072           # 128 KiB — maximum length of a single argument

  # Output limit per stream (OOM protection); output beyond this is truncated
  max_output_bytes: 1048576     # 1 MiB per stream (stdout and stderr separately)

  # Root policy: false = WARN only (command still runs as root); true = refuse to run when euid==0
  deny_root: false
```

| Key | Type | Default | Meaning |
|-----|------|---------|---------|
| `exec.allowlist` | list of strings | `[]` (empty = **disabled**) | When non-empty, a command runs only if its `command` string matches an entry **exactly** — not regex, not prefix, case-sensitive, compared **before** `PATH` resolution. Empty = allowlist off = **any command allowed**. See the warning below |
| `exec.default_timeout_ms` | integer | `30000` (30s) | Timeout applied when the client omits `timeout_ms` (or sends `0`) |
| `exec.max_timeout_ms` | integer | `300000` (5m) | Hard cap. A `timeout_ms` above this is rejected (`isError: true`); the command does not run |
| `exec.default_cwd` | string | `/tmp` | Working directory used when the client omits `cwd`. A client-supplied `cwd` is validated (must exist and be a directory) |
| `exec.env_whitelist` | list of strings | `["PATH", "HOME", "LANG", "TERM"]` | Environment variables copied from the daemon into the child process. Anything not listed (including `LD_PRELOAD`, `LD_LIBRARY_PATH`, `DYLD_INSERT_LIBRARIES`, `IFS`) is **not** passed |
| `exec.max_args` | integer | `256` | Maximum number of `args`. Exceeding it is denied before the command runs |
| `exec.max_arg_len` | integer | `131072` (128 KiB) | Maximum byte length of a single argument. Exceeding it is denied before the command runs |
| `exec.max_output_bytes` | integer | `1048576` (1 MiB) | Maximum captured bytes **per stream** (stdout and stderr each). Output beyond this is truncated and the matching `*_truncated` flag is set |
| `exec.deny_root` | boolean | `false` | `false` = when the daemon runs as root, log a `WARN` on every call but run the command anyway; `true` = refuse to run when the daemon is root (`isError: true`, `DENY` audit) |

Notes on the values, with the security implications spelled out:

- **The allowlist is off by default — and matched exactly.** With the default empty list, **any**
  command an authenticated client requests will run. That is the intended SSH-class behaviour, but it
  means a single valid key can run anything the daemon user can. When you enable it, list commands
  **exactly the way clients call them**: because the match is on the raw `command` string (before
  `PATH` resolution), **`ls` and `/bin/ls` are different entries** — listing one does not permit the
  other. There is no regex, prefix, or case-insensitive matching. See
  [the security guide](execute-command-security.md#2-the-allowlist-is-strict-and-exact--and-off-by-default).
- **No client-supplied environment.** There is intentionally no `env` field in the tool input, and no
  `config.yaml` key to accept one. The child environment is built only from `exec.env_whitelist`. This
  blocks loader-injection attacks (`LD_PRELOAD` and friends).
- **`deny_root` is a hard lever, not the primary defence.** The primary defence against running as
  root is to **run `raxd` as a non-root user** — which the [`raxd service`](commands.md#raxd-service)
  layout does for you by running the daemon as the `raxd` user. `deny_root: true` is the operator's
  lever to refuse execution if the daemon ever finds itself running as root. See
  [the security guide](execute-command-security.md#3-the-deny_root-policy-and-running-as-root) and
  [`service-management.md`](service-management.md#1-non-root-execution).
- **Arguments are logged verbatim.** The `args` a client sends are written to the audit log without
  masking. **Do not pass secrets in arguments.** See
  [the security guide](execute-command-security.md#1-do-not-pass-secrets-in-command-arguments-argv).
- **Audit-log rotation is delegated to the system.** The audit stream (including `execute_command`
  records) goes to `stderr`. Under the [`raxd service`](commands.md#raxd-service) layout on Linux,
  that stderr goes to journald and is size-capped by the drop-in `install` writes; see
  [`service-management.md`](service-management.md#4-audit-log-rotation). For a bare file-output
  deployment, configure `logrotate` yourself.

For the tool's input/output contract, error mapping, and curl examples, see
[`mcp.md`](mcp.md#execute_command).

### File upload (`upload`) fields

The `upload` section configures the MCP **`upload_file`** tool — the capability that writes a file
into the host's filesystem. These keys are read by `raxd serve` at startup and supplied to the tool.
Every key has a safe built-in default, so an absent `config.yaml` (or one that omits the `upload`
section) runs with the defaults below.

> **Read the [`upload_file` security guide](file-upload-security.md) before changing these.** Keep the
> upload root a dedicated directory free of bind-mounts, run `raxd` as a non-root user, and remember
> that `max_file_bytes` is constrained by `max_body_bytes`.

A full `upload` section with the default values:

```yaml
# ~/.config/raxd/config.yaml (upload section)

upload:
  # Upload root: the directory writes are confined to (via os.Root).
  # Empty = use the safe default <state directory>/uploads (created with 0700).
  # A relative path is NOT recommended; use an absolute path or leave empty.
  root: ""

  # Maximum DECODED size of a single uploaded file, in bytes.
  # Default 716800 = 700 KiB. Must be > 0 and <= the ceiling derived from
  # max_body_bytes (see the validation note below), or serve fails at startup.
  max_file_bytes: 716800

  # Total-size cap on the WHOLE upload root, in bytes.
  # 0 = DISABLED (default). When > 0, an upload that would push the total bytes
  # of all files under the upload root over this limit is denied (nothing written).
  # A negative value is rejected at startup. Independent of max_file_bytes.
  max_total_bytes: 0

  # Default file mode (octal string) used when the client omits `mode`.
  # Only 0777 permission bits are allowed; setuid/setgid/sticky and
  # world-writable (0002) are forbidden and rejected at startup.
  default_mode: "0600"

  # Default overwrite policy. false = an existing target is NOT overwritten
  # unless the client explicitly sends overwrite:true.
  overwrite_default: false

  # Root policy: false = WARN only (the file is still written as root);
  # true = refuse to write when euid==0. Separate from exec.deny_root.
  deny_root: false
```

| Key | Type | Default | Meaning |
|-----|------|---------|---------|
| `upload.root` | string | `""` → `<state directory>/uploads` | The directory all writes are confined to (via `os.Root`). When empty, `serve` resolves the **safe default** `<state directory>/uploads` (by default `~/.local/state/raxd/uploads`) and creates it with `0700`. A relative `path` from a client is always taken **relative to this root** |
| `upload.max_file_bytes` | integer | `716800` (700 KiB) | Maximum **decoded** size of a single uploaded file. Exceeding it is denied (`isError: true`); the file is not written. Validated at startup against `max_body_bytes` — see the note below |
| `upload.max_total_bytes` | integer | `0` (disabled) | Total-size cap, in **bytes**, on the **whole upload root**. `0` = no limit (default). When `> 0`, an upload that would push the total bytes of all files under the upload root over this value is denied (`isError: true`, `DENY` audit); nothing is written. A **negative** value is rejected at startup. Independent of `max_file_bytes` (both apply). See the note below |
| `upload.default_mode` | string | `"0600"` | File mode used when the client omits `mode`. Octal string. Validated at startup against the mode policy (only `0777` bits; no setuid/setgid/sticky; no world-writable) |
| `upload.overwrite_default` | boolean | `false` | The default overwrite policy. The `overwrite` field defaults to `false`, so an existing target is preserved unless the client sends `overwrite: true` |
| `upload.deny_root` | boolean | `false` | `false` = when the daemon runs as root, log a `WARN` on every call but write the file anyway; `true` = refuse to write when the daemon is root (`isError: true`, `DENY` audit). **Separate** from `exec.deny_root` |

Startup validation (a misconfiguration fails `serve` at startup with exit `1`, before the listener
binds):

- **`max_file_bytes` is bounded by `max_body_bytes`.** It must be `> 0` and `<= floor((max_body_bytes
  − 1024) × 3/4)`. The ceiling accounts for base64 inflation (≈ +33%) plus a 1024-byte reserve for the
  JSON-RPC/base64 overhead. With the default `max_body_bytes` of 1 MiB the ceiling is roughly 785 KiB,
  so the default `max_file_bytes` of 700 KiB sits safely below it. A value of `0`, a negative value, or
  one above the ceiling is rejected:

  ```
  upload.max_file_bytes=… is invalid: must be > 0 and ≤ … (derived from max_body_bytes=…; SR-76)
  ```

  Setting `max_file_bytes` near or above the ceiling would mean files at the top of the range are cut
  off by the transport body limit (an HTTP `400`, see [`mcp.md`](mcp.md#upload_file)) before the tool
  sees them — keeping it below the ceiling makes the tool itself return a clean "file too large" deny.
- **`max_total_bytes` must be `≥ 0`.** A **negative** value is rejected at startup:

  ```
  upload.max_total_bytes=… is invalid: must be ≥ 0 (0 = disabled; SR-98/AC1)
  ```

  `0` is valid and means the cap is disabled (the default). It is **not** tied to `max_file_bytes`: a
  value such as `0 < max_total_bytes < max_file_bytes` is accepted (an individual file may then be
  refused by the total cap even though it would pass the per-file limit).
- **`default_mode` must pass the mode policy.** It must be a parseable octal string with **only**
  `0777` permission bits — no setuid (`04000`), setgid (`02000`), sticky (`01000`), any higher bit, or
  world-writable (`0002`). An invalid value is rejected:

  ```
  upload.default_mode="…" is invalid: …
  ```

Notes on the values, with the security implications spelled out:

- **The upload root is confined by `os.Root`.** Writes cannot escape the root via `..`, an absolute
  client path, or an out-of-root symlink — those are denied. The one limitation is that `os.Root` does
  **not** block **mount points**: do **not** place a bind-mount inside the upload root. See
  [the security guide](file-upload-security.md#1-do-not-place-a-bind-mount-or-external-filesystem-inside-the-upload-root).
- **The default root is safe.** When `upload.root` is empty, `serve` uses `<state directory>/uploads`
  (created `0700`) — never `/`, `/root`, or a root home directory. Under the service the state
  directory is the system one (`/var/lib/raxd` on Linux), so the default upload root is
  `/var/lib/raxd/uploads`, owned by `raxd`.
- **The mode policy blocks privilege/integrity bits.** setuid/setgid/sticky and world-writable are
  rejected for both `default_mode` and a client-supplied `mode`. The created file inherits the daemon's
  UID/GID; the tool never `chown`s or elevates. See
  [the security guide](file-upload-security.md#4-file-mode-policy--only-0777-permission-bits-default-0600).
- **The destination path is logged.** The relative path is written to the audit log (the file
  **content** is never logged). **Do not put secrets in `path`.** See
  [the security guide](file-upload-security.md#2-do-not-put-secrets-in-the-destination-path).
- **The total-size cap (`max_total_bytes`) bounds disk use.** When enabled (`> 0`), the tool sums the
  size of all regular files under the upload root before each write and **denies** an upload that would
  exceed the cap — closing the application-level disk-fill risk. The accounting includes files written
  before the cap was enabled and files in sub-directories; symlinks are not followed. The denial is
  neutral (no absolute path, no exact byte numbers). With the default `0` the cap is off and behaviour
  is unchanged. There is still **no per-key quota** and **no content inspection** (see
  [the security guide](file-upload-security.md#7-residual-risks-out-of-scope-for-this-version)). A
  filesystem/container quota on the upload root remains a valid complementary measure.
- **No environment variable overrides.** Like the `exec` section, the `upload` keys are read **only**
  from `config.yaml`; there is no env-var override.

For the tool's input/output contract, error mapping, and curl examples, see
[`mcp.md`](mcp.md#upload_file).

### General behaviour

- **Missing file is not an error.** If `config.yaml` does not exist, every default above (networking,
  `exec`, and `upload`) is applied. `raxd status` shows the file path with the suffix
  `(not found, defaults applied)` and exits with code `0`.
- **Malformed YAML is an error.** If the file exists but is not valid YAML, the config loader
  returns an explicit error (`config file is not valid YAML`). For `raxd serve` this is a startup
  error (exit `1`).
- **Invalid `upload` values fail at startup.** An out-of-range `upload.max_file_bytes`, a negative
  `upload.max_total_bytes`, or an invalid `upload.default_mode` makes `serve` exit `1` with the
  messages shown above.
- **`config port` does not write the file yet.** `raxd config port <PORT>` is still a stub. To
  change the port (or any other field, including `exec.*` / `upload.*`) today, edit `config.yaml`
  directly; `raxd serve` reads it on the next start. If you change `port` for a registered service,
  re-run `raxd service install` so the unit picks up the right privileged-port capability.

## Related documents

- [`commands.md`](commands.md) — full command reference, including `raxd status`, `raxd key`,
  `raxd service`, and `raxd serve`.
- [`service-management.md`](service-management.md) — the system-service security and operations guide
  (non-root execution, privileged-port capability, what `uninstall` keeps and what `uninstall --purge
  --yes` removes, audit-log rotation, the macOS verification limitation).
- [`mcp.md`](mcp.md) — the MCP integration guide, including the `execute_command` and `upload_file`
  tool references.
- [`execute-command-security.md`](execute-command-security.md) — mandatory security warnings for
  `execute_command`.
- [`file-upload-security.md`](file-upload-security.md) — mandatory security warnings for `upload_file`.
- [`troubleshooting.md`](troubleshooting.md) — common problems with `serve`, the service, the TLS
  certificate, the config file, `execute_command`, and `upload_file`.
- [`production-readiness.md`](production-readiness.md) — known limitations and pending pre-release items.
- [`development.md`](development.md) — building and testing in Docker.

## Author

**Vladimir Kovalev, OEM TECH** — author of raxd.
