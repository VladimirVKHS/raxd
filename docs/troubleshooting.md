# Troubleshooting

Common problems and how to resolve them. Every behaviour here is taken from the current code; nothing
is hypothetical. Run `raxd` inside Docker (security baseline §6) — see
[`development.md`](development.md).

All error messages follow the project format: a lowercase `error:` line describing what happened,
followed by one or more indented `hint:` lines describing what to do.

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
  `serve` again.

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

The two concrete cases follow.

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

### `curl` to `/healthz` fails with a TLS / certificate error

The server uses a **self-signed** certificate, so a client that verifies certificates will reject it
by default. There is no built-in trust anchor and no mTLS in this build.

- For a controlled local test, skip verification: `curl -k -H "Authorization: Bearer $KEY"
  https://127.0.0.1:7822/healthz`.
- Otherwise, add the generated `cert.pem` to your client's trust store. The certificate's SAN covers
  `127.0.0.1` and `localhost`, so connect using one of those names/addresses.

### A request returns `501 Not Implemented`

This is expected. After authentication, the only route that does real work is `GET /healthz`. Every
other path (for example `/exec`, `/mcp`) returns `501` with the body `not implemented`, because
command execution, the MCP server, and file upload are not implemented yet. There is nothing to fix —
those features arrive in later tasks.

### A request returns `413` (and nothing shows up in the audit stream)

The request body exceeded `max_body_bytes` (default 1 MiB). The `413` is produced by the outermost
body-size limit (`http.MaxBytesReader`), which sits **before** the auth and audit middlewares — so,
unlike the `401` / `403` / `429` cases, a `413` **does not** write any audit line (`FAIL` / `DENY` /
`RATE`). If you are debugging an oversized request, do not look for it in the audit stream: confirm
it from the client side (the `413` response) instead. Reduce the request body or raise
`max_body_bytes` in `config.yaml` (see
[`configuration.md`](configuration.md#networking-and-serve-fields)).

### A request returns `429 Too Many Requests`

You exceeded the rate limit. Limiting is a token bucket applied **per key and per client IP** with a
sustained `rate_limit` (default 10 req/s) and a `rate_burst` (default 20). The audit line shows which
limit was hit:

```
time=… level=WARN msg=RATE fp=a3f9c1d2e847 remote=… reason="rate limit exceeded (key)"
time=… level=WARN msg=RATE fp=- remote=… reason="rate limit exceeded (ip)"
```

Slow down, or raise `rate_limit` / `rate_burst` in `config.yaml` (see
[`configuration.md`](configuration.md#networking-and-serve-fields)).

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
after editing the file. The other commands (`version`, `status`, the `key` group) do not act on the
config values. `raxd config port` is a stub and does not write the file — edit `config.yaml` by hand.

> Cross-reference: the dedicated entries for the two config-load failures live under
> [`raxd serve`](#configuration-load-errors-share-one-generic-hint) above, including the note that an
> invalid `config.yaml` and an invalid `bind_addr` share one generic hint.

## Paths and `$HOME`

### `error: cannot determine config directory: $HOME is not set`

`raxd` could not resolve your home directory.

```
error: cannot determine config directory: $HOME is not set
  hint: set the HOME environment variable and try again
```

Set `HOME` (or the relevant `XDG_CONFIG_HOME` / `XDG_STATE_HOME`) and retry. In a container, make
sure the user running `raxd` has a home directory set.

## Related documents

- [`commands.md`](commands.md) — full command reference, including all `serve` error cases.
- [`configuration.md`](configuration.md) — paths, `keys.db`, the TLS directory, and the
  `config.yaml` networking fields.
- [`development.md`](development.md) — building and testing in Docker.
