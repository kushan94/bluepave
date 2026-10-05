// Command worker processes queued jobs. It claims them from PostgreSQL (FOR UPDATE SKIP LOCKED,
// so any number of workers can run), and wakes up early when the API publishes a job on the
// cache. It also requeues jobs a dead worker left in "running", e.g. after a Spot eviction.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"bluepave.dev/examples/anvil/internal/archive"
	"bluepave.dev/examples/anvil/internal/cache"
	"bluepave.dev/examples/anvil/internal/jobs"
	"bluepave.dev/examples/anvil/internal/server"
	"bluepave.dev/examples/anvil/internal/store"
	"bluepave.dev/examples/anvil/internal/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var (
	jobsProcessed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "anvil_jobs_processed_total",
		Help: "Jobs processed, by kind and outcome (done or failed).",
	}, []string{"kind", "outcome"})

	jobDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "anvil_job_duration_seconds",
		Help:    "Time from claiming a job to saving its result.",
		Buckets: []float64{0.5, 1, 2, 3, 5, 10, 30},
	}, []string{"kind"})

	jobsQueued = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "anvil_jobs_queued",
		Help: "Jobs waiting to be processed (queue depth).",
	})

	jobsArchived = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "anvil_jobs_archived_total",
		Help: "Finished jobs copied to blob storage, by outcome (ok or error).",
	}, []string{"outcome"})

	tracer = otel.Tracer("bluepave.dev/examples/anvil/cmd/worker")
)

func init() {
	telemetry.Registry.MustRegister(jobsProcessed, jobDuration, jobsQueued, jobsArchived)
}

const (
	queueChannel  = "anvil:jobs"
	pollInterval  = 5 * time.Second
	queueInterval = 15 * time.Second
	staleAfter    = 2 * time.Minute
	reapInterval  = time.Minute
	livenessLimit = time.Minute
)

func main() {
	log := server.Logger("worker")
	ctx, stop := server.SignalContext()
	defer stop()
	defer server.Start(ctx, log, "worker")()

	workDelay, err := time.ParseDuration(server.Env("WORK_DELAY", "2s"))
	if err != nil {
		log.Error("WORK_DELAY", "err", err)
		os.Exit(1)
	}

	pool, err := store.Connect(ctx)
	if err != nil {
		log.Error("connect to PostgreSQL", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	db := store.New(pool)

	c, err := cache.FromEnv()
	if err != nil {
		log.Error("configure cache", "err", err)
		os.Exit(1)
	}
	defer c.Close()

	// Optional: copies finished jobs to the blob storage the platform's AppStorage API created.
	arch, err := archive.FromEnv()
	if err != nil {
		log.Error("configure archive", "err", err)
		os.Exit(1)
	}
	log.Info("archive", "enabled", arch != nil)

	w := &worker{store: db, archive: arch, log: log, workDelay: workDelay}
	w.heartbeat()

	// Liveness: the pod is restarted if the main loop stops making progress.
	health := http.NewServeMux()
	health.HandleFunc("GET /healthz", func(rw http.ResponseWriter, r *http.Request) {
		if time.Since(time.Unix(w.lastBeat.Load(), 0)) > livenessLimit {
			http.Error(rw, "main loop stalled", http.StatusServiceUnavailable)
			return
		}
		rw.WriteHeader(http.StatusOK)
	})
	go func() {
		if err := server.Run(ctx, log, ":"+server.Env("PORT", "8081"), health); err != nil {
			log.Error("health server", "err", err)
		}
	}()

	w.run(ctx, c.Subscribe(ctx, queueChannel))
	log.Info("stopped")
}

type worker struct {
	store     *store.Store
	archive   archive.Archiver // nil: archiving disabled
	log       *slog.Logger
	workDelay time.Duration
	lastBeat  atomic.Int64
}

func (w *worker) heartbeat() { w.lastBeat.Store(time.Now().Unix()) }

func (w *worker) run(ctx context.Context, wake <-chan struct{}) {
	poll := time.NewTicker(pollInterval)
	defer poll.Stop()
	reap := time.NewTicker(reapInterval)
	defer reap.Stop()
	queue := time.NewTicker(queueInterval)
	defer queue.Stop()

	for {
		w.heartbeat()
		// Drain the queue, then wait for a wake-up, the poll timer or shutdown.
		for ctx.Err() == nil && w.processOne(ctx) {
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-poll.C:
		case <-queue.C:
			if n, err := w.store.CountQueued(ctx); err == nil {
				jobsQueued.Set(float64(n))
			}
		case <-reap.C:
			if n, err := w.store.RequeueStale(ctx, staleAfter); err != nil {
				w.log.Warn("requeue stale jobs", "err", err)
			} else if n > 0 {
				w.log.Info("requeued stale jobs", "count", n)
			}
		}
	}
}

// processOne claims and runs one job. It returns false when there was nothing to do (or the
// database is unreachable), so the caller waits before trying again.
func (w *worker) processOne(ctx context.Context) bool {
	job, err := w.store.ClaimNext(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return false
	}
	if err != nil {
		if ctx.Err() == nil {
			w.log.Warn("claim job", "err", err)
		}
		return false
	}
	w.heartbeat()
	start := time.Now()

	// One span per job; the claim above and the save below show up as child PostgreSQL spans.
	ctx, span := tracer.Start(ctx, "process job")
	defer span.End()
	span.SetAttributes(attribute.Int64("anvil.job.id", job.ID), attribute.String("anvil.job.kind", string(job.Kind)))

	// Simulated work, so the UI can show jobs moving through "running".
	select {
	case <-ctx.Done():
		// Shutting down mid-job: leave it running; RequeueStale hands it to another worker.
		return false
	case <-time.After(w.workDelay):
	}

	result, jobErr := jobs.Process(job.Kind, job.Input)
	// Use a fresh context: the result must be saved even if shutdown started meanwhile.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	finished, err := w.store.Finish(saveCtx, job.ID, result, jobErr)
	if err != nil {
		span.SetStatus(codes.Error, "save result")
		w.log.ErrorContext(ctx, "save result", "id", job.ID, "err", err)
		return false
	}
	w.archiveJob(saveCtx, finished)
	outcome := "done"
	if jobErr != nil {
		outcome = "failed"
		span.SetStatus(codes.Error, jobErr.Error())
	}
	jobsProcessed.WithLabelValues(string(job.Kind), outcome).Inc()
	jobDuration.WithLabelValues(string(job.Kind)).Observe(time.Since(start).Seconds())
	w.log.InfoContext(ctx, "job finished", "id", job.ID, "kind", job.Kind, "ok", jobErr == nil)
	return true
}

// archiveJob copies a finished job to blob storage. Best-effort: PostgreSQL already has the
// result, so a failure is logged and counted, never retried or surfaced to the user.
func (w *worker) archiveJob(ctx context.Context, job jobs.Job) {
	if w.archive == nil {
		return
	}
	if err := w.archive.Put(ctx, job); err != nil {
		jobsArchived.WithLabelValues("error").Inc()
		w.log.WarnContext(ctx, "archive job", "id", job.ID, "err", err)
		return
	}
	jobsArchived.WithLabelValues("ok").Inc()
}
