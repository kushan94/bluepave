package telemetry

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
)

// RED metrics for HTTP servers: Rate, Errors (by status code), Duration. Labelled by route
// pattern ("GET /api/jobs/{id}"), never by raw path, so label cardinality stays bounded.
var (
	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "anvil_http_requests_total",
		Help: "HTTP requests handled, by component, route and status code.",
	}, []string{"component", "route", "code"})

	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "anvil_http_request_duration_seconds",
		Help:    "HTTP request latency, by component and route.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	}, []string{"component", "route"})
)

func init() {
	Registry.MustRegister(httpRequests, httpDuration)
}

// HTTPMiddleware traces and measures every request. Wrap the ServeMux with it: the mux sets
// r.Pattern while routing, which this middleware then uses as span name and metric label.
func HTTPMiddleware(component string, mux http.Handler) http.Handler {
	measured := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		mux.ServeHTTP(rec, r)

		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		trace.SpanFromContext(r.Context()).SetName(route)
		httpRequests.WithLabelValues(component, route, strconv.Itoa(rec.status)).Inc()
		httpDuration.WithLabelValues(component, route).Observe(time.Since(start).Seconds())
	})
	return otelhttp.NewHandler(measured, component,
		// Health probes every few seconds would drown the real traces.
		otelhttp.WithFilter(func(r *http.Request) bool {
			return r.URL.Path != "/healthz" && r.URL.Path != "/readyz"
		}))
}

// HTTPTransport propagates the current trace to outgoing requests and records client spans.
func HTTPTransport(base http.RoundTripper) http.RoundTripper {
	return otelhttp.NewTransport(base)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the underlying writer (flushing, deadlines).
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }
