package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutes(t *testing.T) {
	h := handler()
	for _, tt := range []struct {
		path   string
		status int
	}{
		{"/healthz", http.StatusOK},
		{"/readyz", http.StatusOK},
		{"/", http.StatusOK},
		{"/missing", http.StatusNotFound},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", tt.path, nil))
		if rec.Code != tt.status {
			t.Errorf("GET %s = %d, want %d", tt.path, rec.Code, tt.status)
		}
	}
}

func TestMetrics(t *testing.T) {
	rec := httptest.NewRecorder()
	metrics().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "_http_requests_total") {
		t.Errorf("metrics output missing the request counter:\n%s", rec.Body.String())
	}
}
