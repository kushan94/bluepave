// Command api is the ${{ values.name }} HTTP service: ${{ values.description }}
//
// What its chart (deploy/chart) expects: serves HTTP on PORT (8080), health on /healthz and
// /readyz, Prometheus metrics on :9090/metrics, and shuts down cleanly on SIGTERM.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

// version is set at build time (-ldflags "-X main.version=<commit>").
var version = "dev"

// requests counts HTTP requests; exposed in Prometheus text format on :9090/metrics.
var requests atomic.Int64

func handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"service": "${{ values.name }}", "version": version})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}

func metrics() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "# HELP ${{ values.name | replace("-", "_") }}_http_requests_total HTTP requests served.\n")
		fmt.Fprintf(w, "# TYPE ${{ values.name | replace("-", "_") }}_http_requests_total counter\n")
		fmt.Fprintf(w, "${{ values.name | replace("-", "_") }}_http_requests_total %d\n", requests.Load())
	})
}

func serve(ctx context.Context, addr string, h http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "${{ values.name }}", "version", version)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	go func() {
		if err := serve(ctx, ":9090", metrics()); err != nil {
			log.Error("metrics server", "err", err)
		}
	}()
	log.Info("listening", "port", port)
	if err := serve(ctx, ":"+port, handler()); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
	log.Info("stopped")
}
