package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
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

// printStoreError maps a keystore error to the canonical user-facing message+hints.
// ISSUE-3: uses errors.Is for wrapped-error compatibility.
func printStoreError(w io.Writer, err error, op string) {
	switch {
	case errors.Is(err, keystore.ErrCorrupt):
		printError(w,
			"key store is corrupted or unreadable",
			`check file permissions on keys.db (must be readable by current user)`,
			`do not attempt to repair the file manually — contact support if data recovery is needed`)
	case errors.Is(err, keystore.ErrLabelTooLong):
		printError(w,
			"label is too long (max 64 characters)",
			"choose a shorter label and try again")
	case errors.Is(err, keystore.ErrNotFound):
		// ux-spec: error: key "<id>" not found
		printError(w, fmt.Sprintf("key %q not found", op),
			`run "raxd key list" to see available key IDs`)
	case errors.Is(err, keystore.ErrAlreadyRevoked):
		// ux-spec: error: key "<id>" is already revoked + hint
		printError(w, fmt.Sprintf("key %q is already revoked", op),
			`run "raxd key list" to see active keys`)
	default:
		printError(w, fmt.Sprintf("%s: %s", op, err.Error()))
	}
}

// runKeyCreate implements "raxd key create [--name <label>]".
// ux-spec: key on stdout in box, warning+metadata on stderr, exit 0 on success.
func runKeyCreate(cmd *cobra.Command, _ []string) error {
	label, _ := cmd.Flags().GetString("name")
	stderr := cmd.ErrOrStderr()
	stdout := cmd.OutOrStdout()

	store, err := openStore()
	if err != nil {
		// Issue 3 (reviewer): delegate to printStoreError to eliminate duplicated blocks.
		printStoreError(stderr, err, "create")
		return err
	}

	plain, rec, err := store.Create(label)
	if err != nil {
		printStoreError(stderr, err, "create")
		return err
	}

	// ux-spec §Принцип 1: WARNING first, to stderr, before the key.
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
	// Fingerprint taken from rec.Fingerprint (persisted at Create time) for consistency with delete audit.
	// ISSUE-1: use cmd.ErrOrStderr() not os.Stderr — honours cobra's output redirection in tests.
	logger := log.New(stderr)
	logger.Info("key created",
		"action", "create",
		"id", rec.ID,
		"fingerprint", rec.Fingerprint,
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
	stderr := cmd.ErrOrStderr()
	stdout := cmd.OutOrStdout()

	store, err := openStore()
	if err != nil {
		// Issue 3 (reviewer): delegate to printStoreError.
		printStoreError(stderr, err, "list")
		return err
	}

	records, err := store.List()
	if err != nil {
		printStoreError(stderr, err, "list")
		return err
	}

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
	stderr := cmd.ErrOrStderr()

	if len(args) == 0 {
		printError(stderr,
			"key delete requires an id argument",
			`run "raxd key list" to find the key ID, then use "raxd key delete <id>"`)
		return fmt.Errorf("missing id argument")
	}

	id := args[0]

	store, err := openStore()
	if err != nil {
		// Issue 3 (reviewer): delegate to printStoreError.
		printStoreError(stderr, err, "delete")
		return err
	}

	// ISSUE-2: retrieve fingerprint from persisted record BEFORE revoking,
	// so that the audit log for delete includes fingerprint (SR-24).
	// We use store.List() which reads all active records under shared flock.
	// Fingerprint is safe to read here: it is a non-sensitive sha256 prefix (SR-15).
	fp := ""
	if recs, lerr := store.List(); lerr == nil {
		for _, r := range recs {
			if r.ID == id {
				fp = r.Fingerprint
				break
			}
		}
	}

	if err := store.Revoke(id); err != nil {
		// Issue 3 (reviewer): delegate to printStoreError; pass id as op context
		// so ErrNotFound/ErrAlreadyRevoked messages include the key ID.
		printStoreError(stderr, err, id)
		return err
	}

	fmt.Fprintf(stderr, "  key %s revoked\n", id)
	fmt.Fprintln(stderr, `  hint: the key can no longer be used for authentication`)

	// SR-24: audit log for delete action with fingerprint from persisted record.
	// ISSUE-1: use cmd.ErrOrStderr() not os.Stderr.
	// ISSUE-2: fingerprint now available from rec.Fingerprint (persisted at Create).
	logger := log.New(stderr)
	logger.Info("key revoked",
		"action", "delete",
		"id", id,
		"fingerprint", fp,
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

