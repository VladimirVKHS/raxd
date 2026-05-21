# Configuration and paths

This document describes where `raxd` stores its configuration and state, how to override those
locations, the `keys.db` key database, the TLS directory, and the `config.yaml` format — **as
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

These paths come from `internal/config.PathSet`, resolved by `config.Paths()`. You can see the
resolved values at any time with [`raxd status`](commands.md#raxd-status).

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

# State (keys.db, tls/) goes under /custom/state/raxd/
export XDG_STATE_HOME=/custom/state
```

When a variable is empty or unset, the corresponding default (`$HOME/.config` or
`$HOME/.local/state`) is used.

## Directory creation and permissions

When `raxd` runs, it ensures the config directory, the state directory, and the TLS directory exist.
They are created with **`0700`** permissions (owner-only access). The permissions are set
explicitly and do not depend on the process `umask`. Creating directories that already exist is
safe and does not widen their permissions (the operation is idempotent).

The only condition under which path resolution fails is when the home directory cannot be
determined (`$HOME` is not set). In that case commands that need the paths (such as `status`)
report an error and exit with a non-zero code:

```
error: cannot determine config directory: $HOME is not set
  hint: set the HOME environment variable and try again
```

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
startup; the other commands do not consume it yet.

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
| `port` | integer | `7822` | TCP port the listener binds to |
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
| `max_body_bytes` | integer | `1048576` (1 MiB) | Maximum size of the request body (enforced via `http.MaxBytesReader`; exceeding it yields `413`) |

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

### General behaviour

- **Missing file is not an error.** If `config.yaml` does not exist, every default above is applied.
  `raxd status` shows the file path with the suffix `(not found, defaults applied)` and exits with
  code `0`.
- **Malformed YAML is an error.** If the file exists but is not valid YAML, the config loader
  returns an explicit error (`config file is not valid YAML`). For `raxd serve` this is a startup
  error (exit `1`).
- **`config port` does not write the file yet.** `raxd config port <PORT>` is still a stub. To
  change the port (or any other field) today, edit `config.yaml` directly; `raxd serve` reads it on
  the next start.

## Related documents

- [`commands.md`](commands.md) — full command reference, including `raxd status`, `raxd key`, and
  `raxd serve`.
- [`troubleshooting.md`](troubleshooting.md) — common problems with `serve`, the TLS certificate,
  and the config file.
- [`development.md`](development.md) — building and testing in Docker.
