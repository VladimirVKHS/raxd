// Package version stores build-time metadata injected via -ldflags.
package version

import "fmt"

// These variables are set at build time via:
//
//	go build -ldflags "-X github.com/vladimirvkhs/raxd/internal/version.Version=1.0.0 \
//	                   -X github.com/vladimirvkhs/raxd/internal/version.Commit=abc1234 \
//	                   -X github.com/vladimirvkhs/raxd/internal/version.Date=2025-06-01"
//
// When building without ldflags the defaults below are used.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Set overwrites the build-time metadata. Called from cmd/raxd/main.go before
// cli.Execute() to pass values received via package-level ldflags variables.
func Set(v, commit, date string) {
	if v != "" {
		Version = v
	}
	if commit != "" {
		Commit = commit
	}
	if date != "" {
		Date = date
	}
}

// Info returns the canonical one-line version string used by the version command.
// Format: "raxd <version> (commit <commit>, built <date>)"
func Info() string {
	return fmt.Sprintf("raxd %s (commit %s, built %s)", Version, Commit, Date)
}
