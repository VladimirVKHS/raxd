package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/log"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"
	"github.com/vladimirvkhs/raxd/internal/config"
	"github.com/vladimirvkhs/raxd/internal/keystore"
)

// newKeyCmd builds the "key" sub-command group and its children.
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

Store the key securely immediately after creation.

Flags:
  --name string   optional human-readable label for the key (max 64 characters)`,
		RunE: runKeyCreate,
	}
	create.Flags().String("name", "", "optional human-readable label for the key (max 64 characters)")

	// key list
	list := &cobra.Command{
		Use:   "list",
		Short: "List all API keys",
		Long: `Display a table of active API keys with their ID, label, creation date,
and last-used date. Revoked keys are not shown.`,
		RunE: runKeyList,
	}

	// key delete
	del := &cobra.Command{
		Use:   "delete <id>",
		Short: "Revoke an API key",
		Long: `Revoke the API key with the given ID. The key is immediately invalidated
and can no longer be used for authentication. This action cannot be undone.

The key record is retained for audit purposes and will not appear in "key list".

Example:
  raxd key delete abc123de`,
		RunE: runKeyDelete,
	}

	cmd.AddCommand(create, list, del)
	return cmd
}

// openStore resolves KeysDB path and opens the keystore.
// Returns a formatted error suitable for CLI display.
func openStore() (*keystore.Store, error) {
	paths, err := config.Paths()
	if err != nil {
		return nil, fmt.Errorf("cannot resolve config paths: %w", err)
	}
	store, err := keystore.Open(paths.KeysDB)
	if err != nil {
		return nil, err
	}
	return store, nil
}

// printError writes a formatted error+hint block to stderr in canonical style.
// ux-spec: "error:" + "hint:" строчными, двухпробельный отступ слева.
func printError(w io.Writer, message string, hints ...string) {
	fmt.Fprintf(w, "error: %s\n", message)
	for _, h := range hints {
		fmt.Fprintf(w, "  hint: %s\n", h)
	}
}

// runKeyCreate implements "raxd key create [--name <label>]".
// ux-spec: key on stdout in box, warning+metadata on stderr, exit 0 on success.
func runKeyCreate(cmd *cobra.Command, _ []string) error {
	label, _ := cmd.Flags().GetString("name")

	store, err := openStore()
	if err != nil {
		switch err {
		case keystore.ErrCorrupt:
			printError(cmd.ErrOrStderr(),
				"key store is corrupted or unreadable",
				`check file permissions on keys.db (must be readable by current user)`,
				`do not attempt to repair the file manually — contact support if data recovery is needed`)
		default:
			printError(cmd.ErrOrStderr(), fmt.Sprintf("cannot open key store: %s", friendlyErr(err)))
		}
		return err
	}

	plain, rec, err := store.Create(label)
	if err != nil {
		switch err {
		case keystore.ErrLabelTooLong:
			printError(cmd.ErrOrStderr(),
				"label is too long (max 64 characters)",
				"choose a shorter label and try again")
		case keystore.ErrCorrupt:
			printError(cmd.ErrOrStderr(),
				"key store is corrupted or unreadable",
				`check file permissions on keys.db (must be readable by current user)`,
				`do not attempt to repair the file manually — contact support if data recovery is needed`)
		default:
			printError(cmd.ErrOrStderr(), fmt.Sprintf("cannot create key: %s", friendlyErr(err)))
		}
		return err
	}

	// ux-spec §Принцип 1: WARNING first, to stderr, before the key.
	stderr := cmd.ErrOrStderr()
	stdout := cmd.OutOrStdout()

	fmt.Fprintln(stderr, "  ! WARNING: This key will NOT be shown again. Save it now.")
	fmt.Fprintln(stderr)

	// ux-spec §Принцип 2, 3: key in box on stdout.
	printKeyBox(stdout, string(plain))
	fmt.Fprintln(stdout)

	// ux-spec §Принцип 3: metadata on stderr.
	labelDisplay := rec.Label
	if labelDisplay == "" {
		labelDisplay = "-"
	}
	fmt.Fprintf(stderr, "  %-9s %s\n", "id", rec.ID)
	fmt.Fprintf(stderr, "  %-9s %s\n", "label", labelDisplay)
	fmt.Fprintf(stderr, "  %-9s %s\n", "created", rec.Created.Format("2006-01-02"))
	fmt.Fprintln(stderr)

	// SR-24: audit log: timestamp+action+id+fingerprint, NOT the key body.
	logger := log.New(stderr)
	logger.Info("key created",
		"action", "create",
		"id", rec.ID,
		"fingerprint", keystore.Fingerprint(string(plain)),
	)

	return nil
}

// printKeyBox prints the key wrapped in a Unicode box frame on the given writer.
// ux-spec: "┌…┐ │ rax_live_… │ └…┘" on stdout.
func printKeyBox(w io.Writer, key string) {
	inner := "  " + key + "  "
	width := utf8.RuneCountInString(inner)
	top := "┌" + strings.Repeat("─", width) + "┐"
	mid := "│" + inner + "│"
	bot := "└" + strings.Repeat("─", width) + "┘"
	fmt.Fprintln(w, top)
	fmt.Fprintln(w, mid)
	fmt.Fprintln(w, bot)
}

// runKeyList implements "raxd key list".
// ux-spec: table on stdout, empty message on stdout, exit 0 always.
func runKeyList(cmd *cobra.Command, _ []string) error {
	store, err := openStore()
	if err != nil {
		switch err {
		case keystore.ErrCorrupt:
			printError(cmd.ErrOrStderr(),
				"key store is corrupted or unreadable",
				`check file permissions on keys.db (must be readable by current user)`,
				`do not attempt to repair the file manually — contact support if data recovery is needed`)
		default:
			printError(cmd.ErrOrStderr(), fmt.Sprintf("cannot open key store: %s", friendlyErr(err)))
		}
		return err
	}

	records, err := store.List()
	if err != nil {
		switch err {
		case keystore.ErrCorrupt:
			printError(cmd.ErrOrStderr(),
				"key store is corrupted or unreadable",
				`check file permissions on keys.db (must be readable by current user)`,
				`do not attempt to repair the file manually — contact support if data recovery is needed`)
		default:
			printError(cmd.ErrOrStderr(), fmt.Sprintf("cannot list keys: %s", friendlyErr(err)))
		}
		return err
	}

	stdout := cmd.OutOrStdout()

	if len(records) == 0 {
		fmt.Fprintln(stdout, "  No API keys found.")
		fmt.Fprintln(stdout, `  hint: create your first key with "raxd key create --name <label>"`)
		return nil
	}

	// ux-spec: dash-separator table, no outer border, left-align, 2-space left indent.
	// olekukonko/tablewriter v1.x API uses functional options.
	table := tablewriter.NewTable(stdout,
		tablewriter.WithHeader([]string{"  ID", "LABEL", "CREATED", "LAST USED"}),
		tablewriter.WithHeaderAlignment(tw.AlignLeft),
		tablewriter.WithAlignment(tw.MakeAlign(4, tw.AlignLeft)),
		tablewriter.WithBorders(tw.Border{
			Left:   tw.Off,
			Right:  tw.Off,
			Top:    tw.Off,
			Bottom: tw.Off,
		}),
	)

	for _, r := range records {
		idDisplay := "  " + truncate(r.ID, 12)
		labelDisplay := r.Label
		if labelDisplay == "" {
			labelDisplay = "-"
		}
		labelDisplay = truncateEllipsis(labelDisplay, 20)

		createdDisplay := r.Created.Format("2006-01-02")

		lastUsedDisplay := "never"
		if !r.LastUsed.IsZero() {
			lastUsedDisplay = r.LastUsed.Format("2006-01-02")
		}

		if err := table.Append([]string{idDisplay, labelDisplay, createdDisplay, lastUsedDisplay}); err != nil {
			return fmt.Errorf("cannot append table row: %w", err)
		}
	}

	if err := table.Render(); err != nil {
		return fmt.Errorf("cannot render table: %w", err)
	}
	return nil
}

// runKeyDelete implements "raxd key delete <id>".
// ux-spec: confirmation on stderr, exit 0 on success; exit 1 on not-found/already-revoked.
func runKeyDelete(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		printError(cmd.ErrOrStderr(),
			"key delete requires an id argument",
			`run "raxd key list" to find the key ID, then use "raxd key delete <id>"`)
		return fmt.Errorf("missing id argument")
	}

	id := args[0]

	store, err := openStore()
	if err != nil {
		switch err {
		case keystore.ErrCorrupt:
			printError(cmd.ErrOrStderr(),
				"key store is corrupted or unreadable",
				`check file permissions on keys.db (must be readable by current user)`,
				`do not attempt to repair the file manually — contact support if data recovery is needed`)
		default:
			printError(cmd.ErrOrStderr(), fmt.Sprintf("cannot open key store: %s", friendlyErr(err)))
		}
		return err
	}

	// Compute fingerprint before revoke for audit (SR-24).
	// Note: we use id as identifier in audit, fingerprint is optional here since
	// we don't have the plain key at delete time — we log id+action per SR-24 contract.
	// SR-24: audit record uses id (not key body).
	if err := store.Revoke(id); err != nil {
		switch err {
		case keystore.ErrNotFound:
			printError(cmd.ErrOrStderr(),
				fmt.Sprintf("key %q not found", id),
				`run "raxd key list" to see available key IDs`)
		case keystore.ErrAlreadyRevoked:
			printError(cmd.ErrOrStderr(),
				fmt.Sprintf("key %q is already revoked", id),
				`run "raxd key list" to see active keys`)
		case keystore.ErrCorrupt:
			printError(cmd.ErrOrStderr(),
				"key store is corrupted or unreadable",
				`check file permissions on keys.db (must be readable by current user)`,
				`do not attempt to repair the file manually — contact support if data recovery is needed`)
		default:
			printError(cmd.ErrOrStderr(), fmt.Sprintf("cannot revoke key: %s", friendlyErr(err)))
		}
		return err
	}

	stderr := cmd.ErrOrStderr()
	fmt.Fprintf(stderr, "  key %s revoked\n", id)
	fmt.Fprintln(stderr, `  hint: the key can no longer be used for authentication`)

	// SR-24: audit log for delete action.
	logger := log.New(os.Stderr)
	logger.Info("key revoked",
		"action", "delete",
		"id", id,
	)

	return nil
}

// truncate returns the first n runes of s (no ellipsis).
// ux-spec: ID column trimmed to 12 chars without "…".
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// truncateEllipsis returns s truncated to n runes with "…" suffix if needed.
// ux-spec: LABEL column trimmed to 20 chars with "…".
func truncateEllipsis(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}

// friendlyErr strips go-internal formatting from errors for user-facing display.
func friendlyErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	// Strip "key store is locked" noise — already handled by specific case.
	_ = msg
	return err.Error()
}

// formatDate formats a time for display; returns "never" for zero time.
func formatDate(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format("2006-01-02")
}
