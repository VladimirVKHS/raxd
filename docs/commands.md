# Command reference

This document describes every command in the `raxd` command tree **as it exists in the current
skeleton** (`bootstrap-cli`). Two service commands (`version`, `status`) are fully working; the
feature commands (`key`, `config port`, `serve`) are present as honest stubs that report
`not implemented yet`.

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
│   ├── create         Create a new API key                (stub)
│   ├── list           List all API keys                   (stub)
│   └── delete         Delete an API key                   (stub)
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
`raxd status | grep state` and `raxd version | ...` are not polluted by the banner.

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

> Note: the skeleton always renders this single fixed (wide) layout. Adaptive width for narrow
> terminals and color/styling are extension points and are not implemented yet.

### stdout vs stderr

- **stdout** carries the machine-readable result (the `version` line, the `status` fields).
- **stderr** carries the banner and all `error:` messages.

This separation means pipes and redirects behave predictably:
`raxd status > status.txt` captures only the status fields; the banner stays on the terminal.

### Exit codes

| Outcome | Exit code |
|---------|-----------|
| `version`, `status` succeed | `0` |
| Any stub command (`key *`, `config port`, `serve`) | `1` |
| `status` cannot determine `$HOME` | non-zero (error) |
| Unknown command or flag (cobra default) | non-zero |

### Error format

Error messages follow a consistent shape:

```
error: <what happened — one sentence, lowercase, no trailing period>
  hint: <what to do — one sentence, starts with a verb>
```

Stub commands print only the `error:` line (no `hint:`), because no user action is required — the
`not implemented yet` state is an expected property of the skeleton.

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

On this skeleton the daemon is never running, so `state` is always `not running`. The fields are
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

**What `status` deliberately does not show** (security): it never prints the contents of `keys.db`,
the contents of the TLS directory, the configured port, or any sensitive data. It prints only the
state string and the resolved paths.

**Error case — `$HOME` cannot be determined.** If the home directory cannot be resolved, `status`
prints an error with a hint to stderr and exits with a non-zero code:

```
error: cannot determine config directory: $HOME is not set
  hint: set the HOME environment variable and try again
```

---

## Stub commands

The following commands are part of the tree and have correct usage strings and help text, but their
logic is **not implemented yet**. Each prints `error: <command>: not implemented yet` to **stderr**
and exits with code `1`.

### `raxd key`

Manage API keys. This is a command group; it has no action of its own — run one of its
sub-commands. Running `raxd key` alone prints the group's help.

- **Short:** `Manage API keys`
- **Long:** Create, list, and delete API keys used to authenticate remote access.

#### `raxd key create`

Create a new API key.

- **Usage:** `raxd key create [--name <label>]`
- **Flag:** `--name string` — human-readable label for the key.
- **Status:** stub — exit `1`.

```
$ raxd key create --name laptop
error: key create: not implemented yet
```

> When implemented (key-management task), this command will generate a new API key, display it
> **once**, and not allow it to be retrieved afterwards. That behaviour does not exist yet.

#### `raxd key list`

List all API keys.

- **Usage:** `raxd key list`
- **Status:** stub — exit `1`.

```
$ raxd key list
error: key list: not implemented yet
```

> When implemented, this command will print a table of keys (ID, label, created, last-used) to
> stdout with exit code `0`. The table layout is not part of the skeleton.

#### `raxd key delete`

Delete an API key.

- **Usage:** `raxd key delete <id>`
- **Status:** stub — exit `1`.

```
$ raxd key delete abc123de
error: key delete: not implemented yet
```

> When implemented, this command will revoke and permanently delete the key with the given ID.

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

> The help text notes that the default port is `7822`. On the skeleton this command does **not**
> write anything to `config.yaml` — actually persisting the port is planned (see the README's
> "Coming next"). The default `7822` currently exists only as a viper default used by the config
> loader.

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
> run. The real foreground daemon and system-service registration arrive in the service-install
> task.

---

## Summary table

| Command | Channel | Exit 0 | Exit 1 | Status |
|---------|---------|--------|--------|--------|
| banner (every command except `--help`) | stderr | — | — | working |
| `raxd version` | stdout | yes | — | working |
| `raxd status` | stdout | yes | — | working |
| `raxd key create` | stderr | — | yes | stub |
| `raxd key list` | stderr | — | yes | stub |
| `raxd key delete` | stderr | — | yes | stub |
| `raxd config port` | stderr | — | yes | stub |
| `raxd serve` | stderr | — | yes | stub |

See also: [`configuration.md`](configuration.md) for paths and `config.yaml`,
[`development.md`](development.md) for building and testing in Docker.
