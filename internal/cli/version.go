package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vladimirvkhs/raxd/internal/version"
)

// newVersionCmd returns the "version" command.
// Output goes to stdout; exit code is always 0.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long:  "Print the raxd version, git commit, and build date.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), version.Info())
			return nil // exit 0
		},
	}
}
