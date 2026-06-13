package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	clog "github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"github.com/vladimirvkhs/raxd/internal/cmdexec"
	"github.com/vladimirvkhs/raxd/internal/config"
	"github.com/vladimirvkhs/raxd/internal/fileupload"
	"github.com/vladimirvkhs/raxd/internal/keystore"
	internalmcp "github.com/vladimirvkhs/raxd/internal/mcp"
	"github.com/vladimirvkhs/raxd/internal/server"
	"github.com/vladimirvkhs/raxd/internal/version"
)

// newServeCmd returns the "serve" command.
// It replaces the honest stub with a foreground TLS server (AC11).
//
// SECURITY: key material is never passed via argv or env (SR-12).
// All configuration comes from config.yaml (SR-7).
// Server is only run in Docker per SECURITY-BASELINE §6.
func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the raxd TLS server",
		Long: `Start raxd as a foreground TLS server.

The server listens on the configured address (default: 127.0.0.1:7822)
with TLS 1.3. Every connection is authenticated with an API key before
any request is processed.

Configuration is read from ~/.config/raxd/config.yaml.
For production use, register raxd as a system service instead.`,
		RunE: runServe,
	}
}

// runServe is the cobra RunE implementation for "raxd serve".
// It follows the ux-spec startup output format (ux-spec.md §1/§2/§4/§5).
func runServe(cmd *cobra.Command, _ []string) error {
	stderr := cmd.ErrOrStderr()

	// Resolve paths and load config.
	paths, err := config.Paths()
	if err != nil {
		fmt.Fprintf(stderr, "error: %s\n", err)
		return err
	}

	if err := config.EnsureDirs(paths); err != nil {
		fmt.Fprintf(stderr, "error: cannot create TLS directory: permission denied\n")
		fmt.Fprintf(stderr, "  hint: check that the current user has write access to ~/.local/state/raxd/\n")
		return err
	}

	cfg, err := config.Load(paths)
	if err != nil {
		fmt.Fprintf(stderr, "error: %s\n", err)
		fmt.Fprintf(stderr, "  hint: set a valid address in config.yaml (field: bind_addr), for example \"127.0.0.1\" or \"0.0.0.0\"\n")
		return err
	}

	// Open keystore (keys.db). Missing/empty → valid empty store (all requests → 401).
	// Corrupt → error at startup.
	ks, err := keystore.Open(paths.KeysDB)
	if err != nil {
		fmt.Fprintf(stderr, "error: key store is corrupted or unreadable\n")
		fmt.Fprintf(stderr, "  hint: check file permissions on the keys.db path shown in \"raxd status\"\n")
		fmt.Fprintf(stderr, "  hint: do not attempt to repair the file manually — contact support if data recovery is needed\n")
		return err
	}

	// Build audit logger (charmbracelet/log, strict logfmt to stderr).
	// F-1/SR-60: LogfmtFormatter обеспечивает строгий парсимый key=value формат.
	// SR-21: logger must not be used to log key bodies or Authorization headers.
	logger := clog.New(stderr)
	logger.SetTimeFormat("2006-01-02T15:04:05Z")
	logger.SetFormatter(clog.LogfmtFormatter) // F-1/SR-60: строгий logfmt (не human-readable TextFormatter)

	// Собираем cmdexec.Config из cfg.Exec (SR-66/plan §Contracts).
	execCfg := cmdexec.Config{
		Allowlist:        cfg.Exec.Allowlist,
		DefaultTimeoutMs: cfg.Exec.DefaultTimeoutMs,
		MaxTimeoutMs:     cfg.Exec.MaxTimeoutMs,
		DefaultCwd:       cfg.Exec.DefaultCwd,
		EnvWhitelist:     cfg.Exec.EnvWhitelist,
		MaxArgs:          cfg.Exec.MaxArgs,
		MaxArgLen:        cfg.Exec.MaxArgLen,
		MaxOutputBytes:   cfg.Exec.MaxOutputBytes,
		DenyRoot:         cfg.Exec.DenyRoot,
	}

	// Резолв upload root (SR-71/AC5a/plan §serve.go):
	// пустой upload.root → <StateDir>/uploads (безопасный дефолт, НЕ /, НЕ /root).
	uploadRoot := cfg.Upload.Root
	if uploadRoot == "" {
		uploadRoot = filepath.Join(paths.StateDir, "uploads")
	}
	// Создаём upload root с правами 0700 (SR-71/plan §serve.go).
	// ВАЖНО: это НОВЫЙ код — не существующий EnsureDirs (который создаёт TLSDir/ConfigDir/StateDir).
	if err := os.MkdirAll(uploadRoot, 0o700); err != nil {
		fmt.Fprintf(stderr, "error: cannot create upload root directory: %s\n", err)
		return err
	}

	// Парс DefaultMode (уже провалидирован в config.buildConfig; здесь только конвертация).
	defaultModeVal, err := fileupload.ParseMode(cfg.Upload.DefaultMode)
	if err != nil {
		// Не должно достигаться (валидация в buildConfig), но на всякий случай.
		fmt.Fprintf(stderr, "error: invalid upload.default_mode: %s\n", err)
		return err
	}

	// Собираем fileupload.Config (plan §serve.go / upload-quota plan §Modules).
	uplCfg := fileupload.Config{
		UploadRoot:    uploadRoot,
		MaxFileBytes:  cfg.Upload.MaxFileBytes,
		MaxTotalBytes: cfg.Upload.MaxTotalBytes, // AC1/SR-98: проброс общего лимита (0 = отключён)
		DefaultMode:   fs.FileMode(defaultModeVal),
		DenyRoot:      cfg.Upload.DenyRoot,
	}

	// Build MCP handler (AC11/SR-29): same port/TLS as serve; no second auth channel (SR-28).
	// auditFn for MCP uses the same logger channel as transport audit.
	// ADR-004: execHandler manages its own exec-audit; not wrapped with withAudit.
	// SR-78: uploadHandler manages its own upload-audit; not wrapped with withAudit.
	auditFn := server.NewAuditFn(logger)
	mcpH, err := internalmcp.NewHandler(version.Version, auditFn, execCfg, uplCfg)
	if err != nil {
		fmt.Fprintf(stderr, "error: failed to build MCP handler: %s\n", err)
		return err
	}

	// Build server (generates or loads TLS cert).
	srv, err := server.New(cfg, paths, ks, logger, mcpH)
	if err != nil {
		printStartError(stderr, err, paths)
		return err
	}

	// Register OnListen hook: prints the startup block ONLY after the TCP listener
	// is successfully bound. This satisfies ux-spec §5 (D-1): if bind fails, Run
	// returns an error without ever calling this hook, so no startup block is printed.
	ci := srv.GetCertInfo()
	keys, listErr := ks.List()
	noKeys := listErr == nil && len(keys) == 0
	started := false // true if onListen fired (used to gate the shutdown block)
	srv.SetOnListen(func(_ string) {
		started = true
		// Print startup block (ux-spec.md §1/§2).
		printStartBlock(stderr, ci, cfg)
		// Warn if no active keys (ux-spec §5.7).
		if noKeys {
			fmt.Fprintf(stderr, "  warning   no API keys found — all connections will be rejected\n")
			fmt.Fprintf(stderr, "  hint      create a key with \"raxd key create --name <label>\"\n")
		}
		fmt.Fprintf(stderr, "  press Ctrl+C to stop\n")
		fmt.Fprintln(stderr)
	})

	// Setup graceful shutdown via signal (AC12, SR-24).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Run server — blocks until ctx is cancelled or a fatal error occurs.
	// onListen fires inside Run, after successful bind, before Serve.
	runErr := srv.Run(ctx)

	if runErr != nil {
		// Bind / startup error: no startup block was printed (started == false),
		// so no shutdown block either. Print error+hint per ux-spec §5.
		if errors.Is(runErr, server.ErrPortInUse) || strings.Contains(runErr.Error(), "address already in use") {
			addr := fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.Port)
			fmt.Fprintf(stderr, "error: cannot bind to %s: address already in use\n", addr)
			fmt.Fprintf(stderr, "  hint: check what is using port %d with \"lsof -i :%d\" and stop it, or change the port with \"raxd config port <PORT>\"\n", cfg.Port, cfg.Port)
		} else {
			fmt.Fprintf(stderr, "error: %s\n", runErr)
		}
		return runErr
	}

	// Server started and completed graceful shutdown (started == true).
	// Print shutdown block only if we actually started (ux-spec §4).
	if started {
		fmt.Fprintf(stderr, "  shutting down  signal received\n")
		fmt.Fprintf(stderr, "  draining       waiting for active connections to finish\n")
		fmt.Fprintf(stderr, "  flushing       usage data flushed\n")
		fmt.Fprintf(stderr, "  stopped\n")
		fmt.Fprintln(stderr)
	}

	return nil
}

// printStartBlock outputs the startup information block to stderr per ux-spec.md.
func printStartBlock(w io.Writer, ci server.CertInfo, cfg *config.Config) {
	certStatus := "loaded"
	keyStatus := "loaded"
	keyExtra := ""
	if ci.Generated {
		certStatus = "generated"
		keyStatus = "generated"
		keyExtra = "  (0600)"
	}

	fmt.Fprintf(w, "  cert      %s  %s\n", certStatus, ci.CertPath)
	fmt.Fprintf(w, "  key       %s  %s%s\n", keyStatus, ci.KeyPath, keyExtra)
	fmt.Fprintf(w, "  tls       TLS 1.3 only\n")
	fmt.Fprintf(w, "  listening https://%s:%d\n", cfg.BindAddr, cfg.Port)
}

// printStartError prints a startup error in ux-spec error:/hint: format.
func printStartError(w io.Writer, err error, paths config.PathSet) {
	switch {
	case errors.Is(err, server.ErrTLSCert) || strings.Contains(err.Error(), "TLS certificate"):
		fmt.Fprintf(w, "error: TLS certificate or key is corrupted or unreadable\n")
		fmt.Fprintf(w, "  hint: remove the files in %s and run \"raxd serve\" again to regenerate\n", paths.TLSDir)
	default:
		fmt.Fprintf(w, "error: failed to generate TLS certificate\n")
		fmt.Fprintf(w, "  hint: check available disk space and write permissions for %s\n", paths.TLSDir)
	}
}

