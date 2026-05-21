package version_test

// version_gaps_test.go — additional tests closing AC gaps for the version package.

import (
	"strings"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/version"
)

// TestInfoNoVPrefix verifies that Info() does not prepend a literal 'v' to the
// version string, so that dev-builds produce "raxd dev …" not "raxd vdev …".
// AC: "без литерального v-префикса"; ux-spec: "Версия печатается как есть".
func TestInfoNoVPrefix(t *testing.T) {
	cases := []struct {
		v      string
		commit string
		date   string
	}{
		{"dev", "none", "unknown"},
		{"1.0.0", "abc1234", "2025-06-01"},
		{"2.3.4", "cafebabe", "2026-01-01"},
	}

	for _, tc := range cases {
		version.Set(tc.v, tc.commit, tc.date)
		got := version.Info()

		// Must start with "raxd <version>" — no 'v' before the version.
		wantPrefix := "raxd " + tc.v
		if !strings.HasPrefix(got, wantPrefix) {
			t.Errorf("Set(%q, …): Info() = %q, must start with %q (no v-prefix)", tc.v, got, wantPrefix)
		}
		// Must not contain "raxd v<version>".
		if strings.Contains(got, "raxd v"+tc.v) {
			t.Errorf("Set(%q, …): Info() = %q, must NOT contain 'raxd v%s'", tc.v, got, tc.v)
		}
	}

	version.Set("dev", "none", "unknown") // restore
}

// TestInfoContainsAllFields verifies that Info() always includes version, commit,
// and date fields — none may be silently dropped.
// AC: "raxd version печатает версию, git-commit и дату сборки".
func TestInfoContainsAllFields(t *testing.T) {
	version.Set("3.0.0", "f0cacc1a", "2026-05-01")
	defer version.Set("dev", "none", "unknown")

	got := version.Info()

	checks := map[string]string{
		"version": "3.0.0",
		"commit":  "commit f0cacc1a",
		"date":    "built 2026-05-01",
	}
	for field, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("Info() missing %s field %q; got: %q", field, want, got)
		}
	}
}
