// Package httpapi hosts the webhook receiver and health endpoints.
package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/githubx"
)

const maxBodyBytes = 26 << 20

// Handler serves webhook deliveries and health checks.
type Handler struct {
	secret []byte
	gh     domain.GitHubAPI
	store  domain.Store
	queue  domain.Queue
	logger *slog.Logger
}

// New builds the HTTP handler.
func New(secret []byte, gh domain.GitHubAPI, st domain.Store, q domain.Queue, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{secret: secret, gh: gh, store: st, queue: q, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", h.handleWebhook)
	mux.HandleFunc("GET /healthz", h.handleHealth)
	mux.HandleFunc("GET /readyz", h.handleReady)
	return mux
}

func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := h.store.Ping(ctx); err != nil {
		http.Error(w, "database unreachable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

func (h *Handler) handleWebhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Warn("webhook body read failed", "err", err)
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}
	if !githubx.VerifySignature(h.secret, body, r.Header.Get("X-Hub-Signature-256")) {
		h.logger.Warn("webhook signature verification failed")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	delivery := r.Header.Get("X-GitHub-Delivery")
	event := r.Header.Get("X-GitHub-Event")
	ok, err := h.store.RecordDelivery(r.Context(), delivery, event)
	if err != nil {
		h.logger.Error("delivery dedupe failed", "delivery", delivery, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		w.WriteHeader(http.StatusOK)
		return
	}

	if err := h.dispatch(r, event, body); err != nil {
		h.logger.Error("webhook dispatch failed", "delivery", delivery, "event", event, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) dispatch(r *http.Request, event string, body []byte) error {
	ctx := r.Context()
	switch event {
	case "ping":
		return nil
	case "pull_request":
		return h.handlePullRequest(ctx, body)
	case "push":
		return h.handlePush(ctx, body)
	case "installation":
		return h.handleInstallation(ctx, body)
	case "installation_repositories":
		return h.handleInstallationRepos(ctx, body)
	case "issue_comment":
		return h.handleIssueComment(ctx, body)
	default:
		h.logger.Debug("unhandled webhook event", "event", event)
		return nil
	}
}

type eventEnvelope struct {
	Action     string `json:"action"`
	Repository struct {
		ID    int64 `json:"id"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name   string `json:"name"`
		Pushed bool   `json:"-"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

type pullRequestEvent struct {
	eventEnvelope
	Number      int `json:"number"`
	PullRequest struct {
		Number int  `json:"number"`
		Draft  bool `json:"draft"`
		Merged bool `json:"merged"`
		Head   struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	} `json:"pull_request"`
}

func (h *Handler) handlePullRequest(ctx context.Context, body []byte) error {
	var ev pullRequestEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return err
	}
	if ev.Sender.Login != "" && h.gh.SelfLogin() != "" && ev.Sender.Login == h.gh.SelfLogin() {
		return nil
	}
	prNumber := ev.PullRequest.Number
	if prNumber == 0 {
		prNumber = ev.Number
	}
	h.audit(ctx, ev.eventEnvelope, "pull_request", map[string]any{"pr": prNumber})

	switch ev.Action {
	case "opened", "reopened", "ready_for_review":
		return h.queue.Enqueue(ctx, domain.JobAnalyzePR, domain.AnalyzePRPayload{
			InstallationID: ev.Installation.ID,
			RepoID:         ev.Repository.ID,
			RepoOwner:      ev.Repository.Owner.Login,
			RepoName:       ev.Repository.Name,
			PRNumber:       prNumber,
			HeadSHA:        ev.PullRequest.Head.SHA,
			Action:         ev.Action,
			SenderLogin:    ev.Sender.Login,
		})
	case "synchronize":
		if err := h.queue.Enqueue(ctx, domain.JobAnalyzePR, domain.AnalyzePRPayload{
			InstallationID: ev.Installation.ID,
			RepoID:         ev.Repository.ID,
			RepoOwner:      ev.Repository.Owner.Login,
			RepoName:       ev.Repository.Name,
			PRNumber:       prNumber,
			HeadSHA:        ev.PullRequest.Head.SHA,
			Action:         ev.Action,
			SenderLogin:    ev.Sender.Login,
		}); err != nil {
			return err
		}
		return h.queue.Enqueue(ctx, domain.JobFeedback, domain.FeedbackPayload{
			InstallationID: ev.Installation.ID,
			RepoID:         ev.Repository.ID,
			RepoOwner:      ev.Repository.Owner.Login,
			RepoName:       ev.Repository.Name,
			PRNumber:       prNumber,
		})
	case "closed":
		if !ev.PullRequest.Merged {
			return nil
		}
		return h.queue.Enqueue(ctx, domain.JobIssueClose, domain.ClosePRIssuesPayload{
			RepoID:    ev.Repository.ID,
			RepoOwner: ev.Repository.Owner.Login,
			RepoName:  ev.Repository.Name,
			PRNumber:  prNumber,
		})
	default:
		return nil
	}
}

type pushEvent struct {
	eventEnvelope
	Ref     string `json:"ref"`
	Before  string `json:"before"`
	After   string `json:"after"`
	Created bool   `json:"created"`
	Deleted bool   `json:"deleted"`
}

func (h *Handler) handlePush(ctx context.Context, body []byte) error {
	var ev pushEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return err
	}
	if ev.Sender.Login != "" && h.gh.SelfLogin() != "" && ev.Sender.Login == h.gh.SelfLogin() {
		return nil
	}
	if ev.Deleted || ev.Before == ev.After {
		return nil
	}
	h.audit(ctx, ev.eventEnvelope, "push", map[string]any{"ref": ev.Ref})
	return h.queue.Enqueue(ctx, domain.JobAnalyzePush, domain.AnalyzePushPayload{
		InstallationID: ev.Installation.ID,
		RepoID:         ev.Repository.ID,
		RepoOwner:      ev.Repository.Owner.Login,
		RepoName:       ev.Repository.Name,
		Before:         ev.Before,
		After:          ev.After,
		Ref:            ev.Ref,
		SenderLogin:    ev.Sender.Login,
	})
}

type installationEvent struct {
	Action       string `json:"action"`
	Installation struct {
		ID      int64 `json:"id"`
		Account struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"account"`
	} `json:"installation"`
}

func (h *Handler) handleInstallation(ctx context.Context, body []byte) error {
	var ev installationEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return err
	}
	if ev.Action != "created" && ev.Action != "new_permissions_accepted" && ev.Action != "unsuspend" {
		return nil
	}
	if err := h.store.UpsertInstallation(ctx, domain.Installation{
		ID:           ev.Installation.ID,
		AccountID:    ev.Installation.Account.ID,
		AccountLogin: ev.Installation.Account.Login,
		AccountType:  ev.Installation.Account.Type,
	}); err != nil {
		return err
	}
	go h.syncRepos(ev.Installation.ID)
	return nil
}

type issueCommentEvent struct {
	eventEnvelope
	Comment struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"comment"`
	Issue struct {
		Number      int    `json:"number"`
		PullRequest any    `json:"pull_request"`
		State       string `json:"state"`
	} `json:"issue"`
}

func (h *Handler) handleIssueComment(ctx context.Context, body []byte) error {
	var ev issueCommentEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return err
	}
	if ev.Action != "created" {
		return nil
	}
	if ev.Sender.Login != "" && h.gh.SelfLogin() != "" && ev.Sender.Login == h.gh.SelfLogin() {
		return nil
	}
	if ev.Issue.PullRequest != nil {
		return nil
	}
	command := strings.ToLower(strings.TrimSpace(ev.Comment.Body))
	if command != "approve" && command != "deny" {
		return nil
	}
	h.audit(ctx, ev.eventEnvelope, "issue_comment", map[string]any{"issue": ev.Issue.Number, "command": command})
	return h.queue.Enqueue(ctx, domain.JobIssueCmd, domain.IssueCommandPayload{
		InstallationID: ev.Installation.ID,
		RepoID:         ev.Repository.ID,
		RepoOwner:      ev.Repository.Owner.Login,
		RepoName:       ev.Repository.Name,
		IssueNumber:    ev.Issue.Number,
		Command:        command,
		SenderLogin:    ev.Sender.Login,
	})
}

type repoRef struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

type installationReposEvent struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	RepositoriesAdded   []repoRef `json:"repositories_added"`
	RepositoriesRemoved []repoRef `json:"repositories_removed"`
}

func (h *Handler) handleInstallationRepos(ctx context.Context, body []byte) error {
	var ev installationReposEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return err
	}
	for _, r := range ev.RepositoriesAdded {
		if err := h.store.UpsertRepo(ctx, domain.Repo{
			ID:             r.ID,
			InstallationID: ev.Installation.ID,
			Owner:          r.Owner.Login,
			Name:           r.Name,
			DefaultBranch:  r.DefaultBranch,
			Enabled:        true,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) syncRepos(installationID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ctx = githubx.WithInstallation(ctx, installationID)
	repos, err := h.gh.ListInstallationRepos(ctx, installationID)
	if err != nil {
		h.logger.Warn("repo sync failed", "installation", installationID, "err", err)
		return
	}
	for _, r := range repos {
		if err := h.store.UpsertRepo(ctx, r); err != nil {
			h.logger.Warn("repo upsert failed", "repo", r.ID, "err", err)
		}
	}
	h.logger.Info("repo sync complete", "installation", installationID, "repos", len(repos))
}

func (h *Handler) audit(ctx context.Context, ev eventEnvelope, kind string, detail map[string]any) {
	detail["action"] = ev.Action
	if err := h.store.Audit(ctx, domain.AuditEntry{
		Event:  kind,
		Action: ev.Action,
		RepoID: ev.Repository.ID,
		Kind:   kind,
		Detail: detail,
	}); err != nil {
		h.logger.Debug("audit write failed", "err", err)
	}
}
