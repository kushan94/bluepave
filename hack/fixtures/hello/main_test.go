package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest("GET", "/", nil))
	if !strings.HasPrefix(rec.Body.String(), "hello from ") {
		t.Errorf("body = %q", rec.Body.String())
	}
}
