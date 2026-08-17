package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/config"
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/githubx"
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/llm"
)

// runCheck validates the installation prerequisites and prints a report.
// Exits non-zero when a required check fails.
func runCheck(includeLLM bool) error {
	ok := true
	check := func(name string, err error, detail string) {
		if err != nil {
			fmt.Printf("[FAIL] %s: %v\n", name, err)
			ok = false
			return
		}
		if detail == "" {
			fmt.Printf("[ OK ] %s\n", name)
		} else {
			fmt.Printf("[ OK ] %s — %s\n", name, detail)
		}
	}

	env, err := config.Load()
	check("config", err, "")
	if err != nil {
		return fmt.Errorf("configuration invalid")
	}
	fmt.Printf("       model=%s base=%s port=%s\n", env.LLMModel, env.LLMBaseURL, env.Port)

	keyPEM, err := env.PrivateKeyPEM()
	check("github app private key", err, "")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, env.DatabaseURL)
	if err != nil {
		check("postgres connect", err, "")
	} else {
		pingErr := pool.Ping(ctx)
		check("postgres connect", pingErr, "")
		if pingErr == nil {
			var versions []string
			rows, qerr := pool.Query(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
			if qerr != nil {
				check("migrations", qerr, "")
			} else {
				defer rows.Close()
				for rows.Next() {
					var v string
					_ = rows.Scan(&v)
					versions = append(versions, v)
				}
				check("migrations", rows.Err(), strings.Join(versions, ", "))
			}
		}
		pool.Close()
	}

	if len(env.GitHubWebhookSecret) < 8 {
		check("webhook secret", fmt.Errorf("must be at least 8 characters"), "")
	} else {
		check("webhook secret", nil, fmt.Sprintf("%d chars", len(env.GitHubWebhookSecret)))
		if len(env.GitHubWebhookSecret) < 16 {
			fmt.Printf("[WARN] webhook secret is short (%d chars); consider a longer secret\n", len(env.GitHubWebhookSecret))
		}
	}

	if keyPEM != nil {
		gh, gherr := githubx.New(githubx.Config{
			AppID:      env.GitHubAppID,
			ClientID:   env.GitHubAppClientID,
			PrivateKey: keyPEM,
			SelfLogin:  env.BotLogin,
		})
		check("github app client", gherr, "")
		if gherr == nil {
			n, authErr := gh.AuthCheck(ctx)
			check("github app auth (JWT)", authErr, fmt.Sprintf("%d installations", n))
			if authErr == nil && n > 0 {
				login, loginErr := gh.ResolveSelfLogin(ctx)
				if loginErr != nil {
					check("bot self-login", loginErr, "")
				} else {
					check("bot self-login", nil, login)
				}
			}
		}
	}

	if includeLLM {
		llmClient := llm.New(llm.Config{
			BaseURL:         env.LLMBaseURL,
			APIKey:          env.LLMAPIKey,
			Model:           env.LLMModel,
			ReasoningEffort: env.LLMReasoningEffort,
			Timeout:         60 * time.Second,
		})
		check("llm ping", llmClient.Ping(ctx), "")
	} else {
		fmt.Printf("[SKIP] llm ping (run `codepeer check --llm` to verify the LLM connection)\n")
	}

	if !ok {
		fmt.Fprintln(os.Stderr, "\ncheck failed — fix the failures above before starting the bot")
		return fmt.Errorf("preflight checks failed")
	}
	fmt.Println("\nall checks passed — the bot is ready to run")
	return nil
}
