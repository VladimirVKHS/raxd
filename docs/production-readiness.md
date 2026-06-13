# Production readiness and known limitations

`raxd` is feature-complete for v1: the CLI, the TLS server, API-key management, the MCP server (with
`ping`, `server_info`, `execute_command`, `upload_file`), the system-service integration, and the
`curl | sh` installer with reproducible release artifacts are **all implemented and verified in
Docker**. This page collects, in one place, the **known limitations and residual risks** you must
weigh **before a public production release**. Nothing here is a defect to be fixed silently — each
item is a deliberate, security-reviewed boundary of v1, tracked against the project's threat models,
with the escalation (or the decision) that governs it. Items that have since been **closed** are
marked as such so this page stays an honest, current picture rather than a backlog.

Read this alongside the per-area security guides:
[`execute-command-security.md`](execute-command-security.md),
[`file-upload-security.md`](file-upload-security.md),
[`service-management.md`](service-management.md), and
[`installation.md`](installation.md#trust-model-v1).

## At a glance

| Area | Limitation | Status / escalation |
|------|------------|---------------------|
| Release host | Artifacts are published on GitHub Releases; `install.sh` resolves them by default | **Closed** — the canonical `curl … install.sh \| bash` works; pick a version policy (`latest` vs a pinned tag) (ОР-3 / ОР-5) |
| License | A root `LICENSE` file (MIT, Vladimir Kovalev / OEM TECH) is present and shipped in the release archives | **Closed** |
| Upload disk usage | A configurable **total-size** cap on the upload root now exists (`upload.max_total_bytes`); a per-key quota and content inspection do not | **Closed at the application level** — set `upload.max_total_bytes`; residual: no per-key quota, no content inspection |
| Service uninstall | `uninstall` keeps the inert `raxd` user and data **by default**; `uninstall --purge --yes` now removes the user and data on request | **Mitigated** — opt-in full cleanup via `--purge --yes`; the keep-by-default behaviour is unchanged by design (П-2) |
| Artifact signature | No GPG/minisign signature of `SHA256SUMS`; v1 trust = TLS + SHA256 | **Deferred by decision** — the owner accepts TLS + SHA256 for now; a signature may be added later (ОР-1) |
| macOS notarization | Not notarized with an Apple Developer ID; the installer only strips the quarantine attribute | Open — no Developer ID available; notarize when one is (ОР-2) |
| macOS Gatekeeper testing | The real Gatekeeper flow is verified on a live macOS host, not in Docker | Limitation of the test env — verify on a Mac before a macOS release (ОР-4) |
| `execute_command` arguments | `args` are logged verbatim (and the `upload_file` `path` is logged) | By design — never pass secrets in `args` or in `path` |
| Running as root | If the daemon runs as root, `execute_command`/`upload_file` run as root (WARN by default) | By design — run non-root (the `raxd service` layout does this) |
| Command sandboxing | No cgroups/rlimits/seccomp/namespaces for `execute_command` | Out of scope v1 — isolation relies on a non-root user inside a container |
| mTLS | No client certificates; authentication is by API key only | Out of scope v1 |

The sections below give the detail for each item.

## 1. Public release host — closed (GitHub Releases) (ОР-3 / ОР-5)

The `curl | sh` installer (`install.sh`) and the reproducible release matrix (four `tar.gz` archives
plus `SHA256SUMS`, built with `make release-all`) exist and are verified end-to-end in Docker. The
**public download host is now in place**: the artifacts are published on **GitHub Releases** for the
repository [`https://github.com/vladimirvkhs/raxd`](https://github.com/vladimirvkhs/raxd), and
`install.sh` resolves them there by default — the old `https://releases.example.com/raxd` placeholder
has been removed.

**The canonical one-liner now works:**

```sh
curl -fsSL https://github.com/vladimirvkhs/raxd/releases/latest/download/install.sh | bash
```

With no `RAXD_BASE_URL` set, `install.sh` treats the empty value as a sentinel meaning "use GitHub
Releases": for `latest` it resolves the real tag through the GitHub API
(`https://api.github.com/repos/vladimirvkhs/raxd/releases/latest`) and downloads the archive and
`SHA256SUMS` from `https://github.com/vladimirvkhs/raxd/releases/download/<tag>/…`. Setting
`RAXD_BASE_URL` explicitly still points the installer at any source you control (the offline-test
path), and that override skips the GitHub API entirely.

**Version policy (the one thing left to choose).** Decide whether you install `latest` or a **pinned**
tag:

- **`latest`** (default): always installs the newest published release. Convenient, but the installed
  version can change between runs.
- **Pinned**: set `RAXD_VERSION=v0.1.0` (or pass `--version v0.1.0`) to install an exact tag — the
  reproducible choice for production and CI.

```sh
# Pin a specific release:
RAXD_VERSION=v0.1.0 \
  curl -fsSL https://github.com/vladimirvkhs/raxd/releases/latest/download/install.sh | bash
```

Tracked previously as residual risks **ОР-3** / **ОР-5** in
[`specs/distribution/threat-model.md`](../specs/distribution/threat-model.md); the host part is now
closed. See [`installation.md`](installation.md#quick-install-curl--sh).

## 2. No GPG/minisign signature of `SHA256SUMS` — deferred by decision (ОР-1)

In v1 the integrity of an install rests on **two** mechanisms only: the **TLS (HTTPS) channel** to the
script and the artifacts, and the **SHA256 verification** of the archive against `SHA256SUMS` before
the binary is placed. There is **no GPG/minisign signature** — `install.sh` does not claim to verify a
signature (no false `gpg --verify`), because there is no signing key or public-key trust
infrastructure in v1.

**The boundary.** Because `SHA256SUMS` is fetched from the **same** source as the archive (GitHub
Releases), the hash check protects you against an archive that is corrupted or substituted **in
transit**, or a single tampered file — but **not** against a compromised source that serves a
coordinated, matching archive **and** `SHA256SUMS`. That gap is exactly what a signature would close.

**This is a deliberate, owner-level decision to defer, not an oversight.** For v1 the project owner
accepts the TLS + SHA256 trust model and the GitHub-Releases provenance, and has chosen **not** to
introduce signing infrastructure yet. Adding a signature later (a dedicated signing key, the public
key distributed out-of-band, and a verify step in `install.sh` **before** the hash check) remains the
path to closing this — but it is scheduled by decision, not a blocking pre-release defect. Tracked as
accepted deviation **П-1** / residual risk **ОР-1**. See
[`installation.md`](installation.md#trust-model-v1).

## 3. No Apple notarization on macOS (ОР-2)

`raxd` is **not** notarized with an Apple Developer ID in v1 — there is **no Developer ID certificate
available** to the project. The installer does the baseline minimum: on macOS it idempotently strips
the `com.apple.quarantine` attribute from the installed binary and prints a Gatekeeper hint.

**Consequence.** Without notarization, Gatekeeper may still warn ("raxd cannot be opened" / "is
damaged") on first run; the manual fix (clear quarantine, or approve under **System Settings → Privacy
& Security**) is printed by the installer.

**Escalation before a macOS release.** Proper code signing + notarization (`codesign` + `notarytool` +
`staple`), gated on obtaining an Apple Developer ID. Tracked as accepted deviation **П-2** / residual
risk **ОР-2**. See [`installation.md`](installation.md#macos-gatekeeper--quarantine).

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

## 5. License — closed (MIT)

A root **`LICENSE`** file is now present in the repository: the **MIT License**, Copyright (c) 2026
**Vladimir Kovalev, OEM TECH**. The release archives include it: `scripts/release.sh` copies the
source-tree `LICENSE` into each archive at build time, so a published `tar.gz` now ships the binary,
`README.md`, **and** `LICENSE`. This previously-open item is **closed**. See
[`installation.md`](installation.md#note-on-licensing).

## 6. `service uninstall` keeps the `raxd` user and data (UID-reuse)

`raxd service uninstall` (the default, no flags) removes the unit/plist, autostart, the privileged-port
capability, and the journald drop-in, but it **intentionally keeps** the inert `raxd` system user (no
login shell, no home, not running) and the state directory (including `keys.db`). **This keep-by-default
behaviour is unchanged** (accepted deviation П-2): deleting a system user is riskier than keeping an
inert one — a future account that reuses the same UID would inherit ownership of any files left behind
(UID-reuse) — and keeping the data directory means an uninstall does not silently destroy keys or audit
data.

**What is new: an opt-in, one-command full cleanup.** For a zero-footprint removal you no longer need to
run `userdel` / `dscl` and delete directories by hand. `raxd service uninstall --purge --yes`
additionally removes the `raxd` system user **and** the state/config directories on both Linux and
macOS. Because purge is irreversible (it erases `keys.db` and the audit data), it is gated behind an
explicit double opt-in:

- **`--purge` without `--yes`** prints an irreversibility warning listing exactly what would be
  destroyed (the `raxd` user, the state dir, the config dir, and `keys.db` — "cannot be recovered"),
  **removes nothing**, and exits with a **non-zero** code.
- **`--purge --yes`** performs the removal non-interactively (no `y/n` prompt — the tool is driven by
  agents/scripts), and is **idempotent**: re-running it when the user/dirs are already gone still exits
  `0` and reports what was already absent.

The purge has guardrails: it refuses if the daemon cannot be stopped first, if it lacks the privileges
for `userdel`/`dscl`, if the target OS user does not match the expected raxd service account, or if the
resolved state/config path is suspicious (empty, `/`, `$HOME`, a blocked system root, or outside the
expected layout) — in each case it removes **nothing** and exits non-zero with a neutral message.

The full behaviour, the exact output, the error cases, and the manual fallback for a default
(`--purge`-less) uninstall are in
[`service-management.md`](service-management.md#3-the-raxd-user-is-kept-after-uninstall)
and [`commands.md`](commands.md#raxd-service-uninstall).

## 7. Upload disk usage — total-size cap available (`upload.max_total_bytes`)

`upload_file` enforces a **per-file** size limit (`upload.max_file_bytes`, default 700 KiB). It now
**also** supports a configurable **total-size cap** on the whole upload root, `upload.max_total_bytes`,
closing the application-level disk-fill risk that was previously left to a filesystem/container quota.

**How it works.** `upload.max_total_bytes` is an integer in **bytes**, default **`0` = no limit**
(disabled, so an upgrade does not suddenly start rejecting uploads). A negative value is rejected at
startup. When set above `0`, an upload that would push the **total** bytes of all regular files under
the upload root over the limit is **denied before anything is written** — `isError: true`, a `DENY`
audit record (`reason=total upload quota exceeded`), no partial/temp file left behind, existing files
untouched, and the server stays up. The accounting walks the upload root and sums **all** previously
written regular files (including ones written before the limit was enabled and those in
sub-directories); symlinks are not followed. The rejection message is **neutral** — it states the quota
was exhausted without leaking absolute paths, the exact byte numbers, or any secret.

```yaml
upload:
  max_total_bytes: 1073741824   # 1 GiB cap on the whole upload root; 0 = disabled (default)
```

See [`configuration.md`](configuration.md#file-upload-upload-fields) for the field and its validation,
and [`file-upload-security.md`](file-upload-security.md#7-residual-risks-out-of-scope-for-this-version)
for how it bounds the disk-fill risk.

**Residual (still out of scope).** The cap is a single **total** limit on the upload root. There is
**no per-key / per-fingerprint quota** (one key cannot be limited independently of another), and **no
content inspection** (no antivirus, content-type check, or content filtering — the decoded bytes are
written as-is). A filesystem/container quota on the upload root remains a valid complementary measure.

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

- [`installation.md`](installation.md) — the `curl | sh` flow (GitHub Releases), the trust model (TLS +
  SHA256, no GPG yet), macOS Gatekeeper, building from source, exit codes, and the note on licensing.
- [`service-management.md`](service-management.md) — the non-root model, the privileged-port
  capability, what `uninstall` keeps and what `uninstall --purge --yes` removes, audit-log rotation,
  and the macOS verification limitation.
- [`execute-command-security.md`](execute-command-security.md) — mandatory warnings for
  `execute_command`.
- [`file-upload-security.md`](file-upload-security.md) — mandatory warnings for `upload_file`,
  including the total-size cap (`upload.max_total_bytes`).
- [`specs/distribution/threat-model.md`](../specs/distribution/threat-model.md) — the full
  distribution threat model (ОР-1 … ОР-5, П-1 … П-4).

## Author

**Vladimir Kovalev, OEM TECH** — author of raxd.
