package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/config"
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/githubx"
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/store"
)

// runSync pulls installations and their repos from the GitHub API into the
// store. Useful after installing the app when the installation webhook may
// have been missed.
func runSync() error {
	env, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, env.DatabaseURL)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres ping: %w", err)
	}
	if err := store.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	st := store.New(pool)

	keyPEM, err := env.PrivateKeyPEM()
	if err != nil {
		return fmt.Errorf("private key: %w", err)
	}
	gh, err := githubx.New(githubx.Config{
		AppID:      env.GitHubAppID,
		ClientID:   env.GitHubAppClientID,
		PrivateKey: keyPEM,
		SelfLogin:  env.BotLogin,
	})
	if err != nil {
		return fmt.Errorf("githubx: %w", err)
	}

	installs, err := gh.ListInstallations(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%d installations\n", len(installs))
	totalRepos := 0
	for _, inst := range installs {
		if err := st.UpsertInstallation(ctx, inst); err != nil {
			return err
		}
		ictx := githubx.WithInstallation(ctx, inst.ID)
		repos, err := gh.ListInstallationRepos(ictx, inst.ID)
		if err != nil {
			fmt.Printf("  %s: repo list failed: %v\n", inst.AccountLogin, err)
			continue
		}
		for _, r := range repos {
			if err := st.UpsertRepo(ctx, r); err != nil {
				return err
			}
		}
		totalRepos += len(repos)
		fmt.Printf("  %s (%s): %d repos\n", inst.AccountLogin, inst.AccountType, len(repos))
	}
	fmt.Printf("synced %d repos\n", totalRepos)
	return nil
}
