// Package queue implements a durable Postgres-backed job queue using
// FOR UPDATE SKIP LOCKED.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/rand"
	"time"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultLeaseTTL = 15 * time.Minute

var _ domain.Queue = (*Queue)(nil)

var jitterRand = rand.New(rand.NewSource(time.Now().UnixNano()))

// Queue is a durable job queue backed by a Postgres jobs table.
type Queue struct {
	pool     *pgxpool.Pool
	leaseTTL time.Duration
}

// New returns a Queue with the given lease TTL; non-positive values default
// to 15 minutes.
func New(pool *pgxpool.Pool, leaseTTL time.Duration) *Queue {
	if leaseTTL <= 0 {
		leaseTTL = defaultLeaseTTL
	}
	return &Queue{pool: pool, leaseTTL: leaseTTL}
}

// Enqueue inserts a job with the given kind and JSON payload.
func (q *Queue) Enqueue(ctx context.Context, kind domain.JobKind, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = q.pool.Exec(ctx, `INSERT INTO jobs (kind, payload) VALUES ($1, $2)`, string(kind), data)
	return err
}

// Dequeue atomically claims the next runnable job.
func (q *Queue) Dequeue(ctx context.Context) (*domain.Job, bool, error) {
	rows, err := q.pool.Query(ctx, `
		UPDATE jobs SET status='running', attempts=attempts+1,
			lease_expires=now() + $1::interval, updated_at=now()
		WHERE id = (
			SELECT id FROM jobs
			WHERE status='pending' AND run_at <= now()
			ORDER BY run_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, kind, payload, attempts, max_attempts, status, run_at, COALESCE(last_error, '') AS last_error, created_at, updated_at;
	`, q.leaseTTL.String())
	if err != nil {
		return nil, false, err
	}
	job, err := pgx.CollectOneRow(rows, pgx.RowToStructByNameLax[domain.Job])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &job, true, nil
}

// Complete removes a finished job.
func (q *Queue) Complete(ctx context.Context, jobID int64) error {
	_, err := q.pool.Exec(ctx, `DELETE FROM jobs WHERE id=$1`, jobID)
	return err
}

// Fail marks a job failed; if retries remain it is rescheduled with
// exponential backoff plus jitter.
func (q *Queue) Fail(ctx context.Context, jobID int64, errMsg string) error {
	var attempts int
	err := q.pool.QueryRow(ctx, `SELECT attempts FROM jobs WHERE id=$1`, jobID).Scan(&attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	delay := BackoffDelay(attempts)
	jitter := 0.8 + jitterRand.Float64()*0.4
	delay = time.Duration(float64(delay) * jitter)
	ct, err := q.pool.Exec(ctx, `
		UPDATE jobs SET status='pending', last_error=$2, run_at=now() + $3::interval, updated_at=now()
		WHERE id=$1 AND attempts < max_attempts AND status='running';
	`, jobID, errMsg, delay.String())
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		_, err = q.pool.Exec(ctx, `
			UPDATE jobs SET status='failed', last_error=$2, updated_at=now()
			WHERE id=$1 AND status='running';
		`, jobID, errMsg)
	}
	return err
}

// ReapExpired resets running jobs whose lease expired.
func (q *Queue) ReapExpired(ctx context.Context, leaseTTL time.Duration) (int64, error) {
	ct, err := q.pool.Exec(ctx, `
		UPDATE jobs SET status='pending', lease_expires=NULL, updated_at=now()
		WHERE status='running' AND lease_expires < now();
	`)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

// BackoffDelay returns min(2^attempts, 600) seconds.
func BackoffDelay(attempts int) time.Duration {
	if attempts <= 0 {
		return time.Second
	}
	exp := math.Pow(2, float64(attempts))
	if exp > 600 {
		exp = 600
	}
	return time.Duration(exp * float64(time.Second))
}
