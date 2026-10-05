package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"bluepave.dev/examples/anvil/internal/cache"
	"bluepave.dev/examples/anvil/internal/jobs"
	"bluepave.dev/examples/anvil/internal/store"
	"bluepave.dev/examples/anvil/internal/telemetry"
)

// jobStore is what the handlers need from the database (store.Store implements it).
type jobStore interface {
	Create(ctx context.Context, kind jobs.Kind, input string) (jobs.Job, error)
	Get(ctx context.Context, id int64) (jobs.Job, error)
	List(ctx context.Context, limit int) ([]jobs.Job, error)
	Ping(ctx context.Context) error
}

// queueChannel carries "a job was queued" wake-ups to workers.
const queueChannel = "anvil:jobs"

// finishedTTL is how long a finished job stays cached; finished jobs never change.
const finishedTTL = 10 * time.Minute

type api struct {
	store jobStore
	cache cache.Cache
	log   *slog.Logger
	// faultRate is the share (0-1) of /api/ requests answered with HTTP 500. It exists only to
	// demonstrate that canary analysis catches a bad release and rolls it back.
	faultRate float64
	requests  atomic.Uint64
}

func (a *api) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", a.ready)
	// API routes go through fault injection inside the mux, so injected errors are measured
	// (and traced) under their real route, exactly like real ones.
	mux.Handle("GET /api/kinds", a.injectFaults(http.HandlerFunc(a.kinds)))
	mux.Handle("POST /api/jobs", a.injectFaults(http.HandlerFunc(a.createJob)))
	mux.Handle("GET /api/jobs", a.injectFaults(http.HandlerFunc(a.listJobs)))
	mux.Handle("GET /api/jobs/{id}", a.injectFaults(http.HandlerFunc(a.getJob)))
	// Telemetry wraps the mux directly: MaxBytesHandler copies the request, which would hide
	// the route pattern the mux records on it.
	return http.MaxBytesHandler(telemetry.HTTPMiddleware("api", mux), 64<<10)
}

// injectFaults fails a share of /api/ requests when faultRate > 0: deterministically, the first
// faultRate*100 of every 100 requests (no randomness, so behaviour is reproducible). Health
// endpoints are never affected, so Kubernetes keeps the pods running and only the canary
// analysis sees the errors.
func (a *api) injectFaults(next http.Handler) http.Handler {
	if a.faultRate <= 0 {
		return next
	}
	failPerHundred := uint64(a.faultRate*100 + 0.5)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && (a.requests.Add(1)-1)%100 < failPerHundred {
			writeError(w, http.StatusInternalServerError, "injected fault")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ready fails when the database is unreachable, so Kubernetes stops sending traffic. The cache
// is optional and doesn't affect readiness.
func (a *api) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.store.Ping(ctx); err != nil {
		a.log.Warn("not ready", "err", err)
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *api) kinds(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, jobs.Kinds)
}

type createRequest struct {
	Kind  jobs.Kind `json:"kind"`
	Input string    `json:"input"`
}

func (a *api) createJob(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "body must be JSON: {\"kind\": ..., \"input\": ...}")
		return
	}
	if err := jobs.Validate(req.Kind, req.Input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	job, err := a.store.Create(r.Context(), req.Kind, req.Input)
	if err != nil {
		a.serverError(w, "create job", err)
		return
	}
	// Best effort: workers also poll, so a lost wake-up only delays the job by a few seconds.
	if err := a.cache.Publish(r.Context(), queueChannel, strconv.FormatInt(job.ID, 10)); err != nil {
		a.log.Warn("publish wake-up", "err", err)
	}
	a.log.InfoContext(r.Context(), "job queued", "id", job.ID, "kind", job.Kind)
	writeJSON(w, http.StatusCreated, job)
}

func (a *api) listJobs(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 100 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 100")
			return
		}
		limit = n
	}
	list, err := a.store.List(r.Context(), limit)
	if err != nil {
		a.serverError(w, "list jobs", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *api) getJob(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return
	}
	key := "anvil:job:" + strconv.FormatInt(id, 10)

	if cached, err := a.cache.Get(r.Context(), key); err == nil {
		w.Header().Set("X-Cache", "hit")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cached)
		return
	} else if !errors.Is(err, cache.ErrMiss) {
		a.log.Warn("cache get", "err", err)
	}

	job, err := a.store.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		a.serverError(w, "get job", err)
		return
	}
	body, _ := json.Marshal(job)
	if job.Status.Finished() {
		if err := a.cache.Set(r.Context(), key, body, finishedTTL); err != nil {
			a.log.Warn("cache set", "err", err)
		}
	}
	w.Header().Set("X-Cache", "miss")
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// serverError logs the real error and returns a generic message, so internals don't leak.
func (a *api) serverError(w http.ResponseWriter, action string, err error) {
	a.log.Error(action, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
