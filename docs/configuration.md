# Configuration and paths

This document describes where `raxd` stores its configuration and state, how to override those
locations, and the `config.yaml` format — **as implemented in the current skeleton**
(`bootstrap-cli`).

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

You can see the resolved paths at any time with [`raxd status`](commands.md#raxd-status).

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

> Files are not created by the skeleton. When key storage and TLS arrive, the keys database and the
> TLS private key are intended to be `0600`. On this skeleton only the **paths** for `keys.db` and
> the TLS directory are reserved — no key or certificate files are written.

The only condition under which path resolution fails is when the home directory cannot be
determined (`$HOME` is not set). In that case commands that need the paths (such as `status`)
report an error and exit with a non-zero code:

```
error: cannot determine config directory: $HOME is not set
  hint: set the HOME environment variable and try again
```

## The `config.yaml` file

Configuration is read from the config file shown above (`~/.config/raxd/config.yaml` by default)
using [viper](https://github.com/spf13/viper). The file is YAML.

### Format

The skeleton recognises a single key:

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

> Important scope note: on this skeleton the config loader exists as a library, but the CLI
> commands do **not** yet act on the loaded values. In particular, `raxd config port <PORT>` is a
> stub and does not write the port to `config.yaml`, and no command currently changes its behaviour
> based on the `port` value. Wiring configuration into command behaviour is planned (see the
> README's "Coming next").

## Related documents

- [`commands.md`](commands.md) — full command reference, including `raxd status`.
- [`development.md`](development.md) — building and testing in Docker.
