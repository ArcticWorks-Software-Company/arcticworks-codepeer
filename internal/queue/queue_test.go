package queue

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBackoffDelay(t *testing.T) {
	cases := []struct {
		attempts int
		want     time.Duration
	}{
		{1, 2 * time.Second},
		{5, 32 * time.Second},
		{10, 600 * time.Second},
		{0, 1 * time.Second},
		{-3, 1 * time.Second},
	}
	for _, tc := range cases {
		if got := BackoffDelay(tc.attempts); got != tc.want {
			t.Errorf("BackoffDelay(%d) = %v, want %v", tc.attempts, got, tc.want)
		}
	}
}

func TestAnalyzePRPayloadRoundTrip(t *testing.T) {
	want := domain.AnalyzePRPayload{
		InstallationID: 42,
		RepoID:         7,
		RepoOwner:      "acme",
		RepoName:       "widgets",
		PRNumber:       123,
		HeadSHA:        "abc123",
		Action:         "synchronize",
		SenderLogin:    "octocat",
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got domain.AnalyzePRPayload
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, want)
	}
}

func TestAnalyzePushPayloadRoundTrip(t *testing.T) {
	want := domain.AnalyzePushPayload{
		InstallationID: 9,
		RepoID:         11,
		RepoOwner:      "acme",
		RepoName:       "widgets",
		Before:         "0000000000000000000000000000000000000000",
		After:          "def456",
		Ref:            "refs/heads/main",
		SenderLogin:    "octocat",
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got domain.AnalyzePushPayload
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, want)
	}
}

func TestIntegrationQueue(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	_, err = pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS jobs (
		id bigserial PRIMARY KEY,
		kind text NOT NULL,
		payload jsonb NOT NULL,
		status text NOT NULL DEFAULT 'pending',
		attempts int NOT NULL DEFAULT 0,
		max_attempts int NOT NULL DEFAULT 5,
		run_at timestamptz NOT NULL DEFAULT now(),
		lease_expires timestamptz,
		last_error text,
		created_at timestamptz NOT NULL DEFAULT now(),
		updated_at timestamptz NOT NULL DEFAULT now()
	); CREATE INDEX IF NOT EXISTS idx_jobs_claim ON jobs (status, run_at) WHERE status = 'pending';
	CREATE INDEX IF NOT EXISTS idx_jobs_running ON jobs (lease_expires) WHERE status = 'running';`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	defer pool.Exec(context.Background(), `DROP TABLE jobs`)

	q := New(pool, 15*time.Minute)

	prPayload := domain.AnalyzePRPayload{
		InstallationID: 1,
		RepoID:         2,
		RepoOwner:      "acme",
		RepoName:       "widgets",
		PRNumber:       1,
		HeadSHA:        "sha-pr",
		Action:         "opened",
		SenderLogin:    "octocat",
	}
	if err := q.Enqueue(ctx, domain.JobAnalyzePR, prPayload); err != nil {
		t.Fatalf("enqueue pr: %v", err)
	}
	pushPayload := domain.AnalyzePushPayload{
		InstallationID: 1,
		RepoID:         2,
		RepoOwner:      "acme",
		RepoName:       "widgets",
		Before:         "before",
		After:          "after",
		Ref:            "refs/heads/main",
		SenderLogin:    "octocat",
	}
	if err := q.Enqueue(ctx, domain.JobAnalyzePush, pushPayload); err != nil {
		t.Fatalf("enqueue push: %v", err)
	}

	job1, ok, err := q.Dequeue(ctx)
	if err != nil || !ok {
		t.Fatalf("first dequeue: ok=%v err=%v", ok, err)
	}
	job2, ok, err := q.Dequeue(ctx)
	if err != nil || !ok {
		t.Fatalf("second dequeue: ok=%v err=%v", ok, err)
	}
	if job1.ID == job2.ID {
		t.Fatalf("dequeued same job twice: %d", job1.ID)
	}
	if job1.Status != domain.JobRunning || job2.Status != domain.JobRunning {
		t.Fatalf("claimed jobs must be running: %q %q", job1.Status, job2.Status)
	}

	if _, ok, err := q.Dequeue(ctx); err != nil || ok {
		t.Fatalf("third dequeue should be empty: ok=%v err=%v", ok, err)
	}

	if job2.Kind != domain.JobAnalyzePush {
		t.Fatalf("kind = %q, want %q", job2.Kind, domain.JobAnalyzePush)
	}
	var gotPush domain.AnalyzePushPayload
	if err := json.Unmarshal(job2.Payload, &gotPush); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !reflect.DeepEqual(gotPush, pushPayload) {
		t.Fatalf("payload mismatch: got %+v want %+v", gotPush, pushPayload)
	}

	if err := q.Fail(ctx, job1.ID, "boom"); err != nil {
		t.Fatalf("fail: %v", err)
	}
	var runAt time.Time
	var lastError string
	err = pool.QueryRow(ctx, `SELECT run_at, last_error FROM jobs WHERE id=$1`, job1.ID).Scan(&runAt, &lastError)
	if err != nil {
		t.Fatalf("select after fail: %v", err)
	}
	if !runAt.After(time.Now()) {
		t.Fatalf("run_at %v should be in the future after Fail", runAt)
	}
	if lastError != "boom" {
		t.Fatalf("last_error = %q, want %q", lastError, "boom")
	}

	if _, ok, err := q.Dequeue(ctx); err != nil || ok {
		t.Fatalf("dequeue during backoff should be empty: ok=%v err=%v", ok, err)
	}

	if _, err := pool.Exec(ctx, `UPDATE jobs SET run_at=now() - interval '1 second' WHERE id=$1`, job1.ID); err != nil {
		t.Fatalf("force run_at: %v", err)
	}
	retried, ok, err := q.Dequeue(ctx)
	if err != nil || !ok {
		t.Fatalf("dequeue after backoff: ok=%v err=%v", ok, err)
	}
	if retried.ID != job1.ID {
		t.Fatalf("retried job id = %d, want %d", retried.ID, job1.ID)
	}
	if retried.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", retried.Attempts)
	}

	if err := q.Complete(ctx, job1.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, ok, err := q.Dequeue(ctx); err != nil || ok {
		t.Fatalf("dequeue after complete should be empty: ok=%v err=%v", ok, err)
	}

	if _, err := pool.Exec(ctx, `UPDATE jobs SET lease_expires=now() - interval '1 hour' WHERE id=$1 AND status='running'`, job2.ID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	reaped, err := q.ReapExpired(ctx, 0)
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if reaped != 1 {
		t.Fatalf("reaped = %d, want 1", reaped)
	}
	reclaimed, ok, err := q.Dequeue(ctx)
	if err != nil || !ok {
		t.Fatalf("dequeue after reap: ok=%v err=%v", ok, err)
	}
	if reclaimed.ID != job2.ID {
		t.Fatalf("reclaimed job id = %d, want %d", reclaimed.ID, job2.ID)
	}
	if reclaimed.Attempts != 2 {
		t.Fatalf("reclaimed attempts = %d, want 2", reclaimed.Attempts)
	}
	if err := q.Complete(ctx, job2.ID); err != nil {
		t.Fatalf("complete job2: %v", err)
	}

	exhausted := domain.AnalyzePRPayload{RepoID: 99}
	if err := q.Enqueue(ctx, domain.JobAnalyzePR, exhausted); err != nil {
		t.Fatalf("enqueue exhausted: %v", err)
	}
	dead, ok, err := q.Dequeue(ctx)
	if err != nil || !ok {
		t.Fatalf("dequeue exhausted: ok=%v err=%v", ok, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET attempts=max_attempts WHERE id=$1`, dead.ID); err != nil {
		t.Fatalf("exhaust attempts: %v", err)
	}
	if err := q.Fail(ctx, dead.ID, "out of retries"); err != nil {
		t.Fatalf("fail exhausted: %v", err)
	}
	var status string
	err = pool.QueryRow(ctx, `SELECT status FROM jobs WHERE id=$1`, dead.ID).Scan(&status)
	if err != nil {
		t.Fatalf("select status: %v", err)
	}
	if status != string(domain.JobFailed) {
		t.Fatalf("status = %q, want %q", status, domain.JobFailed)
	}
}
