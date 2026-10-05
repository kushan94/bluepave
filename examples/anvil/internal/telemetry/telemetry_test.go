package telemetry

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.opentelemetry.io/otel/trace"
)

// The route label must be the mux pattern, never the raw path: one series per route, not per
// job ID. The SLO rules depend on the code label.
func TestHTTPMiddlewareLabelsByRoutePattern(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "500" {
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	h := HTTPMiddleware("test", mux)
	for _, path := range []string{"/api/jobs/1", "/api/jobs/2", "/api/jobs/500", "/nowhere"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}

	for _, tt := range []struct {
		route, code string
		want        float64
	}{
		{"GET /api/jobs/{id}", "200", 2},
		{"GET /api/jobs/{id}", "500", 1},
		{"unmatched", "404", 1},
	} {
		got := testutil.ToFloat64(httpRequests.WithLabelValues("test", tt.route, tt.code))
		if got != tt.want {
			t.Errorf("requests{route=%q,code=%s} = %v, want %v", tt.route, tt.code, got, tt.want)
		}
	}
}

// Tracing must actually start when an endpoint is configured. (A schema-URL conflict once made
// SetupTracing fail at runtime, and the app quietly ran without traces.)
func TestSetupTracingWithEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment=test")
	shutdown, err := SetupTracing(context.Background(), "test", "v0")
	if err != nil {
		t.Fatalf("SetupTracing: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	res, err := Resource(context.Background(), "test", "v0")
	if err != nil {
		t.Fatalf("Resource: %v", err)
	}
	attrs := map[string]string{}
	for _, kv := range res.Attributes() {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	for k, want := range map[string]string{
		"service.name":           "anvil-test",
		"service.namespace":      "anvil",
		"deployment.environment": "test",
	} {
		if attrs[k] != want {
			t.Errorf("resource %s = %q, want %q", k, attrs[k], want)
		}
	}
}

func TestLogHandlerAddsTraceIDs(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(LogHandler(slog.NewJSONHandler(&buf, nil)))

	traceID, _ := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	spanID, _ := trace.SpanIDFromHex("0102030405060708")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))

	log.InfoContext(ctx, "with span")
	log.Info("without span")
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if !strings.Contains(lines[0], `"trace_id":"0102030405060708090a0b0c0d0e0f10"`) || !strings.Contains(lines[0], `"span_id":"0102030405060708"`) {
		t.Errorf("trace ids missing: %s", lines[0])
	}
	if strings.Contains(lines[1], "trace_id") {
		t.Errorf("unexpected trace id without a span: %s", lines[1])
	}
}
