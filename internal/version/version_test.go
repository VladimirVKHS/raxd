package version_test

import (
	"strings"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/version"
)

func TestInfoDefaultValues(t *testing.T) {
	// Reset to known defaults.
	version.Set("dev", "none", "unknown")

	info := version.Info()

	if !strings.HasPrefix(info, "raxd ") {
		t.Errorf("Info() must start with 'raxd ', got: %q", info)
	}
	if !strings.Contains(info, "dev") {
		t.Errorf("Info() must contain version 'dev', got: %q", info)
	}
	if !strings.Contains(info, "commit none") {
		t.Errorf("Info() must contain 'commit none', got: %q", info)
	}
	if !strings.Contains(info, "built unknown") {
		t.Errorf("Info() must contain 'built unknown', got: %q", info)
	}
}

func TestInfoFormat(t *testing.T) {
	version.Set("1.2.3", "abc1234", "2025-06-01")
	defer version.Set("dev", "none", "unknown") // restore

	want := "raxd 1.2.3 (commit abc1234, built 2025-06-01)"
	got := version.Info()
	if got != want {
		t.Errorf("Info() = %q, want %q", got, want)
	}
}

func TestSetPreservesNonEmpty(t *testing.T) {
	version.Set("2.0.0", "deadbeef", "2026-01-01")
	defer version.Set("dev", "none", "unknown")

	if version.Version != "2.0.0" {
		t.Errorf("Version = %q, want 2.0.0", version.Version)
	}
	if version.Commit != "deadbeef" {
		t.Errorf("Commit = %q, want deadbeef", version.Commit)
	}
	if version.Date != "2026-01-01" {
		t.Errorf("Date = %q, want 2026-01-01", version.Date)
	}
}
