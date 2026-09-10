# Deploying CodePeer on Railway

[Railway](https://railway.com) is the fastest way to host CodePeer: one click
provisions the Postgres database and the bot container, wires `DATABASE_URL`
between them, and gives the service a public HTTPS domain for the GitHub
webhook. Migrations run on startup, so there is nothing to run by hand.

Two paths are supported:

| Path | Use it when |
|------|-------------|
| [One-click template](#one-click-deploy) | You want the bot running in a few minutes |
| [Infrastructure as Code](#infrastructure-as-code) | You want the project defined in git and applied from CI |

## Before you deploy

Create the GitHub App first (see [README](README.md#1-create-the-github-app)) —
the deploy form asks for its App ID and private key. Keep the App's webhook URL
blank for now; you fill it in after the deploy, once Railway has assigned a
domain. You also need a [DeepSeek](https://platform.deepseek.com) API key.

The private key downloads as a multi-line `.pem` file, and hosting forms take a
single line. Base64-encode it and paste that:

```powershell
[Convert]::ToBase64String([IO.File]::ReadAllBytes("private-key.pem")) | Set-Clipboard
```

```bash
base64 -w0 private-key.pem
```

CodePeer accepts the private key as raw PEM, base64-encoded PEM, single-line PEM
with `\n` escapes, or a file path, so any of those forms work.

## One-click deploy

[![Deploy on Railway](https://railway.com/button.svg)](https://railway.com/deploy/-9mHKY?referralCode=YZxhW4&utm_medium=integration&utm_source=button&utm_campaign=codepeer)

The template creates:

- **Postgres** — Railway managed Postgres
- **arcticworks-codepeer** — this repository, built from its `Dockerfile`,
  public on port 8080, health-checked at `/healthz`

Fill in four values on the deploy form:

| Variable | Value |
|---|---|
| `GITHUB_APP_ID` | App ID from the GitHub App settings |
| `GITHUB_APP_PRIVATE_KEY` | The base64 blob from the step above |
| `GITHUB_WEBHOOK_SECRET` | Leave the generated secret, or paste your own |
| `LLM_API_KEY` | DeepSeek API key |

Everything else — `DATABASE_URL`, `PORT`, model, queue tuning — is preset.

### After the deploy

1. Open the **arcticworks-codepeer** service, **Settings → Networking**, and
   copy the generated domain (`https://<something>.up.railway.app`).
2. In the GitHub App settings, set the webhook URL to
   `https://<something>.up.railway.app/webhook`, and set the webhook secret to
   the `GITHUB_WEBHOOK_SECRET` value from the service's **Variables** tab.
3. Install the App on the org, account, or repositories you want reviewed.
4. Check `https://<domain>/readyz` returns `ready` (the app is up and the
   database is reachable), then open a pull request to see a review.

Redeploys pick up new commits on `main` automatically. To deploy your own fork
instead, eject the service from the template repository (**Settings → Source →
Upstream Repo → Eject**) or change the source repo.

## Template specification

The template is built in the Railway [template
composer](https://railway.com/workspace/templates); this is the configuration it
holds, recorded here so it can be audited or rebuilt.

**Service `Postgres`** — Railway Postgres, default settings.

**Service `arcticworks-codepeer`** — source
`https://github.com/ArcticWorks-Software-Company/arcticworks-codepeer`, public
networking on port 8080, healthcheck path `/healthz`. Every variable carries a
description, and `BOT_LOGIN` and `GITHUB_APP_CLIENT_ID` are marked optional.

| Variable | Value in the template |
|---|---|
| `DATABASE_URL` | `${{Postgres.DATABASE_URL}}` |
| `PORT` | `8080` |
| `LOG_LEVEL` | `info` |
| `GITHUB_APP_ID` | *(empty, required at deploy)* |
| `GITHUB_APP_PRIVATE_KEY` | *(empty, required at deploy)* |
| `GITHUB_WEBHOOK_SECRET` | `${{secret(64, "abcdef0123456789")}}` |
| `LLM_API_KEY` | *(empty, required at deploy)* |
| `GITHUB_APP_CLIENT_ID` | *(empty, optional)* |
| `BOT_LOGIN` | *(empty, optional)* |
| `LLM_BASE_URL` | `https://api.deepseek.com` |
| `LLM_MODEL` | `deepseek-v4-flash` |
| `LLM_REASONING_EFFORT` | `high` |
| `LLM_TIMEOUT` | `300s` |
| `QUEUE_WORKERS` | `2` |
| `QUEUE_POLL_INTERVAL` | `2s` |
| `QUEUE_MAX_ATTEMPTS` | `5` |
| `QUEUE_LEASE_TTL` | `15m` |

## Infrastructure as Code

[`.railway/railway.ts`](.railway/railway.ts) declares the same project for
Railway's [Infrastructure as Code](https://docs.railway.com/infrastructure-as-code):

```bash
npm install railway
railway login
railway link
railway config plan
railway config apply
```

Secrets are declared as `preserve()`, which means "keep whatever is set in
Railway" — they are never written into the repository. On a fresh project,
apply once, then set the four secrets and redeploy:

```bash
railway variables --set "GITHUB_APP_ID=..." --set "LLM_API_KEY=..."
```

`railway config plan` is read-only, and `railway config apply` asks for
confirmation before it changes anything. Note that omitting a variable from
`railway.ts` deletes it on the next apply.

## Cost and operations

Two billable resources run continuously: the Postgres database and the bot
container. CodePeer is idle between webhook deliveries, so usage tracks the
volume of pull requests rather than uptime. Review work happens in a Postgres
job queue with `QUEUE_WORKERS` workers; raise it if reviews queue up on a busy
org, and raise `LLM_TIMEOUT` before raising `QUEUE_MAX_ATTEMPTS` if the model is
slow.

Logs are JSON on stdout, visible in the service's **Deployments** tab. Secrets
are never logged.
