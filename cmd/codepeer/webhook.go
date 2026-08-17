package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/config"
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/githubx"
)

// runWebhook updates the app-level webhook URL via the API.
func runWebhook(url string) error {
	if url == "" {
		return fmt.Errorf("usage: codepeer webhook <url>")
	}
	env, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	keyPEM, err := env.PrivateKeyPEM()
	if err != nil {
		return fmt.Errorf("private key: %w", err)
	}
	gh, err := githubx.New(githubx.Config{
		AppID:      env.GitHubAppID,
		ClientID:   env.GitHubAppClientID,
		PrivateKey: keyPEM,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := gh.SetAppHookURL(ctx, url); err != nil {
		return err
	}
	fmt.Printf("webhook URL updated to %s\n", url)
	return nil
}
