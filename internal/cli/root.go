// Package cli builds the cobra command tree for raxd.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vladimirvkhs/raxd/internal/banner"
	"github.com/vladimirvkhs/raxd/internal/config"
)

// NewRootCmd constructs the root cobra.Command and registers all sub-commands.
// SilenceUsage and SilenceErrors are true so that the caller (main) fully
// controls exit-code mapping and error printing.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "raxd [command]",
		Short: "raxd — remote access daemon for AI agents",
		Long: `raxd is a remote access daemon that provides secure command execution,
file transfer, and API key management for AI agents.

Use "raxd [command] --help" for more information about a command.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// PersistentPreRun prints the banner to stderr before every command.
		// It does NOT run for --help (cobra skips PersistentPreRun on help).
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.ErrOrStderr(), banner.Render())

			// Ensure XDG directories exist on every invocation.
			// Failure is non-fatal for banner/version — only status/serve care.
			paths, err := config.Paths()
			if err == nil {
				_ = config.EnsureDirs(paths)
			}
		},
	}

	root.AddCommand(
		newKeyCmd(),
		newConfigCmd(),
		newServeCmd(),
		newVersionCmd(),
		newStatusCmd(),
	)

	return root
}

// Execute builds the root command and runs it.
// Returns nil on success (exit 0) or an error on failure (caller maps to exit 1).
func Execute() error {
	return NewRootCmd().Execute()
}
