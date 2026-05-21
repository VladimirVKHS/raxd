# Configuration and paths

This document describes where `raxd` stores its configuration and state, how to override those
locations, the `keys.db` key database, and the `config.yaml` format — **as implemented in the code
today**.

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

## The `config.yaml` file

Configuration is read from the config file shown above (`~/.config/raxd/config.yaml` by default)
using [viper](https://github.com/spf13/viper). The file is YAML.

### Format

The current build recognises a single key:

```yaml
# ~/.config/raxd/config.yaml
port: 7822
```

| Key | Type | Default | Meaning |
|-----|------|---------|---------|
| `port` | integer | `7822` | The TCP port `raxd` is intended to listen on (used by future networking) |

### Behaviour

- **Missing file is not an error.** If `config.yaml` does not exist, the defaults are applied
  (`port: 7822`). `raxd status` shows the file path with the suffix `(not found, defaults
  applied)` and exits with code `0`.
- **Malformed YAML is an error.** If the file exists but is not valid YAML, the config loader
  returns an explicit error (`config file is not valid YAML`).

> Important scope note: the config loader exists as a library, but the CLI commands do **not** yet
> act on the loaded values. In particular, `raxd config port <PORT>` is a stub and does not write
> the port to `config.yaml`, and no command currently changes its behaviour based on the `port`
> value. Wiring configuration into command behaviour is planned (see the README's "Coming next").

## Related documents

- [`commands.md`](commands.md) — full command reference, including `raxd status` and `raxd key`.
- [`development.md`](development.md) — building and testing in Docker.
