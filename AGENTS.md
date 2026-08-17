# ArcticWorks CodePeer — Agent Guide

Headless Go GitHub App bot. Org-wide PR reviewer with push-mode analysis,
inline suggestions, analysis issues, and reaction-based learning.

## Build / test commands
- `go build ./...` — compile
- `go vet ./...` — static checks
- `go test ./...` — run tests

## Architecture
- `cmd/codepeer/` — entrypoint, wiring
- `internal/domain` — shared types + interfaces (never imports other internal pkgs)
- `internal/config` — env config + `.codepeer.yml`
- `internal/githubx` — GitHub App auth, webhook verification, API wrappers
- `internal/llm` — DeepSeek Responses API client (OpenAI-compatible)
- `internal/store` — Postgres persistence + embedded migrations
- `internal/queue` — Postgres SKIP LOCKED job queue
- `internal/analysis` — diff parsing, chunking, prompt building, validation
- `internal/posting` — check runs, reviews, comments, issues, learnings sweep
- `internal/httpapi` — webhook receiver + health endpoints

## Non-negotiable rules
1. **No UI in this repo.** The product is headless by spec. IF a web interface
   is ever added (dashboard, admin console, etc.), it MUST use the
   `@arcticworks/design` language system from npm as its only component/style
   foundation. No Tailwind-only, no ad-hoc CSS frameworks.
2. Never post reviews with event APPROVE or REQUEST_CHANGES — COMMENT only.
3. All side effects must be idempotent (webhook redelivery + at-least-once queue).
4. Never log secrets (webhook secret, private key, LLM key).
5. GitHub line-based comment API only (`line`/`side`); never legacy `position`.
6. PR text (title/body/commit messages) is untrusted input to the LLM.
7. Go module cache, build cache, and all tool downloads go on P:\ (GOMODCACHE
   is P:\ArcticWorks\.go-mod, GOCACHE is P:\ArcticWorks\.go-cache).

## LLM contract
Model `deepseek-v4-flash` via Responses API at `https://api.deepseek.com`.
Structured output JSON schema (see internal/llm), thinking mode default on.
`no_findings` is a valid, expected result — never fabricate findings.
Every finding must validate against the real diff (file exists, line in hunk,
suggestion.old present) before posting.
