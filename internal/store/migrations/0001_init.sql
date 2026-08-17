CREATE TABLE IF NOT EXISTS webhook_deliveries (
    delivery_id text PRIMARY KEY,
    event text NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS installations (
    id bigint PRIMARY KEY,
    account_id bigint NOT NULL,
    account_login text NOT NULL,
    account_type text NOT NULL,
    installed_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS repos (
    id bigint PRIMARY KEY,
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    owner text NOT NULL,
    name text NOT NULL,
    default_branch text NOT NULL DEFAULT 'main',
    enabled boolean NOT NULL DEFAULT true,
    config jsonb,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_repos_installation ON repos(installation_id);

CREATE TABLE IF NOT EXISTS pull_requests (
    repo_id bigint NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    number int NOT NULL,
    last_analyzed_sha text NOT NULL DEFAULT '',
    review_id bigint NOT NULL DEFAULT 0,
    check_run_id bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repo_id, number)
);

CREATE TABLE IF NOT EXISTS analysis_runs (
    id bigserial PRIMARY KEY,
    repo_id bigint NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    kind text NOT NULL,
    input_sha text NOT NULL,
    pr_number int,
    status text NOT NULL DEFAULT 'queued',
    result jsonb,
    error text,
    started_at timestamptz,
    finished_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (repo_id, kind, input_sha)
);

CREATE TABLE IF NOT EXISTS findings (
    id bigserial PRIMARY KEY,
    run_id bigint NOT NULL REFERENCES analysis_runs(id) ON DELETE CASCADE,
    finding_id text NOT NULL,
    file text NOT NULL,
    line int NOT NULL DEFAULT 0,
    severity text NOT NULL,
    category text NOT NULL,
    title text NOT NULL,
    body text NOT NULL,
    suggestion jsonb,
    confidence double precision NOT NULL DEFAULT 0,
    actionable boolean NOT NULL DEFAULT true,
    dedupe_hash text NOT NULL,
    comment_id bigint NOT NULL DEFAULT 0,
    issue_number int NOT NULL DEFAULT 0,
    UNIQUE (run_id, dedupe_hash)
);
CREATE INDEX IF NOT EXISTS idx_findings_comment ON findings(comment_id) WHERE comment_id > 0;

CREATE TABLE IF NOT EXISTS issues (
    id bigserial PRIMARY KEY,
    repo_id bigint NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    number int NOT NULL,
    title text NOT NULL,
    kind text NOT NULL DEFAULT 'finding',
    pr_number int NOT NULL DEFAULT 0,
    finding_ids text[] NOT NULL DEFAULT '{}',
    status text NOT NULL DEFAULT 'open',
    created_at timestamptz NOT NULL DEFAULT now(),
    closed_at timestamptz,
    UNIQUE (repo_id, number)
);

CREATE TABLE IF NOT EXISTS jobs (
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
);
CREATE INDEX IF NOT EXISTS idx_jobs_claim ON jobs (status, run_at) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_jobs_running ON jobs (lease_expires) WHERE status = 'running';

CREATE TABLE IF NOT EXISTS learnings (
    id bigserial PRIMARY KEY,
    repo_id bigint NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    key text NOT NULL,
    signal text NOT NULL DEFAULT 'up',
    weight int NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (repo_id, key, signal)
);

CREATE TABLE IF NOT EXISTS audit_log (
    id bigserial PRIMARY KEY,
    delivery_id text NOT NULL DEFAULT '',
    event text NOT NULL,
    action text NOT NULL DEFAULT '',
    repo_id bigint NOT NULL DEFAULT 0,
    kind text NOT NULL DEFAULT '',
    detail jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
