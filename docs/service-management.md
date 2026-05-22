# Service management guide

This document explains how `raxd` registers itself as a managed **system service** and the security
model behind that registration. Read it before you install `raxd` as a service on a real host.
Everything here is taken from the current code (`internal/service/*`, `internal/cli/service.go`);
nothing is hypothetical.

> `raxd service install` turns `raxd serve` from a foreground process into a managed OS service — a
> systemd unit on Linux, a launchd daemon on macOS — with autostart at boot, restart on failure, and
> a graceful stop. The single most important property of that registration is that **the daemon runs
> under an unprivileged user (`raxd`), not root**, even though installing the service itself requires
> root. This closes the long-standing residual risk that `execute_command` and `upload_file` ran as
> root.

For the command reference (usage, output, exit codes, error texts) see
[`commands.md`](commands.md#raxd-service). For the on-disk layout (paths, permissions) see
[`configuration.md`](configuration.md#service-layout-system-service). For diagnosing problems see
[`troubleshooting.md`](troubleshooting.md#raxd-service).

## Where this runs

Like the rest of `raxd`, the service integration is built and tested **inside Docker** (security
baseline §6). The systemd path is exercised in a privileged systemd-in-Docker container; the launchd
path is **not** testable in Docker (see [§5](#5-the-macos-path-is-not-tested-in-docker)). The
production deployment target is a native systemd host (Linux) or a real macOS host running `raxd`
under the `raxd` user — not a container as the primary runtime.

## What `install` actually does

`raxd service install` performs these steps (root required throughout). On **Linux** (systemd):

1. Checks for root (`os.Geteuid() == 0`); if not root, it fails with `insufficient privileges` and
   does **not** fall back to anything (no silent root daemon).
2. Refuses to duplicate an existing registration — if `/etc/systemd/system/raxd.service` already
   exists, it reports "already installed" and exits `0`.
3. Creates the system user `raxd` idempotently:
   `useradd --system --no-create-home --shell /usr/sbin/nologin --comment "raxd daemon" raxd`. If the
   user already exists (`useradd` exit code `9`), that is treated as success — the existing account is
   reused, never modified.
4. Renders and writes the systemd unit `/etc/systemd/system/raxd.service` (`root:root`, `0644`).
5. Writes the journald drop-in `/etc/systemd/journald.conf.d/raxd.conf` (`root:root`, `0644`).
6. Runs `systemctl daemon-reload`.
7. Runs `systemctl enable raxd` — autostart at boot.

On **macOS** (launchd) the equivalent steps write the plist
`/Library/LaunchDaemons/tech.oem.raxd.plist`, create the state/log/config directories explicitly
(`0700`, owned by `raxd`), and `launchctl bootstrap` + `enable` the job.

`install` does **not** start the service. After a successful install, start it with
`sudo raxd service start`.

> **Install needs root; the daemon does not.** Writing to `/etc/systemd/system/` (or
> `/Library/LaunchDaemons/`), creating the system user, and calling the service manager all require
> root — that is why `install` / `uninstall` / `start` / `stop` check for root and fail with a clear
> message otherwise. But the unit's `User=raxd` (or the plist's `UserName=raxd`) means the **running
> daemon** drops to the unprivileged `raxd` user. The two are not in conflict: root installs, `raxd`
> runs.

## 1. Non-root execution

The generated unit always sets `User=raxd` / `Group=raxd`; the generated plist always sets
`UserName=raxd` / `GroupName=raxd`. Neither template has a code path that runs the daemon as root —
the user fields are fixed, validated values (see [§7](#7-injection-resistance-of-the-generated-files)),
not configurable to `root`.

The default listening port is **7822**, which is **not** a privileged port (`< 1024`), so the daemon
needs no special privileges to bind it. The result is the property the threat model calls R-E1: the
effective UID of the running daemon is **not** `0`.

You can confirm this on Linux directly from `raxd service status` (the `euid` line is read from
`/proc/<pid>/status` of the running daemon):

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

`euid 999` (any non-zero value) is the AC6 guarantee in practice: the daemon — and therefore every
`execute_command` it runs and every `upload_file` it writes — runs as the unprivileged `raxd` user.

> **Why this matters.** `raxd serve` executes commands and writes files on behalf of authenticated
> MCP clients. If the daemon ran as root, every command and every file write would be root. Running
> under `raxd` closes the residual risks that the `execute_command` and `upload_file` security guides
> flagged (command-exec ОР-1 / file-upload ОР-U1). The per-tool `exec.deny_root` /
> `upload.deny_root` levers still exist as a secondary defence (see
> [`execute-command-security.md`](execute-command-security.md) and
> [`file-upload-security.md`](file-upload-security.md)), but the **primary** defence is this non-root
> service layout.

> **macOS note.** On macOS the `status` output does **not** print an `euid` line: the effective UID
> is not readable without `/proc`, so the `euid` field is `0` in the status structure and is omitted
> from the human output (it is only printed when `euid > 0`). The non-root guarantee on macOS rests on
> `UserName=raxd` in the plist — verify it on a real macOS host (see
> [§5](#5-the-macos-path-is-not-tested-in-docker)).

## 2. Privileged ports (< 1024) and the network capability

If you change the listening port to a privileged one (below `1024`, for example `443`), `raxd` does
**not** run the daemon as root and does **not** use a setuid binary. Instead, on Linux the generated
unit gains exactly one Linux capability:

```ini
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE
```

This is `CAP_NET_BIND_SERVICE` — the single capability that lets an unprivileged process bind a port
below `1024`, and nothing more. The `CapabilityBoundingSet` line caps the set of capabilities the
process may ever hold to just that one (ADR-003).

How `raxd` decides:

- At install time, `raxd service install` reads the port from your `config.yaml` (the same
  `config.Load` path `raxd serve` uses), not a hardcoded default. The generator derives a typed
  boolean `NeedNetBindCap = (port < 1024)`.
- **Default port 7822 (≥ 1024): no capability is generated at all.** The unit contains no
  `AmbientCapabilities` line — zero extra privilege. This is the common case.
- **Port < 1024: the two capability lines above are added** and nothing else (never full root, never
  setuid-root, never any other `CAP_*`).

Why an ambient capability rather than `setcap` on the binary: a file capability stored in the
binary's extended attributes is lost the moment the binary is replaced (a `raxd` upgrade), does not
work on filesystems without xattr support, and is cleared at `execve`. The ambient capability is
granted by systemd at every start, so it survives upgrades (ADR-003).

### The `NoNewPrivileges` trade-off (escalation before a privileged-port release)

For the default and any port `≥ 1024`, the unit includes `NoNewPrivileges=yes` (full hardening). For
a privileged port (`< 1024`), `NoNewPrivileges=yes` is **omitted** while the ambient capability is
present (ADR-003, accepted deviation П-1). This is a deliberate, narrow trade-off:

- It applies **only** when you manually choose a privileged port — never with the default 7822.
- The rest of the hardening is preserved in **both** cases: `ProtectSystem=strict`,
  `ProtectHome=yes`, `PrivateTmp=yes`.
- The daemon is still unprivileged (`User=raxd`) and the capability is still a single, targeted one.

> **Operational guidance.** Keeping the default port `7822` avoids this trade-off entirely. If you
> must run on a privileged port in production, treat it as an **escalation point** (threat model
> ОР-1): confirm on your target systemd whether `NoNewPrivileges=yes` can be kept together with the
> ambient capability before you ship that configuration. The default-port path needs no such review.

### macOS

On macOS, launchd starts the daemon as root and drops to `UserName=raxd`. The mechanics of binding a
port below `1024` on macOS (root-bind before drop, or socket activation) are an **open question to be
verified on real macOS** (AC13), and the default port `7822` makes the question moot by default.

## 3. The `raxd` user is kept after uninstall

`raxd service uninstall` removes everything that carries autostart or privilege, but it
**deliberately keeps the system user `raxd`**. On Linux it:

- stops and disables the service (removes the boot autostart);
- removes the unit `/etc/systemd/system/raxd.service` — and with it the `User=`, capability, and
  hardening directives;
- removes the journald drop-in `/etc/systemd/journald.conf.d/raxd.conf`;
- reloads systemd.

After this, `raxd service status` reports `installed no`. The success block makes the kept user
explicit (Linux):

```
  uninstalled   raxd service
  removed       unit file and autostart registration
  removed       journal size limit drop-in
  kept          system user "raxd" (no shell, no home, not running)
  hint: to also remove the user: sudo userdel raxd
  hint: data in /var/lib/raxd is preserved — remove manually if no longer needed
```

On macOS the block has no `drop-in` line, the user-removal hint uses `dscl`, and the data hint names
the macOS state directory:

```
  uninstalled   raxd service
  removed       plist file and autostart registration
  kept          system user "raxd" (no shell, no home, not running)
  hint: to also remove the user: sudo dscl . -delete /Users/raxd
  hint: data in /usr/local/var/raxd is preserved — remove manually if no longer needed
```

> **Why the user is kept (accepted deviation П-2).** Deleting a system user is more dangerous than
> keeping it. The account may have been created or reused outside `raxd`, and removing it can orphan
> files — a future user that reuses the same UID would inherit ownership of whatever state was left
> behind. The `raxd` user has **no login shell** (`/usr/sbin/nologin`) and **no home**, and after
> uninstall it carries **no** unit and therefore **no** autostart or capability. It is an inert,
> unprivileged account.

### Removing the user manually (zero-footprint)

If you need a zero-footprint removal (for example for compliance), remove the user yourself **after**
confirming it owns nothing you still need outside `raxd`:

- **Linux:** `sudo userdel raxd`
- **macOS:** `sudo dscl . -delete /Users/raxd`

The data directory is **not** removed by uninstall either. The `data in … is preserved` hint names
the real, platform-specific state directory: `/var/lib/raxd` on Linux, `/usr/local/var/raxd` on
macOS. Remove the state directory by hand only when you are sure you no longer need `keys.db`, the TLS
material, or any audit data it holds.

## 4. Audit-log rotation

`raxd serve` writes its audit stream to **stderr**. Under the systemd unit (`Type=exec`,
`StandardError=journal`) that stderr goes to **journald**. To stop the audit log from growing without
bound, `install` writes a journald drop-in:

```ini
# /etc/systemd/journald.conf.d/raxd.conf
[Journal]
SystemMaxUse=200M
SystemMaxFileSize=50M
```

- `SystemMaxUse=200M` caps the total disk the journal may use.
- `SystemMaxFileSize=50M` caps the size of a single journal file.

journald rotates and vacuums automatically and synchronously, so once the cap is reached, old records
are evicted rather than the disk filling up. The drop-in is installed by `install` and removed by
`uninstall`. This closes the rotation part of the residual risks the `execute_command` and
`upload_file` guides delegated to the system (command-exec ОР-2 / file-upload ОР-U4).

> **Boundary: journald limits are per-host, not per-`raxd` (accepted deviation П-3).** systemd's
> `SystemMaxUse=` / `SystemMaxFileSize=` apply to the **whole host journal**, not just the `raxd`
> unit. On a dedicated `raxd` host or container — the intended deployment — "the host journal" is
> effectively the `raxd` journal, so the cap behaves as expected. On a host shared with other
> heavily-logging services, the cap is shared among all of them.
>
> **Fallback for a per-`raxd` limit (logrotate).** If you need a limit isolated to `raxd`, switch the
> daemon to file output (`LogsDirectory=/var/log/raxd` plus a file `StandardError=`) and add a
> `logrotate` config for that file. This is more invasive (it requires editing the unit and adding a
> logrotate artifact) and is not what `install` sets up by default — it is the documented escape
> hatch (threat model ОР-2) when the per-host journald cap is not enough.

### macOS

macOS has no journald. The plist sends stderr (and stdout) to a log file via
`StandardErrorPath`/`StandardOutPath` under the log directory (`/usr/local/var/log/raxd/raxd.log`).
Rotation there is done with `newsyslog`. The exact `newsyslog` configuration is part of the macOS
path that is verified on a real macOS host (see [§5](#5-the-macos-path-is-not-tested-in-docker)).

## 5. The macOS path is not tested in Docker

Docker is Linux, so the launchd integration **cannot** be exercised in a container. This is a
**limitation of the test environment, not a relaxation of the requirement** — the service contract
(install, autostart, restart on failure, graceful stop, non-root, uninstall) applies to **both**
platforms (AC13).

What **is** verified for macOS without a real Mac:

- The plist generator (`RenderPlist`) is unit-tested on Linux (the template file has no build tags),
  including the structure, `KeepAlive.SuccessfulExit=false`, `UserName=raxd`, the XDG environment
  variables, and the derived macOS paths (`/usr/local/etc`, `/usr/local/var`).
- The platform-selection logic (`service.New` dispatching by `runtime.GOOS`) is unit-tested.

What **must** be verified on a real macOS host before a macOS release (escalation ОР-4):

- the full lifecycle `install → status → start → stop → uninstall`;
- that the running daemon's effective UID is **not** `0` (via `UserName=raxd`);
- the directory permissions (`0700`, owned by `raxd`);
- creating the `raxd` user (the exact `dscl` procedure is an open question for that environment);
- log rotation via `newsyslog`;
- the privileged-port mechanics (see [§2](#macos)).

Do not read any macOS behaviour in these docs as "tested in Docker" — the Linux integration is what
is exercised in the container.

## 6. Restart on failure vs. graceful stop

The service is configured to **restart on failure but not after a normal stop**:

- **Linux:** `Restart=on-failure`, `RestartSec=2s`. systemd treats a clean exit (including the exit
  caused by `SIGTERM`) as success, so it is **not** restarted; an abnormal exit (a crash, a
  `kill -9`) is restarted after the delay.
- **macOS:** `KeepAlive = { SuccessfulExit = false }`. launchd restarts the job only when it exits
  with a non-zero code; a graceful exit (code `0`) is **not** restarted.

A `raxd service stop` sends `SIGTERM`, which `raxd serve` already handles as a graceful shutdown
(drain connections, flush usage, exit `0`) — inherited unchanged from the foreground server. Because
that is a clean exit, the manager does **not** bring the service back up. The service stays stopped
until you `raxd service start` it again.

This is the intended distinction:

| Event | Exit | Restarted? |
|-------|------|------------|
| `raxd service stop` (graceful `SIGTERM`) | `0` | **No** — stays stopped |
| Crash / panic / `kill -9` | non-zero | **Yes** — manager restarts it |

> The graceful-shutdown behaviour itself (which signals are handled, what is drained) is part of
> `raxd serve` and is documented in [`commands.md`](commands.md#graceful-shutdown). The service
> integration only sends the signal through the manager and chooses the restart policy.

## 7. Injection resistance of the generated files

The unit/plist are generated from a Go `text/template` with **validated** inputs. Before any render,
`raxd` checks every substituted value (`ValidateTemplateData`): the user/group names against a strict
POSIX allowlist, the launchd label against a reverse-DNS allowlist, the executable path and all
directory paths as absolute and free of control characters, and the port as an integer in
`1..65535`. Any value with a newline, an `=`, a quote, or a control character is rejected **before**
the file is written, so a crafted config value cannot inject a fake `ExecStart=`, `User=root`, or an
extra capability directive. The conditional capability/`NoNewPrivileges` block is driven by the typed
`NeedNetBindCap` boolean, never by a raw string.

The service manager itself (`systemctl` / `launchctl` / `useradd` / `chown`) is always invoked as a
fixed binary with separate arguments — never through a shell — so there is no shell-interpolation
vector. Raw stderr from the manager is **not** propagated to the user output (you get a neutral typed
error, never a raw `systemctl` trace or any secret).

## Security summary

- The daemon runs under the unprivileged user `raxd` (`euid != 0`); installing the service needs
  root, the running daemon does not.
- The default port `7822` needs no special privilege. A privileged port (`< 1024`) grants only
  `CAP_NET_BIND_SERVICE` — never full root or setuid-root.
- For a privileged port, `NoNewPrivileges` is omitted (a narrow, deliberate trade-off, П-1); the rest
  of the hardening (`ProtectSystem`/`ProtectHome`/`PrivateTmp`) is always present.
- Registration files are `root:root` (`root:wheel` on macOS), `0644` — the `raxd` user cannot rewrite
  its own service definition. The state directory is `0700`; `keys.db` and the private TLS key stay
  `0600`.
- The audit log goes to journald with a size cap (`SystemMaxUse=200M` / `SystemMaxFileSize=50M`);
  this cap is per-host (П-3), with logrotate as a documented per-`raxd` fallback.
- `uninstall` removes the unit/plist, the autostart, the capability, and the journald drop-in; it
  **keeps** the inert, shell-less `raxd` user (П-2) and the data directory.
- The macOS launchd path is verified on a real macOS host, not in Docker (AC13).
- Template inputs are validated before render; the manager is invoked without a shell; raw manager
  stderr is never shown to the user.

## Related documents

- [`commands.md`](commands.md#raxd-service) — the `raxd service` command reference (usage, output,
  exit codes, error texts).
- [`configuration.md`](configuration.md#service-layout-system-service) — the service on-disk layout
  (paths and permissions) and the port/capability summary.
- [`troubleshooting.md`](troubleshooting.md#raxd-service) — diagnosing service install/start problems.
- [`execute-command-security.md`](execute-command-security.md) and
  [`file-upload-security.md`](file-upload-security.md) — the per-tool security guides whose root-level
  residual risks the non-root service layout closes.
- [`development.md`](development.md) — building and testing in Docker.
</content>
