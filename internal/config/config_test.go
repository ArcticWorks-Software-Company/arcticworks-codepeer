package config

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
)

func TestParseFull(t *testing.T) {
	data := []byte(`
enabled: false
mode: push
strictness: strict
ignore_paths: [vendor/**]
ignore_usernames: [alice]
skip_title_keywords: [wip]
base_branches: [main, develop]
max_findings: 25
per_file_cap: 5
include_nits: true
custom_instructions: [be nice]
instruction_files: [docs/standards.md]
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Enabled {
		t.Error("enabled = true, want false")
	}
	if cfg.Mode != "push" {
		t.Errorf("mode = %q, want push", cfg.Mode)
	}
	if cfg.Strictness != "strict" {
		t.Errorf("strictness = %q, want strict", cfg.Strictness)
	}
	if !reflect.DeepEqual(cfg.IgnorePaths, []string{"vendor/**"}) {
		t.Errorf("ignore_paths = %v", cfg.IgnorePaths)
	}
	if !reflect.DeepEqual(cfg.IgnoreUsernames, []string{"alice"}) {
		t.Errorf("ignore_usernames = %v", cfg.IgnoreUsernames)
	}
	if !reflect.DeepEqual(cfg.SkipTitleKeywords, []string{"wip"}) {
		t.Errorf("skip_title_keywords = %v", cfg.SkipTitleKeywords)
	}
	if !reflect.DeepEqual(cfg.BaseBranches, []string{"main", "develop"}) {
		t.Errorf("base_branches = %v", cfg.BaseBranches)
	}
	if cfg.MaxFindings != 25 {
		t.Errorf("max_findings = %d, want 25", cfg.MaxFindings)
	}
	if cfg.PerFileCap != 5 {
		t.Errorf("per_file_cap = %d, want 5", cfg.PerFileCap)
	}
	if !cfg.IncludeNits {
		t.Error("include_nits = false, want true")
	}
	if !reflect.DeepEqual(cfg.CustomInstructions, []string{"be nice"}) {
		t.Errorf("custom_instructions = %v", cfg.CustomInstructions)
	}
	if !reflect.DeepEqual(cfg.InstructionFiles, []string{"docs/standards.md"}) {
		t.Errorf("instruction_files = %v", cfg.InstructionFiles)
	}
}

func TestParseEmpty(t *testing.T) {
	for _, data := range [][]byte{nil, {}, []byte("")} {
		cfg, err := Parse(data)
		if err != nil {
			t.Fatalf("Parse(%q): %v", data, err)
		}
		if !reflect.DeepEqual(cfg, domain.DefaultRepoConfig()) {
			t.Errorf("Parse(%q) = %+v, want defaults", data, cfg)
		}
	}
}

func TestParseUnknownField(t *testing.T) {
	if _, err := Parse([]byte("bogus_field: true\n")); err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
}

func TestParseInvalidMode(t *testing.T) {
	if _, err := Parse([]byte("mode: aggressive\n")); err == nil {
		t.Fatal("expected error for invalid mode, got nil")
	}
}

func TestParseInvalidStrictness(t *testing.T) {
	if _, err := Parse([]byte("strictness: hostile\n")); err == nil {
		t.Fatal("expected error for invalid strictness, got nil")
	}
}

func TestParseInvalidMaxFindings(t *testing.T) {
	for _, data := range [][]byte{[]byte("max_findings: 0\n"), []byte("max_findings: 99\n")} {
		if _, err := Parse(data); err == nil {
			t.Fatalf("expected error for %q, got nil", data)
		}
	}
}

func TestParseInvalidPerFileCap(t *testing.T) {
	if _, err := Parse([]byte("per_file_cap: 11\n")); err == nil {
		t.Fatal("expected error for out-of-range per_file_cap, got nil")
	}
}

func TestParsePartialOverlay(t *testing.T) {
	cfg, err := Parse([]byte("enabled: false\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	def := domain.DefaultRepoConfig()
	if cfg.Enabled {
		t.Error("enabled = true, want false")
	}
	if cfg.Mode != def.Mode {
		t.Errorf("mode = %q, want default %q", cfg.Mode, def.Mode)
	}
	if cfg.Strictness != def.Strictness {
		t.Errorf("strictness = %q, want default %q", cfg.Strictness, def.Strictness)
	}
	if cfg.MaxFindings != def.MaxFindings {
		t.Errorf("max_findings = %d, want default %d", cfg.MaxFindings, def.MaxFindings)
	}
	if cfg.PerFileCap != def.PerFileCap {
		t.Errorf("per_file_cap = %d, want default %d", cfg.PerFileCap, def.PerFileCap)
	}
	if cfg.IncludeNits != def.IncludeNits {
		t.Errorf("include_nits = %v, want default %v", cfg.IncludeNits, def.IncludeNits)
	}
}

func TestPathIgnoredDefaultLockfile(t *testing.T) {
	cfg := domain.DefaultRepoConfig()
	if !PathIgnored(&cfg, "src/package-lock.json") {
		t.Error("package-lock.json not ignored")
	}
	if !PathIgnored(&cfg, "yarn.lock") {
		t.Error("yarn.lock not ignored")
	}
	if PathIgnored(&cfg, "src/main.go") {
		t.Error("src/main.go unexpectedly ignored")
	}
}

func TestPathIgnoredCustomGlob(t *testing.T) {
	cfg := domain.DefaultRepoConfig()
	cfg.IgnorePaths = []string{"docs/**"}
	if !PathIgnored(&cfg, "docs/a/b.md") {
		t.Error("docs/a/b.md not ignored by docs/**")
	}
	if PathIgnored(&cfg, "src/a/b.md") {
		t.Error("src/a/b.md unexpectedly ignored by docs/**")
	}
}

func TestPathIgnoredCustomPBGo(t *testing.T) {
	cfg := domain.DefaultRepoConfig()
	cfg.IgnorePaths = []string{"*.pb.go"}
	if !PathIgnored(&cfg, "api.pb.go") {
		t.Error("api.pb.go not ignored by *.pb.go")
	}
	if PathIgnored(&cfg, "api.go") {
		t.Error("api.go unexpectedly ignored by *.pb.go")
	}
}

func TestUserIgnoredDefaults(t *testing.T) {
	cfg := domain.DefaultRepoConfig()
	if !UserIgnored(&cfg, "dependabot[bot]") {
		t.Error("dependabot[bot] not ignored")
	}
	if !UserIgnored(&cfg, "github-actions[bot]") {
		t.Error("github-actions[bot] not ignored")
	}
	if UserIgnored(&cfg, "octocat") {
		t.Error("octocat unexpectedly ignored")
	}
}

func TestUserIgnoredCustom(t *testing.T) {
	cfg := domain.DefaultRepoConfig()
	cfg.IgnoreUsernames = []string{"alice"}
	if !UserIgnored(&cfg, "alice") {
		t.Error("alice not ignored")
	}
	if UserIgnored(&cfg, "bob") {
		t.Error("bob unexpectedly ignored")
	}
}

func TestTitleSkipped(t *testing.T) {
	cfg := domain.DefaultRepoConfig()
	if !TitleSkipped(&cfg, "[WIP] feat: add widget") {
		t.Error("[WIP] title not skipped")
	}
	if TitleSkipped(&cfg, "feat: x") {
		t.Error("feat: x unexpectedly skipped")
	}
}

func TestBaseBranchAllowed(t *testing.T) {
	cfg := domain.DefaultRepoConfig()
	if !BaseBranchAllowed(&cfg, "anything") {
		t.Error("empty base_branches should allow all")
	}
	cfg.BaseBranches = []string{"main", "develop"}
	if !BaseBranchAllowed(&cfg, "main") {
		t.Error("main not allowed")
	}
	if BaseBranchAllowed(&cfg, "feature/x") {
		t.Error("feature/x unexpectedly allowed")
	}
}

func TestModeAllows(t *testing.T) {
	cfg := domain.DefaultRepoConfig()
	if !ModeAllows(&cfg, "pr") {
		t.Error("default mode should allow pr")
	}
	if ModeAllows(&cfg, "push") {
		t.Error("default mode should not allow push")
	}
	cfg.Mode = "both"
	if !ModeAllows(&cfg, "pr") || !ModeAllows(&cfg, "push") {
		t.Error("both mode should allow pr and push")
	}
	cfg.Mode = "push"
	if ModeAllows(&cfg, "pr") || !ModeAllows(&cfg, "push") {
		t.Error("push mode should allow push only")
	}
}

func TestEffectiveReviewConfig(t *testing.T) {
	cfg := domain.DefaultRepoConfig()
	cfg.Strictness = "strict"
	cfg.MaxFindings = 20
	cfg.PerFileCap = 4
	cfg.IncludeNits = true
	cfg.CustomInstructions = []string{"be nice"}
	rc := EffectiveReviewConfig(&cfg)
	if rc.Strictness != "strict" || rc.MaxFindings != 20 || rc.PerFileCap != 4 || !rc.IncludeNits {
		t.Errorf("unexpected review config: %+v", rc)
	}
	if !reflect.DeepEqual(rc.CustomInstructions, []string{"be nice"}) {
		t.Errorf("custom_instructions = %v", rc.CustomInstructions)
	}
}

func TestRepoConfigCacheTTL(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	fetch := func(ctx context.Context, path, ref string) (string, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		if path != ".codepeer.yml" {
			t.Errorf("fetch path = %q, want .codepeer.yml", path)
		}
		if ref != "main" {
			t.Errorf("fetch ref = %q, want main", ref)
		}
		return "mode: push\nmax_findings: 7\n", nil
	}
	c := NewRepoConfigCache(50 * time.Millisecond)

	cfg, err := c.Get(context.Background(), "o", "r", "main", fetch)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cfg.Mode != "push" || cfg.MaxFindings != 7 {
		t.Errorf("unexpected config: %+v", cfg)
	}

	cfg2, err := c.Get(context.Background(), "o", "r", "main", fetch)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cfg2.Mode != "push" {
		t.Errorf("cached config mode = %q, want push", cfg2.Mode)
	}
	mu.Lock()
	if calls != 1 {
		t.Errorf("fetch calls within TTL = %d, want 1", calls)
	}
	mu.Unlock()

	time.Sleep(60 * time.Millisecond)
	if _, err := c.Get(context.Background(), "o", "r", "main", fetch); err != nil {
		t.Fatalf("Get after expiry: %v", err)
	}
	mu.Lock()
	if calls != 2 {
		t.Errorf("fetch calls after expiry = %d, want 2", calls)
	}
	mu.Unlock()
}

func TestRepoConfigCacheMissingFile(t *testing.T) {
	calls := 0
	fetch := func(ctx context.Context, path, ref string) (string, error) {
		calls++
		return "", nil
	}
	c := NewRepoConfigCache(time.Minute)
	cfg, err := c.Get(context.Background(), "o", "r", "main", fetch)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(*cfg, domain.DefaultRepoConfig()) {
		t.Errorf("missing file config = %+v, want defaults", *cfg)
	}
	if _, err := c.Get(context.Background(), "o", "r", "main", fetch); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if calls != 1 {
		t.Errorf("fetch calls = %d, want 1 (cached)", calls)
	}
}

func TestRepoConfigCacheInvalidYAML(t *testing.T) {
	fetch := func(ctx context.Context, path, ref string) (string, error) {
		return "mode: [unclosed\n", nil
	}
	c := NewRepoConfigCache(time.Minute)
	cfg, err := c.Get(context.Background(), "o", "r", "main", fetch)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(*cfg, domain.DefaultRepoConfig()) {
		t.Errorf("invalid YAML config = %+v, want defaults", *cfg)
	}
}

func TestRepoConfigCacheFetchError(t *testing.T) {
	fetch := func(ctx context.Context, path, ref string) (string, error) {
		return "", context.DeadlineExceeded
	}
	c := NewRepoConfigCache(time.Minute)
	if _, err := c.Get(context.Background(), "o", "r", "main", fetch); err == nil {
		t.Fatal("expected fetch error to propagate, got nil")
	}
}

func TestLoad(t *testing.T) {
	for _, k := range []string{
		"PORT", "LOG_LEVEL", "DATABASE_URL", "GITHUB_APP_ID",
		"GITHUB_APP_PRIVATE_KEY", "GITHUB_APP_CLIENT_ID", "GITHUB_WEBHOOK_SECRET",
		"BOT_LOGIN", "LLM_BASE_URL", "LLM_API_KEY", "LLM_MODEL",
		"LLM_REASONING_EFFORT", "LLM_TIMEOUT", "QUEUE_WORKERS",
		"QUEUE_POLL_INTERVAL", "QUEUE_MAX_ATTEMPTS", "QUEUE_LEASE_TTL",
	} {
		t.Setenv(k, "")
	}
	t.Setenv("DATABASE_URL", "postgres://user@localhost/db")
	t.Setenv("GITHUB_APP_ID", "12345")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "wh-secret")
	t.Setenv("LLM_TIMEOUT", "45s")
	t.Setenv("QUEUE_WORKERS", "3")
	t.Setenv("GITHUB_APP_CLIENT_ID", "Iv1.client")
	t.Setenv("BOT_LOGIN", "codepeer[bot]")

	e, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if e.Port != "8080" {
		t.Errorf("port = %q, want 8080", e.Port)
	}
	if e.LogLevel != "info" {
		t.Errorf("log_level = %q, want info", e.LogLevel)
	}
	if e.DatabaseURL != "postgres://user@localhost/db" {
		t.Errorf("database_url = %q", e.DatabaseURL)
	}
	if e.GitHubAppID != 12345 {
		t.Errorf("github_app_id = %d, want 12345", e.GitHubAppID)
	}
	if e.GitHubAppClientID != "Iv1.client" {
		t.Errorf("github_app_client_id = %q", e.GitHubAppClientID)
	}
	if e.BotLogin != "codepeer[bot]" {
		t.Errorf("bot_login = %q", e.BotLogin)
	}
	if e.LLMBaseURL != "https://api.deepseek.com" {
		t.Errorf("llm_base_url = %q", e.LLMBaseURL)
	}
	if e.LLMModel != "deepseek-v4-flash" {
		t.Errorf("llm_model = %q", e.LLMModel)
	}
	if e.LLMReasoningEffort != "high" {
		t.Errorf("llm_reasoning_effort = %q", e.LLMReasoningEffort)
	}
	if e.LLMTimeout != 45*time.Second {
		t.Errorf("llm_timeout = %v, want 45s", e.LLMTimeout)
	}
	if e.QueueWorkers != 3 {
		t.Errorf("queue_workers = %d, want 3", e.QueueWorkers)
	}
	if e.QueuePollInterval != 2*time.Second {
		t.Errorf("queue_poll_interval = %v, want 2s", e.QueuePollInterval)
	}
	if e.QueueMaxAttempts != 5 {
		t.Errorf("queue_max_attempts = %d, want 5", e.QueueMaxAttempts)
	}
	if e.QueueLeaseTTL != 15*time.Minute {
		t.Errorf("queue_lease_ttl = %v, want 15m", e.QueueLeaseTTL)
	}
}

func TestLoadMissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("GITHUB_APP_ID", "1")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "x")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected DATABASE_URL error, got %v", err)
	}
}

func TestLoadMissingWebhookSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("GITHUB_APP_ID", "1")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GITHUB_WEBHOOK_SECRET") {
		t.Fatalf("expected GITHUB_WEBHOOK_SECRET error, got %v", err)
	}
}

func TestLoadMissingAppID(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("GITHUB_APP_ID", "")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "x")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GITHUB_APP_ID") {
		t.Fatalf("expected GITHUB_APP_ID error, got %v", err)
	}
}

func TestLoadInvalidDuration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("GITHUB_APP_ID", "1")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "x")
	t.Setenv("LLM_TIMEOUT", "not-a-duration")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "LLM_TIMEOUT") {
		t.Fatalf("expected LLM_TIMEOUT error, got %v", err)
	}
}

func TestLoadInvalidInt(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("GITHUB_APP_ID", "1")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "x")
	t.Setenv("QUEUE_WORKERS", "many")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "QUEUE_WORKERS") {
		t.Fatalf("expected QUEUE_WORKERS error, got %v", err)
	}
}

func TestPrivateKeyPEMInline(t *testing.T) {
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----\n"
	e := Env{GitHubAppPrivateKey: pem}
	got, err := e.PrivateKeyPEM()
	if err != nil {
		t.Fatalf("PrivateKeyPEM: %v", err)
	}
	if string(got) != pem {
		t.Errorf("PrivateKeyPEM = %q, want %q", got, pem)
	}
}

func TestPrivateKeyPEMPath(t *testing.T) {
	p := filepath.Join(t.TempDir(), "key.pem")
	content := []byte("pem-file-contents")
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	e := Env{GitHubAppPrivateKey: p}
	got, err := e.PrivateKeyPEM()
	if err != nil {
		t.Fatalf("PrivateKeyPEM: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("PrivateKeyPEM = %q, want %q", got, content)
	}
}

func TestEnvRedacted(t *testing.T) {
	e := Env{
		GitHubAppPrivateKey: "private-key-contents",
		GitHubWebhookSecret: "wh-secret",
		LLMAPIKey:           "llm-secret",
		BotLogin:            "codepeer[bot]",
	}
	s := e.Redacted()
	if !strings.Contains(s, "<redacted>") {
		t.Error("redacted output missing <redacted>")
	}
	for _, secret := range []string{"private-key-contents", "wh-secret", "llm-secret"} {
		if strings.Contains(s, secret) {
			t.Errorf("redacted output leaks %q", secret)
		}
	}
	if !strings.Contains(s, "codepeer[bot]") {
		t.Error("redacted output should keep non-secret fields")
	}
	if e.GitHubAppPrivateKey != "private-key-contents" {
		t.Error("Redacted mutated the original env")
	}
}

func TestLoadDotenv(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "PORT=9999\n# comment\nQUOTED=\"hello world\"\nEMPTYVAL=\nBADLINE\n"
	if err := os.WriteFile(envFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	old := os.Getenv("CODE_PEER_ENV_FILE")
	t.Setenv("CODE_PEER_ENV_FILE", envFile)
	t.Setenv("EXISTING_KEY", "keep-me")
	if err := loadDotenv(); err != nil {
		t.Fatalf("loadDotenv: %v", err)
	}
	if got := os.Getenv("PORT"); got != "9999" {
		t.Errorf("PORT = %q, want 9999", got)
	}
	if got := os.Getenv("QUOTED"); got != "hello world" {
		t.Errorf("QUOTED = %q, want stripped quotes", got)
	}
	if _, ok := os.LookupEnv("EMPTYVAL"); !ok {
		t.Errorf("EMPTYVAL should be set to empty")
	}
	if _, ok := os.LookupEnv("BADLINE"); ok {
		t.Errorf("BADLINE should be ignored")
	}
	_ = old
}

func TestLoadDotenvMissingFile(t *testing.T) {
	t.Setenv("CODE_PEER_ENV_FILE", filepath.Join(t.TempDir(), "does-not-exist.env"))
	if err := loadDotenv(); err != nil {
		t.Fatalf("missing env file must not error: %v", err)
	}
}
