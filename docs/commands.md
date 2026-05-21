# Command reference

This document describes every command in the `raxd` command tree **as it exists in the code
today**. The service commands (`version`, `status`), the API-key commands
(`key create`, `key list`, `key delete`), and the network server (`serve`) are fully working. The
one remaining feature command, `config port`, is present as an honest stub that reports
`not implemented yet`.

All CLI text (usage strings, messages, the banner, errors) is in English.

> Where to run these commands: per the security baseline, `raxd` is built and run **inside Docker
> only**. This applies in particular to `raxd serve`, which opens a TLS listener. Examples below
> show the command and its output; for how to actually invoke them in a container, see
> [`development.md`](development.md).

## Command tree

```
raxd
├── version            Print version information           (working)
├── status             Show daemon status and paths        (working)
├── key                Manage API keys
│   ├── create         Create a new API key                (working)
│   ├── list           List all API keys                   (working)
│   └── delete         Revoke an API key                   (working)
├── config             Manage configuration
│   └── port           Set the listening port              (stub)
└── serve              Start the raxd TLS server           (working)
```

`raxd --help` lists the root command and all sub-commands. Each command also responds to
`raxd <command> --help` with its own usage and description.

## Global behaviour

These rules apply to the whole command tree.

### The banner (stderr)

Before running any command, `raxd` prints a product banner to **stderr** via the root command's
`PersistentPreRun`. This keeps the machine-readable **stdout** clean — for example
`raxd status | grep state` and `raxd key create > key.txt` are not polluted by the banner.

The banner is **not** printed for `--help` (cobra prints help itself, skipping `PersistentPreRun`).

The banner is a plain-text Unicode box and always contains the author line. On a development build
(no ldflags) it looks like this:

```
┌──────────────────────────────────────────┐
│  raxd  —  Remote Access Daemon            │
│  dev  ·  commit none  ·  built unknown    │
│  Vladimir Kovalev, OEM TECH               │
└──────────────────────────────────────────┘
```

> Note: the binary always renders this single fixed (wide) layout. Adaptive width for narrow
> terminals and color/styling are extension points and are not implemented yet.

### stdout vs stderr

`raxd` is deliberate about which channel carries which content, so pipes and redirects behave
predictably:

- **stdout** carries the machine-readable result: the `version` line, the `status` fields, the
  **key body** printed by `key create`, and the `key list` table.
- **stderr** carries the banner, all human-facing decoration (warnings, metadata, confirmations),
  and every `error:` / `hint:` message.

`raxd serve` is a special case: it is a long-running process and writes **everything** — the startup
block, the audit stream, the shutdown block, and any startup error — to **stderr**. Its **stdout is
always empty**. See [`raxd serve`](#raxd-serve) below.

The practical consequence for key management:

```
raxd key create --name prod > key.txt
```

writes **only** the key (inside its box frame) to `key.txt`; the banner, warning, and metadata
stay on the terminal because they go to stderr.

### Exit codes

| Outcome | Exit code |
|---------|-----------|
| `version`, `status` succeed | `0` |
| `key create` succeeds | `0` |
| `key list` (including an empty store) | `0` |
| `key delete` succeeds | `0` |
| `key create` validation/store error (e.g. label too long, corrupt store) | `1` |
| `key delete` with an unknown id, an already-revoked id, or a missing id argument | `1` |
| `serve` shuts down gracefully (SIGINT / SIGTERM) | `0` |
| `serve` startup error (port in use, no TLS-dir permission, corrupt cert, corrupt `keys.db`, invalid bind address, invalid `config.yaml`) | `1` |
| Stub command (`config port`) | `1` |
| `status` cannot determine `$HOME` | non-zero (error) |
| Unknown command or flag (cobra default) | non-zero |

### Error format

Error messages follow a consistent shape:

```
error: <what happened — one sentence, lowercase, no trailing period>
  hint: <what to do — one sentence>
```

Both `error:` and `hint:` are lowercase, and `hint:` lines are indented by two spaces. A single
error may carry more than one `hint:` line.

Unknown commands and unknown flags use cobra's default messages, which are acceptable at this
stage, for example:

```
Error: unknown command "statu" for "raxd"

Run 'raxd --help' for usage.
```

---

## Working commands

### `raxd version`

Print the raxd version, git commit, and build date.

- **Usage:** `raxd version`
- **Output channel:** stdout
- **Exit code:** `0`

The output is a single line, which is easy to parse in scripts:

```
raxd <version> (commit <commit>, built <date>)
```

On a development build (compiled without ldflags), the default values are used:

```
$ raxd version
raxd dev (commit none, built unknown)
```

A release build injects real values via ldflags (see [`development.md`](development.md)):

```
$ raxd version
raxd 1.0.0 (commit abc1234, built 2025-06-01)
```

The version is printed exactly as provided by the build metadata (no hard-coded `v` prefix), which
avoids producing `vdev` for development builds.

### `raxd status`

Display the current state of the raxd daemon and the filesystem paths used for configuration, key
storage, and TLS certificates.

- **Usage:** `raxd status`
- **Output channel:** stdout
- **Exit code:** `0`

The `state` field reports `not running`. `raxd status` reports the on-disk paths only; it does not
probe a running `serve` process, so the state is shown as `not running` even while a `raxd serve`
process is listening. The fields are printed as aligned `key   value` lines:

```
$ raxd status
  state    not running
  config   /home/user/.config/raxd/config.yaml
  keys     /home/user/.local/state/raxd/keys.db
  tls      /home/user/.local/state/raxd/tls
```

On macOS the canonical config path is the same as on Linux (`~/.config/raxd`, decision D3):

```
  state    not running
  config   /Users/alice/.config/raxd/config.yaml
  keys     /Users/alice/.local/state/raxd/keys.db
  tls      /Users/alice/.local/state/raxd/tls
```

If `config.yaml` does not exist, the path is still shown with an informational suffix — this is not
an error, and the exit code remains `0`:

```
  state    not running
  config   /home/user/.config/raxd/config.yaml  (not found, defaults applied)
  keys     /home/user/.local/state/raxd/keys.db
  tls      /home/user/.local/state/raxd/tls
```

The `keys` line shows the **path** to the key database (`keys.db`). It never prints the contents of
that file. The `tls` line shows the **path** to the TLS directory (`tls/`), where `raxd serve`
stores the certificate and private key. `status` never prints TLS contents, the configured port, or
any other secret — only the state string and the resolved paths.

**Error case — `$HOME` cannot be determined.** If the home directory cannot be resolved, `status`
prints an error with a hint to stderr and exits with a non-zero code:

```
error: cannot determine config directory: $HOME is not set
  hint: set the HOME environment variable and try again
```

---

## API keys (`raxd key`)

`raxd key` is a command group used to issue, list, and revoke the API keys that authenticate remote
access to `raxd`. It has no action of its own — run one of its sub-commands. Running `raxd key`
alone prints the group's help.

- **Short:** `Manage API keys`
- **Long:** Create, list, and delete API keys used to authenticate remote access.

> **Scope note.** Keys created here are now consumed over the network: `raxd serve` authenticates
> every connection against the same `keys.db`. A client presents the full key in the HTTP
> `Authorization: Bearer <key>` header (see [`raxd serve`](#raxd-serve)). What is still missing is
> what runs *behind* authentication — command execution, the MCP server, and file upload are not
> implemented yet (the server answers any route other than the health check with `501 Not
> Implemented`). See the README's "Coming next".

### How a key is stored (security model)

When you create a key, `raxd` shows you the full key **once** and then stores only what it needs to
verify a future presentation of that key:

- A per-key random salt and the SHA-256 hash of the key combined with that salt.
- Metadata: a random id, the optional label, the creation time, the last-used time, the revoked
  flag, and a short non-reversible fingerprint (used for audit logging).

The plaintext key itself is **never** written to disk, never logged, and never returned by any
command other than the one-time output of `key create`. The database file `keys.db` is created with
`0600` permissions. See [`configuration.md`](configuration.md#the-keysdb-key-database) for the path
and storage details.

### `raxd key create`

Generate a new API key for remote access authentication.

- **Usage:** `raxd key create [--name <label>]`
- **Flag:** `--name string` — optional human-readable label for the key (max 64 characters).
- **Output channels:** the **key body** goes to **stdout** (inside a box frame); the warning and
  metadata go to **stderr**.
- **Exit code:** `0` on success.

**The key is displayed exactly once and cannot be retrieved afterwards.** Store it securely the
moment it is shown. There is no command, flag, or file from which the full key can be read again.

The output channels are split on purpose. The full layout the user sees in the terminal interleaves
stderr (warning + metadata) and stdout (the key in its box):

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

The `id` shown here is the **full** 16-character key id. It is the id you pass to `raxd key delete`.
`raxd key list` now shows this **same** full id, so you can copy the id from either the creation
output or `key list`.

When `--name` is not provided, the label is shown as `-`:

```
  id        d7bc3a34da19d94e
  label     -
  created   2026-05-21
```

**Key format.** The key body is `rax_live_` followed by a base64url-encoded random value with no
padding. The random part is 32 bytes (256 bits) of cryptographically secure randomness, so the full
key is roughly 52 characters long (`rax_live_` plus 43 base64url characters). This is the exact
string a network client sends to `raxd serve` as `Authorization: Bearer rax_live_…`.

**Capturing only the key (scripts/CI).** Because the key body is the only thing on stdout, you can
redirect it to a file or capture it in a variable. The banner, warning, and metadata are on stderr:

```sh
# Write only the key (in its box frame) to a file:
raxd key create --name ci > key.txt

# Capture in a variable, suppressing all decoration:
KEY=$(raxd key create 2>/dev/null)
```

> Note: the box frame is part of the stdout output. It helps a human spot the key, but a script
> that needs the bare value must strip the frame characters. A `--raw` flag that would print the
> bare value is a possible future addition; it is **not** implemented today.

> **Audit line (stderr).** On the current build `key create` also writes a single audit record to
> **stderr** via `charmbracelet/log`, for example
> `INFO key created action=create id=d7bc3a34da19d94e fingerprint=…`. It carries only the id and a
> short, non-reversible fingerprint — **never** the key body. It is not shown in the mockup above to
> keep the example clean, and because it goes to stderr it does not affect a captured key
> (`> key.txt` or `$(... 2>/dev/null)`). This output format is not a stable interface and is likely
> to change when audit logging moves to a system journal in a later task.

**Errors.** A label longer than 64 characters is rejected before any key is generated:

```
error: label is too long (max 64 characters)
  hint: choose a shorter label and try again
```

If `keys.db` exists but cannot be read or parsed, `key create` reports a corrupt store and does not
overwrite the file:

```
error: key store is corrupted or unreadable
  hint: check file permissions on keys.db (must be readable by current user)
  hint: do not attempt to repair the file manually — contact support if data recovery is needed
```

These error cases exit with code `1`.

### `raxd key list`

Display a table of active API keys with their id, label, creation date, and last-used date.

- **Usage:** `raxd key list`
- **Output channel:** stdout (table or empty-state message)
- **Exit code:** `0`

The table has four columns — `ID`, `LABEL`, `CREATED`, `LAST USED` — rendered as a bordered box:

```
$ raxd key list
┌──────────────────┬────────────────┬────────────┬───────────┐
│ ID               │ LABEL          │ CREATED    │ LAST USED │
├──────────────────┼────────────────┼────────────┼───────────┤
│ d7bc3a34da19d94e │ production-key │ 2026-05-21 │ never     │
│ e4b550b565a232b6 │ staging        │ 2026-05-21 │ never     │
└──────────────────┴────────────────┴────────────┴───────────┘
```

Column rules:

- **ID** shows the **full** 16-character key id. It is the same id printed by `raxd key create`, and
  it can be passed directly to `raxd key delete`.
- **LABEL** is truncated to 20 characters with a trailing `…` if longer; a key without a label shows
  `-`.
- **CREATED** is `YYYY-MM-DD`.
- **LAST USED** is `YYYY-MM-DD`, or `never` if the key has not been used. Today keys typically show
  `never`: the network server records authentication in its audit stream (see [`raxd serve`](#raxd-serve)),
  but the per-key last-used timestamp is only persisted by `FlushUsage` on a graceful shutdown, and
  the health check is the only request that reaches a handler.

**Revoked keys are not shown.** A key that has been revoked with `key delete` disappears from this
list. There is no flag to include revoked keys; the record is retained only for audit purposes.

**`key list` never reveals a secret.** The table contains only id, label, created, and last-used.
It never prints the key body, the stored hash, the salt, the fingerprint, or the revoked flag.

**Empty store.** When there are no active keys, `key list` prints a friendly message (also on
stdout) and still exits with code `0`:

```
$ raxd key list
  No API keys found.
  hint: create your first key with "raxd key create --name <label>"
```

A missing `keys.db` is treated as an empty store, not an error — the same empty-state message is
shown.

### `raxd key delete`

Revoke the API key with the given id.

- **Usage:** `raxd key delete <id>`
- **Output channel:** stderr (confirmation or error)
- **Exit code:** `0` on success, `1` on error.

Deletion is a **soft revoke**: the record is marked revoked and retained for audit, but it is
immediately invalidated and will no longer appear in `key list`. The wording in the confirmation is
`revoked`, not `deleted`, to make this explicit:

```
$ raxd key delete d7bc3a34da19d94e
  key d7bc3a34da19d94e revoked
  hint: the key can no longer be used for authentication
```

Pass the **full** 16-character id shown by either `raxd key create` or `raxd key list` — both show
the full id, so you can copy it straight from the `key list` table. The id is a random identifier,
not derived from the key body, so it is safe to show in confirmations, errors, and the table.

> **Revocation takes effect immediately on the network.** A running `raxd serve` verifies every
> connection against the live `keys.db`, and `Verify` only considers active records. A key that you
> revoke stops authenticating on its very next request — there is no cache or restart delay.

> **Audit line (stderr).** Like `key create`, a successful `key delete` also writes a single audit
> record to **stderr** via `charmbracelet/log`, for example
> `INFO key revoked action=delete id=d7bc3a34da19d94e fingerprint=…`. As with create, it carries
> only the id and the non-reversible fingerprint — **never** the key body — and is omitted from the
> confirmation mockup above to keep it clean. This output format is not a stable interface and is
> likely to change when audit logging moves to a system journal in a later task.

**Errors** (all exit with code `1`):

A non-existent id:

```
error: key "d7bc3a34da19d94e" not found
  hint: run "raxd key list" to see available key IDs
```

An id that is already revoked:

```
error: key "d7bc3a34da19d94e" is already revoked
  hint: run "raxd key list" to see active keys
```

A missing id argument:

```
error: key delete requires an id argument
  hint: run "raxd key list" to find the key ID, then use "raxd key delete <id>"
```

### Security summary for key management

- The full key (`rax_live_…`) is printed **only once**, only by `key create`, and only on stdout.
- `key list`, error messages, confirmations, and audit logs never contain the key body, the stored
  hash, or the salt.
- `keys.db` stores `sha256(key + per-key-salt)` and the salt — never the plaintext key.
- The id shown in the table and in messages is a random identifier and does not reveal the key.
- A revoked key is invalidated immediately and never reappears in `key list`.

---

## `raxd serve`

Start `raxd` as a **foreground TLS server**.

- **Usage:** `raxd serve`
- **Output channel:** stderr only (the startup block, the audit stream, the shutdown block, and any
  error). **stdout is always empty.**
- **Exit code:** `0` on graceful shutdown (SIGINT / SIGTERM); `1` on a startup error.
- **Help text:**

  ```
  Start raxd as a foreground TLS server.

  The server listens on the configured address (default: 127.0.0.1:7822)
  with TLS 1.3. Every connection is authenticated with an API key before
  any request is processed.

  Configuration is read from ~/.config/raxd/config.yaml.
  For production use, register raxd as a system service instead.
  ```

`raxd serve` is a long-running process: it blocks after the startup block and keeps running until it
receives `SIGINT` (Ctrl+C) or `SIGTERM`. It takes **no flags or positional arguments** other than
`-h` / `--help`; everything is configured through `config.yaml` (see
[`configuration.md`](configuration.md#networking-and-serve-fields)).

> **Run it in Docker.** Like all of `raxd`, `serve` is built and run inside a container only
> (security baseline §6). It opens a network listener, so running it on the host is out of scope.
> See [`development.md`](development.md).

### What `serve` does (scope)

This is the first networked piece of `raxd`. In its current form `serve` provides exactly two
things: a **secure transport** and **per-connection authentication**.

- **TLS 1.3 transport.** The TCP listener is wrapped in `crypto/tls` with `MinVersion =
  tls.VersionTLS13`. A client that offers only TLS 1.2 or lower fails the handshake. TLS 1.3
  cipher suites are not configurable and are intentionally left at the Go defaults.
- **Self-signed certificate.** On the first run, `serve` generates a self-signed ECDSA P-256
  certificate (with SAN `127.0.0.1` and `localhost`) in the TLS directory
  (`~/.local/state/raxd/tls/`). The private key `key.pem` is written with `0600` permissions and
  the certificate `cert.pem` with `0644`. On later runs the existing pair is **reused** and never
  regenerated. There is no built-in trust anchor: clients must trust this self-signed certificate
  explicitly (or skip verification in a controlled local setup).
- **API-key authentication on every connection.** Every request is authenticated **before** any
  routing or handling. The client presents its key in the HTTP `Authorization: Bearer <key>`
  header — for example `Authorization: Bearer rax_live_…`. The key is checked against `keys.db` via
  the keystore's constant-time `Verify`. The key is **never** taken from a command-line argument or
  an environment variable.
- **Host / Origin checks, rate limiting, and an audit log** run as part of the same fixed
  middleware chain (described below).
- **One real operation: a health check.** After successful authentication, the only route that does
  real work is `GET /healthz`, which returns `pong`. Every other path returns `501 Not Implemented`.

**Out of scope for `serve` today (not implemented):**

- Command execution over the network (no shell, no `exec`).
- The MCP server and its tools/resources.
- File upload.
- mTLS / client certificates.
- Registering `raxd` as a systemd/launchd service (`serve` is foreground only — there is no
  `--daemon` mode and `raxd` does not install a service).

These are future tasks. The catch-all route exists precisely as the extension point where they will
attach; until then it answers `501`.

### The request pipeline

Every request passes through a fixed chain before it can reach a handler. A request is rejected at
the first stage it fails:

```
TLS 1.3 handshake
  → body-size limit (http.MaxBytesReader)
  → recover (panics → 500, server stays up)
  → Host / Origin validation        → 403 if rejected
  → authentication (Bearer → Verify) → 401 / 403 if rejected
  → rate limit (per-key + per-IP)    → 429 if exceeded
  → router:  GET /healthz → 200 pong
             anything else → 501 not implemented
```

The audit stream records exactly **one** record per request that reaches the audit-aware chain
(Host/Origin, auth, rate-limit, or the success path). The outermost layer — the body-size limit — is
the one exception: a `413` produced there is **not** audited (see the response-codes note below).

### Response codes

| Condition | HTTP status |
|-----------|-------------|
| No `Authorization` header / not `Bearer` / empty token | `401 Unauthorized` |
| Unknown, revoked, or otherwise unverifiable key (`Verify` returns "not found") | `401 Unauthorized` |
| Key store unreadable/corrupt at request time (`Verify` errors) | `403 Forbidden` |
| `Host` header not in the host allowlist | `403 Forbidden` |
| `Origin` header present and not in the origin allowlist | `403 Forbidden` |
| Per-key or per-IP rate limit exceeded | `429 Too Many Requests` |
| Request body larger than `max_body_bytes` | `413` (via `http.MaxBytesReader`) |
| Authenticated `GET /healthz` | `200 OK` (body `pong`) |
| Authenticated request to any other route | `501 Not Implemented` (body `not implemented`) |

> **The `413` from the body limit is not audited.** The body-size limit
> (`bodyLimitMiddleware`) is the **outermost** layer in the chain — it runs before the auth and
> audit middlewares. When a body exceeds `max_body_bytes`, the `413` is produced by the standard
> library's `http.MaxBytesReader` and the request never reaches the audit-aware chain, so **no**
> audit record (no `FAIL` / `DENY` / `RATE` line) is written for it. This is unlike `401`
> (`FAIL`), `403` (`DENY`), and `429` (`RATE`), which always emit exactly one audit line. In short:
> a `413` is silent in the audit stream — confirm an oversized request another way (for example by
> observing the `413` on the client) rather than by grepping the audit log.

For security, error responses carry an **empty body**: the server does not tell the client *why* a
request was rejected (whether a key is unknown vs. revoked, for instance). The reason is recorded
only in the server's own audit stream (below), and — as noted — the `413` case is not even recorded
there. See [`configuration.md`](configuration.md#networking-and-serve-fields) for the allowlists,
rate-limit, and body-size settings.

### Startup output

The startup block is printed **only after the TCP listener is successfully bound** — it is emitted
from an `OnListen` hook that `serve` registers in `internal/server`. If the start fails before the
bind succeeds (see [Startup errors](#startup-errors-exit-1) below), this block is **not** printed at
all.

On the **first run** (no certificate yet), `serve` generates the pair and prints, on stderr:

```
  cert      generated  /home/user/.local/state/raxd/tls/cert.pem
  key       generated  /home/user/.local/state/raxd/tls/key.pem  (0600)
  tls       TLS 1.3 only
  listening https://127.0.0.1:7822
  press Ctrl+C to stop

```

On **later runs** the existing certificate is loaded (`generated` becomes `loaded`, and the `(0600)`
note is omitted because the permissions are already set):

```
  cert      loaded  /home/user/.local/state/raxd/tls/cert.pem
  key       loaded  /home/user/.local/state/raxd/tls/key.pem
  tls       TLS 1.3 only
  listening https://127.0.0.1:7822
  press Ctrl+C to stop

```

If there are **no API keys** in `keys.db`, the server still starts (an empty store is a valid state),
but it warns that every connection will be rejected with `401`:

```
  cert      loaded  /home/user/.local/state/raxd/tls/cert.pem
  key       loaded  /home/user/.local/state/raxd/tls/key.pem
  tls       TLS 1.3 only
  listening https://127.0.0.1:7822
  warning   no API keys found — all connections will be rejected
  hint      create a key with "raxd key create --name <label>"
  press Ctrl+C to stop

```

### Audit stream

Once running, `serve` writes **one structured line per request** to stderr, using
`charmbracelet/log` in `key=value` form. Silence means health: there are no heartbeat lines. The
format is:

```
time=<UTC ISO-8601> level=<INFO|WARN> msg=<AUTH|FAIL|DENY|RATE> fp=<fingerprint> remote=<IP:port> [reason="<text>"]
```

- `fp` is the 12-hex-character key fingerprint (`keystore.Fingerprint`), or `-` when no key was
  identified. The **key body is never logged** — only the fingerprint.
- `remote` is the client `IP:port` (no DNS resolution).
- `reason` appears only on non-success lines.

| `msg` | level | When |
|-------|-------|------|
| `AUTH` | `INFO` | Request authenticated and passed all gates (reached a handler) |
| `FAIL` | `WARN` | No / invalid / unknown / revoked key (the `401` cases) |
| `DENY` | `WARN` | Corrupt key store (`403`), bad `Host` (`403`), or bad `Origin` (`403`) |
| `RATE` | `WARN` | Rate limit exceeded (`429`), per-key or per-IP |

> **The body-size `413` has no audit line.** The `413` returned when a request body exceeds
> `max_body_bytes` is generated by the outermost `http.MaxBytesReader` layer, which sits **before**
> the audit-aware middlewares. Unlike the `401` / `403` / `429` cases above, it does **not** produce
> a `FAIL`, `DENY`, or `RATE` record — there is no `msg` value for it. Do not expect an oversized
> request to show up in the audit stream.

Examples:

```
time=2026-05-21T14:32:01Z level=INFO msg=AUTH fp=a3f9c1d2e847 remote=127.0.0.1:54312
time=2026-05-21T14:32:05Z level=WARN msg=FAIL fp=- remote=127.0.0.1:54401 reason="no authorization header"
time=2026-05-21T14:32:07Z level=WARN msg=FAIL fp=b7d2a0c19f3e remote=127.0.0.1:54402 reason="authentication failed"
time=2026-05-21T14:32:09Z level=WARN msg=DENY fp=- remote=127.0.0.1:54403 reason="key store unavailable"
time=2026-05-21T14:32:11Z level=WARN msg=DENY fp=- remote=127.0.0.1:54404 reason="invalid host header"
time=2026-05-21T14:32:13Z level=WARN msg=DENY fp=- remote=127.0.0.1:54405 reason="invalid origin header"
time=2026-05-21T14:32:15Z level=WARN msg=RATE fp=a3f9c1d2e847 remote=127.0.0.1:54312 reason="rate limit exceeded (key)"
time=2026-05-21T14:32:16Z level=WARN msg=RATE fp=- remote=127.0.0.1:54500 reason="rate limit exceeded (ip)"
```

Because everything is on stderr, filtering works as expected:

```sh
# Capture all server output to a file (stdout stays empty):
raxd serve 2>server.log

# Watch only failures:
raxd serve 2>&1 | grep -E "FAIL|DENY|RATE"
```

### Calling the health check

The health check is the only working endpoint. It requires a valid key. Because the certificate is
self-signed, a client must trust it or skip verification — the example below uses `curl -k` for a
controlled local test:

```sh
# From inside the container running `raxd serve`, with KEY set to a created key:
curl -k -H "Authorization: Bearer $KEY" https://127.0.0.1:7822/healthz
# → pong
```

- Without the header you get `401` (and a `FAIL` audit line); the body is empty.
- With a valid key, `/healthz` returns `200` and the body `pong`.
- Any other path (for example `/exec`, `/mcp`) returns `501` with the body `not implemented`.

### Graceful shutdown

Press Ctrl+C (or send `SIGTERM`). `serve` stops accepting new connections, drains active ones,
flushes buffered key-usage data, and exits `0`:

```
^C
  shutting down  signal received
  draining       waiting for active connections to finish
  flushing       usage data flushed
  stopped

```

(The leading `^C` is printed by the terminal, not by `raxd`. Under `SIGTERM` it is absent and the
block begins with `shutting down`.)

The shutdown block is printed **only if the server actually started** — that is, only if the startup
block was printed after a successful bind. A run that failed to start (see below) prints neither the
startup nor the shutdown block.

### Startup errors (exit 1)

A startup error is printed in the standard `error:` / `hint:` format on stderr and the process exits
with code `1`.

> **No startup block on a failed start.** The startup block (`cert` / `key` / `tls` /
> `listening …` / `press Ctrl+C`) is printed **only after the TCP listener is successfully bound**,
> via an `OnListen` hook in `internal/server`. If the start fails for any reason — port already in
> use, no permission to create the TLS directory, a corrupt certificate, a corrupt `keys.db`, or a
> bad `config.yaml` — `serve` prints **only** the `error:` / `hint:` lines to stderr and exits `1`.
> Neither the startup block nor the shutdown block appears, so there is never a misleading
> `listening …` line for a server that did not start. This behaviour matches the cert/permission
> errors too: they are detected before the bind, so the startup block is never reached. See
> [`troubleshooting.md`](troubleshooting.md#raxd-serve) for the per-error details.

Port already in use:

```
error: cannot bind to 127.0.0.1:7822: address already in use
  hint: check what is using port 7822 with "lsof -i :7822" and stop it, or change the port with "raxd config port <PORT>"
```

> Note: `raxd config port` is still a stub and does not actually persist the port yet (see below).
> To change the port today, edit `port:` in `config.yaml` directly.

Cannot create the TLS directory (no write permission):

```
error: cannot create TLS directory: permission denied
  hint: check that the current user has write access to ~/.local/state/raxd/
```

Certificate generation failed (disk full / no write permission):

```
error: failed to generate TLS certificate
  hint: check available disk space and write permissions for /home/user/.local/state/raxd/tls/
```

Existing certificate or key is corrupt / unreadable (it is **never** overwritten automatically):

```
error: TLS certificate or key is corrupted or unreadable
  hint: remove the files in /home/user/.local/state/raxd/tls/ and run "raxd serve" again to regenerate
```

`keys.db` is corrupt or unreadable at startup:

```
error: key store is corrupted or unreadable
  hint: check file permissions on the keys.db path shown in "raxd status"
  hint: do not attempt to repair the file manually — contact support if data recovery is needed
```

**Configuration load failure (invalid bind address *or* invalid `config.yaml`).** Both kinds of
config-load failure are handled by a **single** error path in `serve`, and that path prints **one
generic hint** that references `bind_addr` / `config.yaml`. The `error:` line still reports what
actually went wrong (it carries the underlying message from `config.Load`), but the `hint:` line is
**not specialised per cause** — it always points you at the bind address in `config.yaml`.

For an invalid bind address the pair reads naturally, because the cause and the hint line up:

```
error: invalid bind address "0.0.0.256": not a valid IP address
  hint: set a valid address in config.yaml (field: bind_addr), for example "127.0.0.1" or "0.0.0.0"
```

For a malformed `config.yaml` you get the same generic hint even though the real problem is YAML
syntax, not the bind address — so **treat the hint as "fix your `config.yaml`", not literally "fix
`bind_addr`"**:

```
error: config file is not valid YAML: <parser detail>
  hint: set a valid address in config.yaml (field: bind_addr), for example "127.0.0.1" or "0.0.0.0"
```

In this YAML case the `bind_addr` reference is incidental: the actionable part is the `error:` line
(`config file is not valid YAML`), and the fix is to correct the YAML syntax in
`config.yaml`. See [`troubleshooting.md`](troubleshooting.md#error-config-file-is-not-valid-yaml).

### Security summary for `serve`

- TLS 1.3 is mandatory; TLS 1.2 and lower are rejected at the handshake.
- The private TLS key is `0600`; the certificate `0644`; the TLS directory `0700`.
- An existing certificate is reused and never silently overwritten.
- The default bind address is `127.0.0.1` (loopback only).
- Every connection is authenticated before any handler runs; the key is taken only from the
  `Authorization: Bearer` header, never from argv or the environment.
- Rejections return an empty body; the reason lives only in the audit stream (except the body-limit
  `413`, which is not audited at all).
- The audit stream logs the fingerprint, never the key body or the raw `Authorization` header.
- Rate limiting applies per-key and per-IP.
- The only operation behind authentication is the health check; everything else is `501`.

---

## Stub commands

The following command is part of the tree and has correct usage strings and help text, but its
logic is **not implemented yet**. It prints `error: <command>: not implemented yet` to **stderr**
and exits with code `1`.

### `raxd config`

Manage configuration. This is a command group; run one of its sub-commands. Running `raxd config`
alone prints the group's help.

- **Short:** `Manage configuration`
- **Long:** View and modify raxd configuration settings. Configuration is stored in
  `~/.config/raxd/config.yaml`.

#### `raxd config port`

Set the listening port.

- **Usage:** `raxd config port <PORT>`
- **Status:** stub — exit `1`.

```
$ raxd config port 8080
error: config port: not implemented yet
```

> The help text notes that the default port is `7822`. This command does **not** write anything to
> `config.yaml` yet — actually persisting the port is planned (see the README's "Coming next"). To
> change the port today, edit the `port:` key in `config.yaml` by hand; `raxd serve` reads it on the
> next start.

---

## Summary table

| Command | Channel | Exit 0 | Exit 1 | Status |
|---------|---------|--------|--------|--------|
| banner (every command except `--help`) | stderr | — | — | working |
| `raxd version` | stdout | yes | — | working |
| `raxd status` | stdout | yes | — | working |
| `raxd key create` | stdout (key) + stderr (decor) | yes | validation / store error | working |
| `raxd key list` | stdout | yes (incl. empty store) | — | working |
| `raxd key delete` | stderr | yes | not found / already revoked / missing id | working |
| `raxd serve` | stderr | graceful shutdown | startup error (port/cert/db/bind/config) | working |
| `raxd config port` | stderr | — | yes | stub |

See also: [`configuration.md`](configuration.md) for paths, `keys.db`, `config.yaml`, and the
networking/`serve` fields; [`development.md`](development.md) for building and testing in Docker;
[`troubleshooting.md`](troubleshooting.md) for common `serve` problems.
