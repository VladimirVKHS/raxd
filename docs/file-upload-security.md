# `upload_file` security guide

This document collects the **mandatory security warnings** for the MCP `upload_file` tool. Read it
before you enable file upload against any real host. Everything here is taken from the current code;
nothing is hypothetical.

> `upload_file` writes a file to the host's filesystem on behalf of an authenticated MCP client — a
> network write into the host. It is less powerful than `execute_command` (it creates **only a regular
> file**, never elevates privileges, and never changes ownership), but it is still a "dangerous
> primitive": any authenticated client that can reach `/mcp` can place a file inside the upload root.
> Treat the API key that reaches this tool exactly as you would an SSH key.

The tool's safety controls are enforced **on the server**, not in the schema:

- writes are confined to a configured **upload root** via Go's `os.Root` (no `..`-escape, no absolute
  path, no out-of-root symlink);
- the decoded size of a single file is capped by `upload.max_file_bytes`;
- the **total** size of the whole upload root can be capped by `upload.max_total_bytes` (disabled by
  default; see [§7](#7-residual-risks-out-of-scope-for-this-version));
- the file mode is restricted (mask `0777`, no setuid/setgid/sticky, no world-writable; default `0600`);
- existing files are **not** overwritten unless `overwrite: true`;
- writes are **atomic** (temp file → `rename`), so no partial or stray temp file is left behind;
- every call is **audited** without ever logging the file content.

For the tool's input/output contract, error mapping, and curl examples, see
[`mcp.md`](mcp.md#upload_file). For the `upload.*` configuration keys, see
[`configuration.md`](configuration.md#file-upload-upload-fields).

## 1. Do not place a bind-mount or external filesystem inside the upload root

The write confinement is built on Go's `os.Root` (`os.OpenRoot(uploadRoot)` plus `Root.MkdirAll` /
`Root.OpenFile` / `Root.Rename` / `Root.Stat` / `Root.Remove`). `os.Root` defends against `..`-escape
and against symlinks that point outside the root — those are rejected before any write. But it has a
**documented limitation**: it does **not** limit traversal of **mount points**.

> **Do not mount another filesystem (a bind-mount, a network share, or any external mount) inside the
> upload root.** A bind-mount placed *inside* the root that points *outside* it would redirect a write
> through that mount, and `os.Root` does **not** block this. This is a known residual risk (recorded in
> the threat model as ОР-U2).

Mitigation, and the assumption the design relies on:

- **Keep the upload root a dedicated directory with no mount points.** The default upload root is
  `<state-dir>/uploads` (by default `~/.local/state/raxd/uploads`), created with `0700` permissions —
  a daemon data directory that is **not** intended to host bind-mounts.
- **Run inside a container** (security baseline §6), where the filesystem layout under the state
  directory is controlled.

This is the one traversal vector `os.Root` does not close for you; everything else (`../`, absolute
paths, out-of-root symlinks, the TOCTOU symlink-swap race) **is** closed natively. Blocking mount
points would require Linux-only `openat2(RESOLVE_NO_XDEV)`, which is out of the standard library and
out of scope for this version.

## 2. Do not put secrets in the destination path

**The destination path is written to the audit log.** On every `upload_file` call the relative path is
recorded in the audit stream as a `path=` field — exactly like `execute_command` records its `args`
verbatim. There is no masking or redaction of the path.

The practical rule for the **client / agent**:

- **Never encode a secret in the `path`.** Avoid patterns like `path: "tokens/AKIA....secret/key"` —
  whatever you put in the path lands in the audit log.
- The path is logged as a logfmt **value** (`path=<rel>`), so a path containing spaces, `=`, quotes, or
  newlines is automatically quoted/escaped by the logger and **cannot** forge a fake `result=` key or
  inject a new log line. That protects log integrity, but it does **not** hide the path content — the
  path is still recorded.

What this is **not**: the **file content** is **never** logged. The `content` you send (and the decoded
bytes) never appear in the audit log, the tool result, or any error message. And the `raxd` secrets —
the API-key body, its hash, salt, the raw `Authorization` header, the private TLS key — are likewise
**never** logged: the upload layer only ever sees a short, non-reversible key **fingerprint**, never the
key body, and it never reads the key store or the TLS files. The result returned to the client carries
**only** the relative path (never an absolute host path) and the size — never the content.

> **Operational mitigation.** Because the path is logged, keep the audit stream access-restricted
> (operator / `journald`) like any log that may carry sensitive identifiers.

## 3. Running as root — WARN by default, `deny_root` to refuse

If the `raxd` daemon's effective UID is `0` (it is running as **root**), every file it writes is owned
by root. The tool **never** elevates privileges itself — it does not `chown`, `setuid`, `sudo`, or set
process credentials; the created file simply inherits the daemon's UID/GID as-is. But if the daemon
*itself* is root, the files it writes are root-owned.

Two controls apply, both driven by `upload.deny_root` (a **separate** flag from `exec.deny_root`):

- **`deny_root: false` (default) — warn only.** When the daemon runs as root, **every** `upload_file`
  call writes an extra `WARN` audit record (`reason=running-as-root…`) and then the file **is written
  anyway** as root. This keeps "start as root, drop later" container/dev flows working, at the cost of
  writing root-owned files.
- **`deny_root: true` — hard fail.** When the daemon runs as root, the call writes the root `WARN`
  record **and then is denied**: it returns `isError: true` (`upload as root is forbidden by policy`),
  writes a `DENY` audit record, and **no file is written**.

> **Production guidance.** Run `raxd` as a **non-root** user (recorded as residual risk ОР-U1, to be
> closed by the service-install deployment). If you cannot guarantee that the daemon never runs as root,
> set `upload.deny_root: true` so file writes refuse to run with root privileges.

## 4. File-mode policy — only `0777` permission bits, default `0600`

The mode of the created file is controlled and validated by `fileupload.ParseMode` (and the same policy
validates `upload.default_mode` at startup). The policy is deliberately strict:

- **Only permission bits in the `0777` mask are allowed.** **Any** bit outside `0777` is rejected —
  this includes **setuid (`04000`)**, **setgid (`02000`)**, **sticky (`01000`)**, and any higher bits
  (for example `010000`). Such a `mode` is denied with `isError: true` and a `DENY` audit record; the
  file is **not** created.
- **World-writable is also rejected.** The world-writable bit (`0002`) is denied even though it is
  within the `0777` mask, because a world-writable delivered artifact could be overwritten by any local
  host user.
- **An unparseable `mode` is rejected** (it must be an octal string such as `"0600"` or `"0755"`).
- **Default `0600`.** When the client omits `mode`, the configured `upload.default_mode` is used
  (default `0600`). The mode is applied with `chmod` on the open file descriptor **before** any content
  is written, so it is **umask-independent** — the file never exists with wider permissions.
- **No ownership change, no special files.** The file is created under the daemon's UID/GID as-is. The
  tool does **not** `chown`/`setuid`/`sudo`, and it creates **only a regular file** — never a symlink, a
  hard link, a FIFO, or a device node.

Why this matters: writing a setuid/setgid file over the network would be a privilege-escalation vector
(especially if the daemon ever runs as root, see [§3](#3-running-as-root--warn-by-default-deny_root-to-refuse)),
and a world-writable file is a local integrity risk. The policy closes both. Examples like
`mode: "04755"`, `mode: "02755"`, `mode: "01777"`, `mode: "010000"`, or any world-writable value
(`mode: "0666"`, `mode: "0002"`) are **denied**; legitimate values like `"0600"`, `"0644"`, `"0700"`,
`"0755"`, `"0400"`, `"0660"` are accepted and applied exactly.

## 5. Run as non-root, isolate in a container

Defence in depth — the controls below are **not** provided by the tool itself; they are deployment
responsibilities:

- **Non-root user.** As in [§3](#3-running-as-root--warn-by-default-deny_root-to-refuse), run
  `raxd serve` as an unprivileged user so written files are not root-owned. This is the primary defence.
- **Container isolation.** Per the security baseline, `raxd` is built and run **inside Docker only**.
  Run upload inside an isolated container so a written file cannot reach the host's real filesystem
  beyond the mounted upload root.

## 6. Reading the audit stream

Every `upload_file` call writes **exactly one** primary audit record (plus an extra `WARN` record when
the daemon is root). All records go to the same `stderr` audit stream as the rest of the server, in
strict `key=value` (logfmt) form. The file content is **never** logged — only the relative path and, on
success, the size.

| `msg` | level | When | Key fields |
|-------|-------|------|------------|
| `MCP` | `INFO` | the file was written or replaced | `tool=upload_file result=ok path=<rel> size=<N>` |
| `DENY` | `WARN` | traversal, existing file (no overwrite), target is a directory, too-large, **total quota exceeded**, invalid base64, invalid mode, or `deny_root` — **nothing written** | `tool=upload_file reason=<text>` (and `path=<rel>` when the path was known) |
| `FAIL` | `WARN` | an I/O error during the write (for example a full disk) — the write started but failed; the temp file is cleaned up | `tool=upload_file reason=<text>` (and `path=<rel>` when known) |
| `WARN` | `WARN` | extra record on **every** call when the daemon is root (`deny_root=false` **or** `true`) | `tool=upload_file reason=running-as-root…` (`[path=<rel>]` only if the path is already known — see the note below) |

- **The root `WARN` record carries no `path=`.** The root check runs at the very start of the call,
  **before** the path is parsed/validated, so the daemon emits the `WARN` record with an empty path —
  in practice the root `WARN` line has **no** `path=` field. (`path=` is logged only when it is already
  known.) When `deny_root: true`, the `WARN` record is followed by a **separate** `DENY` record, and
  that `DENY` record **does** carry `path=<rel>`.
- **A total-quota denial is a single `DENY` record** with `reason=total upload quota exceeded`. The
  reason is **neutral**: it does not include an absolute path, the current/limit byte numbers, or any
  secret. Nothing is written for it.
- `path=` is the **relative** path inside the upload root — **never** an absolute host path.
- `size=` is an integer (decoded byte count) and appears **only** on the success (`MCP`) record. On
  `DENY`/`FAIL` nothing was written, so `size=` is absent.
- The `result=ok` key appears **only** on the success (`MCP`) record. The `DENY`/`FAIL`/`WARN` records
  carry the label in `msg=` and the text in `reason=`; they do **not** carry a `result=` key.
- The upload fields (`path=`, `size=`) appear **only** when `tool=upload_file`; non-upload audit records
  (`AUTH`/`FAIL`/`DENY`/`RATE`, the `ping`/`server_info` `MCP` records, and the `execute_command`
  records) are unchanged.

See [`troubleshooting.md`](troubleshooting.md#the-upload_file-tool) for how to interpret these lines
when diagnosing a failed call, and [`mcp.md`](mcp.md#upload_file-audit-records) for the upload audit
format in full.

## 7. Residual risks (out of scope for this version)

The upload root's disk use is now bounded at the application level by an optional total-size cap, while
a few limits remain deliberate for the current version. Mitigate the remaining ones by deployment.

### Total-size cap on the upload root (`upload.max_total_bytes`)

There is a **per-file** size cap (`upload.max_file_bytes`, default 700 KiB) **and**, since this
version, an optional **total-size** cap on the whole upload root: `upload.max_total_bytes` (in bytes).

- **Default `0` = disabled** — behaviour is unchanged unless you opt in (an upgrade does not suddenly
  start rejecting uploads).
- **When set `> 0`**, an upload that would push the **total** bytes of all regular files under the
  upload root over the limit is **denied before anything is written**: `isError: true`, a `DENY` audit
  record (`reason=total upload quota exceeded`), no partial/temp file, existing files untouched, and the
  server stays up. The accounting sums **all** previously written regular files (including files written
  before the cap was enabled and files in sub-directories); symlinks are not followed, and an error while
  walking the root fails **closed** (the write is refused rather than silently allowed).
- The denial message is **neutral** — it states the quota was exhausted without leaking the absolute
  path, the exact byte numbers, or any secret.

This closes the application-level disk-fill risk (previously ОР-U3 — "many uploads can gradually fill
the disk"). Configure it in [`configuration.md`](configuration.md#file-upload-upload-fields):

```yaml
upload:
  max_total_bytes: 1073741824   # 1 GiB cap on the whole upload root; 0 = disabled (default)
```

A filesystem or container disk quota on the upload root remains a valid **complementary** measure
(defence in depth, and it bounds non-`raxd` writes too).

### Still out of scope

- **No per-key / per-fingerprint quota.** `max_total_bytes` is a single **total** cap on the upload
  root; it does **not** limit individual keys independently. One key cannot be given a smaller budget
  than another.
- **No content inspection.** There is no antivirus, content-type check, or file-content filtering. The
  tool writes the decoded bytes as-is (recorded as residual risk ОР-U5).
- **Mount points inside the upload root** are not blocked by `os.Root` (see
  [§1](#1-do-not-place-a-bind-mount-or-external-filesystem-inside-the-upload-root); ОР-U2). Keep the
  upload root free of bind-mounts.
- **Running as root** writes root-owned files unless `upload.deny_root: true` (see
  [§3](#3-running-as-root--warn-by-default-deny_root-to-refuse); ОР-U1). The primary fix is to run
  non-root.
- **No download / read / delete.** This is upload only — there is no `download_file`, no host
  filesystem read, and no file deletion through the tool. Those are out of scope for this version.
- **Audit-log rotation.** Rotation is **not** implemented in the binary; the audit stream goes to
  `stderr`. Rotation is delegated to the system — `journald` under systemd, or `logrotate` for file
  output. Configure it so the disk cannot fill up with logs.

## Related documents

- [`mcp.md`](mcp.md#upload_file) — the `upload_file` tool contract, error mapping, audit, and curl
  examples.
- [`configuration.md`](configuration.md#file-upload-upload-fields) — the `upload.*` configuration keys
  and defaults (including `max_total_bytes` and its startup validation).
- [`troubleshooting.md`](troubleshooting.md#the-upload_file-tool) — diagnosing `isError` results and
  reading the upload audit.
- [`execute-command-security.md`](execute-command-security.md) — the companion security guide for the
  `execute_command` tool (secrets in argv, allowlist, `deny_root`, isolation).
- [`production-readiness.md`](production-readiness.md#7-upload-disk-usage--total-size-cap-available-uploadmax_total_bytes) —
  where the upload disk-quota item is tracked.
- [`commands.md`](commands.md#raxd-serve) — the `serve` command that hosts the MCP endpoint.

## Author

**Vladimir Kovalev, OEM TECH** — author of raxd.
