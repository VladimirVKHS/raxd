# Production readiness and known limitations

`raxd` is feature-complete for v1: the CLI, the TLS server, API-key management, the MCP server (with
`ping`, `server_info`, `execute_command`, `upload_file`), the system-service integration, and the
`curl | sh` installer with reproducible release artifacts are **all implemented and verified in
Docker**. This page collects, in one place, the **known limitations and residual risks** you must
weigh **before a public production release**. Nothing here is a defect to be fixed silently — each
item is a deliberate, security-reviewed boundary of v1, tracked against the project's threat models,
with the escalation that closes it.

Read this alongside the per-area security guides:
[`execute-command-security.md`](execute-command-security.md),
[`file-upload-security.md`](file-upload-security.md),
[`service-management.md`](service-management.md), and
[`installation.md`](installation.md#trust-model-v1).

## At a glance

| Area | Limitation | Status / escalation |
|------|------------|---------------------|
| Release host | No public HTTPS host serves the artifacts yet; the default `RAXD_BASE_URL` is a placeholder | Pending — configure a real host before publishing (ОР-3 / ОР-5) |
| Artifact signature | No GPG/minisign signature of `SHA256SUMS`; v1 trust = TLS + SHA256 | Pending — add a signature before any public release (ОР-1) |
| macOS notarization | Not notarized with an Apple Developer ID; the installer only strips the quarantine attribute | Pending — notarize when a Developer ID is available (ОР-2) |
| macOS Gatekeeper testing | The real Gatekeeper flow is verified on a live macOS host, not in Docker | Limitation of the test env — verify on a Mac before a macOS release (ОР-4) |
| License | No `LICENSE` file in the repository; licensing terms are undefined | Pending — add a license before publishing |
| Service uninstall | `raxd service uninstall` keeps the inert `raxd` user (UID-reuse risk) and the data directory | Accepted (П-2) — remove the user/data by hand for a zero-footprint cleanup |
| Upload disk usage | No total-size / disk quota on `upload_file`; only a per-file limit exists | Accepted — bound it with a filesystem/container quota on the upload root |
| `execute_command` arguments | `args` are logged verbatim (and the `upload_file` `path` is logged) | By design — never pass secrets in `args` or in `path` |
| Running as root | If the daemon runs as root, `execute_command`/`upload_file` run as root (WARN by default) | By design — run non-root (the `raxd service` layout does this) |
| Command sandboxing | No cgroups/rlimits/seccomp/namespaces for `execute_command` | Out of scope v1 — isolation relies on a non-root user inside a container |
| mTLS | No client certificates; authentication is by API key only | Out of scope v1 |

The sections below give the detail for each item.

## 1. No public release host yet (ОР-3 / ОР-5)

The `curl | sh` installer (`install.sh`) and the reproducible release matrix (four `tar.gz` archives
plus `SHA256SUMS`, built with `make release-all`) exist and are verified end-to-end in Docker against
a local mock source. What is **not** in place is a public HTTPS host that serves those artifacts: the
default `RAXD_BASE_URL` baked into `install.sh` is the placeholder `https://releases.example.com/raxd`,
which does not serve real files.

**Consequence.** Running the canonical `curl -fsSL https://<base-url>/install.sh | bash` against the
default URL fails at the download step (exit `5`) until a host is configured. Until then, point the
installer at a source you control with `RAXD_BASE_URL`, install manually from a release archive (with
SHA256 verification), or build the artifacts from source in Docker. See
[`installation.md`](installation.md#pointing-the-installer-at-an-artifact-source).

**Escalation before a public release.** Configure a real download host, fix a concrete `RAXD_BASE_URL`
(HTTPS) and a version-pinning policy (`latest` vs a pinned tag), and run the CI release workflow on a
real runner. Tracked as residual risks **ОР-3** and **ОР-5** in
[`specs/distribution/threat-model.md`](../specs/distribution/threat-model.md).

## 2. No GPG/minisign signature of `SHA256SUMS` (ОР-1)

In v1 the integrity of an install rests on **two** mechanisms only: the **TLS (HTTPS) channel** to the
script and the artifacts, and the **SHA256 verification** of the archive against `SHA256SUMS` before
the binary is placed. There is **no GPG/minisign signature** — `install.sh` does not claim to verify a
signature (no false `gpg --verify`), because there is no signing key or public-key trust
infrastructure in v1.

**The boundary.** Because `SHA256SUMS` is fetched from the **same** source as the archive, the hash
check protects you against an archive that is corrupted or substituted **in transit**, or a single
tampered file — but **not** against a compromised source that serves a coordinated, matching archive
**and** `SHA256SUMS`. That gap is exactly what a signature would close.

**Escalation before a public release.** Add a signature (a dedicated signing key, the public key
distributed out-of-band, and a verify step in `install.sh` **before** the hash check). Tracked as
accepted deviation **П-1** / residual risk **ОР-1**. See
[`installation.md`](installation.md#trust-model-v1).

## 3. No Apple notarization on macOS (ОР-2)

`raxd` is **not** notarized with an Apple Developer ID in v1 (there is no certificate). The installer
does the baseline minimum: on macOS it idempotently strips the `com.apple.quarantine` attribute from
the installed binary and prints a Gatekeeper hint.

**Consequence.** Without notarization, Gatekeeper may still warn ("raxd cannot be opened" / "is
damaged") on first run; the manual fix (clear quarantine, or approve under **System Settings → Privacy
& Security**) is printed by the installer.

**Escalation before a macOS release.** Proper code signing + notarization (`codesign` + `notarytool` +
`staple`), gated on an Apple Developer ID. Tracked as accepted deviation **П-2** / residual risk
**ОР-2**. See [`installation.md`](installation.md#macos-gatekeeper--quarantine).

## 4. macOS Gatekeeper / launchd verified outside Docker (ОР-4)

Docker is Linux-only, so two macOS paths cannot be exercised in a container and are verified on a real
macOS host instead:

- The full **Gatekeeper / `xattr`** install flow (the darwin branch of `install.sh` is checked
  statically in CI; the real behaviour needs a Mac).
- The **launchd** service lifecycle (`install → status → start → stop → uninstall`), the non-root
  guarantee via `UserName=raxd`, directory permissions, the `dscl` user-creation procedure, log
  rotation via `newsyslog`, and the privileged-port mechanics.

This is a **limitation of the test environment, not a relaxation of the requirement** — the contract
applies to both platforms. Tracked as accepted deviation **П-4** / residual risk **ОР-4**, consistent
with the service-install model. See
[`service-management.md`](service-management.md#5-the-macos-path-is-not-tested-in-docker).

## 5. No `LICENSE` file

No `LICENSE` file is present in the repository, and licensing terms are not defined at this stage. The
release archives include a `LICENSE` file **only if** one exists in the source tree at build time, so
today the archives ship the binary and `README.md` but no license. A license is expected to be added
before any public release. See [`installation.md`](installation.md#note-on-licensing).

## 6. `service uninstall` keeps the `raxd` user and data (UID-reuse)

`raxd service uninstall` removes the unit/plist, autostart, the privileged-port capability, and the
journald drop-in, but it **intentionally keeps** the inert `raxd` system user (no login shell, no
home, not running) and the state directory (including `keys.db`).

**Why.** Deleting a system user is riskier than keeping an inert one: a future account that reuses the
same UID would inherit ownership of any files left behind (UID-reuse). The data directory is kept so
an uninstall does not silently destroy keys or audit data.

**Consequence / your action.** For a zero-footprint removal, delete the user and the state directory
yourself **after** confirming they hold nothing you still need (`sudo userdel raxd` on Linux,
`sudo dscl . -delete /Users/raxd` on macOS; then remove the state directory). Tracked as accepted
deviation **П-2** in the service-install model. See
[`service-management.md`](service-management.md#3-the-raxd-user-is-kept-after-uninstall).

## 7. No disk quota on `upload_file`

`upload_file` enforces a **per-file** size limit (`upload.max_file_bytes`, default 700 KiB) but there
is **no cap on the total bytes written** across many uploads, and no content inspection
(antivirus/content-type check).

**Consequence / your action.** A client with a valid key can fill the disk with many small files.
Mitigate with a filesystem or container quota on the upload root. See
[`file-upload-security.md`](file-upload-security.md#7-residual-risks-out-of-scope-for-this-version).

## 8. `execute_command` args and `upload_file` path are logged verbatim

Command **arguments** (`args`) for `execute_command` and the destination **path** for `upload_file`
are written to the audit log **verbatim**, with no masking. (The `upload_file` file **content** is
never logged.) This is deliberate — the audit log must show what was run and where a file landed.

**Consequence / your action.** **Never pass a secret in `args` or in a `path`.** See
[`execute-command-security.md`](execute-command-security.md#1-do-not-pass-secrets-in-command-arguments-argv)
and [`file-upload-security.md`](file-upload-security.md#2-do-not-put-secrets-in-the-destination-path).

## 9. Running the daemon as root is a WARN by default

If the daemon runs as root (`euid == 0`), `execute_command` and `upload_file` run as root. By default
(`exec.deny_root: false` / `upload.deny_root: false`) this is allowed with a `WARN` audit record on
**every** call; setting `deny_root: true` makes the tool refuse instead.

**Consequence / your action.** Run `raxd` as a **non-root** user. The easiest way is to register it as
a service — the `raxd service` layout runs the daemon as the unprivileged `raxd` user, which is the
**primary** defence; `deny_root` is a secondary lever. See
[`service-management.md`](service-management.md#1-non-root-execution).

## 10. No command sandboxing; no mTLS

- **No sandboxing for `execute_command`** — no cgroups/rlimits/seccomp/namespaces. Isolation relies on
  running the daemon as a non-root user inside a container; the tool already kills the whole process
  tree on timeout, caps output, and limits argument count/length. Out of scope for v1.
- **No mTLS / client certificates** — authentication is by API key only. Out of scope for v1.

See the [Coming next](../README.md#coming-next) section of the README for the full list of planned,
not-yet-implemented capabilities.

## Related documents

- [`installation.md`](installation.md) — the `curl | sh` flow, the trust model (TLS + SHA256, no GPG
  yet), macOS Gatekeeper, building from source, exit codes, and the note on licensing.
- [`service-management.md`](service-management.md) — the non-root model, the privileged-port
  capability, what `uninstall` keeps, audit-log rotation, and the macOS verification limitation.
- [`execute-command-security.md`](execute-command-security.md) — mandatory warnings for
  `execute_command`.
- [`file-upload-security.md`](file-upload-security.md) — mandatory warnings for `upload_file`.
- [`specs/distribution/threat-model.md`](../specs/distribution/threat-model.md) — the full
  distribution threat model (ОР-1 … ОР-5, П-1 … П-4).

## Author

**Vladimir Kovalev, OEM TECH** — author of raxd.
