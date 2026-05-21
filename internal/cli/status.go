package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/vladimirvkhs/raxd/internal/config"
)

// newStatusCmd returns the "status" command.
// Output goes to stdout in aligned key: value format; exit code is always 0.
// SECURITY: prints only paths and state string — no file contents, no secrets.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status and configuration paths",
		Long: `Display the current state of the raxd daemon and the filesystem paths
used for configuration, key storage, and TLS certificates.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths, err := config.Paths()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "error: %v\n", err)
				fmt.Fprintf(cmd.ErrOrStderr(),
					"  hint: set the HOME environment variable and try again\n")
				return err
			}

			out := cmd.OutOrStdout()

			// Field width is 8 chars (per ux-spec), values start at column 12.
			// Two-space left indent.
			configSuffix := ""
			if _, err := os.Stat(paths.ConfigFile); os.IsNotExist(err) {
				configSuffix = "  (not found, defaults applied)"
			}

			fmt.Fprintf(out, "  %-8s %s\n", "state", "not running")
			fmt.Fprintf(out, "  %-8s %s%s\n", "config", paths.ConfigFile, configSuffix)
			fmt.Fprintf(out, "  %-8s %s\n", "keys", paths.KeysDB)
			fmt.Fprintf(out, "  %-8s %s\n", "tls", paths.TLSDir)

			return nil // exit 0
		},
	}
}
