// Package domain holds the shared types and interfaces used across all
// CodePeer packages. It must not import any other internal package.
package domain

import (
	"context"
	"encoding/json"
	"time"
)

// Severity of a finding, ordered critical > high > medium > low > nit.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityNit      Severity = "nit"
)

// Category of a finding.
type Category string

const (
	CategoryBug             Category = "bug"
	CategorySecurity        Category = "security"
	CategoryPerformance     Category = "performance"
	CategoryCorrectness     Category = "correctness"
	CategoryTest            Category = "test"
	CategoryMaintainability Category = "maintainability"
	CategoryStyle           Category = "style"
	CategoryOther           Category = "other"
)

// Suggestion is an exact before/after fix.
type Suggestion struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// Finding is a single, actionable review finding.
type Finding struct {
	ID         string      `json:"id"`
	File       string      `json:"file"`
	Line       int         `json:"line"` // new-side line in head commit; 0 = file-level
	Severity   Severity    `json:"severity"`
	Category   Category    `json:"category"`
	Title      string      `json:"title"`
	Body       string      `json:"body"`
	Suggestion *Suggestion `json:"suggestion,omitempty"`
	Confidence float64     `json:"confidence"`
	Actionable bool        `json:"actionable"`
}

// ReviewStatus is the overall verdict of a review.
type ReviewStatus string

const (
	StatusApproved         ReviewStatus = "approved"
	StatusChangesRequested ReviewStatus = "changes_requested"
	StatusNoFindings       ReviewStatus = "no_findings"
)

// ReviewResult is the normalized output of the LLM review pass.
type ReviewResult struct {
	Summary  string       `json:"summary"`
	Status   ReviewStatus `json:"status"`
	Findings []Finding    `json:"findings"`
}

// ChangedFile describes one file in a diff.
type ChangedFile struct {
	Path      string `json:"path"`
	Status    string `json:"status"` // added | modified | removed | renamed
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"`
}

// PRInfo is the subset of PR data the pipeline needs.
type PRInfo struct {
	Number    int
	Title     string
	Body      string
	State     string
	Draft     bool
	HeadSHA   string
	BaseRef   string
	HeadRef   string
	UserLogin string
	Merged    bool
}

// ReviewConfig controls a single review pass.
type ReviewConfig struct {
	Strictness         string
	MaxFindings        int
	PerFileCap         int
	IncludeNits        bool
	CustomInstructions []string
}

// ReviewRequest is everything the Reviewer gets.
type ReviewRequest struct {
	RepoOwner    string
	RepoName     string
	PRNumber     int
	PRTitle      string
	PRBody       string
	HeadSHA      string
	Diff         string
	Files        []ChangedFile
	Context      map[string]string // path -> neighboring context (full file or trimmed)
	Instructions string            // repo standards, e.g. AGENTS.md content
	Config       ReviewConfig
}

// Reviewer performs one analysis pass. Implementations must be safe for
// concurrent use.
type Reviewer interface {
	Review(ctx context.Context, req ReviewRequest) (ReviewResult, error)
}

// RepoConfig is the parsed .codepeer.yml.
type RepoConfig struct {
	Enabled            bool     `yaml:"enabled"`
	Mode               string   `yaml:"mode"` // pr | push | both
	Strictness         string   `yaml:"strictness"`
	IgnorePaths        []string `yaml:"ignore_paths"`
	IgnoreUsernames    []string `yaml:"ignore_usernames"`
	SkipTitleKeywords  []string `yaml:"skip_title_keywords"`
	BaseBranches       []string `yaml:"base_branches"`
	MaxFindings        int      `yaml:"max_findings"`
	PerFileCap         int      `yaml:"per_file_cap"`
	IncludeNits        bool     `yaml:"include_nits"`
	CustomInstructions []string `yaml:"custom_instructions"`
	InstructionFiles   []string `yaml:"instruction_files"`
}

// DefaultRepoConfig returns the built-in defaults.
func DefaultRepoConfig() RepoConfig {
	return RepoConfig{
		Enabled:     true,
		Mode:        "pr",
		Strictness:  "balanced",
		MaxFindings: 10,
		PerFileCap:  3,
		IncludeNits: false,
	}
}

// Installation is a GitHub App installation.
type Installation struct {
	ID           int64
	AccountID    int64
	AccountLogin string
	AccountType  string // User | Organization
}

// Repo is a repository the app is installed on.
type Repo struct {
	ID             int64
	InstallationID int64
	Owner          string
	Name           string
	DefaultBranch  string
	Enabled        bool
	Config         *RepoConfig
}

// PRState tracks analysis state per PR.
type PRState struct {
	RepoID          int64
	Number          int
	LastAnalyzedSHA string
	ReviewID        int64
	CheckRunID      int64
}

// AnalysisRun is one recorded analysis pass.
type AnalysisRun struct {
	ID        int64
	RepoID    int64
	Kind      string // pr | push
	InputSHA  string
	PRNumber  int
	Status    string
	Result    *ReviewResult
	Error     string
	CreatedAt time.Time
}

// FindingRecord is a persisted finding.
type FindingRecord struct {
	ID          int64
	RunID       int64
	FindingID   string
	File        string
	Line        int
	Severity    string
	Category    string
	Title       string
	Body        string
	Suggestion  *Suggestion
	Confidence  float64
	Actionable  bool
	DedupeHash  string
	CommentID   int64
	IssueNumber int
}

// IssueRecord is a tracked GitHub issue created by the bot.
type IssueRecord struct {
	ID         int64
	RepoID     int64
	Number     int
	Title      string
	Kind       string // finding | rolling
	PRNumber   int
	FindingIDs []string
	Status     string // open | closed
}

// JobKind enumerates queue job kinds.
type JobKind string

const (
	JobAnalyzePR   JobKind = "analyze_pr"
	JobAnalyzePush JobKind = "analyze_push"
	JobFeedback    JobKind = "feedback_sweep"
	JobIssueClose  JobKind = "close_pr_issues"
)

// JobStatus enumerates job lifecycle states.
type JobStatus string

const (
	JobPending JobStatus = "pending"
	JobRunning JobStatus = "running"
	JobDone    JobStatus = "done"
	JobFailed  JobStatus = "failed"
)

// Job is a unit of queued work.
type Job struct {
	ID          int64
	Kind        JobKind
	Payload     json.RawMessage
	Attempts    int
	MaxAttempts int
	Status      JobStatus
	RunAt       time.Time
	LastError   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// AnalyzePRPayload is the payload for JobAnalyzePR.
type AnalyzePRPayload struct {
	InstallationID int64  `json:"installation_id"`
	RepoID         int64  `json:"repo_id"`
	RepoOwner      string `json:"repo_owner"`
	RepoName       string `json:"repo_name"`
	PRNumber       int    `json:"pr_number"`
	HeadSHA        string `json:"head_sha"`
	Action         string `json:"action"` // opened | synchronize | reopened
	SenderLogin    string `json:"sender_login"`
}

// AnalyzePushPayload is the payload for JobAnalyzePush.
type AnalyzePushPayload struct {
	InstallationID int64  `json:"installation_id"`
	RepoID         int64  `json:"repo_id"`
	RepoOwner      string `json:"repo_owner"`
	RepoName       string `json:"repo_name"`
	Before         string `json:"before"`
	After          string `json:"after"`
	Ref            string `json:"ref"`
	SenderLogin    string `json:"sender_login"`
}

// FeedbackPayload is the payload for JobFeedback.
type FeedbackPayload struct {
	InstallationID int64  `json:"installation_id"`
	RepoID         int64  `json:"repo_id"`
	RepoOwner      string `json:"repo_owner"`
	RepoName       string `json:"repo_name"`
	PRNumber       int    `json:"pr_number"`
}

// ClosePRIssuesPayload is the payload for JobIssueClose.
type ClosePRIssuesPayload struct {
	RepoID    int64  `json:"repo_id"`
	RepoOwner string `json:"repo_owner"`
	RepoName  string `json:"repo_name"`
	PRNumber  int    `json:"pr_number"`
}

// InlineComment is one inline review comment to post.
type InlineComment struct {
	Path      string
	Body      string
	Line      int
	Side      string // RIGHT | LEFT
	StartLine int
	StartSide string
}

// Annotation is a check-run annotation.
type Annotation struct {
	Path      string
	StartLine int
	EndLine   int
	Level     string // notice | warning | failure
	Message   string
	Title     string
}

// Reaction is a single reaction on a bot comment.
type Reaction struct {
	User    string
	Content string // +1 | -1 | eyes | rocket | heart | laugh | confused | hooray
}

// AuditEntry is one audit log row.
type AuditEntry struct {
	DeliveryID string
	Event      string
	Action     string
	RepoID     int64
	Kind       string
	Detail     map[string]any
}

// GitHubAPI is the subset of GitHub operations the bot needs. Implemented by
// internal/githubx.
type GitHubAPI interface {
	// InstallationToken returns a cached installation access token.
	InstallationToken(ctx context.Context, installationID int64) (string, error)
	// SelfLogin returns the bot's own login (for self-event filtering).
	SelfLogin() string
	// GetRawDiff returns the unified diff of a PR.
	GetRawDiff(ctx context.Context, owner, repo string, prNumber int) (string, error)
	// ListPRFiles returns the per-file breakdown of a PR diff.
	ListPRFiles(ctx context.Context, owner, repo string, prNumber int) ([]ChangedFile, error)
	// GetFile returns the content of a file at a ref.
	GetFile(ctx context.Context, owner, repo, path, ref string) (string, error)
	// GetPR returns PR metadata.
	GetPR(ctx context.Context, owner, repo string, prNumber int) (*PRInfo, error)
	// GetDefaultBranch returns the repo default branch name.
	GetDefaultBranch(ctx context.Context, owner, repo string) (string, error)
	// CreateCheckRun creates (or reuses) a check run and returns its ID.
	CreateCheckRun(ctx context.Context, owner, repo, name, headSHA, status, conclusion, title, summary string, annotations []Annotation) (int64, error)
	// UpdateCheckRun updates an existing check run.
	UpdateCheckRun(ctx context.Context, owner, repo string, checkRunID int64, status, conclusion, summary string, annotations []Annotation) error
	// CreateReview posts a summary review with optional inline comments and
	// returns the review ID.
	CreateReview(ctx context.Context, owner, repo string, prNumber int, headSHA, body string, comments []InlineComment) (int64, error)
	// CreateComment posts a single inline review comment.
	CreateComment(ctx context.Context, owner, repo string, prNumber int, headSHA string, c InlineComment) (int64, error)
	// CreateIssue creates an issue and returns its number.
	CreateIssue(ctx context.Context, owner, repo, title, body string, labels []string) (int, error)
	// EditIssue updates an issue body and/or state.
	EditIssue(ctx context.Context, owner, repo string, number int, body *string, state *string) error
	// AddIssueComment comments on an issue.
	AddIssueComment(ctx context.Context, owner, repo string, number int, body string) error
	// GetReactions lists reactions on a review comment.
	GetReactions(ctx context.Context, owner, repo string, commentID int64) ([]Reaction, error)
	// GetPushDiff returns the diff of a push between two SHAs (before may be
	// all-zeroes for branch creation).
	GetPushDiff(ctx context.Context, owner, repo, before, after string) ([]ChangedFile, error)
	// ListInstallationRepos lists repos accessible to an installation.
	ListInstallationRepos(ctx context.Context, installationID int64) ([]Repo, error)
}

// Store is the persistence layer. Implemented by internal/store.
type Store interface {
	Ping(ctx context.Context) error
	// RecordDelivery returns false if the delivery was already recorded.
	RecordDelivery(ctx context.Context, deliveryID, event string) (bool, error)
	UpsertInstallation(ctx context.Context, inst Installation) error
	UpsertRepo(ctx context.Context, r Repo) error
	GetRepo(ctx context.Context, repoID int64) (*Repo, error)
	GetRepoByName(ctx context.Context, owner, name string) (*Repo, error)
	ListReposForInstallation(ctx context.Context, installationID int64) ([]Repo, error)
	SetRepoConfig(ctx context.Context, repoID int64, cfg *RepoConfig) error
	GetPRState(ctx context.Context, repoID int64, number int) (*PRState, error)
	SetPRState(ctx context.Context, repoID int64, number int, headSHA string, reviewID, checkRunID *int64) error
	// CreateRun inserts a new analysis run; returns ErrDuplicateRun if
	// (repo_id, kind, input_sha) already exists.
	CreateRun(ctx context.Context, repoID int64, kind, inputSHA string, prNumber *int) (int64, error)
	CompleteRun(ctx context.Context, runID int64, result *ReviewResult) error
	FailRun(ctx context.Context, runID int64, errMsg string) error
	SaveFindings(ctx context.Context, runID int64, findings []Finding, hashFn func(Finding) string) error
	FindingsForRun(ctx context.Context, runID int64) ([]FindingRecord, error)
	CreateIssue(ctx context.Context, repoID int64, number int, title, kind string, prNumber *int, findingIDs []string) error
	CloseIssue(ctx context.Context, repoID int64, number int) error
	OpenIssueForRepo(ctx context.Context, repoID int64, kind string) (*IssueRecord, error)
	IssuesForPR(ctx context.Context, repoID int64, prNumber int) ([]IssueRecord, error)
	SetFindingComment(ctx context.Context, findingID int64, commentID int64) error
	SetFindingIssue(ctx context.Context, findingID int64, issueNumber int) error
	// UpsertLearning adjusts the weight of a learning key by delta.
	UpsertLearning(ctx context.Context, repoID int64, key, signal string, delta int) error
	// SuppressedKeys returns learning keys that suppress a finding.
	SuppressedKeys(ctx context.Context, repoID int64) (map[string]bool, error)
	// LearningKeysForComments returns dedupe keys for the bot's comments on a
	// PR, used by the feedback sweep. commentID -> key.
	LearningKeysForComments(ctx context.Context, repoID int64, prNumber int) (map[int64]string, error)
	Audit(ctx context.Context, e AuditEntry) error
}

// Queue is the job queue. Implemented by internal/queue.
type Queue interface {
	Enqueue(ctx context.Context, kind JobKind, payload any) error
	// Dequeue claims the next runnable job, if any.
	Dequeue(ctx context.Context) (*Job, bool, error)
	Complete(ctx context.Context, jobID int64) error
	// Fail marks a job failed; if retries remain it is rescheduled with
	// exponential backoff.
	Fail(ctx context.Context, jobID int64, errMsg string) error
	// ReapExpired resets running jobs whose lease expired.
	ReapExpired(ctx context.Context, leaseTTL time.Duration) (int64, error)
}
