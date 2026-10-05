// Package telemetry wires up the three signals every Anvil process emits:
//
//   - traces: OpenTelemetry, exported over OTLP/HTTP when OTEL_EXPORTER_OTLP_ENDPOINT is set
//     (the in-cluster OpenTelemetry Collector), otherwise disabled
//   - metrics: Prometheus, served on their own port (METRICS_PORT, default 9090) so they are
//     never reachable through the public Gateway; Azure Managed Prometheus scrapes them
//   - logs: JSON on stdout with trace_id/span_id, so a log line leads to its trace
package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Registry holds the process's Prometheus metrics. It starts with Go runtime and process
// metrics; packages register their own on top.
var Registry = func() *prometheus.Registry {
	r := prometheus.NewRegistry()
	r.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return r
}()

// SetupTracing installs the global tracer provider and W3C trace-context propagation. The
// returned function flushes buffered spans; call it on shutdown.
func SetupTracing(ctx context.Context, component, version string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}
	// The exporter reads OTEL_EXPORTER_OTLP_ENDPOINT (and the other standard OTEL_* variables).
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	res, err := Resource(ctx, component, version)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		// Honour the caller's sampling decision; sample new traces at OTEL_TRACES_SAMPLER_ARG
		// (default: all of them, which is fine at this traffic level).
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(samplingRatio()))),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// Resource describes this process to the tracing backend. The attributes carry no schema URL on
// purpose: merging attributes pinned to one semantic-conventions version with the SDK's own
// (a newer one) fails with "conflicting Schema URL", which silently disabled tracing once.
func Resource(ctx context.Context, component, version string) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithFromEnv(), // OTEL_RESOURCE_ATTRIBUTES, e.g. deployment.environment
		resource.WithAttributes(
			attribute.String("service.name", "anvil-"+component),
			attribute.String("service.version", version),
			attribute.String("service.namespace", "anvil"),
		),
	)
}

func samplingRatio() float64 {
	if v, err := strconv.ParseFloat(os.Getenv("OTEL_TRACES_SAMPLER_ARG"), 64); err == nil && v >= 0 && v <= 1 {
		return v
	}
	return 1
}

// ServeMetrics serves /metrics on METRICS_PORT until ctx is cancelled.
func ServeMetrics(ctx context.Context, log *slog.Logger) {
	addr := ":" + envOr("METRICS_PORT", "9090")
	srv := &http.Server{
		Addr:              addr,
		Handler:           promhttp.HandlerFor(Registry, promhttp.HandlerOpts{Registry: Registry}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Info("metrics listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("metrics server", "err", err)
	}
}

// LogHandler adds trace_id and span_id to every record logged with a context that carries a
// span (log.InfoContext(ctx, ...)).
func LogHandler(next slog.Handler) slog.Handler { return traceLogHandler{next} }

type traceLogHandler struct{ slog.Handler }

func (h traceLogHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}
	return h.Handler.Handle(ctx, r)
}

func (h traceLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceLogHandler{h.Handler.WithAttrs(attrs)}
}

func (h traceLogHandler) WithGroup(name string) slog.Handler {
	return traceLogHandler{h.Handler.WithGroup(name)}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
