package banner_test

import (
	"strings"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/banner"
	"github.com/vladimirvkhs/raxd/internal/version"
)

func TestRenderContainsAuthor(t *testing.T) {
	version.Set("dev", "none", "unknown")
	out := banner.Render()

	if !strings.Contains(out, "Vladimir Kovalev, OEM TECH") {
		t.Errorf("banner does not contain author string, got:\n%s", out)
	}
}

func TestRenderContainsProductName(t *testing.T) {
	version.Set("dev", "none", "unknown")
	out := banner.Render()

	if !strings.Contains(out, "raxd") {
		t.Errorf("banner does not contain product name 'raxd', got:\n%s", out)
	}
}

func TestRenderContainsBuildInfo(t *testing.T) {
	version.Set("1.0.0", "abc1234", "2025-06-01")
	defer version.Set("dev", "none", "unknown")

	out := banner.Render()

	if !strings.Contains(out, "1.0.0") {
		t.Errorf("banner does not contain version, got:\n%s", out)
	}
	if !strings.Contains(out, "abc1234") {
		t.Errorf("banner does not contain commit, got:\n%s", out)
	}
	if !strings.Contains(out, "2025-06-01") {
		t.Errorf("banner does not contain date, got:\n%s", out)
	}
}

func TestRenderHasBoxDrawing(t *testing.T) {
	version.Set("dev", "none", "unknown")
	out := banner.Render()

	// Must contain Unicode box-drawing characters.
	if !strings.Contains(out, "┌") || !strings.Contains(out, "┐") {
		t.Errorf("banner missing box-drawing top corners, got:\n%s", out)
	}
	if !strings.Contains(out, "└") || !strings.Contains(out, "┘") {
		t.Errorf("banner missing box-drawing bottom corners, got:\n%s", out)
	}
}

func TestRenderNoSecrets(t *testing.T) {
	version.Set("dev", "none", "unknown")
	out := banner.Render()

	// SECURITY: banner must not contain any known secret patterns.
	secrets := []string{"rax_live_", "BEGIN PRIVATE KEY", "BEGIN RSA PRIVATE KEY"}
	for _, s := range secrets {
		if strings.Contains(out, s) {
			t.Errorf("banner contains sensitive pattern %q", s)
		}
	}
}
