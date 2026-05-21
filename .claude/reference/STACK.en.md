# raxd — Technology Stack (source of truth)

> Shared stack contract for the whole agent team. Architect, system-dev, developer, devops,
> mcp-engineer and cli-ux MUST read this before working and MUST NOT introduce other
> dependencies without justifying it in `plan.md` (Trade-offs). Versions target 2025-2026;
> re-check via research-analyst when in doubt.

## Product

**`raxd`** — Remote Access daemon. A single Go binary that is simultaneously:
- a system service (systemd on Linux, launchd on macOS);
- a CLI utility (`raxd <command>`);
- a network server (TCP + TLS);
- an MCP server for AI agents.

Platforms: **macOS + Linux**, architectures **amd64 + arm64**. Windows out of scope.
Author: **Vladimir Kovalev, OEM TECH**.

## Core libraries

| Purpose | Choice | Status / version | URL |
|---|---|---|---|
| CLI + subcommands | `spf13/cobra` | v1.10.x, active | https://github.com/spf13/cobra |
| Cross-platform service | `kardianos/service` (+ unit/plist generation) | maintained | https://github.com/kardianos/service |
| Output styling | `charmbracelet/lipgloss` (v2) | stable v2.0.x — import `charm.land/lipgloss/v2`; path `github.com/charmbracelet/lipgloss/v2` is beta | https://github.com/charmbracelet/lipgloss |
| Logging (color, human) | `charmbracelet/log` | active | https://github.com/charmbracelet/log |
| Tables (key list, etc.) | `olekukonko/tablewriter` | maintained | https://github.com/olekukonko/tablewriter |
| Build/release (matrix) | `goreleaser` v2 | v2.x, active | https://goreleaser.com |
| Config paths (XDG) | manual resolution via `os.Getenv` (stdlib) | `adrg/xdg` NOT used: its macOS default `~/Library/Application Support` conflicts with the single `~/.config/raxd` (D3) | — |
| Configuration | `spf13/viper` | maintained | https://github.com/spf13/viper |
| TLS / certificates | `crypto/tls`, `crypto/x509` (stdlib) | Go 1.22+ | https://pkg.go.dev/crypto/tls |
| Rate limiting | `golang.org/x/time/rate` | stdlib-ext | https://pkg.go.dev/golang.org/x/time/rate |
| MCP server | `github.com/modelcontextprotocol/go-sdk/mcp` | official, v1.x | https://github.com/modelcontextprotocol/go-sdk |

## On-disk layout

- **Config**: `$XDG_CONFIG_HOME/raxd/config.yaml`, else `~/.config/raxd/config.yaml` — single path on Linux and macOS (decision D3; macOS Application Support not used).
- **State/keys**: `$XDG_STATE_HOME/raxd/keys.db` (or equivalent), perms **`0600`**.
- **TLS**: cert `0644`, private key `0600`.
- **Logs**: system journal (journald/syslog) + rotation for file output.

## Cross-compilation (goreleaser)

Matrix: `GOOS={linux,darwin} × GOARCH={amd64,arm64}` → 4 binaries
`raxd_{linux,darwin}_{amd64,arm64}` + archives (`.tar.gz`) + `SHA256SUMS`.
`CGO_ENABLED=0` (static build, simple distribution).

## Install (`curl | sh`)

Script: detect `uname -s`→{linux,darwin}, `uname -m`→{amd64,arm64}; download the right
archive; verify SHA256; install to `/usr/local/bin/raxd` (`0755`); generate and register the
service (systemd unit / launchd plist); on macOS remove `com.apple.quarantine`; print a
beautiful status block (see ux-spec) with app info, author and example commands.

## CLI commands (first-iteration contract)

- `raxd key create [--name <label>]` — issue an API key (shown once).
- `raxd key delete <id>` — revoke a key.
- `raxd key list` — table of keys (id, label, created, last-used).
- `raxd config port <PORT>` — set the listening port.
- (service) `raxd status`, `raxd version`, `raxd serve` (run the service).

Security details in `SECURITY-BASELINE.en.md`; MCP details in `MCP-INTEGRATION.en.md`.
