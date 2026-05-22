// Package cli — service.go: cobra command group "raxd service" with 5 subcommands.
//
// plan.md §Contracts: newServiceCmd() *cobra.Command — group + 5 subcommands.
// ux-spec: output streams, exit codes, error format per command.
//
// Design for testability: serviceManager is injected via the command's Annotations map
// or via a package-level factory set by tests. Production code calls service.New().
//
// SR-95: raw stderr from OS manager is NOT propagated to user output.
// SR-84: ErrPermission → neutral error + hint (no silent root fallback).
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"github.com/vladimirvkhs/raxd/internal/service"
)

// buildServiceCmd builds the service command group, optionally using the given manager
// instead of calling service.New() at runtime. mgr==nil means production mode.
func buildServiceCmd(mgr service.ServiceManager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service [command]",
		Short: "Manage raxd as a system service",
		Long: `Register, start, stop, and monitor raxd as a managed OS service.

On Linux, raxd uses systemd. On macOS, it uses launchd.
The service runs under the unprivileged user "raxd" (not root).

Installation requires root/sudo. The daemon itself always runs as a non-root user.`,
	}

	cmd.AddCommand(
		newServiceInstallCmd(mgr),
		newServiceUninstallCmd(mgr),
		newServiceStartCmd(mgr),
		newServiceStopCmd(mgr),
		newServiceStatusCmd(mgr),
	)

	return cmd
}

// resolveManager returns the injected manager (test) or constructs one from service.New().
func resolveManager(injected service.ServiceManager) (service.ServiceManager, error) {
	if injected != nil {
		return injected, nil
	}
	cfg := service.DefaultConfig()
	cfg.ExecPath, _ = os.Executable()
	return service.New(cfg)
}

// serviceContext returns a context with a reasonable timeout for manager calls.
func serviceContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// ─── install ─────────────────────────────────────────────────────────────────

func newServiceInstallCmd(mgr service.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Register raxd as a system service and enable autostart",
		Long: `Register raxd with the OS service manager (systemd on Linux, launchd on macOS).

Creates system user "raxd" if not present, installs the service unit/plist,
and enables autostart at boot. Requires root or sudo.

After install, start the service with "raxd service start".`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			stderr := cmd.ErrOrStderr()

			m, err := resolveManager(mgr)
			if err != nil {
				printSvcError(stderr, mapManagerError(err))
				return err
			}

			ctx, cancel := serviceContext()
			defer cancel()

			if err := m.Install(ctx); err != nil {
				// AC9: ErrAlreadyInstalled → exit 0, informational block.
				if errors.Is(err, service.ErrAlreadyInstalled) {
					fmt.Fprintf(stderr, "  already installed   raxd service\n")
					fmt.Fprintf(stderr, "  hint: use \"raxd service status\" to check the current state\n")
					return nil // exit 0
				}
				printSvcError(stderr, mapManagerError(err))
				return err
			}

			// Success block (ux-spec §install success).
			unitDisplayPath := unitDisplayPathForOS()
			fmt.Fprintf(stderr, "  %-14s raxd service\n", "installed")
			fmt.Fprintf(stderr, "  %-14s %s\n", "unit", unitDisplayPath)
			if runtime.GOOS == "linux" {
				fmt.Fprintf(stderr, "  %-14s %s\n", "drop-in", "/etc/systemd/journald.conf.d/raxd.conf")
			}
			fmt.Fprintf(stderr, "  %-14s raxd  [not root]\n", "user")
			fmt.Fprintf(stderr, "  %-14s %s\n", "hint:", "start the service now with \"raxd service start\"")

			// Audit log (ux-spec).
			logger := log.New(stderr)
			logger.Info("service installed",
				"action", "install",
				"platform", runtime.GOOS,
				"unit", unitDisplayPath,
				"user", "raxd",
			)

			return nil
		},
	}
}

// ─── uninstall ────────────────────────────────────────────────────────────────

func newServiceUninstallCmd(mgr service.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the system service registration and disable autostart",
		Long: `Unregister raxd from the OS service manager.

Stops the service if running, removes the unit/plist file, and disables autostart.
The system user "raxd" is intentionally kept (see SR-93, ADR-002).
Data in the state directory is preserved.

Requires root or sudo.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			stderr := cmd.ErrOrStderr()

			m, err := resolveManager(mgr)
			if err != nil {
				printSvcError(stderr, mapManagerError(err))
				return err
			}

			ctx, cancel := serviceContext()
			defer cancel()

			if err := m.Uninstall(ctx); err != nil {
				// AC10: ErrNotInstalled@uninstall → exit 0, informational block.
				if errors.Is(err, service.ErrNotInstalled) {
					fmt.Fprintf(stderr, "  not installed   raxd service\n")
					fmt.Fprintf(stderr, "  hint: use \"raxd service install\" to set up the service\n")
					return nil // exit 0
				}
				printSvcError(stderr, mapManagerError(err))
				return err
			}

			// Success block (ux-spec §uninstall success).
			fmt.Fprintf(stderr, "  %-14s raxd service\n", "uninstalled")
			if runtime.GOOS == "linux" {
				fmt.Fprintf(stderr, "  %-14s unit file and autostart registration\n", "removed")
				fmt.Fprintf(stderr, "  %-14s journal size limit drop-in\n", "removed")
				fmt.Fprintf(stderr, "  %-14s system user \"raxd\" (no shell, no home, not running)\n", "kept")
				fmt.Fprintf(stderr, "  hint: to also remove the user: sudo userdel raxd\n")
			} else {
				fmt.Fprintf(stderr, "  %-14s plist file and autostart registration\n", "removed")
				fmt.Fprintf(stderr, "  %-14s system user \"raxd\" (no shell, no home, not running)\n", "kept")
				fmt.Fprintf(stderr, "  hint: to also remove the user: sudo dscl . -delete /Users/raxd\n")
			}
			fmt.Fprintf(stderr, "  hint: data in /var/lib/raxd is preserved — remove manually if no longer needed\n")

			logger := log.New(stderr)
			logger.Info("service uninstalled",
				"action", "uninstall",
				"platform", runtime.GOOS,
			)

			return nil
		},
	}
}

// ─── start ────────────────────────────────────────────────────────────────────

func newServiceStartCmd(mgr service.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the raxd service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			stderr := cmd.ErrOrStderr()

			m, err := resolveManager(mgr)
			if err != nil {
				printSvcError(stderr, mapManagerError(err))
				return err
			}

			ctx, cancel := serviceContext()
			defer cancel()

			if err := m.Start(ctx); err != nil {
				// ErrNotInstalled@start → exit 1, error: block (ux-spec).
				if errors.Is(err, service.ErrNotInstalled) {
					fmt.Fprintf(stderr, "error: raxd service is not installed\n")
					fmt.Fprintf(stderr, "  hint: install it first with \"raxd service install\"\n")
					return err
				}
				printSvcError(stderr, mapManagerError(err))
				return err
			}

			// Query PID after start.
			statusCtx, statusCancel := serviceContext()
			defer statusCancel()
			st, _ := m.Status(statusCtx)

			fmt.Fprintf(stderr, "  %-14s raxd service\n", "started")
			if st.PID > 0 {
				fmt.Fprintf(stderr, "  %-14s %d\n", "pid", st.PID)
			}
			fmt.Fprintf(stderr, "  hint: check status with \"raxd service status\"\n")

			logger := log.New(stderr)
			logger.Info("service started",
				"action", "start",
				"pid", st.PID,
			)

			return nil
		},
	}
}

// ─── stop ─────────────────────────────────────────────────────────────────────

func newServiceStopCmd(mgr service.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the raxd service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			stderr := cmd.ErrOrStderr()

			m, err := resolveManager(mgr)
			if err != nil {
				printSvcError(stderr, mapManagerError(err))
				return err
			}

			ctx, cancel := serviceContext()
			defer cancel()

			if err := m.Stop(ctx); err != nil {
				// ErrNotInstalled@stop → exit 1, error: block (ux-spec).
				if errors.Is(err, service.ErrNotInstalled) {
					fmt.Fprintf(stderr, "error: raxd service is not installed\n")
					fmt.Fprintf(stderr, "  hint: install it first with \"raxd service install\"\n")
					return err
				}
				printSvcError(stderr, mapManagerError(err))
				return err
			}

			fmt.Fprintf(stderr, "  %-14s raxd service\n", "stopped")
			fmt.Fprintf(stderr, "  hint: start again with \"raxd service start\"\n")

			logger := log.New(stderr)
			logger.Info("service stopped",
				"action", "stop",
			)

			return nil
		},
	}
}

// ─── status ───────────────────────────────────────────────────────────────────

func newServiceStatusCmd(mgr service.ServiceManager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the status of the raxd system service",
		Long: `Show whether the raxd system service is installed and running.

Output goes to stdout (suitable for scripting). Use --json for machine-readable JSON.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			stdout := cmd.OutOrStdout()
			stderr := cmd.ErrOrStderr()
			jsonFlag, _ := cmd.Flags().GetBool("json")

			m, err := resolveManager(mgr)
			if err != nil {
				printSvcError(stderr, mapManagerError(err))
				return err
			}

			ctx, cancel := serviceContext()
			defer cancel()

			st, err := m.Status(ctx)
			if err != nil {
				printSvcError(stderr, mapManagerError(err))
				return err
			}

			if jsonFlag {
				return printStatusJSON(stdout, st)
			}
			printStatusHuman(stdout, st)
			return nil // exit 0 always for status (AC10)
		},
	}

	cmd.Flags().Bool("json", false, "output status as JSON")
	return cmd
}

// ─── Output helpers (ux-spec) ─────────────────────────────────────────────────

// printSvcError prints "error: msg\n  hint: ...\n" to w per ux-spec.
// SR-95: no raw OS stderr in output.
func printSvcError(w io.Writer, msg serviceErrorMsg) {
	fmt.Fprintf(w, "error: %s\n", msg.err)
	for _, h := range msg.hints {
		fmt.Fprintf(w, "  hint: %s\n", h)
	}
}

type serviceErrorMsg struct {
	err   string
	hints []string
}

// mapManagerError maps a service.ServiceManager error to a user-facing message (ux-spec table).
// SR-95: neutral text, no raw OS error.
func mapManagerError(err error) serviceErrorMsg {
	switch {
	case errors.Is(err, service.ErrPermission):
		return serviceErrorMsg{
			err: "insufficient privileges to install the service",
			hints: []string{
				"run as root or with sudo: sudo raxd service install",
				"installation requires root to write system service files",
			},
		}
	case errors.Is(err, service.ErrManagerUnavailable):
		return serviceErrorMsg{
			err: "service manager is not available",
			hints: []string{
				"ensure systemd (Linux) or launchd (macOS) is running",
			},
		}
	case errors.Is(err, service.ErrUnsupported):
		return serviceErrorMsg{
			err: "this platform is not supported",
			hints: []string{
				"raxd service management is available on Linux and macOS only",
			},
		}
	case errors.Is(err, service.ErrNotInstalled):
		return serviceErrorMsg{
			err: "raxd service is not installed",
			hints: []string{
				"install it first with \"raxd service install\"",
			},
		}
	case errors.Is(err, service.ErrAlreadyInstalled):
		return serviceErrorMsg{
			err: "raxd service is already installed",
			hints: []string{
				"use \"raxd service status\" to check the current state",
			},
		}
	default:
		return serviceErrorMsg{
			err: fmt.Sprintf("service operation failed: %s", err.Error()),
			hints: []string{
				"run \"raxd service status\" to check current state",
			},
		}
	}
}

// printStatusHuman prints the human-readable status block to stdout (ux-spec §status).
func printStatusHuman(w io.Writer, st service.Status) {
	const w12 = "%-12s"

	if !st.Installed {
		fmt.Fprintf(w, "  "+w12+" %s\n", "installed", "no")
		fmt.Fprintf(w, "  hint: install with \"raxd service install\"\n")
		return
	}

	running := "no"
	if st.Active {
		running = "yes"
	}

	fmt.Fprintf(w, "  "+w12+" %s\n", "installed", "yes")
	fmt.Fprintf(w, "  "+w12+" %s\n", "running", running)

	if st.PID > 0 {
		fmt.Fprintf(w, "  "+w12+" %d\n", "pid", st.PID)
	} else {
		fmt.Fprintf(w, "  "+w12+" %s\n", "pid", "-")
	}

	if st.EUID > 0 {
		fmt.Fprintf(w, "  "+w12+" %d\n", "euid", st.EUID)
	}

	fmt.Fprintf(w, "  "+w12+" raxd  [not root]\n", "user")
	fmt.Fprintf(w, "  "+w12+" %s\n", "unit", unitDisplayPathForOS())
	fmt.Fprintf(w, "  "+w12+" %s\n", "manager", managerNameForOS())
	fmt.Fprintf(w, "  "+w12+" %s\n", "state", st.State)

	if !st.Active {
		fmt.Fprintf(w, "  hint: start with \"raxd service start\"\n")
	}
}

// printStatusJSON writes the status as JSON to w (ux-spec §status --json).
func printStatusJSON(w io.Writer, st service.Status) error {
	type jsonStatus struct {
		Installed bool   `json:"installed"`
		Active    bool   `json:"active"`
		PID       int    `json:"pid"`
		EUID      int    `json:"euid"`
		User      string `json:"user"`
		State     string `json:"state"`
		Manager   string `json:"manager"`
	}

	user := ""
	if st.Installed {
		user = "raxd"
	}
	js := jsonStatus{
		Installed: st.Installed,
		Active:    st.Active,
		PID:       st.PID,
		EUID:      st.EUID,
		User:      user,
		State:     st.State,
		Manager:   managerNameForOS(),
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(js)
}

// ─── Platform helpers ─────────────────────────────────────────────────────────

func unitDisplayPathForOS() string {
	if runtime.GOOS == "darwin" {
		return "/Library/LaunchDaemons/tech.oem.raxd.plist"
	}
	return "/etc/systemd/system/raxd.service"
}

func managerNameForOS() string {
	if runtime.GOOS == "darwin" {
		return "launchd"
	}
	return "systemd"
}
