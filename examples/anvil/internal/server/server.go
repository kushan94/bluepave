// Package server holds what every Anvil process shares: structured logging, an HTTP server
// with sane timeouts, and graceful shutdown on SIGTERM.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bluepave.dev/examples/anvil/internal/telemetry"
)

// Version is set at build time with -ldflags "-X .../server.Version=<git sha>".
var Version = "dev"

// Logger returns a JSON logger tagged with the component name. Records logged with a context
// that carries a span get trace_id and span_id.
func Logger(component string) *slog.Logger {
	h := telemetry.LogHandler(slog.NewJSONHandler(os.Stdout, nil))
	return slog.New(h).With("component", component, "version", Version)
}

// Start sets up tracing and the metrics endpoint for a component. Call the returned function
// on shutdown to flush buffered spans.
func Start(ctx context.Context, log *slog.Logger, component string) func() {
	shutdown, err := telemetry.SetupTracing(ctx, component, Version)
	if err != nil {
		log.Error("tracing disabled", "err", err)
		shutdown = func(context.Context) error { return nil }
	}
	go telemetry.ServeMetrics(ctx, log)
	return func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(flushCtx)
	}
}

// SignalContext is cancelled on SIGTERM (Kubernetes stopping the pod) or SIGINT.
func SignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
}

// Env returns an environment variable or a fallback.
func Env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Run serves handler on addr until ctx is cancelled, then gives in-flight requests up to 10s to
// finish. Kubernetes waits terminationGracePeriodSeconds (30s by default) before killing the pod.
func Run(ctx context.Context, log *slog.Logger, addr string, handler http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	errs := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", addr)
		errs <- srv.ListenAndServe()
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errs; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
