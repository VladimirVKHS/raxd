# Command reference

This document describes every command in the `raxd` command tree **as it exists in the code
today**. The service commands (`version`, `status`), the API-key commands
(`key create`, `key list`, `key delete`), the network server (`serve`), and the system-service group
(`service install` / `uninstall` / `start` / `stop` / `status`) are fully working. The
one remaining feature command, `config port`, is present as an honest stub that reports
`not implemented yet`.

All CLI text (usage strings, messages, the banner, errors) is in English.

> Where to run these commands: per the security baseline, `raxd` is built and run **inside Docker
> only**. This applies in particular to `raxd serve`, which opens a TLS listener and (through the MCP
> `execute_command` and `upload_file` tools) runs commands and writes files on the host. The
> `raxd service` integration is likewise exercised inside Docker (a privileged systemd container);
> the macOS launchd path is verified on a real macOS host, not in a container (see
> [`service-management.md`](service-management.md#5-the-macos-path-is-not-tested-in-docker)).
> Examples below show the command and its output; for how to actually invoke them in a container, see
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
├── service            Manage raxd as a system service
│   ├── install        Register the service + autostart    (working)
│   ├── uninstall      Remove the service registration     (working)
│   ├── start          Start the service                   (working)
│   ├── stop           Stop the service                    (working)
│   └── status         Show the system-service status      (working)
├── config             Manage configuration
│   └── port           Set the listening port              (stub)
└── serve              Start the raxd TLS server           (working)
```

`raxd --help` lists the root command and all sub-commands. Each command also responds to
`raxd <command> --help` with its own usage and description.

> **`raxd status` and `raxd service status` are different commands.** `raxd status` shows the on-disk
> paths and the foreground daemon state; `raxd service status` shows whether the daemon is registered
> and running as a **system service** (installed, active, PID, EUID, autostart, unit path). See
> [`raxd status`](#raxd-status) and [`raxd service status`](#raxd-service-status).

> **MCP is not a CLI command.** The MCP server is not a separate command — it is hosted by
> `raxd serve` on the `/mcp` route. To use it, run `raxd serve` and connect an MCP client to
> `https://127.0.0.1:<port>/mcp`. See [`mcp.md`](mcp.md) and [`raxd serve`](#raxd-serve) below.
> Command execution and file upload are **not** CLI sub-commands either: they are the MCP
> `execute_command` and `upload_file` tools, run by an MCP client over `/mcp`.

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
  **key body** printed by `key create`, the `key list` table, and the `raxd service status` block
  (including its `--json` form).
- **stderr** carries the banner, all human-facing decoration (warnings, metadata, confirmations),
  and every `error:` / `hint:` message. The mutating service commands
  (`service install` / `uninstall` / `start` / `stop`) write their success blocks to **stderr** too.

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
| `service install` succeeds, or the service is **already installed** | `0` |
| `service uninstall` succeeds, or the service is **not installed** | `0` |
| `service start` / `stop` succeed | `0` |
| `service status` (any state, including not installed) | `0` |
| `service install` / `uninstall` / `start` / `stop` without root, or the manager is unavailable/unsupported | `1` |
| `service start` / `stop` when the service is **not installed** | `1` |
| `serve` shuts down gracefully (SIGINT / SIGTERM) | `0` |
| `serve` startup error (port in use, no TLS-dir permission, corrupt cert, corrupt `keys.db`, invalid bind address, invalid `config.yaml`, cannot create upload root) | `1` |
| Stub command (`config port`) | `1` |
| `status` cannot determine `$HOME` | non-zero (error) |
| Unknown command or flag (cobra default) | non-zero |

> **`service` idempotency: the same "not present" sentinel maps to different exit codes by command.**
> Re-installing an installed service, or uninstalling an absent one, is a **success** (exit `0`) — the
> requested end state already holds. But starting or stopping an **absent** service is an error
> (exit `1`) — there is nothing to act on. See the per-command sections below.

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

A release build injects real values via ldflags (see [`development.md`](development.md)). The version
is whatever was passed in `VERSION` at build time — typically a `v`-prefixed git tag from
`git describe --tags`, for example:

```
$ raxd version
raxd v0.1.0 (commit abc1234, built 2026-05-22)
```

The version is printed exactly as provided by the build metadata — raxd never adds or strips a `v`
prefix itself (so a tag like `v0.1.0` shows as `v0.1.0`, and the default development build shows
`dev`, never `vdev`).

> The same version string is what the MCP `server_info` tool reports as its `version` field (see
> [`mcp.md`](mcp.md#server_info)).

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
any other secret — only the state string and the resolved paths. (The upload root, default
`<state directory>/uploads`, is created by `raxd serve`, not listed by `status`.)

> `raxd status` reports the **interactive** paths (`~/.config/raxd`, `~/.local/state/raxd`) for the
> current user. When `raxd` runs as a registered **system service**, the daemon uses system paths
> instead (`/etc/raxd`, `/var/lib/raxd` on Linux), set through the unit/plist environment — see
> [`raxd service status`](#raxd-service-status) and
> [`configuration.md`](configuration.md#service-layout-system-service).

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

> **Scope note.** Keys created here are consumed over the network: `raxd serve` authenticates every
> connection against the same `keys.db`. A client presents the full key in the HTTP
> `Authorization: Bearer <key>` header (see [`raxd serve`](#raxd-serve)). The **same** key
> authenticates the **MCP server** on the `/mcp` route (see [`mcp.md`](mcp.md)), including the
> `execute_command` and `upload_file` tools. Treat any key that can reach those tools like an SSH
> private key — it grants remote command execution and file writes on the host.

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
string a network client sends to `raxd serve` as `Authorization: Bearer rax_live_…` — including an
MCP client connecting to `/mcp` (see [`mcp.md`](mcp.md#connection-parameters)).

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
  but the per-key last-used timestamp is only persisted by `FlushUsage` on a graceful shutdown.

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
> revoke stops authenticating on its very next request — there is no cache or restart delay. This
> applies to MCP requests on `/mcp` too: a revoked key gets `401` before any tool runs (so it can no
> longer run `execute_command` or `upload_file`).

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

## System service (`raxd service`)

`raxd service` is a command group for registering and managing `raxd` as a **managed OS service** —
a systemd unit on Linux, a launchd daemon on macOS. It has no action of its own; run one of its five
sub-commands. Running `raxd service` alone prints the group's help.

- **Short:** `Manage raxd as a system service`
- **Long:**

  ```
  Register, start, stop, and monitor raxd as a managed OS service.

  On Linux, raxd uses systemd. On macOS, it uses launchd.
  The service runs under the unprivileged user "raxd" (not root).

  Installation requires root/sudo. The daemon itself always runs as a non-root user.
  ```

The five sub-commands are **install**, **uninstall**, **start**, **stop**, and **status**. The
contract is the same on both platforms — the platform-specific details (systemd vs launchd, the unit
vs the plist) are hidden inside.

> **Install needs root; the daemon does not.** `install`, `uninstall`, `start`, and `stop` write to
> system directories and call the service manager, so they require root (run them with `sudo`). They
> do **not** silently fall back to anything if you lack privileges — they print
> `insufficient privileges` and exit `1`. The **running daemon**, however, is configured with
> `User=raxd` (Linux) / `UserName=raxd` (macOS), so it runs as the unprivileged `raxd` user. `status`
> does not require root.

> **Read the [service management guide](service-management.md) before installing on a real host.** It
> covers the non-root model, the privileged-port capability, what `uninstall` keeps, log rotation,
> the restart policy, and the macOS verification limitation. The on-disk layout (paths, permissions)
> is in [`configuration.md`](configuration.md#service-layout-system-service).

### What `install` sets up

`raxd service install` (root required) creates the full service registration. On Linux it:

- creates the system user `raxd` if it does not exist
  (`useradd --system --no-create-home --shell /usr/sbin/nologin`; an already-existing user is reused);
- writes the systemd unit `/etc/systemd/system/raxd.service` (`root:root`, `0644`) with
  `User=raxd`, `Restart=on-failure`, an explicit `StateDirectoryMode=0700` /
  `ConfigurationDirectoryMode=0700`, and the journal output;
- writes a journald drop-in `/etc/systemd/journald.conf.d/raxd.conf` that caps the journal size;
- runs `systemctl daemon-reload` and `systemctl enable raxd` (autostart at boot).

On macOS it writes the plist `/Library/LaunchDaemons/tech.oem.raxd.plist`, creates the state/log/config
directories (`0700`, owned by `raxd`), and `launchctl bootstrap` + `enable`s the job.

`install` does **not** start the service — start it with `raxd service start` afterwards. The default
listening port is `7822`, which is unprivileged, so no special capability is needed; for a privileged
port (`< 1024`) the unit gains `CAP_NET_BIND_SERVICE` only (see
[`service-management.md`](service-management.md#2-privileged-ports--1024-and-the-network-capability)).

### `raxd service install`

Register `raxd` with the OS service manager and enable autostart.

- **Usage:** `sudo raxd service install`
- **Output channel:** stderr (success block or `error:` / `hint:`)
- **Exit code:** `0` on success **or** when the service is already installed; `1` on error.

On success it prints the success block to stderr (Linux):

```
  installed     raxd service
  unit          /etc/systemd/system/raxd.service
  drop-in       /etc/systemd/journald.conf.d/raxd.conf
  user          raxd  [not root]
  port          7822
  autostart     enabled
  hint: start the service now with "raxd service start"
```

On macOS the block omits the `drop-in` line and shows the plist path:

```
  installed     raxd service
  unit          /Library/LaunchDaemons/tech.oem.raxd.plist
  user          raxd  [not root]
  port          7822
  autostart     enabled
  hint: start the service now with "raxd service start"
```

The `port` line reflects the port resolved from `config.yaml` (the same value `serve` would bind);
with no config file it shows the default `7822`. After the block, an audit line is written to stderr
(`level=info msg="service installed" action=install platform=… unit=… user=raxd port=…`).

**Already installed (exit 0).** Re-running `install` on an installed service does **not** create a
duplicate and does **not** error — it reports the state and exits `0`:

```
  already installed   raxd service
  hint: use "raxd service status" to check the current state
```

**Errors** (exit `1`). Without root:

```
error: insufficient privileges to install the service
  hint: run as root or with sudo: sudo raxd service install
  hint: installation requires root to write system service files
```

When the service manager cannot be reached (no `systemctl` / `launchctl`):

```
error: service manager is not available
  hint: ensure systemd (Linux) or launchd (macOS) is running
```

On any other registration failure the partially created files are rolled back (the unit/drop-in
created in that run are removed; the `raxd` user is intentionally kept), and a neutral error is
printed — never a raw `systemctl` trace and never a secret. The exact wording depends on the failing
step, but it always follows the `error:` / `hint:` shape and exits `1`.

### `raxd service uninstall`

Remove the service registration and disable autostart.

- **Usage:** `sudo raxd service uninstall`
- **Output channel:** stderr (success block or `error:` / `hint:`)
- **Exit code:** `0` on success **or** when the service is not installed; `1` on error.

On success it stops and disables the service, removes the unit (and, on Linux, the journald drop-in),
and prints (Linux):

```
  uninstalled   raxd service
  removed       unit file and autostart registration
  removed       journal size limit drop-in
  kept          system user "raxd" (no shell, no home, not running)
  hint: to also remove the user: sudo userdel raxd
  hint: data in /var/lib/raxd is preserved — remove manually if no longer needed
```

On macOS the block has no `drop-in` line, the user-removal hint uses `dscl`, and the data hint shows
the macOS state directory:

```
  uninstalled   raxd service
  removed       plist file and autostart registration
  kept          system user "raxd" (no shell, no home, not running)
  hint: to also remove the user: sudo dscl . -delete /Users/raxd
  hint: data in /usr/local/var/raxd is preserved — remove manually if no longer needed
```

The `kept` line is deliberate: the `raxd` user (no login shell, no home, no longer running) is
**intentionally not deleted**, because removing a system user is riskier than keeping an inert one
(UID-reuse). Remove it yourself only if you need a zero-footprint cleanup — see
[`service-management.md`](service-management.md#3-the-raxd-user-is-kept-after-uninstall).

> **The data hint shows the real, platform-specific state directory.** The `data in … is preserved`
> hint prints the actual state directory for the current platform — `/var/lib/raxd` on Linux,
> `/usr/local/var/raxd` on macOS. The state directory (and `keys.db` inside it) is **not** removed by
> `uninstall` on either platform.

**Not installed (exit 0).** Uninstalling an absent service is a no-op success, not an error:

```
  not installed   raxd service
  hint: use "raxd service install" to set up the service
```

**Errors** (exit `1`). Without root, the same `insufficient privileges` error as `install`. If the
manager is unavailable, the `service manager is not available` error.

### `raxd service start`

Start the registered service.

- **Usage:** `sudo raxd service start`
- **Output channel:** stderr (success block or `error:` / `hint:`)
- **Exit code:** `0` on success; `1` if the service is not installed or the start fails.

On success it prints (the `pid` line appears once the manager reports a main PID):

```
  started       raxd service
  pid           1234
  hint: check status with "raxd service status"
```

**Not installed (exit 1).** Unlike `uninstall`, starting an **absent** service is an error — there is
nothing to start:

```
error: raxd service is not installed
  hint: install it first with "raxd service install"
```

Without root you get the `insufficient privileges` error; if the manager is unavailable, the
`service manager is not available` error. A start that the manager rejects produces a neutral failure
(no raw manager output) and exits `1`.

### `raxd service stop`

Stop the running service. The stop sends `SIGTERM`, which `raxd serve` handles as a **graceful
shutdown** (drain connections, flush usage, exit `0`). Because that is a clean exit, the manager does
**not** restart the service — it stays stopped until you start it again.

- **Usage:** `sudo raxd service stop`
- **Output channel:** stderr (success block or `error:` / `hint:`)
- **Exit code:** `0` on success; `1` if the service is not installed or the stop fails.

On success:

```
  stopped       raxd service
  hint: start again with "raxd service start"
```

**Not installed (exit 1).** Like `start`, stopping an absent service is an error:

```
error: raxd service is not installed
  hint: install it first with "raxd service install"
```

Without root you get the `insufficient privileges` error; an unavailable manager gives the
`service manager is not available` error.

> **Restart on failure vs. stop.** `Restart=on-failure` (Linux) / `KeepAlive.SuccessfulExit=false`
> (macOS) means the service is brought back up after a **crash** (non-zero exit / `kill -9`) but
> **not** after a graceful `stop`. See
> [`service-management.md`](service-management.md#6-restart-on-failure-vs-graceful-stop).

### `raxd service status`

Show whether the service is installed and running.

- **Usage:** `raxd service status` (add `--json` for machine-readable output)
- **Flag:** `--json` — print the status as JSON instead of the human-readable block.
- **Output channel:** **stdout** (the status block and the JSON form). The banner and any
  `error:` / `hint:` lines still go to stderr.
- **Exit code:** `0` — always, including when the service is not installed (a status query is not an
  error).

`status` is a query: it does **not** require root and does not mutate anything. Its output goes to
**stdout** so it can be piped, mirroring `raxd status`.

Installed and running (Linux):

```
$ raxd service status
  installed    yes
  running      yes
  pid          1234
  euid         999
  user         raxd  [not root]
  port         7822
  autostart    enabled
  unit         /etc/systemd/system/raxd.service
  manager      systemd
  state        active (running)
```

The `euid` line is read from `/proc/<pid>/status` of the running daemon (Linux) and proves the
non-root guarantee: a non-zero `euid` (here `999`) means the daemon is **not** root.

> **macOS: no `euid` line.** The effective UID is not readable without `/proc`, so on macOS the
> `euid` field is `0` and the line is omitted (it is printed only when `euid > 0`). The non-root
> guarantee on macOS rests on `UserName=raxd` in the plist; verify it on a real macOS host (see
> [`service-management.md`](service-management.md#5-the-macos-path-is-not-tested-in-docker)).

Installed but stopped (the `pid` shows `-`, the `euid` line is absent, and a `hint:` to start is
printed as part of the stdout block):

```
  installed    yes
  running      no
  pid          -
  user         raxd  [not root]
  port         7822
  autostart    enabled
  unit         /etc/systemd/system/raxd.service
  manager      systemd
  state        inactive (dead)
  hint: start with "raxd service start"
```

Not installed (still exit `0`):

```
  installed    no
  hint: install with "raxd service install"
```

#### `raxd service status --json`

With `--json`, the status is printed as JSON to **stdout** (the human block is not printed). The
banner still goes to stderr and does not pollute the parsed output.

```json
{
  "installed": true,
  "active": true,
  "pid": 1234,
  "euid": 999,
  "user": "raxd",
  "port": 7822,
  "autostart": "enabled",
  "unit_path": "/etc/systemd/system/raxd.service",
  "state": "active (running)",
  "manager": "systemd"
}
```

When not installed, the boolean fields are `false`, `user` is empty, `autostart` is `disabled`, and
the numeric fields are `0`:

```json
{
  "installed": false,
  "active": false,
  "pid": 0,
  "euid": 0,
  "user": "",
  "port": 0,
  "autostart": "disabled",
  "unit_path": "/etc/systemd/system/raxd.service",
  "state": "not installed",
  "manager": "systemd"
}
```

> `unit_path` and `manager` reflect the current platform (the systemd unit path / `systemd` on Linux,
> the plist path / `launchd` on macOS) regardless of installation state — they describe where the
> registration *would* live.

### Security summary for the service

- The daemon runs as the unprivileged `raxd` user (`euid != 0`); only `install` / `uninstall` /
  `start` / `stop` need root, and they fail cleanly without it (never a silent root daemon).
- The default port `7822` needs no capability; a privileged port (`< 1024`) grants only
  `CAP_NET_BIND_SERVICE`, never full root or setuid-root.
- The unit/plist/drop-in are `root:root` (`root:wheel` on macOS) `0644` — the `raxd` user cannot
  rewrite its own service definition.
- The audit log is capped via a journald drop-in (`SystemMaxUse` / `SystemMaxFileSize`); the cap is
  per-host, with logrotate as a documented fallback.
- `uninstall` removes the registration, autostart, capability, and drop-in, but **keeps** the inert
  `raxd` user and the state directory.
- `status` never prints a secret — only the state, paths, PID, EUID, port, and user name.
- Template inputs are validated before render and the manager is invoked without a shell; raw manager
  stderr is never shown to the user.

See [`service-management.md`](service-management.md) for the full security model and
[`configuration.md`](configuration.md#service-layout-system-service) for the paths and permissions.

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
> (security baseline §6). It opens a network listener **and** can run commands and write files on the
> host via the MCP `execute_command` and `upload_file` tools, so running it on the host is out of
> scope. See [`development.md`](development.md).

> **For production, register it as a service.** `serve` is foreground only — it has no `--daemon`
> mode. To run `raxd` as a managed service with autostart and restart-on-failure, use
> [`raxd service install`](#raxd-service-install) (which sets `raxd serve` as its `ExecStart` /
> `ProgramArguments`), then `raxd service start`.

### What `serve` does (scope)

`serve` is the networked core of `raxd`. It provides a **secure transport**, **per-connection
authentication**, and, behind that transport, two working endpoints: a **health check** and the
**MCP server**.

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
- **Upload root.** On startup `serve` resolves the upload root for the `upload_file` tool (the
  configured `upload.root`, or the default `<state directory>/uploads`) and creates it with `0700`
  permissions. A failure here is a startup error (see below).
- **Two operations behind authentication:**
  - **Health check** — `GET /healthz` returns `pong`.
  - **MCP server** — the `/mcp` route serves the Model Context Protocol over Streamable HTTP, behind
    the same authentication, `Host`/`Origin` checks, rate limiting, and audit. It exposes four
    tools: two read-only (`ping`, `server_info`), **`execute_command`** (runs a command on the host
    — no shell, mandatory timeout, optional allowlist, output/argument limits, controlled
    `cwd`/environment, per-call audit), and **`upload_file`** (writes one regular file into the
    upload root — `os.Root`-confined, size-limited, controlled mode, no-overwrite default, atomic
    write, per-call audit). See [`mcp.md`](mcp.md) for the full integration guide,
    [`execute-command-security.md`](execute-command-security.md) and
    [`file-upload-security.md`](file-upload-security.md) for the security warnings.

Every **other** path still returns `501 Not Implemented`.

> **Command execution and file upload live behind `/mcp`, not behind separate routes.**
> `execute_command` and `upload_file` are MCP tools, reached by a JSON-RPC `tools/call` on `/mcp` —
> there is no `/exec` or `/upload` HTTP endpoint and no `raxd exec` / `raxd upload` CLI sub-command. A
> request to `/exec`, `/upload`, or any other unimplemented path still answers `501`.

**Out of scope for `serve` today (not implemented):**

- File **download** / host filesystem read / file deletion (`upload_file` is upload-only).
- MCP tools beyond `ping` / `server_info` / `execute_command` / `upload_file`, and MCP Resources /
  Prompts.
- Interactive / PTY command sessions and real-time output streaming (`execute_command` is
  non-interactive and returns output in full after the command finishes).
- Chunked / streaming / resumable upload of files larger than the body limit (`upload_file` ships one
  whole file per request).
- Command sandboxing (cgroups/rlimits/seccomp/namespaces) — isolation relies on a non-root user
  inside a container.
- mTLS / client certificates.

> **Registering `raxd` as a service is a separate command, not a `serve` flag.** `serve` itself is
> foreground only and has no `--daemon` mode. Service registration (systemd/launchd) is done by the
> [`raxd service`](#system-service-raxd-service) group, which wraps `raxd serve` as the service's
> command. The `serve` process and the `service` commands cooperate: the unit/plist runs
> `raxd serve`, and `raxd service stop` signals it for a graceful shutdown.

The catch-all route remains an extension point for future tools; until then any route other than
`/healthz` and `/mcp` answers `501`.

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
             /mcp         → MCP server (Streamable HTTP)
             anything else → 501 not implemented
```

The MCP server sits **behind** the entire chain: a request to `/mcp` must pass Host/Origin, auth, and
rate-limit just like any other, and only then reaches the MCP handler. This applies to
`execute_command` and `upload_file` too — an unauthenticated or rate-limited call never runs a command
and never writes a file.

The audit stream records exactly **one** record per request that reaches the audit-aware chain
(Host/Origin, auth, rate-limit, or the success path), plus — for a `/mcp` tool call — one additional
MCP record written by the tool layer (see [Audit stream](#audit-stream)). The outermost layer — the
body-size limit — is the one exception: a `413` produced there is **not** audited (see the
response-codes note below).

### Response codes

| Condition | HTTP status |
|-----------|-------------|
| No `Authorization` header / not `Bearer` / empty token | `401 Unauthorized` |
| Unknown, revoked, or otherwise unverifiable key (`Verify` returns "not found") | `401 Unauthorized` |
| Key store unreadable/corrupt at request time (`Verify` errors) | `403 Forbidden` |
| `Host` header not in the host allowlist | `403 Forbidden` |
| `Origin` header present and not in the origin allowlist | `403 Forbidden` |
| Per-key or per-IP rate limit exceeded | `429 Too Many Requests` |
| Request body larger than `max_body_bytes` | `413` (via `http.MaxBytesReader`); **but** an oversized `upload_file` body on `/mcp` surfaces as `400` ("failed to read body") from the MCP SDK — see [`mcp.md`](mcp.md#upload_file) |
| Authenticated `GET /healthz` | `200 OK` (body `pong`) |
| Authenticated `POST /mcp` (valid JSON-RPC) | `200 OK` (JSON-RPC response) |
| Authenticated `GET /mcp` (no SSE stream offered) | `405 Method Not Allowed` |
| Authenticated request to any other route | `501 Not Implemented` (body `not implemented`) |

> **MCP protocol errors are JSON-RPC, not HTTP status codes.** Inside an authenticated `POST /mcp`,
> a malformed body or an unknown tool name is reported as a JSON-RPC error (`-32700` / `-32600` /
> `-32601` / `-32602`) with HTTP `200`, not as a `4xx`/`501`. An `execute_command` or `upload_file`
> tool error (allowlist deny, missing binary, limits, `deny_root`, traversal, too-large, bad mode) is
> reported as `isError: true` **inside** the JSON-RPC `result`, also with HTTP `200`. See
> [`mcp.md`](mcp.md#behaviour-and-error-handling).

> **The `413`/`400` from the body limit is not audited.** The body-size limit
> (`bodyLimitMiddleware`) is the **outermost** layer in the chain — it runs before the auth and
> audit middlewares. When a body exceeds `max_body_bytes`, the rejection is produced by the standard
> library's `http.MaxBytesReader` (surfacing as `413` on a plain route, or `400` "failed to read
> body" from the MCP SDK on an oversized `upload_file` request) and the request never reaches the
> audit-aware chain, so **no** audit record (no `FAIL` / `DENY` / `RATE` line) is written for it. This
> is unlike `401` (`FAIL`), `403` (`DENY`), and `429` (`RATE`), which always emit exactly one audit
> line. In short: an oversized body is silent in the audit stream — confirm it another way (for
> example by observing the response code on the client) rather than by grepping the audit log.

For security, error responses carry an **empty body**: the server does not tell the client *why* a
request was rejected (whether a key is unknown vs. revoked, for instance). The reason is recorded
only in the server's own audit stream (below), and — as noted — the body-limit case is not even
recorded there. See [`configuration.md`](configuration.md#networking-and-serve-fields) for the
allowlists, rate-limit, and body-size settings.

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

> The `listening https://127.0.0.1:7822` address is also the base for the MCP endpoint: connect an
> MCP client to `https://127.0.0.1:7822/mcp` (see [`mcp.md`](mcp.md#connection-parameters)).

### Audit stream

Once running, `serve` writes structured lines to stderr, using `charmbracelet/log` in `key=value`
form. Silence means health: there are no heartbeat lines.

**Connection records** — one per request that reaches the audit-aware chain:

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

**MCP records** — one additional line per `/mcp` tool call (`tools/call`), written by the MCP layer.
For the read-only tools (`ping`, `server_info`):

```
time=<UTC ISO-8601> level=INFO msg=MCP fp=<fingerprint> remote=<IP:port> tool=<name> result=ok
```

For **`execute_command`**, the tool writes its own record, carrying the command, arguments, exit
code, and duration (the command-specific fields appear only when `tool=execute_command`):

```
time=<UTC ISO-8601> level=INFO msg=MCP  fp=<fingerprint> remote=<IP:port> tool=execute_command result=ok command=<bin> args=[…] exit_code=<n> duration=<d> timed_out=<bool>
time=<UTC ISO-8601> level=WARN msg=DENY fp=<fingerprint> remote=<IP:port> tool=execute_command reason=<text> command=<bin> args=[…]
time=<UTC ISO-8601> level=WARN msg=FAIL fp=<fingerprint> remote=<IP:port> tool=execute_command reason=<text> command=<bin> args=[…]
time=<UTC ISO-8601> level=WARN msg=WARN fp=<fingerprint> remote=<IP:port> tool=execute_command reason=running-as-root command=<bin> args=[…]
```

For **`upload_file`**, the tool also writes its own record, carrying the destination path and the
size (the upload-specific fields appear only when `tool=upload_file`):

```
time=<UTC ISO-8601> level=INFO msg=MCP  fp=<fingerprint> remote=<IP:port> tool=upload_file result=ok path=<rel> size=<n>
time=<UTC ISO-8601> level=WARN msg=DENY fp=<fingerprint> remote=<IP:port> tool=upload_file reason=<text> [path=<rel>]
time=<UTC ISO-8601> level=WARN msg=FAIL fp=<fingerprint> remote=<IP:port> tool=upload_file reason=<text> [path=<rel>]
time=<UTC ISO-8601> level=WARN msg=WARN fp=<fingerprint> remote=<IP:port> tool=upload_file reason=running-as-root… [path=<rel>]
```

- `tool` is the tool name (`ping`, `server_info`, `execute_command`, or `upload_file`). The `tool=`
  field appears **only** on `MCP`/`DENY`/`FAIL`/`WARN` records that come from the tool layer; the
  connection records (`AUTH`/`FAIL`/`DENY`/`RATE`) for transport rejections never carry it.
- For `execute_command`, a successful call (any exit code, including a timeout) is `msg=MCP
  result=ok`; a rejected call (allowlist, limits, `deny_root`) is `msg=DENY`; a call that could not
  start (missing binary, bad `cwd`) is `msg=FAIL`; and an extra `msg=WARN reason=running-as-root`
  record is written on **every** call when the daemon is root.
- For `upload_file`, a successful write is `msg=MCP result=ok path= size=`; a control rejection
  (traversal, exists, is-a-directory, too-large, bad base64, bad mode, `deny_root`) is `msg=DENY`; an
  I/O failure is `msg=FAIL`; and an extra `msg=WARN reason=running-as-root` record is written on
  **every** call when the daemon is root. `size=` (a plain integer) appears only on the success
  record; `path=` is the **relative** path, never an absolute host path.
- Same `fp` and `remote` as the `AUTH` line for the same request — the key body is never logged.

> **`execute_command` arguments and the `upload_file` destination path are logged verbatim**
> (`args=[…]` / `path=`), with no masking. **Do not put secrets in `args` or in `path`.** (The
> `upload_file` file **content** is never logged.) See
> [`execute-command-security.md`](execute-command-security.md#1-do-not-pass-secrets-in-command-arguments-argv)
> and [`file-upload-security.md`](file-upload-security.md#2-do-not-put-secrets-in-the-destination-path).

So one authenticated `tools/call` produces **two** lines (the `AUTH` connection record and the tool
record) — or three when the daemon is root (the extra `WARN`). See [`mcp.md`](mcp.md#audit) for the
MCP audit details.

> **Audit-log rotation under the service.** This audit stream is on stderr. When `raxd` runs as a
> registered **system service** on Linux, that stderr goes to journald and its growth is capped by the
> journald drop-in `install` writes (`SystemMaxUse` / `SystemMaxFileSize`). See
> [`service-management.md`](service-management.md#4-audit-log-rotation).

> **The body-size `413`/`400` has no audit line.** The rejection returned when a request body exceeds
> `max_body_bytes` is generated by the outermost `http.MaxBytesReader` layer, which sits **before**
> the audit-aware middlewares. Unlike the `401` / `403` / `429` cases above, it does **not** produce
> a `FAIL`, `DENY`, or `RATE` record — there is no `msg` value for it. Do not expect an oversized
> request to show up in the audit stream.

Examples:

```
time=2026-05-21T14:32:01Z level=INFO msg=AUTH fp=a3f9c1d2e847 remote=127.0.0.1:54312
time=2026-05-21T14:32:01Z level=INFO msg=MCP  fp=a3f9c1d2e847 remote=127.0.0.1:54312 tool=ping result=ok
time=2026-05-21T14:32:02Z level=INFO msg=MCP  fp=a3f9c1d2e847 remote=127.0.0.1:54312 tool=execute_command result=ok command=ls args=[-la] exit_code=0 duration=3ms timed_out=false
time=2026-05-21T14:32:03Z level=INFO msg=MCP  fp=a3f9c1d2e847 remote=127.0.0.1:54312 tool=upload_file result=ok path=notes/hello.txt size=6
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

# Watch only MCP tool calls:
raxd serve 2>&1 | grep "msg=MCP"

# Watch only command execution:
raxd serve 2>&1 | grep "tool=execute_command"

# Watch only file uploads:
raxd serve 2>&1 | grep "tool=upload_file"
```

### Calling the endpoints

Both working endpoints require a valid key. Because the certificate is self-signed, a client must
trust it or skip verification — the examples below use `curl -k` for a controlled local test.

**Health check** (`GET /healthz`):

```sh
# From inside the container running `raxd serve`, with KEY set to a created key:
curl -k -H "Authorization: Bearer $KEY" https://127.0.0.1:7822/healthz
# → pong
```

- Without the header you get `401` (and a `FAIL` audit line); the body is empty.
- With a valid key, `/healthz` returns `200` and the body `pong`.

**MCP server** (`POST /mcp`): a JSON-RPC `initialize`/`tools/list`/`tools/call` request. See the
full smoke-test and client setup in [`mcp.md`](mcp.md#curl-smoke-test). A quick `ping`:

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
> above. It is present, set to `true`, only when a tool reports its own error (for example an
> `execute_command` deny or an `upload_file` traversal). See
> [`mcp.md`](mcp.md#behaviour-and-error-handling).

To run a command via the MCP `execute_command` tool (see [`mcp.md`](mcp.md#execute_command) for the
full contract and the [security guide](execute-command-security.md) first):

```sh
curl -k https://127.0.0.1:7822/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "MCP-Protocol-Version: 2025-11-25" \
  -d '{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"execute_command","arguments":{"command":"ls","args":["-la"],"timeout_ms":5000}}}'
```

To write a file via the MCP `upload_file` tool (see [`mcp.md`](mcp.md#upload_file) for the full
contract and the [security guide](file-upload-security.md) first; `content` is base64):

```sh
curl -k https://127.0.0.1:7822/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "MCP-Protocol-Version: 2025-11-25" \
  -d '{"jsonrpc":"2.0","id":20,"method":"tools/call","params":{"name":"upload_file","arguments":{"path":"notes/hello.txt","content":"aGVsbG8K"}}}'
```

- A `GET /mcp` returns `405` (the server is stateless and offers no server→client stream).
- Any other path (for example `/exec` or `/upload`) still returns `501` with the body `not
  implemented` — command execution and file upload are the MCP tools above, not separate routes.

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

> **This is also how `raxd service stop` works.** Stopping the service sends `SIGTERM`, which triggers
> exactly this graceful shutdown. The clean exit (code `0`) is why the service is **not** restarted
> after a normal stop — see [`service-management.md`](service-management.md#6-restart-on-failure-vs-graceful-stop).

### Startup errors (exit 1)

A startup error is printed in the standard `error:` / `hint:` format on stderr and the process exits
with code `1`.

> **No startup block on a failed start.** The startup block (`cert` / `key` / `tls` /
> `listening …` / `press Ctrl+C`) is printed **only after the TCP listener is successfully bound**,
> via an `OnListen` hook in `internal/server`. If the start fails for any reason — port already in
> use, no permission to create the TLS directory or the upload root, a corrupt certificate, a corrupt
> `keys.db`, or a bad `config.yaml` — `serve` prints **only** the `error:` / `hint:` lines to stderr
> and exits `1`. Neither the startup block nor the shutdown block appears, so there is never a
> misleading `listening …` line for a server that did not start. This behaviour matches the
> cert/permission errors too: they are detected before the bind, so the startup block is never
> reached. See [`troubleshooting.md`](troubleshooting.md#raxd-serve) for the per-error details.

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

Cannot create the upload root (no write permission). The upload root is created before the listener
binds, so this is a startup failure:

```
error: cannot create upload root directory: permission denied
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

**Configuration load failure (invalid bind address, invalid `config.yaml`, or invalid `upload.*`).**
The bind-address and YAML-syntax failures are handled by a **single** error path in `serve`, and that
path prints **one generic hint** that references `bind_addr` / `config.yaml`. An invalid
`upload.max_file_bytes` or `upload.default_mode` is also a config-load failure; the `error:` line
names the upload field. The `error:` line always reports what actually went wrong (it carries the
underlying message from `config.Load`), but the `hint:` line is **not specialised per cause**.

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
- The private TLS key is `0600`; the certificate `0644`; the TLS directory `0700`; the upload root
  `0700`.
- An existing certificate is reused and never silently overwritten.
- The default bind address is `127.0.0.1` (loopback only).
- Every connection is authenticated before any handler runs — including `/mcp` and therefore
  `execute_command` and `upload_file`; the key is taken only from the `Authorization: Bearer` header,
  never from argv or the environment.
- Rejections return an empty body; the reason lives only in the audit stream (except the body-limit
  `413`/`400`, which is not audited at all).
- The audit stream logs the fingerprint, never the key body or the raw `Authorization` header.
  `execute_command` records also carry the command and arguments **verbatim**, and `upload_file`
  records carry the destination **path** (never the file content) — no secrets in argv or in path.
- Rate limiting applies per-key and per-IP, including to `execute_command` and `upload_file`.
- The operations behind authentication are the health check and the MCP server (`ping`,
  `server_info`, `execute_command`, `upload_file`); everything else is `501`.
- `execute_command` runs commands without a shell, with a mandatory timeout, an optional allowlist,
  output/argument limits, and a controlled `cwd`/environment. `upload_file` writes a single regular
  file confined to the upload root (`os.Root`), size-limited, with a controlled mode (no
  setuid/setgid/sticky/world-writable). Neither tool elevates privileges (they inherit the daemon's
  UID/GID); run `raxd` as a non-root user — the [`raxd service`](#system-service-raxd-service) layout
  does this for you by running the daemon as the `raxd` user. See
  [`execute-command-security.md`](execute-command-security.md),
  [`file-upload-security.md`](file-upload-security.md), and
  [`service-management.md`](service-management.md).

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
> next start, and the MCP endpoint follows the same port.

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
| `raxd service install` | stderr | yes (incl. already installed) | no root / manager unavailable / register failure | working |
| `raxd service uninstall` | stderr | yes (incl. not installed) | no root / manager unavailable / removal failure | working |
| `raxd service start` | stderr | yes | not installed / no root / start failure | working |
| `raxd service stop` | stderr | yes | not installed / no root / stop failure | working |
| `raxd service status` | stdout | yes (any state) | manager error | working |
| `raxd serve` | stderr | graceful shutdown | startup error (port/cert/db/bind/config/upload-root) | working |
| `raxd config port` | stderr | — | yes | stub |

> Not a CLI command: the **MCP server** is hosted by `raxd serve` on `/mcp`, exposing `ping`,
> `server_info`, `execute_command`, and `upload_file` — see [`mcp.md`](mcp.md). Command execution and
> file upload are the MCP `execute_command` / `upload_file` tools, not CLI sub-commands.

See also: [`service-management.md`](service-management.md) for the system-service security and
operations guide; [`mcp.md`](mcp.md) for the MCP integration guide (including `execute_command` and
`upload_file`); [`execute-command-security.md`](execute-command-security.md) and
[`file-upload-security.md`](file-upload-security.md) for the command-execution and file-upload
security warnings; [`configuration.md`](configuration.md) for paths, `keys.db`, `config.yaml`, the
networking/`serve` fields, the `exec` / `upload` fields, and the service layout;
[`development.md`](development.md) for building and testing in Docker;
[`troubleshooting.md`](troubleshooting.md) for common `serve`, `service`, `execute_command`, and
`upload_file` problems.

## Author

**Vladimir Kovalev, OEM TECH** — author of raxd.
