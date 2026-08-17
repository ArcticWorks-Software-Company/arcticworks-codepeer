package githubx

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/go-github/v69/github"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
)

const (
	maxRetries          = 2
	maxCheckAnnotations = 50
)

var _ domain.GitHubAPI = (*Client)(nil)

// SelfLogin returns the bot's own login.
func (c *Client) SelfLogin() string {
	c.selfMu.Lock()
	defer c.selfMu.Unlock()
	return c.cfg.SelfLogin
}

// GetRawDiff returns the unified diff of a PR.
func (c *Client) GetRawDiff(ctx context.Context, owner, repo string, prNumber int) (string, error) {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return "", err
	}
	var raw string
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		diff, resp, callErr := client.PullRequests.GetRaw(ctx, owner, repo, prNumber, github.RawOptions{Type: github.Diff})
		raw = diff
		return resp, callErr
	})
	if err != nil {
		return "", err
	}
	return raw, nil
}

// ListPRFiles returns the per-file breakdown of a PR diff.
func (c *Client) ListPRFiles(ctx context.Context, owner, repo string, prNumber int) ([]domain.ChangedFile, error) {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return nil, err
	}
	var files []domain.ChangedFile
	opts := &github.ListOptions{PerPage: 100}
	for {
		var page []*github.CommitFile
		_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
			p, resp, callErr := client.PullRequests.ListFiles(ctx, owner, repo, prNumber, opts)
			page = p
			return resp, callErr
		})
		if err != nil {
			return nil, err
		}
		files = append(files, mapCommitFiles(page)...)
		if len(page) < opts.PerPage {
			break
		}
		opts.Page++
	}
	return files, nil
}

// GetFile returns the content of a file at a ref, or "" if it is missing.
func (c *Client) GetFile(ctx context.Context, owner, repo, path, ref string) (string, error) {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return "", err
	}
	var content string
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		file, _, resp, callErr := client.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{Ref: ref})
		if callErr != nil {
			return resp, callErr
		}
		if file != nil {
			content, callErr = file.GetContent()
			if callErr != nil {
				return resp, callErr
			}
		}
		return resp, nil
	})
	if err != nil {
		var ghErr *github.ErrorResponse
		if errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusNotFound {
			return "", nil
		}
		return "", err
	}
	return content, nil
}

// GetPR returns PR metadata.
func (c *Client) GetPR(ctx context.Context, owner, repo string, prNumber int) (*domain.PRInfo, error) {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return nil, err
	}
	var pr *github.PullRequest
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		p, resp, callErr := client.PullRequests.Get(ctx, owner, repo, prNumber)
		pr = p
		return resp, callErr
	})
	if err != nil {
		return nil, err
	}
	info := &domain.PRInfo{
		Number:  pr.GetNumber(),
		Title:   pr.GetTitle(),
		Body:    pr.GetBody(),
		State:   pr.GetState(),
		Draft:   pr.GetDraft(),
		HeadSHA: pr.GetHead().GetSHA(),
		BaseRef: pr.GetBase().GetRef(),
		HeadRef: pr.GetHead().GetRef(),
		Merged:  pr.GetMerged(),
	}
	if pr.GetUser() != nil {
		info.UserLogin = pr.GetUser().GetLogin()
	}
	return info, nil
}

// GetDefaultBranch returns the repo default branch name.
func (c *Client) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return "", err
	}
	var r *github.Repository
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		repository, resp, callErr := client.Repositories.Get(ctx, owner, repo)
		r = repository
		return resp, callErr
	})
	if err != nil {
		return "", err
	}
	return r.GetDefaultBranch(), nil
}

// CreateCheckRun creates a check run and returns its ID.
func (c *Client) CreateCheckRun(ctx context.Context, owner, repo, name, headSHA, status, conclusion, title, summary string, annotations []domain.Annotation) (int64, error) {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return 0, err
	}
	opts := github.CreateCheckRunOptions{
		Name:       name,
		HeadSHA:    headSHA,
		Status:     github.Ptr(status),
		Conclusion: github.Ptr(conclusion),
		Output: &github.CheckRunOutput{
			Title:       github.Ptr(title),
			Summary:     github.Ptr(summary),
			Annotations: mapAnnotations(annotations),
		},
	}
	if status == "in_progress" {
		opts.StartedAt = &github.Timestamp{Time: time.Now()}
	}
	if status == "completed" {
		opts.CompletedAt = &github.Timestamp{Time: time.Now()}
	}
	var created *github.CheckRun
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		run, resp, callErr := client.Checks.CreateCheckRun(ctx, owner, repo, opts)
		created = run
		return resp, callErr
	})
	if err != nil {
		return 0, err
	}
	return created.GetID(), nil
}

// UpdateCheckRun updates an existing check run.
func (c *Client) UpdateCheckRun(ctx context.Context, owner, repo string, checkRunID int64, status, conclusion, summary string, annotations []domain.Annotation) error {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return err
	}
	opts := github.UpdateCheckRunOptions{
		Status:     github.Ptr(status),
		Conclusion: github.Ptr(conclusion),
		Output: &github.CheckRunOutput{
			Summary:     github.Ptr(summary),
			Annotations: mapAnnotations(annotations),
		},
	}
	if status == "completed" {
		opts.CompletedAt = &github.Timestamp{Time: time.Now()}
	}
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		_, resp, callErr := client.Checks.UpdateCheckRun(ctx, owner, repo, checkRunID, opts)
		return resp, callErr
	})
	return err
}

// CreateReview posts a summary review with optional inline comments and
// returns the review ID. The event is always COMMENT.
func (c *Client) CreateReview(ctx context.Context, owner, repo string, prNumber int, headSHA, body string, comments []domain.InlineComment) (int64, error) {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return 0, err
	}
	req := &github.PullRequestReviewRequest{
		CommitID: github.Ptr(headSHA),
		Body:     github.Ptr(body),
		Event:    github.Ptr("COMMENT"),
		Comments: make([]*github.DraftReviewComment, 0, len(comments)),
	}
	for _, cm := range comments {
		draft := &github.DraftReviewComment{
			Path: github.Ptr(cm.Path),
			Body: github.Ptr(cm.Body),
			Side: github.Ptr(cm.Side),
			Line: github.Ptr(cm.Line),
		}
		if cm.StartLine > 0 {
			draft.StartSide = github.Ptr(cm.StartSide)
			draft.StartLine = github.Ptr(cm.StartLine)
		}
		req.Comments = append(req.Comments, draft)
	}
	var review *github.PullRequestReview
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		r, resp, callErr := client.PullRequests.CreateReview(ctx, owner, repo, prNumber, req)
		review = r
		return resp, callErr
	})
	if err != nil {
		return 0, err
	}
	return review.GetID(), nil
}

// CreateComment posts a single inline review comment.
func (c *Client) CreateComment(ctx context.Context, owner, repo string, prNumber int, headSHA string, cm domain.InlineComment) (int64, error) {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return 0, err
	}
	comment := &github.PullRequestComment{
		Body:     github.Ptr(cm.Body),
		CommitID: github.Ptr(headSHA),
		Path:     github.Ptr(cm.Path),
		Side:     github.Ptr(cm.Side),
	}
	if cm.Line > 0 {
		comment.Line = github.Ptr(cm.Line)
		comment.SubjectType = github.Ptr("line")
		if cm.StartLine > 0 {
			comment.StartLine = github.Ptr(cm.StartLine)
			comment.StartSide = github.Ptr(cm.StartSide)
		}
	} else {
		comment.SubjectType = github.Ptr("file")
	}
	var created *github.PullRequestComment
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		posted, resp, callErr := client.PullRequests.CreateComment(ctx, owner, repo, prNumber, comment)
		created = posted
		return resp, callErr
	})
	if err != nil {
		return 0, err
	}
	return created.GetID(), nil
}

// CreateIssue creates an issue and returns its number.
func (c *Client) CreateIssue(ctx context.Context, owner, repo, title, body string, labels []string) (int, error) {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return 0, err
	}
	req := &github.IssueRequest{
		Title:  github.Ptr(title),
		Body:   github.Ptr(body),
		Labels: &labels,
	}
	var issue *github.Issue
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		i, resp, callErr := client.Issues.Create(ctx, owner, repo, req)
		issue = i
		return resp, callErr
	})
	if err != nil {
		return 0, err
	}
	return issue.GetNumber(), nil
}

// EditIssue updates an issue body and/or state.
func (c *Client) EditIssue(ctx context.Context, owner, repo string, number int, body, state *string) error {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return err
	}
	req := &github.IssueRequest{Body: body, State: state}
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		_, resp, callErr := client.Issues.Edit(ctx, owner, repo, number, req)
		return resp, callErr
	})
	return err
}

// AddIssueComment comments on an issue.
func (c *Client) AddIssueComment(ctx context.Context, owner, repo string, number int, body string) error {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return err
	}
	comment := &github.IssueComment{Body: github.Ptr(body)}
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		_, resp, callErr := client.Issues.CreateComment(ctx, owner, repo, number, comment)
		return resp, callErr
	})
	return err
}

// GetReactions lists reactions on a review comment.
func (c *Client) GetReactions(ctx context.Context, owner, repo string, commentID int64) ([]domain.Reaction, error) {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return nil, err
	}
	var reactions []*github.Reaction
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		rs, resp, callErr := client.Reactions.ListPullRequestCommentReactions(ctx, owner, repo, commentID, nil)
		reactions = rs
		return resp, callErr
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Reaction, 0, len(reactions))
	for _, r := range reactions {
		out = append(out, domain.Reaction{
			User:    r.GetUser().GetLogin(),
			Content: r.GetContent(),
		})
	}
	return out, nil
}

// GetPushDiff returns the diff of a push between two SHAs. before may be
// all-zeroes for branch creation.
func (c *Client) GetPushDiff(ctx context.Context, owner, repo, before, after string) ([]domain.ChangedFile, error) {
	if before == after {
		return []domain.ChangedFile{}, nil
	}
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return nil, err
	}
	if isZeroSHA(before) {
		var commit *github.RepositoryCommit
		_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
			cm, resp, callErr := client.Repositories.GetCommit(ctx, owner, repo, after, nil)
			commit = cm
			return resp, callErr
		})
		if err != nil {
			return nil, err
		}
		return mapCommitFiles(commit.Files), nil
	}
	var comparison *github.CommitsComparison
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		cc, resp, callErr := client.Repositories.CompareCommits(ctx, owner, repo, before, after, nil)
		comparison = cc
		return resp, callErr
	})
	if err != nil {
		return nil, err
	}
	return mapCommitFiles(comparison.Files), nil
}

// ListInstallationRepos lists repos accessible to an installation.
func (c *Client) ListInstallationRepos(ctx context.Context, installationID int64) ([]domain.Repo, error) {
	client, err := c.clientFor(ctx, installationID)
	if err != nil {
		return nil, err
	}
	var repos []domain.Repo
	opts := &github.ListOptions{PerPage: 100}
	for {
		var page *github.ListRepositories
		_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
			lr, resp, callErr := client.Apps.ListRepos(ctx, opts)
			page = lr
			return resp, callErr
		})
		if err != nil {
			return nil, err
		}
		for _, r := range page.Repositories {
			repos = append(repos, domain.Repo{
				ID:             r.GetID(),
				InstallationID: installationID,
				Owner:          r.GetOwner().GetLogin(),
				Name:           r.GetName(),
				DefaultBranch:  r.GetDefaultBranch(),
				Enabled:        !r.GetArchived(),
			})
		}
		if len(page.Repositories) < opts.PerPage {
			break
		}
		opts.Page++
	}
	return repos, nil
}

func (c *Client) doWithRetry(ctx context.Context, fn func() (*github.Response, error)) (*github.Response, error) {
	var resp *github.Response
	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return resp, ctx.Err()
			case <-time.After(retryDelay(attempt, resp)):
			}
		}
		resp, err = fn()
		if err == nil {
			return resp, nil
		}
		if !isRetryable(resp, err) {
			return resp, err
		}
	}
	return resp, err
}

func isRetryable(resp *github.Response, err error) bool {
	var rateErr *github.RateLimitError
	if errors.As(err, &rateErr) {
		return true
	}
	if resp == nil || resp.Response == nil {
		return false
	}
	code := resp.StatusCode
	return code == http.StatusForbidden || code == http.StatusTooManyRequests || code >= http.StatusInternalServerError
}

func retryDelay(attempt int, resp *github.Response) time.Duration {
	if resp != nil && resp.Response != nil {
		if header := resp.Response.Header.Get("Retry-After"); header != "" {
			if secs, err := strconv.Atoi(header); err == nil && secs > 0 {
				if secs > 60 {
					secs = 60
				}
				return time.Duration(secs) * time.Second
			}
		}
	}
	if attempt == 1 {
		return 2 * time.Second
	}
	return 4 * time.Second
}

func mapAnnotations(annotations []domain.Annotation) []*github.CheckRunAnnotation {
	if len(annotations) > maxCheckAnnotations {
		annotations = annotations[:maxCheckAnnotations]
	}
	out := make([]*github.CheckRunAnnotation, 0, len(annotations))
	for _, a := range annotations {
		ann := &github.CheckRunAnnotation{
			Path:            github.Ptr(a.Path),
			AnnotationLevel: github.Ptr(a.Level),
			Message:         github.Ptr(a.Message),
			Title:           github.Ptr(a.Title),
		}
		if a.StartLine > 0 {
			ann.StartLine = github.Ptr(a.StartLine)
		}
		if a.EndLine > 0 {
			ann.EndLine = github.Ptr(a.EndLine)
		}
		out = append(out, ann)
	}
	return out
}

func isZeroSHA(sha string) bool {
	if sha == "" {
		return false
	}
	for _, b := range sha {
		if b != '0' {
			return false
		}
	}
	return true
}

func mapCommitFiles(files []*github.CommitFile) []domain.ChangedFile {
	out := make([]domain.ChangedFile, 0, len(files))
	for _, f := range files {
		out = append(out, domain.ChangedFile{
			Path:      f.GetFilename(),
			Status:    f.GetStatus(),
			Additions: f.GetAdditions(),
			Deletions: f.GetDeletions(),
			Patch:     f.GetPatch(),
		})
	}
	return out
}
