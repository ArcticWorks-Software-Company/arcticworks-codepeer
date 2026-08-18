<div align="center">
	<h1>ArcticWorks CodePeer</h1>
	<p>为每个拉取请求做代码审查，由你自己托管的机器人完成。</p>
</div>

[English](README.md) · [简体中文](README.zh-CN.md)

[![CI](https://github.com/ArcticWorks-Software-Company/arcticworks-codepeer/actions/workflows/ci.yml/badge.svg)](https://github.com/ArcticWorks-Software-Company/arcticworks-codepeer/actions/workflows/ci.yml)
[![Language](https://img.shields.io/badge/language-Go-00ADD8.svg?style=flat-square)]()
[![Model](https://img.shields.io/badge/model-deepseek--v4--flash-4D6BFE.svg?style=flat-square)]()

## ArcticWorks CodePeer 是什么？

CodePeer 是一个面向 GitHub 的 AI 代码审查机器人。安装到任意组织或个人账户后，它会监视你授权的每个仓库，审查新代码，并把发现以拉取请求审查、行内建议和分析议题的形式反馈给你。它是一个无头的 Go 服务，没有桌面端或网页界面。由你自己托管，你的仓库秘密不会外泄。

每当 PR 打开、更新或重新打开时，CodePeer 会拉取 diff，用一组专业 LLM 智能体进行分析，并发布包含总结和行内评论的审查。小修复会以 GitHub 原生 `suggestion` 代码块的形式出现，一键即可应用。较大的发现会作为关联的分析议题打开。

每个分析议题都是批准或拒绝制：评论 `approve`，CodePeer 会应用已存储的修复并打开一个关闭该议题的 PR；评论 `deny`（或自己修复），议题则以已解决状态关闭。机器人会忽略自己的事件，因此它自己的 PR 永远不会触发它自己。

目标用户：工程团队、仓库维护者，以及任何希望小项目或个人项目也有审查覆盖的人。

### 功能

- 📝 拉取请求审查，附总结和按严重程度标记的行内评论
- 🧩 五个专业智能体（安全、正确性、性能、可维护性、UX）并行审查 diff，再由主智能体汇总发现
- ✨ 通过 GitHub 原生 `suggestion` 代码块一键修复
- 🗂️ 针对严重和高危发现的分析议题，支持 approve/deny 命令
- 🔧 评论 `approve` 即自动生成修复 PR
- 📈 推送模式：每个仓库一个滚动分析议题，在默认分支推送时追加
- ⚙️ 通过 `.codepeer.yml` 进行仓库级配置
- 🧠 基于反馈的学习，抑制不受欢迎的发现

## 安装与运行

你需要一个 GitHub App（你自己的凭据）、一个 DeepSeek API 密钥，以及运行容器的地方。其余一切都随仓库提供。

### 1. 创建 GitHub App

在你的账户或组织下：Settings、Developer settings、GitHub Apps、New GitHub App。

| 权限 | 级别 | 用途 |
|------------|-------|-----|
| Pull requests | Read & write | 提交审查和行内评论 |
| Contents | Read | Diff、`.codepeer.yml`、文件上下文 |
| Issues | Read & write | 分析议题、approve/deny |
| Checks | Read & write | 状态检查运行 |
| Metadata | Read | 隐式 |

事件：`pull_request`、`push`、`issue_comment`、`installation`、`installation_repositories`。

生成私钥（PEM），记下 App ID 和 Client ID，然后在需要覆盖的地方安装应用：组织、个人账户或单个仓库。

### 2. 配置

复制 `.env.example` 为 `.env` 并填写：

| 变量 | 说明 |
|---|---|
| `DATABASE_URL` | Postgres DSN |
| `GITHUB_APP_ID` | App 设置中的 App ID |
| `GITHUB_APP_CLIENT_ID` | Client ID（JWT 签发者；可选） |
| `GITHUB_APP_PRIVATE_KEY` | PEM 文件路径，或 PEM 内容 |
| `GITHUB_WEBHOOK_SECRET` | 高熵密钥；与 App webhook 上保持一致 |
| `LLM_API_KEY` | DeepSeek API 密钥 |
| `LLM_BASE_URL` | `https://api.deepseek.com`（默认） |
| `LLM_MODEL` | `deepseek-v4-flash`（默认） |
| `BOT_LOGIN` | 机器人登录名，用于过滤自身事件（留空自动解析） |

预检验证：

```powershell
go run ./cmd/codepeer check          # 配置、密钥、数据库、迁移、GitHub 认证
go run ./cmd/codepeer check --llm    # 额外 ping DeepSeek API
```

### 3. 运行

```powershell
Copy-Item .env.example .env        # 按第 2 步填写数值
docker compose up -d --build       # Docker Desktop
.\scripts\docker-podman.ps1 compose up -d --build   # Podman（Windows）
```

迁移在启动时自动运行。健康端点：`GET /healthz`（存活）和 `GET /readyz`（数据库可达）。

### 4. 将 webhook 指向它

在 App 设置中，把 webhook URL 设为 `https://<your-host>/webhook`，密钥与 `.env` 一致。本地开发时 GitHub 无法访问 `localhost`，使用 Smee 隧道：

```powershell
# 1. 在 https://smee.io/new 创建频道
# 2. 将 App webhook URL 设为 https://smee.io/<channel>（密钥一致）
.\scripts\smee.ps1 -Channel https://smee.io/<your-channel>
```

### 5. 冒烟测试

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\smoke-webhook.ps1 -Secret <webhook-secret>
```

向 `localhost:8080/webhook` 发送一条签名的 `pull_request.opened` 投递并打印 HTTP 状态码（预期 `202`）。

## 机器人命令

在任意分析议题上：

| 命令 | 作用 |
|---------|-------------|
| `approve` | 将存储的建议应用到默认分支，并打开带 `Fixes #n` 的修复 PR |
| `deny` | 以已解决状态关闭议题 |
| 点赞 / 点踩 | 喂给学习系统；反复点踩会抑制类似发现 |

## 仓库级配置

仓库根目录下的 `.codepeer.yml`（缓存 5 分钟）：

```yaml
enabled: true
mode: pr            # pr | push | both
strictness: balanced # lenient | balanced | strict
ignore_paths: ["docs/**"]
ignore_usernames: ["some-contractor"]  # dependabot、renovate 和 CI 机器人默认忽略
skip_title_keywords: ["WIP"]
max_findings: 10
per_file_cap: 3
include_nits: false
# 专业智能体并行运行，再由主智能体汇总发现。
# agents: [] 回退为单次通用审查。
agents: [security, correctness, performance, maintainability, ux]
custom_instructions:
  - "Always check for SQL injection"
instruction_files: ["AGENTS.md"]
```

## 能做什么，不能做什么

**能做：**

- 审查每个已安装仓库的每个 PR，附总结和锚定的行内评论
- 并行运行五个专业智能体并汇总其发现
- 为小修复发布一键 `suggestion` 代码块
- 打开分析议题，并把 `approve` 变成带 `Fixes #n` 的修复 PR
- 在推送模式下为每个仓库维护滚动分析议题
- 从点赞/点踩反应中学习，抑制不受欢迎的发现
- 完全独立运行：仅 GitHub App 认证，不依赖 Identity

**不能做：**

- 批准或请求变更；审查按设计仅为 COMMENT
- 保证完美召回；每个发现都应由人复核
- 追溯审查 App 安装前已存在的 PR
- 在禁用议题的仓库上创建议题

## 文档

- [架构](ARCHITECTURE.md)
- [智能体指南](AGENTS.md)

## 致谢

构建过程中使用了：

- [DeepSeek API](https://api-docs.deepseek.com/)：审查引擎（`deepseek-v4-flash`，Responses API）
- [go-github](https://github.com/google/go-github)：GitHub REST 客户端
- [pgx](https://github.com/jackc/pgx)：Postgres 驱动
- [Google eng-practices](https://google.github.io/eng-practices/review/)：本机器人所模仿的审查标准
- CodeRabbit、Sourcery、Copilot code review 和 PR-Agent：架构灵感

## 作者

**ArcticWorks CodePeer** © [ArcticWorks Software Company](https://github.com/ArcticWorks-Software-Company)。按设计保持无头。永远复核 AI 反馈。
