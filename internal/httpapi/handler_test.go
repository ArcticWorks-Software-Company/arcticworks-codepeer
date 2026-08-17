package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
)

type fakeQueue struct {
	mu    sync.Mutex
	jobs  []domain.JobKind
	items []any
}

func (q *fakeQueue) Enqueue(_ context.Context, kind domain.JobKind, payload any) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = append(q.jobs, kind)
	q.items = append(q.items, payload)
	return nil
}
func (q *fakeQueue) Dequeue(context.Context) (*domain.Job, bool, error)        { return nil, false, nil }
func (q *fakeQueue) Complete(context.Context, int64) error                     { return nil }
func (q *fakeQueue) Fail(context.Context, int64, string) error                 { return nil }
func (q *fakeQueue) ReapExpired(context.Context, time.Duration) (int64, error) { return 0, nil }

type fakeStore struct {
	mu         sync.Mutex
	deliveries []string
	installs   []domain.Installation
	repos      []domain.Repo
	cfg        map[int64]*domain.RepoConfig
	audits     []domain.AuditEntry
}

func newFakeStore() *fakeStore { return &fakeStore{cfg: map[int64]*domain.RepoConfig{}} }

func (s *fakeStore) Ping(context.Context) error { return nil }
func (s *fakeStore) RecordDelivery(_ context.Context, deliveryID, _ string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.deliveries {
		if d == deliveryID {
			return false, nil
		}
	}
	s.deliveries = append(s.deliveries, deliveryID)
	return true, nil
}
func (s *fakeStore) UpsertInstallation(_ context.Context, inst domain.Installation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.installs = append(s.installs, inst)
	return nil
}
func (s *fakeStore) UpsertRepo(_ context.Context, r domain.Repo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repos = append(s.repos, r)
	return nil
}
func (s *fakeStore) GetRepo(context.Context, int64) (*domain.Repo, error) { return nil, nil }
func (s *fakeStore) GetRepoByName(context.Context, string, string) (*domain.Repo, error) {
	return nil, nil
}
func (s *fakeStore) ListReposForInstallation(context.Context, int64) ([]domain.Repo, error) {
	return nil, nil
}
func (s *fakeStore) SetRepoConfig(_ context.Context, repoID int64, cfg *domain.RepoConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg[repoID] = cfg
	return nil
}
func (s *fakeStore) GetPRState(context.Context, int64, int) (*domain.PRState, error) { return nil, nil }
func (s *fakeStore) SetPRState(context.Context, int64, int, string, *int64, *int64) error {
	return nil
}
func (s *fakeStore) CreateRun(context.Context, int64, string, string, *int) (int64, error) {
	return 1, nil
}
func (s *fakeStore) CompleteRun(context.Context, int64, *domain.ReviewResult) error { return nil }
func (s *fakeStore) FailRun(context.Context, int64, string) error                   { return nil }
func (s *fakeStore) SaveFindings(context.Context, int64, []domain.Finding, func(domain.Finding) string) error {
	return nil
}
func (s *fakeStore) FindingsForRun(context.Context, int64) ([]domain.FindingRecord, error) {
	return nil, nil
}
func (s *fakeStore) CreateIssue(context.Context, int64, int, string, string, *int, []string) error {
	return nil
}
func (s *fakeStore) CloseIssue(context.Context, int64, int) error { return nil }
func (s *fakeStore) IssueByNumber(context.Context, int64, int) (*domain.IssueRecord, error) {
	return nil, nil
}
func (s *fakeStore) SetIssueFixPR(context.Context, int64, int, int) error { return nil }
func (s *fakeStore) FindingsForIssue(context.Context, int64, int) ([]domain.FindingRecord, error) {
	return nil, nil
}
func (s *fakeStore) OpenIssueForRepo(context.Context, int64, string) (*domain.IssueRecord, error) {
	return nil, nil
}
func (s *fakeStore) IssuesForPR(context.Context, int64, int) ([]domain.IssueRecord, error) {
	return nil, nil
}
func (s *fakeStore) SetFindingComment(context.Context, int64, int64) error { return nil }
func (s *fakeStore) SetFindingIssue(context.Context, int64, int) error     { return nil }
func (s *fakeStore) UpsertLearning(context.Context, int64, string, string, int) error {
	return nil
}
func (s *fakeStore) SuppressedKeys(context.Context, int64) (map[string]bool, error) { return nil, nil }
func (s *fakeStore) LearningKeysForComments(context.Context, int64, int) (map[int64]string, error) {
	return nil, nil
}
func (s *fakeStore) Audit(_ context.Context, e domain.AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audits = append(s.audits, e)
	return nil
}

type fakeGitHub struct{ login string }

func (f *fakeGitHub) InstallationToken(context.Context, int64) (string, error) { return "t", nil }
func (f *fakeGitHub) SelfLogin() string                                        { return f.login }
func (f *fakeGitHub) GetRawDiff(context.Context, string, string, int) (string, error) {
	return "", nil
}
func (f *fakeGitHub) ListPRFiles(context.Context, string, string, int) ([]domain.ChangedFile, error) {
	return nil, nil
}
func (f *fakeGitHub) GetFile(context.Context, string, string, string, string) (string, error) {
	return "", nil
}
func (f *fakeGitHub) GetPR(context.Context, string, string, int) (*domain.PRInfo, error) {
	return nil, nil
}
func (f *fakeGitHub) GetDefaultBranch(context.Context, string, string) (string, error) {
	return "main", nil
}
func (f *fakeGitHub) CreateCheckRun(context.Context, string, string, string, string, string, string, string, string, []domain.Annotation) (int64, error) {
	return 0, nil
}
func (f *fakeGitHub) UpdateCheckRun(context.Context, string, string, int64, string, string, string, []domain.Annotation) error {
	return nil
}
func (f *fakeGitHub) CreateReview(context.Context, string, string, int, string, string, []domain.InlineComment) (int64, error) {
	return 0, nil
}
func (f *fakeGitHub) CreateComment(context.Context, string, string, int, string, domain.InlineComment) (int64, error) {
	return 0, nil
}
func (f *fakeGitHub) CreateIssue(context.Context, string, string, string, string, []string) (int, error) {
	return 0, nil
}
func (f *fakeGitHub) EditIssue(context.Context, string, string, int, *string, *string) error {
	return nil
}
func (f *fakeGitHub) AddIssueComment(context.Context, string, string, int, string) error {
	return nil
}
func (f *fakeGitHub) GetReactions(context.Context, string, string, int64) ([]domain.Reaction, error) {
	return nil, nil
}
func (f *fakeGitHub) GetPushDiff(context.Context, string, string, string, string) ([]domain.ChangedFile, error) {
	return nil, nil
}
func (f *fakeGitHub) ListInstallationRepos(context.Context, int64) ([]domain.Repo, error) {
	return nil, nil
}
func (f *fakeGitHub) GetBranchSHA(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (f *fakeGitHub) GetCommitTreeSHA(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (f *fakeGitHub) GetFileWithSHA(context.Context, string, string, string, string) (string, string, error) {
	return "", "", nil
}
func (f *fakeGitHub) CreateBlob(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (f *fakeGitHub) CreateTree(context.Context, string, string, string, []domain.TreeEntry) (string, error) {
	return "", nil
}
func (f *fakeGitHub) CreateCommit(context.Context, string, string, string, string, []string) (string, error) {
	return "", nil
}
func (f *fakeGitHub) CreateBranch(context.Context, string, string, string, string) error { return nil }
func (f *fakeGitHub) CreatePR(context.Context, string, string, string, string, string, string) (int, error) {
	return 0, nil
}

func sign(t *testing.T, secret, body string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func doWebhook(t *testing.T, secret, signSecret, event, delivery, body string, gh *fakeGitHub, st *fakeStore, q *fakeQueue) *httptest.ResponseRecorder {
	t.Helper()
	h := New([]byte(secret), gh, st, q, nil)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-GitHub-Delivery", delivery)
	if signSecret != "" {
		req.Header.Set("X-Hub-Signature-256", sign(t, signSecret, body))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func prOpenedBody(sender string) string {
	return `{
  "action": "opened",
  "number": 42,
  "pull_request": {
    "number": 42,
    "draft": false,
    "merged": false,
    "head": {"sha": "abc123", "ref": "feature"},
    "base": {"ref": "main"}
  },
  "repository": {"id": 7, "owner": {"login": "acme"}, "name": "core"},
  "sender": {"login": "` + sender + `"},
  "installation": {"id": 1}
}`
}

func TestWebhookPRRouted(t *testing.T) {
	gh := &fakeGitHub{login: "codepeer[bot]"}
	st := newFakeStore()
	q := &fakeQueue{}
	rec := doWebhook(t, "secret", "secret", "pull_request", "d1", prOpenedBody("dev"), gh, st, q)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if len(q.jobs) != 1 || q.jobs[0] != domain.JobAnalyzePR {
		t.Fatalf("jobs = %v, want [analyze_pr]", q.jobs)
	}
	payload, ok := q.items[0].(domain.AnalyzePRPayload)
	if !ok {
		t.Fatalf("payload type = %T", q.items[0])
	}
	if payload.PRNumber != 42 || payload.HeadSHA != "abc123" || payload.RepoID != 7 || payload.InstallationID != 1 {
		t.Errorf("payload = %+v", payload)
	}
}

func TestWebhookBadSignature(t *testing.T) {
	gh := &fakeGitHub{login: "codepeer[bot]"}
	st := newFakeStore()
	q := &fakeQueue{}
	rec := doWebhook(t, "secret", "wrong-secret", "pull_request", "d1", prOpenedBody("dev"), gh, st, q)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if len(q.jobs) != 0 {
		t.Fatalf("jobs enqueued with bad signature: %v", q.jobs)
	}
}

func TestWebhookDuplicateDelivery(t *testing.T) {
	gh := &fakeGitHub{login: "codepeer[bot]"}
	st := newFakeStore()
	q := &fakeQueue{}
	if rec := doWebhook(t, "secret", "secret", "pull_request", "d1", prOpenedBody("dev"), gh, st, q); rec.Code != http.StatusAccepted {
		t.Fatalf("first delivery status = %d", rec.Code)
	}
	if rec := doWebhook(t, "secret", "secret", "pull_request", "d1", prOpenedBody("dev"), gh, st, q); rec.Code != http.StatusOK {
		t.Fatalf("redelivery status = %d, want 200", rec.Code)
	}
	if len(q.jobs) != 1 {
		t.Fatalf("jobs = %d, want 1 (redelivery must not re-enqueue)", len(q.jobs))
	}
}

func TestWebhookSelfEventIgnored(t *testing.T) {
	gh := &fakeGitHub{login: "codepeer[bot]"}
	st := newFakeStore()
	q := &fakeQueue{}
	rec := doWebhook(t, "secret", "secret", "pull_request", "d1", prOpenedBody("codepeer[bot]"), gh, st, q)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(q.jobs) != 0 {
		t.Fatalf("self events must not enqueue: %v", q.jobs)
	}
}

func TestWebhookPushRouted(t *testing.T) {
	gh := &fakeGitHub{login: "codepeer[bot]"}
	st := newFakeStore()
	q := &fakeQueue{}
	body := `{
	  "ref": "refs/heads/main",
	  "before": "aaa111",
	  "after": "bbb222",
	  "created": false,
	  "deleted": false,
	  "repository": {"id": 7, "owner": {"login": "acme"}, "name": "core"},
	  "sender": {"login": "dev"},
	  "installation": {"id": 1}
	}`
	rec := doWebhook(t, "secret", "secret", "push", "d1", body, gh, st, q)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(q.jobs) != 1 || q.jobs[0] != domain.JobAnalyzePush {
		t.Fatalf("jobs = %v", q.jobs)
	}
}

func TestWebhookPRMergedClosesIssues(t *testing.T) {
	gh := &fakeGitHub{login: "codepeer[bot]"}
	st := newFakeStore()
	q := &fakeQueue{}
	body := `{
	  "action": "closed",
	  "number": 42,
	  "pull_request": {"number": 42, "draft": false, "merged": true,
	    "head": {"sha": "abc123", "ref": "feature"}, "base": {"ref": "main"}},
	  "repository": {"id": 7, "owner": {"login": "acme"}, "name": "core"},
	  "sender": {"login": "dev"},
	  "installation": {"id": 1}
	}`
	rec := doWebhook(t, "secret", "secret", "pull_request", "d1", body, gh, st, q)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(q.jobs) != 1 || q.jobs[0] != domain.JobIssueClose {
		t.Fatalf("jobs = %v, want [close_pr_issues]", q.jobs)
	}
}

func TestWebhookInstallationSynced(t *testing.T) {
	gh := &fakeGitHub{login: "codepeer[bot]"}
	st := newFakeStore()
	q := &fakeQueue{}
	body := `{
	  "action": "created",
	  "installation": {"id": 9, "account": {"id": 5, "login": "acme", "type": "Organization"}}
	}`
	rec := doWebhook(t, "secret", "secret", "installation", "d1", body, gh, st, q)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.installs) != 1 || st.installs[0].ID != 9 || st.installs[0].AccountLogin != "acme" {
		t.Errorf("installations = %+v", st.installs)
	}
}

func TestWebhookPing(t *testing.T) {
	gh := &fakeGitHub{login: "codepeer[bot]"}
	st := newFakeStore()
	q := &fakeQueue{}
	rec := doWebhook(t, "secret", "secret", "ping", "d1", `{"zen":"x"}`, gh, st, q)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(q.jobs) != 0 {
		t.Fatalf("ping must not enqueue: %v", q.jobs)
	}
}

func TestHealthz(t *testing.T) {
	h := New([]byte("s"), &fakeGitHub{}, newFakeStore(), &fakeQueue{}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz = %d", rec.Code)
	}
}

func TestAuditRecorded(t *testing.T) {
	gh := &fakeGitHub{login: "codepeer[bot]"}
	st := newFakeStore()
	q := &fakeQueue{}
	doWebhook(t, "secret", "secret", "pull_request", "d1", prOpenedBody("dev"), gh, st, q)
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.audits) != 1 || st.audits[0].Event != "pull_request" || st.audits[0].RepoID != 7 {
		t.Errorf("audits = %+v", st.audits)
	}
}

var _ = json.Marshal

func TestWebhookIssueCommentApprove(t *testing.T) {
	gh := &fakeGitHub{login: "codepeer[bot]"}
	st := newFakeStore()
	q := &fakeQueue{}
	body := `{
	  "action": "created",
	  "comment": {"id": 5, "body": "approve", "user": {"login": "dev"}},
	  "issue": {"number": 12},
	  "repository": {"id": 7, "owner": {"login": "acme"}, "name": "core"},
	  "sender": {"login": "dev"},
	  "installation": {"id": 1}
	}`
	rec := doWebhook(t, "secret", "secret", "issue_comment", "d1", body, gh, st, q)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(q.jobs) != 1 || q.jobs[0] != domain.JobIssueCmd {
		t.Fatalf("jobs = %v, want [issue_command]", q.jobs)
	}
	payload, ok := q.items[0].(domain.IssueCommandPayload)
	if !ok || payload.Command != "approve" || payload.IssueNumber != 12 {
		t.Errorf("payload = %+v", q.items[0])
	}
}

func TestWebhookIssueCommentDeny(t *testing.T) {
	gh := &fakeGitHub{login: "codepeer[bot]"}
	st := newFakeStore()
	q := &fakeQueue{}
	body := `{
	  "action": "created",
	  "comment": {"id": 5, "body": " DENY ", "user": {"login": "dev"}},
	  "issue": {"number": 12},
	  "repository": {"id": 7, "owner": {"login": "acme"}, "name": "core"},
	  "sender": {"login": "dev"},
	  "installation": {"id": 1}
	}`
	rec := doWebhook(t, "secret", "secret", "issue_comment", "d1", body, gh, st, q)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(q.jobs) != 1 || q.jobs[0] != domain.JobIssueCmd {
		t.Fatalf("jobs = %v", q.jobs)
	}
}

func TestWebhookIssueCommentOnPRConversationIgnored(t *testing.T) {
	gh := &fakeGitHub{login: "codepeer[bot]"}
	st := newFakeStore()
	q := &fakeQueue{}
	body := `{
	  "action": "created",
	  "comment": {"id": 5, "body": "approve", "user": {"login": "dev"}},
	  "issue": {"number": 12, "pull_request": {"url": "https://api.github.com/repos/acme/core/pulls/12"}},
	  "repository": {"id": 7, "owner": {"login": "acme"}, "name": "core"},
	  "sender": {"login": "dev"},
	  "installation": {"id": 1}
	}`
	rec := doWebhook(t, "secret", "secret", "issue_comment", "d1", body, gh, st, q)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(q.jobs) != 0 {
		t.Fatalf("PR-conversation comments must not enqueue: %v", q.jobs)
	}
}

func TestWebhookIssueCommentNoiseIgnored(t *testing.T) {
	gh := &fakeGitHub{login: "codepeer[bot]"}
	st := newFakeStore()
	q := &fakeQueue{}
	body := `{
	  "action": "created",
	  "comment": {"id": 5, "body": "looks good to me", "user": {"login": "dev"}},
	  "issue": {"number": 12},
	  "repository": {"id": 7, "owner": {"login": "acme"}, "name": "core"},
	  "sender": {"login": "dev"},
	  "installation": {"id": 1}
	}`
	rec := doWebhook(t, "secret", "secret", "issue_comment", "d1", body, gh, st, q)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(q.jobs) != 0 {
		t.Fatalf("non-command comments must not enqueue: %v", q.jobs)
	}
}
