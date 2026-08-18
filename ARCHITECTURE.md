# Architecture

## Project layout

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

## Webhook ingestion

`POST /webhook` verifies the `X-Hub-Signature-256` HMAC in constant time, dedupes on `X-GitHub-Delivery`, filters the bot's own events, and enqueues work. It responds in milliseconds, so GitHub's 10-second delivery deadline is never in question.

## The queue

A Postgres table with `FOR UPDATE SKIP LOCKED` claims, lease expiry with a janitor, and exponential backoff with jitter. Delivery is at-least-once by design, so every side effect is idempotent: runs are deduped on `(repo, kind, sha)`, stalled runs are restarted, and webhook redeliveries are dropped.

## Multi-agent analysis

The diff is chunked and handed to five specialist agents in parallel, each with its own focused prompt:

| Agent | Domain |
|-------|--------|
| `security` | Injection, secrets, authz, unsafe input, crypto misuse |
| `correctness` | Logic errors, off-by-one, swallowed errors, races, resource lifecycle |
| `performance` | Hot-path allocations, blocking I/O, quadratic loops |
| `maintainability` | Design, YAGNI, dead code, naming, tests, consistency |
| `ux` | Accessibility, design-token violations, input regressions |

A lead-agent compile pass merges cross-agent duplicates, preserves distinct findings, re-calibrates severity, and writes the summary. Deterministic validation runs afterward: every finding must reference a file and line that exists in the real diff, and every `suggestion.old` must be present in the file. Otherwise the finding is dropped or stripped.

## Posting

COMMENT-only reviews (never APPROVE/REQUEST_CHANGES), inline comments with `suggestion` fences, a `CodePeer` check run with annotations, and analysis issues for critical/high findings. Posting is serialized and paced to respect GitHub's content-creation rate limits.

## Approve/deny

`approve` on an analysis issue applies each stored suggestion to the default branch via the git-data API (blob, tree, commit, branch) and opens `codepeer/fix-issue-N` with `Fixes #n` in the body, so the issue closes natively on merge.
