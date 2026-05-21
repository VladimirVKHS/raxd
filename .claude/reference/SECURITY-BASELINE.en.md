# raxd — Security Baseline (mandatory contract)

> By design `raxd` runs arbitrary commands over the network — a powerful tool (like SSH).
> This checklist is a contract, not a wish. `security` owns it; `developer`/`system-dev`/`devops`
> must implement it; `reviewer` and `security-guardian` verify it item by item. Any deviation
> goes through escalation and is recorded in `threat-model.md`.

## 1. API-key authentication

- [ ] Generation: `crypto/rand`, ≥ 128 bits entropy (16+ bytes). Never `math/rand`.
- [ ] Format: identifiable prefix (`rax_live_…`) + base64url/hex body.
- [ ] Storage: never plaintext. Store `sha256(key + per-key-salt)` plus the salt.
      (SHA-256 suffices given high key entropy; bcrypt/argon2 are overkill per request.)
- [ ] Comparison: constant-time ONLY — `crypto/subtle.ConstantTimeCompare` / `hmac.Equal`.
      No `==`/`strings.EqualFold` on secrets (timing attack).
- [ ] Key shown to the user once on `key create`; `key list` exposes only id/label/metadata.
- [ ] Revocation (`key delete`) is immediate: mark revoked, clear cache, further requests → 401/403.

## 2. Transport

- [ ] TLS mandatory. `MinVersion: tls.VersionTLS13` (1.2 acceptable as legacy floor).
- [ ] Self-signed cert generated at install/first run (`crypto/x509`); key `0600`.
- [ ] mTLS optional, for machine-to-machine (`tls.RequireAndVerifyClientCert`).
- [ ] For the HTTP/MCP endpoint: validate `Origin` header (DNS-rebinding protection).
- [ ] Bind a deliberate interface by default; if local-only, bind `127.0.0.1`.

## 3. Command execution (most dangerous)

- [ ] Run via `exec.Command(bin, args...)` WITHOUT shell interpolation. Never `sh -c <user-input>`.
- [ ] Per-command timeout (e.g. 30s default, configurable) via `context`.
- [ ] Optional command allowlist: strict matching (not regex), off by default but
      architecturally provided and documented.
- [ ] Daemon runs NON-root: dedicated system user; for ports <1024 use Linux capabilities
      (`CAP_NET_BIND_SERVICE`), not setuid root.
- [ ] Command working dir/environment are constrained and predictable.

## 4. Audit and resilience

- [ ] Audit-log EVERY action: timestamp, key fingerprint (not the key), command+args, exit code,
      duration, remote address. Structured (JSON), rotated.
- [ ] Log failed auth and anomalies (spikes of denials).
- [ ] Rate limiting: per-key and per-IP (`golang.org/x/time/rate`, token bucket), 429 on excess.
- [ ] Graceful restart on crash (`Restart=on-failure` in unit/plist).
- [ ] No secrets in logs, default CLI output, or commit history.

## 5. Distribution

- [ ] Install script: `set -euo pipefail`, body wrapped in a function (partial-download safety),
      `trap` to clean temp.
- [ ] Verify `SHA256SUMS` of the downloaded binary (minimum; GPG a plus).
- [ ] macOS: sign + notarize for proper distribution; minimum — strip quarantine
      (`xattr -d com.apple.quarantine`) with clear user guidance.

## 6. Dev / test / run environment — Docker only

- [ ] Build, tests (`go build`/`go test`), running the `raxd` daemon and any manual runs of the
      utility happen ONLY inside a Docker container. `raxd` is never run on the host: it executes
      arbitrary commands, so it belongs in an isolated container.
- [ ] Install-flow (`curl | sh`) is tested in a clean Linux container (debian/ubuntu) — a
      fresh-server simulation.
- [ ] The repo ships a `Dockerfile` (and/or `docker-compose.yml`/devcontainer) for a reproducible
      dev/test environment; test commands are documented as docker commands.
- [ ] CI runs build and tests in a container.
- Note: service-integration tests (systemd) run in a systemd-capable container; the developer's
      host never has the service installed.

## Escalation

If an item is infeasible in the chosen architecture, do NOT silently skip it: record it in
`threat-model.md` (risk + why + mitigation) and escalate to the user before release.
