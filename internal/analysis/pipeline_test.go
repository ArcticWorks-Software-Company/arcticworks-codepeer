package analysis

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/store"
)

const sampleDiff = `diff --git a/handlers.go b/handlers.go
index 1111111..2222222 100644
--- a/handlers.go
+++ b/handlers.go
@@ -1,3 +1,4 @@
 package main
 
+import "fmt"
 import "net/http"
 
 func handler(w http.ResponseWriter, r *http.Request) {
@@ -10,4 +11,5 @@ func handler(w http.ResponseWriter, r *http.Request) {
 	if r.Method != "GET" {
 		w.WriteHeader(http.StatusMethodNotAllowed)
+		fmt.Println("debug")
 		return
 	}
diff --git a/newfile.go b/newfile.go
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/newfile.go
@@ -0,0 +1,3 @@
+package main
+
+func helper() string {
+	return "ok"
+}
`

func sampleFiles() []domain.ChangedFile {
	return []domain.ChangedFile{
		{Path: "handlers.go", Status: "modified", Additions: 2, Deletions: 0, Patch: `@@ -1,3 +1,4 @@
 package main
 
+import "fmt"
 import "net/http"
 
 func handler(w http.ResponseWriter, r *http.Request) {
@@ -10,4 +11,5 @@ func handler(w http.ResponseWriter, r *http.Request) {
 	if r.Method != "GET" {
 		w.WriteHeader(http.StatusMethodNotAllowed)
+		fmt.Println("debug")
 		return
 	}
`},
		{Path: "newfile.go", Status: "added", Additions: 3, Deletions: 0, Patch: `@@ -0,0 +1,3 @@
+package main
+
+func helper() string {
+	return "ok"
+}
`},
	}
}

type fakeReviewer struct {
	mu       sync.Mutex
	calls    int
	findings []domain.Finding
	err      error
}

func (f *fakeReviewer) Review(ctx context.Context, req domain.ReviewRequest) (domain.ReviewResult, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return domain.ReviewResult{}, f.err
	}
	res := domain.ReviewResult{Summary: "looks reasonable", Status: domain.StatusNoFindings}
	res.Findings = append(res.Findings, f.findings...)
	if len(res.Findings) > 0 {
		res.Status = domain.StatusChangesRequested
	}
	return res, nil
}

func (f *fakeReviewer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeGitHub struct {
	pr          domain.PRInfo
	rawDiff     string
	files       []domain.ChangedFile
	fileContent map[string]string
}

func (f *fakeGitHub) InstallationToken(context.Context, int64) (string, error) { return "tok", nil }
func (f *fakeGitHub) SelfLogin() string                                        { return "codepeer-bot[bot]" }
func (f *fakeGitHub) GetRawDiff(context.Context, string, string, int) (string, error) {
	return f.rawDiff, nil
}
func (f *fakeGitHub) ListPRFiles(context.Context, string, string, int) ([]domain.ChangedFile, error) {
	return f.files, nil
}
func (f *fakeGitHub) GetFile(_ context.Context, _, _, path, _ string) (string, error) {
	return f.fileContent[path], nil
}
func (f *fakeGitHub) GetPR(context.Context, string, string, int) (*domain.PRInfo, error) {
	return &f.pr, nil
}
func (f *fakeGitHub) GetDefaultBranch(context.Context, string, string) (string, error) {
	return "main", nil
}
func (f *fakeGitHub) CreateCheckRun(context.Context, string, string, string, string, string, string, string, string, []domain.Annotation) (int64, error) {
	return 1, nil
}
func (f *fakeGitHub) UpdateCheckRun(context.Context, string, string, int64, string, string, string, []domain.Annotation) error {
	return nil
}
func (f *fakeGitHub) CreateReview(context.Context, string, string, int, string, string, []domain.InlineComment) (int64, error) {
	return 1, nil
}
func (f *fakeGitHub) CreateComment(context.Context, string, string, int, string, domain.InlineComment) (int64, error) {
	return 1, nil
}
func (f *fakeGitHub) CreateIssue(context.Context, string, string, string, string, []string) (int, error) {
	return 1, nil
}
func (f *fakeGitHub) EditIssue(context.Context, string, string, int, *string, *string) error {
	return nil
}
func (f *fakeGitHub) AddIssueComment(context.Context, string, string, int, string) error { return nil }
func (f *fakeGitHub) GetReactions(context.Context, string, string, int64) ([]domain.Reaction, error) {
	return nil, nil
}
func (f *fakeGitHub) GetPushDiff(_ context.Context, _, _, _, _ string) ([]domain.ChangedFile, error) {
	return f.files, nil
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

type fakeStore struct {
	mu         sync.Mutex
	repo       *domain.Repo
	runs       map[string]int64
	nextRunID  int64
	findings   map[int64][]domain.FindingRecord
	suppressed map[string]bool
	completed  []int64
	failed     []int64
}

func newFakeStore(repo *domain.Repo) *fakeStore {
	return &fakeStore{repo: repo, runs: map[string]int64{}, findings: map[int64][]domain.FindingRecord{}, suppressed: map[string]bool{}}
}

func (s *fakeStore) Ping(context.Context) error { return nil }
func (s *fakeStore) RecordDelivery(context.Context, string, string) (bool, error) {
	return true, nil
}
func (s *fakeStore) UpsertInstallation(context.Context, domain.Installation) error { return nil }
func (s *fakeStore) UpsertRepo(context.Context, domain.Repo) error                 { return nil }
func (s *fakeStore) GetRepo(context.Context, int64) (*domain.Repo, error)          { return s.repo, nil }
func (s *fakeStore) GetRepoByName(context.Context, string, string) (*domain.Repo, error) {
	return s.repo, nil
}
func (s *fakeStore) ListReposForInstallation(context.Context, int64) ([]domain.Repo, error) {
	return nil, nil
}
func (s *fakeStore) SetRepoConfig(context.Context, int64, *domain.RepoConfig) error { return nil }
func (s *fakeStore) GetPRState(context.Context, int64, int) (*domain.PRState, error) {
	return nil, nil
}
func (s *fakeStore) SetPRState(context.Context, int64, int, string, *int64, *int64) error { return nil }
func (s *fakeStore) CreateRun(_ context.Context, repoID int64, kind, sha string, _ *int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := kind + "|" + sha
	if _, ok := s.runs[key]; ok {
		return 0, store.ErrDuplicateRun
	}
	s.nextRunID++
	s.runs[key] = s.nextRunID
	return s.nextRunID, nil
}
func (s *fakeStore) CompleteRun(_ context.Context, runID int64, _ *domain.ReviewResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completed = append(s.completed, runID)
	return nil
}
func (s *fakeStore) FailRun(_ context.Context, runID int64, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed = append(s.failed, runID)
	return nil
}
func (s *fakeStore) SaveFindings(_ context.Context, runID int64, findings []domain.Finding, hashFn func(domain.Finding) string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := make([]domain.FindingRecord, 0, len(findings))
	for i, f := range findings {
		records = append(records, domain.FindingRecord{
			ID: int64(i + 1), RunID: runID, FindingID: f.ID, File: f.File, Line: f.Line,
			Severity: string(f.Severity), Category: string(f.Category), Title: f.Title, Body: f.Body,
			Suggestion: f.Suggestion, Confidence: f.Confidence, Actionable: f.Actionable,
			DedupeHash: hashFn(f),
		})
	}
	s.findings[runID] = records
	return nil
}
func (s *fakeStore) FindingsForRun(_ context.Context, runID int64) ([]domain.FindingRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.findings[runID], nil
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
func (s *fakeStore) SuppressedKeys(context.Context, int64) (map[string]bool, error) {
	return s.suppressed, nil
}
func (s *fakeStore) LearningKeysForComments(context.Context, int64, int) (map[int64]string, error) {
	return nil, nil
}
func (s *fakeStore) Audit(context.Context, domain.AuditEntry) error { return nil }

func testRepo() *domain.Repo {
	return &domain.Repo{ID: 7, InstallationID: 1, Owner: "acme", Name: "core", DefaultBranch: "main", Enabled: true}
}

func cannedFindings() []domain.Finding {
	return []domain.Finding{
		{ID: "A", File: "handlers.go", Line: 12, Severity: domain.SeverityHigh, Category: domain.CategoryMaintainability,
			Title: "Avoid debug prints", Body: "Leftover debug print.", Confidence: 0.9, Actionable: true,
			Suggestion: &domain.Suggestion{Old: `fmt.Println("debug")`, New: ""}},
		{ID: "A2", File: "handlers.go", Line: 12, Severity: domain.SeverityMedium, Category: domain.CategoryStyle,
			Title: "Avoid debug prints", Body: "dup of A", Confidence: 0.5, Actionable: true},
		{ID: "B", File: "handlers.go", Line: 99, Severity: domain.SeverityHigh, Category: domain.CategoryBug,
			Title: "Out of range access", Body: "This line is not in the diff.", Confidence: 0.8, Actionable: true},
		{ID: "C", File: "newfile.go", Line: 2, Severity: domain.SeverityLow, Category: domain.CategoryTest,
			Title: "Missing test", Body: "helper has no test.", Confidence: 0.7, Actionable: true,
			Suggestion: &domain.Suggestion{Old: "nonexistent snippet", New: "fixed"}},
		{ID: "D", File: "newfile.go", Line: 3, Severity: domain.SeverityMedium, Category: domain.CategoryBug,
			Title: "", Body: "", Confidence: 0.4, Actionable: true},
		{ID: "E", File: "handlers.go", Line: 12, Severity: domain.SeverityLow, Category: domain.CategoryStyle,
			Title: "Suppressed by learnings", Body: "downvoted before.", Confidence: 0.6, Actionable: true},
	}
}

func newTestPipeline(reviewer *fakeReviewer, gh *fakeGitHub, st *fakeStore) *Pipeline {
	return &Pipeline{Reviewer: reviewer, GitHub: gh, Store: st, Strictness: "balanced", MaxFindings: 10, PerFileCap: 3}
}

func TestAnalyzePRHappyPath(t *testing.T) {
	rev := &fakeReviewer{findings: cannedFindings()}
	gh := &fakeGitHub{
		pr:      domain.PRInfo{Number: 5, Title: "feat: add helper", Body: "desc", State: "open", Draft: false, HeadSHA: "abc123", BaseRef: "main", UserLogin: "dev"},
		rawDiff: sampleDiff,
		files:   sampleFiles(),
		fileContent: map[string]string{
			"handlers.go": `package main

import "fmt"
import "net/http"

func handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		fmt.Println("debug")
		return
	}
}
`,
			"newfile.go": "package main\n\nfunc helper() string {\n\treturn \"ok\"\n}\n",
		},
	}
	st := newFakeStore(testRepo())
	st.suppressed[DedupeHash(cannedFindings()[5])] = true

	out, err := newTestPipeline(rev, gh, st).AnalyzePR(context.Background(), domain.AnalyzePRPayload{
		InstallationID: 1, RepoID: 7, RepoOwner: "acme", RepoName: "core", PRNumber: 5, HeadSHA: "abc123", Action: "opened", SenderLogin: "dev",
	})
	if err != nil {
		t.Fatalf("AnalyzePR: %v", err)
	}
	if out.Skipped != "" {
		t.Fatalf("unexpected skip: %q", out.Skipped)
	}
	if rev.callCount() != 1 {
		t.Fatalf("reviewer calls = %d, want 1", rev.callCount())
	}

	var titles []string
	for _, f := range out.Result.Findings {
		titles = append(titles, f.Title)
	}
	joined := strings.Join(titles, "|")
	if strings.Contains(joined, "Out of range access") {
		t.Errorf("out-of-hunk finding should have been dropped: %v", titles)
	}
	if strings.Contains(joined, "Suppressed by learnings") {
		t.Errorf("suppressed finding should have been dropped: %v", titles)
	}
	if !strings.Contains(joined, "Avoid debug prints") || !strings.Contains(joined, "Missing test") {
		t.Errorf("expected kept findings, got: %v", titles)
	}
	if got := len(out.Result.Findings); got != 2 {
		t.Errorf("findings after validation = %d, want 2: %v", got, titles)
	}
	if out.Result.Status != domain.StatusChangesRequested {
		t.Errorf("status = %s, want changes_requested", out.Result.Status)
	}

	for _, f := range out.Result.Findings {
		switch f.Title {
		case "Avoid debug prints":
			if f.Severity != domain.SeverityHigh {
				t.Errorf("dedupe should keep high severity, got %s", f.Severity)
			}
			if f.Suggestion == nil || f.Suggestion.Old != `fmt.Println("debug")` {
				t.Errorf("suggestion should be kept when old text is in the patch")
			}
		case "Missing test":
			if f.Suggestion != nil {
				t.Errorf("suggestion with absent old text should be nil-ed")
			}
		}
	}

	if len(st.completed) != 1 || st.completed[0] != out.RunID {
		t.Errorf("CompleteRun not recorded for run %d: %v", out.RunID, st.completed)
	}
	if len(out.Findings) != len(out.Result.Findings) {
		t.Errorf("persisted findings (%d) != result findings (%d)", len(out.Findings), len(out.Result.Findings))
	}
}

func TestAnalyzePRDuplicateRun(t *testing.T) {
	rev := &fakeReviewer{findings: nil}
	gh := &fakeGitHub{
		pr:      domain.PRInfo{Number: 5, Title: "feat", State: "open", HeadSHA: "abc123", BaseRef: "main"},
		rawDiff: sampleDiff,
		files:   sampleFiles(),
	}
	st := newFakeStore(testRepo())
	p := newTestPipeline(rev, gh, st)
	payload := domain.AnalyzePRPayload{RepoID: 7, RepoOwner: "acme", RepoName: "core", PRNumber: 5, HeadSHA: "abc123"}

	if _, err := p.AnalyzePR(context.Background(), payload); err != nil {
		t.Fatalf("first run: %v", err)
	}
	out, err := p.AnalyzePR(context.Background(), payload)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if out.Skipped != "already analyzed" {
		t.Errorf("Skipped = %q, want already analyzed", out.Skipped)
	}
	if rev.callCount() != 1 {
		t.Errorf("reviewer calls = %d, want 1", rev.callCount())
	}
}

func TestAnalyzePRSkips(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(gh *fakeGitHub, st *fakeStore)
		wantSkip string
	}{
		{"draft", func(gh *fakeGitHub, _ *fakeStore) { gh.pr.Draft = true }, "draft PR"},
		{"user ignored", func(_ *fakeGitHub, _ *fakeStore) {}, "user ignored"},
		{"title keyword", func(gh *fakeGitHub, _ *fakeStore) { gh.pr.Title = "WIP feat" }, "title keyword"},
		{"repo disabled", func(_ *fakeGitHub, st *fakeStore) { st.repo.Enabled = false }, "repo disabled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rev := &fakeReviewer{}
			gh := &fakeGitHub{
				pr:      domain.PRInfo{Number: 5, Title: "feat", State: "open", HeadSHA: "abc123", BaseRef: "main"},
				rawDiff: sampleDiff,
				files:   sampleFiles(),
			}
			st := newFakeStore(testRepo())
			if tc.name == "user ignored" {
				gh.pr.UserLogin = ""
				payloadUser := "dependabot[bot]"
				_ = payloadUser
				st.repo.Config = &domain.RepoConfig{Enabled: true, Mode: "pr", Strictness: "balanced", IgnoreUsernames: []string{"botuser"}}
			}
			tc.mutate(gh, st)
			payload := domain.AnalyzePRPayload{RepoID: 7, RepoOwner: "acme", RepoName: "core", PRNumber: 5, HeadSHA: "abc123", SenderLogin: "botuser"}
			out, err := newTestPipeline(rev, gh, st).AnalyzePR(context.Background(), payload)
			if err != nil {
				t.Fatalf("AnalyzePR: %v", err)
			}
			if out.Skipped != tc.wantSkip {
				t.Errorf("Skipped = %q, want %q", out.Skipped, tc.wantSkip)
			}
			if rev.callCount() != 0 {
				t.Errorf("reviewer should not be called on skip, calls=%d", rev.callCount())
			}
		})
	}
}

func TestAnalyzePushNotDefaultBranch(t *testing.T) {
	rev := &fakeReviewer{}
	gh := &fakeGitHub{files: sampleFiles()}
	st := newFakeStore(testRepo())
	cfg := domain.DefaultRepoConfig()
	cfg.Mode = "both"
	st.repo.Config = &cfg
	out, err := newTestPipeline(rev, gh, st).AnalyzePush(context.Background(), domain.AnalyzePushPayload{
		RepoID: 7, RepoOwner: "acme", RepoName: "core", Before: "old", After: "new", Ref: "refs/heads/feature",
	})
	if err != nil {
		t.Fatalf("AnalyzePush: %v", err)
	}
	if out.Skipped != "not default branch" {
		t.Errorf("Skipped = %q, want not default branch", out.Skipped)
	}
}

func TestAnalyzePushHappyPath(t *testing.T) {
	rev := &fakeReviewer{findings: []domain.Finding{
		{ID: "P1", File: "newfile.go", Line: 2, Severity: domain.SeverityMedium, Category: domain.CategoryBug,
			Title: "helper ignores error", Body: "returned value unused.", Confidence: 0.8, Actionable: true},
	}}
	gh := &fakeGitHub{files: sampleFiles()}
	st := newFakeStore(testRepo())
	cfg := domain.DefaultRepoConfig()
	cfg.Mode = "both"
	st.repo.Config = &cfg

	out, err := newTestPipeline(rev, gh, st).AnalyzePush(context.Background(), domain.AnalyzePushPayload{
		RepoID: 7, RepoOwner: "acme", RepoName: "core", Before: "old", After: "new", Ref: "refs/heads/main",
	})
	if err != nil {
		t.Fatalf("AnalyzePush: %v", err)
	}
	if out.Skipped != "" {
		t.Fatalf("unexpected skip: %q", out.Skipped)
	}
	if len(out.Result.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(out.Result.Findings))
	}
}

func TestParseHunksAndLineChecks(t *testing.T) {
	hunks, err := ParseHunks(sampleDiff)
	if err != nil {
		t.Fatalf("ParseHunks: %v", err)
	}
	a := hunks["handlers.go"]
	if len(a) != 2 {
		t.Fatalf("handlers.go hunks = %d, want 2", len(a))
	}
	if a[0].NewStart != 1 || a[0].NewCount != 4 {
		t.Errorf("hunk0 = %+v, want new 1..4", a[0])
	}
	if a[1].NewStart != 11 || a[1].NewCount != 5 {
		t.Errorf("hunk1 = %+v, want new 11..15", a[1])
	}
	b := hunks["newfile.go"]
	if len(b) != 1 || b[0].NewStart != 1 || b[0].NewCount != 3 {
		t.Fatalf("newfile.go hunks = %+v, want new 1..3", b)
	}

	for line, want := range map[int]bool{1: true, 4: true, 8: false, 11: true, 12: true, 15: true, 16: false, 99: false} {
		if got := NewLineInDiff(a, line); got != want {
			t.Errorf("NewLineInDiff(handlers.go, %d) = %v, want %v", line, got, want)
		}
	}
}

func TestSplitDiffByFile(t *testing.T) {
	m, err := SplitDiffByFile(sampleDiff)
	if err != nil {
		t.Fatalf("SplitDiffByFile: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("files = %d, want 2", len(m))
	}
	if !strings.Contains(m["handlers.go"], `fmt.Println("debug")`) {
		t.Error("handlers.go section missing content")
	}
	if !strings.Contains(m["newfile.go"], "func helper()") {
		t.Error("newfile.go section missing content")
	}
}

func TestChunkFiles(t *testing.T) {
	big := domain.ChangedFile{Path: "big.go", Status: "modified", Additions: 2000, Deletions: 0, Patch: "big"}
	small := domain.ChangedFile{Path: "a.go", Status: "modified", Additions: 10, Deletions: 0, Patch: "a"}
	small2 := domain.ChangedFile{Path: "b.go", Status: "modified", Additions: 5, Deletions: 0, Patch: "b"}
	diffByFile := map[string]string{"big.go": "big", "a.go": "a", "b.go": "b"}

	chunks := ChunkFiles([]domain.ChangedFile{small, big, small2}, diffByFile)
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2 (big file isolated)", len(chunks))
	}
	// Big file first by additions desc, then the two small files together.
	if len(chunks[0].Files) != 1 || chunks[0].Files[0].Path != "big.go" {
		t.Errorf("chunk0 = %+v, want [big.go]", chunks[0].Files)
	}
	if len(chunks[1].Files) != 2 {
		t.Errorf("chunk1 files = %d, want 2", len(chunks[1].Files))
	}
	if !strings.Contains(chunks[1].DiffText, "--- FILE: a.go ---") {
		t.Errorf("chunk1 diff text missing file header: %q", chunks[1].DiffText)
	}
}

func TestDedupeHashStable(t *testing.T) {
	f1 := domain.Finding{File: "a.go", Line: 10, Title: "  Fix the NPE!!!  "}
	f2 := domain.Finding{File: "a.go", Line: 10, Title: "fix the npe"}
	if DedupeHash(f1) != DedupeHash(f2) {
		t.Error("dedupe hash should be case/punctuation-insensitive")
	}
	if DedupeHash(f1) != DedupeHash(f1) {
		t.Error("dedupe hash should be deterministic")
	}
}

func TestExtractAddedLines(t *testing.T) {
	lines := ExtractAddedLines(sampleFiles()[0].Patch)
	if len(lines) != 2 {
		t.Fatalf("added lines = %d (%v), want 2", len(lines), lines)
	}
	if lines[0] != `import "fmt"` {
		t.Errorf("line0 = %q", lines[0])
	}
}

func TestPipelineChunkErrorPropagates(t *testing.T) {
	rev := &fakeReviewer{err: errors.New("llm down")}
	gh := &fakeGitHub{
		pr:      domain.PRInfo{Number: 5, Title: "feat", State: "open", HeadSHA: "abc123", BaseRef: "main"},
		rawDiff: sampleDiff,
		files:   sampleFiles(),
	}
	st := newFakeStore(testRepo())
	_, err := newTestPipeline(rev, gh, st).AnalyzePR(context.Background(), domain.AnalyzePRPayload{
		RepoID: 7, RepoOwner: "acme", RepoName: "core", PRNumber: 5, HeadSHA: "abc123",
	})
	if err == nil {
		t.Fatal("expected error when all chunks fail")
	}
	if len(st.failed) != 1 {
		t.Errorf("FailRun calls = %d, want 1", len(st.failed))
	}
}
