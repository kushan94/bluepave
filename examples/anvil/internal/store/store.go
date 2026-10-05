// Package store keeps jobs in PostgreSQL. PostgreSQL is also the queue: workers claim jobs with
// FOR UPDATE SKIP LOCKED, so a job is never lost if the cache (which only carries wake-up
// notifications) restarts.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"bluepave.dev/examples/anvil/internal/jobs"
)

// ErrNotFound is returned when a job doesn't exist.
var ErrNotFound = errors.New("job not found")

// Store reads and writes jobs.
type Store struct {
	pool *pgxpool.Pool
}

// New wraps a connection pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const schema = `
CREATE TABLE IF NOT EXISTS jobs (
	id         bigserial   PRIMARY KEY,
	kind       text        NOT NULL,
	input      text        NOT NULL,
	status     text        NOT NULL DEFAULT 'queued',
	result     text        NOT NULL DEFAULT '',
	error      text        NOT NULL DEFAULT '',
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);
-- Workers only ever look for queued jobs; keep that lookup cheap as the table grows.
CREATE INDEX IF NOT EXISTS jobs_queued ON jobs (id) WHERE status = 'queued';
`

// Migrate creates the schema. Only the API runs it, so two processes never race on DDL.
func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, schema)
	return err
}

// Ping checks the database is reachable (readiness).
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

const columns = `id, kind, input, status, result, error, created_at, updated_at`

func scan(row pgx.Row) (jobs.Job, error) {
	var j jobs.Job
	err := row.Scan(&j.ID, &j.Kind, &j.Input, &j.Status, &j.Result, &j.Error, &j.CreatedAt, &j.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return j, ErrNotFound
	}
	return j, err
}

// Create queues a new job.
func (s *Store) Create(ctx context.Context, kind jobs.Kind, input string) (jobs.Job, error) {
	return scan(s.pool.QueryRow(ctx,
		`INSERT INTO jobs (kind, input) VALUES ($1, $2) RETURNING `+columns, kind, input))
}

// Get returns one job.
func (s *Store) Get(ctx context.Context, id int64) (jobs.Job, error) {
	return scan(s.pool.QueryRow(ctx, `SELECT `+columns+` FROM jobs WHERE id = $1`, id))
}

// List returns the most recent jobs, newest first.
func (s *Store) List(ctx context.Context, limit int) ([]jobs.Job, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+columns+` FROM jobs ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []jobs.Job{}
	for rows.Next() {
		j, err := scan(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, j)
	}
	return list, rows.Err()
}

// ClaimNext atomically moves the oldest queued job to running and returns it. SKIP LOCKED lets
// many workers claim different jobs concurrently without blocking each other.
// It returns ErrNotFound when the queue is empty.
func (s *Store) ClaimNext(ctx context.Context) (jobs.Job, error) {
	return scan(s.pool.QueryRow(ctx, `
		UPDATE jobs SET status = 'running', updated_at = now()
		WHERE id = (
			SELECT id FROM jobs WHERE status = 'queued'
			ORDER BY id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING `+columns))
}

// Finish records a job's outcome.
func (s *Store) Finish(ctx context.Context, id int64, result string, jobErr error) (jobs.Job, error) {
	status, errText := jobs.StatusDone, ""
	if jobErr != nil {
		status, errText, result = jobs.StatusFailed, jobErr.Error(), ""
	}
	return scan(s.pool.QueryRow(ctx, `
		UPDATE jobs SET status = $2, result = $3, error = $4, updated_at = now()
		WHERE id = $1
		RETURNING `+columns, id, status, result, errText))
}

// CountQueued returns how many jobs are waiting (the queue-depth metric).
func (s *Store) CountQueued(ctx context.Context) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE status = 'queued'`).Scan(&n)
	return n, err
}

// RequeueStale puts back jobs stuck in running longer than olderThan, e.g. because a worker
// was killed mid-job (a Spot eviction does exactly that). It returns how many it requeued.
func (s *Store) RequeueStale(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE jobs SET status = 'queued', updated_at = now()
		WHERE status = 'running' AND updated_at < now() - make_interval(secs => $1)`,
		olderThan.Seconds())
	if err != nil {
		return 0, fmt.Errorf("requeue stale jobs: %w", err)
	}
	return tag.RowsAffected(), nil
}
