# `execute_command` security guide

This document collects the **mandatory security warnings** for the MCP `execute_command` tool. Read it
before you enable command execution against any real host. Everything here is taken from the current
code; nothing is hypothetical.

> `execute_command` is the **most dangerous** capability in `raxd`. By design it runs an arbitrary
> binary on the host on behalf of an authenticated MCP client — functionally this is remote code
> execution of the same class as SSH. Any authenticated client that can reach `/mcp` can run commands
> with the privileges of the `raxd serve` process. Treat the API key that reaches this tool exactly as
> you would an SSH private key.

For the tool's input/output contract and curl examples, see
[`mcp.md`](mcp.md#execute_command). For the `exec.*` configuration keys, see
[`configuration.md`](configuration.md#command-execution-exec-fields).

## 1. Do not pass secrets in command arguments (argv)

**Command arguments are written to the audit log verbatim.** The argument vector (`args`) is logged
exactly as the client sent it, with no masking or redaction. This is a deliberate, security-reviewed
decision: there is no reliable way to detect "which token in an arbitrary argv is a secret", and
heuristic masking would give a false sense of safety while still risking corrupted, incomplete audit
records. Audit completeness is the primary control for investigating command-execution incidents, so
arguments are kept whole.

The practical rule for the **client / agent**:

- **Never put a secret in `args`.** Avoid patterns like:
  - `mysql -pPASSWORD …`
  - `curl -H "Authorization: Bearer TOKEN" …`
  - `git clone https://user:token@host/…`
- **Pass secrets through the target command's own mechanisms** instead — for example a credentials
  file the command reads, or an environment variable that the *target* command supports. (Note:
  `execute_command` does **not** accept an `env` field from the client; the child environment is a
  fixed server-side whitelist — see [§5](#5-environment-and-working-directory-are-controlled-by-the-server).
  Use a credentials file the command reads, or store the secret where the command expects it.)

What this is **not**: this is about *client/third-party* secrets that a client chooses to put on a
command line. The `raxd` secrets — the API-key body, its hash, salt, the raw `Authorization` header,
and the private TLS key — are **never** written to the audit log, the tool result, or any error
message. The exec layer only ever sees a short, non-reversible key **fingerprint**, never the key
body. Those two concerns are separate; only the first is your responsibility as the caller.

> **Operational mitigation.** Because arguments are logged verbatim, the audit stream itself is
> sensitive. Keep it access-restricted (operator / `journald`) like any log that may contain
> credentials. See [Audit](#7-reading-the-audit-stream) below.

## 2. The allowlist is strict and exact — and off by default

`exec.allowlist` controls which commands may run.

- **Off by default.** The default is an empty list (`exec.allowlist: []`), which means the allowlist is
  **disabled** and **any** command an authenticated client requests is allowed to run. That is the
  intended behaviour of an SSH-class tool, but it means a single valid key can run anything the daemon
  user can. **For production, turn the allowlist on.**
- **Exact, literal string match.** When the allowlist is non-empty, a command runs **only if** the
  `command` string the client sent is **exactly equal** to one of the allowlist entries. The match is:
  - **not** a regex,
  - **not** a prefix,
  - **not** case-insensitive,
  - **not** whitespace-normalised,
  - performed **before** the binary is resolved on `PATH`.

The most important consequence for operators:

> **`ls` and `/bin/ls` are different entries.** The allowlist compares the *raw string the client
> sends*, not the resolved binary path. If you list `ls`, a client that calls `/bin/ls` is **denied**
> (and vice-versa). List the commands **exactly the way your clients invoke them**. If your clients use
> bare names, list bare names; if they use absolute paths, list those absolute paths. If different
> clients use different aliases for the same binary, list each form you intend to permit.

This exactness is a deliberate trade-off: it is predictable for the operator and cannot be bypassed by
clever string forms, but it does **not** protect against a client calling the same binary under a
different name/path that you also happened to allow. Resolving aliases to a canonical path is **not**
implemented in this version.

A denied command produces `isError: true`, a `command not allowed` result, and a `DENY` audit line.
The command is **not** started.

## 3. The `deny_root` policy and running as root

If the `raxd` daemon's effective UID is `0` (it is running as **root**), every command it runs also
runs as root. The tool **never** elevates privileges itself — it does not use `setuid`, `sudo`, or set
`SysProcAttr.Credential`; the child process simply inherits the daemon's UID/GID as-is. But if the
daemon *itself* is root, that inheritance is already a full root-level RCE surface.

Two controls apply, both driven by `exec.deny_root`:

- **`deny_root: false` (default) — warn only.** When the daemon runs as root, **every**
  `execute_command` call writes an extra `WARN` audit record (`reason=running-as-root`) and then the
  command **runs anyway**. This default keeps legitimate "start as root, drop later" container and dev
  flows working, at the cost of executing as root.
- **`deny_root: true` — hard fail.** When the daemon runs as root, the call writes the root `WARN`
  record **and then is denied**: it returns `isError: true` (`execution as root is forbidden by
  policy`), writes a `DENY` audit record, and the command is **not** started.

> **Production guidance.** Run `raxd` as a **non-root** user. If you cannot guarantee that the daemon
> never runs as root (for example a container that starts as root before dropping privileges), set
> `deny_root: true` so command execution refuses to run with root privileges.

## 4. Run as non-root, isolate in a container

Defence in depth — the controls below are **not** provided by the tool itself; they are deployment
responsibilities:

- **Non-root user.** As in [§3](#3-the-deny_root-policy-and-running-as-root), run `raxd serve` as an
  unprivileged user. This is the primary defence against privilege escalation.
- **Container isolation.** Per the security baseline, `raxd` is built and run **inside Docker only**.
  Run command execution inside an isolated container so that a misbehaving command cannot reach the
  host's filesystem, processes, or network beyond what the container exposes.

## 5. Environment and working directory are controlled by the server

The client cannot influence the child process's environment. There is **no `env` field** in the tool
input. The child environment is built **explicitly** from a server-side whitelist
(`exec.env_whitelist`, default `PATH`, `HOME`, `LANG`, `TERM`). Dangerous dynamic-loader and shell
variables — `LD_PRELOAD`, `LD_LIBRARY_PATH`, `DYLD_INSERT_LIBRARIES`, `IFS` — are **not** in the
default whitelist and are **not** passed to the child even if they are set in the daemon's own
environment.

The working directory is also server-controlled: with no `cwd` the command runs in `exec.default_cwd`
(default `/tmp`); a provided `cwd` is validated (it must exist and be a directory) before the command
starts, and an invalid `cwd` is denied.

## 6. No shell — metacharacters are literal

The command is launched as a **binary plus an argument list** (`exec.CommandContext`), never through a
shell. `sh -c <string>` is never used. Shell metacharacters in `args` (`;`, `|`, `$()`, `&&`, `>`,
backticks) are passed to the process as **literal** arguments — they do **not** start sub-shells,
pipes, redirections, or command substitution. There is no shell-injection surface, because there is no
shell.

A relative-path binary resolved from the current directory is rejected (`exec.ErrDot`), so a command
cannot be hijacked by a binary dropped in the working directory.

## 7. Reading the audit stream

Every `execute_command` call writes **exactly one** primary audit record (plus an extra `WARN` record
when the daemon is root). All records go to the same `stderr` audit stream as the rest of the server,
in strict `key=value` (logfmt) form. The key body is never logged — only the fingerprint.

| `msg` | level | When | Key fields |
|-------|-------|------|------------|
| `MCP` | `INFO` | command ran (any exit code, **including a timeout**) | `tool=execute_command result=ok command= args= exit_code= duration= timed_out=` |
| `DENY` | `WARN` | allowlist deny, input-limit deny, or `deny_root` deny — command **not** started | `tool=execute_command reason= command= args=` |
| `FAIL` | `WARN` | binary not found, relative path, or bad `cwd` — command **not** started | `tool=execute_command reason= command= args=` |
| `WARN` | `WARN` | extra record on **every** call when the daemon is root (`deny_root=false` or `true`) | `tool=execute_command reason=running-as-root command= args=` |

- A non-zero `exit_code` is **success** (`msg=MCP result=ok`), not a failure — the command ran.
- A timeout is also **success** (`msg=MCP result=ok … timed_out=true`) — the command ran and was
  killed; the result is partial output, not an error.
- `exit_code`, `duration`, and `timed_out` appear **only** on the `MCP` (success) record. On `DENY` /
  `FAIL` the command was never started, so those fields are absent.
- The exec fields (`command=`, `args=`, …) appear **only** when `tool=execute_command`; non-exec audit
  records (`AUTH`/`FAIL`/`DENY`/`RATE` and `ping`/`server_info` `MCP` records) are unchanged.

See [`troubleshooting.md`](troubleshooting.md#the-execute_command-tool) for how to interpret these
lines when diagnosing a failed call, and [`mcp.md`](mcp.md#audit) for the MCP audit format in general.

## 8. Residual risks (out of scope for this version)

These limits are deliberate for the current version; mitigate them by deployment:

- **Resource exhaustion / fork bombs.** The tool kills the whole process group on timeout or
  cancellation, caps each output stream (`exec.max_output_bytes`, default 1 MiB), and caps argument
  count/length before launch. It does **not** apply cgroups, rlimits, seccomp, or namespaces. A
  command that forks aggressively can still consume host resources until the timeout fires. Mitigate
  with container resource limits and (if you can) an allowlist.
- **Audit-log rotation.** Rotation is **not** implemented in the binary; the audit stream goes to
  `stderr`. Rotation is delegated to the system — `journald` under systemd, or `logrotate` for file
  output. For a production deployment that writes the audit log to a file, configure rotation so the
  disk cannot fill up.
- **Secrets in argv.** As covered in [§1](#1-do-not-pass-secrets-in-command-arguments-argv), arguments
  are logged verbatim; keep the audit stream access-restricted.

## Related documents

- [`mcp.md`](mcp.md#execute_command) — the `execute_command` tool contract, error mapping, and curl
  examples.
- [`configuration.md`](configuration.md#command-execution-exec-fields) — the `exec.*` configuration
  keys and defaults.
- [`troubleshooting.md`](troubleshooting.md#the-execute_command-tool) — diagnosing `isError` results
  and reading the exec audit.
- [`commands.md`](commands.md#raxd-serve) — the `serve` command that hosts the MCP endpoint.

## Author

**Vladimir Kovalev, OEM TECH** — author of raxd.
</content>
