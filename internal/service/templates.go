// Package service — template rendering for systemd unit and launchd plist.
//
// NO BUILD TAGS: this file is compiled on all platforms so that renderPlist
// is unit-testable on Linux (in Docker). AC13 requirement.
//
// SR-90: validateTemplateData must be called BEFORE any template rendering.
// Injection of newlines/control chars/special chars into template fields would
// allow an attacker to inject arbitrary systemd/launchd directives.
//
// plan.md §Contracts: renderUnit(d TemplateData) / renderPlist(d) — pure render, no I/O.
package service

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"text/template"
)

// Note on ConfigHome / StateHome (BUG-1 macOS fix):
// plistTemplateText uses {{.ConfigHome}} and {{.StateHome}} rather than hardcoded
// /etc and /var/lib so that macOS gets /usr/local/etc and /usr/local/var respectively.
// These fields are populated by TemplateDataFromConfig as filepath.Dir(ConfigDir) /
// filepath.Dir(StateDir), preserving the invariant:
//   filepath.Join(ConfigHome, "raxd") == ConfigDir
//   filepath.Join(StateHome,  "raxd") == StateDir

// ─── TemplateData (plan.md §Contracts) ───────────────────────────────────────

// TemplateData carries validated values for unit/plist template rendering.
//
// SR-90: NeedNetBindCap is a TYPED bool derived from Port < 1024.
// Conditional directives (AmbientCapabilities/NoNewPrivileges) derive from this bool,
// NOT from a raw string. This prevents injection of capability names.
type TemplateData struct {
	// ExecPath: absolute normalized path to the raxd binary.
	// Validated: filepath.IsAbs + no \n\r\x00 or control chars.
	ExecPath string

	// User: POSIX user name for the daemon process (default: "raxd").
	// Validated: ^[a-z_][a-z0-9_-]{0,31}$ (SR-90 allowlist).
	User string

	// Group: POSIX group name (default: "raxd"). Same validation as User.
	Group string

	// Label: reverse-DNS launchd job label (macOS, default: "tech.oem.raxd").
	// Validated: ^[a-z][a-z0-9._-]{0,253}$ (SR-90 allowlist).
	Label string

	// Port: TCP port raxd listens on. Range: 1..65535.
	// NeedNetBindCap is derived from Port < 1024 (ADR-003, SR-85).
	Port int

	// NeedNetBindCap: true when Port < 1024 → add AmbientCapabilities, omit NoNewPrivileges.
	// TYPED bool — not a raw string (SR-90).
	NeedNetBindCap bool

	// StateDir: absolute path for service state (/var/lib/raxd on Linux, /usr/local/var/raxd on macOS).
	StateDir string

	// ConfigDir: absolute path for configuration (/etc/raxd on Linux, /usr/local/etc/raxd on macOS).
	// FULL raxd-specific directory — NOT the XDG parent (BUG-1 fix).
	ConfigDir string

	// LogPath: absolute path for log directory (macOS only; Linux uses journald).
	LogPath string

	// ConfigHome: parent of ConfigDir, used as XDG_CONFIG_HOME in the launchd plist.
	// Invariant: filepath.Join(ConfigHome, "raxd") == ConfigDir.
	// Linux: /etc; macOS: /usr/local/etc (BUG-1 macOS fix).
	ConfigHome string

	// StateHome: parent of StateDir, used as XDG_STATE_HOME in the launchd plist.
	// Invariant: filepath.Join(StateHome, "raxd") == StateDir.
	// Linux: /var/lib; macOS: /usr/local/var (BUG-1 macOS fix).
	StateHome string
}

// ─── Validation regexps (SR-90) ───────────────────────────────────────────────

var (
	// posixNameRe matches valid POSIX user/group names: ^[a-z_][a-z0-9_-]{0,31}$
	// SR-90: allowlist; no spaces, \n, \r, =, quotes, control chars.
	posixNameRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

	// labelRe matches reverse-DNS launchd labels: ^[a-z][a-z0-9._-]{0,253}$
	labelRe = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,253}$`)
)

// hasControlChar returns true if s contains any ASCII control character (\x00-\x1f, \x7f).
// SR-90: control chars (including \n, \r) are forbidden in all template fields.
func hasControlChar(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// ValidateTemplateData validates all fields in d against the SR-90 allowlists.
// Returns an error if any field is invalid; the error message identifies the offending field.
// This MUST be called before renderUnit/renderPlist. Both renderers call it internally.
func ValidateTemplateData(d TemplateData) error {
	// User
	if !posixNameRe.MatchString(d.User) {
		return fmt.Errorf("invalid User %q: must match ^[a-z_][a-z0-9_-]{0,31}$ (SR-90)", d.User)
	}

	// Group
	if !posixNameRe.MatchString(d.Group) {
		return fmt.Errorf("invalid Group %q: must match ^[a-z_][a-z0-9_-]{0,31}$ (SR-90)", d.Group)
	}

	// Label
	if !labelRe.MatchString(d.Label) {
		return fmt.Errorf("invalid Label %q: must match ^[a-z][a-z0-9._-]{0,253}$ (SR-90)", d.Label)
	}

	// ExecPath: must be absolute, normalized, no control chars.
	if d.ExecPath == "" {
		return fmt.Errorf("ExecPath is empty (SR-90)")
	}
	if !filepath.IsAbs(d.ExecPath) {
		return fmt.Errorf("ExecPath %q is not absolute (SR-90)", d.ExecPath)
	}
	if hasControlChar(d.ExecPath) {
		return fmt.Errorf("ExecPath %q contains control characters (SR-90)", d.ExecPath)
	}
	// Normalize and verify not changed (no ".." or double slashes that could escape).
	cleaned := filepath.Clean(d.ExecPath)
	if cleaned != d.ExecPath {
		return fmt.Errorf("ExecPath %q is not normalized (cleaned: %q) (SR-90)", d.ExecPath, cleaned)
	}

	// Port: 1..65535
	if d.Port < 1 || d.Port > 65535 {
		return fmt.Errorf("Port %d is out of range 1..65535 (SR-90)", d.Port)
	}

	// StateDir, ConfigDir, LogPath, ConfigHome, StateHome: absolute, no control chars.
	for _, field := range []struct {
		name string
		val  string
	}{
		{"StateDir", d.StateDir},
		{"ConfigDir", d.ConfigDir},
		{"LogPath", d.LogPath},
		{"ConfigHome", d.ConfigHome},
		{"StateHome", d.StateHome},
	} {
		if field.val == "" {
			return fmt.Errorf("%s is empty (SR-90)", field.name)
		}
		if !filepath.IsAbs(field.val) {
			return fmt.Errorf("%s %q is not absolute (SR-90)", field.name, field.val)
		}
		if hasControlChar(field.val) {
			return fmt.Errorf("%s %q contains control characters (SR-90)", field.name, field.val)
		}
	}

	return nil
}

// ─── Systemd unit template (service-design.md §2.3) ─────────────────────────

// unitTemplateText is the systemd unit template (service-design.md §2.3).
// Conditional capability block driven by NeedNetBindCap bool (SR-90, ADR-003).
const unitTemplateText = `# /etc/systemd/system/raxd.service
# Generated by: raxd service install. Do not edit manually.
# Regenerate: raxd service install (idempotent, AC9).

[Unit]
Description=raxd — Remote Access Daemon for AI agents (OEM TECH)
After=network.target
Documentation=https://github.com/vladimirvkhs/raxd

[Service]
Type=exec
ExecStart={{.ExecPath}} serve
User={{.User}}
Group={{.Group}}

# Restart on failure only (AC4); graceful SIGTERM → clean exit → no restart (AC5).
Restart=on-failure
RestartSec=2s

# State directory: systemd creates /var/lib/raxd with owner=raxd before start.
# StateDirectoryMode=0700 is EXPLICIT — default is 0755, which is wider than baseline §2.
StateDirectory=raxd
StateDirectoryMode=0700

# Config directory: systemd creates /etc/raxd owned by raxd BEFORE ExecStart (BUG-1 fix).
# Without this, config.EnsureDirs → MkdirAll(/etc/raxd) fails under ProtectSystem=strict.
# ConfigurationDirectoryMode=0700 is EXPLICIT per SR-89 baseline.
ConfigurationDirectory=raxd
ConfigurationDirectoryMode=0700

# Path environment: overrides XDG defaults so internal/config/paths.go resolves
# /etc/raxd (ConfigDir) and /var/lib/raxd (StateDir) without code changes (ADR-002).
Environment=XDG_CONFIG_HOME=/etc
Environment=XDG_STATE_HOME=/var/lib
Environment=HOME=/var/lib/raxd

# Journal: stderr → journald (explicit per service-design.md §2.1, AC8 documentation).
StandardOutput=journal
StandardError=journal
SyslogIdentifier=raxd
{{- if .NeedNetBindCap}}

# Ambient capability: present ONLY when Port<1024 (NeedNetBindCap=true, ADR-003).
# The hardening directive for privilege escalation is omitted here (ADR-003, accepted deviation П-1).
# Remaining hardening PRESERVED (SR-87).
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE
{{- else}}

# Full hardening for default port >= 1024.
NoNewPrivileges=yes
{{- end}}
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
`

var unitTmpl = template.Must(template.New("unit").Parse(unitTemplateText))

// RenderUnit generates a systemd unit file from d.
// Calls ValidateTemplateData first — returns error if any field is invalid (SR-90).
// No I/O: returns the rendered string or an error. Does NOT write any file.
func RenderUnit(d TemplateData) (string, error) {
	if err := ValidateTemplateData(d); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := unitTmpl.Execute(&buf, d); err != nil {
		return "", fmt.Errorf("unit template execution failed: %w", err)
	}
	return buf.String(), nil
}

// ─── launchd plist template (service-design.md §4) ───────────────────────────

// plistTemplateText is the launchd plist template (service-design.md §4).
// AC13: this template is in templates.go (no build tag) so it compiles and is
// unit-testable on Linux inside Docker.
const plistTemplateText = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
    "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- /Library/LaunchDaemons/{{.Label}}.plist -->
<!-- Generated by: raxd service install. Do not edit manually. -->
<plist version="1.0">
<dict>
    <!-- Required: unique job identifier (reverse DNS) -->
    <key>Label</key>
    <string>{{.Label}}</string>

    <!-- Command: raxd serve (AC1) -->
    <key>ProgramArguments</key>
    <array>
        <string>{{.ExecPath}}</string>
        <string>serve</string>
    </array>

    <!-- Autostart at load/boot (AC3) -->
    <key>RunAtLoad</key>
    <true/>

    <!-- Restart on failure only: SuccessfulExit=false means restart when exit!=0.
         Graceful stop returns code 0 → NOT restarted (AC5).
         Kill / panic → exit!=0 → restarted (AC4). -->
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>

    <!-- Non-root execution: launchd starts as root, drops to UserName (AC6, SR-83) -->
    <key>UserName</key>
    <string>{{.User}}</string>

    <key>GroupName</key>
    <string>{{.Group}}</string>

    <!-- Environment: XDG paths so internal/config/paths.go resolves system dirs
         without code changes (ADR-002). HOME needed as fallback in paths.go.
         ConfigHome=filepath.Dir(ConfigDir), StateHome=filepath.Dir(StateDir) —
         invariant: XDG_CONFIG_HOME + "/raxd" == ConfigDir (BUG-1 macOS fix). -->
    <key>EnvironmentVariables</key>
    <dict>
        <key>XDG_CONFIG_HOME</key>
        <string>{{.ConfigHome}}</string>
        <key>XDG_STATE_HOME</key>
        <string>{{.StateHome}}</string>
        <key>HOME</key>
        <string>{{.StateDir}}</string>
    </dict>

    <!-- Working directory -->
    <key>WorkingDirectory</key>
    <string>{{.StateDir}}</string>

    <!-- Log output (macOS has no journald; rotation via newsyslog, see ops docs) -->
    <key>StandardOutPath</key>
    <string>{{.LogPath}}/raxd.log</string>

    <key>StandardErrorPath</key>
    <string>{{.LogPath}}/raxd.log</string>
</dict>
</plist>
`

var plistTmpl = template.Must(template.New("plist").Parse(plistTemplateText))

// RenderPlist generates a launchd plist from d.
// Calls ValidateTemplateData first — returns error if any field is invalid (SR-90).
// No I/O: returns the rendered string or an error. Does NOT write any file.
// AC13: this function is compiled on Linux and unit-testable in Docker.
func RenderPlist(d TemplateData) (string, error) {
	if err := ValidateTemplateData(d); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := plistTmpl.Execute(&buf, d); err != nil {
		return "", fmt.Errorf("plist template execution failed: %w", err)
	}
	return buf.String(), nil
}

// ─── journald drop-in template (service-design.md §3) ────────────────────────

// journaldDropInContent is the journald drop-in for log size limiting (ADR-004, SR-94).
// Values: SystemMaxUse=200M, SystemMaxFileSize=50M (production defaults).
const journaldDropInContent = `# /etc/systemd/journald.conf.d/raxd.conf
# Installed by: raxd service install
# Removed by:   raxd service uninstall (SR-93)
# Purpose: limit audit log growth (closes command-exec OR-2 / file-upload OR-U4)
#
# NOTE: journald limits are per-host (global), not per-unit (ADR-004, П-3).

[Journal]
SystemMaxUse=200M
SystemMaxFileSize=50M
`

// JournaldDropIn returns the content of the journald drop-in configuration.
// The caller (systemdManager.Install) writes this to the filesystem.
func JournaldDropIn() string {
	return journaldDropInContent
}

// ─── Helper: template data from Config ───────────────────────────────────────

// TemplateDataFromConfig constructs a TemplateData from a Config.
// NeedNetBindCap is derived from Port < 1024 (ADR-003, SR-90 typed bool).
// ConfigHome and StateHome are filepath.Dir(ConfigDir) / filepath.Dir(StateDir):
//
//	Linux:  ConfigHome=/etc,            StateHome=/var/lib
//	macOS:  ConfigHome=/usr/local/etc,  StateHome=/usr/local/var
//
// Invariant E: filepath.Join(ConfigHome,"raxd") == ConfigDir (BUG-1 macOS fix).
func TemplateDataFromConfig(cfg Config) TemplateData {
	return TemplateData{
		ExecPath:       cfg.ExecPath,
		User:           cfg.User,
		Group:          cfg.Group,
		Label:          cfg.Label,
		Port:           cfg.Port,
		NeedNetBindCap: cfg.Port < 1024,
		StateDir:       cfg.StateDir,
		ConfigDir:      cfg.ConfigDir,
		LogPath:        cfg.LogPath,
		ConfigHome:     filepath.Dir(cfg.ConfigDir),
		StateHome:      filepath.Dir(cfg.StateDir),
	}
}

// ─── Helper: neutral error message (SR-95) ───────────────────────────────────

// neutralizeStderr returns a neutral failure message regardless of the raw stderr content.
// SR-95: raw stderr from systemctl/launchctl must NOT be propagated to user output.
//
// The raw argument is intentionally ignored — any content from the OS manager
// (paths, error text, PEM fragments) must not appear in user-facing error messages.
// Callers wrap this return value in a typed ServiceError.
func neutralizeStderr(_ string) string {
	return "manager command failed"
}
