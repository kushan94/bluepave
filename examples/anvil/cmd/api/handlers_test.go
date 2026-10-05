package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"bluepave.dev/examples/anvil/internal/cache"
	"bluepave.dev/examples/anvil/internal/jobs"
	"bluepave.dev/examples/anvil/internal/store"
)

// fakeStore is an in-memory jobStore.
type fakeStore struct {
	mu   sync.Mutex
	jobs []jobs.Job
	gets int
}

func (f *fakeStore) Create(_ context.Context, kind jobs.Kind, input string) (jobs.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j := jobs.Job{ID: int64(len(f.jobs) + 1), Kind: kind, Input: input, Status: jobs.StatusQueued, CreatedAt: time.Now()}
	f.jobs = append(f.jobs, j)
	return j, nil
}

func (f *fakeStore) Get(_ context.Context, id int64) (jobs.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	for _, j := range f.jobs {
		if j.ID == id {
			return j, nil
		}
	}
	return jobs.Job{}, store.ErrNotFound
}

func (f *fakeStore) List(_ context.Context, limit int) ([]jobs.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.jobs[:min(limit, len(f.jobs))], nil
}

func (f *fakeStore) Ping(context.Context) error { return nil }

// memCache is an in-memory cache.Cache that records publishes.
type memCache struct {
	cache.Noop
	mu        sync.Mutex
	data      map[string][]byte
	published []string
}

func (m *memCache) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.data[key]; ok {
		return b, nil
	}
	return nil, cache.ErrMiss
}

func (m *memCache) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *memCache) Publish(_ context.Context, _, msg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.published = append(m.published, msg)
	return nil
}

func newTestAPI() (*api, *fakeStore, *memCache) {
	s := &fakeStore{}
	c := &memCache{data: map[string][]byte{}}
	return &api{store: s, cache: c, log: slog.New(slog.NewTextHandler(io.Discard, nil))}, s, c
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCreateJob(t *testing.T) {
	a, s, c := newTestAPI()
	rec := do(t, a.routes(), "POST", "/api/jobs", `{"kind":"uppercase","input":"hello"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var job jobs.Job
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.ID != 1 || job.Status != jobs.StatusQueued || len(s.jobs) != 1 {
		t.Errorf("unexpected job %+v", job)
	}
	if len(c.published) != 1 || c.published[0] != "1" {
		t.Errorf("worker wake-up not published: %v", c.published)
	}
}

func TestCreateJobRejectsBadInput(t *testing.T) {
	a, s, _ := newTestAPI()
	for name, body := range map[string]string{
		"unknown kind":  `{"kind":"rm -rf","input":"x"}`,
		"empty input":   `{"kind":"reverse","input":"  "}`,
		"not json":      `kind=reverse`,
		"unknown field": `{"kind":"reverse","input":"x","admin":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			if rec := do(t, a.routes(), "POST", "/api/jobs", body); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
	if len(s.jobs) != 0 {
		t.Errorf("invalid requests created %d jobs", len(s.jobs))
	}
}

func TestGetJobCachesOnlyFinishedJobs(t *testing.T) {
	a, s, _ := newTestAPI()
	h := a.routes()
	do(t, h, "POST", "/api/jobs", `{"kind":"reverse","input":"abc"}`)

	// Queued: never cached, because it will change.
	for range 2 {
		if rec := do(t, h, "GET", "/api/jobs/1", ""); rec.Header().Get("X-Cache") != "miss" {
			t.Fatalf("queued job served from cache")
		}
	}

	s.jobs[0].Status, s.jobs[0].Result = jobs.StatusDone, "cba"
	do(t, h, "GET", "/api/jobs/1", "") // miss, then cached
	getsBefore := s.gets
	rec := do(t, h, "GET", "/api/jobs/1", "")
	if rec.Header().Get("X-Cache") != "hit" || s.gets != getsBefore {
		t.Errorf("finished job not served from cache (X-Cache=%s)", rec.Header().Get("X-Cache"))
	}
}

func TestGetJobErrors(t *testing.T) {
	a, _, _ := newTestAPI()
	h := a.routes()
	if rec := do(t, h, "GET", "/api/jobs/42", ""); rec.Code != http.StatusNotFound {
		t.Errorf("missing job: status = %d, want 404", rec.Code)
	}
	if rec := do(t, h, "GET", "/api/jobs/abc", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("bad id: status = %d, want 400", rec.Code)
	}
}

func TestFaultInjection(t *testing.T) {
	a, _, _ := newTestAPI()
	a.faultRate = 1
	h := a.routes()
	if rec := do(t, h, "GET", "/api/jobs", ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("/api/jobs with faultRate=1: status = %d, want 500", rec.Code)
	}
	if rec := do(t, h, "GET", "/healthz", ""); rec.Code != http.StatusOK {
		t.Errorf("/healthz must never fail: status = %d", rec.Code)
	}
	a.faultRate = 0
	if rec := do(t, a.routes(), "GET", "/api/jobs", ""); rec.Code != http.StatusOK {
		t.Errorf("/api/jobs with faultRate=0: status = %d, want 200", rec.Code)
	}
}

func TestFaultInjectionRate(t *testing.T) {
	a, _, _ := newTestAPI()
	a.faultRate = 0.25
	h := a.routes()
	failed := 0
	for range 200 {
		if do(t, h, "GET", "/api/jobs", "").Code == http.StatusInternalServerError {
			failed++
		}
	}
	if failed != 50 {
		t.Errorf("faultRate=0.25 over 200 requests: %d failed, want exactly 50", failed)
	}
}

func TestListJobsLimit(t *testing.T) {
	a, _, _ := newTestAPI()
	if rec := do(t, a.routes(), "GET", "/api/jobs?limit=1000", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
