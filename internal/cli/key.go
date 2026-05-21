package cli

import "github.com/spf13/cobra"

// newKeyCmd builds the "key" sub-command group and its children.
// All children are stubs; real logic arrives in the key-management task.
func newKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key [command]",
		Short: "Manage API keys",
		Long:  "Create, list, and delete API keys used to authenticate remote access.",
	}

	// key create
	create := &cobra.Command{
		Use:   "create [--name <label>]",
		Short: "Create a new API key",
		Long: `Generate a new API key for remote access authentication.
The key is displayed once and cannot be retrieved afterwards.

  Flags:
    --name string   human-readable label for the key`,
		RunE: newStub("key create"),
	}
	create.Flags().String("name", "", "human-readable label for the key")

	// key list
	list := &cobra.Command{
		Use:   "list",
		Short: "List all API keys",
		Long: `Display a table of all API keys with their ID, label, creation date,
and last-used date.`,
		RunE: newStub("key list"),
	}

	// key delete
	del := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete an API key",
		Long: `Revoke and permanently delete the API key with the given ID.
This action cannot be undone.`,
		RunE: newStub("key delete"),
	}

	cmd.AddCommand(create, list, del)
	return cmd
}
