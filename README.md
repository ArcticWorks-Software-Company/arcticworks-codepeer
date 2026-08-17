# ArcticWorks CodePeer

**Type:** GitHub App / bot service (headless Go server; no desktop or web UI)

**Tagline:** Code review that never sleeps.

## Overview

ArcticWorks CodePeer is an org-wide code reviewer bot. Installed once on the ArcticWorks-Software-Company org, it watches every repository, reviews new code, and reports what it finds — as pull-request reviews, inline suggestions, and analysis issues.

Primary mode is PR-based: on every PR (opened, updated, reopened) CodePeer analyzes the diff and posts a review with a summary and inline comments. Small fixes are posted as native GitHub `suggestion` blocks, so a user applies them with one click. Larger findings are opened as linked analysis issues.

Optional push mode analyzes commits on every push and appends findings to a single rolling analysis issue per repository, instead of flooding the issue tracker.

Every analysis issue is approve-or-deny: comment `approve` and CodePeer opens a PR with the fixes; comment `deny` — or fix it yourself — and the issue is closed as resolved. The bot ignores its own events, so its own PRs never retrigger it.

## Audience

- Engineering teams on the ArcticWorks org
- Repository maintainers
- Anyone who wants review coverage on small or solo projects

## Key features

- PR review on `pull_request.opened` / `synchronize` / `reopened`
- Summary + inline comments, with one-click `suggestion` blocks for small fixes
- Analysis issues for larger findings, linked to the PR
- Approve/deny by commenting on the issue (or reacting)
- Approved → fix branch pushed automatically + PR that closes the issue
- Optional push mode with a rolling analysis issue per repo
- Per-repo configuration via `.codepeer.yml` (enable/disable, ignore paths, strictness, mode)
- LLM-backed analysis with an asynchronous queue (never blocks the webhook)
- Self-event filtering and loop protection
- PostgreSQL-backed state, dedupe, and audit history

## Identity

Integrates with ArcticWorks Identity for any future dashboard or shared settings — but Identity is optional: CodePeer authenticates as a GitHub App using per-installation tokens and runs fully standalone.

## Think

CodeRabbit, Sourcery, Amazon CodeGuru, Codacy.

## Deployment

### 1. Create the GitHub App

1. Org settings → Developer settings → GitHub Apps → New GitHub App.
2. **Permissions** (least privilege):
   - `Pull requests`: Read & write (submit reviews, inline comments)
   - `Contents`: Read (diffs, `.codepeer.yml`, file context)
   - `Issues`: Read & write (analysis issues, approve/deny)
   - `Checks`: Read & write (status check run)
   - `Metadata`: Read (implicit)
3. **Events**: `pull_request`, `push`, `issue_comment`, `installation`,
   `installation_repositories` (`installation` events arrive automatically).
4. Generate a private key (PEM) and note the App ID (and Client ID for the
   JWT issuer). Install the app on the ArcticWorks-Software-Company org
   (all repositories or a selection).

### 2. Configure

Copy `.env.example` to `.env` and fill in:

| Variable | Description |
|---|---|
| `DATABASE_URL` | Postgres DSN |
| `GITHUB_APP_ID` | App ID from the App settings |
| `GITHUB_APP_CLIENT_ID` | Client ID (JWT issuer; optional) |
| `GITHUB_APP_PRIVATE_KEY` | Path to the PEM file, or the PEM contents |
| `GITHUB_WEBHOOK_SECRET` | High-entropy secret; set the same value on the App webhook |
| `LLM_API_KEY` | DeepSeek API key |
| `LLM_BASE_URL` | `https://api.deepseek.com` (default) |
| `LLM_MODEL` | `deepseek-v4-flash` (default) |
| `BOT_LOGIN` | Bot login for self-event filtering (auto-resolved if empty) |

### 3. Run

Docker Compose (Docker Desktop, or Podman via the compatibility pipe):

```powershell
docker compose up -d --build          # Docker Desktop
.\scripts\docker-podman.ps1 compose up -d --build   # Podman on Windows
```

Or plain Podman:

```powershell
podman build -t codepeer .
podman run -d --name codepeer-db -p 5432:5432 -e POSTGRES_USER=codepeer -e POSTGRES_PASSWORD=codepeer -e POSTGRES_DB=codepeer postgres:17-alpine
podman run -d --name codepeer --network=host --env-file .env codepeer
```

The service runs migrations automatically on startup. Health endpoints:
`GET /healthz` (liveness) and `GET /readyz` (database reachable).

### 4. Webhook

In the App settings, point the webhook at
`https://<your-host>/webhook` with the webhook secret from `.env`.
For local development, expose the port with Smee or a tunnel and set the
webhook URL to the tunnel.

### 5. Local smoke test

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\smoke-webhook.ps1 -Secret <webhook-secret>
```

Posts a signed `pull_request.opened` delivery to `localhost:8080/webhook`
and prints the HTTP status (expect `202`).

### Per-repo config

`.codepeer.yml` at the repository root (cached for 5 minutes):

```yaml
enabled: true
mode: pr            # pr | push | both
strictness: balanced # lenient | balanced | strict
ignore_paths: ["docs/**"]
ignore_usernames: ["dependabot[bot]"]
skip_title_keywords: ["WIP"]
max_findings: 10
per_file_cap: 3
include_nits: false
custom_instructions:
  - "Always check for SQL injection"
instruction_files: ["AGENTS.md"]
```

### Approve/deny

Comment `approve` on an analysis issue and CodePeer applies the stored
suggestions to the default branch and opens a fix PR (`Fixes #n` in the
body auto-closes the issue on merge). Comment `deny` to close the issue as
resolved. 👍/👎 reactions on bot comments feed the learning system.

### Evaluation

```powershell
$env:LLM_API_KEY = "<key>"
go run ./cmd/eval
```

Runs the reviewer against `testdata/eval/cases` (seeded defects) and
reports precision/recall per case.

### Development

```powershell
go build ./...
go vet ./...
go test ./...                              # unit tests
$env:DATABASE_URL = "postgres://codepeer:codepeer@localhost:5434/codepeer?sslmode=disable"
go test -run TestIntegration ./internal/store/... ./internal/queue/...  # real-DB tests
```

