// Package service_test — unit-tests for template rendering and anti-injection validation.
//
// AC13: plist generator is tested on Linux (no build tags on templates.go).
// SR-90: every injection vector is tested before render.
// No build tags on this file — runs on any platform (Linux Docker).
package service_test

import (
	"strings"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/service"
)

// validData returns a minimal valid TemplateData for Linux (port >= 1024).
// ConfigHome and StateHome are filepath.Dir of the respective full paths —
// mirrors what TemplateDataFromConfig computes (BUG-1 invariant E).
func validData() service.TemplateData {
	return service.TemplateData{
		ExecPath:       "/usr/local/bin/raxd",
		User:           "raxd",
		Group:          "raxd",
		Label:          "tech.oem.raxd",
		Port:           7822,
		StateDir:       "/var/lib/raxd",
		ConfigDir:      "/etc/raxd",
		LogPath:        "/var/log/raxd",
		ConfigHome:     "/etc",       // filepath.Dir("/etc/raxd")
		StateHome:      "/var/lib",   // filepath.Dir("/var/lib/raxd")
		NeedNetBindCap: false,
	}
}

// validDataDarwin returns TemplateData built from the darwin defaults.
// AC13: constructed directly (not via New()) so tests run on Linux in Docker.
func validDataDarwin() service.TemplateData {
	cfg := service.DefaultConfigForGOOS("darwin")
	cfg.ExecPath = "/usr/local/bin/raxd"
	return service.TemplateDataFromConfig(cfg)
}

// ─── renderUnit tests ─────────────────────────────────────────────────────────

// TestRenderUnit_DefaultPort verifies systemd unit for port >= 1024:
// - NoNewPrivileges=yes present
// - No AmbientCapabilities
// - Hardening directives present (SR-87)
// - StateDirectoryMode=0700 explicit (SR-89)
// - StandardError=journal explicit (service-design.md §2.1)
func TestRenderUnit_DefaultPort(t *testing.T) {
	d := validData()
	out, err := service.RenderUnit(d)
	if err != nil {
		t.Fatalf("RenderUnit failed: %v", err)
	}

	mustContain := []string{
		"NoNewPrivileges=yes",
		"ProtectSystem=strict",
		"ProtectHome=yes",
		"PrivateTmp=yes",
		"StateDirectoryMode=0700",
		// BUG-1 fix: systemd must pre-create /etc/raxd owned by raxd before ExecStart.
		"ConfigurationDirectory=raxd",
		"ConfigurationDirectoryMode=0700",
		"StandardError=journal",
		"ExecStart=/usr/local/bin/raxd serve",
		"User=raxd",
		"Group=raxd",
		"Restart=on-failure",
		"WantedBy=multi-user.target",
		"Environment=XDG_CONFIG_HOME=/etc",
		"Environment=XDG_STATE_HOME=/var/lib",
		"Environment=HOME=/var/lib/raxd",
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("unit missing %q\nGot:\n%s", want, out)
		}
	}

	mustAbsent := []string{
		"AmbientCapabilities",
		"CapabilityBoundingSet",
	}
	for _, absent := range mustAbsent {
		if strings.Contains(out, absent) {
			t.Errorf("unit must NOT contain %q for port >= 1024\nGot:\n%s", absent, out)
		}
	}
}

// TestRenderUnit_PrivilegedPort verifies systemd unit for port < 1024 (SR-85, SR-86, ADR-003):
// - AmbientCapabilities=CAP_NET_BIND_SERVICE present
// - CapabilityBoundingSet=CAP_NET_BIND_SERVICE present
// - NoNewPrivileges NOT present (П-1)
// - Hardening directives still present (SR-87)
func TestRenderUnit_PrivilegedPort(t *testing.T) {
	d := validData()
	d.Port = 443
	d.NeedNetBindCap = true

	out, err := service.RenderUnit(d)
	if err != nil {
		t.Fatalf("RenderUnit failed: %v", err)
	}

	mustContain := []string{
		"AmbientCapabilities=CAP_NET_BIND_SERVICE",
		"CapabilityBoundingSet=CAP_NET_BIND_SERVICE",
		"ProtectSystem=strict",
		"ProtectHome=yes",
		"PrivateTmp=yes",
		// BUG-1 fix: ConfigurationDirectory must be present in both unit variants.
		"ConfigurationDirectory=raxd",
		"ConfigurationDirectoryMode=0700",
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("unit (priv port) missing %q\nGot:\n%s", want, out)
		}
	}

	// NoNewPrivileges must be absent when AmbientCapabilities is set (ADR-003, П-1).
	if strings.Contains(out, "NoNewPrivileges") {
		t.Errorf("unit (priv port) must NOT contain NoNewPrivileges\nGot:\n%s", out)
	}
}

// TestRenderUnit_NoOtherCaps ensures only CAP_NET_BIND_SERVICE is ever set, never CAP_SYS_ADMIN etc.
// SR-85: "НЕ полный root, НЕ setuid-root, НЕ иные capability"
func TestRenderUnit_NoOtherCaps(t *testing.T) {
	forbidden := []string{
		"CAP_SYS_ADMIN", "CAP_NET_ADMIN", "CAP_SYS_PTRACE",
		"setuid", "User=root",
	}

	for _, port := range []int{80, 443, 7822, 8080} {
		d := validData()
		d.Port = port
		d.NeedNetBindCap = port < 1024

		out, err := service.RenderUnit(d)
		if err != nil {
			t.Fatalf("RenderUnit (port=%d) failed: %v", port, err)
		}
		for _, f := range forbidden {
			if strings.Contains(out, f) {
				t.Errorf("unit (port=%d) must NOT contain %q\nGot:\n%s", port, f, out)
			}
		}
	}
}

// ─── renderPlist tests (AC13 — Linux-testable) ────────────────────────────────

// TestRenderPlist_Structure verifies plist content for macOS (AC13: tested on Linux).
func TestRenderPlist_Structure(t *testing.T) {
	d := validData()
	out, err := service.RenderPlist(d)
	if err != nil {
		t.Fatalf("RenderPlist failed: %v", err)
	}

	mustContain := []string{
		"<key>Label</key>",
		"<string>tech.oem.raxd</string>",
		"<key>ProgramArguments</key>",
		"<string>/usr/local/bin/raxd</string>",
		"<string>serve</string>",
		"<key>RunAtLoad</key>",
		"<true/>",
		"<key>KeepAlive</key>",
		"<key>SuccessfulExit</key>",
		"<false/>",
		"<key>UserName</key>",
		"<string>raxd</string>",
		"<key>GroupName</key>",
		"<key>XDG_CONFIG_HOME</key>",
		"<key>XDG_STATE_HOME</key>",
		"<key>HOME</key>",
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("plist missing %q\nGot:\n%s", want, out)
		}
	}
}

// TestRenderPlist_KeepAliveSuccessfulExitFalse validates AC4/AC5 contract:
// KeepAlive.SuccessfulExit=false means restart only on non-zero exit.
// This is distinct from just checking presence of <false/>.
func TestRenderPlist_KeepAliveSuccessfulExitFalse(t *testing.T) {
	d := validData()
	out, err := service.RenderPlist(d)
	if err != nil {
		t.Fatalf("RenderPlist failed: %v", err)
	}

	// The SuccessfulExit key must be followed by <false/>, not <true/>.
	idx := strings.Index(out, "<key>SuccessfulExit</key>")
	if idx < 0 {
		t.Fatalf("plist missing SuccessfulExit key\nGot:\n%s", out)
	}
	after := out[idx:]
	if !strings.Contains(after[:100], "<false/>") {
		t.Errorf("SuccessfulExit must be <false/> (AC4/AC5), got segment:\n%s", after[:100])
	}
}

// ─── validateTemplateData tests (SR-90 anti-injection) ───────────────────────

// TestValidateTemplateData_Valid confirms valid data passes without error.
func TestValidateTemplateData_Valid(t *testing.T) {
	d := validData()
	if err := service.ValidateTemplateData(d); err != nil {
		t.Errorf("expected no error for valid data, got: %v", err)
	}
}

// TestValidateTemplateData_UserInjection tests SR-90 injection vectors for User field.
func TestValidateTemplateData_UserInjection(t *testing.T) {
	vectors := []struct {
		name string
		user string
	}{
		{"newline injection", "raxd\nExecStart=/bin/sh"},
		{"equals sign", "raxd=admin"},
		{"space", "raxd admin"},
		{"double quote", `raxd"root`},
		{"single quote", "raxd'root"},
		{"carriage return", "raxd\rroot"},
		{"control char", "raxd\x00root"},
		{"empty", ""},
		{"too long", strings.Repeat("a", 33)},
		{"starts with digit", "1raxd"},
		{"uppercase", "Raxd"},
	}

	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			d := validData()
			d.User = v.user
			err := service.ValidateTemplateData(d)
			if err == nil {
				t.Errorf("expected error for User=%q, got nil", v.user)
			}
		})
	}
}

// TestValidateTemplateData_ExecPathInjection tests SR-90 injection vectors for ExecPath.
func TestValidateTemplateData_ExecPathInjection(t *testing.T) {
	vectors := []struct {
		name string
		path string
	}{
		{"newline injection", "/usr/bin/raxd\nUser=root"},
		{"carriage return", "/usr/bin/raxd\rroot"},
		{"relative path", "usr/bin/raxd"},
		{"empty", ""},
		{"control char", "/usr/bin/raxd\x01"},
	}

	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			d := validData()
			d.ExecPath = v.path
			err := service.ValidateTemplateData(d)
			if err == nil {
				t.Errorf("expected error for ExecPath=%q, got nil", v.path)
			}
		})
	}
}

// TestValidateTemplateData_LabelInjection tests SR-90 injection for Label.
func TestValidateTemplateData_LabelInjection(t *testing.T) {
	vectors := []struct {
		name  string
		label string
	}{
		{"newline injection", "tech.oem.raxd\nUserName=root"},
		{"space", "tech oem raxd"},
		{"empty", ""},
		{"control char", "tech.oem.raxd\x00"},
	}

	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			d := validData()
			d.Label = v.label
			err := service.ValidateTemplateData(d)
			if err == nil {
				t.Errorf("expected error for Label=%q, got nil", v.label)
			}
		})
	}
}

// TestValidateTemplateData_PortRange tests SR-90 port validation: 1..65535.
func TestValidateTemplateData_PortRange(t *testing.T) {
	invalid := []int{0, -1, 65536, 99999, -1024}
	for _, p := range invalid {
		d := validData()
		d.Port = p
		if err := service.ValidateTemplateData(d); err == nil {
			t.Errorf("expected error for Port=%d, got nil", p)
		}
	}

	valid := []int{1, 80, 443, 1024, 7822, 65535}
	for _, p := range valid {
		d := validData()
		d.Port = p
		d.NeedNetBindCap = p < 1024
		if err := service.ValidateTemplateData(d); err != nil {
			t.Errorf("expected no error for Port=%d, got: %v", p, err)
		}
	}
}

// TestValidateTemplateData_StateDirInjection tests injection in StateDir/ConfigDir/LogPath.
func TestValidateTemplateData_StateDirInjection(t *testing.T) {
	d := validData()
	d.StateDir = "/var/lib/raxd\nUser=root"
	if err := service.ValidateTemplateData(d); err == nil {
		t.Error("expected error for StateDir with newline injection, got nil")
	}

	d2 := validData()
	d2.ConfigDir = "/etc\rroot"
	if err := service.ValidateTemplateData(d2); err == nil {
		t.Error("expected error for ConfigDir with CR injection, got nil")
	}

	d3 := validData()
	d3.LogPath = "/var/log\x01raxd"
	if err := service.ValidateTemplateData(d3); err == nil {
		t.Error("expected error for LogPath with control char, got nil")
	}
}

// TestRenderUnit_InjectionRejectedBeforeRender verifies that injection is caught
// before any template is rendered — the poisoned directive must NOT appear in output.
// SR-90: «невалидное значение → ошибка ДО записи артефакта»
func TestRenderUnit_InjectionRejectedBeforeRender(t *testing.T) {
	d := validData()
	d.User = "raxd\nExecStart=/bin/sh"

	out, err := service.RenderUnit(d)
	if err == nil {
		t.Errorf("RenderUnit must return error for injected User, got nil")
	}
	if strings.Contains(out, "/bin/sh") {
		t.Errorf("injected directive appeared in output despite error")
	}
}

// TestRenderPlist_InjectionRejectedBeforeRender same check for plist.
func TestRenderPlist_InjectionRejectedBeforeRender(t *testing.T) {
	d := validData()
	d.ExecPath = "/usr/local/bin/raxd\nUser=root"

	out, err := service.RenderPlist(d)
	if err == nil {
		t.Errorf("RenderPlist must return error for injected ExecPath, got nil")
	}
	if strings.Contains(out, "User=root") {
		t.Errorf("injected directive appeared in plist output despite error")
	}
}

// ─── BUG-1 macOS path invariant tests (AC13 — Linux-testable) ────────────────

// TestPlist_DarwinXDGPaths verifies that the launchd plist rendered with darwin
// defaults contains macOS-correct XDG_CONFIG_HOME and XDG_STATE_HOME values.
//
// AC13: constructed via validDataDarwin() (uses DefaultConfigForGOOS("darwin")),
// NOT via New(), so this test runs on Linux in Docker without dispatching to darwin.
func TestPlist_DarwinXDGPaths(t *testing.T) {
	d := validDataDarwin()
	out, err := service.RenderPlist(d)
	if err != nil {
		t.Fatalf("RenderPlist (darwin) failed: %v", err)
	}

	// macOS XDG homes must NOT be the Linux values.
	if strings.Contains(out, "<string>/etc</string>") {
		t.Errorf("darwin plist must not contain Linux XDG_CONFIG_HOME /etc\nGot:\n%s", out)
	}
	if strings.Contains(out, "<string>/var/lib</string>") {
		t.Errorf("darwin plist must not contain Linux XDG_STATE_HOME /var/lib\nGot:\n%s", out)
	}

	// macOS XDG homes must be the correct values.
	mustContain := []string{
		"<string>/usr/local/etc</string>",   // XDG_CONFIG_HOME for darwin
		"<string>/usr/local/var</string>",   // XDG_STATE_HOME for darwin
		"<string>/usr/local/var/raxd</string>", // HOME / WorkingDirectory
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("darwin plist missing %q\nGot:\n%s", want, out)
		}
	}
}

// TestDefaultConfigForGOOS_Paths verifies that platform-specific Config paths are correct.
//
// Linux regress: ConfigDir=/etc/raxd, StateDir=/var/lib/raxd.
// Darwin new:    ConfigDir=/usr/local/etc/raxd, StateDir=/usr/local/var/raxd.
func TestDefaultConfigForGOOS_Paths(t *testing.T) {
	cases := []struct {
		goos            string
		wantConfigDir   string
		wantStateDir    string
		wantLogPath     string
	}{
		{
			goos:          "linux",
			wantConfigDir: "/etc/raxd",
			wantStateDir:  "/var/lib/raxd",
			wantLogPath:   "/var/log/raxd",
		},
		{
			goos:          "darwin",
			wantConfigDir: "/usr/local/etc/raxd",
			wantStateDir:  "/usr/local/var/raxd",
			wantLogPath:   "/usr/local/var/log/raxd",
		},
	}

	for _, tc := range cases {
		cfg := service.DefaultConfigForGOOS(tc.goos)
		if cfg.ConfigDir != tc.wantConfigDir {
			t.Errorf("GOOS=%s ConfigDir = %q, want %q", tc.goos, cfg.ConfigDir, tc.wantConfigDir)
		}
		if cfg.StateDir != tc.wantStateDir {
			t.Errorf("GOOS=%s StateDir = %q, want %q", tc.goos, cfg.StateDir, tc.wantStateDir)
		}
		if cfg.LogPath != tc.wantLogPath {
			t.Errorf("GOOS=%s LogPath = %q, want %q", tc.goos, cfg.LogPath, tc.wantLogPath)
		}
	}
}

// TestTemplateDataFromConfig_InvariantE verifies that the XDG-home invariant holds
// for both linux and darwin configs (BUG-1 invariant E):
//
//	filepath.Join(ConfigHome, "raxd") == ConfigDir
//	filepath.Join(StateHome,  "raxd") == StateDir
func TestTemplateDataFromConfig_InvariantE(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		cfg := service.DefaultConfigForGOOS(goos)
		cfg.ExecPath = "/usr/local/bin/raxd"
		td := service.TemplateDataFromConfig(cfg)

		wantConfigDir := td.ConfigHome + "/raxd"
		if wantConfigDir != td.ConfigDir {
			t.Errorf("GOOS=%s invariant E violated: ConfigHome(%q)+/raxd = %q, ConfigDir = %q",
				goos, td.ConfigHome, wantConfigDir, td.ConfigDir)
		}

		wantStateDir := td.StateHome + "/raxd"
		if wantStateDir != td.StateDir {
			t.Errorf("GOOS=%s invariant E violated: StateHome(%q)+/raxd = %q, StateDir = %q",
				goos, td.StateHome, wantStateDir, td.StateDir)
		}
	}
}

// TestPlist_LinuxXDGPathsRegress is a regression test: Linux plist still uses
// /etc and /var/lib for XDG homes (unchanged from before the BUG-1 macOS fix).
func TestPlist_LinuxXDGPathsRegress(t *testing.T) {
	d := validData() // Linux defaults
	out, err := service.RenderPlist(d)
	if err != nil {
		t.Fatalf("RenderPlist (linux) failed: %v", err)
	}

	mustContain := []string{
		"<string>/etc</string>",      // XDG_CONFIG_HOME
		"<string>/var/lib</string>",  // XDG_STATE_HOME
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("linux plist missing %q\nGot:\n%s", want, out)
		}
	}
}
