package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Building the routes catches pattern conflicts, which panic at startup (and crash-looped the
// first deployment).
func TestRoutes(t *testing.T) {
	var apiPaths []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiPaths = append(apiPaths, r.Method+" "+r.URL.Path)
		_, _ = io.WriteString(w, `[]`)
	}))
	defer api.Close()
	apiURL, _ := url.Parse(api.URL)
	h := routes(apiURL)

	tests := []struct {
		method, path string
		status       int
		bodyContains string
	}{
		{"GET", "/healthz", http.StatusOK, ""},
		{"GET", "/", http.StatusOK, "Anvil"},
		{"GET", "/app.js", http.StatusOK, "use strict"},
		{"GET", "/api/jobs", http.StatusOK, "[]"},
		{"POST", "/api/jobs", http.StatusOK, "[]"},
		{"GET", "/missing", http.StatusNotFound, ""},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if !strings.Contains(rec.Body.String(), tt.bodyContains) {
				t.Errorf("body does not contain %q", tt.bodyContains)
			}
			for _, name := range []string{"Content-Security-Policy", "Cross-Origin-Embedder-Policy", "Cache-Control"} {
				if rec.Header().Get(name) == "" {
					t.Errorf("missing %s header", name)
				}
			}
		})
	}
	if len(apiPaths) != 2 {
		t.Errorf("API received %v, want the two /api/ requests", apiPaths)
	}
}
