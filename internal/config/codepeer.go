package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
)

type rawRepoConfig struct {
	Enabled            *bool    `yaml:"enabled"`
	Mode               *string  `yaml:"mode"`
	Strictness         *string  `yaml:"strictness"`
	IgnorePaths        []string `yaml:"ignore_paths"`
	IgnoreUsernames    []string `yaml:"ignore_usernames"`
	SkipTitleKeywords  []string `yaml:"skip_title_keywords"`
	BaseBranches       []string `yaml:"base_branches"`
	MaxFindings        *int     `yaml:"max_findings"`
	PerFileCap         *int     `yaml:"per_file_cap"`
	IncludeNits        *bool    `yaml:"include_nits"`
	CustomInstructions []string `yaml:"custom_instructions"`
	InstructionFiles   []string `yaml:"instruction_files"`
}

// Parse decodes .codepeer.yml content into a RepoConfig, overlaying set
// fields on top of the defaults. Unknown fields and invalid values error.
func Parse(data []byte) (domain.RepoConfig, error) {
	cfg := domain.DefaultRepoConfig()

	var raw rawRepoConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		return domain.RepoConfig{}, fmt.Errorf("parse .codepeer.yml: %w", err)
	}

	if raw.Enabled != nil {
		cfg.Enabled = *raw.Enabled
	}
	if raw.Mode != nil {
		switch *raw.Mode {
		case "pr", "push", "both":
			cfg.Mode = *raw.Mode
		default:
			return domain.RepoConfig{}, fmt.Errorf("invalid mode %q: must be one of pr, push, both", *raw.Mode)
		}
	}
	if raw.Strictness != nil {
		switch *raw.Strictness {
		case "lenient", "balanced", "strict":
			cfg.Strictness = *raw.Strictness
		default:
			return domain.RepoConfig{}, fmt.Errorf("invalid strictness %q: must be one of lenient, balanced, strict", *raw.Strictness)
		}
	}
	if raw.MaxFindings != nil {
		if *raw.MaxFindings < 1 || *raw.MaxFindings > 50 {
			return domain.RepoConfig{}, fmt.Errorf("max_findings must be between 1 and 50, got %d", *raw.MaxFindings)
		}
		cfg.MaxFindings = *raw.MaxFindings
	}
	if raw.PerFileCap != nil {
		if *raw.PerFileCap < 1 || *raw.PerFileCap > 10 {
			return domain.RepoConfig{}, fmt.Errorf("per_file_cap must be between 1 and 10, got %d", *raw.PerFileCap)
		}
		cfg.PerFileCap = *raw.PerFileCap
	}
	if raw.IncludeNits != nil {
		cfg.IncludeNits = *raw.IncludeNits
	}
	if raw.IgnorePaths != nil {
		cfg.IgnorePaths = raw.IgnorePaths
	}
	if raw.IgnoreUsernames != nil {
		cfg.IgnoreUsernames = raw.IgnoreUsernames
	}
	if raw.SkipTitleKeywords != nil {
		cfg.SkipTitleKeywords = raw.SkipTitleKeywords
	}
	if raw.BaseBranches != nil {
		cfg.BaseBranches = raw.BaseBranches
	}
	if raw.CustomInstructions != nil {
		cfg.CustomInstructions = raw.CustomInstructions
	}
	if raw.InstructionFiles != nil {
		cfg.InstructionFiles = raw.InstructionFiles
	}

	return cfg, nil
}

type cacheEntry struct {
	cfg     domain.RepoConfig
	expires time.Time
}

// RepoConfigCache caches parsed .codepeer.yml content per repository.
type RepoConfigCache struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[string]cacheEntry
}

// NewRepoConfigCache returns a cache that expires entries after ttl. A ttl
// <= 0 falls back to 5 minutes.
func NewRepoConfigCache(ttl time.Duration) *RepoConfigCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &RepoConfigCache{ttl: ttl, items: make(map[string]cacheEntry)}
}

// Get returns the parsed config for a repository, fetching .codepeer.yml via
// the callback when the cache entry is missing or expired. A missing file or
// invalid YAML yields the defaults; only fetch errors are returned.
func (c *RepoConfigCache) Get(ctx context.Context, owner, repo, ref string, fetch func(ctx context.Context, path, ref string) (string, error)) (*domain.RepoConfig, error) {
	key := owner + "/" + repo

	c.mu.Lock()
	if e, ok := c.items[key]; ok && time.Now().Before(e.expires) {
		c.mu.Unlock()
		cfg := e.cfg
		return &cfg, nil
	}
	c.mu.Unlock()

	content, err := fetch(ctx, ".codepeer.yml", ref)
	if err != nil {
		return nil, err
	}

	cfg := domain.DefaultRepoConfig()
	if content != "" {
		parsed, err := Parse([]byte(content))
		if err != nil {
			slog.Warn("invalid .codepeer.yml; using defaults", "repo", key, "error", err)
		} else {
			cfg = parsed
		}
	}

	c.mu.Lock()
	c.items[key] = cacheEntry{cfg: cfg, expires: time.Now().Add(c.ttl)}
	c.mu.Unlock()

	return &cfg, nil
}

var defaultIgnoredPatterns = []string{
	"**/*.lock",
	"**/package-lock.json",
	"**/yarn.lock",
	"**/pnpm-lock.yaml",
	"**/*.min.js",
	"**/*.min.css",
	"**/*.map",
	"**/*.pb.go",
	"**/vendor/**",
	"**/node_modules/**",
	"**/dist/**",
	"**/__generated__/**",
	"**/*.generated.*",
	"**/*.png",
	"**/*.jpg",
	"**/*.jpeg",
	"**/*.gif",
	"**/*.svg",
	"**/*.ico",
	"**/*.woff*",
	"**/*.ttf",
	"**/*.eot",
	"**/*.zip",
	"**/*.tar*",
	"**/*.gz",
	"**/*.exe",
	"**/*.dll",
	"**/*.so",
	"**/*.dylib",
	"**/*.class",
	"**/*.jar",
	"**/*.bin",
	"**/*.pdf",
}

// PathIgnored reports whether a changed file path should be skipped, using
// both the built-in patterns and the repo's ignore_paths.
func PathIgnored(cfg *domain.RepoConfig, path string) bool {
	if matchAny(defaultIgnoredPatterns, path) {
		return true
	}
	if cfg != nil {
		return matchAny(cfg.IgnorePaths, path)
	}
	return false
}

func matchAny(patterns []string, path string) bool {
	for _, p := range patterns {
		if ok, err := doublestar.Match(p, path); err == nil && ok {
			return true
		}
	}
	return false
}

var defaultIgnoredUsers = []string{
	"dependabot[bot]",
	"renovate[bot]",
	"dependabot-preview[bot]",
	"github-actions[bot]",
	"imgbot[bot]",
}

// UserIgnored reports whether a PR author login should be skipped, using
// both the built-in bot list and the repo's ignore_usernames.
func UserIgnored(cfg *domain.RepoConfig, login string) bool {
	for _, u := range defaultIgnoredUsers {
		if login == u {
			return true
		}
	}
	if cfg != nil {
		for _, u := range cfg.IgnoreUsernames {
			if login == u {
				return true
			}
		}
	}
	return false
}

var defaultSkipKeywords = []string{
	"WIP",
	"DRAFT",
	"[skip review]",
	"[skip ci-review]",
	"do not review",
}

// TitleSkipped reports whether a PR title matches a skip keyword, using both
// the built-in keywords and the repo's skip_title_keywords. Matching is
// case-insensitive substring matching.
func TitleSkipped(cfg *domain.RepoConfig, title string) bool {
	lower := strings.ToLower(title)
	for _, k := range defaultSkipKeywords {
		if strings.Contains(lower, strings.ToLower(k)) {
			return true
		}
	}
	if cfg != nil {
		for _, k := range cfg.SkipTitleKeywords {
			if strings.Contains(lower, strings.ToLower(k)) {
				return true
			}
		}
	}
	return false
}

// BaseBranchAllowed reports whether a PR base ref should be analyzed. An
// empty BaseBranches allows all refs.
func BaseBranchAllowed(cfg *domain.RepoConfig, baseRef string) bool {
	if cfg == nil || len(cfg.BaseBranches) == 0 {
		return true
	}
	for _, b := range cfg.BaseBranches {
		if baseRef == b {
			return true
		}
	}
	return false
}

// ModeAllows reports whether the repo config enables analysis for the given
// mode ("pr" or "push").
func ModeAllows(cfg *domain.RepoConfig, mode string) bool {
	if cfg == nil {
		return mode == "pr"
	}
	return cfg.Mode == mode || cfg.Mode == "both"
}

// EffectiveReviewConfig maps the repo config onto the review controls used
// for a single analysis pass.
func EffectiveReviewConfig(cfg *domain.RepoConfig) domain.ReviewConfig {
	if cfg == nil {
		d := domain.DefaultRepoConfig()
		cfg = &d
	}
	return domain.ReviewConfig{
		Strictness:         cfg.Strictness,
		MaxFindings:        cfg.MaxFindings,
		PerFileCap:         cfg.PerFileCap,
		IncludeNits:        cfg.IncludeNits,
		CustomInstructions: cfg.CustomInstructions,
	}
}
