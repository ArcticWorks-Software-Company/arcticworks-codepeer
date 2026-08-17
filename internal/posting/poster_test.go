package posting

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
)

type checkRunCall struct {
	owner       string
	repo        string
	name        string
	headSHA     string
	status      string
	conclusion  string
	title       string
	summary     string
	annotations []domain.Annotation
}

type reviewCall struct {
	owner    string
	repo     string
	prNumber int
	headSHA  string
	body     string
}

type commentCall struct {
	owner    string
	repo     string
	prNumber int
	headSHA  string
	comment  domain.InlineComment
}

type issueCall struct {
	owner  string
	repo   string
	title  string
	body   string
	labels []string
	num    int
}

type issueCommentCall struct {
	owner  string
	repo   string
	number int
	body   string
}

type editIssueCall struct {
	owner  string
	repo   string
	number int
	state  string
}

type fakeGitHub struct {
	mu            sync.Mutex
	checkRuns     []checkRunCall
	reviews       []reviewCall
	comments      []commentCall
	issues        []issueCall
	issueComments []issueCommentCall
	editIssues    []editIssueCall
	reactionCalls []int64
	reactions     map[int64][]domain.Reaction
	fileContents  map[string]string
	blobs         []blobCall
	trees         []treeCall
	commits       []commitCall
	branches      []branchCall
	prs           []prCall
	nextCheckID   int64
	nextReviewID  int64
	nextCommentID int64
	nextIssueNum  int
}

func (f *fakeGitHub) InstallationToken(context.Context, int64) (string, error) {
	return "token", nil
}

func (f *fakeGitHub) SelfLogin() string { return "codepeer[bot]" }

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

func (f *fakeGitHub) CreateCheckRun(_ context.Context, owner, repo, name, headSHA, status, conclusion, title, summary string, annotations []domain.Annotation) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextCheckID++
	f.checkRuns = append(f.checkRuns, checkRunCall{owner, repo, name, headSHA, status, conclusion, title, summary, annotations})
	return f.nextCheckID, nil
}

func (f *fakeGitHub) UpdateCheckRun(context.Context, string, string, int64, string, string, string, []domain.Annotation) error {
	return nil
}

func (f *fakeGitHub) CreateReview(_ context.Context, owner, repo string, prNumber int, headSHA, body string, _ []domain.InlineComment) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextReviewID++
	f.reviews = append(f.reviews, reviewCall{owner, repo, prNumber, headSHA, body})
	return f.nextReviewID, nil
}

func (f *fakeGitHub) CreateComment(_ context.Context, owner, repo string, prNumber int, headSHA string, c domain.InlineComment) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextCommentID++
	f.comments = append(f.comments, commentCall{owner, repo, prNumber, headSHA, c})
	return f.nextCommentID, nil
}

func (f *fakeGitHub) CreateIssue(_ context.Context, owner, repo, title, body string, labels []string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextIssueNum++
	f.issues = append(f.issues, issueCall{owner, repo, title, body, labels, f.nextIssueNum})
	return f.nextIssueNum, nil
}

func (f *fakeGitHub) EditIssue(_ context.Context, owner, repo string, number int, _ *string, state *string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := ""
	if state != nil {
		s = *state
	}
	f.editIssues = append(f.editIssues, editIssueCall{owner, repo, number, s})
	return nil
}

func (f *fakeGitHub) AddIssueComment(_ context.Context, owner, repo string, number int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issueComments = append(f.issueComments, issueCommentCall{owner, repo, number, body})
	return nil
}

func (f *fakeGitHub) GetReactions(_ context.Context, _, _ string, commentID int64) ([]domain.Reaction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reactionCalls = append(f.reactionCalls, commentID)
	return f.reactions[commentID], nil
}

func (f *fakeGitHub) GetPushDiff(context.Context, string, string, string, string) ([]domain.ChangedFile, error) {
	return nil, nil
}

func (f *fakeGitHub) ListInstallationRepos(context.Context, int64) ([]domain.Repo, error) {
	return nil, nil
}

type blobCall struct{ content string }
type treeCall struct {
	base    string
	entries []domain.TreeEntry
}
type commitCall struct {
	message string
	tree    string
	parents []string
}
type branchCall struct {
	name string
	sha  string
}
type prCall struct {
	title string
	body  string
	head  string
	base  string
}

func (f *fakeGitHub) GetBranchSHA(context.Context, string, string, string) (string, error) {
	return "basesha", nil
}

func (f *fakeGitHub) GetCommitTreeSHA(context.Context, string, string, string) (string, error) {
	return "basetree", nil
}

func (f *fakeGitHub) GetFileWithSHA(_ context.Context, _, _, path, _ string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fileContents[path], "oldblob", nil
}

func (f *fakeGitHub) CreateBlob(_ context.Context, _, _, content string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blobs = append(f.blobs, blobCall{content})
	return "newblob", nil
}

func (f *fakeGitHub) CreateTree(_ context.Context, _, _, base string, entries []domain.TreeEntry) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trees = append(f.trees, treeCall{base, entries})
	return "newtree", nil
}

func (f *fakeGitHub) CreateCommit(_ context.Context, _, _, message, tree string, parents []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits = append(f.commits, commitCall{message, tree, parents})
	return "newcommit", nil
}

func (f *fakeGitHub) CreateBranch(_ context.Context, _, _, name, sha string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.branches = append(f.branches, branchCall{name, sha})
	return nil
}

func (f *fakeGitHub) CreatePR(_ context.Context, _, _, title, body, head, base string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prs = append(f.prs, prCall{title, body, head, base})
	return 77, nil
}

type prStateKey struct {
	repoID int64
	number int
}

type learningKey struct {
	repoID int64
	key    string
	signal string
}

type fakeStore struct {
	mu                      sync.Mutex
	repo                    *domain.Repo
	prStates                map[prStateKey]domain.PRState
	issuesByRepo            map[int64][]domain.IssueRecord
	nextIssueID             int64
	findingComments         map[int64]int64
	findingIssues           map[int64]int
	issueFindings           map[int][]domain.FindingRecord
	fixPRs                  map[int]int
	learning                map[learningKey]int
	learningKeysForComments map[int64]string
}

func (s *fakeStore) Ping(context.Context) error { return nil }

func (s *fakeStore) RecordDelivery(context.Context, string, string) (bool, error) {
	return true, nil
}

func (s *fakeStore) UpsertInstallation(context.Context, domain.Installation) error { return nil }

func (s *fakeStore) UpsertRepo(context.Context, domain.Repo) error { return nil }

func (s *fakeStore) GetRepo(context.Context, int64) (*domain.Repo, error) { return s.repo, nil }

func (s *fakeStore) GetRepoByName(context.Context, string, string) (*domain.Repo, error) {
	return nil, nil
}

func (s *fakeStore) ListReposForInstallation(context.Context, int64) ([]domain.Repo, error) {
	return nil, nil
}

func (s *fakeStore) SetRepoConfig(context.Context, int64, *domain.RepoConfig) error { return nil }

func (s *fakeStore) GetPRState(context.Context, int64, int) (*domain.PRState, error) {
	return nil, nil
}

func (s *fakeStore) SetPRState(_ context.Context, repoID int64, number int, headSHA string, reviewID, checkRunID *int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps := domain.PRState{RepoID: repoID, Number: number, LastAnalyzedSHA: headSHA}
	if reviewID != nil {
		ps.ReviewID = *reviewID
	}
	if checkRunID != nil {
		ps.CheckRunID = *checkRunID
	}
	s.prStates[prStateKey{repoID, number}] = ps
	return nil
}

func (s *fakeStore) CreateRun(context.Context, int64, string, string, *int) (int64, error) {
	return 0, nil
}

func (s *fakeStore) CompleteRun(context.Context, int64, *domain.ReviewResult) error { return nil }
func (s *fakeStore) RunByKey(context.Context, int64, string, string) (*domain.AnalysisRun, error) {
	return nil, nil
}
func (s *fakeStore) RestartRun(context.Context, int64) error { return nil }

func (s *fakeStore) FailRun(context.Context, int64, string) error { return nil }

func (s *fakeStore) SaveFindings(context.Context, int64, []domain.Finding, func(domain.Finding) string) error {
	return nil
}

func (s *fakeStore) FindingsForRun(context.Context, int64) ([]domain.FindingRecord, error) {
	return nil, nil
}

func (s *fakeStore) CreateIssue(_ context.Context, repoID int64, number int, title, kind string, prNumber *int, findingIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextIssueID++
	rec := domain.IssueRecord{
		ID:         s.nextIssueID,
		RepoID:     repoID,
		Number:     number,
		Title:      title,
		Kind:       kind,
		FindingIDs: findingIDs,
		Status:     "open",
	}
	if prNumber != nil {
		rec.PRNumber = *prNumber
	}
	s.issuesByRepo[repoID] = append(s.issuesByRepo[repoID], rec)
	return nil
}

func (s *fakeStore) CloseIssue(_ context.Context, repoID int64, number int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.issuesByRepo[repoID] {
		if s.issuesByRepo[repoID][i].Number == number {
			s.issuesByRepo[repoID][i].Status = "closed"
		}
	}
	return nil
}

func (s *fakeStore) OpenIssueForRepo(_ context.Context, repoID int64, kind string) (*domain.IssueRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, iss := range s.issuesByRepo[repoID] {
		if iss.Kind == kind && iss.Status == "open" {
			cp := iss
			return &cp, nil
		}
	}
	return nil, nil
}

func (s *fakeStore) IssuesForPR(_ context.Context, repoID int64, prNumber int) ([]domain.IssueRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.IssueRecord
	for _, iss := range s.issuesByRepo[repoID] {
		if iss.PRNumber == prNumber {
			out = append(out, iss)
		}
	}
	return out, nil
}

func (s *fakeStore) IssueByNumber(_ context.Context, repoID int64, number int) (*domain.IssueRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, iss := range s.issuesByRepo[repoID] {
		if iss.Number == number {
			cp := iss
			return &cp, nil
		}
	}
	return nil, nil
}

func (s *fakeStore) SetIssueFixPR(_ context.Context, _ int64, number, prNumber int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fixPRs[number] = prNumber
	return nil
}

func (s *fakeStore) FindingsForIssue(_ context.Context, _ int64, issueNumber int) ([]domain.FindingRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.issueFindings[issueNumber], nil
}

func (s *fakeStore) SetFindingComment(_ context.Context, findingID, commentID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.findingComments[findingID] = commentID
	return nil
}

func (s *fakeStore) SetFindingIssue(_ context.Context, findingID int64, issueNumber int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.findingIssues[findingID] = issueNumber
	return nil
}

func (s *fakeStore) UpsertLearning(_ context.Context, repoID int64, key, signal string, delta int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.learning[learningKey{repoID, key, signal}] += delta
	return nil
}

func (s *fakeStore) SuppressedKeys(context.Context, int64) (map[string]bool, error) {
	return nil, nil
}

func (s *fakeStore) LearningKeysForComments(_ context.Context, _ int64, _ int) (map[int64]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int64]string, len(s.learningKeysForComments))
	for k, v := range s.learningKeysForComments {
		out[k] = v
	}
	return out, nil
}

func (s *fakeStore) Audit(context.Context, domain.AuditEntry) error { return nil }

func newTestPoster() (*Poster, *fakeGitHub, *fakeStore) {
	gh := &fakeGitHub{reactions: map[int64][]domain.Reaction{}, fileContents: map[string]string{}}
	st := &fakeStore{
		repo:                    &domain.Repo{ID: 7, Owner: "acme", Name: "core", DefaultBranch: "main", Enabled: true},
		prStates:                map[prStateKey]domain.PRState{},
		issuesByRepo:            map[int64][]domain.IssueRecord{},
		findingComments:         map[int64]int64{},
		findingIssues:           map[int64]int{},
		issueFindings:           map[int][]domain.FindingRecord{},
		fixPRs:                  map[int]int{},
		learning:                map[learningKey]int{},
		learningKeysForComments: map[int64]string{},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(gh, st, logger), gh, st
}

func testFindings() []domain.FindingRecord {
	return []domain.FindingRecord{
		{
			ID:         1,
			RunID:      10,
			FindingID:  "f1",
			File:       "a.go",
			Line:       12,
			Severity:   string(domain.SeverityCritical),
			Category:   string(domain.CategorySecurity),
			Title:      "SQL injection risk",
			Body:       "User input is concatenated into a query.",
			Suggestion: &domain.Suggestion{Old: "q := \"SELECT\" + in", New: "q := \"SELECT\" + sanitize(in)"},
			DedupeHash: "h1",
		},
		{
			ID:         2,
			RunID:      10,
			FindingID:  "f2",
			File:       "b.go",
			Line:       30,
			Severity:   string(domain.SeverityLow),
			Category:   string(domain.CategoryStyle),
			Title:      "Naming could be clearer",
			Body:       "Rename x to a descriptive name.",
			DedupeHash: "h2",
		},
	}
}

func testOutput(findings []domain.FindingRecord) ReviewOutput {
	return ReviewOutput{
		Kind:      "pr",
		RepoOwner: "acme",
		RepoName:  "widgets",
		RepoID:    1,
		PRNumber:  7,
		HeadSHA:   "deadbeefcafe1234",
		Result:    domain.ReviewResult{Status: domain.StatusChangesRequested, Summary: "Several issues found."},
		Findings:  findings,
		RunID:     10,
	}
}

func TestPostPRReview(t *testing.T) {
	p, gh, st := newTestPoster()
	out := testOutput(testFindings())

	if err := p.PostPRReview(context.Background(), out); err != nil {
		t.Fatalf("PostPRReview: %v", err)
	}

	if len(gh.checkRuns) != 1 {
		t.Fatalf("expected 1 check run, got %d", len(gh.checkRuns))
	}
	cr := gh.checkRuns[0]
	if cr.status != "completed" || cr.conclusion != "neutral" {
		t.Fatalf("check run = %q/%q, want completed/neutral", cr.status, cr.conclusion)
	}
	if cr.summary != "2 findings" {
		t.Fatalf("check run summary = %q, want %q", cr.summary, "2 findings")
	}
	if len(cr.annotations) != 2 {
		t.Fatalf("expected 2 annotations, got %d", len(cr.annotations))
	}
	if cr.annotations[0].Level != "failure" || cr.annotations[1].Level != "warning" {
		t.Fatalf("annotation levels = %q, %q", cr.annotations[0].Level, cr.annotations[1].Level)
	}

	if len(gh.reviews) != 1 {
		t.Fatalf("expected 1 review, got %d", len(gh.reviews))
	}
	if !strings.Contains(gh.reviews[0].body, "## CodePeer Review") {
		t.Fatalf("review body missing header: %q", gh.reviews[0].body)
	}
	if !strings.Contains(gh.reviews[0].body, "Findings below should be addressed.") {
		t.Fatalf("review body missing verdict: %q", gh.reviews[0].body)
	}

	if len(gh.comments) != 2 {
		t.Fatalf("expected 2 inline comments, got %d", len(gh.comments))
	}
	foundSuggestion := false
	for _, c := range gh.comments {
		if strings.Contains(c.comment.Body, "```suggestion") {
			foundSuggestion = true
		}
		if c.comment.Side != "RIGHT" {
			t.Fatalf("comment side = %q, want RIGHT", c.comment.Side)
		}
	}
	if !foundSuggestion {
		t.Fatalf("no inline comment contained a suggestion fence")
	}

	if len(gh.issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(gh.issues))
	}
	if gh.issues[0].labels[0] != "codepeer" || gh.issues[0].labels[1] != "critical" {
		t.Fatalf("issue labels = %v", gh.issues[0].labels)
	}
	if !strings.Contains(gh.issues[0].body, "Found in PR: acme/widgets#7") {
		t.Fatalf("issue body missing PR reference: %q", gh.issues[0].body)
	}

	if st.findingComments[1] == 0 || st.findingComments[2] == 0 {
		t.Fatalf("comment ids not stored: %v", st.findingComments)
	}
	if st.findingIssues[1] != gh.issues[0].num {
		t.Fatalf("finding issue = %d, want %d", st.findingIssues[1], gh.issues[0].num)
	}

	ps, ok := st.prStates[prStateKey{out.RepoID, out.PRNumber}]
	if !ok {
		t.Fatal("PR state not stored")
	}
	if ps.ReviewID == 0 || ps.CheckRunID == 0 {
		t.Fatalf("PR state missing ids: %+v", ps)
	}
}

func TestPostPRReviewNoFindings(t *testing.T) {
	p, gh, _ := newTestPoster()
	out := testOutput(nil)
	out.Result.Status = domain.StatusApproved
	out.Result.Summary = "Clean."

	if err := p.PostPRReview(context.Background(), out); err != nil {
		t.Fatalf("PostPRReview: %v", err)
	}

	if len(gh.checkRuns) != 1 || gh.checkRuns[0].conclusion != "success" {
		t.Fatalf("check run = %+v, want success conclusion", gh.checkRuns)
	}
	if gh.checkRuns[0].summary != "no findings" {
		t.Fatalf("summary = %q, want %q", gh.checkRuns[0].summary, "no findings")
	}
	if len(gh.reviews) != 1 {
		t.Fatalf("expected 1 review, got %d", len(gh.reviews))
	}
	if !strings.Contains(gh.reviews[0].body, "No findings — nothing to act on.") {
		t.Fatalf("review body = %q", gh.reviews[0].body)
	}
	if len(gh.comments) != 0 || len(gh.issues) != 0 {
		t.Fatalf("expected no comments/issues, got %d/%d", len(gh.comments), len(gh.issues))
	}
}

func TestPostPRReviewSkipped(t *testing.T) {
	p, gh, _ := newTestPoster()
	out := testOutput(testFindings())
	out.Skipped = "draft PR"

	if err := p.PostPRReview(context.Background(), out); err != nil {
		t.Fatalf("PostPRReview: %v", err)
	}

	if len(gh.checkRuns) != 0 || len(gh.reviews) != 0 || len(gh.comments) != 0 || len(gh.issues) != 0 {
		t.Fatalf("skipped output still posted: checkRuns=%d reviews=%d comments=%d issues=%d",
			len(gh.checkRuns), len(gh.reviews), len(gh.comments), len(gh.issues))
	}
}

func TestPostPushRollingIssue(t *testing.T) {
	p, gh, st := newTestPoster()
	f := domain.FindingRecord{
		ID:         1,
		RunID:      20,
		FindingID:  "p1",
		File:       "c.go",
		Line:       5,
		Severity:   string(domain.SeverityCritical),
		Category:   string(domain.CategoryBug),
		Title:      "Nil deref",
		Body:       "Missing nil check.",
		DedupeHash: "h1",
	}
	out := ReviewOutput{
		Kind:      "push",
		RepoOwner: "acme",
		RepoName:  "widgets",
		RepoID:    1,
		HeadSHA:   "abcdef1234567890",
		Findings:  []domain.FindingRecord{f},
	}

	if err := p.PostPush(context.Background(), out); err != nil {
		t.Fatalf("PostPush: %v", err)
	}

	if len(gh.issues) != 1 || gh.issues[0].title != "CodePeer Rolling Analysis" {
		t.Fatalf("rolling issue = %+v", gh.issues)
	}
	rollingNum := gh.issues[0].num
	if len(gh.issueComments) != 1 {
		t.Fatalf("expected 1 issue comment, got %d", len(gh.issueComments))
	}
	body := gh.issueComments[0].body
	if !strings.Contains(body, "Nil deref") || !strings.Contains(body, "Push: `abcdef12`") {
		t.Fatalf("push comment body = %q", body)
	}
	if st.findingIssues[1] != rollingNum {
		t.Fatalf("finding issue = %d, want %d", st.findingIssues[1], rollingNum)
	}

	f.IssueNumber = rollingNum
	out.Findings = []domain.FindingRecord{f}
	if err := p.PostPush(context.Background(), out); err != nil {
		t.Fatalf("second PostPush: %v", err)
	}
	if len(gh.issueComments) != 1 {
		t.Fatalf("second push posted %d comments, want still 1", len(gh.issueComments))
	}
}

func TestClosePRIssues(t *testing.T) {
	p, gh, st := newTestPoster()
	st.issuesByRepo[1] = []domain.IssueRecord{
		{ID: 1, RepoID: 1, Number: 5, Title: "CodePeer: x", Kind: "finding", PRNumber: 3, Status: "open", FindingIDs: []string{"h1"}},
		{ID: 2, RepoID: 1, Number: 6, Title: "CodePeer: y", Kind: "finding", PRNumber: 3, Status: "closed"},
		{ID: 3, RepoID: 1, Number: 7, Title: "CodePeer Rolling Analysis", Kind: "rolling", Status: "open"},
	}

	if err := p.ClosePRIssues(context.Background(), 1, "acme", "widgets", 3); err != nil {
		t.Fatalf("ClosePRIssues: %v", err)
	}

	if len(gh.issueComments) != 1 {
		t.Fatalf("expected 1 close comment, got %d", len(gh.issueComments))
	}
	if !strings.Contains(gh.issueComments[0].body, "Resolved by #3") {
		t.Fatalf("close comment body = %q", gh.issueComments[0].body)
	}
	if len(gh.editIssues) != 1 || gh.editIssues[0].number != 5 || gh.editIssues[0].state != "closed" {
		t.Fatalf("edit issues = %+v", gh.editIssues)
	}
	if st.issuesByRepo[1][0].Status != "closed" {
		t.Fatalf("issue 5 status = %q, want closed", st.issuesByRepo[1][0].Status)
	}
}

func TestLearnSweep(t *testing.T) {
	p, gh, st := newTestPoster()
	gh.reactions[5] = []domain.Reaction{
		{User: "alice", Content: "+1"},
		{User: "bob", Content: "-1"},
		{User: "codepeer[bot]", Content: "+1"},
		{User: "alice", Content: "rocket"},
	}
	st.learningKeysForComments[5] = "key1"

	payload := domain.FeedbackPayload{RepoID: 1, RepoOwner: "acme", RepoName: "widgets", PRNumber: 4}
	if err := p.LearnSweep(context.Background(), payload); err != nil {
		t.Fatalf("LearnSweep: %v", err)
	}

	if got := st.learning[learningKey{1, "key1", "up"}]; got != 1 {
		t.Fatalf("up delta = %d, want 1", got)
	}
	if got := st.learning[learningKey{1, "key1", "down"}]; got != -2 {
		t.Fatalf("down delta = %d, want -2", got)
	}
}

func TestBuildCommentBodyFence(t *testing.T) {
	f := domain.FindingRecord{
		Severity:   string(domain.SeverityMedium),
		Category:   string(domain.CategoryBug),
		Title:      "Fence test",
		Body:       "See suggestion.",
		Suggestion: &domain.Suggestion{Old: "x", New: "```\nfoo\n```"},
	}
	body := BuildCommentBody(f)
	if !strings.Contains(body, "```suggestion") {
		t.Fatalf("body missing suggestion fence: %q", body)
	}
	if !strings.Contains(body, "````") {
		t.Fatalf("body missing four-backtick fence: %q", body)
	}
}

func TestHandleIssueCommandDeny(t *testing.T) {
	p, gh, st := newTestPoster()
	st.mu.Lock()
	st.issuesByRepo[7] = []domain.IssueRecord{{ID: 1, RepoID: 7, Number: 12, Title: "x", Kind: "finding", Status: "open"}}
	st.mu.Unlock()

	err := p.HandleIssueCommand(context.Background(), domain.IssueCommandPayload{
		RepoID: 7, RepoOwner: "acme", RepoName: "core", IssueNumber: 12, Command: "deny", SenderLogin: "dev",
	})
	if err != nil {
		t.Fatalf("HandleIssueCommand deny: %v", err)
	}
	gh.mu.Lock()
	defer gh.mu.Unlock()
	if len(gh.editIssues) != 1 || gh.editIssues[0].state != "closed" || gh.editIssues[0].number != 12 {
		t.Errorf("editIssues = %+v, want one close of issue 12", gh.editIssues)
	}
	if len(gh.issueComments) != 1 || !strings.Contains(gh.issueComments[0].body, "deny") {
		t.Errorf("issueComments = %+v", gh.issueComments)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.issuesByRepo[7][0].Status != "closed" {
		t.Errorf("issue should be closed in store, status = %s", st.issuesByRepo[7][0].Status)
	}
}

func TestHandleIssueCommandApprove(t *testing.T) {
	p, gh, st := newTestPoster()
	gh.fileContents["a.go"] = `package main

func main() {
	fmt.Println("debug")
}
`
	st.mu.Lock()
	st.issuesByRepo[7] = []domain.IssueRecord{{ID: 1, RepoID: 7, Number: 12, Title: "x", Kind: "finding", Status: "open"}}
	st.issueFindings[12] = []domain.FindingRecord{{
		ID: 1, File: "a.go", Line: 4, Severity: "high", Title: "Remove debug print",
		Suggestion: &domain.Suggestion{Old: `fmt.Println("debug")`, New: ""},
	}}
	st.mu.Unlock()

	err := p.HandleIssueCommand(context.Background(), domain.IssueCommandPayload{
		RepoID: 7, RepoOwner: "acme", RepoName: "core", IssueNumber: 12, Command: "approve", SenderLogin: "dev",
	})
	if err != nil {
		t.Fatalf("HandleIssueCommand approve: %v", err)
	}
	gh.mu.Lock()
	defer gh.mu.Unlock()
	if len(gh.blobs) != 1 || strings.Contains(gh.blobs[0].content, `fmt.Println("debug")`) {
		t.Errorf("blobs = %+v, want one blob with snippet removed", gh.blobs)
	}
	if len(gh.trees) != 1 || len(gh.trees[0].entries) != 1 || gh.trees[0].entries[0].Path != "a.go" {
		t.Errorf("trees = %+v", gh.trees)
	}
	if len(gh.commits) != 1 || len(gh.commits[0].parents) != 1 || gh.commits[0].parents[0] != "basesha" {
		t.Errorf("commits = %+v", gh.commits)
	}
	if len(gh.branches) != 1 || gh.branches[0].name != "codepeer/fix-issue-12" {
		t.Errorf("branches = %+v", gh.branches)
	}
	if len(gh.prs) != 1 {
		t.Fatalf("prs = %+v, want one fix PR", gh.prs)
	}
	if !strings.Contains(gh.prs[0].body, "Fixes #12") {
		t.Errorf("pr body missing closing keyword: %q", gh.prs[0].body)
	}
	if gh.prs[0].base != "main" || gh.prs[0].head != "codepeer/fix-issue-12" {
		t.Errorf("pr = %+v", gh.prs[0])
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.fixPRs[12] != 77 {
		t.Errorf("fixPRs[12] = %d, want 77", st.fixPRs[12])
	}
}

func TestHandleIssueCommandApproveNoSuggestions(t *testing.T) {
	p, gh, st := newTestPoster()
	st.mu.Lock()
	st.issuesByRepo[7] = []domain.IssueRecord{{ID: 1, RepoID: 7, Number: 12, Title: "x", Kind: "finding", Status: "open"}}
	st.issueFindings[12] = []domain.FindingRecord{{ID: 1, File: "a.go", Line: 4, Severity: "high", Title: "No fix"}}
	st.mu.Unlock()

	err := p.HandleIssueCommand(context.Background(), domain.IssueCommandPayload{
		RepoID: 7, RepoOwner: "acme", RepoName: "core", IssueNumber: 12, Command: "approve", SenderLogin: "dev",
	})
	if err != nil {
		t.Fatalf("approve without suggestions: %v", err)
	}
	gh.mu.Lock()
	defer gh.mu.Unlock()
	if len(gh.prs) != 0 || len(gh.blobs) != 0 {
		t.Errorf("no fix PR should be created without suggestions: prs=%d blobs=%d", len(gh.prs), len(gh.blobs))
	}
	if len(gh.issueComments) != 1 || !strings.Contains(gh.issueComments[0].body, "no automated fix") {
		t.Errorf("issueComments = %+v", gh.issueComments)
	}
}

func TestHandleIssueCommandApproveIdempotent(t *testing.T) {
	p, gh, st := newTestPoster()
	st.mu.Lock()
	st.issuesByRepo[7] = []domain.IssueRecord{{ID: 1, RepoID: 7, Number: 12, Title: "x", Kind: "finding", Status: "open", FixPRNumber: 77}}
	st.issueFindings[12] = []domain.FindingRecord{{ID: 1, File: "a.go", Suggestion: &domain.Suggestion{Old: "a", New: "b"}}}
	st.mu.Unlock()

	err := p.HandleIssueCommand(context.Background(), domain.IssueCommandPayload{
		RepoID: 7, RepoOwner: "acme", RepoName: "core", IssueNumber: 12, Command: "approve", SenderLogin: "dev",
	})
	if err != nil {
		t.Fatalf("idempotent approve: %v", err)
	}
	gh.mu.Lock()
	defer gh.mu.Unlock()
	if len(gh.prs) != 0 || len(gh.blobs) != 0 || len(gh.issueComments) != 0 {
		t.Errorf("idempotent approve must not post: prs=%d blobs=%d comments=%d", len(gh.prs), len(gh.blobs), len(gh.issueComments))
	}
}

func TestHandleIssueCommandUntrackedIssue(t *testing.T) {
	p, gh, _ := newTestPoster()
	err := p.HandleIssueCommand(context.Background(), domain.IssueCommandPayload{
		RepoID: 7, RepoOwner: "acme", RepoName: "core", IssueNumber: 999, Command: "approve", SenderLogin: "dev",
	})
	if err != nil {
		t.Fatalf("untracked issue: %v", err)
	}
	gh.mu.Lock()
	defer gh.mu.Unlock()
	if len(gh.prs) != 0 || len(gh.editIssues) != 0 {
		t.Errorf("untracked issue must be a no-op")
	}
}
