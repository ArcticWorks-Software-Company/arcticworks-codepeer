<div align="center">
	<h1>ArcticWorks CodePeer</h1>
	<p><b>Code review that never sleeps.</b></p>
</div>

[![CI](https://github.com/ArcticWorks-Software-Company/arcticworks-codepeer/actions/workflows/ci.yml/badge.svg)]()
[![Language](https://img.shields.io/badge/language-Go-00ADD8.svg?style=flat-square)]()
[![Model](https://img.shields.io/badge/model-deepseek--v4--flash-4D6BFE.svg?style=flat-square)]()
[![Platform](https://img.shields.io/badge/platform-GitHub%20App%20%2B%20Docker-0078D6.svg?style=flat-square)]()

### What does this do?

ArcticWorks CodePeer is an AI code reviewer for GitHub. Install it on any organization or personal account — it watches every repo you give it access to, reviews new code, and reports what it finds as pull-request reviews, inline suggestions, and analysis issues. Headless Go server; no desktop or web UI. You host it; your repos keep their secrets.

### How even?

On every PR (opened, updated, reopened) CodePeer fetches the diff, runs a team of specialist LLM agents over it, and posts a review with a summary and inline comments. Small fixes arrive as native GitHub `suggestion` blocks — one click to apply. Larger findings are opened as linked analysis issues. On every push it can optionally append findings to a single rolling analysis issue per repo instead of flooding the tracker.

Every analysis issue is approve-or-deny: comment `approve` and CodePeer applies the stored fixes and opens a PR that closes the issue; comment `deny` — or fix it yourself — and the issue is closed as resolved. The bot ignores its own events, so its own PRs never retrigger it.

Who it's for: engineering teams, repository maintainers, and anyone who wants review coverage on small or solo projects.

The things you get:

1. **PR review** - Summary review plus severity-tagged inline comments on `pull_request.opened` / `synchronize` / `reopened`
2. **Multi-agent analysis** - Five specialist agents (security, correctness, performance, maintainability, UX) review the diff in parallel, then a lead agent compiles and calibrates the findings
3. **One-click fixes** - Native GitHub `suggestion` blocks for small fixes
4. **Analysis issues** - Critical and high findings become linked issues with approve/deny commands
5. **Auto-fix PRs** - `approve` builds a fix branch via the git-data API and opens a PR with `Fixes #n`
6. **Push mode** - Rolling analysis issue per repo, appended on default-branch pushes
7. **Per-repo config** - `.codepeer.yml`: enable/disable, ignore paths, strictness, mode, agent mix
8. **Async queue** - Postgres SKIP LOCKED job queue; the webhook never blocks
9. **Learning loop** - Thumbs up/down on bot comments suppress similar future findings

### How do I use it?

You need a GitHub App (your own credentials), a DeepSeek API key, and somewhere to run the container. Everything else comes with the repo.

#### 1. Create the GitHub App

On your account or org: Settings -> Developer settings -> GitHub Apps -> New GitHub App.

| Permission | Level | Why |
|------------|-------|-----|
| Pull requests | Read & write | Submit reviews and inline comments |
| Contents | Read | Diffs, `.codepeer.yml`, file context |
| Issues | Read & write | Analysis issues, approve/deny |
| Checks | Read & write | Status check run |
| Metadata | Read | Implicit |

Events: `pull_request`, `push`, `issue_comment`, `installation`, `installation_repositories`.
Generate a private key (PEM), note the App ID and Client ID, then install the app wherever you want coverage — an org, a personal account, or a single repo.

#### 2. Configure

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

#### 3. Run it

```powershell
Copy-Item .env.example .env        # fill in the values from step 2
docker compose up -d --build       # Docker Desktop
.\scripts\docker-podman.ps1 compose up -d --build   # Podman (Windows)
```

Migrations run automatically on startup. Health endpoints: `GET /healthz` (liveness) and `GET /readyz` (database reachable).

#### 4. Point the webhook at it

In the App settings, set the webhook URL to `https://<your-host>/webhook` with the secret from `.env`. For local development GitHub cannot reach `localhost` — use a Smee tunnel:

```powershell
# 1. Create a channel at https://smee.io/new
# 2. Set the App webhook URL to https://smee.io/<channel> (same secret)
.\scripts\smee.ps1 -Channel https://smee.io/<your-channel>
```

#### 5. Smoke test

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\smoke-webhook.ps1 -Secret <webhook-secret>
```

Posts a signed `pull_request.opened` delivery to `localhost:8080/webhook` and prints the HTTP status (expect `202`).

### CLI reference

```
  check [--llm]         Pre-flight validation of config, DB, GitHub auth, LLM
  sync                  Pull installations and repos from the GitHub API into the store
  webhook <url>         Update the app-level webhook URL via the API
  version               Print the version

  go run ./cmd/eval     Seeded-defect precision/recall evaluation (needs LLM_API_KEY)
```

Bot commands, on any analysis issue:

| Command | What it does |
|---------|-------------|
| `approve` | Applies stored suggestions to the default branch, opens a fix PR with `Fixes #n` |
| `deny` | Closes the issue as resolved |
| Thumbs up / down | Feed the learning system; repeated downvotes suppress similar findings |

### How it works under the hood

#### Webhook ingestion

`POST /webhook` verifies the `X-Hub-Signature-256` HMAC in constant time, dedupes on `X-GitHub-Delivery`, filters the bot's own events, and enqueues work — responding in milliseconds so GitHub's 10-second delivery deadline is never in question.

#### The queue

A Postgres table with `FOR UPDATE SKIP LOCKED` claims, lease expiry and a janitor, and exponential backoff with jitter. At-least-once by design, so every side effect is idempotent: runs are deduped on `(repo, kind, sha)`, stalled runs are restarted, and webhook redeliveries are dropped.

#### Multi-agent analysis

The diff is chunked and handed to five specialist agents in parallel, each with its own focused prompt:

| Agent | Domain |
|-------|--------|
| `security` | Injection, secrets, authz, unsafe input, crypto misuse |
| `correctness` | Logic errors, off-by-one, swallowed errors, races, resource lifecycle |
| `performance` | Hot-path allocations, blocking I/O, quadratic loops |
| `maintainability` | Design, YAGNI, dead code, naming, tests, consistency |
| `ux` | Accessibility, design-token violations, input regressions |

A lead-agent compile pass then merges cross-agent duplicates, preserves distinct findings, re-calibrates severity, and writes the summary. Deterministic validation runs afterward: every finding must reference a file and line that exists in the real diff, and every `suggestion.old` must be present in the file — otherwise it is dropped or stripped.

#### Posting

COMMENT-only reviews (never APPROVE/REQUEST_CHANGES), inline comments with `suggestion` fences, a `CodePeer` check run with annotations, and analysis issues for critical/high findings. Posting is serialized and paced to respect GitHub's content-creation rate limits.

#### Approve/deny

`approve` on an analysis issue applies each stored suggestion to the default branch via the git-data API (blob -> tree -> commit -> branch) and opens `codepeer/fix-issue-N` with `Fixes #n` in the body, so the issue closes natively on merge.

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
# Specialist agents run in parallel, then a lead agent compiles the findings.
# agents: [] reverts to a single general review pass.
agents: [security, correctness, performance, maintainability, ux]
custom_instructions:
  - "Always check for SQL injection"
instruction_files: ["AGENTS.md"]
```

### Project layout

```
arcticworks-codepeer/
├── cmd/
│   ├── codepeer/           The bot server
│   │   ├── main.go         Entry point, worker pool wiring
│   │   ├── check.go        codepeer check pre-flight validation
│   │   ├── sync.go         codepeer sync repo discovery
│   │   └── webhook.go      codepeer webhook URL management
│   └── eval/               Seeded-defect precision/recall harness
├── internal/
│   ├── domain/             Shared types and interfaces
│   ├── config/             Env loading + .codepeer.yml parsing
│   ├── githubx/            App auth, webhook verification, API wrappers
│   ├── llm/                DeepSeek Responses API client, agents, compiler
│   ├── store/              Postgres persistence + embedded migrations
│   ├── queue/              SKIP LOCKED job queue
│   ├── analysis/           Diff parsing, chunking, multi-agent pipeline
│   ├── posting/            Reviews, comments, issues, learnings sweep
│   └── httpapi/            Webhook receiver + health endpoints
├── scripts/
│   ├── smoke-webhook.ps1   Signed webhook generator
│   ├── smee.ps1            Smee tunnel helper
│   └── docker-podman.ps1   Docker CLI against the Podman engine
├── testdata/eval/cases/    Seeded-defect dataset
├── Dockerfile
├── docker-compose.yml
└── AGENTS.md               Agent guide and non-negotiable rules
```

### What it can and can't do

**Can do:**
- Review every PR in every installed repo, with summary plus anchored inline comments
- Run five specialist agents in parallel and compile their findings
- Post one-click `suggestion` blocks for small fixes
- Open analysis issues and turn `approve` into a fix PR with `Fixes #n`
- Maintain a rolling analysis issue per repo in push mode
- Learn from thumbs up/down reactions and suppress disliked findings
- Run fully standalone: GitHub App auth only, no Identity dependency

**Can't do:**
- Approve or request changes — reviews are COMMENT-only by design
- Guarantee perfect recall; every finding should be verified by a human
- Retroactively review PRs that existed before the App was installed
- Create issues on repos with issues disabled

**Deferred:**
- Reaction-based approve/deny (reactions currently feed learning only)
- Optional enterprise identity integrations (runs fully standalone)

### Credits

Built with help from:

- [DeepSeek API](https://api-docs.deepseek.com/) - the review engine (`deepseek-v4-flash`, Responses API)
- [go-github](https://github.com/google/go-github) - GitHub REST client
- [pgx](https://github.com/jackc/pgx) - Postgres driver
- [Google eng-practices](https://google.github.io/eng-practices/review/) - the review standards this bot mimics
- CodeRabbit, Sourcery, Copilot code review, and PR-Agent - architecture inspiration

---

<div align="center">
	<br/><br/>
	<i>headless by design. reviews responsibly. always verify AI feedback.</i>
	<br/>
	<sub>self-hostable. bring your own app, key, and repos.</sub>
</div>
