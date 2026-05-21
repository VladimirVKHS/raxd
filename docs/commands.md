# Command reference

This document describes every command in the `raxd` command tree **as it exists in the code
today**. The service commands (`version`, `status`) and the API-key commands
(`key create`, `key list`, `key delete`) are fully working. The remaining feature commands
(`config port`, `serve`) are present as honest stubs that report `not implemented yet`.

All CLI text (usage strings, messages, the banner, errors) is in English.

> Where to run these commands: per the security baseline, `raxd` is built and run **inside Docker
> only**. Examples below show the command and its output; for how to actually invoke them in a
> container, see [`development.md`](development.md).

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
└── serve              Start the raxd daemon                (stub)
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
| Any stub command (`config port`, `serve`) | `1` |
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

The daemon is never running on the current build, so `state` is always `not running`. The fields are
printed as aligned `key   value` lines:

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
that file. `status` also never prints TLS contents, the configured port, or any other secret — only
the state string and the resolved paths.

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

> **Scope note.** This release implements local key management only: generation, listing,
> revocation, and secure storage on disk. There is **no network layer yet** — keys are not
> presented over a connection, and there is no TLS server or MCP server to present them to. Those
> arrive in later tasks (see the README's "Coming next"). Today the keys you create are stored and
> can be revoked; what consumes them over the network does not exist yet.

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
key is roughly 52 characters long (`rax_live_` plus 43 base64url characters).

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
- **LAST USED** is `YYYY-MM-DD`, or `never` if the key has not been used. On the current build keys
  show `never`, because there is no network layer yet to record usage.

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

## Stub commands

The following commands are part of the tree and have correct usage strings and help text, but their
logic is **not implemented yet**. Each prints `error: <command>: not implemented yet` to **stderr**
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
> `config.yaml` yet — actually persisting the port is planned (see the README's "Coming next"). The
> default `7822` currently exists only as a viper default used by the config loader.

### `raxd serve`

Start the raxd daemon.

- **Usage:** `raxd serve`
- **Status:** stub — exit `1`.

```
$ raxd serve
error: serve: not implemented yet
```

> `serve` is an **honest stub** (decision D4): it prints the message and exits with a non-zero code
> **without** starting a blocking process, opening a port, or calling `net.Listen`. It is safe to
> run. The real foreground daemon and system-service registration arrive in a later task.

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
| `raxd config port` | stderr | — | yes | stub |
| `raxd serve` | stderr | — | yes | stub |

See also: [`configuration.md`](configuration.md) for paths, `keys.db`, and `config.yaml`,
[`development.md`](development.md) for building and testing in Docker.
