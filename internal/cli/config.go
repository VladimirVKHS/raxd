package cli

import "github.com/spf13/cobra"

// newConfigCmd builds the "config" sub-command group and its children.
// All children are stubs; real logic arrives in future tasks.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config [command]",
		Short: "Manage configuration",
		Long: `View and modify raxd configuration settings.
Configuration is stored in ~/.config/raxd/config.yaml.`,
	}

	// config port
	port := &cobra.Command{
		Use:   "port <PORT>",
		Short: "Set the listening port",
		Long: `Configure the TCP port that raxd listens on for incoming connections.
Default port is 7822.

  Example:
    raxd config port 8080`,
		RunE: newStub("config port"),
	}

	cmd.AddCommand(port)
	return cmd
}
