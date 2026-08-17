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
