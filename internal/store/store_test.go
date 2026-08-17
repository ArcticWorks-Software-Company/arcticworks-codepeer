package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepoConfigJSONRoundTrip(t *testing.T) {
	cfg := domain.RepoConfig{
		Enabled:            true,
		Mode:               "both",
		Strictness:         "strict",
		IgnorePaths:        []string{"vendor/", "generated/"},
		IgnoreUsernames:    []string{"dependabot[bot]"},
		SkipTitleKeywords:  []string{"WIP", "do not merge"},
		BaseBranches:       []string{"main", "develop"},
		MaxFindings:        5,
		PerFileCap:         2,
		IncludeNits:        true,
		CustomInstructions: []string{"always run gofmt"},
		InstructionFiles:   []string{"AGENTS.md"},
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got domain.RepoConfig
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(cfg, got) {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, cfg)
	}
}

func TestErrDuplicateRun(t *testing.T) {
	wrapped := fmt.Errorf("create run: %w", ErrDuplicateRun)
	if !errors.Is(wrapped, ErrDuplicateRun) {
		t.Fatal("errors.Is should match a wrapped ErrDuplicateRun")
	}
}

func TestFindingHashFnContract(t *testing.T) {
	hashFn := func(f domain.Finding) string {
		return fmt.Sprintf("%s:%d:%s", f.File, f.Line, f.Title)
	}
	f := domain.Finding{File: "a.go", Line: 12, Title: "nil check"}
	if hashFn(f) != hashFn(f) {
		t.Fatal("hash must be deterministic for the same finding")
	}
	g := f
	g.Line = 13
	if hashFn(f) == hashFn(g) {
		t.Fatal("different findings must hash differently")
	}
}

func intPtr(v int) *int { return &v }

func TestIntegrationStore(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping store integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	s := New(pool)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	instID := rng.Int63()
	if instID == 0 {
		instID = 1
	}
	repoID := rng.Int63()
	if repoID == 0 {
		repoID = 1
	}
	owner := fmt.Sprintf("test-owner-%d", instID)
	name := fmt.Sprintf("test-repo-%d", repoID)

	deliveryID := fmt.Sprintf("test-delivery-%d", rng.Int63())
	ok, err := s.RecordDelivery(ctx, deliveryID, "push")
	if err != nil {
		t.Fatalf("RecordDelivery: %v", err)
	}
	if !ok {
		t.Fatal("first RecordDelivery should return true")
	}
	ok, err = s.RecordDelivery(ctx, deliveryID, "push")
	if err != nil {
		t.Fatalf("RecordDelivery dedupe: %v", err)
	}
	if ok {
		t.Fatal("second RecordDelivery should return false")
	}

	if err := s.UpsertInstallation(ctx, domain.Installation{
		ID: instID, AccountID: instID, AccountLogin: owner, AccountType: "Organization",
	}); err != nil {
		t.Fatalf("UpsertInstallation: %v", err)
	}

	cfg := domain.DefaultRepoConfig()
	repo := domain.Repo{
		ID: repoID, InstallationID: instID, Owner: owner, Name: name,
		DefaultBranch: "main", Enabled: true, Config: &cfg,
	}
	if err := s.UpsertRepo(ctx, repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	got, err := s.GetRepo(ctx, repoID)
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.Owner != owner || got.Name != name || got.DefaultBranch != "main" {
		t.Fatalf("GetRepo mismatch: %+v", got)
	}
	if got.Config == nil || got.Config.Strictness != "balanced" {
		t.Fatalf("GetRepo config mismatch: %+v", got.Config)
	}

	gotByName, err := s.GetRepoByName(ctx, owner, name)
	if err != nil {
		t.Fatalf("GetRepoByName: %v", err)
	}
	if gotByName.ID != repoID {
		t.Fatalf("GetRepoByName mismatch: %+v", gotByName)
	}

	newCfg := domain.DefaultRepoConfig()
	newCfg.Mode = "push"
	if err := s.SetRepoConfig(ctx, repoID, &newCfg); err != nil {
		t.Fatalf("SetRepoConfig: %v", err)
	}
	got, err = s.GetRepo(ctx, repoID)
	if err != nil {
		t.Fatalf("GetRepo after SetRepoConfig: %v", err)
	}
	if got.Config == nil || got.Config.Mode != "push" {
		t.Fatalf("SetRepoConfig not applied: %+v", got.Config)
	}

	sha := fmt.Sprintf("sha-%d", rng.Int63())
	runID, err := s.CreateRun(ctx, repoID, "pr", sha, intPtr(7))
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	_, err = s.CreateRun(ctx, repoID, "pr", sha, nil)
	if !errors.Is(err, ErrDuplicateRun) {
		t.Fatalf("CreateRun duplicate: got %v, want ErrDuplicateRun", err)
	}

	if err := s.CompleteRun(ctx, runID, &domain.ReviewResult{Status: domain.StatusNoFindings, Summary: "clean"}); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}

	sha2 := fmt.Sprintf("sha-%d", rng.Int63())
	failRunID, err := s.CreateRun(ctx, repoID, "pr", sha2, nil)
	if err != nil {
		t.Fatalf("CreateRun second: %v", err)
	}
	if err := s.FailRun(ctx, failRunID, "boom"); err != nil {
		t.Fatalf("FailRun: %v", err)
	}

	reviewID := int64(11)
	if err := s.SetPRState(ctx, repoID, 7, sha, &reviewID, nil); err != nil {
		t.Fatalf("SetPRState: %v", err)
	}
	st, err := s.GetPRState(ctx, repoID, 7)
	if err != nil {
		t.Fatalf("GetPRState: %v", err)
	}
	if st == nil || st.LastAnalyzedSHA != sha || st.ReviewID != reviewID || st.CheckRunID != 0 {
		t.Fatalf("GetPRState mismatch: %+v", st)
	}
	st, err = s.GetPRState(ctx, repoID, 7777)
	if err != nil {
		t.Fatalf("GetPRState untracked: %v", err)
	}
	if st != nil {
		t.Fatalf("GetPRState untracked should return nil state, got %+v", st)
	}

	hashFn := func(f domain.Finding) string { return f.ID }
	findings := []domain.Finding{
		{
			ID: "f1", File: "a.go", Line: 3, Severity: domain.SeverityMedium,
			Category: domain.CategoryBug, Title: "nil deref", Body: "check err",
			Suggestion: &domain.Suggestion{Old: "x", New: "y"},
			Confidence: 0.9, Actionable: true,
		},
	}
	if err := s.SaveFindings(ctx, runID, findings, hashFn); err != nil {
		t.Fatalf("SaveFindings: %v", err)
	}
	recs, err := s.FindingsForRun(ctx, runID)
	if err != nil {
		t.Fatalf("FindingsForRun: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("FindingsForRun: got %d records, want 1", len(recs))
	}
	if recs[0].DedupeHash != "f1" || recs[0].Suggestion == nil || recs[0].Suggestion.Old != "x" {
		t.Fatalf("FindingsForRun mismatch: %+v", recs[0])
	}

	if err := s.SaveFindings(ctx, runID, findings, hashFn); err != nil {
		t.Fatalf("SaveFindings second: %v", err)
	}
	recs, err = s.FindingsForRun(ctx, runID)
	if err != nil {
		t.Fatalf("FindingsForRun after dedupe: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("SaveFindings should dedupe: got %d records", len(recs))
	}

	commentID := int64(55555)
	if err := s.SetFindingComment(ctx, recs[0].ID, commentID); err != nil {
		t.Fatalf("SetFindingComment: %v", err)
	}
	keys, err := s.LearningKeysForComments(ctx, repoID, 7)
	if err != nil {
		t.Fatalf("LearningKeysForComments: %v", err)
	}
	if keys[commentID] != "f1" {
		t.Fatalf("LearningKeysForComments mismatch: %+v", keys)
	}

	if err := s.UpsertLearning(ctx, repoID, "suppressed-key", "down", -2); err != nil {
		t.Fatalf("UpsertLearning: %v", err)
	}
	if err := s.UpsertLearning(ctx, repoID, "fine-key", "up", 1); err != nil {
		t.Fatalf("UpsertLearning fine: %v", err)
	}
	if err := s.UpsertLearning(ctx, repoID, "suppressed-key", "down", 0); err != nil {
		t.Fatalf("UpsertLearning repeat: %v", err)
	}
	suppressed, err := s.SuppressedKeys(ctx, repoID)
	if err != nil {
		t.Fatalf("SuppressedKeys: %v", err)
	}
	if !suppressed["suppressed-key"] {
		t.Fatalf("suppressed-key should be suppressed: %+v", suppressed)
	}
	if suppressed["fine-key"] {
		t.Fatalf("fine-key should not be suppressed: %+v", suppressed)
	}

	issueNumber := rng.Intn(100000) + 1000000
	if err := s.CreateIssue(ctx, repoID, issueNumber, "rolling issue", "rolling", intPtr(7), []string{"f1"}); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	openIssue, err := s.OpenIssueForRepo(ctx, repoID, "rolling")
	if err != nil {
		t.Fatalf("OpenIssueForRepo: %v", err)
	}
	if openIssue == nil || openIssue.Number != issueNumber {
		t.Fatalf("OpenIssueForRepo mismatch: %+v", openIssue)
	}
	if err := s.CloseIssue(ctx, repoID, issueNumber); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	openIssue, err = s.OpenIssueForRepo(ctx, repoID, "rolling")
	if err != nil {
		t.Fatalf("OpenIssueForRepo closed: %v", err)
	}
	if openIssue != nil {
		t.Fatalf("OpenIssueForRepo after close should return nil, got %+v", openIssue)
	}
	if err := s.CreateIssue(ctx, repoID, issueNumber, "reopened", "rolling", intPtr(7), nil); err != nil {
		t.Fatalf("CreateIssue reopen: %v", err)
	}
	openIssue, err = s.OpenIssueForRepo(ctx, repoID, "rolling")
	if err != nil {
		t.Fatalf("OpenIssueForRepo reopened: %v", err)
	}
	if openIssue == nil || openIssue.Status != "open" {
		t.Fatalf("reopened issue should be open: %+v", openIssue)
	}
	prIssues, err := s.IssuesForPR(ctx, repoID, 7)
	if err != nil {
		t.Fatalf("IssuesForPR: %v", err)
	}
	if len(prIssues) != 1 {
		t.Fatalf("IssuesForPR: got %d issues, want 1", len(prIssues))
	}

	if err := s.Audit(ctx, domain.AuditEntry{
		DeliveryID: deliveryID, Event: "push", Action: "analyze",
		RepoID: repoID, Kind: "pr", Detail: map[string]any{"sha": sha},
	}); err != nil {
		t.Fatalf("Audit: %v", err)
	}

	if err := s.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}
