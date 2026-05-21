package cli

import "github.com/spf13/cobra"

// newServeCmd returns the "serve" command.
//
// SECURITY (D4 / security-requirements §serve): this is an "honest" stub — it
// prints the error message and exits with a non-zero code WITHOUT starting any
// blocking process, opening a port, or calling net.Listen / exec.Command.
func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the raxd daemon",
		Long: `Start raxd as a foreground daemon process.
For production use, register raxd as a system service instead.`,
		// RunE returns errNotImplemented → cobra exits with code 1.
		RunE: newStub("serve"),
	}
}
