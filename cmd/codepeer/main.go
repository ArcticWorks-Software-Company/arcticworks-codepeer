// Command codepeer runs the ArcticWorks CodePeer GitHub App bot.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/analysis"
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/config"
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/githubx"
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/httpapi"
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/llm"
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/posting"
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/queue"
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/store"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "check":
			includeLLM := false
			for _, a := range os.Args[2:] {
				if a == "--llm" || a == "-llm" {
					includeLLM = true
				}
			}
			if err := runCheck(includeLLM); err != nil {
				os.Exit(1)
			}
			return
		case "sync":
			if err := runSync(); err != nil {
				fmt.Fprintln(os.Stderr, "codepeer sync:", err)
				os.Exit(1)
			}
			return
		case "webhook":
			url := ""
			if len(os.Args) > 2 {
				url = os.Args[2]
			}
			if err := runWebhook(url); err != nil {
				fmt.Fprintln(os.Stderr, "codepeer webhook:", err)
				os.Exit(1)
			}
			return
		case "version", "--version", "-v":
			fmt.Println("codepeer dev")
			return
		}
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "codepeer:", err)
		os.Exit(1)
	}
}

func run() error {
	env, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	var level slog.Level
	switch env.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
	logger.Info("database ready")

	st := store.New(pool)
	q := queue.New(pool, env.QueueLeaseTTL)

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
	resolveCtx, cancelResolve := context.WithTimeout(ctx, 30*time.Second)
	login, err := gh.ResolveSelfLogin(resolveCtx)
	cancelResolve()
	if err != nil {
		logger.Warn("self-login resolution failed; self-event filtering disabled until BOT_LOGIN is set", "err", err)
	} else if login != "" {
		logger.Info("bot identity resolved", "login", login)
	}

	reviewer := llm.New(llm.Config{
		BaseURL:         env.LLMBaseURL,
		APIKey:          env.LLMAPIKey,
		Model:           env.LLMModel,
		ReasoningEffort: env.LLMReasoningEffort,
		Timeout:         env.LLMTimeout,
	})
	pipeline := analysis.NewPipeline(reviewer, gh, st)
	poster := posting.New(gh, st, logger)
	cfgCache := config.NewRepoConfigCache(5 * time.Minute)

	worker := &Worker{
		Pipeline: pipeline,
		Poster:   poster,
		Store:    st,
		GitHub:   gh,
		Queue:    q,
		Config:   cfgCache,
		Logger:   logger,
		JobTTL:   env.LLMTimeout + 5*time.Minute,
	}

	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		worker.Start(ctx, env.QueueWorkers, env.QueuePollInterval)
	}()

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n, err := q.ReapExpired(ctx, env.QueueLeaseTTL); err != nil {
					logger.Warn("lease reaper failed", "err", err)
				} else if n > 0 {
					logger.Info("reaped expired jobs", "count", n)
				}
			}
		}
	}()

	server := &http.Server{
		Addr:              ":" + env.Port,
		Handler:           httpapi.New([]byte(env.GitHubWebhookSecret), gh, st, q, logger),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("codepeer listening", "port", env.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	select {
	case <-workerDone:
	case <-time.After(30 * time.Second):
		logger.Warn("worker shutdown timed out")
	}
	return nil
}

// Worker consumes queue jobs and dispatches to the pipeline and poster.
type Worker struct {
	Pipeline *analysis.Pipeline
	Poster   *posting.Poster
	Store    domain.Store
	GitHub   domain.GitHubAPI
	Queue    domain.Queue
	Config   *config.RepoConfigCache
	Logger   *slog.Logger
	JobTTL   time.Duration
}

// Start runs the worker pool until ctx is cancelled.
func (w *Worker) Start(ctx context.Context, workers int, pollInterval time.Duration) {
	if workers < 1 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		go w.loop(ctx, pollInterval, i)
	}
	<-ctx.Done()
}

func (w *Worker) loop(ctx context.Context, pollInterval time.Duration, id int) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		job, ok, err := w.Queue.Dequeue(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			w.Logger.Warn("dequeue failed", "worker", id, "err", err)
		}
		if ok && job != nil {
			w.process(ctx, job, id)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) process(ctx context.Context, job *domain.Job, workerID int) {
	jobCtx, cancel := context.WithTimeout(ctx, w.JobTTL)
	defer cancel()
	jobCtx = githubx.WithInstallation(jobCtx, w.installationID(job))

	logger := w.Logger.With("job", job.ID, "kind", job.Kind, "worker", workerID, "attempt", job.Attempts+1)
	if err := w.dispatch(jobCtx, job); err != nil {
		logger.Warn("job failed", "err", err)
		if ferr := w.Queue.Fail(ctx, job.ID, err.Error()); ferr != nil {
			logger.Error("fail job", "err", ferr)
		}
		return
	}
	if err := w.Queue.Complete(ctx, job.ID); err != nil {
		logger.Error("complete job", "err", err)
	}
	logger.Debug("job done")
}

func (w *Worker) installationID(job *domain.Job) int64 {
	var probe struct {
		InstallationID int64 `json:"installation_id"`
	}
	if err := jsonUnmarshal(job.Payload, &probe); err == nil {
		return probe.InstallationID
	}
	return 0
}

func (w *Worker) dispatch(ctx context.Context, job *domain.Job) error {
	switch job.Kind {
	case domain.JobAnalyzePR:
		var p domain.AnalyzePRPayload
		if err := jsonUnmarshal(job.Payload, &p); err != nil {
			return fmt.Errorf("payload: %w", err)
		}
		w.refreshConfig(ctx, p.RepoID, p.RepoOwner, p.RepoName, p.HeadSHA)
		out, err := w.Pipeline.AnalyzePR(ctx, p)
		if err != nil {
			return err
		}
		if out.Skipped != "" {
			w.Logger.Debug("analysis skipped", "reason", out.Skipped, "repo", p.RepoName, "pr", p.PRNumber)
		}
		return w.Poster.PostPRReview(ctx, *out)
	case domain.JobAnalyzePush:
		var p domain.AnalyzePushPayload
		if err := jsonUnmarshal(job.Payload, &p); err != nil {
			return fmt.Errorf("payload: %w", err)
		}
		w.refreshConfig(ctx, p.RepoID, p.RepoOwner, p.RepoName, p.After)
		out, err := w.Pipeline.AnalyzePush(ctx, p)
		if err != nil {
			return err
		}
		if out.Skipped != "" {
			w.Logger.Debug("analysis skipped", "reason", out.Skipped, "repo", p.RepoName)
		}
		return w.Poster.PostPush(ctx, *out)
	case domain.JobFeedback:
		var p domain.FeedbackPayload
		if err := jsonUnmarshal(job.Payload, &p); err != nil {
			return fmt.Errorf("payload: %w", err)
		}
		return w.Poster.LearnSweep(ctx, p)
	case domain.JobIssueClose:
		var p domain.ClosePRIssuesPayload
		if err := jsonUnmarshal(job.Payload, &p); err != nil {
			return fmt.Errorf("payload: %w", err)
		}
		return w.Poster.ClosePRIssues(ctx, p.RepoID, p.RepoOwner, p.RepoName, p.PRNumber)
	case domain.JobIssueCmd:
		var p domain.IssueCommandPayload
		if err := jsonUnmarshal(job.Payload, &p); err != nil {
			return fmt.Errorf("payload: %w", err)
		}
		return w.Poster.HandleIssueCommand(ctx, p)
	default:
		return fmt.Errorf("unknown job kind %q", job.Kind)
	}
}

func (w *Worker) refreshConfig(ctx context.Context, repoID int64, owner, repo, ref string) {
	cfg, err := w.Config.Get(ctx, owner, repo, ref, func(ctx context.Context, path, ref string) (string, error) {
		return w.GitHub.GetFile(ctx, owner, repo, path, ref)
	})
	if err != nil {
		w.Logger.Warn("config fetch failed", "repo", repo, "err", err)
		return
	}
	if err := w.Store.SetRepoConfig(ctx, repoID, cfg); err != nil {
		w.Logger.Warn("config persist failed", "repo", repo, "err", err)
	}
}

func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
