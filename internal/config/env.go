// Package config owns environment loading and per-repo .codepeer.yml
// parsing, matching, and caching.
package config

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Env holds the process configuration loaded from environment variables.
type Env struct {
	Port                string
	LogLevel            string
	DatabaseURL         string
	GitHubAppID         int64
	GitHubAppPrivateKey string
	GitHubAppClientID   string
	GitHubWebhookSecret string
	BotLogin            string
	LLMBaseURL          string
	LLMAPIKey           string
	LLMModel            string
	LLMReasoningEffort  string
	LLMTimeout          time.Duration
	QueueWorkers        int
	QueuePollInterval   time.Duration
	QueueMaxAttempts    int
	QueueLeaseTTL       time.Duration
}

// Load reads the configuration from environment variables (including a
// .env file in the working directory when present), applying defaults for
// optional values and returning a descriptive error when required values
// are missing or malformed.
func Load() (Env, error) {
	if err := loadDotenv(); err != nil {
		return Env{}, err
	}
	e := Env{
		Port:                envOr("PORT", "8080"),
		LogLevel:            envOr("LOG_LEVEL", "info"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		GitHubAppPrivateKey: os.Getenv("GITHUB_APP_PRIVATE_KEY"),
		GitHubAppClientID:   os.Getenv("GITHUB_APP_CLIENT_ID"),
		GitHubWebhookSecret: os.Getenv("GITHUB_WEBHOOK_SECRET"),
		BotLogin:            os.Getenv("BOT_LOGIN"),
		LLMBaseURL:          envOr("LLM_BASE_URL", "https://api.deepseek.com"),
		LLMAPIKey:           os.Getenv("LLM_API_KEY"),
		LLMModel:            envOr("LLM_MODEL", "deepseek-v4-flash"),
		LLMReasoningEffort:  envOr("LLM_REASONING_EFFORT", "high"),
	}

	var err error
	if e.LLMTimeout, err = durationEnv("LLM_TIMEOUT", 300*time.Second); err != nil {
		return Env{}, err
	}
	if e.QueuePollInterval, err = durationEnv("QUEUE_POLL_INTERVAL", 2*time.Second); err != nil {
		return Env{}, err
	}
	if e.QueueLeaseTTL, err = durationEnv("QUEUE_LEASE_TTL", 15*time.Minute); err != nil {
		return Env{}, err
	}
	if e.QueueWorkers, err = intEnv("QUEUE_WORKERS", 2); err != nil {
		return Env{}, err
	}
	if e.QueueMaxAttempts, err = intEnv("QUEUE_MAX_ATTEMPTS", 5); err != nil {
		return Env{}, err
	}

	if e.DatabaseURL == "" {
		return Env{}, errors.New("DATABASE_URL is required")
	}
	if e.GitHubWebhookSecret == "" {
		return Env{}, errors.New("GITHUB_WEBHOOK_SECRET is required")
	}

	appIDStr := os.Getenv("GITHUB_APP_ID")
	if appIDStr == "" {
		return Env{}, errors.New("GITHUB_APP_ID is required")
	}
	if e.GitHubAppID, err = strconv.ParseInt(appIDStr, 10, 64); err != nil {
		return Env{}, fmt.Errorf("GITHUB_APP_ID: invalid int64 %q: %w", appIDStr, err)
	}

	return e, nil
}

// PrivateKeyPEM returns the GitHub App private key as PEM bytes. The value may
// be the PEM contents, the PEM base64-encoded, the PEM on a single line with
// escaped newlines, or a path to a PEM file. The single-line forms exist for
// hosts whose variable inputs cannot carry a multi-line value.
func (e Env) PrivateKeyPEM() ([]byte, error) {
	raw := e.GitHubAppPrivateKey
	// A real PEM has no literal backslash-n, so unescaping is a no-op for it.
	if unescaped := strings.ReplaceAll(raw, `\n`, "\n"); strings.HasPrefix(strings.TrimSpace(unescaped), "-----BEGIN") {
		return []byte(unescaped), nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(raw), "")); err == nil &&
		bytes.HasPrefix(bytes.TrimSpace(decoded), []byte("-----BEGIN")) {
		return decoded, nil
	}
	return os.ReadFile(raw)
}

// Redacted returns a copy of the env as a string with secret values replaced,
// safe for debug logging.
func (e Env) Redacted() string {
	if e.GitHubAppPrivateKey != "" {
		e.GitHubAppPrivateKey = "<redacted>"
	}
	if e.GitHubWebhookSecret != "" {
		e.GitHubWebhookSecret = "<redacted>"
	}
	if e.LLMAPIKey != "" {
		e.LLMAPIKey = "<redacted>"
	}
	return fmt.Sprintf("%+v", e)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func durationEnv(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q: %w", key, v, err)
	}
	return d, nil
}

func intEnv(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q: %w", key, v, err)
	}
	return n, nil
}
