<div align="center">
	<h1>ArcticWorks CodePeer</h1>
	<p>Code review for every pull request, from a bot you host.</p>
</div>

[![CI](https://github.com/ArcticWorks-Software-Company/arcticworks-codepeer/actions/workflows/ci.yml/badge.svg)](https://github.com/ArcticWorks-Software-Company/arcticworks-codepeer/actions/workflows/ci.yml)
[![Language](https://img.shields.io/badge/language-Go-00ADD8.svg?style=flat-square)]()
[![Model](https://img.shields.io/badge/model-deepseek--v4--flash-4D6BFE.svg?style=flat-square)]()

## What is ArcticWorks CodePeer?

CodePeer is an AI code reviewer for GitHub. Install it on any organization or personal account and it watches every repo you give it access to, reviews new code, and reports what it finds as pull request reviews, inline suggestions, and analysis issues. It is a headless Go server with no desktop or web UI. You host it, so your repos keep their secrets.

On every PR (opened, updated, reopened) CodePeer fetches the diff, runs a team of specialist LLM agents over it, and posts a review with a summary and inline comments. Small fixes arrive as native GitHub `suggestion` blocks that apply with one click. Larger findings are opened as linked analysis issues.

Every analysis issue is approve-or-deny: comment `approve` and CodePeer applies the stored fixes and opens a PR that closes the issue; comment `deny` (or fix it yourself) and the issue is closed as resolved. The bot ignores its own events, so its own PRs never retrigger it.

Who it's for: engineering teams, repository maintainers, and anyone who wants review coverage on small or solo projects.

### Features

- 📝 Pull request reviews with a summary plus severity-tagged inline comments
- 🧩 Five specialist agents (security, correctness, performance, maintainability, UX) reviewing the diff in parallel, then a lead agent compiles the findings
- ✨ One-click fixes via native GitHub `suggestion` blocks
- 🗂️ Analysis issues for critical and high findings, with approve/deny commands
- 🔧 Auto-fix PRs when you comment `approve`
- 📈 Push mode with a rolling analysis issue per repo, appended on default-branch pushes
- ⚙️ Per-repo config through `.codepeer.yml`
- 🧠 Reaction-based learning to suppress disliked findings

## Installing and running

You need a GitHub App (your own credentials), a DeepSeek API key, and somewhere to run the container. Everything else comes with the repo.

### 1. Create the GitHub App

On your account or org: Settings, Developer settings, GitHub Apps, New GitHub App.

| Permission | Level | Why |
|------------|-------|-----|
| Pull requests | Read & write | Submit reviews and inline comments |
| Contents | Read | Diffs, `.codepeer.yml`, file context |
| Issues | Read & write | Analysis issues, approve/deny |
| Checks | Read & write | Status check run |
| Metadata | Read | Implicit |

Events: `pull_request`, `push`, `issue_comment`, `installation`, `installation_repositories`.

Generate a private key (PEM), note the App ID and Client ID, then install the app wherever you want coverage: an org, a personal account, or a single repo.

### 2. Configure

Copy `.env.example` to `.env` and fill in:

| Variable | Description |
|---|---|
| `DATABASE_URL` | Postgres DSN |
| `GITHUB_APP_ID` | App ID from the App settings |
| `GITHUB_APP_CLIENT_ID` | Client ID (JWT issuer; optional) |
| `GITHUB_APP_PRIVATE_KEY` | Path to the PEM file, or the PEM contents |
| `GITHUB_WEBHOOK_SECRET` | High-entropy secret; same value on the App webhook |
| `LLM_API_KEY` | DeepSeek API key |
| `LLM_BASE_URL` | `https://api.deepseek.com` (default) |
| `LLM_MODEL` | `deepseek-v4-flash` (default) |
| `BOT_LOGIN` | Bot login for self-event filtering (auto-resolved if empty) |

Pre-flight validation:

```powershell
go run ./cmd/codepeer check          # config, key, DB, migrations, GitHub auth
go run ./cmd/codepeer check --llm    # additionally pings the DeepSeek API
```

### 3. Run it

```powershell
Copy-Item .env.example .env        # fill in the values from step 2
docker compose up -d --build       # Docker Desktop
.\scripts\docker-podman.ps1 compose up -d --build   # Podman (Windows)
```

Migrations run automatically on startup. Health endpoints: `GET /healthz` (liveness) and `GET /readyz` (database reachable).

### 4. Point the webhook at it

In the App settings, set the webhook URL to `https://<your-host>/webhook` with the secret from `.env`. For local development GitHub cannot reach `localhost`, so use a Smee tunnel:

```powershell
# 1. Create a channel at https://smee.io/new
# 2. Set the App webhook URL to https://smee.io/<channel> (same secret)
.\scripts\smee.ps1 -Channel https://smee.io/<your-channel>
```

### 5. Smoke test

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\smoke-webhook.ps1 -Secret <webhook-secret>
```

Posts a signed `pull_request.opened` delivery to `localhost:8080/webhook` and prints the HTTP status (expect `202`).

## Bot commands

On any analysis issue:

| Command | What it does |
|---------|-------------|
| `approve` | Applies stored suggestions to the default branch and opens a fix PR with `Fixes #n` |
| `deny` | Closes the issue as resolved |
| Thumbs up / down | Feeds the learning system; repeated downvotes suppress similar findings |

## Per-repo config

`.codepeer.yml` at the repository root (cached for 5 minutes):

```yaml
enabled: true
mode: pr            # pr | push | both
strictness: balanced # lenient | balanced | strict
ignore_paths: ["docs/**"]
ignore_usernames: ["some-contractor"]  # dependabot, renovate, and CI bots are ignored by default
skip_title_keywords: ["WIP"]
max_findings: 10
per_file_cap: 3
include_nits: false
# Specialist agents run in parallel, then a lead agent compiles the findings.
# agents: [] reverts to a single general review pass.
agents: [security, correctness, performance, maintainability, ux]
custom_instructions:
  - "Always check for SQL injection"
instruction_files: ["AGENTS.md"]
```

## What it can and can't do

**Can do:**

- Review every PR in every installed repo, with summary plus anchored inline comments
- Run five specialist agents in parallel and compile their findings
- Post one-click `suggestion` blocks for small fixes
- Open analysis issues and turn `approve` into a fix PR with `Fixes #n`
- Maintain a rolling analysis issue per repo in push mode
- Learn from thumbs up/down reactions and suppress disliked findings
- Run fully standalone: GitHub App auth only, no Identity dependency

**Can't do:**

- Approve or request changes; reviews are COMMENT-only by design
- Guarantee perfect recall; every finding should be verified by a human
- Retroactively review PRs that existed before the App was installed
- Create issues on repos with issues disabled

## Documentation

- [Architecture](ARCHITECTURE.md)
- [Agent guide](AGENTS.md)

## Credits

Built with help from:

- [DeepSeek API](https://api-docs.deepseek.com/): the review engine (`deepseek-v4-flash`, Responses API)
- [go-github](https://github.com/google/go-github): GitHub REST client
- [pgx](https://github.com/jackc/pgx): Postgres driver
- [Google eng-practices](https://google.github.io/eng-practices/review/): the review standards this bot mimics
- CodeRabbit, Sourcery, Copilot code review, and PR-Agent: architecture inspiration

## Author

**ArcticWorks CodePeer** © [ArcticWorks Software Company](https://github.com/ArcticWorks-Software-Company). Headless by design. Always verify AI feedback.
