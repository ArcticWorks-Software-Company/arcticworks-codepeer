// Railway Infrastructure as Code for ArcticWorks CodePeer.
//
// One project, two resources: a managed Postgres database and the bot service
// built from this repository's Dockerfile. Migrations run on startup, so no
// pre-deploy command is needed.
//
//   npm install railway          # the TypeScript IaC SDK
//   railway login && railway link
//   railway config plan          # preview
//   railway config apply         # apply after confirmation
//
// Secrets are declared as preserve() so their values live in Railway and never
// in git. On a fresh project, apply once, set the four secret values (dashboard
// or `railway variables --set`), then redeploy. For a one-click alternative,
// see the "Deploy on Railway" button in README.md and RAILWAY.md.

import {
  defineRailway,
  github,
  postgres,
  preserve,
  project,
  service,
} from "railway/iac";

export default defineRailway(() => {
  const db = postgres("Postgres");

  const bot = service("arcticworks-codepeer", {
    source: github("ArcticWorks-Software-Company/arcticworks-codepeer", {
      branch: "main",
    }),
    healthcheck: "/healthz",
    env: {
      PORT: "8080",
      LOG_LEVEL: "info",
      DATABASE_URL: db.env.DATABASE_URL,

      // Set these once in Railway; preserve() keeps them out of source.
      GITHUB_APP_ID: preserve(),
      GITHUB_APP_PRIVATE_KEY: preserve(),
      GITHUB_WEBHOOK_SECRET: preserve(),
      LLM_API_KEY: preserve(),

      // Optional overrides.
      GITHUB_APP_CLIENT_ID: preserve(),
      BOT_LOGIN: "",
      LLM_BASE_URL: "https://api.deepseek.com",
      LLM_MODEL: "deepseek-v4-flash",
      LLM_REASONING_EFFORT: "high",
      LLM_TIMEOUT: "300s",
      QUEUE_WORKERS: "2",
      QUEUE_POLL_INTERVAL: "2s",
      QUEUE_MAX_ATTEMPTS: "5",
      QUEUE_LEASE_TTL: "15m",
    },
  });

  return project("codepeer", {
    resources: [db, bot],
  });
});
