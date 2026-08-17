package analysis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/store"
)

// ReviewOutput is the result of one analysis pass, consumed by posting.
type ReviewOutput struct {
	Kind         string
	RepoOwner    string
	RepoName     string
	RepoID       int64
	PRNumber     int
	HeadSHA      string
	Result       domain.ReviewResult
	Findings     []domain.FindingRecord
	RunID        int64
	Skipped      string
	SkippedFiles []string
	// PartialFiles lists changed files that could not be analyzed.
	PartialFiles []string
}

// Pipeline orchestrates one analysis pass: context, chunking, LLM review,
// merge, dedupe, validation, caps, persistence.
type Pipeline struct {
	Reviewer           domain.Reviewer
	GitHub             domain.GitHubAPI
	Store              domain.Store
	Strictness         string
	MaxFindings        int
	PerFileCap         int
	IncludeNits        bool
	CustomInstructions []string
	// Agents selects the specialist review agents. nil = use the repo
	// config (default all); empty = legacy single general pass.
	Agents []string
}

// NewPipeline builds a pipeline with balanced defaults.
func NewPipeline(r domain.Reviewer, gh domain.GitHubAPI, st domain.Store) *Pipeline {
	return &Pipeline{
		Reviewer:    r,
		GitHub:      gh,
		Store:       st,
		Strictness:  "balanced",
		MaxFindings: 10,
		PerFileCap:  3,
	}
}

// AnalyzePR runs the full PR pipeline.
func (p *Pipeline) AnalyzePR(ctx context.Context, payload domain.AnalyzePRPayload) (*ReviewOutput, error) {
	skip := func(reason string, extra []string) *ReviewOutput {
		return &ReviewOutput{
			Kind:         "pr",
			RepoOwner:    payload.RepoOwner,
			RepoName:     payload.RepoName,
			RepoID:       payload.RepoID,
			PRNumber:     payload.PRNumber,
			HeadSHA:      payload.HeadSHA,
			Skipped:      reason,
			SkippedFiles: extra,
		}
	}

	repo, err := p.Store.GetRepo(ctx, payload.RepoID)
	if err != nil {
		return nil, err
	}
	if repo == nil || !repo.Enabled {
		return skip("repo disabled", nil), nil
	}
	cfg := repo.Config
	if cfg == nil {
		d := domain.DefaultRepoConfig()
		cfg = &d
	}
	if !modeAllows(cfg, "pr") {
		return skip("pr mode disabled", nil), nil
	}
	if userIgnored(cfg, payload.SenderLogin) {
		return skip("user ignored", nil), nil
	}

	pr, err := p.GitHub.GetPR(ctx, payload.RepoOwner, payload.RepoName, payload.PRNumber)
	if err != nil {
		return nil, err
	}
	if pr.Draft {
		return skip("draft PR", nil), nil
	}
	if titleSkipped(cfg, pr.Title) {
		return skip("title keyword", nil), nil
	}
	if !baseBranchAllowed(cfg, pr.BaseRef) {
		return skip("base branch not allowed", nil), nil
	}

	rawDiff, err := p.GitHub.GetRawDiff(ctx, payload.RepoOwner, payload.RepoName, payload.PRNumber)
	if err != nil {
		return nil, err
	}
	files, err := p.GitHub.ListPRFiles(ctx, payload.RepoOwner, payload.RepoName, payload.PRNumber)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return skip("no files changed", nil), nil
	}
	kept, skipped := FilterFiles(files, cfg.IgnorePaths)
	if len(kept) == 0 {
		paths := make([]string, 0, len(skipped))
		for _, f := range skipped {
			paths = append(paths, f.Path)
		}
		return skip("all files ignored", paths), nil
	}

	runID, err := p.Store.CreateRun(ctx, repo.ID, "pr", payload.HeadSHA, &payload.PRNumber)
	if err != nil {
		if !errors.Is(err, store.ErrDuplicateRun) {
			return nil, err
		}
		runID, err = p.recoverRun(ctx, repo.ID, "pr", payload.HeadSHA)
		if err != nil {
			return nil, err
		}
		if runID == 0 {
			return skip("already analyzed", nil), nil
		}
	}

	diffByFile, _ := SplitDiffByFile(rawDiff)
	ctxFiles := BuildContext(ctx, p.GitHub, payload.RepoOwner, payload.RepoName, payload.HeadSHA, kept)
	instructions := BuildInstructions(ctx, p.GitHub, payload.RepoOwner, payload.RepoName, payload.HeadSHA, cfg.InstructionFiles)

	out, err := p.runAgents(ctx, domain.ReviewRequest{
		RepoOwner:    payload.RepoOwner,
		RepoName:     payload.RepoName,
		PRNumber:     payload.PRNumber,
		PRTitle:      pr.Title,
		PRBody:       pr.Body,
		HeadSHA:      payload.HeadSHA,
		Diff:         rawDiff,
		Files:        kept,
		Context:      ctxFiles,
		Instructions: instructions,
		Config:       p.reviewConfig(cfg),
	}, diffByFile, p.selectAgents(cfg))
	if err != nil {
		_ = p.Store.FailRun(ctx, runID, err.Error())
		return nil, err
	}

	result, partialFiles, err := p.finish(ctx, repo, runID, out, diffByFile, ctxFiles)
	if err != nil {
		return nil, err
	}
	return &ReviewOutput{
		Kind:         "pr",
		RepoOwner:    payload.RepoOwner,
		RepoName:     payload.RepoName,
		RepoID:       repo.ID,
		PRNumber:     payload.PRNumber,
		HeadSHA:      payload.HeadSHA,
		Result:       result,
		Findings:     out.findings,
		RunID:        runID,
		PartialFiles: partialFiles,
	}, nil
}

// AnalyzePush runs the full push pipeline (rolling-issue mode).
func (p *Pipeline) AnalyzePush(ctx context.Context, payload domain.AnalyzePushPayload) (*ReviewOutput, error) {
	skip := func(reason string, extra []string) *ReviewOutput {
		return &ReviewOutput{
			Kind:         "push",
			RepoOwner:    payload.RepoOwner,
			RepoName:     payload.RepoName,
			RepoID:       payload.RepoID,
			HeadSHA:      payload.After,
			Skipped:      reason,
			SkippedFiles: extra,
		}
	}

	repo, err := p.Store.GetRepo(ctx, payload.RepoID)
	if err != nil {
		return nil, err
	}
	if repo == nil || !repo.Enabled {
		return skip("repo disabled", nil), nil
	}
	cfg := repo.Config
	if cfg == nil {
		d := domain.DefaultRepoConfig()
		cfg = &d
	}
	if !modeAllows(cfg, "push") {
		return skip("push mode disabled", nil), nil
	}
	if payload.Before == payload.After {
		return skip("no new commits", nil), nil
	}
	defaultBranch := repo.DefaultBranch
	if defaultBranch == "" {
		defaultBranch, err = p.GitHub.GetDefaultBranch(ctx, payload.RepoOwner, payload.RepoName)
		if err != nil {
			return nil, err
		}
	}
	if payload.Ref != "refs/heads/"+defaultBranch {
		return skip("not default branch", nil), nil
	}
	if userIgnored(cfg, payload.SenderLogin) {
		return skip("user ignored", nil), nil
	}

	files, err := p.GitHub.GetPushDiff(ctx, payload.RepoOwner, payload.RepoName, payload.Before, payload.After)
	if err != nil {
		return nil, err
	}
	kept, skipped := FilterFiles(files, cfg.IgnorePaths)
	if len(kept) == 0 {
		paths := make([]string, 0, len(skipped))
		for _, f := range skipped {
			paths = append(paths, f.Path)
		}
		return skip("all files ignored", paths), nil
	}

	runID, err := p.Store.CreateRun(ctx, repo.ID, "push", payload.After, nil)
	if err != nil {
		if !errors.Is(err, store.ErrDuplicateRun) {
			return nil, err
		}
		runID, err = p.recoverRun(ctx, repo.ID, "push", payload.After)
		if err != nil {
			return nil, err
		}
		if runID == 0 {
			return skip("already analyzed", nil), nil
		}
	}

	patchMap := map[string]string{}
	for _, f := range kept {
		patchMap[f.Path] = f.Patch
	}
	ctxFiles := BuildContext(ctx, p.GitHub, payload.RepoOwner, payload.RepoName, payload.After, kept)
	instructions := BuildInstructions(ctx, p.GitHub, payload.RepoOwner, payload.RepoName, payload.After, cfg.InstructionFiles)

	out, err := p.runAgents(ctx, domain.ReviewRequest{
		RepoOwner:    payload.RepoOwner,
		RepoName:     payload.RepoName,
		HeadSHA:      payload.After,
		Files:        kept,
		Context:      ctxFiles,
		Instructions: instructions,
		Config:       p.reviewConfig(cfg),
	}, patchMap, p.selectAgents(cfg))
	if err != nil {
		_ = p.Store.FailRun(ctx, runID, err.Error())
		return nil, err
	}

	result, partialFiles, err := p.finish(ctx, repo, runID, out, patchMap, ctxFiles)
	if err != nil {
		return nil, err
	}
	return &ReviewOutput{
		Kind:         "push",
		RepoOwner:    payload.RepoOwner,
		RepoName:     payload.RepoName,
		RepoID:       repo.ID,
		HeadSHA:      payload.After,
		Result:       result,
		Findings:     out.findings,
		RunID:        runID,
		PartialFiles: partialFiles,
	}, nil
}

type chunkOutcome struct {
	results     []domain.ReviewResult
	failed      int
	failedFiles []string
	findings    []domain.FindingRecord
	compiled    *domain.ReviewResult
}

// recoverRun resolves a duplicate run key: terminal runs return 0 (already
// analyzed), stalled runs are restarted and reused.
func (p *Pipeline) recoverRun(ctx context.Context, repoID int64, kind, sha string) (int64, error) {
	existing, err := p.Store.RunByKey(ctx, repoID, kind, sha)
	if err != nil {
		return 0, err
	}
	if existing == nil {
		return 0, nil
	}
	switch existing.Status {
	case "done", "failed":
		return 0, nil
	default:
		slog.Warn("restarting stalled analysis run", "run", existing.ID, "kind", kind, "sha", sha)
		if err := p.Store.RestartRun(ctx, existing.ID); err != nil {
			return 0, err
		}
		return existing.ID, nil
	}
}

type chunkReview struct {
	files  []domain.ChangedFile
	diff   string
	agent  string
	err    error
	result domain.ReviewResult
}

// runAgents runs the specialist review agents over the diff in parallel and
// compiles their findings. An empty agents list falls back to the legacy
// single general pass.
func (p *Pipeline) runAgents(ctx context.Context, base domain.ReviewRequest, diffByFile map[string]string, agents []string) (*chunkOutcome, error) {
	specs := agentSpecs(agents)
	if len(specs) == 0 {
		return p.runChunks(ctx, base, diffByFile)
	}

	chunks := ChunkFiles(base.Files, diffByFile)
	type job struct {
		spec agentSpec
		ch   Chunk
	}
	jobs := make([]job, 0, len(specs)*len(chunks))
	for _, s := range specs {
		for _, ch := range chunks {
			jobs = append(jobs, job{spec: s, ch: ch})
		}
	}

	sem := make(chan struct{}, 6)
	var mu sync.Mutex
	out := &chunkOutcome{}
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	reviews := make([]chunkReview, len(jobs))

	for i, j := range jobs {
		wg.Add(1)
		go func(idx int, j job) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			req := base
			req.Diff = j.ch.DiffText
			req.Files = j.ch.Files
			req.Focus = j.spec.Focus
			res, err := p.Reviewer.Review(ctx, req)
			if err != nil {
				slog.Warn("agent review failed, retrying once",
					"agent", j.spec.Name, "files", filePaths(j.ch.Files), "err", err)
				time.Sleep(2 * time.Second)
				res, err = p.Reviewer.Review(ctx, req)
			}
			if err == nil {
				for k := range res.Findings {
					if j.spec.Category != "" {
						res.Findings[k].Category = j.spec.Category
					}
				}
			}
			mu.Lock()
			reviews[idx] = chunkReview{files: j.ch.Files, diff: j.ch.DiffText, agent: j.spec.Name, err: err, result: res}
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				out.failed++
				out.failedFiles = append(out.failedFiles, filePaths(j.ch.Files)...)
			} else {
				out.results = append(out.results, res)
			}
			mu.Unlock()
		}(i, j)
	}
	wg.Wait()

	if len(out.results) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return out, nil
	}
	if firstErr != nil {
		slog.Warn("some agent reviews failed after retry",
			"failed", out.failed, "succeeded", len(out.results), "failed_files", out.failedFiles)
	}

	candidates := make([]domain.Finding, 0)
	for _, r := range out.results {
		candidates = append(candidates, r.Findings...)
	}
	candidates = dedupeFindings(candidates)
	compiled, err := p.Reviewer.Compile(ctx, domain.CompileInput{
		RepoOwner:  base.RepoOwner,
		RepoName:   base.RepoName,
		PRNumber:   base.PRNumber,
		PRTitle:    base.PRTitle,
		PRBody:     base.PRBody,
		HeadSHA:    base.HeadSHA,
		Files:      base.Files,
		Candidates: candidates,
		Config:     base.Config,
	})
	if err != nil {
		slog.Warn("lead-agent compile failed, falling back to deterministic merge", "err", err)
		return out, nil
	}
	slog.Info("multi-agent review compiled", "agents", len(specs), "candidates", len(candidates), "findings", len(compiled.Findings))
	out.compiled = &compiled
	return out, nil
}

func (p *Pipeline) runChunks(ctx context.Context, base domain.ReviewRequest, diffByFile map[string]string) (*chunkOutcome, error) {
	chunks := ChunkFiles(base.Files, diffByFile)
	sem := make(chan struct{}, 4)
	var mu sync.Mutex
	out := &chunkOutcome{}
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex

	reviews := make([]chunkReview, len(chunks))
	for i, ch := range chunks {
		wg.Add(1)
		go func(idx int, ch Chunk) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			req := base
			req.Diff = ch.DiffText
			req.Files = ch.Files
			res, err := p.Reviewer.Review(ctx, req)
			if err != nil {
				slog.Warn("review chunk failed, retrying once",
					"files", filePaths(ch.Files), "err", err)
				time.Sleep(2 * time.Second)
				res, err = p.Reviewer.Review(ctx, req)
			}
			mu.Lock()
			reviews[idx] = chunkReview{files: ch.Files, diff: ch.DiffText, err: err, result: res}
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				out.failed++
				out.failedFiles = append(out.failedFiles, filePaths(ch.Files)...)
			} else {
				out.results = append(out.results, res)
			}
			mu.Unlock()
		}(i, ch)
	}
	wg.Wait()

	if len(out.results) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return out, nil
	}
	if firstErr != nil {
		slog.Warn("some review chunks failed after retry",
			"failed", out.failed, "succeeded", len(out.results), "failed_files", out.failedFiles)
	}
	return out, nil
}

func filePaths(files []domain.ChangedFile) []string {
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	return paths
}

// selectAgents resolves the specialist agent list for a run.
func (p *Pipeline) selectAgents(cfg *domain.RepoConfig) []string {
	if p.Agents != nil {
		return p.Agents
	}
	if cfg.Agents != nil {
		return cfg.Agents
	}
	return domain.DefaultAgents
}

func (p *Pipeline) reviewConfig(cfg *domain.RepoConfig) domain.ReviewConfig {
	strictness := p.Strictness
	if strictness == "" {
		strictness = cfg.Strictness
	}
	inst := append([]string{}, cfg.CustomInstructions...)
	inst = append(inst, p.CustomInstructions...)
	return domain.ReviewConfig{
		Strictness:         strictness,
		MaxFindings:        p.MaxFindings,
		PerFileCap:         p.PerFileCap,
		IncludeNits:        p.IncludeNits || cfg.IncludeNits,
		CustomInstructions: inst,
	}
}

// finish merges, dedupes, validates, suppresses, caps, persists. It returns
// the merged result and the list of files that failed analysis.
func (p *Pipeline) finish(ctx context.Context, repo *domain.Repo, runID int64, out *chunkOutcome, diffByFile, ctxFiles map[string]string) (domain.ReviewResult, []string, error) {
	var result domain.ReviewResult
	if out.compiled != nil {
		result = *out.compiled
	} else {
		summary := []string{}
		allNoFindings := true
		for _, r := range out.results {
			if r.Summary != "" {
				summary = append(summary, r.Summary)
			}
			if r.Status != domain.StatusNoFindings {
				allNoFindings = false
			}
			result.Findings = append(result.Findings, r.Findings...)
		}
		result.Summary = strings.Join(summary, " ")
		switch {
		case len(result.Findings) > 0:
			result.Status = domain.StatusChangesRequested
		case allNoFindings && out.failed == 0:
			result.Status = domain.StatusNoFindings
		default:
			result.Status = domain.StatusApproved
		}
	}

	partial := dedupeStrings(out.failedFiles)
	if len(partial) > 0 {
		warning := fmt.Sprintf("Partial review warning: %d changed file(s) could not be analyzed (analysis errors): %s.",
			len(partial), strings.Join(partial, ", "))
		if result.Summary == "" {
			result.Summary = warning
		} else {
			result.Summary += "\n\n" + warning
		}
	}

	keptPaths := map[string]bool{}
	for path := range diffByFile {
		keptPaths[path] = true
	}

	result.Findings = dedupeFindings(result.Findings)
	validated := make([]domain.Finding, 0, len(result.Findings))
	for _, f := range result.Findings {
		if f.Title == "" || f.Body == "" {
			continue
		}
		if !keptPaths[f.File] {
			continue
		}
		if !validSeverity(f.Severity) {
			f.Severity = domain.SeverityMedium
		}
		if !validCategory(f.Category) {
			f.Category = domain.CategoryOther
		}
		if f.Line > 0 {
			if patch, ok := diffByFile[f.File]; ok {
				if hunks, err := ParseHunks(patch); err == nil && len(hunks[f.File]) > 0 {
					if !NewLineInDiff(hunks[f.File], f.Line) {
						continue
					}
				}
			}
		}
		if f.Suggestion != nil {
			old := strings.TrimSpace(f.Suggestion.Old)
			if old == "" || !strings.Contains(diffByFile[f.File], old) && !strings.Contains(ctxFiles[f.File], old) {
				f.Suggestion = nil
			}
		}
		validated = append(validated, f)
	}
	result.Findings = validated

	suppressed, err := p.Store.SuppressedKeys(ctx, repo.ID)
	if err == nil && len(suppressed) > 0 {
		kept := result.Findings[:0]
		for _, f := range result.Findings {
			if suppressed[DedupeHash(f)] {
				continue
			}
			kept = append(kept, f)
		}
		result.Findings = kept
	}

	result.Findings = applyCaps(result.Findings, p.PerFileCap, p.MaxFindings, p.IncludeNits)

	if err := p.Store.CompleteRun(ctx, runID, &result); err != nil {
		return result, partial, err
	}
	if err := p.Store.SaveFindings(ctx, runID, result.Findings, DedupeHash); err != nil {
		return result, partial, err
	}
	records, err := p.Store.FindingsForRun(ctx, runID)
	if err != nil {
		return result, partial, err
	}
	out.findings = records
	return result, partial, nil
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// SeverityRank orders severities.
func SeverityRank(s domain.Severity) int {
	switch s {
	case domain.SeverityCritical:
		return 5
	case domain.SeverityHigh:
		return 4
	case domain.SeverityMedium:
		return 3
	case domain.SeverityLow:
		return 2
	case domain.SeverityNit:
		return 1
	default:
		return 0
	}
}

// DedupeHash is the stable identity of a finding.
func DedupeHash(f domain.Finding) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s", f.File, f.Line, normalizeTitle(f.Title))))
	return hex.EncodeToString(sum[:8])
}

func normalizeTitle(s string) string {
	var b strings.Builder
	lastSpace := true
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func dedupeFindings(fs []domain.Finding) []domain.Finding {
	seen := map[string]int{}
	out := make([]domain.Finding, 0, len(fs))
	for _, f := range fs {
		key := fmt.Sprintf("%s|%d|%s", f.File, f.Line, normalizeTitle(f.Title))
		if idx, ok := seen[key]; ok {
			if SeverityRank(f.Severity) > SeverityRank(out[idx].Severity) {
				out[idx] = f
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, f)
	}
	return out
}

func applyCaps(fs []domain.Finding, perFileCap, maxFindings int, includeNits bool) []domain.Finding {
	sort.SliceStable(fs, func(i, j int) bool {
		if SeverityRank(fs[i].Severity) != SeverityRank(fs[j].Severity) {
			return SeverityRank(fs[i].Severity) > SeverityRank(fs[j].Severity)
		}
		if fs[i].File != fs[j].File {
			return fs[i].File < fs[j].File
		}
		return fs[i].Line < fs[j].Line
	})

	if !includeNits {
		kept := fs[:0]
		for _, f := range fs {
			if f.Severity == domain.SeverityNit {
				continue
			}
			kept = append(kept, f)
		}
		fs = kept
	}

	if perFileCap > 0 {
		counts := map[string]int{}
		kept := fs[:0]
		for _, f := range fs {
			if counts[f.File] >= perFileCap {
				continue
			}
			counts[f.File]++
			kept = append(kept, f)
		}
		fs = kept
	}

	if maxFindings > 0 && len(fs) > maxFindings {
		fs = fs[:maxFindings]
	}
	return fs
}

func validSeverity(s domain.Severity) bool {
	switch s {
	case domain.SeverityCritical, domain.SeverityHigh, domain.SeverityMedium, domain.SeverityLow, domain.SeverityNit:
		return true
	}
	return false
}

func validCategory(c domain.Category) bool {
	switch c {
	case domain.CategoryBug, domain.CategorySecurity, domain.CategoryPerformance, domain.CategoryCorrectness,
		domain.CategoryTest, domain.CategoryMaintainability, domain.CategoryStyle, domain.CategoryOther:
		return true
	}
	return false
}

func modeAllows(cfg *domain.RepoConfig, mode string) bool {
	return cfg.Mode == mode || cfg.Mode == "both"
}

func userIgnored(cfg *domain.RepoConfig, login string) bool {
	if login == "" {
		return false
	}
	for _, u := range cfg.IgnoreUsernames {
		if u == login {
			return true
		}
	}
	switch login {
	case "dependabot[bot]", "renovate[bot]", "dependabot-preview[bot]", "github-actions[bot]", "imgbot[bot]":
		return true
	}
	return false
}

func titleSkipped(cfg *domain.RepoConfig, title string) bool {
	low := strings.ToLower(title)
	keywords := append([]string{}, cfg.SkipTitleKeywords...)
	keywords = append(keywords, "wip", "draft", "[skip review]", "[skip ci-review]", "do not review")
	for _, k := range keywords {
		if k != "" && strings.Contains(low, strings.ToLower(k)) {
			return true
		}
	}
	return false
}

func baseBranchAllowed(cfg *domain.RepoConfig, baseRef string) bool {
	if len(cfg.BaseBranches) == 0 {
		return true
	}
	for _, b := range cfg.BaseBranches {
		if b == baseRef {
			return true
		}
	}
	return false
}
