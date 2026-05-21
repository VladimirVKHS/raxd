package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	clog "github.com/charmbracelet/log"
	"github.com/vladimirvkhs/raxd/internal/config"
	"github.com/vladimirvkhs/raxd/internal/keystore"
)

// ErrPortInUse is returned by Run when the configured port is already in use.
var ErrPortInUse = errors.New("address already in use")

// CertInfo carries metadata about the TLS certificate for startup output.
type CertInfo struct {
	CertPath  string
	KeyPath   string
	Generated bool // true → "generated"; false → "loaded"
}

// Server is the raxd TLS HTTP server. It owns the http.Server, TLS config,
// middleware chain, and rate limiters.
type Server struct {
	httpServer *http.Server
	limiters   *Limiters
	store      *keystore.Store
	logger     *clog.Logger
	certInfo   CertInfo

	// onListen is called once, immediately after the TCP listener is successfully
	// created and before Serve begins. The addr argument is the actual bound address.
	// Used by serve.go to defer the startup block until the port is really bound.
	// Nil means no hook. Must not be called when Run returns an error before listen.
	onListen func(addr string)

	// afterShutdownHook is called immediately after http.Server.Shutdown returns,
	// before store.FlushUsage(). Nil in production; set by tests to verify SR-24 order.
	afterShutdownHook func()
}

// New creates a fully configured *Server.
// It loads or generates the TLS certificate, builds the middleware chain,
// and registers routes.
//
// Contract (plan.md):
//   - Returns ErrTLSCert if TLS cert/key files are corrupt (AC13).
//   - Does not bind the port — that happens in Run.
//
// SR-21: logger must not log key body, Authorization header, or private TLS key.
func New(cfg *config.Config, paths config.PathSet, store *keystore.Store, logger *clog.Logger) (*Server, error) {
	// Load or generate TLS certificate (AC2/AC3, SR-3/SR-4/SR-5/SR-6).
	cr, err := loadOrCreateCert(paths.TLSDir)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTLSCert, err)
	}

	// Build TLS config (AC1, SR-1/SR-2).
	tlsCfg := buildTLSConfig(cr.cert)

	// Build rate limiters (AC6, SR-17/SR-18).
	limiters := NewLimiters(
		cfg.RateLimit,
		cfg.RateBurst,
		cfg.LimiterTTL(),
	)

	// Build audit function (AC8/AC9, SR-19/SR-21).
	auditFn := func(rec AuditRecord) {
		writeAudit(logger, rec)
	}

	// Build middleware chain (plan.md §Поток запроса):
	// audit(outer) → recover → Host/Origin → auth → rate-limit → mux
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("/", dispatchHandler)

	var handler http.Handler = mux

	// Layer 6 (innermost, wraps mux): AUTH success audit (ISSUE-3/SR-19).
	// Writes exactly ONE "success" audit record per request that passed all gates.
	// Must be innermost so it runs only when rate-limit (Layer 5) allows through.
	handler = authSuccessAuditMiddleware(auditFn)(handler)

	// Layer 5: rate-limit (per-key + per-IP token bucket, SR-17).
	handler = rateLimitMiddleware(limiters, auditFn)(handler)

	// Layer 4: auth (Bearer → keystore.Verify)
	handler = authMiddleware(store, auditFn)(handler)

	// Layer 3: Host/Origin validation (SR-14: before auth; SR-19/SR-20: denials audited)
	handler = hostOriginMiddleware(cfg.HostAllow, cfg.OriginAllow, auditFn)(handler)

	// Layer 2: recover (panic protection, SR-25)
	handler = recoverMiddleware(handler)

	// Layer 1 (outermost): body size limit (SR-25 — large-body flooding protection).
	// Applied before recover so that body-limit errors are caught by recoverMiddleware
	// if a handler panics on oversized body, and every handler inherits the limit.
	handler = bodyLimitMiddleware(cfg.MaxBodyBytes)(handler)

	addr := fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.Port)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		TLSConfig:         tlsCfg,
		ReadTimeout:       cfg.ReadTimeout,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}

	certPath := paths.TLSDir + "/cert.pem"
	keyPath := paths.TLSDir + "/key.pem"

	return &Server{
		httpServer: httpSrv,
		limiters:   limiters,
		store:      store,
		logger:     logger,
		certInfo: CertInfo{
			CertPath:  certPath,
			KeyPath:   keyPath,
			Generated: cr.generated,
		},
	}, nil
}

// GetCertInfo returns information about the TLS certificate (generated or loaded).
func (s *Server) GetCertInfo() CertInfo {
	return s.certInfo
}

// SetOnListen registers a function to be called once, immediately after the TCP
// listener is successfully bound and before the server starts accepting connections.
// The addr argument is the actual bound address (e.g. "127.0.0.1:7822").
//
// This is the seam used by serve.go to print the startup block only after a
// successful bind — satisfying ux-spec §5 (no startup block on bind error).
// Must be set before calling Run. Safe to leave nil (no-op).
func (s *Server) SetOnListen(fn func(addr string)) {
	s.onListen = fn
}

// SetAfterShutdownHook registers a function to be called immediately after
// http.Server.Shutdown returns and before store.FlushUsage().
// This is a test seam for verifying SR-24 ordering; must not be called in production code.
func (s *Server) SetAfterShutdownHook(fn func()) {
	s.afterShutdownHook = fn
}

// Addr returns the configured listen address.
func (s *Server) Addr() string {
	return s.httpServer.Addr
}

// Run starts the TLS listener and blocks until ctx is cancelled or an error occurs.
//
// Graceful shutdown sequence (AC12, SR-24):
//  1. ctx cancelled → http.Server.Shutdown(shutdownCtx)
//  2. store.FlushUsage()
//  3. Return nil (ErrServerClosed is swallowed as success).
//
// Returns ErrPortInUse when bind fails with EADDRINUSE (AC13).
func (s *Server) Run(ctx context.Context) error {
	// Start GC for rate limiters — stops when ctx is done (SR-18).
	s.limiters.StartGC(ctx, 5*time.Minute)

	// Bind the TCP listener manually so we can distinguish EADDRINUSE.
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		if isAddrInUse(err) {
			return fmt.Errorf("%w: %s", ErrPortInUse, s.httpServer.Addr)
		}
		return err
	}

	// Wrap in TLS.
	tlsLn := tls.NewListener(ln, s.httpServer.TLSConfig)

	// Fire onListen hook AFTER successful bind, BEFORE Serve.
	// serve.go uses this to print the startup block only when the port is actually bound.
	// Satisfies ux-spec §5: no startup block on bind error (D-1).
	if s.onListen != nil {
		s.onListen(ln.Addr().String())
	}

	// Serve in a goroutine; wait for ctx cancellation.
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- s.httpServer.Serve(tlsLn)
	}()

	select {
	case err := <-serveErr:
		// Serve returned before ctx was cancelled (e.g. listener closed).
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		// Context cancelled → graceful shutdown.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := s.httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// Best-effort: log but continue to FlushUsage.
			s.logger.Warn("shutdown error", "err", err)
		}

		// afterShutdownHook is invoked immediately after Shutdown and before FlushUsage.
		// Used by tests to verify SR-24 ordering; nil in production.
		if s.afterShutdownHook != nil {
			s.afterShutdownHook()
		}

		// SR-24: FlushUsage AFTER Shutdown completes.
		if err := s.store.FlushUsage(); err != nil {
			s.logger.Warn("flush usage error", "err", err)
		}

		return nil
	}
}

// buildTLSConfig creates the tls.Config for the server.
// SR-1: MinVersion = TLS 1.3.
// SR-2: CipherSuites NOT set (not configurable in TLS 1.3).
func buildTLSConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		// CipherSuites intentionally omitted (SR-2: not configurable in TLS 1.3).
	}
}

// isAddrInUse reports whether err indicates that the port is already in use.
func isAddrInUse(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "address already in use") || strings.Contains(msg, "bind: address already in use")
}
