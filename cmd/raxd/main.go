// Command raxd — Remote Access Daemon for AI agents.
//
// Build-time metadata is injected via -ldflags:
//
//	go build -ldflags "\
//	  -X github.com/vladimirvkhs/raxd/internal/version.Version=$(git describe --tags --always) \
//	  -X github.com/vladimirvkhs/raxd/internal/version.Commit=$(git rev-parse --short HEAD) \
//	  -X github.com/vladimirvkhs/raxd/internal/version.Date=$(date -u +%Y-%m-%d)" \
//	  ./cmd/raxd
//
// When building without ldflags the defaults are: version=dev, commit=none, date=unknown.
package main

import (
	"os"

	"github.com/vladimirvkhs/raxd/internal/cli"
	"github.com/vladimirvkhs/raxd/internal/version"
)

// These package-level variables are the ldflags injection targets.
// They must be plain string variables (not const, not function results) — see ADR-002.
var (
	buildVersion = "dev"
	buildCommit  = "none"
	buildDate    = "unknown"
)

func main() {
	// Pass ldflags values into the version package before executing any command.
	version.Set(buildVersion, buildCommit, buildDate)

	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
