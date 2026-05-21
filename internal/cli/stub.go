package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// errNotImplemented is the sentinel error returned by all stub commands.
var errNotImplemented = errors.New("not implemented yet")

// newStub returns a cobra RunE function that always fails with the canonical
// "error: <name>: not implemented yet" message on stderr, then returns a
// non-zero exit code via cobra's error handling.
//
// SECURITY: no networking, no exec, no key material in stubs.
func newStub(name string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		fmt.Fprintf(cmd.ErrOrStderr(), "error: %s: not implemented yet\n", name)
		return errNotImplemented
	}
}
