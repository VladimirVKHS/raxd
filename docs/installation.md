# Installation

How to install the `raxd` binary on a fresh host. Everything here is taken from the current
`install.sh`, `scripts/release.sh`, and the `Makefile`; nothing is hypothetical. Where a capability is
a placeholder or is deliberately left out of v1, that is called out honestly rather than glossed over.

> **Status: the installer exists and is verified, the public download host does not yet.** The
> `curl | sh` installer (`install.sh`) is implemented and has been verified end-to-end in Docker
> against a local artifact source (the `TEST1`–`TEST9` install-flow checks are green). What is **not**
> in place yet is a public release host that serves the release archives over HTTPS: the default
> download URL in `install.sh` is a **placeholder** (`https://releases.example.com/raxd`). Until a real
> host is configured, point the installer at your own source with `RAXD_BASE_URL` (see
> [Quick install](#quick-install-curl--sh)), install [manually from a release archive](#manual-installation),
> or [build the artifacts from source](#building-release-artifacts-from-source).

`install.sh` installs **only the `raxd` binary**. It does **not** register a system service — that is a
separate, already-shipped command, `raxd service install`, documented in
[`service-management.md`](service-management.md). After installing the binary, `install.sh` prints a
hint pointing you at that command; see [Registering the service](#registering-the-service-not-installsh).

## Supported platforms

`raxd` is a single static binary (`CGO_ENABLED=0`) built for the following four targets:

| OS (`uname -s`) | Maps to | Architecture (`uname -m`) | Maps to | Artifact |
|-----------------|---------|---------------------------|---------|----------|
| `Linux`         | `linux`  | `x86_64`         | `amd64` | `raxd_<version>_linux_amd64.tar.gz` |
| `Linux`         | `linux`  | `aarch64` / `arm64` | `arm64` | `raxd_<version>_linux_arm64.tar.gz` |
| `Darwin` (macOS)| `darwin` | `x86_64`         | `amd64` | `raxd_<version>_darwin_amd64.tar.gz` |
| `Darwin` (macOS)| `darwin` | `arm64` (Apple Silicon) | `arm64` | `raxd_<version>_darwin_arm64.tar.gz` |

`install.sh` normalises the architecture string (`x86_64` → `amd64`, `aarch64`/`arm64` → `arm64`)
before selecting the artifact.

**Out of scope:** Windows and any non-{macOS, Linux} platform; 32-bit architectures (for example
`i686`). The installer rejects them explicitly with exit `2` — see
[Troubleshooting → unsupported platform](troubleshooting.md#error-unsupported-platform--unsupported-architecture-exit-2).

## Quick install (`curl | sh`)

The canonical, intended form once a public host is configured is a single command:

```sh
curl -fsSL https://<base-url>/install.sh | bash
```

> **Honesty note: there is no public `<base-url>` yet.** The default `RAXD_BASE_URL` baked into
> `install.sh` is the placeholder `https://releases.example.com/raxd`, which does not serve real
> artifacts. Running the command above as-is will fail at the download step (exit `5`) until a real
> release host is set up. To install today, supply your own source with `RAXD_BASE_URL` (below), use
> [Manual installation](#manual-installation), or [build from source](#building-release-artifacts-from-source).

### Pointing the installer at an artifact source

`install.sh` reads the artifact location and version from the environment, so you can run it against any
HTTPS source that hosts the release archives and `SHA256SUMS`:

| Variable | Purpose | Default |
|----------|---------|---------|
| `RAXD_BASE_URL` | Base URL for the artifacts (the archive and `SHA256SUMS` are fetched from `<base-url>/<archive>` and `<base-url>/SHA256SUMS`) | `https://releases.example.com/raxd` (placeholder) |
| `RAXD_VERSION` | Release tag — selects which archive name is requested | `latest` |
| `RAXD_PREFIX` | Install directory, overriding auto-detection (see [Install path and privileges](#install-path-and-privileges)) | _(unset → auto-detect)_ |

For example, to install version `v0.1.0` from your own HTTPS host:

```sh
RAXD_BASE_URL=https://artifacts.example.org/raxd RAXD_VERSION=v0.1.0 \
  curl -fsSL https://artifacts.example.org/raxd/install.sh | bash
```

> **Security: `RAXD_BASE_URL` and `--prefix` are a trusted input.** They let you point the installer at
> any source and any directory. This is intentional (it is how the offline tests and non-root installs
> work — see the [trust model](#trust-model-v1)), but it means **you** vouch for the source. Only ever
> set `RAXD_BASE_URL` to a host you trust over HTTPS; the integrity check (below) protects against a
> corrupted or substituted *single* file in transit, not against a malicious host that serves a matching
> archive and `SHA256SUMS` together. This is accepted deviation **П-3** / residual risk **ОР-3** in the
> security requirements.

### Flags

When you run `install.sh` directly (or pass arguments through `curl | sh` with `bash -s --`), it accepts:

| Flag | Effect |
|------|--------|
| `--prefix <dir>` | Install into `<dir>` instead of the auto-detected directory (same as `RAXD_PREFIX`) |
| `--version <tag>` | Install the given version tag (same as `RAXD_VERSION`) |
| `-h`, `--help` | Print usage (variables, flags, exit codes) and exit `0` |

Passing flags through a pipe uses `bash -s --`:

```sh
curl -fsSL https://<base-url>/install.sh | bash -s -- --prefix ~/.local/bin --version v0.1.0
```

An unknown flag, or `--prefix` / `--version` with no argument, fails with exit `1` and a hint
(`error: unknown flag: <flag>` + `hint: use --help for usage information`).

### What the installer does (and only that)

`install.sh` performs exactly these steps and nothing else (no shell `eval` of downloaded content, no
extra binaries downloaded or run, no interactive prompts for a basic install, and it never starts the
daemon):

1. Detect the OS and architecture (`uname -s` / `uname -m`); reject unsupported platforms.
2. Build the artifact name and download the archive **and** `SHA256SUMS` over HTTPS (`curl -fsSL`) into
   a private `mktemp -d` directory.
3. Verify the archive's SHA-256 against `SHA256SUMS` **before** anything is placed (hard-fail on
   mismatch).
4. Unpack the archive and copy the `raxd` binary into the install directory (mode `0755`, atomic
   replace via `install`).
5. On macOS, idempotently strip the `com.apple.quarantine` attribute and print a Gatekeeper hint.
6. Check that `raxd` is reachable on `PATH`; if not, print a `PATH` hint.
7. Print a success line and a hint about `raxd service install`.

A `trap` removes the temporary directory on **any** exit (success, error, or interruption), and the
whole body runs inside a `main` function called by a single line at the very end of the file — so a
truncated download (a `curl | sh` cut short) does not run a partial install.

The installer's progress lines look like this on a successful run:

```
==> detected platform: linux/amd64
==> downloading raxd_v0.1.0_linux_amd64.tar.gz...
==> downloading SHA256SUMS...
==> verifying SHA256 integrity...
==> SHA256 verified — archive is intact
==> extracting...
==> installing to /usr/local/bin/raxd...
==> binary installed: /usr/local/bin/raxd
==> raxd installed successfully (v0.1.0)
hint: to register a system service, run: raxd service install
```

## Install path and privileges

`install.sh` does not require root unless the chosen directory does. It picks the install directory like
this (auto-detect, accepted decision ADR-003):

1. If `--prefix` or `RAXD_PREFIX` is set, install there (you choose a directory you can write to).
2. Otherwise, if `/usr/local/bin` is **writable** (or you are root), install into `/usr/local/bin`.
3. Otherwise, fall back to `~/.local/bin`.

Privilege rules:

- For a directory you can already write to, **no `sudo` is used**.
- `sudo` is requested **only** when the chosen directory is not writable and `sudo` is available — and
  the installer says so explicitly before doing it (`sudo install -m 0755 raxd <dst>`). The only thing
  it elevates for is writing the binary; it never starts or elevates the daemon.
- If the directory is not writable and `sudo` is unavailable, it fails with exit `4` and a hint to run
  as root or pass `--prefix ~/.local/bin`.

After installing, the script verifies that `raxd` is on `PATH` (`command -v raxd`). If the install
directory is not on `PATH` (common for `~/.local/bin` on a fresh host), it prints a hint to add it, for
example:

```
hint: raxd is installed but /home/user/.local/bin is not in $PATH
hint: add to ~/.bashrc or ~/.zshrc:
  export PATH="/home/user/.local/bin:$PATH"
```

The binary is installed with mode `0755`; the installer never creates world-writable files or
directories.

## Integrity verification (SHA256)

Integrity is checked **before** the binary is placed. `install.sh`:

1. Downloads `SHA256SUMS` alongside the archive (both over HTTPS, `curl -fsSL` — a download failure is a
   hard error, exit `5`, not a silent empty file).
2. Filters `SHA256SUMS` down to the line for the exact archive it downloaded. If no matching line is
   found (for example the requested `RAXD_VERSION` does not exist in that `SHA256SUMS`), it fails with
   exit `3`.
3. Runs `sha256sum -c` (Linux) or `shasum -a 256 -c` (macOS) on that line. On a **mismatch** the
   install aborts with exit `3`, the binary is **not** placed, and the temporary files are removed by the
   `trap`.

This was verified live in Docker (not just statically): substituting the downloaded archive with garbage
makes the check fail and `install.sh` exits `3` with no binary installed.

### Verifying a download manually

If you download a release archive yourself, verify it against `SHA256SUMS` before using it. The
`SHA256SUMS` file uses the native `sha256sum` format (`<hash>␣␣<file>`), which both tools accept:

```sh
# Linux (GNU coreutils)
sha256sum -c SHA256SUMS

# macOS
shasum -a 256 -c SHA256SUMS
```

A line ending in `OK` confirms the archive matches; `FAILED` means do not use it.

## Trust model (v1)

Read this before relying on `curl | sh` for a production host.

In v1, the integrity of what you install rests on **two** mechanisms:

1. **TLS (HTTPS) channel** to both the install script and the artifacts. The default `RAXD_BASE_URL` is
   `https://…`, and downloads use `curl -fsSL`, so a transport error or HTTP error is not masked as an
   empty file.
2. **SHA256 verification** of the archive against `SHA256SUMS` **before** the binary is placed
   ([above](#integrity-verification-sha256)).

What v1 deliberately does **not** have:

- **No GPG / minisign signature** of `SHA256SUMS`. There is no signing key or public-key trust
  infrastructure in v1, so `install.sh` does **not** claim to verify a signature (no false
  `gpg --verify`). This is accepted deviation **П-1** / residual risk **ОР-1**. A signature
  (a dedicated signing key, the public key distributed out-of-band, and a verify step in `install.sh`
  *before* the hash check) is to be added before any public release.

**The boundary of this model:** because `SHA256SUMS` is fetched from the **same** source as the archive,
the hash check protects you against an archive that is corrupted or substituted **in transit** or against
a single tampered file — but **not** against a compromised source that serves a coordinated, matching
archive **and** `SHA256SUMS`. That gap is exactly what a signature would close, which is why it is tracked
as **ОР-1** for the public release.

Practical implication: only set `RAXD_BASE_URL` (and `--prefix`) to sources and directories you trust
(see the [note above](#pointing-the-installer-at-an-artifact-source)).

## Manual installation

If you have a release archive and `SHA256SUMS` (downloaded from a host, or
[built from source](#building-release-artifacts-from-source)), you can install without the script:

1. Download the archive for your platform and the `SHA256SUMS` file into the same directory. Pick the
   archive matching your `uname -s` / `uname -m` from the [supported-platforms table](#supported-platforms),
   for example `raxd_v0.1.0_linux_amd64.tar.gz`.

2. Verify integrity (do **not** skip this):

   ```sh
   # Linux
   sha256sum -c SHA256SUMS
   # macOS
   shasum -a 256 -c SHA256SUMS
   ```

   If the line for your archive does not say `OK`, stop.

3. Unpack — the archive contains the binary named `raxd` (plus `README.md`; a `LICENSE` file is included
   only if one exists in the source tree — see [Note on licensing](#note-on-licensing)):

   ```sh
   tar -xzf raxd_v0.1.0_linux_amd64.tar.gz
   ```

4. Place it on your `PATH` with mode `0755`:

   ```sh
   # User directory, no sudo
   install -m 0755 raxd ~/.local/bin/raxd

   # System directory (needs root)
   sudo install -m 0755 raxd /usr/local/bin/raxd
   ```

5. If you used `~/.local/bin` and it is not on `PATH`, add it:

   ```sh
   export PATH="$HOME/.local/bin:$PATH"   # also add to ~/.bashrc or ~/.zshrc
   ```

6. On macOS, see [macOS Gatekeeper](#macos-gatekeeper--quarantine) below.

7. Confirm it works:

   ```sh
   raxd version
   # → raxd v0.1.0 (commit <commit>, built <date>)
   ```

## macOS Gatekeeper / quarantine

`raxd` is **not** notarized with an Apple Developer ID in v1 (no certificate — accepted deviation
**П-2** / residual risk **ОР-2**). Two consequences:

- **`install.sh` removes the quarantine attribute for you.** On macOS the installer runs
  `xattr -d com.apple.quarantine <dst>` against the installed binary. This is idempotent and harmless
  (a no-op if the attribute is not present — and in practice, a binary fetched via `curl` usually is
  not quarantined at all; the attribute is typically added when you download via a browser or AirDrop).
  It then prints a Gatekeeper hint:

  ```
  hint: if macOS Gatekeeper blocks raxd, run:
    xattr -d com.apple.quarantine /usr/local/bin/raxd
    or: System Settings → Privacy & Security → allow '/usr/local/bin/raxd'
  hint: for a notarized build with full Gatekeeper approval, request notarization via Apple Developer Program
  ```

- **Gatekeeper may still warn.** Without notarization you may see a "raxd cannot be opened" /
  "is damaged" warning on first run. The installer prints the hint above covering the manual fix.

To clear quarantine manually:

```sh
xattr -d com.apple.quarantine /usr/local/bin/raxd
```

Or allow it via **System Settings → Privacy & Security** (approve the blocked binary there).

> **Verification limitation (AC13 / accepted deviation П-4).** The full macOS Gatekeeper flow cannot be
> tested in Docker, because Docker is Linux-only. In CI the macOS path is checked **statically** (the
> darwin branch of `install.sh` removes quarantine and prints the instruction); the real Gatekeeper
> behaviour is verified on a live macOS host outside Docker (residual risk **ОР-4**). Proper code
> signing + notarization (`codesign` + `notarytool` + `staple`) is a separate future task, gated on an
> Apple Developer ID.

## Building release artifacts from source

Until a public host serves the artifacts, you can build all four archives + `SHA256SUMS` yourself from
the source tree. **All builds run inside Docker** (security baseline §6 — `go build` on the host is
blocked by a guard). The build is offline from the committed `vendor/` directory
(`-mod=vendor`, `CGO_ENABLED=0`).

Build everything (cross-compile the four targets, archive them, generate `SHA256SUMS`) in one Docker run:

```sh
docker run --rm \
  -v "$(pwd)/dist:/src/dist" \
  -e VERSION=v0.1.0 \
  -w /src \
  raxd-build \
  sh -c "make build-all release-all VERSION=v0.1.0"
```

(Build the `raxd-build` image first with `docker build --target build -t raxd-build .`.) The result in
`dist/` is exactly four archives plus one `SHA256SUMS`:

```
dist/raxd_v0.1.0_linux_amd64.tar.gz
dist/raxd_v0.1.0_linux_arm64.tar.gz
dist/raxd_v0.1.0_darwin_amd64.tar.gz
dist/raxd_v0.1.0_darwin_arm64.tar.gz
dist/SHA256SUMS
```

The release version is injected into the binary via ldflags, so `raxd version` reports a real value
(not the default `dev`):

```
raxd v0.1.0 (commit abc1234, built 2026-05-22)
```

### Verifying the install-flow locally

Two `Makefile` targets exercise the installer end-to-end in a clean Docker container against a local mock
HTTP source (no network, no public host needed):

```sh
# Full local CI gate: unit tests + cross-build + release-all + install-flow, all in Docker.
make ci-local VERSION=v0.1.0

# Just the install-flow test (consumes an already-built dist/).
make test-install VERSION=v0.1.0
```

`make ci-local` is the v1 verification gate: it runs `go vet` + unit tests in Docker, cross-builds the
four targets and produces the archives + `SHA256SUMS` in Docker, then runs the installer in a clean
`debian:stable-slim` container against a loopback-only mock HTTP server (`python3 -m http.server
--bind 127.0.0.1`, test image only) and confirms `raxd version`. None of these steps run `go build` on
the host. See [`development.md`](development.md) for the project layout and the Docker-only rationale.

## Exit codes

`install.sh` uses these exit codes (also shown by `install.sh --help`):

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | General error (bad flag, missing SHA tool, binary missing from archive) |
| `2` | Unsupported platform (OS or architecture) |
| `3` | SHA256 mismatch (corrupted/substituted archive, or no matching `SHA256SUMS` entry) |
| `4` | No write permission and no `sudo` for the target directory |
| `5` | Download failure (archive or `SHA256SUMS` could not be fetched) |

For what to do about each, see [Troubleshooting → Installation](troubleshooting.md#installation-installsh).

## Registering the service (not `install.sh`)

`install.sh` installs the binary only. To run `raxd` as a managed system service (autostart, restart on
failure, graceful stop, running as the unprivileged `raxd` user), use the separate command after
installing:

```sh
sudo raxd service install
```

`install.sh` prints this hint at the end of a successful install. The full service security and
operations model — non-root execution, the privileged-port capability, what `uninstall` keeps, and
audit-log rotation — is in [`service-management.md`](service-management.md). The on-disk service paths
are in [`configuration.md`](configuration.md#service-layout-system-service).

## Uninstall

There is **no uninstall script** in v1 (self-update, uninstall, and downgrade are out of scope). To
remove a manually-installed binary, delete it from wherever it was installed:

```sh
rm -f /usr/local/bin/raxd        # or ~/.local/bin/raxd, or your --prefix path
```

If you registered the system service, remove that first with the dedicated command (it also removes the
unit/plist, autostart, and journald drop-in, but intentionally keeps the inert `raxd` user and the data
directory):

```sh
sudo raxd service uninstall
```

See [`service-management.md`](service-management.md) for exactly what `uninstall` keeps and how to remove
the `raxd` user and state directory yourself if you want a zero-footprint removal.

## Note on licensing

No `LICENSE` file is present in the repository yet, and licensing terms are not defined at this stage.
The release archives include a `LICENSE` file **only if** one exists in the source tree at build time —
so today the archives ship the binary and `README.md` but no license. A license is expected to be added
before any public release.

## Related documents

- [`commands.md`](commands.md) — full command reference (`version`, `status`, the `key` group, the
  `service` group, `serve`, and the `config port` stub).
- [`service-management.md`](service-management.md) — registering and operating `raxd` as a system
  service (the step after installation).
- [`troubleshooting.md`](troubleshooting.md#installation-installsh) — installation problems and exit
  codes.
- [`development.md`](development.md) — building and testing in Docker, project layout, build metadata.
- [`mcp.md`](mcp.md) — connecting an AI agent over MCP once `raxd serve` is running.

## Author

**Vladimir Kovalev, OEM TECH** — author of raxd.
