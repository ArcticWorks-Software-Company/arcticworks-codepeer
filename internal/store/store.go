// Package store implements the domain.Store interface on Postgres.
package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrDuplicateRun is returned when an analysis run for the same
// (repo_id, kind, input_sha) already exists.
var ErrDuplicateRun = errors.New("duplicate analysis run")

// Store persists CodePeer state in Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a Store backed by pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Ping verifies the database is reachable.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// RecordDelivery records a webhook delivery, returning false if it was
// already recorded.
func (s *Store) RecordDelivery(ctx context.Context, deliveryID, event string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `INSERT INTO webhook_deliveries (delivery_id, event) VALUES ($1, $2) ON CONFLICT (delivery_id) DO NOTHING`, deliveryID, event)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// UpsertInstallation inserts or updates an installation.
func (s *Store) UpsertInstallation(ctx context.Context, inst domain.Installation) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO installations (id, account_id, account_login, account_type, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (id) DO UPDATE SET
			account_id = EXCLUDED.account_id,
			account_login = EXCLUDED.account_login,
			account_type = EXCLUDED.account_type,
			updated_at = now()`,
		inst.ID, inst.AccountID, inst.AccountLogin, inst.AccountType)
	return err
}

// UpsertRepo inserts or updates a repo, preserving its existing config.
func (s *Store) UpsertRepo(ctx context.Context, r domain.Repo) error {
	var cfg any
	if r.Config != nil {
		b, err := json.Marshal(r.Config)
		if err != nil {
			return err
		}
		cfg = string(b)
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO repos (id, installation_id, owner, name, default_branch, enabled, config, updated_at)
		VALUES ($1, $2, $3, $4, COALESCE(NULLIF($5, ''), 'main'), $6, $7, now())
		ON CONFLICT (id) DO UPDATE SET
			installation_id = EXCLUDED.installation_id,
			owner = EXCLUDED.owner,
			name = EXCLUDED.name,
			updated_at = now()`,
		r.ID, r.InstallationID, r.Owner, r.Name, r.DefaultBranch, r.Enabled, cfg)
	return err
}

type repoRow struct {
	ID             int64  `db:"id"`
	InstallationID int64  `db:"installation_id"`
	Owner          string `db:"owner"`
	Name           string `db:"name"`
	DefaultBranch  string `db:"default_branch"`
	Enabled        bool   `db:"enabled"`
	Config         []byte `db:"config"`
}

func repoFromRow(row repoRow) (*domain.Repo, error) {
	repo := &domain.Repo{
		ID:             row.ID,
		InstallationID: row.InstallationID,
		Owner:          row.Owner,
		Name:           row.Name,
		DefaultBranch:  row.DefaultBranch,
		Enabled:        row.Enabled,
	}
	if len(row.Config) > 0 && string(row.Config) != "null" {
		var cfg domain.RepoConfig
		if err := json.Unmarshal(row.Config, &cfg); err != nil {
			return nil, err
		}
		repo.Config = &cfg
	}
	return repo, nil
}

const repoColumns = `id, installation_id, owner, name, COALESCE(default_branch, 'main'), enabled, config`

// GetRepo returns the repo with the given ID.
func (s *Store) GetRepo(ctx context.Context, repoID int64) (*domain.Repo, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+repoColumns+` FROM repos WHERE id = $1`, repoID)
	if err != nil {
		return nil, err
	}
	row, err := parseRow[repoRow](rows)
	if err != nil {
		return nil, err
	}
	return repoFromRow(row)
}

// GetRepoByName returns the repo with the given owner and name.
func (s *Store) GetRepoByName(ctx context.Context, owner, name string) (*domain.Repo, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+repoColumns+` FROM repos WHERE owner = $1 AND name = $2`, owner, name)
	if err != nil {
		return nil, err
	}
	row, err := parseRow[repoRow](rows)
	if err != nil {
		return nil, err
	}
	return repoFromRow(row)
}

// ListReposForInstallation returns all repos for an installation.
func (s *Store) ListReposForInstallation(ctx context.Context, installationID int64) ([]domain.Repo, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+repoColumns+` FROM repos WHERE installation_id = $1 ORDER BY id`, installationID)
	if err != nil {
		return nil, err
	}
	rs, err := parseRows[repoRow](rows)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Repo, 0, len(rs))
	for _, r := range rs {
		repo, err := repoFromRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, *repo)
	}
	return out, nil
}

// SetRepoConfig stores the config for a repo.
func (s *Store) SetRepoConfig(ctx context.Context, repoID int64, cfg *domain.RepoConfig) error {
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE repos SET config = $2, updated_at = now() WHERE id = $1`, repoID, string(b))
	return err
}

type prRow struct {
	RepoID          int64  `db:"repo_id"`
	Number          int    `db:"number"`
	LastAnalyzedSHA string `db:"last_analyzed_sha"`
	ReviewID        int64  `db:"review_id"`
	CheckRunID      int64  `db:"check_run_id"`
}

// GetPRState returns the tracked state for a PR, or (nil, nil) if untracked.
func (s *Store) GetPRState(ctx context.Context, repoID int64, number int) (*domain.PRState, error) {
	rows, err := s.pool.Query(ctx, `SELECT repo_id, number, last_analyzed_sha, review_id, check_run_id FROM pull_requests WHERE repo_id = $1 AND number = $2`, repoID, number)
	if err != nil {
		return nil, err
	}
	row, err := parseRow[prRow](rows)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &domain.PRState{
		RepoID:          row.RepoID,
		Number:          row.Number,
		LastAnalyzedSHA: row.LastAnalyzedSHA,
		ReviewID:        row.ReviewID,
		CheckRunID:      row.CheckRunID,
	}, nil
}

// SetPRState upserts the tracked state for a PR. Nil reviewID/checkRunID
// leave the existing values unchanged.
func (s *Store) SetPRState(ctx context.Context, repoID int64, number int, headSHA string, reviewID, checkRunID *int64) error {
	rid := int64(0)
	if reviewID != nil {
		rid = *reviewID
	}
	crid := int64(0)
	if checkRunID != nil {
		crid = *checkRunID
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO pull_requests (repo_id, number, last_analyzed_sha, review_id, check_run_id, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (repo_id, number) DO UPDATE SET
			last_analyzed_sha = EXCLUDED.last_analyzed_sha,
			review_id = COALESCE(NULLIF(EXCLUDED.review_id, 0), pull_requests.review_id),
			check_run_id = COALESCE(NULLIF(EXCLUDED.check_run_id, 0), pull_requests.check_run_id),
			updated_at = now()`,
		repoID, number, headSHA, rid, crid)
	return err
}

// CreateRun inserts a new analysis run, returning ErrDuplicateRun if
// (repo_id, kind, input_sha) already exists.
func (s *Store) CreateRun(ctx context.Context, repoID int64, kind, inputSHA string, prNumber *int) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO analysis_runs (repo_id, kind, input_sha, pr_number, status, started_at)
		VALUES ($1, $2, $3, $4, 'running', now())
		RETURNING id`, repoID, kind, inputSHA, prNumber).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return 0, ErrDuplicateRun
		}
		return 0, err
	}
	return id, nil
}

// CompleteRun marks a run done and stores its result.
func (s *Store) CompleteRun(ctx context.Context, runID int64, result *domain.ReviewResult) error {
	var res any
	if result != nil {
		b, err := json.Marshal(result)
		if err != nil {
			return err
		}
		res = string(b)
	}
	_, err := s.pool.Exec(ctx, `UPDATE analysis_runs SET status = 'done', result = $2, finished_at = now() WHERE id = $1`, runID, res)
	return err
}

// FailRun marks a run failed with an error message.
func (s *Store) FailRun(ctx context.Context, runID int64, errMsg string) error {
	_, err := s.pool.Exec(ctx, `UPDATE analysis_runs SET status = 'failed', error = $2, finished_at = now() WHERE id = $1`, runID, errMsg)
	return err
}

const insertFindingSQL = `INSERT INTO findings (run_id, finding_id, file, line, severity, category, title, body, suggestion, confidence, actionable, dedupe_hash)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	ON CONFLICT (run_id, dedupe_hash) DO NOTHING`

// SaveFindings batch-inserts findings for a run, skipping duplicate hashes.
func (s *Store) SaveFindings(ctx context.Context, runID int64, findings []domain.Finding, hashFn func(domain.Finding) string) error {
	if len(findings) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, f := range findings {
		var sug any
		if f.Suggestion != nil {
			b, err := json.Marshal(f.Suggestion)
			if err != nil {
				return err
			}
			sug = string(b)
		}
		batch.Queue(insertFindingSQL, runID, f.ID, f.File, f.Line, string(f.Severity), string(f.Category), f.Title, f.Body, sug, f.Confidence, f.Actionable, hashFn(f))
	}
	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range findings {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return br.Close()
}

type findingRow struct {
	ID          int64   `db:"id"`
	RunID       int64   `db:"run_id"`
	FindingID   string  `db:"finding_id"`
	File        string  `db:"file"`
	Line        int     `db:"line"`
	Severity    string  `db:"severity"`
	Category    string  `db:"category"`
	Title       string  `db:"title"`
	Body        string  `db:"body"`
	Suggestion  []byte  `db:"suggestion"`
	Confidence  float64 `db:"confidence"`
	Actionable  bool    `db:"actionable"`
	DedupeHash  string  `db:"dedupe_hash"`
	CommentID   int64   `db:"comment_id"`
	IssueNumber int     `db:"issue_number"`
}

// FindingsForRun returns all findings recorded for a run.
func (s *Store) FindingsForRun(ctx context.Context, runID int64) ([]domain.FindingRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, run_id, finding_id, file, line, severity, category, title, body, suggestion, confidence, actionable, dedupe_hash, comment_id, issue_number FROM findings WHERE run_id = $1 ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	rs, err := parseRows[findingRow](rows)
	if err != nil {
		return nil, err
	}
	out := make([]domain.FindingRecord, 0, len(rs))
	for _, r := range rs {
		rec := domain.FindingRecord{
			ID:          r.ID,
			RunID:       r.RunID,
			FindingID:   r.FindingID,
			File:        r.File,
			Line:        r.Line,
			Severity:    r.Severity,
			Category:    r.Category,
			Title:       r.Title,
			Body:        r.Body,
			Confidence:  r.Confidence,
			Actionable:  r.Actionable,
			DedupeHash:  r.DedupeHash,
			CommentID:   r.CommentID,
			IssueNumber: r.IssueNumber,
		}
		if len(r.Suggestion) > 0 && string(r.Suggestion) != "null" {
			var sug domain.Suggestion
			if err := json.Unmarshal(r.Suggestion, &sug); err != nil {
				return nil, err
			}
			rec.Suggestion = &sug
		}
		out = append(out, rec)
	}
	return out, nil
}

// CreateIssue inserts a tracked issue, reopening it if it already exists.
func (s *Store) CreateIssue(ctx context.Context, repoID int64, number int, title, kind string, prNumber *int, findingIDs []string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO issues (repo_id, number, title, kind, pr_number, finding_ids)
		VALUES ($1, $2, $3, $4, COALESCE($5, 0), COALESCE($6::text[], '{}'))
		ON CONFLICT (repo_id, number) DO UPDATE SET
			status = 'open',
			closed_at = NULL`,
		repoID, number, title, kind, prNumber, findingIDs)
	return err
}

// CloseIssue marks a tracked issue closed.
func (s *Store) CloseIssue(ctx context.Context, repoID int64, number int) error {
	_, err := s.pool.Exec(ctx, `UPDATE issues SET status = 'closed', closed_at = now() WHERE repo_id = $1 AND number = $2`, repoID, number)
	return err
}

type issueRow struct {
	ID          int64    `db:"id"`
	RepoID      int64    `db:"repo_id"`
	Number      int      `db:"number"`
	Title       string   `db:"title"`
	Kind        string   `db:"kind"`
	PRNumber    int      `db:"pr_number"`
	FindingIDs  []string `db:"finding_ids"`
	Status      string   `db:"status"`
	FixPRNumber int      `db:"fix_pr_number"`
}

func issueFromRow(row issueRow) *domain.IssueRecord {
	return &domain.IssueRecord{
		ID:          row.ID,
		RepoID:      row.RepoID,
		Number:      row.Number,
		Title:       row.Title,
		Kind:        row.Kind,
		PRNumber:    row.PRNumber,
		FindingIDs:  row.FindingIDs,
		Status:      row.Status,
		FixPRNumber: row.FixPRNumber,
	}
}

const issueColumns = `id, repo_id, number, title, kind, COALESCE(pr_number, 0), finding_ids, status, COALESCE(fix_pr_number, 0)`

// OpenIssueForRepo returns the first open issue of a kind, or (nil, nil).
func (s *Store) OpenIssueForRepo(ctx context.Context, repoID int64, kind string) (*domain.IssueRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+issueColumns+` FROM issues WHERE repo_id = $1 AND kind = $2 AND status = 'open' ORDER BY id LIMIT 1`, repoID, kind)
	if err != nil {
		return nil, err
	}
	row, err := parseRow[issueRow](rows)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return issueFromRow(row), nil
}

// IssuesForPR returns all tracked issues for a PR.
func (s *Store) IssuesForPR(ctx context.Context, repoID int64, prNumber int) ([]domain.IssueRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+issueColumns+` FROM issues WHERE repo_id = $1 AND pr_number = $2 ORDER BY id`, repoID, prNumber)
	if err != nil {
		return nil, err
	}
	rs, err := parseRows[issueRow](rows)
	if err != nil {
		return nil, err
	}
	out := make([]domain.IssueRecord, 0, len(rs))
	for _, r := range rs {
		out = append(out, *issueFromRow(r))
	}
	return out, nil
}

// IssueByNumber returns a tracked issue by number, or (nil, nil).
func (s *Store) IssueByNumber(ctx context.Context, repoID int64, number int) (*domain.IssueRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+issueColumns+` FROM issues WHERE repo_id = $1 AND number = $2`, repoID, number)
	if err != nil {
		return nil, err
	}
	row, err := parseRow[issueRow](rows)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return issueFromRow(row), nil
}

// SetIssueFixPR records the fix PR number opened for an issue.
func (s *Store) SetIssueFixPR(ctx context.Context, repoID int64, number, prNumber int) error {
	_, err := s.pool.Exec(ctx, `UPDATE issues SET fix_pr_number = $3 WHERE repo_id = $1 AND number = $2`, repoID, number, prNumber)
	return err
}

// FindingsForIssue returns findings linked to an issue, ordered by ID.
func (s *Store) FindingsForIssue(ctx context.Context, repoID int64, issueNumber int) ([]domain.FindingRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT f.id, f.run_id, f.finding_id, f.file, f.line, f.severity, f.category, f.title, f.body, f.suggestion, f.confidence, f.actionable, f.dedupe_hash, f.comment_id, f.issue_number
		FROM findings f
		JOIN analysis_runs r ON f.run_id = r.id
		WHERE r.repo_id = $1 AND f.issue_number = $2
		ORDER BY f.id`, repoID, issueNumber)
	if err != nil {
		return nil, err
	}
	rs, err := parseRows[findingRow](rows)
	if err != nil {
		return nil, err
	}
	out := make([]domain.FindingRecord, 0, len(rs))
	for _, r := range rs {
		rec := domain.FindingRecord{
			ID:          r.ID,
			RunID:       r.RunID,
			FindingID:   r.FindingID,
			File:        r.File,
			Line:        r.Line,
			Severity:    r.Severity,
			Category:    r.Category,
			Title:       r.Title,
			Body:        r.Body,
			Confidence:  r.Confidence,
			Actionable:  r.Actionable,
			DedupeHash:  r.DedupeHash,
			CommentID:   r.CommentID,
			IssueNumber: r.IssueNumber,
		}
		if len(r.Suggestion) > 0 && string(r.Suggestion) != "null" {
			var sug domain.Suggestion
			if err := json.Unmarshal(r.Suggestion, &sug); err != nil {
				return nil, err
			}
			rec.Suggestion = &sug
		}
		out = append(out, rec)
	}
	return out, nil
}

// SetFindingComment records the comment ID posted for a finding.
func (s *Store) SetFindingComment(ctx context.Context, findingID int64, commentID int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE findings SET comment_id = $2 WHERE id = $1`, findingID, commentID)
	return err
}

// SetFindingIssue records the issue number associated with a finding.
func (s *Store) SetFindingIssue(ctx context.Context, findingID int64, issueNumber int) error {
	_, err := s.pool.Exec(ctx, `UPDATE findings SET issue_number = $2 WHERE id = $1`, findingID, issueNumber)
	return err
}

// UpsertLearning adjusts the weight of a learning key by delta.
func (s *Store) UpsertLearning(ctx context.Context, repoID int64, key, signal string, delta int) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO learnings (repo_id, key, signal, weight)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (repo_id, key, signal) DO UPDATE SET
			weight = learnings.weight + EXCLUDED.weight,
			updated_at = now()`,
		repoID, key, signal, delta)
	return err
}

// SuppressedKeys returns learning keys with a net weight of -2 or lower.
func (s *Store) SuppressedKeys(ctx context.Context, repoID int64) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT key FROM learnings WHERE repo_id = $1 AND weight <= -2`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys[k] = true
	}
	return keys, rows.Err()
}

// LearningKeysForComments maps bot comment IDs to finding dedupe hashes for
// a PR.
func (s *Store) LearningKeysForComments(ctx context.Context, repoID int64, prNumber int) (map[int64]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT comment_id, dedupe_hash FROM findings WHERE comment_id > 0 AND run_id IN (SELECT id FROM analysis_runs WHERE repo_id = $1 AND pr_number = $2)`, repoID, prNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[int64]string{}
	for rows.Next() {
		var commentID int64
		var hash string
		if err := rows.Scan(&commentID, &hash); err != nil {
			return nil, err
		}
		m[commentID] = hash
	}
	return m, rows.Err()
}

// Audit records an audit log entry.
func (s *Store) Audit(ctx context.Context, e domain.AuditEntry) error {
	var detail any
	if e.Detail != nil {
		b, err := json.Marshal(e.Detail)
		if err != nil {
			return err
		}
		detail = string(b)
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO audit_log (delivery_id, event, action, repo_id, kind, detail)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		e.DeliveryID, e.Event, e.Action, e.RepoID, e.Kind, detail)
	return err
}

func parseRow[T any](rows pgx.Rows) (T, error) {
	defer rows.Close()
	return pgx.CollectOneRow(rows, pgx.RowToStructByPos[T])
}

func parseRows[T any](rows pgx.Rows) ([]T, error) {
	return pgx.CollectRows(rows, pgx.RowToStructByPos[T])
}
