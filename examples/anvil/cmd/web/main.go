// Command web serves the Anvil UI and forwards /api/ to the API service, so the browser
// talks to a single origin (no CORS) and the API is never exposed directly.
package main

import (
	"embed"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"bluepave.dev/examples/anvil/internal/server"
	"bluepave.dev/examples/anvil/internal/telemetry"
)

//go:embed static
var static embed.FS

func main() {
	log := server.Logger("web")
	ctx, stop := server.SignalContext()
	defer stop()
	defer server.Start(ctx, log, "web")()

	apiURL, err := url.Parse(server.Env("API_URL", "http://api:8080"))
	if err != nil {
		log.Error("API_URL", "err", err)
		os.Exit(1)
	}
	if err := server.Run(ctx, log, ":"+server.Env("PORT", "8080"), routes(apiURL)); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}

func routes(apiURL *url.URL) http.Handler {
	files, _ := fs.Sub(static, "static")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	proxy := httputil.NewSingleHostReverseProxy(apiURL)
	// Passes the trace on to the API, so one trace spans web -> api -> PostgreSQL.
	proxy.Transport = telemetry.HTTPTransport(http.DefaultTransport)
	mux.Handle("/api/", proxy)
	// No method here: "GET /" would overlap "/api/" ambiguously and ServeMux panics at startup.
	mux.Handle("/", http.FileServerFS(files))
	return securityHeaders(telemetry.HTTPMiddleware("web", mux))
}

// securityHeaders applies a strict browser security policy: only same-origin scripts, styles
// and requests; no framing; no MIME sniffing; no cross-origin embedding; nothing cached (the
// pages show live data and the static files are tiny).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Embedder-Policy", "require-corp")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(noStoreWriter{w}, r)
	})
}

// noStoreWriter sets Cache-Control as the status is written: http.FileServer deletes it from
// error responses (404s included), which would leave those cacheable.
type noStoreWriter struct{ http.ResponseWriter }

func (w noStoreWriter) WriteHeader(code int) {
	w.Header().Set("Cache-Control", "no-store")
	w.ResponseWriter.WriteHeader(code)
}

// Write without WriteHeader means an implicit 200.
func (w noStoreWriter) Write(b []byte) (int, error) {
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController (used by the reverse proxy to flush) reach the real writer.
func (w noStoreWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
