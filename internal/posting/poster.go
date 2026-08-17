package posting

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
)

const checkRunName = "CodePeer"

const (
	maxAnnotationsPerRun = 50
	maxCommentsPerReview = 20
	maxIssuesPerReview   = 5
	maxPushFindings      = 10
	maxCloseIssues       = 10
	maxLearnComments     = 50
)

// Poster owns all outward GitHub side effects for CodePeer analyses.
type Poster struct {
	GitHub domain.GitHubAPI
	Store  domain.Store
	Logger *slog.Logger
}

// New builds a Poster; a nil logger falls back to slog.Default.
func New(gh domain.GitHubAPI, st domain.Store, logger *slog.Logger) *Poster {
	if logger == nil {
		logger = slog.Default()
	}
	return &Poster{GitHub: gh, Store: st, Logger: logger}
}

type pendingComment struct {
	findingID int64
	comment   domain.InlineComment
}

func pendingInlineComments(fs []domain.FindingRecord) []pendingComment {
	var out []pendingComment
	for _, f := range fs {
		if len(out) >= maxCommentsPerReview {
			break
		}
		if f.Line <= 0 {
			continue
		}
		out = append(out, pendingComment{
			findingID: f.ID,
			comment: domain.InlineComment{
				Path: f.File,
				Body: BuildCommentBody(f),
				Line: f.Line,
				Side: "RIGHT",
			},
		})
	}
	return out
}

func issueReferencesFinding(issues []domain.IssueRecord, hash string) bool {
	for _, iss := range issues {
		if iss.Kind != "finding" || iss.Status == "closed" {
			continue
		}
		if slices.Contains(iss.FindingIDs, hash) {
			return true
		}
	}
	return false
}

// PostPRReview posts the check run, summary review, inline comments and
// analysis issues for one PR analysis pass.
func (p *Poster) PostPRReview(ctx context.Context, out ReviewOutput) error {
	if out.Skipped != "" {
		p.Logger.Info("skipping PR posting", "reason", out.Skipped, "pr", out.PRNumber)
		return nil
	}

	hasFindings := len(out.Findings) > 0
	conclusion := "success"
	summary := "no findings"
	if hasFindings {
		conclusion = "neutral"
		summary = strconv.Itoa(len(out.Findings)) + " findings"
	}

	var annotations []domain.Annotation
	for _, f := range out.Findings {
		if len(annotations) >= maxAnnotationsPerRun {
			break
		}
		if f.Line <= 0 {
			continue
		}
		level := "warning"
		if f.Severity == string(domain.SeverityCritical) {
			level = "failure"
		}
		annotations = append(annotations, domain.Annotation{
			Path:      f.File,
			StartLine: f.Line,
			EndLine:   f.Line,
			Level:     level,
			Message:   f.Title,
			Title:     f.Severity + " " + f.Category,
		})
	}

	checkRunID, err := p.GitHub.CreateCheckRun(ctx, out.RepoOwner, out.RepoName, checkRunName, out.HeadSHA, "completed", conclusion, "CodePeer review", summary, annotations)
	if err != nil {
		return err
	}

	reviewID, err := p.GitHub.CreateReview(ctx, out.RepoOwner, out.RepoName, out.PRNumber, out.HeadSHA, BuildSummaryBody(out), nil)
	if err != nil {
		return err
	}

	comments := pendingInlineComments(out.Findings)
	for i, c := range comments {
		if i > 0 {
			time.Sleep(time.Second)
		}
		commentID, err := p.GitHub.CreateComment(ctx, out.RepoOwner, out.RepoName, out.PRNumber, out.HeadSHA, c.comment)
		if err != nil {
			p.Logger.Warn("posting inline comment failed", "file", c.comment.Path, "line", c.comment.Line, "error", err)
			continue
		}
		if err := p.Store.SetFindingComment(ctx, c.findingID, commentID); err != nil {
			p.Logger.Warn("storing comment id failed", "finding", c.findingID, "error", err)
		}
	}

	existing, err := p.Store.IssuesForPR(ctx, out.RepoID, out.PRNumber)
	if err != nil {
		p.Logger.Warn("listing PR issues failed", "error", err)
		existing = nil
	}
	posted := 0
	attempted := 0
	for _, f := range out.Findings {
		if posted >= maxIssuesPerReview {
			break
		}
		if f.Severity != string(domain.SeverityCritical) && f.Severity != string(domain.SeverityHigh) {
			continue
		}
		if f.DedupeHash != "" && issueReferencesFinding(existing, f.DedupeHash) {
			continue
		}
		if attempted > 0 {
			time.Sleep(time.Second)
		}
		attempted++
		num, err := p.GitHub.CreateIssue(ctx, out.RepoOwner, out.RepoName, issueTitle(f), BuildIssueBody(out, f, out.PRNumber, out.RepoOwner, out.RepoName), []string{"codepeer", f.Severity})
		if err != nil {
			p.Logger.Warn("creating issue failed", "finding", f.FindingID, "error", err)
			continue
		}
		if err := p.Store.CreateIssue(ctx, out.RepoID, num, issueTitle(f), "finding", &out.PRNumber, []string{f.DedupeHash}); err != nil {
			p.Logger.Warn("storing issue failed", "number", num, "error", err)
		}
		if err := p.Store.SetFindingIssue(ctx, f.ID, num); err != nil {
			p.Logger.Warn("storing finding issue failed", "finding", f.FindingID, "error", err)
		}
		posted++
	}

	return p.Store.SetPRState(ctx, out.RepoID, out.PRNumber, out.HeadSHA, &reviewID, &checkRunID)
}

// PostPush maintains the rolling push-mode issue, posting one combined comment
// per push.
func (p *Poster) PostPush(ctx context.Context, out ReviewOutput) error {
	if out.Skipped != "" {
		p.Logger.Info("skipping push posting", "reason", out.Skipped)
		return nil
	}

	rolling, err := p.Store.OpenIssueForRepo(ctx, out.RepoID, "rolling")
	if err != nil {
		return err
	}
	if rolling == nil {
		num, err := p.GitHub.CreateIssue(ctx, out.RepoOwner, out.RepoName, "CodePeer Rolling Analysis", "Rolling findings from push-mode analysis. Each comment below corresponds to a push.", []string{"codepeer"})
		if err != nil {
			return err
		}
		if err := p.Store.CreateIssue(ctx, out.RepoID, num, "CodePeer Rolling Analysis", "rolling", nil, nil); err != nil {
			return err
		}
		rolling, err = p.Store.OpenIssueForRepo(ctx, out.RepoID, "rolling")
		if err != nil {
			return err
		}
		if rolling == nil {
			return errors.New("rolling issue not found after creation")
		}
	}

	var newFindings []domain.FindingRecord
	for _, f := range out.Findings {
		if len(newFindings) >= maxPushFindings {
			break
		}
		if f.Severity != string(domain.SeverityCritical) && f.Severity != string(domain.SeverityHigh) && f.Severity != string(domain.SeverityMedium) {
			continue
		}
		if f.IssueNumber == rolling.Number {
			continue
		}
		newFindings = append(newFindings, f)
	}
	if len(newFindings) == 0 {
		return nil
	}

	time.Sleep(time.Second)
	body := BuildPushCommentBody(newFindings, out.HeadSHA)
	if err := p.GitHub.AddIssueComment(ctx, out.RepoOwner, out.RepoName, rolling.Number, body); err != nil {
		return err
	}
	for _, f := range newFindings {
		if err := p.Store.SetFindingIssue(ctx, f.ID, rolling.Number); err != nil {
			p.Logger.Warn("storing finding issue failed", "finding", f.FindingID, "error", err)
		}
	}
	return nil
}

// ClosePRIssues closes open finding issues after a PR is merged or closed.
func (p *Poster) ClosePRIssues(ctx context.Context, repoID int64, owner, repo string, prNumber int) error {
	issues, err := p.Store.IssuesForPR(ctx, repoID, prNumber)
	if err != nil {
		return err
	}
	closed := 0
	for _, issue := range issues {
		if issue.Kind != "finding" || issue.Status == "closed" {
			continue
		}
		if closed >= maxCloseIssues {
			break
		}
		if closed > 0 {
			time.Sleep(time.Second)
		}
		if err := p.GitHub.AddIssueComment(ctx, owner, repo, issue.Number, "The PR was merged or closed. Resolved by #"+strconv.Itoa(prNumber)+"."); err != nil {
			return err
		}
		state := "closed"
		if err := p.GitHub.EditIssue(ctx, owner, repo, issue.Number, nil, &state); err != nil {
			return err
		}
		if err := p.Store.CloseIssue(ctx, repoID, issue.Number); err != nil {
			return err
		}
		closed++
	}
	return nil
}

// LearnSweep converts reactions on bot comments into learning signals.
func (p *Poster) LearnSweep(ctx context.Context, payload domain.FeedbackPayload) error {
	keys, err := p.Store.LearningKeysForComments(ctx, payload.RepoID, payload.PRNumber)
	if err != nil {
		return err
	}
	self := p.GitHub.SelfLogin()
	seen := 0
	for commentID, key := range keys {
		if seen >= maxLearnComments {
			break
		}
		seen++
		reactions, err := p.GitHub.GetReactions(ctx, payload.RepoOwner, payload.RepoName, commentID)
		if err != nil {
			continue
		}
		for _, r := range reactions {
			if r.User == "" || r.User == self || strings.HasSuffix(r.User, "[bot]") {
				continue
			}
			switch r.Content {
			case "+1":
				if err := p.Store.UpsertLearning(ctx, payload.RepoID, key, "up", 1); err != nil {
					p.Logger.Warn("upsert learning failed", "key", key, "error", err)
				}
			case "-1":
				if err := p.Store.UpsertLearning(ctx, payload.RepoID, key, "down", -2); err != nil {
					p.Logger.Warn("upsert learning failed", "key", key, "error", err)
				}
			}
		}
	}
	return nil
}
